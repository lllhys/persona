//go:build !windows

package node

import (
	"strings"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/prompts"
	"github.com/nwverse/persona/server/internal/proto"
)

func inboxPriority(t *testing.T, inbox *fakeInbox, i int) (priority, content string) {
	t.Helper()
	users := inbox.users()
	if len(users) <= i {
		t.Fatalf("the inbox read %d messages, want message %d", len(users), i+1)
	}
	priority, _ = users[i]["priority"].(string)
	content, _ = users[i]["message"].(map[string]any)["content"].(string)
	return priority, content
}

// askLine is an assistant line of session id calling tool (the call its dialog waits on).
func askLine(id, tool, input string) string {
	return `{"type":"assistant","uuid":"a2","timestamp":"2026-11-04T09:00:09Z","sessionId":"` + id + `","message":{"role":"assistant",` +
		`"content":[{"type":"tool_use","id":"toolu_1","name":"` + tool + `","input":` + input + `}]}}`
}

// A claude outside Persona's panes that waits on a question it asked takes the phone's message as its answer: it goes at
// priority now, which ends that wait and runs the message; queued it would wait behind the question. Its row says a
// reply answers it. One waiting on a permission prompt gets the message queued as before (at now it would decline
// the prompt, the computer's to approve), its row saying the computer answers it; a busy one gets it queued too.
func TestAWaitingOutsideClaudeTakesAMessageAsItsAnswer(t *testing.T) {
	n, f, home := herdrNode(t)
	noPaneEnv(t)
	shortPeerWait(t, 3*time.Second)
	f.reply("session.snapshot", snapshot(nil, nil, nil, nil))
	p := writeTranscript(t, home, home, sidA, time.Now(),
		`{"type":"user","uuid":"u1","timestamp":"2026-11-04T09:00:00Z","cwd":"`+home+`","sessionId":"`+sidA+`","message":{"role":"user","content":"plan dinner"}}`,
		askLine(sidA, "AskUserQuestion", `{"questions":[{"question":"Which one?","header":"Dinner","options":[{"label":"A"},{"label":"B"}],"multiSelect":false}]}`))
	inbox := newFakeInbox(t, p)
	writeOutsideClaude(t, home, sidA, "claude-desktop-3p", "waiting", inbox, nil)
	s := byID(listSessions(t, n))[sidA]
	if s.Status != proto.StatusBlocked || s.AttentionReason != proto.AttentionClaudeAnswer || s.Capabilities == nil || !s.Capabilities.Interrupt {
		t.Fatalf("asking: row %+v caps %+v", s, s.Capabilities)
	}
	res, err := request(t, n, "session.send", map[string]any{"session_id": sidA, "text": "B, the second one", "client_msg_id": "m-1"})
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	if pr, content := inboxPriority(t, inbox, 0); pr != "now" || !strings.Contains(content, "B, the second one") {
		t.Fatalf("answer went at %q: %q", pr, content)
	}
	if res["waits_on_computer"] != nil { // the status file still says waiting: the answer ended the wait all the same
		t.Fatalf("the answer said it waits on the computer: %v", res)
	}
	writeOutsideClaude(t, home, sidA, "claude-desktop-3p", "busy", inbox, nil)
	if _, err := request(t, n, "session.send", map[string]any{"session_id": sidA, "text": "and then this", "client_msg_id": "m-2"}); err != nil {
		t.Fatalf("send: %v", err)
	}
	if pr, _ := inboxPriority(t, inbox, 1); pr != "next" {
		t.Fatalf("a busy session's message went at %q", pr)
	}

	// The question was answered on the computer, and the next call waits on a permission prompt. A send reads the
	// transcript as it is now, not as the list read it a moment ago (metaMinInterval).
	appendLines(t, p, askLine(sidA, "Bash", `{"command":"rm -rf build"}`))
	writeOutsideClaude(t, home, sidA, "claude-desktop-3p", "waiting", inbox, nil)
	res, err = request(t, n, "session.send", map[string]any{"session_id": sidA, "text": "why delete it?", "client_msg_id": "m-3"})
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	if pr, _ := inboxPriority(t, inbox, 2); pr != "next" || res["waits_on_computer"] != true {
		t.Fatalf("a message to a session on a permission prompt went at %q: %v", pr, res)
	}
	forgetMetas(n)
	s = byID(listSessions(t, n))[sidA]
	if s.Status != proto.StatusBlocked || s.AttentionReason != proto.AttentionClaudeComputer || s.Capabilities == nil || !s.Capabilities.Interrupt {
		t.Fatalf("permission prompt: row %+v caps %+v", s, s.Capabilities)
	}
}

// The phone's Stop on a claude outside Persona's panes cuts its turn through its inbox: a message at priority now that
// asks it to stop, and no key pressed anywhere. One at rest has nothing to stop (done, nothing sent); one without an
// inbox is refused.
func TestStopAnOutsideClaudeThroughItsInbox(t *testing.T) {
	n, f, home := herdrNode(t)
	noPaneEnv(t)
	shortPeerWait(t, 3*time.Second)
	p := bgTranscript(t, home, home, sidA, "plan")
	inbox := newFakeInbox(t, p)
	f.reply("session.snapshot", snapshot(nil, nil, nil, nil))
	f.reset()
	writeOutsideClaude(t, home, sidA, "claude-desktop", "busy", inbox, nil)
	if _, err := request(t, n, "session.interrupt", map[string]any{"session_id": sidA}); err != nil {
		t.Fatalf("interrupt: %v", err)
	}
	if pr, content := inboxPriority(t, inbox, 0); pr != "now" || !strings.Contains(content, prompts.PeerStop()) ||
		!strings.HasPrefix(content, `<cross-session-message from-name="Persona">`) {
		t.Fatalf("stop went at %q: %q", pr, content)
	}
	for _, c := range f.trace() {
		if strings.Contains(c, "pane.") || strings.Contains(c, "keys") {
			t.Fatalf("a key was pressed: %v", f.trace())
		}
	}

	writeOutsideClaude(t, home, sidA, "claude-desktop", "idle", inbox, nil)
	if s := byID(listSessions(t, n))[sidA]; s.Capabilities == nil || s.Capabilities.Interrupt {
		t.Fatalf("an idle session offers Stop: %+v", s.Capabilities)
	}
	if _, err := request(t, n, "session.interrupt", map[string]any{"session_id": sidA}); err != nil {
		t.Fatalf("interrupt at rest: %v", err)
	}
	if len(inbox.users()) != 1 {
		t.Fatalf("something was sent to a session at rest: %v", inbox.users())
	}

	writeOutsideClaude(t, home, sidA, "claude-desktop", "waiting", nil, nil) // no inbox
	if s := byID(listSessions(t, n))[sidA]; s.Capabilities == nil || s.Capabilities.Interrupt || s.AttentionReason != proto.AttentionClaudeComputer {
		t.Fatalf("no inbox: %+v %q", s.Capabilities, s.AttentionReason)
	}
	if _, err := request(t, n, "session.interrupt", map[string]any{"session_id": sidA}); err == nil || !strings.Contains(err.Error(), peerNoInboxMsg) {
		t.Fatalf("no inbox: %v", err)
	}
}
