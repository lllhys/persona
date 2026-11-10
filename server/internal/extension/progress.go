package extension

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/nwverse/persona/server/internal/i18n"
)

// Installing in steps (docs/protocol/protocol.md "Extension install, upgrade and uninstall progress"): a preview says which step it is at through the
// context, so the hub can show an install's progress without the registry knowing about operations.

// The steps of an install or upgrade, in order, and of an uninstall.
const (
	StepDownload = "download"
	StepVerify   = "verify"
	StepUnpack   = "unpack"
	StepCheck    = "check"
	StepConsent  = "consent"
	StepInstall  = "install"
	StepStart    = "start"
	StepTools    = "tools" // installing its command-line programs (tools.go), between consent and install
	StepNodes    = "nodes"
	StepStop     = "stop"
	StepRemove   = "remove"
)

// A step's states.
const (
	StepPending = "pending"
	StepRunning = "running"
	StepDone    = "done"
	StepFailed  = "failed"
	StepSkipped = "skipped"
)

// StepEvent is what a step reports: its state, and for a download the bytes so far and the size (0: not known),
// and whether the package came from the cache.
type StepEvent struct {
	State  string
	Bytes  int64
	Total  int64
	Cached bool
	// Tool is the tools step's program the event is about ("" the step itself), ToolState its state and Reused
	// whether it is the machine's own copy.
	Tool      string
	ToolState string
	Reused    bool
}

// ProgressFunc receives the steps a preview goes through.
type ProgressFunc func(step string, ev StepEvent)

type progressKey struct{}

// WithProgress is ctx with f receiving the steps of what is done with it.
func WithProgress(ctx context.Context, f ProgressFunc) context.Context {
	return context.WithValue(ctx, progressKey{}, f)
}

// WithoutProgress is ctx whose steps nobody receives (a bundle's previews, inside the main one's check).
func WithoutProgress(ctx context.Context) context.Context { return WithProgress(ctx, nil) }

// report tells ctx's ProgressFunc, if any.
func report(ctx context.Context, step string, ev StepEvent) {
	if f, _ := ctx.Value(progressKey{}).(ProgressFunc); f != nil {
		f(step, ev)
	}
}

func reportState(ctx context.Context, step, state string) { report(ctx, step, StepEvent{State: state}) }

// ErrDownloadStalled: a download got no data for StallAfter.
var ErrDownloadStalled = errors.New("download stalled")

// ErrChecksum: a downloaded package is not the one its index (or URL) pins.
var ErrChecksum = errors.New("checksum mismatch")

// kindErr is an error with its own text that is also kind (errors.Is), without kind's text tacked onto it.
type kindErr struct {
	msg  string
	kind error
}

func (e kindErr) Error() string { return e.msg }
func (e kindErr) Unwrap() error { return e.kind }

func errOfKind(kind error, format string, args ...any) error {
	return kindErr{fmt.Sprintf(format, args...), kind}
}

// kindMessage is a translated message that is also kind (errors.Is): an operation shows it in its own language
// (i18n.Localize uses its Text), where errOfKind would keep the default language's text.
type kindMessage struct {
	*i18n.Message
	kind error
}

func (e kindMessage) Unwrap() error { return e.kind }

func messageOfKind(kind error, key string, args ...i18n.Args) error {
	return kindMessage{i18n.Err(key, args...).(*i18n.Message), kind}
}

// StallAfter is how long a download may get no data before it gives up (rather than wait out its whole timeout: a
// connection that stops sending mid-way was the usual reason an install took minutes, #3277). Tests shorten it.
var StallAfter = 30 * time.Second

// progressWriter counts what is written to w, reports it (at most every 200 ms) and cancels the download when
// nothing comes for StallAfter.
type progressWriter struct {
	ctx    context.Context
	w      io.Writer
	total  int64
	n      int64
	last   time.Time
	timer  *time.Timer
	mu     sync.Mutex
	stalls bool
}

// watchDownload is w counting into ctx's progress; ctx is cancelled when the download stalls. stop ends the watch and
// says whether it stalled.
func watchDownload(ctx context.Context, w io.Writer, total int64) (context.Context, *progressWriter, func() bool) {
	ctx, cancel := context.WithCancel(ctx)
	pw := &progressWriter{ctx: ctx, w: w, total: total}
	pw.timer = time.AfterFunc(StallAfter, func() {
		pw.mu.Lock()
		pw.stalls = true
		pw.mu.Unlock()
		cancel()
	})
	report(ctx, StepDownload, StepEvent{State: StepRunning, Total: total})
	return ctx, pw, func() bool {
		pw.timer.Stop()
		cancel()
		pw.mu.Lock()
		defer pw.mu.Unlock()
		return pw.stalls
	}
}

func (pw *progressWriter) Write(b []byte) (int, error) {
	n, err := pw.w.Write(b)
	pw.timer.Reset(StallAfter)
	pw.n += int64(n)
	if now := time.Now(); now.Sub(pw.last) >= 200*time.Millisecond {
		pw.last = now
		report(pw.ctx, StepDownload, StepEvent{State: StepRunning, Bytes: pw.n, Total: pw.total})
	}
	return n, err
}

// The package cache: market packages by their sha256, so installing again (a retry, a reinstall, a member of two
// packs) does not download again. At most cacheKeep files are kept, the least recently used go first.
const cacheKeep = 16

// cached is the cached package with sha256 sum, and the name it was downloaded as ("" when there is none).
func cached(dir, sum string) (file, name string) {
	if dir == "" || sum == "" {
		return "", ""
	}
	matches, _ := filepath.Glob(filepath.Join(dir, strings.ToLower(sum)+"-*"))
	if len(matches) == 0 {
		return "", ""
	}
	now := time.Now()
	_ = os.Chtimes(matches[0], now, now) // recently used
	return matches[0], strings.TrimPrefix(filepath.Base(matches[0]), strings.ToLower(sum)+"-")
}

// keep puts a checked download into the cache (a link when it can, else a copy) and trims the cache.
func keep(dir, sum, name, file string) {
	if dir == "" || sum == "" {
		return
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return
	}
	base := filepath.Base(name)
	if base == "" || base == "." || base == "/" {
		base = "package"
	}
	to := filepath.Join(dir, strings.ToLower(sum)+"-"+base)
	if err := os.Link(file, to); err != nil && !errors.Is(err, os.ErrExist) {
		if err := copyFile(file, to+".part", 0o600); err != nil {
			_ = os.Remove(to + ".part")
			return
		}
		_ = os.Rename(to+".part", to)
	}
	entries, _ := os.ReadDir(dir)
	type kept struct {
		path string
		at   time.Time
	}
	var all []kept
	for _, e := range entries {
		if fi, err := e.Info(); err == nil && fi.Mode().IsRegular() && !strings.HasSuffix(e.Name(), ".part") {
			all = append(all, kept{filepath.Join(dir, e.Name()), fi.ModTime()})
		}
	}
	slices.SortFunc(all, func(a, b kept) int { return b.at.Compare(a.at) })
	for _, k := range all[min(len(all), cacheKeep):] {
		_ = os.Remove(k.path)
	}
}
