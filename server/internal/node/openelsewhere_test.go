package node

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nwverse/persona/server/internal/rpc"
)

// isRefusal: err is a refusal with this code that says nothing was sent.
func isRefusal(err error, code string) bool {
	var re *rpc.Error
	return errors.As(err, &re) && re.Code == code && strings.Contains(string(re.Data), `"delivered":"none"`)
}

// writeClaudeSession writes sessions/<pid>.json as a running claude does.
func writeClaudeSession(t *testing.T, home string, pid int, id, entrypoint string) {
	t.Helper()
	dir := filepath.Join(home, ".claude", "sessions")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	body := fmt.Sprintf(`{"pid":%d,"sessionId":%q,"cwd":%q,"entrypoint":%q,"kind":"interactive"}`, pid, id, home, entrypoint)
	if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("%d.json", pid)), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

// A Claude session open in Claude's desktop app (no pane Persona drives, so it lists as not running) is not resumed
// from the phone: that would start a second claude writing the same transcript. A file left by a claude that exited
// does not count.
func TestResumeRefusedWhileOpenElsewhere(t *testing.T) {
	n, _, home := herdrNode(t)
	const open, closed = "0bd545f1-681d-4d98-bf5a-eb7c1251a0f0", "2e2f25fd-6a4c-4ebb-8615-788af4aaa7e2"
	writeClaudeSession(t, home, os.Getpid(), open, "claude-desktop") // this test's own pid: alive
	writeClaudeSession(t, home, 999999, closed, "claude-desktop")    // no such process

	_, err := request(t, n, "session.start", map[string]any{"cwd": home, "resume_session_id": open})
	if !isRefusal(err, openElsewhere) || !strings.Contains(err.Error(), "Claude 桌面版") {
		t.Fatalf("resume of a session open in the desktop app: %v", err)
	}
	if _, where, ok := n.claudeOpenElsewhere(closed); ok {
		t.Fatalf("a dead claude's file counted as open (%s)", where)
	}
	if got := whereOpen("cli"); got != whereTerminal {
		t.Fatal(got)
	}
	if got := whereOpen("claude-desktop-3p"); got != whereDesktop {
		t.Fatal(got)
	}
}

// The phone's send to such a session (the way the report came in: an idle-looking session, a message) is refused the
// same way, before anything is started or typed; herdr is never asked to make a tab.
func TestSendRefusedWhileOpenElsewhere(t *testing.T) {
	n, f, home := herdrNode(t)
	const open = "0bd545f1-681d-4d98-bf5a-eb7c1251a0f0"
	writeClaudeSession(t, home, os.Getpid(), open, "claude-desktop")
	f.reset()
	_, err := request(t, n, "session.send", map[string]any{"session_id": open, "text": "still there?", "client_msg_id": "m-1"})
	if !isRefusal(err, openElsewhere) || !strings.Contains(err.Error(), "正在 Claude 桌面版里开着") {
		t.Fatalf("send to a session open in the desktop app: %v", err)
	}
	// The hub says it in the app's language by where it is open (apierr openElsewhereKeys), and the words name no
	// device: the send may have come from the web or a computer, not the phone.
	if data := refusalData(t, err); !strings.Contains(data, `"where":"desktop"`) {
		t.Fatalf("refusal data: %s", data)
	}
	if strings.Contains(err.Error(), "手机") {
		t.Fatalf("the refusal names the phone: %v", err)
	}
	for _, c := range f.trace() {
		if strings.Contains(c, "workspace") || strings.Contains(c, "tab.create") || strings.Contains(c, "pane.") {
			t.Fatalf("herdr was asked to start something: %v", f.trace())
		}
	}
}
