package node

import (
	"encoding/json"
	"slices"
	"testing"
)

func (f *fakeRuntime) drivenSessions() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.driven)
}

// The hub starts a session through pi's companion, which starts it in pi-web: the runtime inside pi-web, already
// running it, is told the hub drove it (runtime.driven), so its questions go to the phone first (#8097). The starter
// is not told; a later list does not tell it again.
func TestRuntimeDrivenToldToTheRuntimeAlreadyRunningIt(t *testing.T) {
	n, _ := runtimeNode(t)
	const id = "01a1b2c3-0000-7000-8000-000000000001"
	web := dialRuntime(t, n, map[string]any{"runtime": "pi-web", "agent": "pi", "caps": map[string]any{"send": true}})
	files := dialRuntime(t, n, map[string]any{"runtime": "pi-files", "agent": "pi", "launcher": "pi-web", "caps": map[string]any{"start": true, "send": true}})
	files.answer = func(method string, params json.RawMessage) (any, error) {
		if method == "session.start" {
			_ = web.peer.Notify("runtime.sessions", map[string]any{"sessions": []map[string]any{{"session_id": id, "status": "working"}}})
			waitFor(t, "listed by pi-web", func() bool { return n.runtimes.ownerOf(id) != nil })
			return map[string]any{"session": map[string]any{"session_id": id, "cwd": "/w", "status": "offline"}}, nil
		}
		return map[string]any{"ok": true}, nil
	}
	if _, err := nodeCall(t, n, "session.start", map[string]any{"cwd": "/w", "launcher": "pi-web", "async": true}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "pi-web told", func() bool { return len(web.drivenSessions()) == 1 })
	if got := web.drivenSessions(); got[0] != id {
		t.Fatalf("told %v", got)
	}
	_ = web.peer.Notify("runtime.sessions", map[string]any{"sessions": []map[string]any{{"session_id": id, "status": "idle"}}})
	_ = web.peer.Notify("runtime.sessions", map[string]any{"sessions": []map[string]any{{"session_id": id, "status": "working"}}})
	if _, err := nodeCall(t, n, "session.send", map[string]any{"session_id": id, "text": "x"}); err != nil { // after the lists above
		t.Fatal(err)
	}
	if got := web.drivenSessions(); len(got) != 1 {
		t.Fatalf("told again: %v", got)
	}
	if got := files.drivenSessions(); len(got) != 0 {
		t.Fatalf("the starter was told: %v", got)
	}
}

// When pi-web lists the session only after the start answered, its list is when it is told.
func TestRuntimeDrivenToldWhenTheRunningRuntimeListsItLater(t *testing.T) {
	n, _ := runtimeNode(t)
	const id = "01a1b2c3-0000-7000-8000-000000000002"
	web := dialRuntime(t, n, map[string]any{"runtime": "pi-web", "agent": "pi", "caps": map[string]any{"send": true}})
	files := dialRuntime(t, n, map[string]any{"runtime": "pi-files", "agent": "pi", "launcher": "pi-web", "caps": map[string]any{"start": true, "send": true}})
	files.answer = func(method string, params json.RawMessage) (any, error) {
		if method == "session.start" {
			return map[string]any{"session": map[string]any{"session_id": id, "cwd": "/w", "status": "offline"}}, nil
		}
		return map[string]any{"ok": true}, nil
	}
	if _, err := nodeCall(t, n, "session.start", map[string]any{"cwd": "/w", "launcher": "pi-web", "async": true}); err != nil {
		t.Fatal(err)
	}
	if got := web.drivenSessions(); len(got) != 0 {
		t.Fatalf("told before it ran it: %v", got)
	}
	_ = web.peer.Notify("runtime.sessions", map[string]any{"sessions": []map[string]any{{"session_id": id, "status": "working"}}})
	waitFor(t, "pi-web told", func() bool { return slices.Equal(web.drivenSessions(), []string{id}) })
}

// A runtime's send that only queued behind the running turn, and an interrupt it saw through to the end, say so
// through the node: the manager tells "queued" and "stopped" apart from plain success (#8097).
func TestRuntimeSendQueuedAndInterruptSettledPassThrough(t *testing.T) {
	n, _ := runtimeNode(t)
	const id = "01a1b2c3-0000-7000-8000-000000000003"
	web := dialRuntime(t, n, map[string]any{"runtime": "pi-web", "agent": "pi", "caps": map[string]any{"send": true, "interrupt": true}})
	queued, settled := false, false
	web.answer = func(method string, params json.RawMessage) (any, error) {
		switch method {
		case "session.send":
			return map[string]any{"ok": true, "queued": queued}, nil
		case "session.interrupt":
			return map[string]any{"ok": true, "settled": settled}, nil
		}
		return map[string]any{"ok": true}, nil
	}
	_ = web.peer.Notify("runtime.sessions", map[string]any{"sessions": []map[string]any{{"session_id": id, "status": "working"}}})
	waitFor(t, "listed", func() bool { return n.runtimes.ownerOf(id) != nil })
	result := func(method string, params map[string]any) map[string]any {
		t.Helper()
		raw, err := nodeCall(t, n, method, params)
		if err != nil {
			t.Fatal(err)
		}
		var m map[string]any
		_ = json.Unmarshal(raw, &m)
		return m
	}
	if m := result("session.send", map[string]any{"session_id": id, "text": "a", "client_msg_id": "q-1"}); m["queued"] != nil {
		t.Fatalf("delivered send: %v", m)
	}
	queued = true
	if m := result("session.send", map[string]any{"session_id": id, "text": "b", "client_msg_id": "q-2"}); m["queued"] != true || m["ok"] != true {
		t.Fatalf("queued send: %v", m)
	}
	if m := result("session.interrupt", map[string]any{"session_id": id}); m["settled"] != nil || m["ok"] != true {
		t.Fatalf("unconfirmed stop: %v", m)
	}
	settled = true
	if m := result("session.interrupt", map[string]any{"session_id": id}); m["settled"] != true {
		t.Fatalf("settled stop: %v", m)
	}
}
