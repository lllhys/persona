package node

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// A Codex App chat's rollout, with lines appended as the App writes them.
func appRollout(t *testing.T, lines ...string) (path string, write func(...string)) {
	t.Helper()
	path = filepath.Join(t.TempDir(), "rollout.jsonl")
	write = func(lines ...string) {
		f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
		if err != nil {
			t.Fatal(err)
		}
		for _, l := range lines {
			f.WriteString(l + "\n")
		}
		f.Close()
	}
	write(lines...)
	return path, write
}

const (
	turnStarted  = `{"type":"event_msg","payload":{"type":"task_started"}}`
	turnComplete = `{"type":"event_msg","payload":{"type":"task_complete"}}`
)

func fastCodexDesktopHold(t *testing.T) {
	t.Helper()
	poll, grace := codexDesktopHoldPoll, codexDesktopStartGrace
	codexDesktopHoldPoll, codexDesktopStartGrace = 10*time.Millisecond, time.Minute
	t.Cleanup(func() { codexDesktopHoldPoll, codexDesktopStartGrace = poll, grace })
}

func turnText(t *testing.T, p map[string]any) string {
	t.Helper()
	req := p["turnStart"].(map[string]any)["request"].(map[string]any)
	in := req["input"].([]any)[0].(map[string]any)
	return in["text"].(string)
}

func noTurn(t *testing.T, bus *fakeCodexBus, within time.Duration) {
	t.Helper()
	select {
	case p := <-bus.turns:
		t.Fatalf("a turn was started: %q", turnText(t, p))
	case <-time.After(within):
	}
}

func oneTurn(t *testing.T, bus *fakeCodexBus) string {
	t.Helper()
	select {
	case p := <-bus.turns:
		return turnText(t, p)
	case <-time.After(5 * time.Second):
		t.Fatal("no turn was started")
		return ""
	}
}

// The case behind the report: a message sent while the App's chat runs a turn reached the agent twice, steered into
// the turn and then again as a turn of its own. It must go in once, after the turn, and the send says it is queued.
func TestSendCodexDesktopWhileWorkingDeliversOnceAfterTheTurn(t *testing.T) {
	fastCodexDesktopHold(t)
	home, bus := startFakeCodexBus(t, "thread-busy")
	path, write := appRollout(t, turnStarted)
	n := &Node{dir: t.TempDir()}
	ctx, queued := withSendQueued(context.Background())
	src := codexSource{home: home, thread: "thread-busy", path: path}
	if err := n.sendCodexDesktop(ctx, src, "finish the import", nil); err != nil {
		t.Fatal(err)
	}
	if !*queued {
		t.Fatal("the send did not say the message waits behind the turn")
	}
	noTurn(t, bus, 200*time.Millisecond) // nothing steered into the running turn

	write(turnComplete)
	if got := oneTurn(t, bus); got != "finish the import" {
		t.Fatalf("turn text = %q", got)
	}
	noTurn(t, bus, 300*time.Millisecond) // and never again
}

// Two messages while it works go in together, in order, as one turn.
func TestSendCodexDesktopWhileWorkingKeepsOrderInOneTurn(t *testing.T) {
	fastCodexDesktopHold(t)
	home, bus := startFakeCodexBus(t, "thread-two")
	path, write := appRollout(t, turnStarted)
	n := &Node{dir: t.TempDir()}
	src := codexSource{home: home, thread: "thread-two", path: path}
	for _, text := range []string{"first", "second"} {
		ctx, queued := withSendQueued(context.Background())
		if err := n.sendCodexDesktop(ctx, src, text, nil); err != nil || !*queued {
			t.Fatalf("send %q: err %v, queued %v", text, err, *queued)
		}
	}
	noTurn(t, bus, 100*time.Millisecond)
	write(turnComplete)
	if got := oneTurn(t, bus); got != "first\n\nsecond" {
		t.Fatalf("turn text = %q", got)
	}
	noTurn(t, bus, 300*time.Millisecond)
}

// An idle chat takes the message at once. A second message right after it, before the App has written the turn the
// first started, waits for that turn instead of being steered into it.
func TestSendCodexDesktopRightAfterAStartWaitsForThatTurn(t *testing.T) {
	fastCodexDesktopHold(t)
	home, bus := startFakeCodexBus(t, "thread-idle")
	path, write := appRollout(t, turnStarted, turnComplete)
	n := &Node{dir: t.TempDir()}
	src := codexSource{home: home, thread: "thread-idle", path: path}

	ctx, queued := withSendQueued(context.Background())
	if err := n.sendCodexDesktop(ctx, src, "one", nil); err != nil || *queued {
		t.Fatalf("idle send: err %v, queued %v", err, *queued)
	}
	if got := oneTurn(t, bus); got != "one" {
		t.Fatalf("turn text = %q", got)
	}

	ctx, queued = withSendQueued(context.Background())
	if err := n.sendCodexDesktop(ctx, src, "two", nil); err != nil || !*queued {
		t.Fatalf("second send: err %v, queued %v", err, *queued)
	}
	noTurn(t, bus, 200*time.Millisecond)
	write(turnStarted) // the App writes the first message's turn
	noTurn(t, bus, 100*time.Millisecond)
	write(turnComplete)
	if got := oneTurn(t, bus); got != "two" {
		t.Fatalf("turn text = %q", got)
	}
	noTurn(t, bus, 300*time.Millisecond)
}

// A chat whose rollout the App deleted is read from its history database: a last turn in progress holds the message.
func TestSendCodexDesktopWhileWorkingReadsTheHistoryDatabase(t *testing.T) {
	fastCodexDesktopHold(t)
	home, bus := startFakeCodexBus(t, "thread-db")
	status := make(chan string, 1)
	status <- "inProgress"
	defer func(f func(codexSource) (string, string, bool)) { codexHistoryLastTurn = f }(codexHistoryLastTurn)
	current := "inProgress"
	codexHistoryLastTurn = func(src codexSource) (string, string, bool) {
		select {
		case current = <-status:
		default:
		}
		return "turn-9", current, src.thread == "thread-db"
	}
	n := &Node{dir: t.TempDir()}
	ctx, queued := withSendQueued(context.Background())
	if err := n.sendCodexDesktop(ctx, codexSource{home: home, thread: "thread-db"}, "go on", nil); err != nil || !*queued {
		t.Fatalf("send: err %v, queued %v", err, *queued)
	}
	noTurn(t, bus, 200*time.Millisecond)
	status <- "completed"
	if got := oneTurn(t, bus); got != "go on" {
		t.Fatalf("turn text = %q", got)
	}
	noTurn(t, bus, 300*time.Millisecond)
}

// Through the node's session.send, as agent_send reaches it: a Codex App chat that works answers queued, and its
// message goes in once, after the turn.
func TestSessionSendToAWorkingCodexAppChatAnswersQueuedAndDeliversOnce(t *testing.T) {
	fastCodexDesktopHold(t)
	home, bus := startFakeCodexBus(t, appChat)
	if err := os.WriteFile(filepath.Join(home, ".codex-global-state.json"), []byte(`{"projectless-thread-ids":["`+appChat+`"]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	meta := `{"type":"session_meta","payload":{"id":"` + appChat + `","originator":"Codex Desktop","source":"vscode"}}`
	path, write := appRollout(t, meta, turnStarted)
	n := remoteTestNode(t)
	id := n.registerCodex(home, appChat, path)
	res, err := n.handle(context.Background(), "session.send", mustJSON(map[string]any{"session_id": id, "text": "check the logs"}))
	if err != nil {
		t.Fatal(err)
	}
	if m, _ := res.(map[string]any); m["queued"] != true {
		t.Fatalf("session.send answered %v, want queued", res)
	}
	noTurn(t, bus, 200*time.Millisecond)
	write(turnComplete)
	if got := oneTurn(t, bus); got != "check the logs" {
		t.Fatalf("turn text = %q", got)
	}
	noTurn(t, bus, 300*time.Millisecond)
}
