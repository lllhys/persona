package node

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/proto"
	"github.com/nwverse/persona/server/internal/rpc"
)

// An older session a runtime stopped listing (DSH lists its latest 80 of thousands) is still its own: a send goes to
// the runtime, not to herdr as a Claude session to resume, and what the runtime pushes for it reaches the hub.
func TestRuntimeOlderSessionStillGoesToItsRuntime(t *testing.T) {
	n, hub := runtimeNode(t)
	host := dialRuntime(t, n, dshHello())
	const old = "0bd545f1-681d-4d98-bf5a-eb7c1251a0f0" // DSH's older sessions have plain uuids, as Claude's do
	_ = host.peer.Notify("runtime.sessions", map[string]any{"sessions": []map[string]any{{"session_id": old, "status": "done"}}})
	waitFor(t, "listed", func() bool { _, ok := listed(t, n)[old]; return ok })
	_ = host.peer.Notify("runtime.sessions", map[string]any{"sessions": []map[string]any{{"session_id": "session-new", "status": "working"}}})
	waitFor(t, "pushed out of the list", func() bool { _, ok := listed(t, n)[old]; return !ok })

	if _, err := nodeCall(t, n, "session.send", map[string]any{"session_id": old, "text": "still there?", "client_msg_id": "m-1"}); err != nil {
		t.Fatalf("send to an older session of the runtime: %v", err)
	}
	if got := host.got("session.send"); len(got) != 1 || !strings.Contains(string(got[0]), "still there?") {
		t.Fatalf("the runtime was asked to send %d times", len(got))
	}
	if _, err := nodeCall(t, n, "session.watch", map[string]any{"session_id": old, "since_seq": 0}); err != nil {
		t.Fatal(err)
	}
	_ = host.peer.Notify("session.items", map[string]any{"session_id": old, "from_seq": 3, "to_seq": 4, "items": []map[string]any{{"id": "4#0", "seq": 4}}})
	waitFor(t, "its items reach the hub", func() bool {
		hub.mu.Lock()
		defer hub.mu.Unlock()
		return len(hub.items) == 1
	})
}

// A session this machine has nothing for (no agent, no transcript) that a runtime has without ever listing it (the
// phone kept it from before a node restart) is asked about once and then goes to that runtime; a session no runtime
// has is asked about once a minute at most and goes the old way.
func TestRuntimeAnswersForASessionItDoesNotList(t *testing.T) {
	n, _ := runtimeNode(t)
	host := dialRuntime(t, n, dshHello())
	const mine, other = "aaaaaaaa-1111-4222-8333-444455556666", "bbbbbbbb-1111-4222-8333-444455556666"
	host.mu.Lock()
	host.answer = func(method string, params json.RawMessage) (any, error) {
		var p struct {
			SessionID string `json:"session_id"`
		}
		_ = json.Unmarshal(params, &p)
		if method == "session.items" && p.SessionID != mine {
			return nil, &rpc.Error{Message: "DSH 里没有会话 " + p.SessionID, Code: "not_found"}
		}
		return map[string]any{"ok": true, "items": []any{}}, nil
	}
	host.mu.Unlock()

	if _, err := nodeCall(t, n, "session.send", map[string]any{"session_id": mine, "text": "hi", "client_msg_id": "m-2"}); err != nil {
		t.Fatalf("send to a session the runtime has: %v", err)
	}
	if len(host.got("session.send")) != 1 {
		t.Fatal("not sent through the runtime")
	}
	if _, err := nodeCall(t, n, "session.send", map[string]any{"session_id": mine, "text": "again", "client_msg_id": "m-3"}); err != nil {
		t.Fatal(err)
	}
	if len(host.got("session.items")) != 1 {
		t.Fatalf("asked %d times about a session it already claimed", len(host.got("session.items")))
	}

	_, err := nodeCall(t, n, "session.send", map[string]any{"session_id": other, "text": "hi", "client_msg_id": "m-4"})
	if err == nil {
		t.Fatal("a session nobody has was sent")
	}
	_, _ = nodeCall(t, n, "session.send", map[string]any{"session_id": other, "text": "hi", "client_msg_id": "m-5"})
	if len(host.got("session.items")) != 2 || len(host.got("session.send")) != 2 {
		t.Fatalf("items asked %d times, sends %d", len(host.got("session.items")), len(host.got("session.send")))
	}
}

// A runtime that refuses to read a session it does not list because reading it would start the agent (not_loaded:
// an agent Paseo let go, #8132) has that session all the same: the calls go to it, and the refusal reaches the hub as
// it is, nothing read.
func TestRuntimeRefusingToLoadASessionStillOwnsIt(t *testing.T) {
	n, _ := runtimeNode(t)
	host := dialRuntime(t, n, dshHello())
	const stored = "cccccccc-1111-4222-8333-444455556666"
	host.mu.Lock()
	host.answer = func(method string, params json.RawMessage) (any, error) {
		if method == "session.items" {
			return nil, &rpc.Error{Message: "reading it would start the agent", Code: proto.CodeNotLoaded}
		}
		return map[string]any{"ok": true}, nil
	}
	host.mu.Unlock()

	if _, err := nodeCall(t, n, "session.send", map[string]any{"session_id": stored, "text": "go on", "client_msg_id": "m-7"}); err != nil {
		t.Fatalf("send to a session its runtime would have to start: %v", err)
	}
	if len(host.got("session.send")) != 1 {
		t.Fatal("not sent through the runtime")
	}
	_, err := nodeCall(t, n, "session.items", map[string]any{"session_id": stored, "turns": 1})
	var re *rpc.Error
	if !errors.As(err, &re) || re.Code != proto.CodeNotLoaded {
		t.Fatalf("a read of it: %v", err)
	}
	if got := len(host.got("session.items")); got != 2 {
		t.Fatalf("items asked %d times: once to find the owner, once for the read", got)
	}
}

// A session of a runtime that has gone (DSH closed) is refused with a reason, not resumed in herdr.
func TestRuntimeAwaySessionIsRefusedNotResumed(t *testing.T) {
	n, _ := runtimeNode(t)
	host := dialRuntime(t, n, dshHello())
	const id = "0bd545f1-681d-4d98-bf5a-eb7c1251a0f0"
	_ = host.peer.Notify("runtime.sessions", map[string]any{"sessions": []map[string]any{{"session_id": id, "status": "done"}}})
	waitFor(t, "listed", func() bool { _, ok := listed(t, n)[id]; return ok })
	_ = host.conn.Close()
	waitFor(t, "gone", func() bool { _, ok := listed(t, n)[id]; return !ok })
	_, err := nodeCall(t, n, "session.send", map[string]any{"session_id": id, "text": "hi", "client_msg_id": "m-6"})
	var re *rpc.Error
	if !errors.As(err, &re) || re.Code != "runtime_away" || !strings.Contains(re.Message, "DSH 没有连上") || strings.Contains(re.Message, "herdr") {
		t.Fatalf("got %v", err)
	}
	if !strings.Contains(string(re.Data), `"none"`) {
		t.Fatalf("data %s", re.Data)
	}
	// Unwatching stays quiet.
	if _, err := nodeCall(t, n, "session.unwatch", map[string]any{"session_id": id}); err != nil {
		t.Fatalf("unwatch: %v", err)
	}
	time.Sleep(10 * time.Millisecond)
}

// Two runtimes list one session: pi's web server from disk (offline), a pi in a terminal running it. The one running
// it gets the calls, whichever connected last — a send to the other would make a second writer of a file pi does
// not lock. When it stops running the session, the one that can open it from disk takes over.
func TestRuntimeRunningBeatsOnDisk(t *testing.T) {
	n, _ := runtimeNode(t)
	const id = "01a19a53-0cab-7122-bec6-6f326bf6e73d"
	tui := dialRuntime(t, n, map[string]any{"runtime": "pi-4242", "agent": "pi", "caps": map[string]any{"send": true}})
	_ = tui.peer.Notify("runtime.sessions", map[string]any{"sessions": []map[string]any{{"session_id": id, "status": "working"}}})
	waitFor(t, "listed by the terminal", func() bool { _, ok := listed(t, n)[id]; return ok })
	web := dialRuntime(t, n, map[string]any{"runtime": "pi-web", "agent": "pi", "caps": map[string]any{"send": true}}) // newer
	_ = web.peer.Notify("runtime.sessions", map[string]any{"sessions": []map[string]any{{"session_id": id, "status": "offline"}}})
	waitFor(t, "the web server's list taken", func() bool { return len(n.runtimes.appendSessions(nil)) == 1 && n.runtimes.ownerOf(id) != nil })
	if _, err := nodeCall(t, n, "session.send", map[string]any{"session_id": id, "text": "one", "client_msg_id": "m-a"}); err != nil {
		t.Fatal(err)
	}
	if len(tui.got("session.send")) != 1 || len(web.got("session.send")) != 0 {
		t.Fatalf("sends: terminal %d, web %d (want the running one)", len(tui.got("session.send")), len(web.got("session.send")))
	}
	if s := listed(t, n)[id]; s.Status != "working" {
		t.Fatalf("listed as %q, want the running runtime's row", s.Status)
	}
	_ = tui.peer.Notify("runtime.sessions", map[string]any{"sessions": []map[string]any{}}) // the terminal closed it
	waitFor(t, "the web server takes over", func() bool { return n.runtimes.ownerOf(id) != nil && listed(t, n)[id].Status == "offline" })
	if _, err := nodeCall(t, n, "session.send", map[string]any{"session_id": id, "text": "two", "client_msg_id": "m-b"}); err != nil {
		t.Fatal(err)
	}
	if len(web.got("session.send")) != 1 {
		t.Fatal("the web server did not get the send once the terminal let go")
	}
}

// A runtime that starts sessions somewhere else (pi's companion starts them in pi-web) reports them offline; the
// runtime already running it keeps it, and the start answers with that runtime's row.
func TestRuntimeStartLeavesTheSessionToTheOneRunningIt(t *testing.T) {
	n, _ := runtimeNode(t)
	const id = "01a19a6d-3f4f-711a-892d-f86bcccb2477"
	web := dialRuntime(t, n, map[string]any{"runtime": "pi-web", "agent": "pi", "caps": map[string]any{"send": true}})
	files := dialRuntime(t, n, map[string]any{"runtime": "pi-files", "agent": "pi", "launcher": "pi-web", "caps": map[string]any{"start": true, "send": true}})
	files.answer = func(method string, params json.RawMessage) (any, error) {
		if method == "session.start" {
			// pi-web made the session and its extension listed it as running before the companion answered.
			_ = web.peer.Notify("runtime.sessions", map[string]any{"sessions": []map[string]any{{"session_id": id, "status": "working", "title": "in pi-web"}}})
			waitFor(t, "listed by pi-web", func() bool { return n.runtimes.ownerOf(id) != nil })
			return map[string]any{"session": map[string]any{"session_id": id, "cwd": "/w", "status": "offline"}}, nil
		}
		return map[string]any{"ok": true}, nil
	}
	raw, err := nodeCall(t, n, "session.start", map[string]any{"cwd": "/w", "launcher": "pi-web", "async": true})
	if err != nil {
		t.Fatal(err)
	}
	var started struct {
		Session proto.Session `json:"session"`
	}
	_ = json.Unmarshal(raw, &started)
	if started.Session.Status != proto.StatusWorking || started.Session.SourceInstanceID != "pi-web" {
		t.Fatalf("started %s (want pi-web's running row)", raw)
	}
	_ = files.peer.Notify("runtime.sessions", map[string]any{"sessions": []map[string]any{}}) // its next list: not on disk yet
	time.Sleep(100 * time.Millisecond)
	if s, ok := listed(t, n)[id]; !ok || s.Status != proto.StatusWorking {
		t.Fatalf("lost after the starter's next list: %+v %v", s, ok)
	}
	if _, err := nodeCall(t, n, "session.send", map[string]any{"session_id": id, "text": "hi", "client_msg_id": "m-s"}); err != nil {
		t.Fatal(err)
	}
	if len(web.got("session.send")) != 1 || len(files.got("session.send")) != 0 {
		t.Fatal("the send did not go to pi-web, which runs it")
	}
}
