package node

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/nwverse/persona/server/internal/compat"
	"github.com/nwverse/persona/server/internal/lazyre"
	"os"
	"path/filepath"
	"strings"
)

// Guest conversations (the hub's assistant/guest.go, docs/assistant/assistant.md ("Guest conversations")): an assistant conversation that
// answers someone else for the user. It is started on Claude Code only (no other agent, runtime or the Agent SDK,
// whose built-in tools could not be taken away), never in the warm spare, and every process it runs in (a resume,
// a compaction, a model switch) has no built-in tools (--tools ""), only Persona's MCP server (--strict-mcp-config:
// none of the user's or the extensions'), and only its own tools allowed. The hub lists and runs only those for it
// and gives it no live snapshot; the tools it has are kept in its sessionState.Guest.

// maxGuestTools is the hub's bound (assistant/guest.go).
const maxGuestTools = 8

var guestToolName = lazyre.New(`^[a-z][a-z0-9_]{0,63}$`)

// checkGuestTools: one to maxGuestTools tool names, plain identifiers (they go into --allowedTools).
func checkGuestTools(tools []string) error {
	if len(tools) == 0 || len(tools) > maxGuestTools {
		return fmt.Errorf("a guest conversation takes 1 to %d tools", maxGuestTools)
	}
	for _, t := range tools {
		if !guestToolName.MatchString(t) {
			return fmt.Errorf("bad guest tool %q", t)
		}
	}
	return nil
}

// guestParams is assistant.send's guest: the tools of Persona's it may use.
type guestParams struct {
	Tools []string `json:"tools"`
}

type guestKey struct{}

// withGuest marks ctx's assistant.send as starting a guest conversation with tools.
func withGuest(ctx context.Context, tools []string) context.Context {
	return context.WithValue(ctx, guestKey{}, tools)
}

func guestFrom(ctx context.Context) []string {
	tools, _ := ctx.Value(guestKey{}).([]string)
	return tools
}

// errGuestAgent refuses a guest conversation anything but Claude Code on the CLI.
var errGuestAgent = errors.New("a guest conversation runs on Claude Code only (its built-in tools can be taken away there)")

// assistantGuestPrepare makes a guest conversation's directory and writes only its instructions there.
func (n *Node) assistantGuestPrepare(params json.RawMessage) (any, error) {
	var a struct {
		Dir      string `json:"dir"`
		ClaudeMD string `json:"claude_md"`
	}
	if err := json.Unmarshal(params, &a); err != nil || a.Dir == "" || strings.Contains(a.Dir, "..") || filepath.IsAbs(a.Dir) {
		return nil, errors.New("bad guest dir")
	}
	dir, err := n.preparedDir(a.Dir)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	for _, name := range []string{"CLAUDE.md", "AGENTS.md"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(a.ClaudeMD), 0o600); err != nil {
			return nil, err
		}
	}
	return map[string]any{"cwd": dir}, nil
}

// guestOf is conversation id's guest tools (nil: not a guest).
func (n *Node) guestOf(id string) []string {
	if id == "" {
		return nil
	}
	st, _ := n.state.get(id)
	return st.Guest
}

// guestMCPAndTools are a guest conversation's process flags: Persona's MCP server alone, told it is a guest's, no
// built-in tools, and only its tools allowed.
func (n *Node) guestMCPAndTools(id string, tools []string, legacyMCP bool) (mcpJSON string, toolFlags []string) {
	env := assistantMCPEnv(id, n.dir)
	env["PERSONA_GUEST"] = "1"
	mcp, _ := json.Marshal(map[string]any{"mcpServers": compat.MCPRegistrationServers(map[string]any{"persona": map[string]any{"command": n.bin, "args": []string{"mcp"}, "env": env}}, legacyMCP)})
	allowed := compat.MCPAllowedToolNames(tools, legacyMCP)
	return string(mcp), []string{"--mcp-config", string(mcp), "--strict-mcp-config", "--tools", "", "--allowedTools", strings.Join(allowed, ",")}
}

// assistantDirOf is where a directory the hub names relative to HOME for the assistant (".persona/assistant",
// ".persona/health", ".persona/guest", or the former name's spelling of them, which a hub sends for older nodes) is on
// this node: in the state directory the node runs on (stateRoot), whichever spelling it came in. Joined to HOME as
// written, the former spelling recreated the former directory once its alias was gone (#1681).
func (n *Node) assistantDirOf(rel string) string {
	sub, ok := compat.StateSubpath(filepath.ToSlash(rel))
	if !ok || n.dir == "" {
		return filepath.Join(n.home, rel)
	}
	return filepath.Join(n.stateRoot(), filepath.FromSlash(sub))
}

// stateRoot is the node's state directory as its assistant's sessions run in it. A custom PERSONA_HOME is that root, the
// data_dir the hub reports the assistant's directory in (#5505). On the default layouts it is whichever of ~/.persona and
// the former state directory (compat.StateDir) is a directory rather than an alias of the other and is the node's: an
// installation still kept under the former name keeps the path Claude Code knows its sessions and memory by
// (docs/operations/rename.md), and a migrated one never gets the former directory back. n.dir as it is when it is
// neither (not made yet).
func (n *Node) stateRoot() string {
	dir := filepath.Clean(n.dir)
	if n.customDataDir() {
		return dir
	}
	want, err := os.Stat(dir)
	if err != nil {
		return dir
	}
	for _, name := range []string{".persona", compat.StateDir} {
		p := filepath.Join(n.home, name)
		if st, err := os.Lstat(p); err == nil && st.IsDir() && os.SameFile(st, want) {
			return p
		}
	}
	return dir
}

// preparedDir is assistantDirOf(rel) for assistant.prepare and assistant.guest_prepare, which make it: below the node's
// state directory only, never the state directory itself (#1681). A state directory that is not there (moved or removed
// under a running node) is refused rather than made again empty, which would split the assistant's files in two.
func (n *Node) preparedDir(rel string) (string, error) {
	dir := n.assistantDirOf(rel)
	if _, ok := compat.StateSubpath(filepath.ToSlash(rel)); ok && n.dir != "" {
		root := n.stateRoot()
		if st, err := os.Stat(root); err != nil || !st.IsDir() {
			return "", fmt.Errorf("the state directory %s is not there: not making the assistant's directory in a new one", root)
		}
	}
	return dir, nil
}

// customDataDir: the node's data directory is a root of its own (PERSONA_HOME), not one of the default ones.
func (n *Node) customDataDir() bool {
	if n.dir == "" {
		return false
	}
	d := filepath.Clean(n.dir)
	return d != filepath.Join(n.home, ".persona") && d != filepath.Join(n.home, compat.StateDir)
}
