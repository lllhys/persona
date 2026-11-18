package node

import (
	"os/exec"
	"strings"
	"testing"
)

// Git Bash gets the main chat's arguments one by one: with Go's own quoting codexArgs' TOML values merged into one
// argument and Codex started without the persona MCP server (#6418).
func TestMSYSCmdLineKeepsCodexArgs(t *testing.T) {
	bash := gitBash()
	if bash == "" {
		t.Skip("no Git Bash")
	}
	want := append(codexArgsForHome(`C:\Users\me\.persona\bin\persona-node.exe`, "proc-1", `C:\Users\me\.persona`, false, 1000),
		"--mcp-config", `{"mcpServers":{"persona":{"command":"C:\\Users\\me\\x.exe","args":["mcp"]}}}`,
		"it's * [a] {b,c} \\q", `C:\a b\`, `\"`, "")
	cmd := exec.Command(bash, append([]string{"-c", `for a in "$@"; do printf '%s\0' "$a"; done`, "persona"}, want...)...)
	msysCmdLine(cmd)
	out, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Split(strings.TrimSuffix(string(out), "\x00"), "\x00")
	if len(got) != len(want) {
		t.Fatalf("got %d args, want %d: %q", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("arg %d = %q, want %q", i, got[i], want[i])
		}
	}
}

// A program other than Git Bash splits its command line like the MSVC runtime: Go's own quoting stays.
func TestMSYSCmdLineOnlyForGitBash(t *testing.T) {
	cmd := exec.Command("cmd.exe", "/c", `{"a":"C:\\b"}`)
	msysCmdLine(cmd)
	if cmd.SysProcAttr != nil && cmd.SysProcAttr.CmdLine != "" {
		t.Fatalf("CmdLine = %q, want Go's quoting", cmd.SysProcAttr.CmdLine)
	}
}
