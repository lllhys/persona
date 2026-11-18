package node

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/proto"
	"github.com/nwverse/persona/server/internal/rpc"
)

// shortStartRetries makes the retries of a launcher whose command is missing quick for a test.
func shortStartRetries(t *testing.T, delay time.Duration) {
	t.Helper()
	retries, wait := assistantStartRetries, assistantStartRetryDelay
	assistantStartRetries, assistantStartRetryDelay = 3, delay
	t.Cleanup(func() { assistantStartRetries, assistantStartRetryDelay = retries, wait })
}

// While Claude Code is being reinstalled by its auto-updater, claude is missing for a couple of seconds and a launcher
// started then exits with "command not found: claude" (#2581: a /agent in a group chat failed with "send to claude:
// write |1: file already closed"). The send starts it again and the message gets through once claude is back.
func TestAssistantSendRetriesWhileClaudeIsMissing(t *testing.T) {
	fakeClaudeHome(t, "read line\necho '{\"type\":\"system\",\"subtype\":\"init\"}'\nsleep 30\n")
	shortStartRetries(t, 300*time.Millisecond)
	claude := filepath.Join(os.Getenv("HOME"), "bin", "claude")
	moved := claude + ".installing"
	if err := os.Rename(claude, moved); err != nil {
		t.Fatal(err)
	}
	back := make(chan error, 1)
	go func() {
		time.Sleep(500 * time.Millisecond) // npm writing the new install
		back <- os.Rename(moved, claude)
	}()
	n := codexTestNode(t)
	withFakeHub(t, n)
	id, err := n.assistantSend(context.Background(), "", t.TempDir(), "claude", "hello", nil, "", "")
	if err := <-back; err != nil {
		t.Fatal(err)
	}
	if err != nil {
		t.Fatalf("claude came back while the send retried, it still failed: %v", err)
	}
	n.mu.Lock()
	p := n.procs[id]
	n.mu.Unlock()
	if p == nil || !p.alive() {
		t.Fatal("the process that started is not the conversation's")
	}
	n.stopAssistant(id)
}

// Still missing after the retries: the send fails with what the shell said, not with the closed pipe.
func TestAssistantSendSaysTheCommandIsMissing(t *testing.T) {
	fakeClaudeHome(t, "exit 0\n")
	shortStartRetries(t, 100*time.Millisecond)
	if err := os.Remove(filepath.Join(os.Getenv("HOME"), "bin", "claude")); err != nil {
		t.Fatal(err)
	}
	// The launcher's shell keeps /usr/bin and /bin (fakeClaudeHome), where a system-wide install (npm install -g with
	// the distribution's node) puts claude: there nothing is missing, and the send would run the real one (#6257).
	if p, err := exec.LookPath("claude"); err == nil {
		t.Skipf("this machine's own claude is on the launcher's PATH: %s", p)
	}
	n := codexTestNode(t)
	withFakeHub(t, n)
	start := time.Now()
	_, err := n.assistantSend(context.Background(), "", t.TempDir(), "claude", "hello", nil, "", "")
	if err == nil {
		t.Fatal("a launcher whose command is missing answered ok")
	}
	msg := err.Error()
	if !strings.Contains(msg, "没有启动起来") || !strings.Contains(msg, "command not found") || strings.Contains(msg, "file already closed") {
		t.Fatalf("error: %q", msg)
	}
	// Coded, so the app is told Claude Code is missing on that machine (docs/assistant/onboarding.md 4.6); nothing was delivered.
	var re *rpc.Error
	if !errors.As(err, &re) || re.Code != proto.CodeClaudeMissing || re.Message != msg || !strings.Contains(string(re.Data), `"delivered":"none"`) {
		t.Fatalf("not coded claude_missing: %#v", err)
	}
	if elapsed := time.Since(start); elapsed < 3*100*time.Millisecond {
		t.Fatalf("gave up after %v, before its %d retries", elapsed, assistantStartRetries)
	}
	n.mu.Lock()
	left := len(n.procs)
	n.mu.Unlock()
	if left != 0 {
		t.Fatal("a dead process is still kept")
	}
}

// A launcher that fails for another reason is not started again: it would fail the same way, only later.
func TestAssistantSendDoesNotRetryOtherStartFailures(t *testing.T) {
	fakeClaudeHome(t, "echo 'personad launch opus: no persona node' >&2\nexit 1\n")
	shortStartRetries(t, 2*time.Second)
	n := codexTestNode(t)
	withFakeHub(t, n)
	start := time.Now()
	if _, err := n.assistantSend(context.Background(), "", t.TempDir(), "claude", "hello", nil, "", ""); err == nil {
		t.Fatal("a launcher that did not start answered ok")
	}
	if elapsed := time.Since(start); elapsed >= 2*time.Second {
		t.Fatalf("waited %v: a failure other than a missing command was retried", elapsed)
	}
}

// What a failed start was missing, by what the shell or personad launch printed.
func TestMissingFrom(t *testing.T) {
	for _, tc := range []struct {
		launcher string
		lines    []string
		notFound bool
		want     string
	}{
		{"claude", []string{"zsh:1: command not found: claude"}, true, proto.CodeClaudeMissing},
		{"cc_work", []string{"cc_work:1: command not found: claude"}, true, proto.CodeClaudeMissing}, // a ~/.zshrc function calling claude
		{"cc_gone", []string{"zsh:1: command not found: cc_gone"}, true, proto.CodeLauncherMissing},
		{"cc_gone", []string{"bash: line 1: cc_gone: command not found"}, true, proto.CodeLauncherMissing},
		{"claude", []string{"bash: claude: command not found"}, true, proto.CodeClaudeMissing},
		{"work", []string{"personad launch: claude not found in PATH or the usual install locations"}, false, proto.CodeClaudeMissing},
		{"claude", nil, true, proto.CodeClaudeMissing},
		{"cc_gone", nil, true, proto.CodeLauncherMissing},
		{"claude", []string{"Error: unknown option '--plugin-dir'"}, false, ""},
		// ~/.zshrc noise under zsh -ic says nothing about why the start failed
		{"cc_work", []string{"/home/u/.zshrc:12: command not found: compdef", "Error: unknown option '--plugin-dir'"}, false, ""},
		{"cc_work", []string{"/home/u/.zshrc:12: command not found: compdef", "zsh:1: command not found: nvm"}, true, ""},
		{"cc_gone", []string{"/home/u/.zshrc:12: command not found: compdef", "zsh:1: command not found: cc_gone"}, true, proto.CodeLauncherMissing},
		{"cc_work", []string{"/home/u/.zshrc:12: command not found: compdef", "cc_work:1: command not found: claude"}, true, proto.CodeClaudeMissing},
	} {
		if got := missingFrom(tc.launcher, tc.lines, tc.notFound); got != tc.want {
			t.Errorf("missingFrom(%s, %q, %v) = %q, want %q", tc.launcher, tc.lines, tc.notFound, got, tc.want)
		}
	}
}
