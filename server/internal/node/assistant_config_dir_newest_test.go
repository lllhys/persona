package node

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func ageTranscript(t *testing.T, profile, id string) {
	t.Helper()
	old := time.Now().Add(-time.Hour)
	if err := os.Chtimes(filepath.Join(profile, "projects", "-assistant", id+".jsonl"), old, old); err != nil {
		t.Fatal(err)
	}
}

func putSessionState(t *testing.T, n *Node, id string, fields map[string]any) {
	t.Helper()
	raw, _ := json.Marshal(fields)
	var st sessionState
	if err := json.Unmarshal(raw, &st); err != nil {
		t.Fatal(err)
	}
	n.state.put(id, st)
}

// A wrapper launcher that sets its own CLAUDE_CONFIG_DIR only ever opens its own profile. When that profile holds an
// older copy of the conversation and a separate ~/.claude a newer one, moving to ~/.claude would conflict with the
// wrapper and refuse every message; the conversation resumes from the copy the wrapper can open instead (#4553).
func TestAssistantResumeStaysInAWrapperProfileWithAnOlderCopy(t *testing.T) {
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
	cwd := filepath.Join(n.dir, "assistant")
	id := "a5500000-0000-4000-8000-000000000301"
	writeProfileTranscript(t, profile, cwd, id)
	ageTranscript(t, profile, id)
	writeProfileTranscript(t, filepath.Join(n.home, ".claude"), cwd, id)
	putSessionState(t, n, id, map[string]any{"launcher": "work", "cwd": cwd, "config_dir": profile})
	params := map[string]any{"launcher": "work", "cwd": cwd, "text": "hello", "session_id": id, "model_fingerprint": "wrapper-older-copy"}
	if _, err := nodeCall(t, n, "assistant.send", params); err != nil {
		t.Fatalf("resume refused: %v", err)
	}
	if got := envValue(f.proc(id).cmd.Environ(), "CLAUDE_CONFIG_DIR"); got != profile {
		t.Fatalf("process profile %q, want %q", got, profile)
	}
	if st, _ := n.state.get(id); st.ConfigDir != profile {
		t.Fatalf("recorded profile %q, want %q", st.ConfigDir, profile)
	}
}

// A conversation's live process keeps writing to its own profile. A newer copy appearing elsewhere while it runs does
// not move the record: the next message goes to the same process, so the record keeps naming the profile it runs in.
func TestAssistantResumeKeepsARunningProcessProfile(t *testing.T) {
	f, real := resumeTest(t)
	n := f.n
	cwd := filepath.Join(n.dir, "assistant")
	id := "a5500000-0000-4000-8000-000000000302"
	private := filepath.Join(n.dir, "profiles", "cc-old")
	writeProfileTranscript(t, private, cwd, id)
	putSessionState(t, n, id, map[string]any{"launcher": "claude", "cwd": cwd, "config_dir": private})
	params := map[string]any{"launcher": "claude", "cwd": cwd, "text": "hello", "session_id": id, "model_fingerprint": "running-process"}
	if _, err := nodeCall(t, n, "assistant.send", params); err != nil {
		t.Fatalf("first message: %v", err)
	}
	f.idle(t, id)
	first := f.proc(id)
	ageTranscript(t, private, id)
	writeProfileTranscript(t, filepath.Join(real, ".claude"), cwd, id)
	n.mu.Lock()
	delete(n.pathsAt, id) // past the half minute the remembered path is trusted for
	n.mu.Unlock()
	if got := n.transcriptPath(id); !sameScopePath(got, filepath.Join(real, ".claude", "projects", "-assistant", id+".jsonl")) {
		t.Fatalf("newest copy found at %q, want the one in ~/.claude", got)
	}
	if _, err := nodeCall(t, n, "assistant.send", params); err != nil {
		t.Fatalf("second message: %v", err)
	}
	p := f.proc(id)
	if p != first {
		t.Fatal("the second message started another process")
	}
	if got := envValue(p.cmd.Environ(), "CLAUDE_CONFIG_DIR"); !sameScopePath(got, private) {
		t.Fatalf("process profile %q, want %q", got, private)
	}
	if st, _ := n.state.get(id); !sameScopePath(st.ConfigDir, private) {
		t.Fatalf("recorded profile %q while the process runs in %q", st.ConfigDir, private)
	}
}
