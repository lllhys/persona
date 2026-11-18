package node

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/host"
	"github.com/nwverse/persona/server/internal/proto"
	"github.com/nwverse/persona/server/internal/transcript"
)

const (
	noCode  = "No code changes"
	edit    = "notes.txt +1 -1"
	longMsg = "Reply with only the word ok6, ignoring all of this padding text which only exists to make the prompt much longer than a single terminal row can show so that the rewind list has to truncate it somewhere in the middle of the sentence."
)

func fixtureRows(t *testing.T, name string) []string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "screen", "testdata", "claude-2.1.318-rewind-"+name+"-119.txt"))
	if err != nil {
		t.Fatal(err)
	}
	return panelRows(strings.Split(string(b), "\n"))
}

// panelRows are the non-blank rows from the Rewind header on, trailing spaces dropped.
func panelRows(lines []string) []string {
	head := -1
	for i, l := range lines {
		if strings.TrimSpace(l) == "Rewind" {
			head = i
		}
	}
	var rows []string
	if head < 0 {
		return nil
	}
	for _, l := range lines[head:] {
		if l = strings.TrimRight(l, " "); l != "" {
			rows = append(rows, l)
		}
	}
	return rows
}

// The simulated pane draws /rewind the way the recorded screens do (the recordings are cut off at the pane's 40 rows,
// so a recording is the start of what the simulation draws).
func TestClaudeSimMatchesRecordedScreens(t *testing.T) {
	four := []simPrompt{{"Append the line \"alpha\" to notes.txt. Reply with only: done", noCode},
		{"Use the Edit tool (not Bash) to replace the line \"alpha\" in notes.txt with \"beta\". Reply with only: ok2", edit},
		{"Use the Edit tool (not Bash) to replace the line \"beta\" in notes.txt with \"gamma\". Reply with only: ok3", edit}}
	three := []simPrompt{four[0], four[1], {"Reply with only: ok5", noCode}}
	long := []simPrompt{{longMsg, noCode}, {"用中文只回复：好的", noCode}}
	for _, c := range []struct {
		fixture string
		prompts []simPrompt
		mode    string
		cursor  int
		choice  int
		age     string
	}{
		{"list-current-edits", four, "list", 3, 0, ""},
		{"list-middle", four, "list", 2, 0, ""},
		{"list-second", four, "list", 1, 0, ""},
		{"list-top", four, "list", 0, 0, ""},
		{"list-current", three, "list", 3, 0, ""},
		{"list-chinese", long, "list", 1, 0, ""},
		{"list-long", long, "list", 0, 0, ""},
		{"options-code", four, "options", 1, 0, "47s ago"},
		{"options-code-last", four, "options", 1, 4, "47s ago"},
		{"options-conversation-selected", four, "options", 2, 1, "1m ago"},
		{"options-nocode", three, "options", 2, 0, "10s ago"},
		{"options-long", long, "options", 0, 0, "18s ago"},
		{"nothing", nil, "nothing", 0, 0, ""},
	} {
		sim := newClaudeSim(c.prompts...)
		sim.mode, sim.cursor, sim.choice = c.mode, c.cursor, c.choice
		if c.age != "" {
			sim.age = c.age
		}
		want := fixtureRows(t, c.fixture)
		got := panelRows(strings.Split(sim.screen(), "\n"))
		if len(got) < len(want) {
			t.Errorf("%s: drew %d rows, recorded %d\n%s", c.fixture, len(got), len(want), strings.Join(got, "\n"))
			continue
		}
		for i := range want {
			if got[i] != want[i] {
				t.Errorf("%s row %d:\n got  %q\n want %q", c.fixture, i, got[i], want[i])
				break
			}
		}
	}
}

func withFastRewind(t *testing.T) {
	t.Helper()
	tick, step, open, done, draft := rewindTick, rewindStepWait, rewindOpenWait, rewindDoneWait, rewindDraftWait
	rewindTick, rewindStepWait, rewindOpenWait, rewindDoneWait, rewindDraftWait = time.Millisecond, 60*time.Millisecond, 400*time.Millisecond, 400*time.Millisecond, 200*time.Millisecond
	t.Cleanup(func() {
		rewindTick, rewindStepWait, rewindOpenWait, rewindDoneWait, rewindDraftWait = tick, step, open, done, draft
	})
}

func rewindNode(t *testing.T, sim *claudeSim, agent, status string) *Node {
	t.Helper()
	withFastRewind(t)
	a := host.Agent{PaneID: "w1:p1", Agent: agent, AgentStatus: status}
	return &Node{hosts: herdrHosts(sim.serve(t)), agents: map[string]host.Agent{"s1": a}, state: newState()}
}

func rewindCode(t *testing.T, err error) string {
	t.Helper()
	if err == nil {
		t.Fatal("no error")
	}
	code, _ := refusalOf(t, err)
	return code
}

func threePrompts() []simPrompt {
	return []simPrompt{
		{"Append the line \"alpha\" to notes.txt. Reply with only: done", noCode},
		{"Use the Edit tool (not Bash) to replace the line \"alpha\" in notes.txt with \"beta\". Reply with only: ok2", edit},
		{"Reply with only: ok5", noCode},
	}
}

func after(n int) *int { return &n }

// The last prompt, conversation only: /rewind typed and opened, one Up, enter on the prompt, enter on "Restore
// conversation" (the first choice of a prompt with no code changes after it). Claude puts the prompt back in its
// box; the node empties it for the app's own.
func TestRewindConversationToLastPrompt(t *testing.T) {
	sim := newClaudeSim(threePrompts()...)
	n := rewindNode(t, sim, "claude", proto.StatusIdle)
	res, err := n.rewind(context.Background(), "s1", rewindOpts{Prompt: "Reply with only: ok5", Occurrence: 1, PromptsAfter: after(0), Restore: RestoreConversation})
	if err != nil {
		t.Fatal(err)
	}
	if res["prompt"] != "Reply with only: ok5" || res["restore"] != RestoreConversation || res["code_restored"] != false || res["input_cleared"] != true {
		t.Fatalf("result %v", res)
	}
	if got := sim.pressed(); got != "enter up enter enter ctrl+k ctrl+u" {
		t.Fatalf("keys %q", got)
	}
	if sim.rewound != 2 || len(sim.prompts) != 2 || sim.restored != "Restore conversation" || sim.box != "" || sim.mode != "idle" {
		t.Fatalf("pane: rewound %d prompts %d %q box %q mode %s", sim.rewound, len(sim.prompts), sim.restored, sim.box, sim.mode)
	}
	if strings.Join(sim.text, "") != "/rewind" {
		t.Fatalf("typed %q", sim.text)
	}
}

// The panel can close before Claude puts the rewound prompt back. The rewind must not answer during that temporarily
// empty box: a client sending immediately would otherwise have its text prepended by the late restore (#4609).
func TestRewindWaitsForALateRestoredPrompt(t *testing.T) {
	for _, c := range []struct {
		name           string
		prompts        []simPrompt
		prompt         string
		withTranscript bool
	}{
		{"last of several", threePrompts(), "Reply with only: ok5", false},
		{"only prompt", []simPrompt{{"the only prompt", noCode}}, "the only prompt", true},
	} {
		t.Run(c.name, func(t *testing.T) {
			sim := newClaudeSim(c.prompts...)
			sim.restoreGate = make(chan struct{})
			sim.restoreSeenEmpty = make(chan struct{})
			empty := sim.restoreSeenEmpty
			n := rewindNode(t, sim, "claude", proto.StatusIdle)
			var f *transcript.File
			var transcriptPath string
			var transcriptLines []string
			if c.withTranscript {
				transcriptLines = []string{
					`{"type":"user","uuid":"u1","parentUuid":null,"timestamp":"2026-11-05T10:00:01Z","message":{"role":"user","content":"the only prompt"}}`,
					`{"type":"assistant","uuid":"a1","parentUuid":"u1","timestamp":"2026-11-05T10:00:02Z","message":{"role":"assistant","id":"m1","content":[{"type":"text","text":"old answer"}]}}`,
					`{"type":"system","subtype":"turn_duration","uuid":"t1","parentUuid":"a1","timestamp":"2026-11-05T10:00:03Z","durationMs":1000}`,
				}
				transcriptPath = filepath.Join(t.TempDir(), "only.jsonl")
				if err := os.WriteFile(transcriptPath, []byte(strings.Join(transcriptLines, "\n")+"\n"), 0o600); err != nil {
					t.Fatal(err)
				}
				f = transcript.NewFile(transcriptPath)
				if _, err := f.Refresh(); err != nil {
					t.Fatal(err)
				}
				n.files = map[string]*transcript.File{"s1": f}
				n.watches = map[string]*watch{}
				n.metas = map[string]quickMeta{}
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			type answer struct {
				res map[string]any
				err error
			}
			answered := make(chan answer, 1)
			go func() {
				res, err := n.rewind(ctx, "s1", rewindOpts{Prompt: c.prompt, Occurrence: 1, PromptsAfter: after(0), Restore: RestoreConversation})
				answered <- answer{res, err}
			}()
			released, finished := false, false
			defer func() {
				cancel()
				if !released {
					close(sim.restoreGate)
				}
				if !finished {
					select {
					case <-answered:
					case <-time.After(2 * time.Second):
						t.Error("the rewind goroutine did not stop after cleanup")
					}
				}
			}()
			select {
			case <-empty:
			case <-time.After(2 * time.Second):
				t.Fatal("the rewind never read the empty box after confirming")
			}
			select {
			case got := <-answered:
				finished = true
				t.Fatalf("answered before the restored prompt arrived: result %v, error %v", got.res, got.err)
			case <-time.After(20 * time.Millisecond):
			}
			close(sim.restoreGate)
			released = true
			var got answer
			select {
			case got = <-answered:
				finished = true
			case <-time.After(2 * time.Second):
				t.Fatal("the rewind did not answer after the prompt was restored")
			}
			if got.err != nil || got.res["input_cleared"] != true {
				t.Fatalf("result %v, error %v", got.res, got.err)
			}
			if sim.box != "" || !strings.Contains(sim.pressed(), "ctrl+k ctrl+u") {
				t.Fatalf("restored prompt was not removed: box %q, keys %q", sim.box, sim.pressed())
			}
			followUp := "new message"
			if f != nil {
				followUp = c.prompt
			}
			if err := n.sendText(context.Background(), "w1:p1", followUp); err != nil {
				t.Fatal(err)
			}
			if sim.box != followUp {
				t.Fatalf("quick follow-up was combined with the old prompt: %q", sim.box)
			}
			if f != nil {
				items, _, rewound := f.Page(0, 100)
				if len(items) != 0 {
					t.Fatalf("the only turn survived Node.rewind: %v", items)
				}
				// Claude writes the same prompt again as another root with an empty parent after the rewind.
				transcriptLines = append(transcriptLines,
					`{"type":"user","uuid":"u2","parentUuid":null,"timestamp":"2026-11-05T10:01:01Z","message":{"role":"user","content":"the only prompt"}}`,
					`{"type":"assistant","uuid":"a2","parentUuid":"u2","timestamp":"2026-11-05T10:01:02Z","message":{"role":"assistant","id":"m2","content":[{"type":"text","text":"new answer"}]}}`,
					`{"type":"system","subtype":"turn_duration","uuid":"t2","parentUuid":"a2","timestamp":"2026-11-05T10:01:03Z","durationMs":1000}`,
				)
				if err := os.WriteFile(transcriptPath, []byte(strings.Join(transcriptLines, "\n")+"\n"), 0o600); err != nil {
					t.Fatal(err)
				}
				if err := n.refresh("s1", f); err != nil {
					t.Fatal(err)
				}
				items, _, after := f.Page(0, 100)
				var texts []string
				for _, it := range items {
					if it.Text != "" {
						texts = append(texts, it.Text)
					}
				}
				if strings.Join(texts, "|") != "the only prompt|new answer" || after.History != rewound.History {
					t.Fatalf("same-text resend revived the old turn: items %q, history %q -> %q", texts, rewound.History, after.History)
				}
			}
		})
	}
}

// If Claude restores nothing, an empty box is still a successful end state, but only after the ordinary key-effect
// grace. The grace shares the existing draft budget rather than extending the rewind.
func TestRewindWaitsForEmptyBoxGraceWhenNothingIsRestored(t *testing.T) {
	sim := newClaudeSim(threePrompts()...)
	sim.restoreNothing = true
	n := rewindNode(t, sim, "claude", proto.StatusIdle)
	started := time.Now()
	res, err := n.rewind(context.Background(), "s1", rewindOpts{Prompt: "Reply with only: ok5", Occurrence: 1, PromptsAfter: after(0), Restore: RestoreConversation})
	if err != nil || res["input_cleared"] != true {
		t.Fatalf("result %v, error %v", res, err)
	}
	if elapsed := time.Since(started); elapsed < rewindStepWait {
		t.Fatalf("accepted the first empty read after %v, before the %v grace", elapsed, rewindStepWait)
	}
	if sim.box != "" || strings.Contains(sim.pressed(), "ctrl+k") {
		t.Fatalf("an empty box was cleared: box %q, keys %q", sim.box, sim.pressed())
	}
}

// An earlier prompt with code changes: code and conversation is the first choice (no arrow keys), conversation alone
// is the second.
func TestRewindPicksTheChoiceByLabel(t *testing.T) {
	const ok2 = "Use the Edit tool (not Bash) to replace the line \"alpha\" in notes.txt with \"beta\". Reply with only: ok2"
	sim := newClaudeSim(threePrompts()...)
	n := rewindNode(t, sim, "claude", proto.StatusIdle)
	res, err := n.rewind(context.Background(), "s1", rewindOpts{Prompt: ok2, Occurrence: 1, PromptsAfter: after(1), Restore: RestoreCodeAndConv})
	if err != nil {
		t.Fatal(err)
	}
	if res["restore"] != RestoreCodeAndConv || res["code_restored"] != true {
		t.Fatalf("result %v", res)
	}
	if got := sim.pressed(); got != "enter up up enter enter ctrl+k ctrl+u" || sim.restored != "Restore code and conversation" || len(sim.prompts) != 1 {
		t.Fatalf("keys %q restored %q prompts %d", got, sim.restored, len(sim.prompts))
	}

	sim = newClaudeSim(threePrompts()...)
	n = rewindNode(t, sim, "claude", proto.StatusIdle)
	res, err = n.rewind(context.Background(), "s1", rewindOpts{Prompt: ok2, Occurrence: 1, PromptsAfter: after(1), Restore: RestoreConversation})
	if err != nil {
		t.Fatal(err)
	}
	if res["code_restored"] != false || sim.restored != "Restore conversation" || sim.pressed() != "enter up up enter down enter ctrl+k ctrl+u" {
		t.Fatalf("keys %q restored %q result %v", sim.pressed(), sim.restored, res)
	}
}

// Code and conversation for a prompt with no code changes after it is the conversation alone, and the result says so.
func TestRewindCodeAndConversationWithoutCodeChanges(t *testing.T) {
	sim := newClaudeSim(threePrompts()...)
	n := rewindNode(t, sim, "claude", proto.StatusIdle)
	res, err := n.rewind(context.Background(), "s1", rewindOpts{Prompt: "Reply with only: ok5", Occurrence: 1, Restore: RestoreCodeAndConv})
	if err != nil {
		t.Fatal(err)
	}
	if res["restore"] != RestoreConversation || res["code_restored"] != false || sim.restored != "Restore conversation" {
		t.Fatalf("result %v restored %q", res, sim.restored)
	}
}

// Of identical prompts the Nth from the end is the one; a long prompt is found by its cut row and checked in full on
// the confirmation screen.
func TestRewindOccurrenceAndLongPrompts(t *testing.T) {
	sim := newClaudeSim(simPrompt{"继续", noCode}, simPrompt{"继续", noCode}, simPrompt{longMsg, noCode}, simPrompt{"继续", noCode})
	n := rewindNode(t, sim, "claude", proto.StatusIdle)
	if _, err := n.rewind(context.Background(), "s1", rewindOpts{Prompt: "继续", Occurrence: 2, PromptsAfter: after(2), Restore: RestoreConversation}); err != nil {
		t.Fatal(err)
	}
	if sim.rewound != 1 || len(sim.prompts) != 1 {
		t.Fatalf("went back to %d, %d prompts left", sim.rewound, len(sim.prompts))
	}

	sim = newClaudeSim(simPrompt{longMsg, noCode}, simPrompt{"继续", noCode})
	n = rewindNode(t, sim, "claude", proto.StatusIdle)
	res, err := n.rewind(context.Background(), "s1", rewindOpts{Prompt: longMsg, Occurrence: 1, PromptsAfter: after(1), Restore: RestoreConversation})
	if err != nil || sim.rewound != 0 || res["input_cleared"] != true {
		t.Fatalf("%v %v rewound %d", res, err, sim.rewound)
	}
}

// Anything that is not what the step needs stops the run with the panel closed and nothing restored.
func TestRewindStopsWhenTheScreenIsNotAsExpected(t *testing.T) {
	const ok2 = "Use the Edit tool (not Bash) to replace the line \"alpha\" in notes.txt with \"beta\". Reply with only: ok2"
	for _, c := range []struct {
		name  string
		setup func(*claudeSim)
		opts  rewindOpts
		code  string
		keys  string
	}{
		{"the list holds more prompts after it than the app saw", nil,
			rewindOpts{Prompt: ok2, Occurrence: 1, PromptsAfter: after(0), Restore: RestoreConversation}, rewindMismatch, "enter up up esc"},
		{"the list holds fewer", nil,
			rewindOpts{Prompt: ok2, Occurrence: 1, PromptsAfter: after(5), Restore: RestoreConversation}, rewindMismatch, "enter up up esc"},
		{"no such prompt: the list ends", nil,
			rewindOpts{Prompt: "never sent", Occurrence: 1, Restore: RestoreConversation}, rewindMismatch, "enter up up up up esc"},
		{"too few identical prompts", nil,
			rewindOpts{Prompt: ok2, Occurrence: 2, Restore: RestoreConversation}, rewindMismatch, "enter up up up up esc"},
		{"the confirmation quotes another message", func(s *claudeSim) { s.quoteOther = "something else entirely" },
			rewindOpts{Prompt: "Reply with only: ok5", Occurrence: 1, Restore: RestoreConversation}, rewindMismatch, "enter up enter esc esc"},
	} {
		sim := newClaudeSim(threePrompts()...)
		if c.setup != nil {
			c.setup(sim)
		}
		n := rewindNode(t, sim, "claude", proto.StatusIdle)
		_, err := n.rewind(context.Background(), "s1", c.opts)
		if got := rewindCode(t, err); got != c.code {
			t.Errorf("%s: code %s (%v)", c.name, got, err)
		}
		if sim.mode != "idle" || sim.rewound != -1 || len(sim.prompts) != 3 || sim.box != "" {
			t.Errorf("%s: pane not left alone: mode %s rewound %d prompts %d box %q", c.name, sim.mode, sim.rewound, len(sim.prompts), sim.box)
		}
		if got := sim.pressed(); got != c.keys {
			t.Errorf("%s: keys %q, want %q", c.name, got, c.keys)
		}
	}
}

// Nothing is pressed while a dialog has the keyboard, the box holds a draft, the session works, or it is not Claude.
func TestRewindRefusals(t *testing.T) {
	opts := rewindOpts{Prompt: "Reply with only: ok5", Occurrence: 1, Restore: RestoreConversation}
	ctx := context.Background()

	sim := newClaudeSim(threePrompts()...)
	sim.mode, sim.cursor = "list", 3 // the panel is already open (at the computer)
	if code := rewindCode(t, func() error {
		_, err := rewindNode(t, sim, "claude", proto.StatusIdle).rewind(ctx, "s1", opts)
		return err
	}()); code != "dialog_open" {
		t.Fatalf("open panel: %s", code)
	}

	sim = newClaudeSim(threePrompts()...)
	sim.box = "half a thought"
	n := rewindNode(t, sim, "claude", proto.StatusIdle)
	_, err := n.rewind(ctx, "s1", opts)
	if code, data := refusalOf(t, err); code != "input_draft" || data["draft"] != "half a thought" {
		t.Fatalf("draft: %s %v", code, data)
	}

	for name, c := range map[string]struct{ agent, status, code string }{
		"working": {"claude", proto.StatusWorking, rewindBusy},
		"blocked": {"claude", proto.StatusBlocked, rewindBusy},
		"codex":   {"codex", proto.StatusIdle, rewindUnsupported},
	} {
		s := newClaudeSim(threePrompts()...)
		_, err := rewindNode(t, s, c.agent, c.status).rewind(ctx, "s1", opts)
		if got := rewindCode(t, err); got != c.code {
			t.Errorf("%s: %s", name, got)
		}
		if s.pressed() != "" || len(s.text) != 0 {
			t.Errorf("%s: pressed %q typed %q", name, s.pressed(), s.text)
		}
	}
	if sim.pressed() != "" {
		t.Fatalf("a refusal pressed %q", sim.pressed())
	}

	// Not a session of this machine.
	n = rewindNode(t, newClaudeSim(), "claude", proto.StatusIdle)
	if code := rewindCode(t, func() error { _, err := n.rewind(ctx, "gone", opts); return err }()); code != rewindUnsupported {
		t.Fatalf("unknown session: %s", code)
	}
	// Bad parameters never reach the pane.
	for _, bad := range []rewindOpts{{Occurrence: 1, Restore: RestoreConversation}, {Prompt: "x", Restore: RestoreConversation}, {Prompt: "x", Occurrence: 1, Restore: "summary"}} {
		if _, err := n.rewind(ctx, "s1", bad); err == nil || !strings.Contains(err.Error(), "bad params") {
			t.Errorf("%+v: %v", bad, err)
		}
	}
}

func TestRewindNothingToRewindTo(t *testing.T) {
	sim := newClaudeSim()
	n := rewindNode(t, sim, "claude", proto.StatusIdle)
	_, err := n.rewind(context.Background(), "s1", rewindOpts{Prompt: "x", Occurrence: 1, Restore: RestoreConversation})
	if code := rewindCode(t, err); code != rewindNothing || sim.mode != "idle" || sim.pressed() != "enter esc" {
		t.Fatalf("%s mode %s keys %q", code, sim.mode, sim.pressed())
	}
}

// When the final enter changes nothing (the panel stays), nothing more is pressed: the rewind may or may not have
// happened and Esc would not undo it.
func TestRewindPanelStaysOpenAfterConfirm(t *testing.T) {
	sim := newClaudeSim(threePrompts()...)
	sim.stuck = true
	n := rewindNode(t, sim, "claude", proto.StatusIdle)
	_, err := n.rewind(context.Background(), "s1", rewindOpts{Prompt: "Reply with only: ok5", Occurrence: 1, Restore: RestoreConversation})
	if code := rewindCode(t, err); code != rewindFailed {
		t.Fatalf("code %s", code)
	}
	if got := sim.pressed(); got != "enter up enter enter" {
		t.Fatalf("keys %q", got)
	}
}

// Claude's input box after the rewind holds something that is not the prompt (typed at the computer meanwhile): it is
// left alone and the app is told the box was not emptied.
func TestRewindLeavesAnotherDraftInTheBox(t *testing.T) {
	sim := newClaudeSim(threePrompts()...)
	sim.boxAfter = "typed meanwhile"
	n := rewindNode(t, sim, "claude", proto.StatusIdle)
	res, err := n.rewind(context.Background(), "s1", rewindOpts{Prompt: "Reply with only: ok5", Occurrence: 1, Restore: RestoreConversation})
	if err != nil || res["input_cleared"] != false {
		t.Fatalf("%v %v", res, err)
	}
	if sim.box != "typed meanwhile" || strings.Contains(sim.pressed(), "ctrl+c") {
		t.Fatalf("box %q keys %q", sim.box, sim.pressed())
	}
}

// Text typed at the computer during the empty-box grace is not the restored prompt either. It ends the wait without
// being cleared, just as a draft already visible when the panel closes does.
func TestRewindLeavesALateOtherDraftInTheBox(t *testing.T) {
	sim := newClaudeSim(threePrompts()...)
	sim.boxAfter = "typed meanwhile"
	sim.restoreGate = make(chan struct{})
	sim.restoreSeenEmpty = make(chan struct{})
	gate, empty := sim.restoreGate, sim.restoreSeenEmpty
	go func() {
		<-empty
		close(gate)
	}()
	n := rewindNode(t, sim, "claude", proto.StatusIdle)
	res, err := n.rewind(context.Background(), "s1", rewindOpts{Prompt: "Reply with only: ok5", Occurrence: 1, Restore: RestoreConversation})
	if err != nil || res["input_cleared"] != false {
		t.Fatalf("%v %v", res, err)
	}
	if sim.box != "typed meanwhile" || strings.Contains(sim.pressed(), "ctrl+c") || strings.Contains(sim.pressed(), "ctrl+k") {
		t.Fatalf("box %q keys %q", sim.box, sim.pressed())
	}
}

// A prompt of several lines comes back as a taller box, which only Ctrl+C empties.
func TestRewindEmptiesATallBoxWithCtrlC(t *testing.T) {
	const multi = "first line\nsecond line"
	sim := newClaudeSim(simPrompt{"hello", noCode}, simPrompt{multi, noCode})
	n := rewindNode(t, sim, "claude", proto.StatusIdle)
	res, err := n.rewind(context.Background(), "s1", rewindOpts{Prompt: multi, Occurrence: 1, PromptsAfter: after(0), Restore: RestoreConversation})
	if err != nil || res["input_cleared"] != true {
		t.Fatalf("%v %v", res, err)
	}
	if got := sim.pressed(); got != "enter up enter enter ctrl+c" || sim.box != "" {
		t.Fatalf("keys %q box %q", got, sim.box)
	}
}

// A rewind that went through leaves the rewound turns out of the session's transcript at once (the file is told; the
// watchers get a batch with the new history token): Claude writes nothing for it until the next message.
func TestRewindLeavesTheRewoundTurnsOutOfTheTranscript(t *testing.T) {
	lines := []string{
		`{"type":"user","uuid":"u1","parentUuid":null,"timestamp":"2026-11-05T10:00:01Z","message":{"role":"user","content":"一"}}`,
		`{"type":"assistant","uuid":"a1","parentUuid":"u1","timestamp":"2026-11-05T10:00:02Z","message":{"role":"assistant","id":"m1","content":[{"type":"text","text":"答一"}]}}`,
		`{"type":"user","uuid":"u2","parentUuid":"a1","timestamp":"2026-11-05T10:00:03Z","message":{"role":"user","content":"二"}}`,
		`{"type":"assistant","uuid":"a2","parentUuid":"u2","timestamp":"2026-11-05T10:00:04Z","message":{"role":"assistant","id":"m2","content":[{"type":"text","text":"答二"}]}}`,
	}
	p := filepath.Join(t.TempDir(), "t.jsonl")
	if err := os.WriteFile(p, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	f := transcript.NewFile(p)
	if _, err := f.Refresh(); err != nil {
		t.Fatal(err)
	}
	n := &Node{files: map[string]*transcript.File{"s1": f}, watches: map[string]*watch{}, metas: map[string]quickMeta{}}
	n.forgetRewound("s1", rewindOpts{Prompt: "二", Occurrence: 1})
	items, _, span := f.Page(0, 100)
	var got []string
	for _, it := range items {
		got = append(got, it.Text)
	}
	if strings.Join(got, "|") != "一|答一" || !strings.HasSuffix(span.History, ".r1") {
		t.Fatalf("items %q history %q", got, span.History)
	}
	// A prompt the transcript does not have changes nothing.
	n.forgetRewound("s1", rewindOpts{Prompt: "没有", Occurrence: 1})
	if items, _, _ := f.Page(0, 100); len(items) != 2 {
		t.Fatalf("%d items", len(items))
	}
}

// The session list's quick read takes the agent's last words off the conversation that is left after a rewind done
// in the terminal: the turn gone back over ended in an API error, and the hub would report that error for the session
// (TurnError, "Model service error") as long as it was the last text.
func TestQuickMetaAfterATerminalRewindDropsTheRewoundError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s.jsonl")
	lines := []string{
		`{"type":"user","uuid":"u1","parentUuid":null,"timestamp":"2026-11-05T12:00:00Z","message":{"role":"user","content":"一"}}`,
		`{"type":"assistant","uuid":"a1","parentUuid":"u1","timestamp":"2026-11-05T12:00:05Z","message":{"id":"m1","role":"assistant","content":[{"type":"text","text":"答一"}]}}`,
		`{"type":"system","subtype":"turn_duration","uuid":"t1","parentUuid":"a1","timestamp":"2026-11-05T12:00:06Z","durationMs":6000}`,
		`{"type":"user","uuid":"u2","parentUuid":"t1","timestamp":"2026-11-05T12:10:00Z","message":{"role":"user","content":"按这个做"}}`,
		`{"type":"assistant","uuid":"k2","parentUuid":"u2","timestamp":"2026-11-05T12:10:05Z","message":{"id":"m2","role":"assistant","content":[{"type":"thinking","thinking":"想一想"}]}}`,
		`{"type":"assistant","uuid":"e2","parentUuid":"k2","timestamp":"2026-11-05T12:10:09Z","isApiErrorMessage":true,"message":{"id":"m3","role":"assistant","model":"<synthetic>","content":[{"type":"text","text":"API Error: the reply ended during reasoning"}]}}`,
		`{"type":"system","subtype":"turn_duration","uuid":"t2","parentUuid":"e2","timestamp":"2026-11-05T12:10:10Z","durationMs":10000}`,
	}
	write := func() os.FileInfo {
		t.Helper()
		if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		st, _ := os.Stat(path)
		return st
	}
	prev := readQuickMeta(path, write())
	if !strings.HasPrefix(prev.lastText, "API Error:") {
		t.Fatalf("before the rewind: %q", prev.lastText)
	}
	// Rewound in the terminal; a while later Claude writes a recap hanging from where the user went back to.
	lines = append(lines, `{"type":"system","subtype":"away_summary","uuid":"s1","parentUuid":"t1","timestamp":"2026-11-05T12:13:00Z","content":"刚才在做的事"}`)
	m := readQuickMetaSince(path, write(), prev)
	if m.lastText != "答一" || !m.lastTextAt.Equal(time.Date(2026, 11, 5, 12, 0, 5, 0, time.UTC)) {
		t.Fatalf("after the rewind: %q at %v", m.lastText, m.lastTextAt)
	}
}

func TestSessionListRewindKeepsVisibleFailureWhenHiddenTurnSucceeded(t *testing.T) {
	const id = "0d000000-0000-4000-8000-000000009999"
	const failure = "API Error: 529 A failed"
	p := filepath.Join(t.TempDir(), id+".jsonl")
	lines := []string{
		`{"type":"user","uuid":"u1","parentUuid":null,"timestamp":"2026-01-01T12:00:00Z","message":{"role":"user","content":"A"}}`,
		`{"type":"assistant","uuid":"e1","parentUuid":"u1","timestamp":"2026-01-01T12:00:01Z","isApiErrorMessage":true,"error":"server_error","message":{"id":"m-e1","model":"<synthetic>","role":"assistant","content":[{"type":"text","text":"API Error: 529 A failed"}]}}`,
		`{"type":"system","subtype":"turn_duration","uuid":"d1","parentUuid":"e1","timestamp":"2026-01-01T12:00:02Z","durationMs":10}`,
		`{"type":"user","uuid":"u2","parentUuid":"d1","timestamp":"2026-01-01T12:01:00Z","message":{"role":"user","content":"B"}}`,
		`{"type":"assistant","uuid":"a2","parentUuid":"u2","timestamp":"2026-01-01T12:01:01Z","message":{"id":"m-a2","model":"claude-opus-5-5","role":"assistant","content":[{"type":"text","text":"B succeeded"}]}}`,
		`{"type":"system","subtype":"turn_duration","uuid":"d2","parentUuid":"a2","timestamp":"2026-01-01T12:01:02Z","durationMs":10}`,
		`{"type":"system","subtype":"away_summary","uuid":"s1","parentUuid":"d1","timestamp":"2026-01-01T12:02:00Z","content":"recap"}`,
	}
	node := func() *Node {
		sim := newClaudeSim(simPrompt{"A", noCode}, simPrompt{"B", noCode})
		n := rewindNode(t, sim, "claude", proto.StatusDone)
		n.agents = map[string]host.Agent{id: n.agents["s1"]}
		n.paths, n.metas, n.bgScans = map[string]string{id: p}, map[string]quickMeta{}, map[string]*bgScan{}
		n.files, n.watches = map[string]*transcript.File{}, map[string]*watch{}
		return n
	}
	lastText := func(n *Node) string {
		t.Helper()
		for _, s := range n.sessionList(context.Background(), false) {
			if s.SessionID == id {
				return s.LastText
			}
		}
		t.Fatal("no session row")
		return ""
	}
	writeLines(t, p, lines)
	if got := lastText(node()); got != failure {
		t.Fatalf("LastText = %q after the successful turn was hidden, want visible failure %q", got, failure)
	}

	// A newer visible tool-only success still retires the old preview: no blanket !rewound behavior.
	lines = append(lines,
		`{"type":"assistant","uuid":"c1","parentUuid":"s1","timestamp":"2026-01-01T12:03:00Z","message":{"id":"m-c1","model":"claude-opus-5-5","role":"assistant","content":[{"type":"tool_use","id":"tool-c1","name":"Bash","input":{"command":"go test ./internal/assistant"}}]}}`,
		`{"type":"system","subtype":"turn_duration","uuid":"d3","parentUuid":"c1","timestamp":"2026-01-01T12:03:01Z","durationMs":10}`)
	writeLines(t, p, lines)
	if got := lastText(node()); got != "" {
		t.Fatalf("LastText = %q after a newer visible tool-only success, want the old preview retired", got)
	}
}

// The quick read leaves lines over 8 KB unparsed; they still take their place in the tree. A prompt with an image (the
// case #4966 was reported on: a 360 KB line) or a long paste is always one, and so is a big tool result in the middle of
// the turn gone back over: without them the rewound turn was not found, or not hidden past the gap.
func TestQuickMetaAfterATerminalRewindOverBigLines(t *testing.T) {
	img := strings.Repeat("A", 20<<10)
	out := strings.Repeat("x", 20<<10)
	head := []string{
		`{"type":"user","uuid":"u1","parentUuid":null,"timestamp":"2026-11-05T12:00:00Z","message":{"role":"user","content":"一"}}`,
		`{"type":"assistant","uuid":"a1","parentUuid":"u1","timestamp":"2026-11-05T12:00:05Z","message":{"id":"m1","role":"assistant","content":[{"type":"text","text":"答一"}]}}`,
		`{"type":"system","subtype":"turn_duration","uuid":"t1","parentUuid":"a1","timestamp":"2026-11-05T12:00:06Z","durationMs":6000}`,
	}
	tail := []string{
		`{"type":"assistant","uuid":"e2","parentUuid":"k2","timestamp":"2026-11-05T12:10:09Z","isApiErrorMessage":true,"message":{"id":"m3","role":"assistant","model":"<synthetic>","content":[{"type":"text","text":"API Error: the reply ended during reasoning"}]}}`,
		`{"type":"system","subtype":"turn_duration","uuid":"t2","parentUuid":"e2","timestamp":"2026-11-05T12:10:10Z","durationMs":10000}`,
	}
	for name, turn := range map[string][]string{
		"an image prompt": {
			`{"type":"user","uuid":"u2","parentUuid":"t1","timestamp":"2026-11-05T12:10:00Z","message":{"role":"user","content":[{"type":"text","text":"[Image #12] 按这个做"},{"type":"image","source":{"type":"base64","media_type":"image/png","data":"` + img + `"}}]}}`,
			`{"type":"assistant","uuid":"k2","parentUuid":"u2","timestamp":"2026-11-05T12:10:05Z","message":{"id":"m2","role":"assistant","content":[{"type":"thinking","thinking":"想一想"}]}}`,
		},
		"a long paste": {
			`{"type":"user","uuid":"u2","parentUuid":"t1","timestamp":"2026-11-05T12:10:00Z","message":{"role":"user","content":"按这个做 ` + out + `"}}`,
			`{"type":"assistant","uuid":"k2","parentUuid":"u2","timestamp":"2026-11-05T12:10:05Z","message":{"id":"m2","role":"assistant","content":[{"type":"thinking","thinking":"想一想"}]}}`,
		},
		"a big tool result": {
			`{"type":"user","uuid":"u2","parentUuid":"t1","timestamp":"2026-11-05T12:10:00Z","message":{"role":"user","content":"按这个做"}}`,
			`{"type":"assistant","uuid":"a2","parentUuid":"u2","timestamp":"2026-11-05T12:10:02Z","message":{"id":"m2","role":"assistant","content":[{"type":"tool_use","id":"tu2","name":"Read","input":{"file_path":"/tmp/x"}}]}}`,
			`{"type":"user","uuid":"r2","parentUuid":"a2","timestamp":"2026-11-05T12:10:03Z","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"tu2","content":"` + out + `"}]},"toolUseResult":{"stdout":"` + out + `"}}`,
			`{"type":"assistant","uuid":"k2","parentUuid":"r2","timestamp":"2026-11-05T12:10:05Z","message":{"id":"m4","role":"assistant","content":[{"type":"thinking","thinking":"想一想"}]}}`,
		},
	} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "s.jsonl")
			lines := append(append(append([]string{}, head...), turn...), tail...)
			write := func() os.FileInfo {
				t.Helper()
				if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
					t.Fatal(err)
				}
				st, _ := os.Stat(path)
				return st
			}
			prev := readQuickMeta(path, write())
			if !strings.HasPrefix(prev.lastText, "API Error:") {
				t.Fatalf("before the rewind: %q", prev.lastText)
			}
			lines = append(lines, `{"type":"system","subtype":"away_summary","uuid":"s1","parentUuid":"t1","timestamp":"2026-11-05T12:13:00Z","content":"刚才在做的事"}`)
			m := readQuickMetaSince(path, write(), prev)
			if m.lastText != "答一" {
				t.Fatalf("after the rewind: %q", m.lastText)
			}
			if m.lastPromptAt != prev.lastPromptAt || m.activity.Before(prev.activity) {
				t.Fatalf("prompt at %v (was %v), activity %v (was %v)", m.lastPromptAt, prev.lastPromptAt, m.activity, prev.activity)
			}
		})
	}
}

// A rewind done from the app leaves the rewound turn out of the session list at once too: Claude writes nothing to the
// file for it, so the quick read (readQuickMetaSince) has to know what the node went back over. Before, the row kept
// the rewound turn's last words (an "API Error: …", from which the hub reports TurnError) until a line was written.
// The rewound prompt may be further back than the quick read's tail (a big tool result in the turn).
func TestAppRewindLeavesTheRewoundTurnOutOfTheSessionList(t *testing.T) {
	const id = "0d000000-0000-4000-8000-000000004966"
	head := []string{
		`{"type":"user","uuid":"u1","parentUuid":null,"timestamp":"2026-11-05T12:00:00Z","message":{"role":"user","content":"一"}}`,
		`{"type":"assistant","uuid":"a1","parentUuid":"u1","timestamp":"2026-11-05T12:00:05Z","message":{"id":"m1","role":"assistant","content":[{"type":"text","text":"答一"}]}}`,
		`{"type":"system","subtype":"turn_duration","uuid":"t1","parentUuid":"a1","timestamp":"2026-11-05T12:00:06Z","durationMs":6000}`,
		`{"type":"user","uuid":"u2","parentUuid":"t1","timestamp":"2026-11-05T12:10:00Z","message":{"role":"user","content":"按这个做"}}`,
	}
	tail := []string{
		`{"type":"assistant","uuid":"e2","parentUuid":"k2","timestamp":"2026-11-05T12:10:09Z","isApiErrorMessage":true,"message":{"id":"m3","role":"assistant","model":"<synthetic>","content":[{"type":"text","text":"API Error: the reply ended during reasoning"}]}}`,
		`{"type":"system","subtype":"turn_duration","uuid":"t2","parentUuid":"e2","timestamp":"2026-11-05T12:10:10Z","durationMs":10000}`,
	}
	big := strings.Repeat("x", quickTail+(16<<10))
	for name, turn := range map[string][]string{
		"in the tail": {
			`{"type":"assistant","uuid":"k2","parentUuid":"u2","timestamp":"2026-11-05T12:10:05Z","message":{"id":"m2","role":"assistant","content":[{"type":"thinking","thinking":"想一想"}]}}`,
		},
		"before the tail": {
			`{"type":"assistant","uuid":"a2","parentUuid":"u2","timestamp":"2026-11-05T12:10:02Z","message":{"id":"m2","role":"assistant","content":[{"type":"tool_use","id":"tu2","name":"Read","input":{"file_path":"/tmp/x"}}]}}`,
			`{"type":"user","uuid":"r2","parentUuid":"a2","timestamp":"2026-11-05T12:10:03Z","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"tu2","content":"` + big + `"}]}}`,
			`{"type":"assistant","uuid":"k2","parentUuid":"r2","timestamp":"2026-11-05T12:10:05Z","message":{"id":"m4","role":"assistant","content":[{"type":"thinking","thinking":"想一想"}]}}`,
		},
	} {
		t.Run(name, func(t *testing.T) {
			p := filepath.Join(t.TempDir(), id+".jsonl")
			lines := append(append(append([]string{}, head...), turn...), tail...)
			if err := os.WriteFile(p, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			sim := newClaudeSim(simPrompt{"一", noCode}, simPrompt{"按这个做", noCode})
			n := rewindNode(t, sim, "claude", proto.StatusIdle)
			n.agents = map[string]host.Agent{id: n.agents["s1"]}
			n.paths, n.metas, n.bgScans = map[string]string{id: p}, map[string]quickMeta{}, map[string]*bgScan{}
			n.files, n.watches = map[string]*transcript.File{}, map[string]*watch{}
			row := func() proto.Session {
				t.Helper()
				for _, s := range n.sessionList(context.Background(), false) {
					if s.SessionID == id {
						return s
					}
				}
				t.Fatal("no row")
				return proto.Session{}
			}
			if s := row(); !strings.HasPrefix(s.LastText, "API Error:") {
				t.Fatalf("before the rewind: %q", s.LastText)
			}
			if _, err := n.rewind(context.Background(), id, rewindOpts{Prompt: "按这个做", Occurrence: 1, PromptsAfter: after(0), Restore: RestoreConversation}); err != nil {
				t.Fatal(err)
			}
			// Nothing written to the file since: the row is read again all the same.
			if s := row(); s.LastText != "答一" || s.TurnError != "" {
				t.Fatalf("after the rewind: %q (turn error %q)", s.LastText, s.TurnError)
			}
		})
	}
}

// A local slash command such as /rename (#5397): Claude Code writes it as system lines (the command, its output) and
// leaves it out of its /rewind list, while the app shows it as one of the user's messages and counts it when it says
// how many came after the one to go back to. The node counts as Claude does.
func TestRewindOverALocalCommand(t *testing.T) {
	const id = "0d000000-0000-4000-8000-000000005397"
	lines := []string{
		`{"type":"user","uuid":"u1","parentUuid":null,"timestamp":"2026-11-06T09:00:00Z","isSidechain":false,"message":{"role":"user","content":"first"}}`,
		`{"type":"assistant","uuid":"a1","parentUuid":"u1","timestamp":"2026-11-06T09:00:05Z","isSidechain":false,"message":{"id":"m1","role":"assistant","content":[{"type":"text","text":"one"}]}}`,
		`{"type":"system","subtype":"turn_duration","uuid":"t1","parentUuid":"a1","timestamp":"2026-11-06T09:00:06Z","isSidechain":false,"durationMs":6000,"isMeta":false}`,
		`{"type":"user","uuid":"u2","parentUuid":"t1","timestamp":"2026-11-06T09:01:00Z","isSidechain":false,"message":{"role":"user","content":"second"}}`,
		`{"type":"assistant","uuid":"a2","parentUuid":"u2","timestamp":"2026-11-06T09:01:05Z","isSidechain":false,"message":{"id":"m2","role":"assistant","content":[{"type":"text","text":"two"}]}}`,
		`{"type":"system","subtype":"turn_duration","uuid":"t2","parentUuid":"a2","timestamp":"2026-11-06T09:01:06Z","isSidechain":false,"durationMs":6000,"isMeta":false}`,
		// /rename as Claude Code 2.1.309 writes it: two local_command system lines and a meta note for the model.
		`{"type":"system","subtype":"local_command","uuid":"c1","parentUuid":"t2","timestamp":"2026-11-06T09:02:00Z","isSidechain":false,"content":"<command-name>/rename</command-name>\n            <command-message>rename</command-message>\n            <command-args>release notes</command-args>","level":"info","isMeta":false,"userType":"external","entrypoint":"cli","version":"2.1.309"}`,
		`{"type":"system","subtype":"local_command","uuid":"c2","parentUuid":"c1","timestamp":"2026-11-06T09:02:00Z","isSidechain":false,"content":"<local-command-stdout>Session renamed to: release notes</local-command-stdout>","level":"info","isMeta":false,"commandRun":{"command":"rename","args":"release notes"},"userType":"external","entrypoint":"cli","version":"2.1.309"}`,
		`{"type":"user","uuid":"c3","parentUuid":"c2","timestamp":"2026-11-06T09:02:00Z","isSidechain":false,"isMeta":true,"message":{"role":"user","content":"<system-reminder>The user named this session \"release notes\". This may indicate the session's focus or intent.</system-reminder>"},"userType":"external","entrypoint":"cli","version":"2.1.309"}`,
		`{"type":"user","uuid":"u3","parentUuid":"c3","timestamp":"2026-11-06T09:03:00Z","isSidechain":false,"message":{"role":"user","content":"third"}}`,
		`{"type":"assistant","uuid":"a3","parentUuid":"u3","timestamp":"2026-11-06T09:03:05Z","isSidechain":false,"message":{"id":"m3","role":"assistant","content":[{"type":"text","text":"three"}]}}`,
		`{"type":"system","subtype":"turn_duration","uuid":"t3","parentUuid":"a3","timestamp":"2026-11-06T09:03:06Z","isSidechain":false,"durationMs":6000,"isMeta":false}`,
	}
	setup := func(t *testing.T) (*Node, *claudeSim) {
		t.Helper()
		p := filepath.Join(t.TempDir(), id+".jsonl")
		if err := os.WriteFile(p, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		// Claude's own list: the three messages, not /rename.
		sim := newClaudeSim(simPrompt{"first", noCode}, simPrompt{"second", noCode}, simPrompt{"third", noCode})
		n := rewindNode(t, sim, "claude", proto.StatusIdle)
		n.agents = map[string]host.Agent{id: n.agents["s1"]}
		n.paths, n.metas, n.bgScans = map[string]string{id: p}, map[string]quickMeta{}, map[string]*bgScan{}
		n.files, n.watches = map[string]*transcript.File{}, map[string]*watch{}
		return n, sim
	}

	// The app's count: second, then /rename and third after it.
	t.Run("to a message before the command", func(t *testing.T) {
		n, sim := setup(t)
		res, err := n.rewind(context.Background(), id, rewindOpts{Prompt: "second", Occurrence: 1, PromptsAfter: after(2), Restore: RestoreConversation})
		if err != nil {
			t.Fatal(err)
		}
		if res["prompt"] != "second" || sim.rewound != 1 {
			t.Fatalf("rewound to %d: %v", sim.rewound, res)
		}
	})
	t.Run("to a message after it", func(t *testing.T) {
		n, sim := setup(t)
		if _, err := n.rewind(context.Background(), id, rewindOpts{Prompt: "third", Occurrence: 1, PromptsAfter: after(0), Restore: RestoreConversation}); err != nil {
			t.Fatal(err)
		}
		if sim.rewound != 2 {
			t.Fatalf("rewound to %d", sim.rewound)
		}
	})
	t.Run("to the first one", func(t *testing.T) {
		n, sim := setup(t)
		if _, err := n.rewind(context.Background(), id, rewindOpts{Prompt: "first", Occurrence: 1, PromptsAfter: after(3), Restore: RestoreConversation}); err != nil {
			t.Fatal(err)
		}
		if sim.rewound != 0 {
			t.Fatalf("rewound to %d", sim.rewound)
		}
	})
	// A client that already counts as Claude does (leaves /rename out) is not miscounted.
	t.Run("counted as Claude does", func(t *testing.T) {
		n, sim := setup(t)
		if _, err := n.rewind(context.Background(), id, rewindOpts{Prompt: "second", Occurrence: 1, PromptsAfter: after(1), Restore: RestoreConversation}); err != nil {
			t.Fatal(err)
		}
		if sim.rewound != 1 {
			t.Fatalf("rewound to %d", sim.rewound)
		}
	})
	// A count that is off for another reason is still refused, and nothing is restored.
	for name, o := range map[string]rewindOpts{
		"too many after":        {Prompt: "second", Occurrence: 1, PromptsAfter: after(4)},
		"too few after":         {Prompt: "second", Occurrence: 1, PromptsAfter: after(0)},
		"the command itself":    {Prompt: "/rename release notes", Occurrence: 1, PromptsAfter: after(1)},
		"not in the transcript": {Prompt: "fourth", Occurrence: 1, PromptsAfter: after(0)},
	} {
		t.Run(name, func(t *testing.T) {
			n, sim := setup(t)
			o.Restore = RestoreConversation
			if code := rewindCode(t, func() error { _, err := n.rewind(context.Background(), id, o); return err }()); code != rewindMismatch {
				t.Fatalf("code %q", code)
			}
			if sim.rewound != -1 {
				t.Fatalf("rewound to %d", sim.rewound)
			}
		})
	}
}
