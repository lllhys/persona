package node

// omp's stopped sessions on disk (docs/integrations/omp.md): the node lists the recent ones as offline rows. pi has a
// companion process for this; omp does not need one — the node reads the head of what omp wrote:
// <agent dir>/sessions/<encoded cwd>/<time>_<uuid>.jsonl, a title line and a session line with the id and cwd.
//
// Nothing of the conversation leaves this machine: a row carries the title, the cwd and the time, no words.

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/nwverse/persona/server/internal/proto"
)

const (
	ompDiskEvery = 15 * time.Second // a list this soon after the last scan reuses it
	ompDiskDays  = 7                // how old a session file may be
	ompDiskMax   = 80               // the most rows listed
	ompDiskHead  = 8 << 10          // the head read for the title and the session line (over-long lines skipped)
)

// ompDiskTimeout is one scan's reading. A slower disk keeps the last list and what this scan read, so the next scan
// goes on from there (a var for tests).
var ompDiskTimeout = 3 * time.Second

// ompDiskSession is one stopped session as its file tells it.
type ompDiskSession struct {
	ID       string    // the session id (session line, a UUID)
	Path     string    // the session file
	Cwd      string    // the session's working directory
	Title    string    // the title (the session line's, else the title line's)
	Modified time.Time // the file's mtime: when it was last used
}

// ompDiskEntry is what a file's head and tail said, kept until its mtime or size changes.
type ompDiskEntry struct {
	mtime time.Time
	size  int64
	s     ompDiskSession
}

// ompDiskState is the scan's cache: entries by path, the sessions by id, and when it last ran.
type ompDiskState struct {
	mu      sync.Mutex
	dir     string
	files   map[string]ompDiskEntry
	byID    map[string]ompDiskSession
	order   []ompDiskSession
	at      time.Time
	reads   int // files read in all, a test's cache-hit count
	scanned time.Time
}

// ompAgentDir is omp's agent directory, the one omp and its Persona extension use (integrations/omp/lib/compat.js):
// PI_CODING_AGENT_DIR when it is set, whatever its name, else <home>/.omp/agent.
func ompAgentDir(home string) string {
	if d := strings.TrimSpace(os.Getenv("PI_CODING_AGENT_DIR")); d != "" {
		return d
	}
	return filepath.Join(home, ".omp", "agent")
}

// omps is this node's scan state, made on first use (tests build Nodes by hand).
func (n *Node) omps() *ompDiskState {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.ompDisk == nil {
		n.ompDisk = &ompDiskState{}
	}
	return n.ompDisk
}

// ompSessions returns the recent stopped sessions, newest first: cached for ompDiskEvery, and a scan that runs out of
// time keeps the last result.
func (n *Node) ompSessions() []ompDiskSession {
	d := n.omps()
	dir := filepath.Join(ompAgentDir(n.home), "sessions")
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.dir == dir && time.Since(d.at) < ompDiskEvery {
		return d.order
	}
	if d.dir != dir {
		d.files, d.order = nil, nil
	}
	if d.files == nil {
		d.files = map[string]ompDiskEntry{}
	}
	d.dir = dir
	d.scanLocked()
	d.at = time.Now()
	return d.order
}

// ompDiskSession is the recent session id on disk, live or not.
func (n *Node) ompDiskSession(id string) (ompDiskSession, bool) {
	if id == "" {
		return ompDiskSession{}, false
	}
	for _, s := range n.ompSessions() {
		if s.ID == id {
			return s, true
		}
	}
	return ompDiskSession{}, false
}

// ompSession is the stopped session id, false when it is not one of the recent ones (a live session has a row of its
// own and is not listed from disk), or when a live family pane may be writing its file.
func (n *Node) ompSession(id string) (ompDiskSession, bool) {
	if _, live := n.agent(id); id == "" || live {
		return ompDiskSession{}, false
	}
	s, ok := n.ompDiskSession(id)
	if !ok || n.ompDiskWrittenByLivePane(s) {
		return ompDiskSession{}, false
	}
	return s, true
}

// ompDiskWrittenByLivePane: an omp pane the node knows only by its fallback id runs in the session's directory, so its
// file may be the one that omp is still writing (no extension, no herdr integration: nothing says which session).
// The session the pane last reported (before its id went missing again) is hidden; so is a file written since the
// pane was first seen; a pane from before this node started hides the directory's rows, and getting them back when
// the pane goes beats resuming into a second omp. Directories are compared as physical paths, so a pane opened
// through a symlink still matches the file's directory.
func (n *Node) ompDiskWrittenByLivePane(s ompDiskSession) bool {
	if s.Cwd == "" {
		return false
	}
	panes := n.ompFallbackPanes()
	if len(panes) == 0 {
		return false
	}
	cwd := realPath(filepath.Clean(s.Cwd))
	for _, p := range panes {
		if p.cwd == "" || realPath(filepath.Clean(p.cwd)) != cwd {
			continue
		}
		if s.ID == p.session || p.seen.IsZero() || !s.Modified.Before(p.seen.Add(-2*time.Second)) {
			return true
		}
	}
	return false
}

// ompOfflineRows are the stopped sessions no live row covers, as the list wants them.
func (n *Node) ompOfflineRows(seen map[string]bool) []proto.Session {
	var out []proto.Session
	for _, d := range n.ompSessions() {
		if seen[d.ID] || d.ID == "" || n.ompDiskWrittenByLivePane(d) {
			continue // a live row covers it, or a live pane may be writing it
		}
		s := proto.Session{
			SessionID: d.ID, Agent: ompAgent, Launcher: ompAgent, Status: proto.StatusOffline,
			Title: d.Title, Cwd: d.Cwd, UpdatedAt: d.Modified, Rev: d.Modified.UnixMilli(),
		}
		out = append(out, s)
	}
	return out
}

// scanLocked walks the session files (newest ompDiskMax within ompDiskDays) and parses the ones that changed.
func (d *ompDiskState) scanLocked() {
	files, err := filepath.Glob(filepath.Join(d.dir, "*", "*.jsonl"))
	if err != nil {
		return
	}
	type cand struct {
		path string
		st   os.FileInfo
	}
	var cands []cand
	now := time.Now()
	for _, p := range files {
		st, err := os.Stat(p)
		if err != nil || now.Sub(st.ModTime()) > ompDiskDays*24*time.Hour {
			continue
		}
		cands = append(cands, cand{p, st})
	}
	sort.Slice(cands, func(i, j int) bool { return cands[i].st.ModTime().After(cands[j].st.ModTime()) })
	if len(cands) > ompDiskMax {
		cands = cands[:ompDiskMax]
	}
	kept := make(map[string]ompDiskEntry, len(cands))
	seen := map[string]bool{}
	var order []ompDiskSession
	deadline := time.Now().Add(ompDiskTimeout)
	full, read := true, 0
	for _, c := range cands {
		var s ompDiskSession
		if e, ok := d.files[c.path]; ok && e.mtime.Equal(c.st.ModTime()) && e.size == c.st.Size() {
			s = e.s
		} else {
			if read > 0 && time.Now().After(deadline) {
				full = false // a slow disk: the rest next time, from what this scan read
				break
			}
			read++
			d.reads++
			s = readOmpSession(c.path)
			s.Modified = c.st.ModTime()
		}
		kept[c.path] = ompDiskEntry{mtime: c.st.ModTime(), size: c.st.Size(), s: s}
		if s.ID == "" || seen[s.ID] {
			continue // no session, or the same session in two directories: the newest file is the one above
		}
		seen[s.ID] = true
		order = append(order, s)
	}
	if !full {
		// The last list stands until a scan finishes; the heads read here are kept, so each scan reads only files it
		// has not seen and a slow disk still gets through them.
		if d.files == nil {
			d.files = map[string]ompDiskEntry{}
		}
		for p, e := range kept {
			d.files[p] = e
		}
		return
	}
	d.files = kept
	d.order = order
}

// readOmpSession reads the head for the session and its title.
func readOmpSession(path string) ompDiskSession {
	s := ompDiskSession{Path: path}
	f, err := os.Open(path)
	if err != nil {
		return s
	}
	defer f.Close()
	head := make([]byte, ompDiskHead)
	n, _ := io.ReadFull(f, head)
	head = head[:n]
	var titleLine string
	for _, line := range strings.Split(string(head), "\n") {
		var e struct {
			Type  string `json:"type"`
			Title string `json:"title"`
			ID    string `json:"id"`
			Cwd   string `json:"cwd"`
		}
		if json.Unmarshal([]byte(line), &e) != nil {
			continue
		}
		switch {
		case e.Type == "title" && titleLine == "":
			titleLine = e.Title
		case e.Type == "session" && e.ID != "" && s.ID == "":
			s.ID, s.Cwd, s.Title = e.ID, e.Cwd, e.Title
		}
	}
	if s.ID == "" {
		s.ID = strings.TrimSuffix(filepath.Base(path), ".jsonl")
		if i := strings.LastIndexByte(s.ID, '_'); i >= 0 {
			s.ID = s.ID[i+1:]
		}
	}
	if s.Title == "" {
		s.Title = titleLine
	}
	return s
}
