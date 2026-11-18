package node

import (
	"encoding/json"
	"time"
)

// A runtime that runs Claude Code's command line itself (Paseo, docs/integrations/paseo.md) writes transcripts that
// say "sdk-cli", as a script running `claude -p` does, and the offline list leaves those out (claude_oneshot.go). While
// the runtime lists a session its row is the session. Once it no longer lists it (archived in Paseo: closing a session
// from Persona archives it) only the transcript is left, and nothing in it says whose it was (#4521). So the runtime
// says: beside its rows, a runtime.sessions push names the Claude transcripts on this machine that are sessions of
// its own though it does not list them (unlisted_transcripts). The offline list keeps those as any session not
// running, under its usual window and count. A push without the field says nothing about them (a runtime that was
// just started and has not read its archive yet): what the runtime named before stands. Not for a daemon: its
// transcripts are not on this machine.

// unlistedMax bounds what one runtime may name: more than a week of anyone's archived sessions.
const unlistedMax = 2000

// unlistedKeep is how long what a runtime named stands once it is gone, and the rows it listed whose conversation is a
// transcript here (runtimeaway.go): as long as the list of sessions not running goes back. A runtime that restarts (its
// bridge, the program it bridges) is back in seconds and says them again; one that stays away (Paseo not running, the
// computer switched to another engine) leaves its Claude conversations readable meanwhile, refusing only what needs
// it. Ten minutes once (#4521): the conversations left the list while Paseo was down, and came back with it (#8131).
const unlistedKeep = offlineWindow

// unlistedOf is the transcripts a runtime named, and when its last connection left (zero while it is connected).
type unlistedOf struct {
	set  map[string]bool
	gone time.Time
}

// unlistedSet is the transcripts a push names, as a set: only ids Claude itself uses, the last unlistedMax of them (a
// runtime that names more than fit says which matter most last). nil when it names none.
func unlistedSet(ids []string) map[string]bool {
	var out map[string]bool
	for i := len(ids) - 1; i >= 0 && len(out) < unlistedMax; i-- {
		if !validID(ids[i]) {
			continue
		}
		if out == nil {
			out = make(map[string]bool, min(len(ids), unlistedMax))
		}
		out[ids[i]] = true
	}
	return out
}

// unlistedTranscripts reads the unlisted_transcripts of a runtime.sessions push; said is false when the push does not
// have the field, or has something else there than a list of ids.
func unlistedTranscripts(params json.RawMessage) (set map[string]bool, said bool) {
	var p struct {
		Unlisted []string `json:"unlisted_transcripts"`
	}
	if json.Unmarshal(params, &p) != nil || p.Unlisted == nil {
		return nil, false
	}
	return unlistedSet(p.Unlisted), true
}

// setUnlisted replaces the transcripts h's runtime names as its own though it does not list them, and says whether
// they changed.
func (b *runtimeRegistry) setUnlisted(h *runtimeConn, m map[string]bool) (changed bool) {
	id := h.id()
	if id == "" {
		return false
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	old := b.unlisted[id].set
	if len(m) != len(old) {
		changed = true
	} else {
		for t := range m {
			if !old[t] {
				changed = true
				break
			}
		}
	}
	if b.unlisted == nil {
		b.unlisted = map[string]unlistedOf{}
	}
	b.unlisted[id] = unlistedOf{set: m}
	return changed
}

// leftUnlisted notes that runtime id has no connection any more: what it named stands for unlistedKeep. Callers hold
// b.mu.
func (b *runtimeRegistry) leftUnlisted(id string) {
	u, ok := b.unlisted[id]
	if !ok || id == "" {
		return
	}
	for _, h := range b.conns {
		if h.id() == id {
			return // another connection of the same runtime (it reconnected before the old one was gone)
		}
	}
	if len(u.set) == 0 {
		delete(b.unlisted, id)
		return
	}
	u.gone = time.Now()
	b.unlisted[id] = u
}

// namesUnlisted: runtime rt names transcript id as a session of its own that it does not list (unlistedTranscripts),
// connected or for unlistedKeep once it left. Callers hold b.mu.
func (b *runtimeRegistry) namesUnlisted(rt, id string) bool {
	u, ok := b.unlisted[rt]
	return ok && u.set[id] && (u.gone.IsZero() || time.Since(u.gone) <= unlistedKeep)
}

// unlistedTranscripts is every transcript a runtime names as a session of its own that it does not list: the
// connected ones, and for unlistedKeep those that left; nil when none does.
func (b *runtimeRegistry) unlistedTranscripts() map[string]bool {
	if b == nil {
		return nil
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	var out map[string]bool
	for id, u := range b.unlisted {
		if !u.gone.IsZero() && time.Since(u.gone) > unlistedKeep {
			delete(b.unlisted, id)
			continue
		}
		for t := range u.set {
			if out == nil {
				out = make(map[string]bool, len(u.set))
			}
			out[t] = true
		}
	}
	return out
}
