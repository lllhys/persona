package node

import (
	"context"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/proto"
)

// A native child Agent's MCP server is a descendant of the same assistant process. Even if it names another session,
// the node resolves it to the parent and attests the parent's Inbox origin to the manager.
func TestInboxNativeChildMCPInheritsParentOrigin(t *testing.T) {
	if runtime.GOOS != "linux" && runtime.GOOS != "darwin" {
		t.Skip("no peer process here")
	}
	n, hub := runtimeNode(t)
	ask := mcpRelay(t, n, hub)
	self := &exec.Cmd{Process: &os.Process{Pid: os.Getpid()}}
	n.mu.Lock()
	n.procs["parent-session"] = &assistantProc{id: "parent-session", origin: originInbox, cmd: self}
	n.mu.Unlock()
	call := mcpCall(true, "child-supplied-session")
	got := ask(call)
	if got["assistant"] != true || got["session_id"] != "parent-session" || got["origin"] != originInbox {
		t.Fatalf("native child MCP call relayed as %v, want the parent's Inbox identity", got)
	}
}

// Codex's temporary process key and its eventual thread id must resolve to the same origin during startup, before the
// manager's origin book can contain the new thread.
func TestInboxCodexStartupKeyKeepsOrigin(t *testing.T) {
	empty := &Node{}
	if got := empty.assistantOrigin("not-started"); got != "" {
		t.Fatalf("a node without loaded session state returned origin %q", got)
	}
	n, _ := testNode(t)
	p := &assistantProc{id: "thread-session", origin: originInbox, cx: &codexConn{key: "proc-inbox"}}
	n.mu.Lock()
	n.akeys = map[string]*assistantProc{"proc-inbox": p}
	n.mu.Unlock()
	for _, id := range []string{"proc-inbox", "thread-session"} {
		if got := n.assistantOrigin(id); got != originInbox {
			t.Fatalf("assistantOrigin(%q)=%q, want inbox", id, got)
		}
	}
}

// The built-in Claude CLI really receives Plan mode through its permission-mode control, even when its start asks for
// bypass. Origin is recorded before the first turn and re-attested by the manager on later sends, so a node restart
// cannot silently return the conversation to bypass or lose its policy identity. (Only bypass is a startup flag;
// other modes use the control request.)
func TestInboxClaudeStartsInPlanAndPersistsOrigin(t *testing.T) {
	f := cliTest(t)
	// No model fingerprint: filling the launcher's default execution must not discard the start's attested origin.
	execution := modelExecution{Origin: originInbox}
	id, err := f.n.assistantSend(context.Background(), "", t.TempDir(), "claude", "Investigate", nil, proto.ModeBypass, "", execution)
	if err != nil {
		t.Fatal(err)
	}
	f.idle(t, id)
	in := f.file("stdin.jsonl")
	if !strings.Contains(in, `"subtype":"set_permission_mode"`) || !strings.Contains(in, `"mode":"plan"`) {
		t.Fatalf("Claude control requests did not select Plan: %s", in)
	}
	argv := f.file("cli-argv")
	if !strings.Contains(argv, "--tools "+assistantBuiltinTools) {
		t.Fatalf("Inbox investigation lost built-in file, shell, web or tool-search capabilities: %s", argv)
	}
	if !strings.Contains(argv, assistantAllowedTools) {
		t.Fatalf("Plan mode would ask before inbox_suggest instead of allowing Persona's own tools: %s", argv)
	}
	st, ok := f.n.state.get(id)
	if !ok || st.Origin != originInbox || st.AppMode != proto.ModePlan {
		t.Fatalf("persisted session state=%+v present=%v", st, ok)
	}
	p := f.proc(id)
	p.mu.Lock()
	processOrigin, executionOrigin := p.origin, p.model.Origin
	p.mu.Unlock()
	if processOrigin != originInbox || executionOrigin != originInbox {
		t.Fatalf("spawned process origin=%q execution origin=%q, want inbox", processOrigin, executionOrigin)
	}
	if got := f.n.assistantOrigin(id); got != originInbox {
		t.Fatalf("live process origin=%q, want inbox", got)
	}
}

// Inbox handling never inherits the ordinary bypass-safe bot send/reply exemption. Reads remain available, and the
// same bot sends and replies still work for an ordinary bypass conversation.
func TestInboxLarkOutboundDeniedOrdinaryBypassPreserved(t *testing.T) {
	for _, tc := range []struct {
		name, origin, command, want string
	}{
		{"Inbox bot send", originInbox, "lark-cli im +messages-send --as bot --text hello", "deny"},
		{"Inbox bot reply", originInbox, "lark-cli im +messages-reply --as bot --text hello", "deny"},
		{"Inbox raw API send", originInbox, "lark-cli api POST /open-apis/im/v1/messages --as bot --data '{}'", "deny"},
		{"Inbox ordinary write", originInbox, "lark-cli docs +create --title example", "deny"},
		{"Inbox unknown write", originInbox, "lark-cli custom $action", "deny"},
		{"Inbox read", originInbox, "lark-cli docs +fetch --doc-id example", "allow"},
		{"ordinary bot send", "", "lark-cli im +messages-send --as bot --text hello", "allow"},
		{"ordinary bot reply", "", "lark-cli im +messages-reply --as bot --text hello", "allow"},
		{"ordinary read", "", "lark-cli docs +fetch --doc-id example", "allow"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			n := codexTestNode(t)
			fh := withFakeHub(t, n)
			const sid = "11111111-1111-4111-8111-111111111111"
			cfg := olCfg()
			cfg.CLI = &proto.LarkCLI{Bin: "/nonexistent/lark-cli"}
			n.larkRisks = map[string]string{"\x00docs +fetch": "read", "\x00docs +create": "write"}
			n.mu.Lock()
			n.larkCfg, n.larkCfgAt = cfg, time.Now().Add(time.Hour)
			n.procs[sid] = &assistantProc{id: sid, agent: "claude", origin: tc.origin, exited: make(chan struct{})}
			n.mu.Unlock()
			n.persistModes(sid, proto.ModeBypass, "")
			decision, reason, err := n.larkCheck(context.Background(), sid, t.TempDir(), tc.command,
				proto.Interaction{SessionID: sid, Cwd: t.TempDir(), ExpiresAt: time.Now().Add(time.Minute)})
			if err != nil || decision != tc.want {
				t.Fatalf("decision=%q reason=%q err=%v, want %q", decision, reason, err, tc.want)
			}
			if tc.origin == originInbox && tc.want == "deny" && !strings.Contains(reason, "inbox_suggest") {
				t.Fatalf("Inbox refusal is not actionable: %q", reason)
			}
			if len(fh.interactions()) != 0 {
				t.Fatalf("policy test unexpectedly opened a confirmation card: %v", fh.interactions())
			}
		})
	}
}
