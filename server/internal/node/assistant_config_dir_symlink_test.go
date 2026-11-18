package node

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A conversation from before #4310 run through a wrapper that sets its own CLAUDE_CONFIG_DIR, on a machine where
// ~/.claude/projects (or all of ~/.claude) is a symlink to that profile's: the transcript is found under ~/.claude, but
// it is the wrapper's profile's. The resume keeps the wrapper's profile instead of refusing every message. A transcript
// that really is elsewhere (a separate ~/.claude/projects, or one linked to another profile) is still refused.
func TestAssistantProfileFollowsSymlinkedProjects(t *testing.T) {
	for _, scenario := range []string{"projects-linked", "home-linked", "separate-home", "linked-elsewhere"} {
		t.Run(scenario, func(t *testing.T) {
			f := cliTest(t)
			n := f.n
			n.home = os.Getenv("HOME")
			n.dir = filepath.Join(n.home, ".persona")
			n.cdirs = newClaudeDirs()
			n.cdirs.scan = func() map[int]procEnv { return nil }
			t.Setenv("CLAUDE_CONFIG_DIR", "")
			os.Unsetenv("CLAUDE_CONFIG_DIR")
			profile := filepath.Join(n.home, ".config", "claude-code", "profiles", "work")
			writeFile(t, filepath.Join(n.home, ".zshrc"), `function work { CLAUDE_CONFIG_DIR=`+shellQuote(profile)+` command claude "$@"; }`+"\n")
			home := filepath.Join(n.home, ".claude")
			link := func(target, name string) {
				t.Helper()
				if err := os.MkdirAll(target, 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.MkdirAll(filepath.Dir(name), 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(target, name); err != nil {
					t.Fatal(err)
				}
			}
			holder, refused := profile, false
			switch scenario {
			case "projects-linked":
				link(filepath.Join(profile, "projects"), filepath.Join(home, "projects"))
			case "home-linked":
				link(profile, home)
			case "separate-home":
				holder, refused = home, true
				if err := os.MkdirAll(filepath.Join(profile, "projects"), 0o700); err != nil {
					t.Fatal(err)
				}
			case "linked-elsewhere":
				holder, refused = t.TempDir(), true
				link(filepath.Join(holder, "projects"), filepath.Join(home, "projects"))
			}
			cwd := filepath.Join(n.dir, "assistant")
			id := "a5500000-0000-4000-8000-000000000201"
			writeFile(t, filepath.Join(holder, "projects", "-assistant", id+".jsonl"),
				`{"type":"user","cwd":`+jsonString(t, cwd)+`,"message":{"role":"user","content":"earlier"}}`+"\n")
			if got := n.transcriptPath(id); !strings.HasPrefix(got, home+string(filepath.Separator)) && scenario != "separate-home" {
				t.Fatalf("transcript found at %q, want it under %s as on the reported machine", got, home)
			}
			raw, _ := json.Marshal(map[string]any{"launcher": "work", "cwd": cwd})
			var st sessionState
			if err := json.Unmarshal(raw, &st); err != nil {
				t.Fatal(err)
			}
			n.state.put(id, st)
			params := map[string]any{"launcher": "work", "cwd": cwd, "text": "hello", "session_id": id, "model_fingerprint": "symlink-test"}
			_, err := nodeCall(t, n, "assistant.send", params)
			if refused {
				if err == nil || !strings.Contains(err.Error(), "conflicts with the launcher's own profile") {
					t.Fatalf("a transcript outside the wrapper's profile was not refused: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("resume refused: %v", err)
			}
			p := f.proc(id)
			if got := envValue(p.cmd.Environ(), "CLAUDE_CONFIG_DIR"); got != profile {
				t.Fatalf("process profile %q, want %q", got, profile)
			}
			if st, _ := n.state.get(id); st.ConfigDir != profile {
				t.Fatalf("recorded profile %q, want %q", st.ConfigDir, profile)
			}
		})
	}
}
