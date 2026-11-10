package transcript

import (
	"strings"
	"testing"
)

// longReport is a report the agent writes before a tool call: words for the user, not narration.
var longReport = "今天的进展：\n\n" + strings.Repeat("第一项已经合并，第二项在等 CI，第三项需要你决定是否上线。", 4)

func TestNarrationIsAPassingLine(t *testing.T) {
	for _, c := range []struct {
		text string
		want bool
	}{
		{"我先查一下。", true},
		{"Let me check the calendar.", true},
		{"先看看日历，再看看待办。", true},
		{"Checking v1.2 on example.com now", true},
		{longReport, false},
		{"第一句。第二句。第三句。", false},
		{"One. Two. Three.", false},
		{"- 合并了 A\n- 合并了 B", false},
		{"## 结论", false},
		{"1. 先做这个", false},
		{"| a | b |", false},
		{"**结论**：可以上线。", false},
		{"（1）先合并 A。", false},
		{"第一段。\n\n第二段。", false},
		{strings.Repeat("长", narrationMost+1), false},
		{"  ", false},
	} {
		if got := Narration(c.text); got != c.want {
			t.Errorf("Narration(%q) = %v, want %v", c.text, got, c.want)
		}
	}
}

func TestAnswerPartsKeepWordsForTheUser(t *testing.T) {
	parts := []AnswerPart{
		{Text: "我先查一下。", BeforeTool: true},
		{Text: longReport, BeforeTool: true},
		{Text: "已经发给它了。"},
	}
	if got := AnswerParts(parts); len(got) != 2 || got[0] != 1 || got[1] != 2 {
		t.Fatalf("AnswerParts = %v, want [1 2]", got)
	}
	// The last part stays even when it is a passing line before a call (the turn is still running).
	if got := AnswerParts(parts[:1]); len(got) != 1 || got[0] != 0 {
		t.Fatalf("AnswerParts(last only) = %v, want [0]", got)
	}
	// Words not followed by a tool call are kept however short.
	if got := AnswerParts([]AnswerPart{{Text: "好。"}, {Text: "完成。"}}); len(got) != 2 {
		t.Fatalf("AnswerParts(no tools) = %v, want both", got)
	}
}

// The case reported: a long report, an agent_send call, a short closing line. LastText is the closing line; the
// turn's answer starts with the report.
func TestTurnAnswerLongTextToolShortText(t *testing.T) {
	meta := &Meta{}
	for i, line := range []string{
		`{"type":"user","uuid":"u1","message":{"role":"user","content":"汇报一下"}}`,
		`{"type":"assistant","uuid":"a1","parentUuid":"u1","message":{"role":"assistant","content":[{"type":"text","text":"我先查一下。"},{"type":"tool_use","id":"t0","name":"Bash","input":{}}]}}`,
		`{"type":"user","uuid":"u2","parentUuid":"a1","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"t0","content":"ok"}]}}`,
		`{"type":"assistant","uuid":"a2","parentUuid":"u2","message":{"role":"assistant","content":[{"type":"text","text":` + quote(longReport) + `},{"type":"tool_use","id":"t1","name":"mcp__persona__agent_send","input":{}}]}}`,
		`{"type":"user","uuid":"u3","parentUuid":"a2","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"t1","content":"sent"}]}}`,
		`{"type":"assistant","uuid":"a3","parentUuid":"u3","message":{"role":"assistant","content":[{"type":"text","text":"已经发给它了。"}]}}`,
	} {
		ParseLine(i+1, []byte(line), meta)
	}
	if meta.LastText != "已经发给它了。" {
		t.Fatalf("LastText = %q", meta.LastText)
	}
	want := strings.TrimSpace(longReport) + "\n\n已经发给它了。"
	if got := meta.TurnAnswer(); got != want {
		t.Fatalf("TurnAnswer = %q, want %q", got, want)
	}
	// The next message starts a new turn: the report is the last turn's, not this one's.
	ParseLine(7, []byte(`{"type":"user","uuid":"u4","parentUuid":"a3","message":{"role":"user","content":"好的"}}`), meta)
	ParseLine(8, []byte(`{"type":"assistant","uuid":"a4","parentUuid":"u4","message":{"role":"assistant","content":[{"type":"text","text":"收到。"}]}}`), meta)
	if got := meta.TurnAnswer(); got != "收到。" {
		t.Fatalf("next turn's answer = %q", got)
	}
	// A photo with no caption starts a turn too.
	ParseLine(9, []byte(`{"type":"user","uuid":"u5","parentUuid":"a4","message":{"role":"user","content":[{"type":"image","source":{"type":"base64","media_type":"image/png","data":"iVBORw0KGgo="}}]}}`), meta)
	ParseLine(10, []byte(`{"type":"assistant","uuid":"a5","parentUuid":"u5","message":{"role":"assistant","content":[{"type":"text","text":"是一只猫。"}]}}`), meta)
	if got := meta.TurnAnswer(); got != "是一只猫。" {
		t.Fatalf("answer after a photo = %q", got)
	}
}

// With none of the turn's words read, or words that disagree with LastText, TurnAnswer is LastText.
func TestTurnAnswerFallsBackToLastText(t *testing.T) {
	m := &Meta{LastText: "earlier words"}
	if got := m.TurnAnswer(); got != "earlier words" {
		t.Fatalf("no parts: %q", got)
	}
	m.turnParts = []AnswerPart{{Text: "a rewound reply"}}
	if got := m.TurnAnswer(); got != "earlier words" {
		t.Fatalf("disagreeing parts: %q", got)
	}
}
