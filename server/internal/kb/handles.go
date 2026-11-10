package kb

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"slices"
	"strings"

	"github.com/nwverse/persona/server/internal/kbwire"
)

// Identity hints (docs/extensions/connectors.md "Sender identity hints"): who a sender is in a form two sources can share — an email address,
// a phone number, a GitHub login — so the same person can later be recognised across them. Only an HMAC of each,
// under the hub's own key (Engine.HandleKey), is kept: in the person's handles.json, never in meta.json, so no tool
// or prompt shows them (they mean nothing to a model). Merging people is not done here.

// Defined in the contract package kbwire (docs/server/architecture.md "Contract packages"); the old names stay.
type Handle = kbwire.Handle

const (
	HandleEmail    = kbwire.HandleEmail
	HandleGitHub   = kbwire.HandleGitHub
	HandleLark     = kbwire.HandleLark
	HandlePhone    = kbwire.HandlePhone
	HandleTelegram = kbwire.HandleTelegram
)

// normalizeHandle is the form two sources agree on: an address in lower case, a number as + and digits, the rest trimmed
// and lower case. "" when nothing is left.
func normalizeHandle(h Handle) string {
	v := strings.TrimSpace(h.Value)
	switch h.Kind {
	case HandlePhone:
		var b strings.Builder
		for i, r := range v {
			if r >= '0' && r <= '9' || r == '+' && i == 0 {
				b.WriteRune(r)
			}
		}
		if s := b.String(); strings.Trim(s, "+") != "" {
			return s
		}
		return ""
	default:
		return strings.ToLower(v)
	}
}

// HashHandle is h as kept: "<kind>:<HMAC-SHA256 under key, 32 hex digits>", "" when h is empty or there is no key.
func HashHandle(key []byte, h Handle) string {
	v := normalizeHandle(h)
	if len(key) == 0 || v == "" || h.Kind == "" {
		return ""
	}
	m := hmac.New(sha256.New, key)
	m.Write([]byte(h.Kind + ":" + v))
	return h.Kind + ":" + hex.EncodeToString(m.Sum(nil))[:32]
}

// PersonHandles are the hashed hints kept for a person.
func (s *Store) PersonHandles(id string) []string {
	var out []string
	_, _ = readJSON(s.path(KindPerson, id, "handles.json"), &out)
	return out
}

// addHandles keeps a person's new hints, hashed (only ever added); it reports whether any was new. Callers hold
// personMu (notePerson's updatePerson), so two senders' hints for one person do not overwrite each other.
func (e *Engine) addHandles(personID string, hs []Handle) bool {
	if len(e.HandleKey) == 0 || len(hs) == 0 {
		return false
	}
	kept := e.Store.PersonHandles(personID)
	n := len(kept)
	for _, h := range hs {
		if x := HashHandle(e.HandleKey, h); x != "" && !slices.Contains(kept, x) {
			kept = append(kept, x)
		}
	}
	if len(kept) == n {
		return false
	}
	return writeJSON(e.Store.path(KindPerson, personID, "handles.json"), kept) == nil
}
