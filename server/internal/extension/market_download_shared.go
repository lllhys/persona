package extension

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Shared package downloads (#3692). On a slow link a phone gave up on its preview before the package arrived, and
// the download went with it: asking again started from nothing, next to a second download of the same package on
// the same link, which then stalled. Now a package (by its cache and sha256) downloads once at a time: who asks for
// it meanwhile waits for that download and sees its progress, and a download whose callers all went away goes on
// into the cache within its budget, so the next try joins it or finds the package there. Only the user's Cancel
// (a caller leaving with ErrUserCanceled) stops it, when nobody else waits for it.

// ErrUserCanceled is the cause a preview's context is canceled with when the user cancels it: a download nobody
// else waits for then stops, instead of going on into the cache.
var ErrUserCanceled = errors.New("canceled by the user")

// staleFetchAfter: a download's directory in the cache with no write for this long is left from a run that ended
// mid-way (a running one writes at least every StallAfter, or gives up).
const staleFetchAfter = 10 * time.Minute

var fetches = struct {
	sync.Mutex
	m map[string]*fetching
}{m: map[string]*fetching{}}

// fetching is one download going on; its fields but emit's are guarded by fetches.
type fetching struct {
	dir     string // its own directory in the cache, removed once it ended and nobody waits for it
	cancel  context.CancelFunc
	done    chan struct{}
	waiters int
	ended   bool
	name    string // when ended: the name it goes by, and file in dir, or why it failed
	file    string
	err     error

	// emit: what each step reported last (for a caller joining on the way), and who gets what comes next.
	emit     sync.Mutex
	last     map[string]StepEvent
	order    []string
	watchers map[int]context.Context
	nextID   int
}

// sharedFetch downloads pkg to file through the one download of it going on, starting it when there is none.
func (m *Market) sharedFetch(ctx context.Context, pkg *MarketPackage, oci *ociReference, file string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err // nothing starts for a caller already gone
	}
	key := filepath.Clean(m.Cache) + "\x00" + strings.ToLower(pkg.SHA256)
	fetches.Lock()
	f := fetches.m[key]
	if f == nil {
		// One that ended since the caller looked in the cache put the package there.
		if from, _ := cached(m.Cache, pkg.SHA256); from != "" {
			fetches.Unlock()
			if name, ok, err := m.fromCache(ctx, pkg, oci != nil, file); err != nil || ok {
				return name, err
			}
			fetches.Lock()
			f = fetches.m[key]
		}
	}
	if f == nil {
		var err error
		if f, err = m.startFetch(key, pkg, oci); err != nil {
			fetches.Unlock()
			return "", err
		}
	}
	f.waiters++
	fetches.Unlock()
	id := f.watch(ctx)
	select {
	case <-f.done:
	case <-ctx.Done():
	}
	f.unwatch(id)
	fetches.Lock()
	defer fetches.Unlock()
	f.waiters--
	if !f.ended {
		if f.waiters == 0 && errors.Is(context.Cause(ctx), ErrUserCanceled) {
			if fetches.m[key] == f {
				delete(fetches.m, key) // whoever asks next starts a new one
			}
			f.cancel()
		}
		return "", ctx.Err()
	}
	defer f.idle()
	if f.err != nil {
		return "", f.err
	}
	if err := os.Link(f.file, file); err != nil {
		if err := copyFile(f.file, file, 0o600); err != nil {
			return "", err
		}
	}
	return f.name, nil
}

// startFetch starts pkg's download (fetches held).
func (m *Market) startFetch(key string, pkg *MarketPackage, oci *ociReference) (*fetching, error) {
	if err := os.MkdirAll(m.Cache, 0o700); err != nil {
		return nil, err
	}
	dropStaleFetches(m.Cache)
	dir, err := os.MkdirTemp(m.Cache, ".fetch-")
	if err != nil {
		return nil, err
	}
	f := &fetching{dir: dir, done: make(chan struct{}), last: map[string]StepEvent{}, watchers: map[int]context.Context{}}
	ctx, cancel := context.WithTimeoutCause(WithProgress(context.Background(), f.report), m.totalBudget(pkg), errOverBudget)
	f.cancel = cancel
	fetches.m[key] = f
	go func() {
		defer cancel()
		file := filepath.Join(dir, "download")
		name, err := m.fetch(ctx, pkg, oci, file)
		fetches.Lock()
		defer fetches.Unlock()
		if err == nil {
			keep(m.Cache, pkg.SHA256, name, file) // before it leaves the list: who looks next finds it here
		}
		if fetches.m[key] == f {
			delete(fetches.m, key)
		}
		f.ended, f.name, f.file, f.err = true, name, file, err
		close(f.done)
		f.idle()
	}()
	return f, nil
}

// idle removes the directory of a download that ended once nobody waits for it (fetches held); the package stays
// in the cache.
func (f *fetching) idle() {
	if f.waiters == 0 {
		_ = os.RemoveAll(f.dir)
	}
}

// report is the download's progress, passed on to everyone waiting for it.
func (f *fetching) report(step string, ev StepEvent) {
	f.emit.Lock()
	defer f.emit.Unlock()
	if _, ok := f.last[step]; !ok {
		f.order = append(f.order, step)
	}
	f.last[step] = ev
	for _, ctx := range f.watchers {
		report(ctx, step, ev)
	}
}

// watch has ctx's progress get the download's, starting with where it is now.
func (f *fetching) watch(ctx context.Context) int {
	f.emit.Lock()
	defer f.emit.Unlock()
	for _, step := range f.order {
		report(ctx, step, f.last[step])
	}
	f.nextID++
	f.watchers[f.nextID] = ctx
	return f.nextID
}

func (f *fetching) unwatch(id int) {
	f.emit.Lock()
	defer f.emit.Unlock()
	delete(f.watchers, id)
}

// dropStaleFetches removes the download directories a run that ended mid-way left in cache.
func dropStaleFetches(cache string) {
	dirs, _ := filepath.Glob(filepath.Join(cache, ".fetch-*"))
	for _, dir := range dirs {
		newest := time.Time{}
		for _, p := range []string{dir, filepath.Join(dir, "download")} {
			if fi, err := os.Stat(p); err == nil && fi.ModTime().After(newest) {
				newest = fi.ModTime()
			}
		}
		if !newest.IsZero() && time.Since(newest) > staleFetchAfter {
			_ = os.RemoveAll(dir)
		}
	}
}
