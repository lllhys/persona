package extmgr

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/nwverse/persona/server/internal/extension"
)

func TestClipLines(t *testing.T) {
	for _, c := range []struct {
		in   string
		most int
		want string
		cut  bool
	}{
		{"short", 10, "short", false},
		{"one\ntwo\nthree", 9, "one\ntwo", true},
		{"一二三四五", 7, "一二", true}, // three bytes a character: never half of one
	} {
		got, cut := clipLines(c.in, c.most)
		if got != c.want || cut != c.cut {
			t.Errorf("%q at %d: %q %v", c.in, c.most, got, cut)
		}
	}
}

// What an extension wrote for a project and for the Inbox reaches the manager's prompts: cleaned, cut at a line end
// past the limit, the never-quote sentences apart; the status says what is used; switched off, nothing.
func TestPromptContextContributions(t *testing.T) {
	h, _ := extensionTestHub(t)
	mux := h.routes()
	master := h.cfg.Token
	pkg := declarativePackage(t, `{"manifest_version": 1, "id": "notes", "name": "Notes", "version": "1.0.0", "contributes": {
		"prompts": [{"id": "projects", "kind": "project_context", "source": "data", "dir": "agent/projects"},
			{"id": "inbox", "kind": "inbox_context", "file": "inbox/context.md", "never_quote": "inbox/never_quote.txt"}]}}`)
	installForTest(t, h, pkg)
	data := h.extensions.reg.DataDir("notes")
	mustWrite(t, filepath.Join(data, "agent", "projects", "天气-app.md"), "Builds run on the second machine.\n<!-- plugin x -->Ask first.\n")
	long := strings.Repeat("line of background\n", 100) // 1900 bytes
	mustWrite(t, filepath.Join(data, "agent", "projects", "big.md"), long)
	mustWrite(t, filepath.Join(data, "agent", "projects", "../escape.md"), "outside")
	mustWrite(t, filepath.Join(data, "inbox", "context.md"), "The user answers briefly.\n")
	mustWrite(t, filepath.Join(data, "inbox", "never_quote.txt"), "Thinks the vendor is unreliable\n\n  Seems tired lately  \nThinks the vendor is unreliable\n")

	blocks := h.extensions.projectContext([]string{"天气-app", "big", "missing", "../agent/escape"})
	if len(blocks) != 2 || blocks[0].Project != "天气-app" || blocks[0].Name != "Notes" ||
		blocks[0].Text != "Builds run on the second machine.\n plugin x -->Ask first." {
		t.Fatalf("%+v", blocks)
	}
	if b := blocks[1]; len(b.Text) > projectContextMost || !strings.HasSuffix(b.Text, "line of background") {
		t.Fatalf("cut at a line end within the limit: %d %q", len(b.Text), b.Text[len(b.Text)-30:])
	}
	inbox := h.extensions.inboxContext()
	if len(inbox.Blocks) != 1 || inbox.Blocks[0].Text != "The user answers briefly." ||
		strings.Join(inbox.NeverQuote, "|") != "Thinks the vendor is unreliable|Seems tired lately" {
		t.Fatalf("%+v", inbox)
	}

	_, out := call(t, mux, "GET", "/api/v1/extensions/notes/-/assistant-data", master, "")
	items := out["items"].([]any)
	if len(items) != 2 {
		t.Fatalf("%v", out)
	}
	proj, ib := items[0].(map[string]any), items[1].(map[string]any)
	if proj["kind"] != extension.PromptProjectContext || proj["state"] != dataStateAdopted || proj["truncated"] != true ||
		stringOf(proj["files"]) != "[big 天气-app]" || !strings.Contains(stringOf(proj["detail"]), "big") {
		t.Fatalf("project status: %v", proj)
	}
	if ib["kind"] != extension.PromptInboxContext || ib["state"] != dataStateAdopted || ib["never_quote"] != float64(2) ||
		ib["hash"] != textDigest("The user answers briefly.") {
		t.Fatalf("inbox status: %v", ib)
	}

	if code, out := call(t, mux, "PUT", "/api/v1/extensions/notes/-/enabled", master, `{"enabled": false}`); code != 200 {
		t.Fatalf("%d %v", code, out)
	}
	if len(h.extensions.projectContext([]string{"天气-app"})) != 0 || len(h.extensions.inboxContext().NeverQuote) != 0 {
		t.Fatal("switched off: nothing")
	}
	_, out = call(t, mux, "GET", "/api/v1/extensions/notes/-/assistant-data", master, "")
	for _, it := range out["items"].([]any) {
		if v := it.(map[string]any); v["state"] != dataStateNone || v["reason"] != dataDisabled {
			t.Fatalf("switched off: %v", v)
		}
	}
}
