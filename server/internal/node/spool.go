package node

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/nwverse/persona/server/internal/identity"
)

// While a resident node has no manager connection (the manager restarting, crashed, or not installed yet), it answers
// node.sock itself in standby: what hooks report and what `personad notify` sends goes into a bounded spool persisted
// under the data dir, and the next manager connection's Node replays it, in order, before it takes new hooks. Hooks
// that wait on the phone, and `personad ask`, are told to retry while the manager has been away less than standbyHold,
// so a manager restart does not drop their card to the terminal; past that they go to the terminal at once, as without
// a manager. Everything else on the socket (MCP, launchers, CLI calls) is answered "away", which clients take for "no
// node", exactly as before (docs/protocol/protocol.md "When the manager is away").

// spoolFile is the spool's name in the data dir: one JSON entry per line, oldest first.
const spoolFile = "node-spool.jsonl"

// Bounds of the spool: the oldest entries go first when either is reached. An entry older than spoolMaxAge is dropped
// at replay: a day-old "done" is no news.
var (
	spoolMost      = 500
	spoolBytesMost = 1 << 20
	spoolMaxAge    = 24 * time.Hour
)

// spoolTriesMost: a notification refused (or not answered) this often is dropped: it would hold the spool forever.
// Only real refusals count, not "not now" (nodeproto.ErrSpoolBusy, ErrHubAway); with the retries on the same
// connection (replayRetryAfter) this is about 7 minutes of them.
const spoolTriesMost = 5

// SpoolPath is where a resident node keeps what it took while its manager was away.
func SpoolPath() string { return filepath.Join(identity.HomeDir(), spoolFile) }

// spoolEntry is one thing taken in standby. ID goes with it to the manager (spool_id), which drops a repeat.
type spoolEntry struct {
	ID      string      `json:"id"`
	At      time.Time   `json:"at"`
	Kind    string      `json:"kind"` // "hook" or "notify"
	Request sockRequest `json:"request"`
	Tries   int         `json:"tries,omitempty"` // replays that failed
}

// spool is the file of entries. Only the resident node's process uses it (Attach): its standby writes it and its
// connections' Nodes replay it, one at a time (replayMu).
type spool struct {
	path     string
	replayMu sync.Mutex
	logf     func(string, ...any)
	rewrites int // how often writeLocked replaced (or removed) the file, under spoolMu: tests count them
}

var spoolMu sync.Mutex // one spool file per process

func openSpool(path string) *spool { return &spool{path: path} }

func (s *spool) log(format string, args ...any) {
	if s.logf != nil {
		s.logf(format, args...)
	}
}

// load reads the entries; a line that does not parse is skipped.
func (s *spool) load() []spoolEntry {
	spoolMu.Lock()
	defer spoolMu.Unlock()
	entries, _, err := s.loadLocked()
	if err != nil {
		s.log("load %s: %v", s.path, err)
	}
	return entries
}

// loadLocked reads the entries and, for each, its line in the file: a rewrite keeps those bytes instead of encoding
// every entry again.
func (s *spool) loadLocked() (entries []spoolEntry, lines [][]byte, err error) {
	b, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil, nil
		}
		return nil, nil, err
	}
	sc := bufio.NewScanner(bytes.NewReader(b))
	sc.Buffer(make([]byte, 0, 64<<10), spoolBytesMost+1)
	badLines := 0
	for sc.Scan() {
		raw := sc.Bytes()
		if len(bytes.TrimSpace(raw)) == 0 {
			continue
		}
		var e spoolEntry
		if err := json.Unmarshal(raw, &e); err == nil && e.ID != "" {
			entries = append(entries, e)
			lines = append(lines, bytes.Clone(raw)) // the scanner reuses its buffer
		} else {
			badLines++
		}
	}
	if err := sc.Err(); err != nil {
		s.log("scanner error reading %s: %v", s.path, err)
		return entries, lines, err
	}
	if badLines > 0 {
		s.log("skipped %d bad line(s) in %s", badLines, s.path)
	}
	return entries, lines, nil
}

// add appends e, dropping the oldest entries past the bounds. dropped is how many went.
func (s *spool) add(e spoolEntry) (dropped int, err error) {
	spoolMu.Lock()
	defer spoolMu.Unlock()
	line, err := json.Marshal(e)
	if err != nil {
		return 0, err
	}
	if len(line) >= spoolBytesMost {
		return 0, errors.New("too large to keep")
	}
	if ok, err := s.appendLocked(line); ok || err != nil {
		return 0, err
	}
	_, lines, err := s.loadLocked()
	if err != nil {
		return 0, err
	}
	lines = append(lines, line)
	size := 0
	for _, l := range lines {
		size += len(l) + 1
	}
	for len(lines) > spoolMost || size > spoolBytesMost {
		size -= len(lines[0]) + 1
		lines = lines[1:]
		dropped++
	}
	return dropped, s.writeLocked(lines)
}

// appendLocked adds line at the end of the file when that stays within the bounds (counting every line, bad ones too)
// and the file ends with a whole line. ok false: add rewrites the file instead (there is none yet, a bound is reached,
// or a torn last line from a crash is to go).
func (s *spool) appendLocked(line []byte) (ok bool, err error) {
	f, err := os.OpenFile(s.path, os.O_RDWR|os.O_APPEND, 0)
	if os.IsNotExist(err) {
		return false, nil
	} else if err != nil {
		return false, err
	}
	defer f.Close()
	count, size, last := 0, 0, byte('\n')
	buf := make([]byte, 32<<10)
	for {
		k, err := f.Read(buf)
		if k > 0 {
			count += bytes.Count(buf[:k], []byte{'\n'})
			size, last = size+k, buf[k-1]
		}
		if err == io.EOF {
			break
		} else if err != nil {
			return false, err
		}
	}
	if last != '\n' || count+1 > spoolMost || size+len(line)+1 > spoolBytesMost {
		return false, nil
	}
	if _, err = f.Write(append(line, '\n')); err == nil {
		err = f.Sync()
	}
	if err != nil {
		_ = f.Truncate(int64(size)) // not kept: leave no part of it
		return false, err
	}
	return true, nil
}

// failed counts a failed replay of id and says whether it is to be dropped now.
func (s *spool) failed(id string) (drop bool, err error) {
	spoolMu.Lock()
	defer spoolMu.Unlock()
	entries, lines, err := s.loadLocked()
	if err != nil {
		return false, err
	}
	kept := lines[:0]
	for i, x := range entries {
		if x.ID != id {
			kept = append(kept, lines[i])
			continue
		}
		if x.Tries++; x.Tries >= spoolTriesMost {
			drop = true
			continue
		}
		b, err := json.Marshal(x)
		if err != nil {
			return false, err
		}
		kept = append(kept, b)
	}
	return drop, s.writeLocked(kept)
}

// remove takes the entries ids out (delivered, or dropped), in one rewrite; none of them there writes nothing.
func (s *spool) remove(ids ...string) error {
	spoolMu.Lock()
	defer spoolMu.Unlock()
	gone := make(map[string]bool, len(ids))
	for _, id := range ids {
		gone[id] = true
	}
	entries, lines, err := s.loadLocked()
	if err != nil {
		return err
	}
	kept := lines[:0]
	for i, x := range entries {
		if !gone[x.ID] {
			kept = append(kept, lines[i])
		}
	}
	if len(kept) == len(entries) {
		return nil
	}
	return s.writeLocked(kept)
}

// spoolRemoveEvery bounds how far the file lags behind a replay: what it handed over leaves the file in one rewrite,
// once this many are due and when the pass ends. A node stopped in between sends at most these again, and the manager
// drops the repeats by spool_id.
const spoolRemoveEvery = 64

// spoolRemovals collects the entries a replay pass is done with (sp.remove once spoolRemoveEvery are due, and at flush).
type spoolRemovals struct {
	sp   *spool
	logf func(string, ...any)
	ids  []string
}

func (r *spoolRemovals) add(id string) {
	if r.ids = append(r.ids, id); len(r.ids) >= spoolRemoveEvery {
		r.flush()
	}
}

func (r *spoolRemovals) flush() {
	if len(r.ids) == 0 {
		return
	}
	if err := r.sp.remove(r.ids...); err != nil {
		r.logf("spool: %v", err)
	}
	r.ids = r.ids[:0]
}

// writeLocked replaces the file with lines (atomically: a crash leaves the old file or the new one); none removes it.
func (s *spool) writeLocked(lines [][]byte) error {
	s.rewrites++
	if len(lines) == 0 {
		if err := os.Remove(s.path); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	_ = os.Remove(tmp) // a leftover keeps its mode through O_TRUNC
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	w := bufio.NewWriter(f)
	for _, l := range lines {
		_, _ = w.Write(l)
		_ = w.WriteByte('\n')
	}
	if err := w.Flush(); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}
