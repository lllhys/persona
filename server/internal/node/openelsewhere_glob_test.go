package node

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// writeClaudeConfigSession writes config's sessions/<pid>.json as a running claude does. The test process stands in
// for that claude; no second claude is started.
func writeClaudeConfigSession(t *testing.T, config string, pid int, id string) string {
	t.Helper()
	sessions := filepath.Join(config, "sessions")
	if err := os.MkdirAll(sessions, 0o700); err != nil {
		t.Fatal(err)
	}
	body := fmt.Sprintf(`{"pid":%d,"sessionId":%q,"entrypoint":"claude-desktop","kind":"interactive"}`, pid, id)
	path := filepath.Join(sessions, fmt.Sprintf("%d.json", pid))
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func quietClaudeConfigSession(t *testing.T, config string, pid int, id string) string {
	t.Helper()
	path := writeClaudeConfigSession(t, config, pid, id)
	old := time.Now().Add(-time.Hour)
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatal(err)
	}
	return path
}

// A config directory is a literal path, not a glob pattern. In particular, claude[work is a valid directory name but
// an invalid pattern, and claude[work] must not scan claudew (or any other sibling matched by that character class).
func TestLiveClaudesLiteralConfigDir(t *testing.T) {
	const (
		id         = "0bd545f1-681d-4d98-bf5a-eb7c1251a0f0"
		siblingID  = "2e2f25fd-6a4c-4ebb-8615-788af4aaa7e2"
		siblingPID = 999999 // deterministic and not a live process; it must never be deduplicated with the real PID
	)
	for _, name := range []string{"claude[work", "claude[work]", "claude-work"} {
		t.Run(name, func(t *testing.T) {
			n, _ := testNode(t)
			config := filepath.Join(t.TempDir(), name)
			t.Setenv("CLAUDE_CONFIG_DIR", config)
			pid := os.Getpid()
			path := quietClaudeConfigSession(t, config, pid, id)
			siblingPath := ""
			if name == "claude[work]" {
				sibling := filepath.Join(filepath.Dir(config), "claudew")
				siblingPath = quietClaudeConfigSession(t, sibling, siblingPID, siblingID)
			}

			live := n.liveClaudes()
			if len(live) != 1 {
				t.Errorf("liveClaudes() = %+v, want only pid %d with session %s (never sibling pid %d/session %s)", live, pid, id, siblingPID, siblingID)
			} else {
				if live[0].pid != pid || live[0].SessionID != id {
					t.Errorf("liveClaudes() = %+v, want only pid %d with session %s (never sibling pid %d/session %s)", live, pid, id, siblingPID, siblingID)
				}
				if got, want := live[0].dir, filepath.Join(config, "sessions"); got != want {
					t.Errorf("live claude's sessions dir = %q, want %q", got, want)
				}
			}
			cached, ok := n.cfiles.files[path]
			if !ok {
				t.Errorf("cache does not contain the literal session file %q: %v", path, n.cfiles.files)
			} else if cached.proc.SessionID != id {
				t.Errorf("cached literal session = %q, want %q", cached.proc.SessionID, id)
			}
			if len(n.cfiles.files) != 1 {
				t.Errorf("cache contains files other than the literal pid %d/session %s: %v", pid, id, n.cfiles.files)
			}
			if siblingPath != "" {
				// parse caches a quiet file before sameProcess rejects its dead PID. Its distinct PID also means
				// seen[pid] cannot hide this scan, even when a broken glob returns the literal file first.
				if _, ok := n.cfiles.files[siblingPath]; ok {
					t.Errorf("pattern-matching sibling pid %d/session %s was scanned and cached: %q", siblingPID, siblingID, siblingPath)
				}
				if gotPID, _, ok := n.claudeOpenElsewhere(siblingID); ok || gotPID == siblingPID {
					t.Errorf("sibling session leaked into open-elsewhere results: pid %d, ok %v", gotPID, ok)
				}
			}
			gotPID, where, ok := n.claudeOpenElsewhere(id)
			if !ok || gotPID != pid || where != whereDesktop {
				t.Errorf("claudeOpenElsewhere = (%d, %q, %v), want (%d, %q, true)", gotPID, where, ok, pid, whereDesktop)
			}
			if err := n.openElsewhereRefusal(id); !isRefusal(err, openElsewhere) {
				t.Errorf("openElsewhereRefusal = %v, want open_elsewhere refusal", err)
			}
		})
	}
}

// Backslash escaping is simultaneous with the other Unix escapes: a literal backslash next to a bracket, star, or
// question mark must not turn that character back into pattern syntax.
func TestLiveClaudesLiteralConfigDirUnixMetacharacters(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("backslash is a path separator on Windows")
	}
	n, _ := testNode(t)
	config := filepath.Join(t.TempDir(), `claude\[work]*?`)
	t.Setenv("CLAUDE_CONFIG_DIR", config)
	pid := os.Getpid()
	const id = "0bd545f1-681d-4d98-bf5a-eb7c1251a0f0"
	writeClaudeConfigSession(t, config, pid, id)
	live := n.liveClaudes()
	if len(live) != 1 || live[0].pid != pid || live[0].SessionID != id {
		t.Fatalf("liveClaudes() = %+v, want only pid %d with session %s", live, pid, id)
	}
}

// The RPC boundary refuses both resume and send before the fake engine is asked to start a process or type anything.
func TestRPCRefusesLiteralConfigDirOpenElsewhere(t *testing.T) {
	const id = "0bd545f1-681d-4d98-bf5a-eb7c1251a0f0"
	for _, name := range []string{"claude[work", "claude[work]", "claude-work"} {
		for _, method := range []string{"session.start", "session.send"} {
			t.Run(name+"/"+method, func(t *testing.T) {
				n, f, home := herdrNode(t)
				config := filepath.Join(t.TempDir(), name)
				t.Setenv("CLAUDE_CONFIG_DIR", config)
				pid := os.Getpid()
				writeClaudeConfigSession(t, config, pid, id)
				f.reset()

				params := map[string]any{"cwd": home, "resume_session_id": id}
				if method == "session.send" {
					params = map[string]any{"session_id": id, "text": "still there?", "client_msg_id": "m-1"}
				}
				_, err := request(t, n, method, params)
				if !isRefusal(err, openElsewhere) {
					t.Fatalf("%s error = %v, want open_elsewhere refusal", method, err)
				}
				data := refusalData(t, err)
				if !strings.Contains(data, fmt.Sprintf(`"pid":%d`, pid)) || !strings.Contains(data, `"where":"desktop"`) {
					t.Errorf("refusal data = %s, want live pid %d and desktop", data, pid)
				}
				if calls := f.trace("workspace.create", "tab.create", "pane.run", "pane.send_input", "pane.send_keys", "agent.prompt", "pane.send_text"); len(calls) != 0 {
					t.Errorf("%s made fake-engine start/type calls: %v", method, calls)
				}
			})
		}
	}
}

func TestLiteralGlobPathForOS(t *testing.T) {
	for _, tt := range []struct {
		name, goos, path, want string
	}{
		{name: "unix ordinary", goos: "linux", path: "/tmp/claude-work/sessions", want: "/tmp/claude-work/sessions"},
		{name: "unix closing bracket alone", goos: "darwin", path: "/tmp/claude]work/sessions", want: "/tmp/claude]work/sessions"},
		{name: "unix escaped bracket makes closing bracket literal", goos: "linux", path: "/tmp/claude[work]/sessions", want: `/tmp/claude\[work]/sessions`},
		{name: "unix all escapes are simultaneous", goos: "darwin", path: `/tmp/claude\[work]*?/sessions`, want: `/tmp/claude\\\[work]\*\?/sessions`},
		{name: "windows ordinary", goos: "windows", path: `C:\claude-work\sessions`, want: `C:\claude-work\sessions`},
		{name: "windows character classes", goos: "windows", path: `C:\claude[work]*?\sessions`, want: `C:\claude[[]work][*][?]\sessions`},
		{name: "windows replacements are simultaneous", goos: "windows", path: `[*?`, want: `[[][*][?]`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := literalGlobPathForOS(tt.path, tt.goos); got != tt.want {
				t.Errorf("literalGlobPathForOS(%q, %q) = %q, want %q", tt.path, tt.goos, got, tt.want)
			}
		})
	}
}
