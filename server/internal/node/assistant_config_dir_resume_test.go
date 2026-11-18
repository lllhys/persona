package node

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// resumeCLI resolves its directory as Claude Code does: unset means ~/.claude, a set value (an empty one too) is the
// directory. A resume whose transcript is not there fails the way Claude Code does.
const resumeCLI = `if [ "${CLAUDE_CONFIG_DIR+set}" = set ]; then dir=$CLAUDE_CONFIG_DIR; printf 'set:%s\n' "$dir"; else dir=$HOME/.claude; printf 'unset\n'; fi > "$CLI_FAKE_DIR/cli-profile"
prev=
for a in "$@"; do
  if [ "$prev" = --resume ] && ! ls "$dir"/projects/*/"$a.jsonl" >/dev/null 2>&1; then
    echo "No conversation found with session ID: $a" >&2
    exit 1
  fi
  prev=$a
done
echo '{"type":"system","subtype":"init"}'
while IFS= read -r line; do
  case "$line" in *'"type":"user"'*) echo '{"type":"result","subtype":"success"}' ;; esac
done
`

// resumeTest is cliTest with resumeCLI as the machine's claude, a home reached through a symlink (as /home -> /data00/home)
// and no CLAUDE_CONFIG_DIR in the node's service environment.
func resumeTest(t *testing.T) (*cliFixture, string) {
	t.Helper()
	f := cliTest(t)
	real := os.Getenv("HOME")
	if err := os.WriteFile(filepath.Join(real, "bin", "claude"), []byte("#!/bin/sh\n"+resumeCLI), 0o700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(t.TempDir(), "home")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", link)
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	os.Unsetenv("CLAUDE_CONFIG_DIR")
	f.n.home = link
	f.n.dir = filepath.Join(link, ".persona")
	f.n.cdirs = newClaudeDirs()
	f.n.cdirs.scan = func() map[int]procEnv { return nil }
	return f, real
}

func lookupEnv(env []string, key string) (string, bool) {
	for _, kv := range env {
		if k, v, ok := strings.Cut(kv, "="); ok && k == key {
			return v, true
		}
	}
	return "", false
}

func writeProfileTranscript(t *testing.T, profile, cwd, id string) {
	t.Helper()
	writeFile(t, filepath.Join(profile, "projects", "-assistant", id+".jsonl"),
		`{"type":"user","cwd":`+jsonString(t, cwd)+`,"message":{"role":"user","content":"earlier"}}`+"\n")
}

// A conversation from before #4310 whose transcript is in ~/.claude resumes with CLAUDE_CONFIG_DIR unset: "default" is
// Claude's unset directory, and Claude Code reads an empty value as a directory of its own (#4504).
func TestAssistantResumesHomeProfileConversationWithTheVariableUnset(t *testing.T) {
	f, real := resumeTest(t)
	n := f.n
	id := "a5500000-0000-4000-8000-000000000101"
	cwd := filepath.Join(n.dir, "assistant")
	writeProfileTranscript(t, filepath.Join(real, ".claude"), cwd, id)
	params := map[string]any{"launcher": "claude", "cwd": cwd, "text": "hello", "session_id": id, "model_fingerprint": "resume-test"}
	if _, err := nodeCall(t, n, "assistant.send", params); err != nil {
		t.Fatalf("resume failed: %v (child saw %q)", err, f.file("cli-profile"))
	}
	if got := f.file("cli-profile"); got != "unset\n" {
		t.Fatalf("child CLAUDE_CONFIG_DIR = %q, want unset", got)
	}
	if st, _ := n.state.get(id); st.ConfigDir != "default" {
		t.Fatalf("recorded profile %q, want default", st.ConfigDir)
	}
}

// A recorded profile keeps the conversation only when it holds the newest copy of its transcript, its projects/ shared
// through a symlink included. One with no copy (the conversation moved launchers) or only an older one (a rollback
// resumed it elsewhere, #4553) gives way to the newest copy: resuming the older one would drop the later turns.
func TestAssistantResumeFollowsTheTranscriptOverARecordedProfile(t *testing.T) {
	aged := func(t *testing.T, profile, id string) {
		t.Helper()
		old := filepath.Join(profile, "projects", "-assistant", id+".jsonl")
		if err := os.Chtimes(old, time.Now().Add(-time.Hour), time.Now().Add(-time.Hour)); err != nil {
			t.Fatal(err)
		}
	}
	scenarios := []string{
		"recorded-lacks-transcript", "recorded-lacks-home-transcript",
		"recorded-holds-older-copy-than-home", "recorded-holds-older-copy-than-other",
		"recorded-shares-projects", "recorded-holds-newest-copy",
	}
	for _, scenario := range scenarios {
		t.Run(scenario, func(t *testing.T) {
			f, real := resumeTest(t)
			n := f.n
			id := "a5500000-0000-4000-8000-000000000102"
			cwd := filepath.Join(n.dir, "assistant")
			private := filepath.Join(n.dir, "profiles", "cc-old")
			other := t.TempDir()
			n.cdirs.known = []string{other}
			want := private
			switch scenario {
			case "recorded-lacks-transcript":
				want = other
				writeProfileTranscript(t, other, cwd, id)
			case "recorded-lacks-home-transcript": // e.g. a private-profile launcher, then the main chat's
				want = "default"
				writeProfileTranscript(t, filepath.Join(real, ".claude"), cwd, id)
			case "recorded-holds-older-copy-than-home": // the rollback continued it in ~/.claude
				want = "default"
				writeProfileTranscript(t, private, cwd, id)
				aged(t, private, id)
				writeProfileTranscript(t, filepath.Join(real, ".claude"), cwd, id)
			case "recorded-holds-older-copy-than-other":
				want = other
				writeProfileTranscript(t, private, cwd, id)
				aged(t, private, id)
				writeProfileTranscript(t, other, cwd, id)
			case "recorded-holds-newest-copy":
				writeProfileTranscript(t, other, cwd, id)
				aged(t, other, id)
				writeProfileTranscript(t, private, cwd, id)
			case "recorded-shares-projects":
				writeProfileTranscript(t, filepath.Join(real, ".claude"), cwd, id)
				if err := os.MkdirAll(private, 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(filepath.Join(real, ".claude", "projects"), filepath.Join(private, "projects")); err != nil {
					t.Fatal(err)
				}
			}
			raw, _ := json.Marshal(map[string]any{"launcher": "cc-old", "cwd": cwd, "config_dir": private})
			var st sessionState
			if err := json.Unmarshal(raw, &st); err != nil {
				t.Fatal(err)
			}
			n.state.put(id, st)
			params := map[string]any{"launcher": "claude", "cwd": cwd, "text": "hello", "session_id": id, "model_fingerprint": "resume-test"}
			if _, err := nodeCall(t, n, "assistant.send", params); err != nil {
				t.Fatalf("resume failed: %v (child saw %q)", err, f.file("cli-profile"))
			}
			p := f.proc(id)
			got, set := lookupEnv(p.cmd.Environ(), "CLAUDE_CONFIG_DIR")
			if want == "default" && set || want != "default" && !sameScopePath(got, want) {
				t.Fatalf("process profile %q (set %v), want %q", got, set, want)
			}
			if child := strings.TrimSuffix(f.file("cli-profile"), "\n"); want == "default" && child != "unset" || want != "default" && !sameScopePath(strings.TrimPrefix(child, "set:"), want) {
				t.Fatalf("CLI child profile %q, want %q", child, want)
			}
			if st, _ := n.state.get(id); st.ConfigDir != want && !sameScopePath(st.ConfigDir, want) {
				t.Fatalf("recorded profile %q, want %q", st.ConfigDir, want)
			}
			// The recorded profile, asked for explicitly, is no move.
			n.stopAssistant(id)
			params["config_dir"] = want
			if _, err := nodeCall(t, n, "assistant.send", params); err != nil {
				t.Fatalf("resume naming its own profile: %v", err)
			}
		})
	}
}
