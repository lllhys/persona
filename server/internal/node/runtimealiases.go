package node

import (
	"bufio"
	"encoding/json"
	"errors"
	"io"
	"os"
	"sync"
)

// A runtime's session listed under an id of its own whose conversation is a Claude transcript here (Paseo's: one
// started from Persona, paseo-<agent id>, or one whose id the bridge kept when Paseo forked it, docs/integrations/paseo.md
// "Session IDs") went by other ids on this machine: the transcript's, and the sessions Paseo forked it from. Before
// #6709 such a conversation was listed under its transcript, and Persona's archive (the user's, or closed sessions
// archiving themselves) kept a record under each id it was listed under: the Archived list repeated the conversation
// listed under its own id now (#8131). The row names those ids (aliases), the manager shows no archive record under
// one (agent_archives.go), and the node lists none of them beside it.

// forkLineMost bounds how far back a transcript's line is followed; forkHeadMost how much of a transcript's first
// record is read for the session it was forked from (a first prompt may carry an image, and forkedFrom is its last key).
const (
	forkLineMost = 64
	forkHeadMost = 8 << 20
)

// forkLineCache caches each transcript's line (forkLine), by its path: the first records it is read from never change.
var forkLineCache = struct {
	sync.Mutex
	of map[string][]string
}{of: map[string][]string{}}

// aliasesOf is what the conversation of a runtime's row under id, transcript t here, is known by besides id: t, then
// the sessions t was forked from, newest first, back to id or to a session another row lists (listed), where its line
// is another conversation's.
func (n *Node) aliasesOf(id, t string, listed func(string) bool) []string {
	out := []string{t}
	for _, s := range n.forkLine(t) {
		if s == id || listed(s) {
			break
		}
		out = append(out, s)
	}
	return out
}

// forkLine is the sessions Claude transcript t was forked from, newest first: the session its first record names
// (forkedFrom, written on every record a fork copied), then that one's, while each is a transcript here; a session
// whose transcript is gone ends the line, its id known from its fork's records. Nil when t is no fork, or its first
// record is not whole yet (asked again next time).
func (n *Node) forkLine(t string) []string {
	p := n.ownTranscriptPath(t)
	if p == "" {
		return nil
	}
	forkLineCache.Lock()
	line, ok := forkLineCache.of[p]
	forkLineCache.Unlock()
	if ok {
		return line
	}
	seen := map[string]bool{t: true}
	for at := p; at != "" && len(line) < forkLineMost; {
		parent, whole := forkedFrom(at)
		if !whole {
			return line // being written: not kept
		}
		if parent == "" || seen[parent] {
			break
		}
		seen[parent] = true
		line = append(line, parent)
		at = n.ownTranscriptPath(parent)
	}
	forkLineCache.Lock()
	if len(forkLineCache.of) > 4000 {
		clear(forkLineCache.of)
	}
	forkLineCache.of[p] = line
	forkLineCache.Unlock()
	return line
}

// forkedFrom is the session the transcript at path was forked from (its first record's forkedFrom.sessionId), "" for
// none; whole is false while that record is still being written (no end of line yet, within forkHeadMost).
func forkedFrom(path string) (parent string, whole bool) {
	f, err := os.Open(path)
	if err != nil {
		return "", true // nothing to read: the line ends here
	}
	defer f.Close()
	line, err := bufio.NewReaderSize(io.LimitReader(f, forkHeadMost), 64<<10).ReadBytes('\n')
	if errors.Is(err, io.EOF) {
		return "", len(line) >= forkHeadMost // past the bound: given up on, not waited for
	}
	if err != nil {
		return "", true
	}
	var r struct {
		ForkedFrom struct {
			SessionID string `json:"sessionId"`
		} `json:"forkedFrom"`
	}
	if json.Unmarshal(line, &r) != nil || !validID(r.ForkedFrom.SessionID) {
		return "", true
	}
	return r.ForkedFrom.SessionID, true
}
