package node

import (
	"testing"

	"github.com/nwverse/persona/server/internal/assistantwire"
	"github.com/nwverse/persona/server/internal/host"
)

// An agent's MCP call names no session: the node takes the one its pane runs as the host shows it now, else as the
// hooks bound it; a stale binding does not win over the live agent.
func TestPaneCaller(t *testing.T) {
	n := &Node{panes: map[string]string{"w1:p1": "sess-hook", "w1:p2": "sess-stale"},
		agents: map[string]host.Agent{"sess-host": {PaneID: "w1:p2"}}}
	for pane, want := range map[string]string{"w1:p1": "sess-hook", "w1:p2": "sess-host", "w1:p3": "", "": ""} {
		if got := n.paneCaller(pane); got != want {
			t.Errorf("paneCaller(%q) = %q, want %q", pane, got, want)
		}
	}
}

// The agents' own tools that act for their caller get the pane's session; others, an assistant's call and a call
// that names its session go as they are.
func TestMCPCaller(t *testing.T) {
	n := &Node{panes: map[string]string{"w1:p1": "sess-pane"}}
	for _, c := range []struct {
		name, session string
		assistant     bool
		want          string
	}{
		{"report_to_supervisor", "", false, "sess-pane"},
		{"todo_decisions", "", false, "sess-pane"},
		{"todo_decision_add", "", false, "sess-pane"},
		{"todo_bind_thread", "", false, "sess-pane"},
		{"dispatch_start", "", false, "sess-pane"},
		{"dispatch_read", "", false, "sess-pane"},
		{"task_plan", "", false, "sess-pane"},
		{"task_review", "", false, "sess-pane"},
		{"task_spawn", "", false, "sess-pane"},
		{"todo_repositories_get", "", false, "sess-pane"},
		{"todo_repositories_set", "", false, "sess-pane"},
		{"todo_repository_links_set", "", false, "sess-pane"},
		{"todo_repository_links_add", "", false, "sess-pane"},
		{"task_plan", "", true, ""},
		{"todo_repository_links_add", "", true, ""},
		{"kb_search", "", false, ""},
		{"todo_decisions", "", true, ""},
		{"todo_bind_thread", "", true, ""},
		{"todo_decisions", "sess-named", false, "sess-named"},
	} {
		if got := n.mcpCaller(c.name, c.session, "w1:p1", c.assistant); got != c.want {
			t.Errorf("mcpCaller(%q, %q, assistant=%v) = %q, want %q", c.name, c.session, c.assistant, got, c.want)
		}
	}
}

// Every tool the manager gives the agents acts for the agent calling it, so the node names the pane's session for each
// one, the to-do's repository tools included (#9089: they were missing, and a task agent's todo_repositories_get came
// back "Persona could not tell which agent session is calling"). A pane no agent runs in still names nobody: the hub
// refuses that call as before.
func TestMCPCallerNamesEveryAgentTool(t *testing.T) {
	n := &Node{panes: map[string]string{"w1:p1": "sess-pane"}}
	if len(assistantwire.AgentToolNames) == 0 {
		t.Fatal("no agent tools")
	}
	for _, name := range assistantwire.AgentToolNames {
		if got := n.mcpCaller(name, "", "w1:p1", false); got != "sess-pane" {
			t.Errorf("mcpCaller(%q) from the agent's pane = %q, want its session", name, got)
		}
		if got := n.mcpCaller(name, "", "w9:p9", false); got != "" {
			t.Errorf("mcpCaller(%q) from a pane no agent runs in = %q, want none", name, got)
		}
	}
}

// An agent's tool list is asked for by its pane: the hub tells a task leader's tools from any other agent's by the
// session the pane runs. A session the request names is left as it is, and so is a pane no agent runs in.
func TestMCPToolsNameTheAgentOfThePane(t *testing.T) {
	n, hub := runtimeNode(t)
	n.mu.Lock()
	n.panes = map[string]string{"w1:p1": "sess-pane"}
	n.mu.Unlock()
	ask := mcpRelay(t, n, hub)
	check := func(req map[string]any, want string) {
		t.Helper()
		if got := ask(req); got["session_id"] != want {
			t.Errorf("%v: relayed session %v, want %q", req, got["session_id"], want)
		}
	}
	check(map[string]any{"kind": "mcp.tools", "pane": "w1:p1"}, "sess-pane")
	check(map[string]any{"kind": "mcp.tools", "pane": "w1:p1", "session_id": "named"}, "named")
	check(map[string]any{"kind": "mcp.tools", "pane": "w9:p9"}, "")
	// A call of a to-do repository tool goes to the hub as the pane's agent's (#9089).
	repo := map[string]any{"kind": "mcp.call", "name": "todo_repositories_get", "args": map[string]any{"id": "todo_1"}, "pane": "w1:p1"}
	check(repo, "sess-pane")
}
