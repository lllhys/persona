package node

import (
	"context"
	"github.com/nwverse/persona/server/internal/compat"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/nwverse/persona/server/internal/identity"
	"github.com/nwverse/persona/server/internal/proto"
)

// Tests start claude without the plugin unless one says which claude it is: asking a real one would make what they
// expect depend on the machine.
func init() { claudeVersionOf = func(context.Context) string { return "" } }

func TestVersionAtLeast(t *testing.T) {
	for v, want := range map[string]bool{"2.1.0": true, "2.1.318": true, "2.2.0-beta": true, "3.0.0": true, "2.0.99": false, "1.9.9": false,
		"": false, "2.1": false, "x.1.0": false} {
		if got := versionAtLeast(v, pluginMinVersion); got != want {
			t.Errorf("%q: %v", v, got)
		}
	}
}

// The agents Persona starts load its plugin for their session (--plugin-dir), with a claude that takes it; with an
// older one, or one whose version is not known, they start as before. The plugin is written when the node starts.
func TestAgentPlugin(t *testing.T) {
	for _, tc := range []struct {
		version string
		flag    bool
	}{{"2.1.318", true}, {"2.0.9", false}, {"", false}} {
		claudeVersionOf = func(context.Context) string { return tc.version }
		n, f, home := herdrNode(t)
		if err := n.writeClaudeConfig(); err != nil {
			t.Fatal(err)
		}
		plugin := filepath.Join(personaDir(home), "agent-plugin")
		if st, err := os.Stat(filepath.Join(plugin, "skills", "persona-office", "scripts", "office.sh")); err != nil || st.Mode()&0o111 == 0 {
			t.Fatalf("the plugin's script: %v", err)
		}
		if _, err := os.Stat(filepath.Join(plugin, ".claude-plugin", "plugin.json")); err != nil {
			t.Fatal(err)
		}
		cwd := mkdir(t, filepath.Join(home, "proj"))
		f.reply("workspace.list", map[string]any{"workspaces": []any{map[string]any{"workspace_id": "w1", "label": "proj"}}})
		startHerdr(f, "w1:p2", proto.StatusIdle)
		f.reply("tab.create", map[string]any{"tab": map[string]any{"tab_id": "w1:t2"}, "root_pane": map[string]any{"pane_id": "w1:p2"}})
		f.screenSteps("w1:p2", isKeys(), screenFixture(t, "claude-2.1.312-empty-162.txt"))
		if _, err := request(t, n, "session.start", map[string]any{"session_id": startID, "cwd": cwd, "async": true}); err != nil {
			t.Fatal(err)
		}
		waitCalls(t, f, "pane.send_input", 1)
		want := startCmd("claude", "--session-id "+startID, personaDir(home))
		if tc.flag { // the hooks go in a plugin too, and Persona passes no --settings (hooksplugin.go)
			want = inAgentEnv(personaDir(home), "HERDR_AGENT=claude claude --session-id "+startID+" --mcp-config "+shellQuote(filepath.Join(personaDir(home), "mcp.json"))+
				" --plugin-dir "+shellQuote(plugin)+
				" --allowedTools "+shellQuote("Bash(sh "+filepath.Join(plugin, "skills", "persona-office", "scripts", "office.sh")+" *)")+
				" --plugin-dir "+shellQuote(filepath.Join(personaDir(home), "claude-hooks"))+
				" --plugin-dir "+shellQuote(filepath.Join(personaDir(home), liveRepliesDir)), shellQuote) // live replies: on by default
		}
		if typed := f.trace("pane.send_input"); !slices.Contains(typed, call("pane.send_input", map[string]any{"pane_id": "w1:p2", "keys": []string{"Enter"}, "text": want})) {
			t.Errorf("claude %q: typed %v, want %s", tc.version, typed, want)
		}
	}
	claudeVersionOf = func(context.Context) string { return "" }
}

// The assistant is allowed Persona's tools and its own copy of the office skill's script, at its exact path.
func TestAssistantAllowsTheOfficeScript(t *testing.T) {
	n, home := testNode(t)
	want := assistantAllowedTools + ",mcp__" + compat.MCPServer + ",Bash(sh " + filepath.Join(identity.AssistantDir(home), ".claude", "skills", "persona-office", "scripts", "office.sh") + " *)"
	if got := n.assistantTools(true); got != want {
		t.Fatalf("%s\nwant %s", got, want)
	}
}
