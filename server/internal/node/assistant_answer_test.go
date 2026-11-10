package node

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The push for a finished main-chat turn carries the conversation row's LastText (assistant.fleet_push.go doneAlert).
// A turn that wrote a long report, called agent_send and closed with a short line pushes from the report's first
// words, cut to the snippet's length, not just the closing line.
func TestAssistantRowSaysTheTurnsAnswerFromItsFirstWords(t *testing.T) {
	t.Setenv("PERSONA_HOME", "")
	n, _ := claudeHome(t)
	n.metas, n.bgScans = map[string]quickMeta{}, map[string]*bgScan{}
	q := func(s string) string { b, _ := json.Marshal(s); return string(b) }
	report := "今天的进展：第一项已经合并，" + strings.Repeat("第二项在等 CI，第三项需要你决定是否上线。", 12)
	dir := filepath.Join(n.home, ".claude", "projects", n.assistantProject())
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	body := strings.Join([]string{
		`{"type":"user","uuid":"u1","timestamp":"2026-11-10T14:50:00Z","message":{"role":"user","content":"汇报一下"}}`,
		`{"type":"assistant","uuid":"a1","parentUuid":"u1","timestamp":"2026-11-10T14:54:00Z","message":{"role":"assistant","content":[{"type":"text","text":` + q(report) + `},{"type":"tool_use","id":"t1","name":"mcp__persona__agent_send","input":{}}]}}`,
		`{"type":"user","uuid":"u2","parentUuid":"a1","timestamp":"2026-11-10T14:54:01Z","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"t1","content":"sent"}]}}`,
		`{"type":"assistant","uuid":"a2","parentUuid":"u2","timestamp":"2026-11-10T14:54:02Z","message":{"role":"assistant","content":[{"type":"text","text":"已经发给它了。"}]}}`,
	}, "\n") + "\n"
	p := filepath.Join(dir, "dddddddd-0000-0000-0000-000000000001.jsonl")
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	st, err := os.Stat(p)
	if err != nil {
		t.Fatal(err)
	}
	m := n.meta(p, st)
	if m.lastText != "已经发给它了。" {
		t.Fatalf("lastText %q", m.lastText)
	}
	row := snippet(assistantAnswer(m)) // what assistantSessions puts in LastText
	if !strings.HasPrefix(row, "今天的进展：第一项已经合并，") || !strings.HasSuffix(row, "…") {
		t.Fatalf("row %q: want the report's first words, cut", row)
	}
	// A short answer is whole: the report and the closing line.
	m.answer = "第一项已经合并。\n\n已经发给它了。"
	if row := snippet(assistantAnswer(m)); row != "第一项已经合并。 已经发给它了。" {
		t.Fatalf("short row %q", row)
	}
	// An error notice the turn ended on stays the row (the push marks it ⚠️ by its start).
	m.lastText = "API Error: overloaded"
	if row := snippet(assistantAnswer(m)); row != "API Error: overloaded" {
		t.Fatalf("error row %q", row)
	}
}
