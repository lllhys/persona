package link

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"time"
	"unicode"

	"github.com/nwverse/persona/server/internal/i18n"
)

// Renaming this computer from here (docs/operations/link.md "The computer's name"): `persona-link name <new>`, the
// Windows tray and the Mac menu bar ask the running persona-link (RequestRename), which asks the hub over its link
// (rename) and keeps the hub's answer in its status for them (Status.Rename). The hub writes the name to hub.json and
// tells every client and this computer's Persona Link (endpoints); the id never changes. The request is a file beside
// link.json, as the status is: the running persona-link takes it within renamePoll, and drops one older than
// renameFresh (asked while none was running, or long ago).

// The codes a rename is refused with (RenameError.Code, Status.Rename.Code; renamed's Code from the hub).
const (
	RenameInvalidName = "invalid_name" // too long, or not one line
	RenamePending     = "pending"      // the hub waits for the user to confirm this computer first
	RenameNotRunning  = "not_running"  // no persona-link runs here to ask the hub
	RenamePassive     = "passive"      // passive mode: nothing here talks to the hub; rename it on the hub
	RenameOffline     = "offline"      // not connected to the hub now
	RenameUnsupported = "unsupported"  // the hub does not rename computers (an older one)
	RenameTimeout     = "timeout"      // no answer in time
)

// MaxMachineName is how long a computer's name may be, in characters (the hub's limit, hub checkMachineName).
const MaxMachineName = 40

const (
	renameFresh = 30 * time.Second
	renamePoll  = 500 * time.Millisecond
)

// RenameResult is how the last rename asked of this persona-link went: Name, what the hub calls the computer now; or
// Code and Error, why not.
type RenameResult struct {
	ID    string `json:"id"`
	Name  string `json:"name,omitempty"`
	Code  string `json:"code,omitempty"`
	Error string `json:"error,omitempty"`
}

// RenameError is a rename that did not happen: Code (Rename*) picks the words, Detail is the hub's own when it gave
// some for a code there are no words for.
type RenameError struct {
	Code   string
	Detail string
}

func (e *RenameError) Error() string { return e.Text(i18n.Default) }

// Text is the error in l.
func (e *RenameError) Text(l i18n.Lang) string {
	if key := "link-rename." + e.Code; e.Code != "" && i18n.Has(key) {
		return i18n.T(l, key)
	}
	if e.Detail != "" {
		return i18n.T(l, "link-rename.refused", i18n.Args{"detail": e.Detail})
	}
	return i18n.T(l, "link-rename.failed")
}

// CheckMachineName is name as the hub takes it (trimmed; "" asks for the computer's id again), or a *RenameError when
// it would refuse it.
func CheckMachineName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if len([]rune(name)) > MaxMachineName || strings.ContainsFunc(name, unicode.IsControl) {
		return "", &RenameError{Code: RenameInvalidName}
	}
	return name, nil
}

// RenamePath is where a rename asked of the persona-link of the link.json at path waits for it: beside link.json.
func RenamePath(path string) string {
	return strings.TrimSuffix(path, ".json") + ".rename.json"
}

// renameRequest is a rename waiting for the running persona-link.
type renameRequest struct {
	ID   string    `json:"id"`
	Name string    `json:"name"`
	At   time.Time `json:"at"`
}

// takeRename takes the rename waiting for the running persona-link at path, when there is a fresh one.
func takeRename(path string, now time.Time) (renameRequest, bool) {
	file := RenamePath(path)
	b, err := os.ReadFile(file)
	if err != nil {
		return renameRequest{}, false
	}
	_ = os.Remove(file)
	var r renameRequest
	if json.Unmarshal(b, &r) != nil || r.ID == "" || now.Sub(r.At) > renameFresh || r.At.Sub(now) > renameFresh {
		return renameRequest{}, false
	}
	r.ID = clipText(r.ID, 64)
	return r, true
}

// RequestRename asks the persona-link running with the link.json at path to have the hub call this computer name (""
// its id again), and waits until ctx ends for the hub's answer: the name the hub has now, or a *RenameError.
func RequestRename(ctx context.Context, path, name string) (string, error) {
	name, err := CheckMachineName(name)
	if err != nil {
		return "", err
	}
	if !Running(path) {
		return "", &RenameError{Code: RenameNotRunning}
	}
	st, err := ReadStatus(path)
	switch {
	case err == nil && st.Mode == ModePassive:
		return "", &RenameError{Code: RenamePassive}
	case err != nil || st.State != "connected":
		return "", &RenameError{Code: RenameOffline}
	}
	id := make([]byte, 8)
	_, _ = rand.Read(id)
	req := renameRequest{ID: hex.EncodeToString(id), Name: name, At: time.Now().UTC()}
	b, _ := json.Marshal(req)
	file := RenamePath(path)
	if err := os.WriteFile(file+".tmp", b, 0o600); err != nil {
		return "", err
	}
	if err := os.Rename(file+".tmp", file); err != nil {
		return "", err
	}
	t := time.NewTicker(200 * time.Millisecond)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			// Not taken in time: nobody takes it later.
			if b, err := os.ReadFile(file); err == nil && strings.Contains(string(b), req.ID) {
				_ = os.Remove(file)
			}
			return "", &RenameError{Code: RenameTimeout}
		case <-t.C:
		}
		st, err := ReadStatus(path) // may be being replaced right now: read again on the next tick
		if err != nil || st.Rename == nil || st.Rename.ID != req.ID {
			continue
		}
		if r := st.Rename; r.Code != "" || r.Error != "" || r.Name == "" {
			return "", &RenameError{Code: r.Code, Detail: r.Error}
		}
		return st.Rename.Name, nil
	}
}

// renames hands the hub the renames asked of this persona-link while the link is up (the hub answers renamed); a hub
// that never said what it calls this computer (named false) does not rename computers.
func (l *link) renames(ctx context.Context, o Options, rep *reporter) {
	t := time.NewTicker(renamePoll)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		req, ok := takeRename(o.Path, time.Now())
		if !ok {
			continue
		}
		if !l.named.Load() {
			rep.set(func(s *Status) { s.Rename = &RenameResult{ID: req.ID, Code: RenameUnsupported} })
			continue
		}
		o.Logf("link: asking the hub to rename this computer")
		if err := l.send(ctx, Message{Type: "rename", ID: req.ID, MachineName: req.Name}); err != nil {
			rep.set(func(s *Status) { s.Rename = &RenameResult{ID: req.ID, Code: RenameOffline, Error: err.Error()} })
		}
	}
}

// renamed takes in the hub's answer to a rename: the new name is kept in link.json, as endpoints' is.
func (l *link) renamed(m Message, c *Config, o Options, rep *reporter) {
	res := RenameResult{ID: clipText(m.ID, 64), Code: clipText(m.Code, 64), Error: clipText(m.Error, 300)}
	if res.Code == "" && res.Error == "" {
		res.Name = clipName(m.MachineName)
		l.keepMachineName(res.Name, c, o)
	}
	o.Logf("link: rename: %s", orText(res.Code, "done"))
	rep.set(func(s *Status) {
		s.MachineName = c.MachineName
		s.Rename = &res
	})
}

// keepMachineName keeps what the hub calls this computer in link.json (and c), when it changed.
func (l *link) keepMachineName(name string, c *Config, o Options) {
	if name == "" || name == c.MachineName {
		return
	}
	err := Update(o.Path, func(f *Config) error {
		if f.Token != c.Token {
			return errPairedAgain
		}
		f.MachineName = name
		return nil
	})
	switch {
	case err == nil:
		c.MachineName = name
	case errors.Is(err, errPairedAgain):
		l.pairedAgain()
	}
}

func orText(s, fallback string) string {
	if s == "" {
		return fallback
	}
	return s
}
