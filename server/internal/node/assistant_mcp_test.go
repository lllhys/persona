package node

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/proto"
)

func writeFakeMCPBin(t *testing.T, dir, name, helpOut string) string {
	t.Helper()
	bin := filepath.Join(dir, name)
	script := "#!/bin/sh\necho " + shellQuote(helpOut) + "\n"
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return bin
}

// exampleMCP is an extension's MCP server as the hub hands it over (mcp_servers).
func exampleMCP() proto.AssistantMCPServer {
	return proto.AssistantMCPServer{Plugin: "example", ID: "example", Command: "example-agent", Env: "PERSONA_EXAMPLE_AGENT",
		Args: []string{"mcp-server", "--control"}, Requires: []string{"mcp-server", "--control"},
		Allow: []string{"hub_agents", "agent_sessions"}, Deny: []string{"approval_decide", "herdr_prompt"}}
}

func TestAssistantMCPServersNone(t *testing.T) {
	servers, allow, deny := assistantMCPServers(nil)
	if len(servers) != 0 || allow != nil || deny != nil {
		t.Fatalf("got %v / %v / %v", servers, allow, deny)
	}
}

func TestAssistantMCPServersRules(t *testing.T) {
	bin := writeFakeMCPBin(t, t.TempDir(), "example-agent", "Usage: example-agent mcp-server --control")
	t.Setenv("PERSONA_EXAMPLE_AGENT", bin)
	servers, allow, deny := assistantMCPServers([]proto.AssistantMCPServer{exampleMCP()})
	m, _ := servers["example"].(map[string]any)
	args, _ := m["args"].([]string)
	if m["command"] != bin || strings.Join(args, " ") != "mcp-server --control" {
		t.Fatalf("got %v", m)
	}
	if _, ok := m["env"]; ok {
		t.Fatal("no env: the command reads its own config, Persona passes no tokens")
	}
	if !slices.Equal(allow, []string{"mcp__example__hub_agents", "mcp__example__agent_sessions"}) {
		t.Fatalf("allow %v", allow)
	}
	if !slices.Equal(deny, []string{"mcp__example__approval_decide", "mcp__example__herdr_prompt"}) {
		t.Fatalf("deny %v", deny)
	}
	// The assistant's own server name is never taken.
	s := exampleMCP()
	s.ID = "persona"
	if servers, _, _ := assistantMCPServers([]proto.AssistantMCPServer{s}); len(servers) != 0 {
		t.Fatalf("took persona: %v", servers)
	}
}

func TestAssistantMCPBin(t *testing.T) {
	dir := t.TempDir()
	bin := writeFakeMCPBin(t, dir, "example-agent", "Usage: example-agent mcp-server --control")
	oldBin := writeFakeMCPBin(t, t.TempDir(), "example-agent", "Usage: example-agent mcp-server")
	s := exampleMCP()

	// PERSONA_ overrides executable lookup without reading unrelated variables.
	t.Setenv("PERSONA_EXAMPLE_AGENT", bin)
	t.Setenv("OTHER_EXAMPLE_AGENT", filepath.Join(dir, "missing"))
	if got := assistantMCPBin(s); got != bin {
		t.Fatalf("env override: got %q, want %q", got, bin)
	}

	// An explicit empty value turns the server off even when PATH has the command.
	t.Setenv("PERSONA_EXAMPLE_AGENT", "")
	t.Setenv("PATH", dir)
	if got := assistantMCPBin(s); got != "" {
		t.Fatalf("explicit empty should turn off, got %q", got)
	}
	t.Setenv("PERSONA_EXAMPLE_AGENT", "off")
	if got := assistantMCPBin(s); got != "" {
		t.Fatalf("off should turn off, got %q", got)
	}

	// With the override absent, an unrelated variable cannot enable the server.
	os.Unsetenv("PERSONA_EXAMPLE_AGENT")
	t.Setenv("OTHER_EXAMPLE_AGENT", bin)
	t.Setenv("PATH", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	if got := assistantMCPBin(s); got != "" {
		t.Fatalf("unrelated variable enabled server: %q", got)
	}

	// A build whose --help lacks a word of requires is left out.
	t.Setenv("PERSONA_EXAMPLE_AGENT", oldBin)
	if got := assistantMCPBin(s); got != "" {
		t.Fatalf("older build should be left out, got %q", got)
	}
	t.Setenv("PERSONA_EXAMPLE_AGENT", filepath.Join(dir, "missing"))
	if got := assistantMCPBin(s); got != "" {
		t.Fatalf("missing override: got %q", got)
	}

	// Neither set: PATH, then ~/bin.
	os.Unsetenv("PERSONA_EXAMPLE_AGENT")
	t.Setenv("PATH", dir)
	if got := assistantMCPBin(s); got != bin {
		t.Fatalf("PATH: got %q, want %q", got, bin)
	}
	t.Setenv("PATH", filepath.Dir(oldBin))
	t.Setenv("HOME", t.TempDir())
	if got := assistantMCPBin(s); got != "" {
		t.Fatalf("older build on PATH: got %q", got)
	}
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	inHome := writeFakeMCPBin(t, filepath.Join(home, "bin"), "example-agent", "mcp-server --control")
	t.Setenv("PATH", t.TempDir())
	t.Setenv("HOME", home)
	if got := assistantMCPBin(s); got != inHome {
		t.Fatalf("~/bin: got %q, want %q", got, inHome)
	}
	t.Setenv("HOME", t.TempDir())
	if got := assistantMCPBin(s); got != "" {
		t.Fatalf("absent: got %q", got)
	}

	// Without requires any executable will do; a command name with a slash is never looked up.
	s.Requires = nil
	t.Setenv("PATH", filepath.Dir(oldBin))
	if got := assistantMCPBin(s); got != oldBin {
		t.Fatalf("no requires: got %q", got)
	}
	s.Command = "../example-agent"
	if got := assistantMCPBin(s); got != "" {
		t.Fatalf("a path as command: got %q", got)
	}
}

func TestClaudeAssistantMCPAndToolsWiring(t *testing.T) {
	n, _ := testNode(t)
	bin := writeFakeMCPBin(t, t.TempDir(), "example-agent", "Usage: example-agent mcp-server --control")
	t.Setenv("PERSONA_EXAMPLE_AGENT", bin)
	n.larkCfg, n.larkCfgAt = proto.AssistantLark{MCPServers: []proto.AssistantMCPServer{exampleMCP()}}, time.Now().Add(time.Hour)

	servers := func(mcpJSON string) map[string]map[string]any {
		var cfg struct {
			MCPServers map[string]map[string]any `json:"mcpServers"`
		}
		if err := json.Unmarshal([]byte(mcpJSON), &cfg); err != nil {
			t.Fatalf("unmarshal mcp config: %v", err)
		}
		return cfg.MCPServers
	}
	mcpJSON, flags := n.claudeAssistantMCPAndTools("sess-123", "", true)
	got := servers(mcpJSON)
	if _, ok := got["persona"]; !ok {
		t.Fatal("missing persona mcp server")
	}
	if got["example"]["command"] != bin {
		t.Fatalf("example server = %v, want %q", got["example"], bin)
	}
	allowedIdx := slices.Index(flags, "--allowedTools")
	if allowedIdx < 0 || !slices.Contains(strings.Split(flags[allowedIdx+1], ","), "mcp__example__hub_agents") {
		t.Fatalf("--allowedTools: %v", flags)
	}
	disallowedIdx := slices.Index(flags, "--disallowedTools")
	if disallowedIdx < 0 || disallowedIdx+1 >= len(flags) {
		t.Fatalf("missing --disallowedTools in flags: %v", flags)
	}
	if !slices.Contains(strings.Split(flags[disallowedIdx+1], ","), "mcp__example__approval_decide") {
		t.Fatalf("--disallowedTools %v does not contain mcp__example__approval_decide", flags[disallowedIdx+1])
	}

	// Negotiating away the built-in alias must not weaken extension deny rules.
	_, canonicalFlags := n.claudeAssistantMCPAndTools("sess-123", "", false)
	if mcpFlag(t, canonicalFlags, "--disallowedTools") != mcpFlag(t, flags, "--disallowedTools") {
		t.Fatalf("extension deny rules changed: %v", canonicalFlags)
	}
	if !slices.Contains(strings.Split(mcpFlag(t, canonicalFlags, "--allowedTools"), ","), "mcp__example__hub_agents") {
		t.Fatalf("extension allow rules changed: %v", canonicalFlags)
	}

	// Turned off on this machine: no server and no --disallowedTools.
	t.Setenv("PERSONA_EXAMPLE_AGENT", "")
	mcpJSONOff, flagsOff := n.claudeAssistantMCPAndTools("sess-123", "", true)
	if _, ok := servers(mcpJSONOff)["example"]; ok {
		t.Fatal("example server should not be present when turned off")
	}
	if slices.Contains(flagsOff, "--disallowedTools") {
		t.Fatalf("unexpected --disallowedTools when turned off: %v", flagsOff)
	}
}
