package node

import (
	"os"
	"path/filepath"
	"testing"
)

// A conversation whose model configuration names no model resumes on its launcher's default: Claude Code would keep
// the model the session ran on last, so the node names the default unless the launcher names a model itself (#2487).
func TestAssistantResumeModel(t *testing.T) {
	configured := func(model string) modelExecution {
		return modelExecution{Model: model, Fingerprint: "configured", IsMain: true}
	}
	for _, tt := range []struct {
		name, rc, launcher string
		user, project      string // the user's settings.json, the project's .claude/settings.json
		execution          modelExecution
		resume             bool
		want               string // the --model it starts with, "" for none
	}{
		{"the default", "", "claude", "", "", configured(""), true, "default"},
		{"the user's settings", "", "claude", `{"model":"sonnet"}`, "", configured(""), true, "sonnet"},
		{"the project's over the user's", "", "claude", `{"model":"sonnet"}`, `{"model":"opus"}`, configured(""), true, "opus"},
		{"a configured model", "", "claude", `{"model":"sonnet"}`, "", configured("haiku"), true, "haiku"},
		{"a new conversation", "", "claude", "", "", configured(""), false, ""},
		{"a hub that resolved nothing", "", "claude", "", "", modelExecution{}, true, ""},
		{"the shell exports a model", "export ANTHROPIC_MODEL=gateway-model", "claude", "", "", configured(""), true, ""},
		{"a settings env names a model", "", "claude", `{"env":{"ANTHROPIC_DEFAULT_OPUS_MODEL":"gateway-opus"}}`, "", configured(""), true, ""},
		{"the launcher names a model", `function pinned { export ANTHROPIC_MODEL=gateway-model; command claude "$@"; }`, "pinned", "", "", configured(""), true, ""},
		{"the launcher passes --model", `function fixed { command claude --model gateway-model "$@"; }`, "fixed", "", "", configured(""), true, ""},
		{"a launcher that names none", `function plain { command claude "$@"; }`, "plain", `{"model":"sonnet"}`, "", configured(""), true, "sonnet"},
		{"a model setting that is no model id", "", "claude", `{"model":"not a model"}`, "", configured(""), true, ""},
	} {
		t.Run(tt.name, func(t *testing.T) {
			fakeClaudeHome(t, "read line\nprintf '%s\\n' '{\"type\":\"system\",\"subtype\":\"init\"}'\nsleep 30\n")
			for _, k := range append([]string{"CLAUDE_CONFIG_DIR"}, claudeModelVars...) {
				t.Setenv(k, "")
			}
			managed, managedDirs := claudeManagedSettings, claudeManagedSettingsDirs
			claudeManagedSettings, claudeManagedSettingsDirs = nil, nil
			t.Cleanup(func() { claudeManagedSettings, claudeManagedSettingsDirs = managed, managedDirs })
			if err := os.WriteFile(filepath.Join(os.Getenv("HOME"), ".zshrc"), []byte(tt.rc+"\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			n := codexTestNode(t)
			n.dir = t.TempDir()
			withFakeHub(t, n)
			cwd := t.TempDir()
			for dir, s := range map[string]string{n.defaultClaudeDir(): tt.user, filepath.Join(cwd, ".claude"): tt.project} {
				if s == "" {
					continue
				}
				if err := os.MkdirAll(dir, 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(dir, "settings.json"), []byte(s), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			p, err := n.spawnClaude("11111111-2222-3333-4444-555555555555", cwd, tt.launcher, tt.resume, false, tt.execution)
			if err != nil {
				t.Fatal(err)
			}
			defer n.stopAssistant(p.id)
			got := ""
			if i := indexOf(p.cmd.Args, "--model"); i >= 0 && i+1 < len(p.cmd.Args) {
				got = p.cmd.Args[i+1]
			}
			if got != tt.want {
				t.Fatalf("--model %q, want %q: %q", got, tt.want, p.cmd.Args)
			}
		})
	}
}
