package node

import (
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/proto"
)

// Claude Code's question after a turn stopped on a usage limit is answered with its first choice only when that
// choice is the selected one; other screens get nothing (#6237).
func TestLimitQuestionKeys(t *testing.T) {
	q := screenFixture(t, "claude-limit-question.txt")
	if got := limitQuestionKeys(q); !slices.Equal(got, []string{"enter"}) {
		t.Fatalf("on the question: %q", got)
	}
	moved := strings.Replace(strings.Replace(q, "❯ 1.", "  1.", 1), "  2. Wait here", "❯ 2. Wait here", 1)
	if got := limitQuestionKeys(moved); got != nil {
		t.Errorf("with another choice selected: %q", got)
	}
	if got := limitQuestionKeys(screenFixture(t, "claude-2.1.312-empty-162.txt")); got != nil {
		t.Errorf("on an empty input box: %q", got)
	}
}

// limitQuestionNode is switchNode for an agent that stopped on its usage limit and shows Claude Code's question (herdr:
// blocked). Enter on it brings the input box back and the agent idle, as Claude Code does.
func limitQuestionNode(t *testing.T) (*Node, *fakeHerdr, string) {
	t.Helper()
	n, f, _, cwd, _ := switchNode(t, proto.StatusBlocked, nil)
	f.setScreen("w1:p1", screenFixture(t, "claude-limit-question.txt"))
	var once sync.Once
	f.after(func(c herdrCall) {
		if c.Method == "pane.send_keys" && str(c.Params["pane_id"]) == "w1:p1" && slices.Equal(keysOf(c), []string{"enter"}) {
			once.Do(func() {
				f.setScreens("w1:p1", screenFixture(t, "claude-2.1.312-empty-162.txt"), screenFixture(t, "claude-2.1.312-empty-162.ansi"))
				f.reply("session.snapshot", snapshot(nil, nil, []any{claudeAgent(sidA, proto.StatusIdle, "w1:p1", "w1:t1", "w1", cwd, "")}, nil))
			})
		}
	})
	return n, f, cwd
}

// The assistant's switch of an agent on that question answers it (stop and wait), then quits the agent and resumes it
// on the launcher picked, with the message; before #6237 it was refused as busy (switch_busy) for as long as the
// question stood.
func TestSwitchNowAnswersTheLimitQuestion(t *testing.T) {
	n, f, cwd := limitQuestionNode(t)
	if err := pick(t, n, map[string]any{"launcher": "work", "from": "claude"}); err != nil {
		t.Fatal(err)
	}
	f.reset()
	quitsToShell(f, cwd)
	comesUpWhen(f, "w1:p1", func(string) bool { return true })
	if err := tellNow(t, n, sidA, "go on"); err != nil {
		t.Fatalf("send: %v\n%s", err, strings.Join(f.trace(), "\n"))
	}
	keys := f.trace("pane.send_keys")
	if len(keys) == 0 || !strings.Contains(keys[0], "enter") {
		t.Fatalf("the question was not answered first: %q", keys)
	}
	if starts := typedStarts(f, "w1:p1"); len(starts) != 1 || !strings.Contains(starts[0], " launch work ") {
		t.Fatalf("typed %q", starts)
	}
	if st, _ := n.state.get(sidA); st.Launcher != "work" || st.PendingSwitch != nil {
		t.Errorf("record after the switch = %+v", st)
	}
}

// Any other blocked screen, here the same question with another choice selected (someone at the computer), is left
// alone: the switch is refused as busy with nothing pressed.
func TestSwitchNowLeavesOtherQuestions(t *testing.T) {
	n, f, _, _, _ := switchNode(t, proto.StatusBlocked, nil)
	q := screenFixture(t, "claude-limit-question.txt")
	f.setScreen("w1:p1", strings.Replace(strings.Replace(q, "❯ 1.", "  1.", 1), "  3. Switch", "❯ 3. Switch", 1))
	n.state.setPendingSwitch(sidA, &proto.PendingSwitch{Launcher: "work", At: time.Now()})
	f.reset()
	err := tellNow(t, n, sidA, "go on")
	if code, _ := refusalOf(t, err); code != proto.CodeSwitchBusy {
		t.Fatalf("refused with %q (%v)", code, err)
	}
	if got := drove(f); len(got) != 0 {
		t.Errorf("drove the terminal: %q", got)
	}
}

// The batch switch's session.switch (#6008) of an agent on that question answers it and switches it with no message:
// before #6237 its queued switch was refused as busy every time it was tried, until it aged out.
func TestSessionSwitchAnswersTheLimitQuestion(t *testing.T) {
	n, f, cwd := limitQuestionNode(t)
	if err := pick(t, n, map[string]any{"launcher": "work", "from": "claude"}); err != nil {
		t.Fatal(err)
	}
	f.reset()
	quitsToShell(f, cwd)
	comesUpWhen(f, "w1:p1", func(string) bool { return true })
	if _, err := request(t, n, "session.switch", map[string]any{"session_id": sidA}); err != nil {
		t.Fatalf("switch: %v\n%s", err, strings.Join(f.trace(), "\n"))
	}
	if starts := typedStarts(f, "w1:p1"); len(starts) != 1 || !strings.Contains(starts[0], " launch work ") {
		t.Fatalf("typed %q", starts)
	}
	if got := f.trace("agent.prompt"); len(got) != 0 {
		t.Errorf("prompted %q", got)
	}
}
