package node

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/proto"
)

// Starts on a machine whose engine is a runtime (Paseo) that must survive a lost answer or a takeover (#8130): the id
// the hub names for a new session reaches the runtime, rows of sessions Persona started say so, a branch goes on with
// the launcher its session ran on, and a session taken over while the hub watches it is watched in the runtime too.

// The id the hub names for a new session goes to the runtime (session_id), which gives its session that id: a start
// whose answer the hub never got is found by it. A takeover keeps its own id and carries none; an id the node would
// not take is not passed on.
func TestEngineRuntimeStartCarriesTheNamedSession(t *testing.T) {
	n, _ := runtimeNode(t)
	if err := n.useEngine(context.Background(), proto.EnginePaseo); err != nil {
		t.Fatal(err)
	}
	host := dialRuntime(t, n, paseoEngineHello())
	host.answer = func(method string, params json.RawMessage) (any, error) {
		var p struct {
			SessionID string `json:"session_id"`
			Resume    string `json:"resume"`
		}
		_ = json.Unmarshal(params, &p)
		id := p.SessionID
		if id == "" {
			id = "paseo-own"
		}
		if p.Resume != "" {
			id = p.Resume
		}
		return map[string]any{"session": map[string]any{"session_id": id, "status": "idle", "cwd": "/w/app", "started_by_persona": true}}, nil
	}
	const named = "bbbbbbbb-0000-4000-8000-000000000042"
	raw, err := nodeCall(t, n, "session.start", map[string]any{"cwd": "/w/app", "launcher": "paseo-claude", "session_id": named, "prompt": "go"})
	if err != nil {
		t.Fatal(err)
	}
	var res struct {
		Session proto.Session `json:"session"`
	}
	_ = json.Unmarshal(raw, &res)
	if res.Session.SessionID != named || !res.Session.StartedByPersona {
		t.Fatalf("started %s", raw)
	}
	params := func(i int) map[string]any {
		var m map[string]any
		_ = json.Unmarshal(host.got("session.start")[i], &m)
		return m
	}
	if params(0)["session_id"] != named {
		t.Fatalf("the runtime was not told the session's id: %v", params(0))
	}
	if _, err := nodeCall(t, n, "session.start", map[string]any{"cwd": "/w/app", "launcher": "paseo-claude", "session_id": "../not-an-id"}); err != nil {
		t.Fatal(err)
	}
	if _, ok := params(1)["session_id"]; ok {
		t.Fatalf("an id the node would not take was passed on: %v", params(1))
	}
	sdkTranscript(t, n, sdkSession, "where we left off")
	if _, err := nodeCall(t, n, "session.start", map[string]any{"cwd": "/w/app", "resume_session_id": sdkSession, "session_id": named}); err != nil {
		t.Fatal(err)
	}
	if p := params(2); p["resume"] != sdkSession || p["session_id"] != nil {
		t.Fatalf("a takeover: %v", p)
	}
}

// A row a runtime says Persona started stays so (Paseo knows by the label on its agent, also after the node restarts in
// the middle of a start); the rest are not, and a daemon's never are: its sessions are another machine's or user's.
func TestRuntimeRowsSayWhichSessionsPersonaStarted(t *testing.T) {
	n, _ := runtimeNode(t)
	host := dialRuntime(t, n, paseoEngineHello())
	_ = host.peer.Notify("runtime.sessions", map[string]any{"sessions": []map[string]any{
		{"session_id": "paseo-mine", "status": "idle", "started_by_persona": true},
		{"session_id": "paseo-theirs", "status": "idle"},
	}})
	waitFor(t, "listed", func() bool { _, ok := listed(t, n)["paseo-theirs"]; return ok })
	if got := listed(t, n); !got["paseo-mine"].StartedByPersona || got["paseo-theirs"].StartedByPersona {
		t.Fatalf("started_by_persona: mine %v, theirs %v", got["paseo-mine"].StartedByPersona, got["paseo-theirs"].StartedByPersona)
	}
	daemon := &runtimeConn{daemon: "dmn", hello: runtimeHello{Runtime: "dmn"}}
	if rows := normalizeRuntimeSessions(daemon, []proto.Session{{SessionID: "dmn.s1", StartedByPersona: true}}); rows[0].StartedByPersona {
		t.Fatal("a daemon's session said Persona started it")
	}
}

// A session a runtime listed only while starting it, or after that start failed, is not remembered as the runtime's
// once it leaves the list: a Claude session whose takeover failed is this machine's again (its next message takes it
// over again, its pages are its transcript's). One it listed as its own is remembered as before.
func TestRuntimeRowsOnlyStartingAreNotRemembered(t *testing.T) {
	n, _ := runtimeNode(t)
	host := dialRuntime(t, n, paseoEngineHello())
	_ = host.peer.Notify("runtime.sessions", map[string]any{"sessions": []map[string]any{
		{"session_id": sdkSession, "status": "idle", "starting": true},
		{"session_id": "paseo-failed", "status": "offline", "start_error": "Paseo did not start this session: no"},
		{"session_id": "paseo-ran", "status": "idle"},
	}})
	waitFor(t, "listed", func() bool { _, ok := listed(t, n)["paseo-ran"]; return ok })
	_ = host.peer.Notify("runtime.sessions", map[string]any{"sessions": []map[string]any{}})
	waitFor(t, "off the list", func() bool { _, ok := listed(t, n)["paseo-ran"]; return !ok })
	for id, want := range map[string]bool{sdkSession: false, "paseo-failed": false, "paseo-ran": true} {
		if _, _, known := n.runtimes.belongs(id); known != want {
			t.Errorf("%s remembered as the runtime's: %v, want %v", id, known, want)
		}
	}
}

// A takeover the runtime had under way when it left (Paseo still importing the session, or the import failed) stays
// listed as the conversation it is, not running: not as starting, nor as a start that failed, for as long as the node
// keeps the rows of a runtime that left.
func TestRuntimeAwayDropsATakeoverUnderWay(t *testing.T) {
	const (
		importing = "5d510000-0000-4000-8000-0000000000d1"
		failed    = "5d510000-0000-4000-8000-0000000000d2"
	)
	t.Setenv("PERSONA_HOME", "")
	n, _ := runtimeNode(t)
	t.Cleanup(freshOffline)
	dir := filepath.Join(n.home, ".claude", "projects", "-src-app")
	writeClaudeRun(t, dir, importing, time.Minute, "sdk-cli", "sdk-cli")
	writeClaudeRun(t, dir, failed, 2*time.Minute, "sdk-cli", "sdk-cli")
	host := dialRuntime(t, n, paseoHello())
	_ = host.peer.Notify("runtime.sessions", map[string]any{"sessions": []map[string]any{
		{"session_id": importing, "transcript": importing, "agent": "claude", "cwd": "/src/app", "status": "idle", "starting": true},
		{"session_id": failed, "transcript": failed, "agent": "claude", "cwd": "/src/app", "status": "offline",
			"start_error": "Paseo did not start this session: no"},
	}, "unlisted_transcripts": []string{}})
	waitFor(t, "listed by Paseo", func() bool { return listed(t, n)[importing].Starting })
	_ = host.conn.Close()
	waitFor(t, "the runtime left", func() bool { return runtimeGone(n, "paseo") && !listed(t, n)[importing].Starting })
	freshOffline()
	got := listed(t, n)
	for _, id := range []string{importing, failed} {
		if s, ok := got[id]; !ok || s.Status != proto.StatusOffline || s.Starting || s.StartError != "" {
			t.Fatalf("%s while Paseo is away: listed %v, %+v", id, ok, s)
		}
	}
}

// "Branch to new chat" of a runtime's session keeps the launcher it ran on (Paseo: its provider), and the branch's
// first message has the runtime take it over with that launcher: not with whichever provider ran last, maybe another
// account's.
func TestRuntimeBranchGoesOnWithItsLauncher(t *testing.T) {
	n, _ := runtimeNode(t)
	sdkTranscript(t, n, sdkSession, "the build is fixed")
	if err := n.useEngine(context.Background(), proto.EnginePaseo); err != nil {
		t.Fatal(err)
	}
	host := dialRuntime(t, n, paseoEngineHello())
	var branch string
	host.mu.Lock()
	host.answer = func(method string, params json.RawMessage) (any, error) {
		if method == "session.start" {
			host.mu.Lock()
			id := branch
			host.mu.Unlock()
			return map[string]any{"session": map[string]any{"session_id": id, "transcript": id, "agent": "claude", "status": "idle", "cwd": "/w/app"}}, nil
		}
		return map[string]any{"ok": true}, nil
	}
	host.mu.Unlock()
	_ = host.peer.Notify("runtime.sessions", map[string]any{"sessions": []map[string]any{{"session_id": "paseo-a1", "transcript": sdkSession,
		"agent": "claude", "title": "Fix the build", "cwd": "/w/app", "status": "idle", "launcher": "my-claude", "model": "claude-opus-5-5"}}})
	waitFor(t, "listed", func() bool { return listed(t, n)["paseo-a1"].CanFork })
	id, err := n.fork(context.Background(), "paseo-a1", "")
	if err != nil {
		t.Fatal(err)
	}
	host.mu.Lock()
	branch = id
	host.mu.Unlock()
	if st, _ := n.state.get(id); st.Launcher != "my-claude" {
		t.Fatalf("the branch's launcher: %q", st.Launcher)
	}
	if _, err := nodeCall(t, n, "session.send", map[string]any{"session_id": id, "text": "carry on", "client_msg_id": "m-branch"}); err != nil {
		t.Fatal(err)
	}
	var start map[string]any
	_ = json.Unmarshal(host.got("session.start")[0], &start)
	if start["resume"] != id || start["launcher"] != "my-claude" {
		t.Fatalf("taken over with %v", start)
	}
}

// The phone has a Claude session open that runs nowhere (the node follows its transcript), and sends to it: the runtime
// takes it over. From then on its live text comes from the runtime, which is asked to watch it as one whose conversation
// is that transcript; the runtime's next list naming the same transcript moves nothing, and the node's watch of the
// transcript goes on.
func TestEngineRuntimeWatchesASessionItTookOverWhileOpen(t *testing.T) {
	n, hub := runtimeNode(t)
	sdkTranscript(t, n, sdkSession, "where we left off")
	if err := n.useEngine(context.Background(), proto.EnginePaseo); err != nil {
		t.Fatal(err)
	}
	host := dialRuntime(t, n, paseoEngineHello())
	row := map[string]any{"session_id": sdkSession, "transcript": sdkSession, "agent": "claude", "status": "working", "cwd": "/w/app"}
	host.mu.Lock()
	host.answer = func(method string, params json.RawMessage) (any, error) {
		if method == "session.start" {
			return map[string]any{"session": row}, nil
		}
		return map[string]any{"ok": true}, nil
	}
	host.mu.Unlock()
	waitFor(t, "listed offline", func() bool { freshOffline(); return listed(t, n)[sdkSession].Status == proto.StatusOffline })
	runtimeSessionCall(t, hub, "session.watch", map[string]any{"session_id": sdkSession, "since_seq": 0})
	if _, err := nodeCall(t, n, "session.send", map[string]any{"session_id": sdkSession, "text": "carry on", "client_msg_id": "m-tw"}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the runtime asked to watch it", func() bool { return len(host.got("session.watch")) == 1 })
	var w map[string]any
	_ = json.Unmarshal(host.got("session.watch")[0], &w)
	if w["session_id"] != sdkSession || w["transcript"] != true {
		t.Fatalf("watched with %v", w)
	}
	// The runtime was asked to watch it already, so its list asks for no second watch (setSessions' rewatch).
	n.runtimes.mu.Lock()
	h := n.runtimes.byRuntime(proto.EnginePaseo)
	n.runtimes.mu.Unlock()
	n.mu.Lock()
	before := n.watches[sdkSession]
	n.mu.Unlock()
	if h == nil || !n.runtimes.watchedOn(h, sdkSession) || before == nil {
		t.Fatalf("before the list: the runtime watches it %v, the transcript followed %v", h != nil && n.runtimes.watchedOn(h, sdkSession), before != nil)
	}
	_ = host.peer.Notify("runtime.sessions", map[string]any{"sessions": []map[string]any{row}})
	// The node takes a runtime's events in order, each before the request that follows it: once this request is
	// answered (refused: no such package), the list has been taken in, and a transcript it moved would have been
	// unwatched and watched again (transcriptMoved).
	_ = host.peer.Call(context.Background(), "runtime.package", map[string]any{"name": "-"}, &struct{}{})
	if listed(t, n)[sdkSession].SourceKind != proto.SourceRuntime {
		t.Fatal("the runtime's list did not make the session its own")
	}
	n.mu.Lock()
	after := n.watches[sdkSession]
	n.mu.Unlock()
	if got := len(host.got("session.watch")); got != 1 || after != before || len(host.got("session.unwatch")) != 0 {
		t.Fatalf("after the list: %d watches, %d unwatches, the same transcript watch %v", got, len(host.got("session.unwatch")), after == before)
	}
	// The phone closes the chat: the runtime stops watching too.
	runtimeSessionCall(t, hub, "session.unwatch", map[string]any{"session_id": sdkSession})
	waitFor(t, "unwatched in the runtime", func() bool { return len(host.got("session.unwatch")) == 1 })
}

// The same takeover answered with a row that names no transcript, which a later list of the runtime names: meanwhile
// the session's items are the runtime's, which ties the message to its own line. Once the session reads its transcript
// again, the message's line there carries the message's client_msg_id (#8358). It stayed on the runtime's line: the
// e2e's branch, taken over this way, read back without it about every other run.
func TestEngineRuntimeTakeoverKeepsTheMessageID(t *testing.T) {
	n, hub := runtimeNode(t)
	path := sdkTranscript(t, n, sdkSession, "where we left off")
	if err := n.useEngine(context.Background(), proto.EnginePaseo); err != nil {
		t.Fatal(err)
	}
	host := dialRuntime(t, n, paseoEngineHello())
	row := map[string]any{"session_id": sdkSession, "agent": "claude", "status": "working", "cwd": "/w/app"}
	host.mu.Lock()
	host.answer = func(method string, params json.RawMessage) (any, error) {
		switch method {
		case "session.start":
			return map[string]any{"session": row}, nil
		case "session.items": // its own items: the message, on a line the runtime tied itself
			return map[string]any{"to_seq": 7, "items": []proto.Item{{ID: "7#0", Seq: 7, Role: "user", Kind: proto.KindText,
				Text: "carry on", ClientMsgID: "m-tk", TS: time.Now()}}}, nil
		}
		return map[string]any{"ok": true}, nil
	}
	host.mu.Unlock()
	waitFor(t, "listed offline", func() bool { freshOffline(); return listed(t, n)[sdkSession].Status == proto.StatusOffline })
	runtimeSessionCall(t, hub, "session.watch", map[string]any{"session_id": sdkSession, "since_seq": 0})
	if _, err := nodeCall(t, n, "session.send", map[string]any{"session_id": sdkSession, "text": "carry on", "client_msg_id": "m-tk"}); err != nil {
		t.Fatal(err)
	}
	if got := pageTags(t, n, sdkSession); got["carry on"] != "m-tk" || len(host.got("session.items")) != 1 {
		t.Fatalf("the runtime's page: %v (%d asked)", got, len(host.got("session.items")))
	}
	// Claude Code writes the message into the transcript, and the runtime's next list names it.
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = f.WriteString(userLine("u9", time.Now(), "carry on") + "\n")
	_ = f.Close()
	row["transcript"] = sdkSession
	_ = host.peer.Notify("runtime.sessions", map[string]any{"sessions": []map[string]any{row}})
	waitFor(t, "its transcript named", func() bool { return n.runtimes.transcriptOf(sdkSession) == sdkSession })
	if got := pageTags(t, n, sdkSession); got["carry on"] != "m-tk" || len(host.got("session.items")) != 1 {
		t.Fatalf("the transcript's page: %v (the runtime asked %d times)", got, len(host.got("session.items")))
	}
}
