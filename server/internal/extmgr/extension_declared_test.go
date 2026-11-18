package extmgr

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A declarative kit: its quick prompt, theme and triage template are listed for the apps, its skill and instructions go
// to the assistant; switched off one by one, each leaves.
func TestDeclarativeKit(t *testing.T) {
	h, _ := extensionTestHub(t)
	mux := h.routes()
	master := h.cfg.Token
	dir := declarativePackage(t, `{"manifest_version": 1, "id": "kit", "name": "Kit", "version": "1.0.0", "contributes": {
		"skills": [{"id": "review", "dir": "skills/review"}],
		"prompts": [{"id": "tone", "kind": "instructions", "file": "assistant/tone.md"},
			{"id": "today", "kind": "quick", "title": "今天", "text": "今天做了什么？"}],
		"themes": [{"id": "forest", "name": "森林", "light": {"accent": "#2E7D32"}, "dark": {"accent": "#FFEB3B"}},
			{"id": "pale", "name": "淡", "light": {"accent": "#FFF59D"}, "dark": {"accent": "#81C784"}}],
		"triage_templates": [{"id": "quiet", "title": "大群安静", "text": "只看提到我的。"}]}}`)
	_ = os.MkdirAll(filepath.Join(dir, "skills", "review"), 0o755)
	_ = os.WriteFile(filepath.Join(dir, "skills", "review", "SKILL.md"), []byte("# review\n"), 0o644)
	_ = os.MkdirAll(filepath.Join(dir, "assistant"), 0o755)
	_ = os.WriteFile(filepath.Join(dir, "assistant", "tone.md"), []byte("Answer in short sentences.\n"), 0o644)
	installForTest(t, h, dir)

	_, out := call(t, mux, "GET", "/api/v1/extensions/-/prompts", master, "")
	if ps := out["prompts"].([]any); len(ps) != 1 || ps[0].(map[string]any)["title"] != "今天" || ps[0].(map[string]any)["extension_name"] != "Kit" {
		t.Fatalf("%v", out)
	}
	_, out = call(t, mux, "GET", "/api/v1/extensions/-/themes", master, "")
	ths := out["themes"].([]any)
	if len(ths) != 2 || ths[0].(map[string]any)["problems"] != nil || ths[1].(map[string]any)["problems"] == nil {
		t.Fatalf("the pale one fails the contrast check: %v", ths)
	}
	_, out = call(t, mux, "GET", "/api/v1/extensions/-/triage-templates", master, "")
	if ts := out["templates"].([]any); len(ts) != 1 || ts[0].(map[string]any)["text"] != "只看提到我的。" {
		t.Fatalf("%v", out)
	}
	skills := h.extensions.extensionSkills()
	if len(skills) != 1 || skills[0].Name != "review" || string(skills[0].Files["SKILL.md"]) != "# review\n" {
		t.Fatalf("%+v", skills)
	}
	if got := h.extensions.extensionInstructions(context.Background(), ""); !strings.Contains(got, "Answer in short sentences.") {
		t.Fatal(got)
	}

	for _, c := range []string{"skills/review", "prompts/tone", "prompts/today", "themes/forest", "triage_templates/quiet"} {
		if code, out := call(t, mux, "PUT", "/api/v1/extensions/kit/-/contributions/"+c, master, `{"enabled": false}`); code != 200 {
			t.Fatalf("%s: %d %v", c, code, out)
		}
	}
	if len(h.extensions.extensionSkills()) != 0 || strings.Contains(h.extensions.extensionInstructions(context.Background(), ""), "short sentences") {
		t.Fatal("switched off: not the assistant's any more")
	}
	if _, out := call(t, mux, "GET", "/api/v1/extensions/-/prompts", master, ""); len(out["prompts"].([]any)) != 0 {
		t.Fatal(out)
	}
	if _, out := call(t, mux, "GET", "/api/v1/extensions/-/themes", master, ""); len(out["themes"].([]any)) != 1 {
		t.Fatal("only the pale one is on:", out)
	}
	if _, out := call(t, mux, "GET", "/api/v1/plugins/-/triage-templates", master, ""); len(out["templates"].([]any)) != 0 {
		t.Fatal("the old path too:", out)
	}
}
