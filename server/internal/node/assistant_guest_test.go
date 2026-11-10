package node

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"github.com/nwverse/persona/server/internal/compat"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/proto"
)

// A guest conversation's process (#5195): Persona's MCP server alone, told it is a guest's, no built-in tools, only
// its own tools allowed, none of the extensions' servers or deny rules. An ordinary one keeps everything.
func TestGuestProcessFlags(t *testing.T) {
	n, _ := testNode(t)
	bin := writeFakeMCPBin(t, t.TempDir(), "example-agent", "Usage: example-agent mcp-server --control")
	t.Setenv("PERSONA_EXAMPLE_AGENT", bin)
	n.larkCfg, n.larkCfgAt = proto.AssistantLark{MCPServers: []proto.AssistantMCPServer{exampleMCP()}}, time.Now().Add(time.Hour)
	n.state.put("guest-1", sessionState{Launcher: "claude", Guest: []string{"lark_agent_reply"}})

	servers := func(mcpJSON string) map[string]map[string]any {
		var cfg struct {
			MCPServers map[string]map[string]any `json:"mcpServers"`
		}
		if err := json.Unmarshal([]byte(mcpJSON), &cfg); err != nil {
			t.Fatal(err)
		}
		return cfg.MCPServers
	}
	flagValue := func(flags []string, name string) (string, bool) {
		i := slices.Index(flags, name)
		if i < 0 || i+1 >= len(flags) {
			return "", false
		}
		return flags[i+1], true
	}

	mcpJSON, flags := n.claudeAssistantMCPAndTools("guest-1", t.TempDir(), true)
	got := servers(mcpJSON)
	if len(got) != 2 || got["persona"] == nil || got[compat.MCPServer] == nil {
		t.Fatalf("servers = %v, want Persona's alone", got)
	}
	env, _ := got["persona"]["env"].(map[string]any)
	if env["PERSONA_GUEST"] != "1" || env["PERSONA_SESSION_ID"] != "guest-1" || env["PERSONA_ASSISTANT"] != "1" {
		t.Fatalf("persona env = %v", env)
	}
	if v, ok := flagValue(flags, "--tools"); !ok || v != "" {
		t.Fatalf("--tools = %q (%v), want no built-in tools: %q", v, ok, flags)
	}
	if v, _ := flagValue(flags, "--allowedTools"); v != strings.Join(compat.MCPToolNames([]string{"lark_agent_reply"}), ",") {
		t.Fatalf("--allowedTools = %q, want only the guest's tool", v)
	}
	if !slices.Contains(flags, "--strict-mcp-config") || slices.Contains(flags, "--disallowedTools") {
		t.Fatalf("flags = %q", flags)
	}

	mcpJSON, flags = n.claudeAssistantMCPAndTools("ordinary-1", t.TempDir(), true)
	if servers(mcpJSON)["example"] == nil || slices.Contains(flags, "--tools") || slices.Contains(flags, "--strict-mcp-config") {
		t.Fatalf("an ordinary conversation changed: %s %q", mcpJSON, flags)
	}
	if v, _ := flagValue(flags, "--allowedTools"); !slices.Contains(strings.Split(v, ","), assistantAllowedTools) {
		t.Fatalf("an ordinary conversation lost %s: %q", assistantAllowedTools, v)
	}
}

// Started as a guest: a new conversation with its tools kept in its state, never the warm spare, its process started
// with the guest's flags; a later message keeps it on what it started on whatever launcher or runtime it names.
func TestGuestConversationStarts(t *testing.T) {
	fakeClaudeHome(t, "for a in \"$@\"; do printf ' [%s]' \"$a\"; done > \"$HOME/args-$$\"\nread line\necho '{\"type\":\"system\",\"subtype\":\"init\"}'\nsleep 30\n")
	n := codexTestNode(t)
	withFakeHub(t, n)
	cwd := t.TempDir()
	if err := n.assistantWarm(cwd, "claude"); err != nil {
		t.Fatal(err)
	}
	n.mu.Lock()
	spare := n.spare
	n.mu.Unlock()
	if spare == nil {
		t.Fatal("no spare to leave alone")
	}
	t.Cleanup(spare.stop)

	id, err := n.assistantSend(withGuest(context.Background(), []string{"lark_agent_reply"}), "", cwd, "claude", "someone asks", nil, "", "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { n.stopAssistant(id) })
	if id == spare.id {
		t.Fatal("a guest conversation took the warm spare")
	}
	n.mu.Lock()
	still := n.spare == spare
	n.mu.Unlock()
	if !still {
		t.Fatal("the spare was handed out")
	}
	if st, _ := n.state.get(id); !slices.Equal(st.Guest, []string{"lark_agent_reply"}) || st.Cwd != cwd {
		t.Fatalf("state = %+v", st)
	}
	var line string
	files, _ := filepath.Glob(filepath.Join(os.Getenv("HOME"), "args-*"))
	for _, f := range files {
		if raw, _ := os.ReadFile(f); strings.Contains(string(raw), "["+id+"]") {
			line = string(raw)
		}
	}
	for _, want := range []string{"[--tools] []", "[--strict-mcp-config]", "[--allowedTools] [" + strings.Join(compat.MCPToolNames([]string{"lark_agent_reply"}), ",") + "]"} {
		if !strings.Contains(line, want) {
			t.Fatalf("the guest's process has no %s: %s", want, line)
		}
	}

	// A later message naming another launcher and the Agent SDK still goes to it as it is.
	if _, err := n.assistantSend(context.Background(), id, "", "codex", "more", nil, "", "", modelExecution{Runtime: proto.AssistantRuntimeAgentSDK}); err != nil {
		t.Fatalf("a later message: %v", err)
	}
	if st, _ := n.state.get(id); st.Launcher != "claude" || st.Runtime != "" || st.Guest == nil {
		t.Fatalf("state after a later message = %+v", st)
	}
}

// Only Claude Code on the CLI runs a guest: the Agent SDK is refused before anything starts.
func TestGuestRefusesTheAgentSDK(t *testing.T) {
	fakeClaudeHome(t, "read line\necho '{\"type\":\"system\",\"subtype\":\"init\"}'\nsleep 30\n")
	n := codexTestNode(t)
	withFakeHub(t, n)
	_, err := n.assistantSend(withGuest(context.Background(), []string{"lark_agent_reply"}), "", t.TempDir(), "claude", "hi", nil, "", "",
		modelExecution{Runtime: proto.AssistantRuntimeAgentSDK})
	if !errors.Is(err, errGuestAgent) {
		t.Fatalf("err = %v, want errGuestAgent", err)
	}
	n.mu.Lock()
	procs := len(n.procs)
	n.mu.Unlock()
	if procs != 0 {
		t.Fatal("a process started")
	}
}

// Its directory holds only its instructions; a path out of the home is refused. Tool names are plain identifiers.
func TestGuestPrepareAndTools(t *testing.T) {
	n, _ := testNode(t)
	out, err := n.assistantGuestPrepare(json.RawMessage(`{"dir":".persona/guest","claude_md":"# guest"}`))
	if err != nil {
		t.Fatal(err)
	}
	cwd := out.(map[string]any)["cwd"].(string)
	if cwd != filepath.Join(n.home, ".persona", "guest") {
		t.Fatalf("cwd = %q", cwd)
	}
	entries, _ := os.ReadDir(cwd)
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	if !slices.Equal(names, []string{"AGENTS.md", "CLAUDE.md"}) {
		t.Fatalf("the directory holds %v", names)
	}
	for _, bad := range []string{`{"dir":"../x"}`, `{"dir":"/tmp/x"}`, `{"dir":""}`} {
		if _, err := n.assistantGuestPrepare(json.RawMessage(bad)); err == nil {
			t.Errorf("%s accepted", bad)
		}
	}
	for _, c := range []struct {
		tools []string
		ok    bool
	}{
		{[]string{"lark_agent_reply"}, true},
		{nil, false},
		{[]string{"lark_agent_reply,Bash"}, false},
		{[]string{"mcp__persona__x"}, true}, // a name; the hub says which are a guest's
		{[]string{"Bash"}, false},
		{[]string{"a", "b", "c", "d", "e", "f", "g", "h", "i"}, false},
	} {
		if err := checkGuestTools(c.tools); (err == nil) != c.ok {
			t.Errorf("%q: err %v, want ok %v", c.tools, err, c.ok)
		}
	}
}

// The node relays its MCP server's calls with the guest's word for it: what the server says, or what the node knows of
// the conversation (a guest's process started before the server said so is one all the same).
func TestGuestRelayTellsTheHub(t *testing.T) {
	n := codexTestNode(t)
	fh := withFakeHub(t, n)
	n.state.put("guest-1", sessionState{Launcher: "claude", Guest: []string{"lark_agent_reply"}})
	relay := func(req map[string]any) {
		t.Helper()
		a, b := net.Pipe()
		go n.serveSock(context.Background(), a)
		line, _ := json.Marshal(req)
		if _, err := b.Write(append(line, '\n')); err != nil {
			t.Fatal(err)
		}
		if _, err := bufio.NewReader(b).ReadBytes('\n'); err != nil {
			t.Fatal(err)
		}
		_ = b.Close()
	}
	relay(map[string]any{"kind": "mcp.tools", "assistant": true, "session_id": "guest-1"})
	relay(map[string]any{"kind": "mcp.call", "assistant": true, "session_id": "guest-1", "name": "lark_agent_reply"})
	relay(map[string]any{"kind": "mcp.tools", "assistant": true, "session_id": "other", "guest": true})
	relay(map[string]any{"kind": "mcp.call", "assistant": true, "session_id": "ordinary", "name": "kb_search"})
	fh.mu.Lock()
	defer fh.mu.Unlock()
	want := []bool{true, true, true, false}
	if len(fh.mcp) != len(want) {
		t.Fatalf("relayed %v", fh.mcp)
	}
	for i, p := range fh.mcp {
		if got, _ := p["guest"].(bool); got != want[i] {
			t.Errorf("%v: guest %v, want %v", p, got, want[i])
		}
	}
}
