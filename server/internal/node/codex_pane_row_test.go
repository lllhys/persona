package node

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/host"
	"github.com/nwverse/persona/server/internal/proto"
)

// A Codex started in a herdr pane, joined on the app-server that runs its thread (#7538). herdr names the pane's
// session; once Codex puts a name in the pane's title that session_index.jsonl does not have, the pane was mapped to
// no thread at all, and the thread was listed only as its app-server's row: no pane, no terminal.

// rowsOf are the listed rows of thread id.
func rowsOf(n *Node, id string) []proto.Session {
	var out []proto.Session
	for _, s := range n.sessionList(context.Background(), false) {
		if s.SessionID == id || s.SourceThreadID == id {
			out = append(out, s)
		}
	}
	return out
}

func TestCodexPaneKeepsItsRowUnderATitleTheIndexDoesNotName(t *testing.T) {
	for _, title := range []string{"合成会话 | repo", "[ . ] Action Required | 合成会话 | repo"} {
		t.Run(title, func(t *testing.T) {
			n := remoteTestNode(t)
			n.panes, n.panePIDs = map[string]string{}, map[string]panePID{}
			home := canonicalCodexHome(filepath.Join(n.home, ".codex"), n.home)
			const (
				cwd    = "/work/repo"
				claude = "0c000000-0000-4000-8000-000000000001"
			)
			now := time.Now()
			writeRollout(t, n.home, remoteRaw, cwd, now, now, event("task_started"), event("task_complete"))
			writeRollout(t, n.home, remoteOther, cwd, now, now.Add(-time.Minute), event("task_started"), event("task_complete"))
			// One app-server runs both threads: the pane's, and one no pane shows (the ChatGPT app's, say).
			connectRemoteFixture(t, n, home, "discovered-09b03a5e", remoteRaw, remoteOther)
			codex := host.Agent{Agent: "codex", PaneID: "w4:p1", TabID: "w4:t1", WorkspaceID: "w4", Cwd: cwd, ForegroundCwd: cwd,
				Title: "repo", AgentStatus: proto.StatusIdle, AgentSession: &host.AgentSession{Kind: "id", Value: remoteRaw}}
			other := host.Agent{Agent: "claude", PaneID: "w4:p2", TabID: "w4:t2", WorkspaceID: "w4", Cwd: cwd,
				Title: "Fix the build | repo", AgentStatus: proto.StatusIdle, AgentSession: &host.AgentSession{Kind: "id", Value: claude}}
			n.setAgents([]host.Agent{codex, other})
			n.joinCodexPanes(context.Background())
			waitFor(t, "joined", func() bool { return n.codexJoined(remoteRaw) })

			codex.Title = title // Codex names the thread: in the title, not in session_index.jsonl
			n.setAgents([]host.Agent{codex, other})

			rows := rowsOf(n, remoteRaw)
			if len(rows) != 1 {
				t.Fatalf("%d rows for the pane's thread, want 1: %+v", len(rows), rows)
			}
			s := rows[0]
			if s.PaneID != "w4:p1" || s.TabID != "w4:t1" || s.SourceKind != "" || s.CodexRoute != codexRouteAppServer {
				t.Errorf("the pane's thread is listed with pane %q, tab %q, source %q, route %q; want its pane's row on the app-server route",
					s.PaneID, s.TabID, s.SourceKind, s.CodexRoute)
			}
			if s.Capabilities == nil || !s.Capabilities.Terminal || !s.Capabilities.Send || !s.Capabilities.Approve {
				t.Errorf("capabilities %+v, want terminal, send and approve", s.Capabilities)
			}

			// A thread no pane shows stays its app-server's row.
			rows = rowsOf(n, remoteOther)
			if len(rows) != 1 {
				t.Fatalf("%d rows for the thread with no pane, want 1: %+v", len(rows), rows)
			}
			if s := rows[0]; s.SourceKind != "codex_app_server" || s.PaneID != "" || s.Capabilities == nil || s.Capabilities.Terminal {
				t.Errorf("the thread with no pane: source %q, pane %q, capabilities %+v", s.SourceKind, s.PaneID, s.Capabilities)
			}

			// A Claude Code pane is its session's row whatever its title says.
			rows = rowsOf(n, claude)
			if len(rows) != 1 || rows[0].PaneID != "w4:p2" || rows[0].Agent != "claude" {
				t.Errorf("the Claude Code pane: %+v", rows)
			}
		})
	}
}

// The thread herdr named is known under another name and the title names none the index has: a /resume in Codex
// since, to a thread that cannot be told. The pane takes neither the thread herdr named nor another from its directory.
func TestCodexPaneUnderAnotherNameThanItsReportedThreadsIsLeftOut(t *testing.T) {
	n, home := testNode(t)
	n.metas, n.bgScans, n.panes, n.panePIDs = map[string]quickMeta{}, map[string]*bgScan{}, map[string]string{}, map[string]panePID{}
	const (
		started = "0a000000-0000-4000-8000-000000000011"
		cwd     = "/work/repo"
	)
	now := time.Now()
	writeRollout(t, home, started, cwd, now, now, event("task_started"), event("task_complete"))
	index := `{"id":"` + started + `","thread_name":"早先的线程"}` + "\n"
	if err := os.WriteFile(filepath.Join(home, ".codex", "session_index.jsonl"), []byte(index), 0o644); err != nil {
		t.Fatal(err)
	}
	pane := host.Agent{Agent: "codex", PaneID: "w4:p1", Cwd: cwd, ForegroundCwd: cwd, AgentStatus: proto.StatusIdle,
		Title: "别的线程 | repo", AgentSession: &host.AgentSession{Kind: "id", Value: started}}
	n.setAgents([]host.Agent{pane})
	if len(n.agents) != 0 {
		t.Fatalf("the pane shows %v", n.agents)
	}
	pane.Title = "早先的线程 | repo" // its own name: the thread herdr named
	n.setAgents([]host.Agent{pane})
	if a, ok := n.agents[started]; !ok || a.PaneID != "w4:p1" {
		t.Fatalf("the pane shows %v, want %s", n.agents, started)
	}
}

// herdr names a subagent's thread, which stands for its conversation, and the conversation has a name in the index.
// The title is not that name (the user's own, or the subagent's): the name tells nothing of a thread herdr did not
// name, so the pane stays its conversation's row.
func TestCodexPaneReportingASubagentKeepsItsConversationsRowUnderAnotherTitle(t *testing.T) {
	n, home := testNode(t)
	n.metas, n.bgScans, n.panes, n.panePIDs = map[string]quickMeta{}, map[string]*bgScan{}, map[string]string{}, map[string]panePID{}
	const (
		parent = "0a000000-0000-4000-8000-000000000021"
		sub    = "0a000000-0000-4000-8000-000000000022"
		cwd    = "/work/repo"
	)
	now := time.Now()
	writeRollout(t, home, parent, cwd, now, now.Add(-time.Minute), event("task_started"), event("task_complete"))
	writeMetaRollout(t, filepath.Join(home, ".codex"), sub, `{"id":"`+sub+`","session_id":"`+parent+`","parent_thread_id":"`+parent+
		`","cwd":"`+cwd+`","source":{"subagent":{"thread_spawn":{"parent_thread_id":"`+parent+`","depth":1}}},"thread_source":"subagent"}`, now)
	index := `{"id":"` + parent + `","thread_name":"主会话的名字"}` + "\n"
	if err := os.WriteFile(filepath.Join(home, ".codex", "session_index.jsonl"), []byte(index), 0o644); err != nil {
		t.Fatal(err)
	}
	pane := host.Agent{Agent: "codex", PaneID: "w4:p1", TabID: "w4:t1", Cwd: cwd, ForegroundCwd: cwd, AgentStatus: proto.StatusIdle,
		Title: "自己起的标题 | repo", AgentSession: &host.AgentSession{Kind: "id", Value: sub}}
	n.setAgents([]host.Agent{pane})
	if a, ok := n.agents[parent]; !ok || a.PaneID != "w4:p1" || len(n.agents) != 1 {
		t.Fatalf("the pane shows %v, want its conversation %s", n.agents, parent)
	}
	rows := rowsOf(n, parent)
	if len(rows) != 1 || rows[0].PaneID != "w4:p1" || rows[0].TabID != "w4:t1" {
		t.Fatalf("the conversation is listed as %+v, want its pane's row", rows)
	}
}

// The thread was renamed a minute after it started (Persona's rename goes to the app-server's thread/name/set), so
// session_index.jsonl holds two lines for it and the node keeps the latest name. herdr names the thread; the pane's
// title shows the earlier name, or the latest one with full-width bars in it. Either way the pane is the thread's row.
func TestCodexPaneOfARenamedThreadKeepsItsRow(t *testing.T) {
	for _, title := range []string{
		"调研 合成浏览器操控能力 | repo",    // the name the thread was given first, which the title kept
		"1114｜调研｜合成浏览器适配 | repo", // the latest name, with full-width bars
		"[ . ] Action Required | 调研 合成浏览器操控能力 | repo",
		"调研 合成浏览器... | repo", // the earlier name cut short
	} {
		t.Run(title, func(t *testing.T) {
			n, home := testNode(t)
			n.metas, n.bgScans, n.panes, n.panePIDs = map[string]quickMeta{}, map[string]*bgScan{}, map[string]string{}, map[string]panePID{}
			const (
				thread = "0a000000-0000-4000-8000-000000000031"
				cwd    = "/work/repo"
			)
			now := time.Now()
			writeRollout(t, home, thread, cwd, now, now, event("task_started"), event("task_complete"))
			index := `{"id":"` + thread + `","thread_name":"调研 合成浏览器操控能力","updated_at":"2026-11-14T07:07:54Z"}` + "\n" +
				`{"id":"` + thread + `","thread_name":"1114｜调研｜合成浏览器适配","updated_at":"2026-11-14T07:08:56Z"}` + "\n"
			if err := os.WriteFile(filepath.Join(home, ".codex", "session_index.jsonl"), []byte(index), 0o644); err != nil {
				t.Fatal(err)
			}
			pane := host.Agent{Agent: "codex", PaneID: "w4:p1", TabID: "w4:t1", Cwd: cwd, ForegroundCwd: cwd, AgentStatus: proto.StatusIdle,
				Title: title, AgentSession: &host.AgentSession{Kind: "id", Value: thread}}
			n.setAgents([]host.Agent{pane})
			if a, ok := n.agents[thread]; !ok || a.PaneID != "w4:p1" || len(n.agents) != 1 {
				t.Fatalf("the pane shows %v, want %s", n.agents, thread)
			}
			rows := rowsOf(n, thread)
			if len(rows) != 1 || rows[0].PaneID != "w4:p1" || rows[0].TabID != "w4:t1" {
				t.Fatalf("the thread is listed as %+v, want its pane's row", rows)
			}
		})
	}
}

// A name the reported thread never had is still no name of it: the /resume rule holds for a renamed thread too.
func TestCodexPaneOfARenamedThreadUnderANameItNeverHadIsLeftOut(t *testing.T) {
	n, home := testNode(t)
	n.metas, n.bgScans, n.panes, n.panePIDs = map[string]quickMeta{}, map[string]*bgScan{}, map[string]string{}, map[string]panePID{}
	const (
		thread = "0a000000-0000-4000-8000-000000000041"
		cwd    = "/work/repo"
	)
	now := time.Now()
	writeRollout(t, home, thread, cwd, now, now, event("task_started"), event("task_complete"))
	index := `{"id":"` + thread + `","thread_name":"先前的名字"}` + "\n" + `{"id":"` + thread + `","thread_name":"后来的名字"}` + "\n"
	if err := os.WriteFile(filepath.Join(home, ".codex", "session_index.jsonl"), []byte(index), 0o644); err != nil {
		t.Fatal(err)
	}
	pane := host.Agent{Agent: "codex", PaneID: "w4:p1", Cwd: cwd, ForegroundCwd: cwd, AgentStatus: proto.StatusIdle,
		Title: "别的线程 | repo", AgentSession: &host.AgentSession{Kind: "id", Value: thread}}
	n.setAgents([]host.Agent{pane})
	if len(n.agents) != 0 {
		t.Fatalf("the pane shows %v", n.agents)
	}
}

// The reporter's setup: the renamed thread is joined on the app-server that runs it, and the title kept the first
// name. The pane is the thread's one row, on the app-server route, with its terminal.
func TestCodexPaneOfARenamedJoinedThreadKeepsItsRow(t *testing.T) {
	n := remoteTestNode(t)
	n.panes, n.panePIDs = map[string]string{}, map[string]panePID{}
	home := canonicalCodexHome(filepath.Join(n.home, ".codex"), n.home)
	const cwd = "/work/repo"
	now := time.Now()
	writeRollout(t, n.home, remoteRaw, cwd, now, now, event("task_started"), event("task_complete"))
	connectRemoteFixture(t, n, home, "discovered-09b03a5e", remoteRaw)
	codex := host.Agent{Agent: "codex", PaneID: "w4:p1", TabID: "w4:t1", WorkspaceID: "w4", Cwd: cwd, ForegroundCwd: cwd,
		Title: "repo", AgentStatus: proto.StatusIdle, AgentSession: &host.AgentSession{Kind: "id", Value: remoteRaw}}
	n.setAgents([]host.Agent{codex})
	n.joinCodexPanes(context.Background())
	waitFor(t, "joined", func() bool { return n.codexJoined(remoteRaw) })

	index := `{"id":"` + remoteRaw + `","thread_name":"调研 合成浏览器操控能力"}` + "\n" +
		`{"id":"` + remoteRaw + `","thread_name":"1114｜调研｜合成浏览器适配"}` + "\n"
	if err := os.WriteFile(filepath.Join(home, "session_index.jsonl"), []byte(index), 0o644); err != nil {
		t.Fatal(err)
	}
	codex.Title = "调研 合成浏览器操控能力 | repo"
	n.setAgents([]host.Agent{codex})

	rows := rowsOf(n, remoteRaw)
	if len(rows) != 1 {
		t.Fatalf("%d rows for the pane's thread, want 1: %+v", len(rows), rows)
	}
	if s := rows[0]; s.PaneID != "w4:p1" || s.SourceKind != "" || s.CodexRoute != codexRouteAppServer ||
		s.Capabilities == nil || !s.Capabilities.Terminal {
		t.Errorf("the pane's thread: pane %q, source %q, route %q, capabilities %+v; want its pane's row with a terminal",
			s.PaneID, s.SourceKind, s.CodexRoute, s.Capabilities)
	}
}
