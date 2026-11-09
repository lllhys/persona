package screen

import (
	"slices"
	"strconv"
	"strings"
	"testing"
)

const rewindPrefix = "claude-2.1.318-rewind-"

func rewindFixture(t *testing.T, name string) *RewindScreen {
	t.Helper()
	r := ParseRewind(fixture(t, rewindPrefix+name+"-119.txt"))
	if r == nil {
		t.Fatalf("%s: no /rewind panel found", name)
	}
	return r
}

// The list as Claude Code draws it, recorded in a throwaway session with four prompts (the last two tests below use
// others): the cursor starts on (current); a window of two rows scrolls with ↑ / ↓ markers.
func TestParseRewindList(t *testing.T) {
	cur := rewindFixture(t, "list-current")
	if cur.Kind != RewindList || cur.Above != 2 || cur.Below != 0 || len(cur.Rows) != 2 {
		t.Fatalf("list-current: %+v", cur)
	}
	if last := cur.Rows[1]; !last.Current || !last.Selected || last.Text != "(current)" {
		t.Fatalf("the cursor starts on (current): %+v", cur.Rows)
	}
	if first := cur.Rows[0]; first.Text != "Reply with only: ok5" || first.Changes != "No code changes" || first.Selected {
		t.Fatalf("last prompt: %+v", first)
	}

	edits := rewindFixture(t, "list-current-edits")
	if edits.Above != 2 || !strings.HasSuffix(edits.Rows[0].Text, "Reply with only: ok3") || edits.Rows[0].Changes != "notes.txt +1 -1" {
		t.Fatalf("list-current-edits: %+v", edits)
	}

	mid := rewindFixture(t, "list-middle")
	if mid.Above != 1 || mid.Below != 1 || len(mid.Rows) != 2 || mid.Rows[0].Selected || !mid.Rows[1].Selected {
		t.Fatalf("list-middle: %+v", mid)
	}
	if sel := mid.Selected(); sel == nil || !strings.HasSuffix(sel.Text, "ok3") {
		t.Fatalf("list-middle selected: %+v", sel)
	}

	second := rewindFixture(t, "list-second")
	if second.Above != 0 || second.Below != 2 || second.Rows[0].Selected || !second.Rows[1].Selected {
		t.Fatalf("list-second: %+v", second)
	}

	top := rewindFixture(t, "list-top")
	if top.Above != 0 || top.Below != 2 || !top.Rows[0].Selected || top.Rows[0].Changes != "No code changes" {
		t.Fatalf("list-top: %+v", top)
	}

	chinese := rewindFixture(t, "list-chinese")
	if sel := chinese.Selected(); sel == nil || sel.Text != "用中文只回复：好的" || chinese.Below != 1 {
		t.Fatalf("list-chinese: %+v", chinese)
	}

	long := rewindFixture(t, "list-long")
	sel := long.Selected()
	if sel == nil || !strings.HasSuffix(sel.Text, "…") {
		t.Fatalf("list-long: %+v", long)
	}
}

// The second screen: which choices it offers depends on whether code changed after the prompt.
func TestParseRewindOptions(t *testing.T) {
	code := rewindFixture(t, "options-code")
	if code.Kind != RewindOptions || code.Choice(RewindChoiceBoth) == nil || len(code.Choices) != 5 {
		t.Fatalf("options-code: %+v", code)
	}
	if !strings.HasSuffix(code.Message, "ok2") || strings.Contains(code.Message, "ago") {
		t.Fatalf("message %q", code.Message)
	}
	if both := code.Choice(RewindChoiceBoth); both == nil || both.Number != 1 || !both.Selected {
		t.Fatalf("both: %+v", code.Choices)
	}
	if c := code.Choice(RewindChoiceConversation); c == nil || c.Number != 2 || c.Selected {
		t.Fatalf("conversation: %+v", code.Choices)
	}

	conv := rewindFixture(t, "options-conversation-selected")
	if sel := conv.SelectedChoice(); sel == nil || sel.Label != RewindChoiceConversation {
		t.Fatalf("selected: %+v", conv.Choices)
	}

	last := rewindFixture(t, "options-code-last")
	if sel := last.SelectedChoice(); sel == nil || sel.Number != 5 || !strings.HasPrefix(sel.Label, "Summarize up to here") {
		t.Fatalf("options-code-last: %+v", last.Choices)
	}

	none := rewindFixture(t, "options-nocode")
	if none.Choice(RewindChoiceBoth) != nil || none.Choice(RewindChoiceConversation) == nil || len(none.Choices) != 4 {
		t.Fatalf("options-nocode: %+v", none.Choices)
	}
	if none.Message != "Reply with only: ok5" {
		t.Fatalf("message %q", none.Message)
	}

	// A long prompt is wrapped over several quoted rows.
	long := rewindFixture(t, "options-long")
	if !strings.HasPrefix(long.Message, "Reply with only the word ok6") || !strings.HasSuffix(long.Message, "middle of the sentence.") {
		t.Fatalf("options-long message %q", long.Message)
	}
	listLong := rewindFixture(t, "list-long")
	if !RewindShows(listLong.Selected().Text, long.Message) {
		t.Fatalf("the cut row %q is the start of %q", listLong.Selected().Text, long.Message)
	}
}

// 2.1.304's wording ("The conversation will be forked") has the same panel and choices.
func TestParseRewindOlderVersion(t *testing.T) {
	r := ParseRewind(fixture(t, "claude-rewind-2.1.304-confirm-apple-restore-120.txt"))
	if r == nil || r.Kind != RewindOptions || r.Message != "apple" || r.Choice(RewindChoiceConversation) == nil || r.Choice(RewindChoiceBoth) != nil {
		t.Fatalf("%+v", r)
	}
}

func TestParseRewindNothingAndNotRewind(t *testing.T) {
	if r := rewindFixture(t, "nothing"); r.Kind != RewindNothing {
		t.Fatalf("%+v", r)
	}
	// After the panel is closed or the conversation was rewound, and while a session is only idle, no panel shows —
	// including when an earlier Rewind panel is part of the scrollback text above the input box.
	for _, name := range []string{"cancelled", "result-conversation", "result-code", "idle-before", "multiline-draft", "after-ctrl-c"} {
		if r := ParseRewind(fixture(t, rewindPrefix+name+"-119.txt")); r != nil {
			t.Errorf("%s: found a panel: %+v", name, r)
		}
	}
}

func TestRewindShows(t *testing.T) {
	for _, c := range []struct {
		shown, prompt string
		want          bool
	}{
		{"fix the flaky test", "fix the flaky test", true},
		{"fix  the flaky\ntest", "fix the flaky test", true},
		{"fix the flaky test", "fix the flaky tes", false},
		{"fix the flaky test", "fix the flaky test please", false},
		{"fix the fl…", "fix the flaky test please", true},
		{"fix the fl…", "fix the other test", false},
		{"…", "anything", false},
		{"", "", false},
		{"用中文只回复：好的", "用中文只回复： 好的", true},
	} {
		if got := RewindShows(c.shown, c.prompt); got != c.want {
			t.Errorf("RewindShows(%q, %q) = %v, want %v", c.shown, c.prompt, got, c.want)
		}
	}
}

// What Claude's input box holds once the prompt was put back: the recorded boxes, plain and with colors.
func TestRewindDraftIs(t *testing.T) {
	in := ClaudeInput(fixture(t, rewindPrefix+"result-conversation-119.ansi"))
	prompt := `Use the Edit tool (not Bash) to replace the line "beta" in notes.txt with "gamma". Reply with only: ok3`
	if !in.Draft || !RewindDraftIs(in.Text, prompt) {
		t.Fatalf("composer after the rewind: %+v", in)
	}
	if RewindDraftIs(in.Text, "something else") || RewindDraftIs("", prompt) {
		t.Fatal("another text is not the prompt")
	}
	if !RewindDraftIs("[Pasted text #1 +30 lines]", prompt) {
		t.Fatal("a paste placeholder stands for the prompt's pasted part")
	}
	multi := ClaudeInput(fixture(t, rewindPrefix+"multiline-draft-119.ansi"))
	if !multi.Draft || !RewindDraftIs(multi.Text, "line one\nline two") {
		t.Fatalf("multi-line draft: %+v", multi)
	}
	if cleared := ClaudeInput(fixture(t, rewindPrefix+"after-ctrl-c-119.ansi")); cleared.Draft || cleared.Text != "" {
		t.Fatalf("after ctrl+c the box is empty: %+v", cleared)
	}
	// The idle session before /rewind is opened: no draft, nothing open.
	if code, _ := CheckClaude(fixture(t, rewindPrefix+"idle-before-119.ansi")); code != "" {
		t.Fatalf("idle session: %q", code)
	}
	// An open /rewind panel is a dialog for the send check.
	for _, name := range []string{"list-middle", "options-code", "options-nocode"} {
		if code, _ := CheckClaude(fixture(t, rewindPrefix+name+"-119.txt")); code != DialogOpen {
			t.Errorf("%s: code %q, want dialog_open", name, code)
		}
	}
}

// The row under each prompt of the /rewind list ("notes.txt +1 -1", "No code changes") is that prompt's detail, not
// an option of its own (#4306): an extra button, and every button under it one prompt too far.
func TestRewindListOptionsArePrompts(t *testing.T) {
	cases := []struct {
		name   string
		labels []string
		detail []string
		keys   [][]string
		cursor string
	}{
		{"list-middle", []string{`Use the Edit tool (not Bash) to replace the line "alpha" in notes.txt with "beta". Reply with only: ok2`,
			`Use the Edit tool (not Bash) to replace the line "beta" in notes.txt with "gamma". Reply with only: ok3`},
			[]string{"notes.txt +1 -1", "notes.txt +1 -1"}, [][]string{{"up", "enter"}, {"enter"}}, "2"},
		{"list-top", []string{`Append the line "alpha" to notes.txt. Reply with only: done`,
			`Use the Edit tool (not Bash) to replace the line "alpha" in notes.txt with "beta". Reply with only: ok2`},
			[]string{"No code changes", "notes.txt +1 -1"}, [][]string{{"enter"}, {"down", "enter"}}, "1"},
		{"list-chinese", []string{"Reply with only the word ok6, ignoring all of this padding text which only exists to make the prompt much lo…",
			"用中文只回复：好的"}, []string{"No code changes", "No code changes"}, [][]string{{"up", "enter"}, {"enter"}}, "2"},
	}
	for _, c := range cases {
		d := ParseDialog(fixture(t, rewindPrefix+c.name+"-119.txt"))
		if d == nil {
			t.Fatalf("%s: not read as a dialog", c.name)
		}
		if d.Question != rewindListQuestion || d.Cursor != c.cursor || len(d.Options) != len(c.labels) {
			t.Fatalf("%s: %q cursor %q %+v", c.name, d.Question, d.Cursor, d.Options)
		}
		for i, o := range d.Options {
			if o.Key != strconv.Itoa(i+1) || o.Label != c.labels[i] || o.Detail != c.detail[i] || !slices.Equal(o.Keys, c.keys[i]) {
				t.Errorf("%s: option %d: %+v", c.name, i+1, o)
			}
		}
	}
	// The cursor moved on the computer, the window did not: the same dialog (a card stays answerable, its keys
	// worked out again). The window scrolled: another one, even when the rows on screen read the same.
	rev := func(name string) string { return ParseDialog(fixture(t, rewindPrefix+name+"-119.txt")).Rev }
	if rev("list-top") != rev("list-second") || rev("list-long") != rev("list-chinese") {
		t.Error("the cursor moving changed Rev")
	}
	scrolled := strings.Replace(fixture(t, rewindPrefix+"list-top-119.txt"), "↓ 2 more", "↓ 1 more", 1)
	if scrolled == fixture(t, rewindPrefix+"list-top-119.txt") {
		t.Fatal("list-top has no ↓ 2 more row")
	}
	scrolled = strings.Replace(scrolled, "Restore the code and/or conversation to the point before…\n", "Restore the code and/or conversation to the point before…\n\n     ↑ 1 more", 1)
	if d := ParseDialog(scrolled); d == nil || len(d.Options) != 2 || d.Rev == rev("list-top") {
		t.Errorf("a scrolled window kept Rev: %+v", d)
	}
	// With the cursor on (current) there is no prompt to pick: nothing read, as before.
	for _, name := range []string{"list-current", "list-current-edits"} {
		if d := ParseDialog(fixture(t, rewindPrefix+name+"-119.txt")); d != nil {
			t.Errorf("%s: read as a dialog: %+v", name, d.Options)
		}
	}
	// A /rewind list left in the conversation above a live dialog is not that dialog.
	stale := strings.Join([]string{"   Rewind", "", "   Restore the code and/or conversation to the point before…", "",
		"   ❯ first prompt", "     No code changes", "", "     second prompt", "     notes.txt +1 -1", "",
		"   Enter to continue · Esc to cancel", "", "● done", "", strings.Repeat("─", 40),
		" Do you trust the files in this folder?", "", " ❯ Yes, proceed", "   No, exit", "", " Enter to confirm · Esc to cancel"}, "\n")
	if d := ParseDialog(stale); d == nil || d.Question == rewindListQuestion || len(d.Options) != 2 || d.Options[0].Label != "Yes, proceed" {
		t.Errorf("stale /rewind list: %+v", d)
	}
}
