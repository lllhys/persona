package node

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/host"
	"github.com/nwverse/persona/server/internal/proto"
)

// Two panes show the same title, which names an indexed thread one of them already runs. The other pane may not stay
// on its stale reported thread: that would list two different conversations under the same current title.
func TestCodexPaneTitleAlreadyClaimedDoesNotKeepStaleThread(t *testing.T) {
	n, home := testNode(t)
	n.metas, n.bgScans, n.panes, n.panePIDs = map[string]quickMeta{}, map[string]*bgScan{}, map[string]string{}, map[string]panePID{}
	const (
		reported = "0a000000-0000-4000-8000-000000000051"
		claimed  = "0a000000-0000-4000-8000-000000000052"
		cwd      = "/work/repo"
	)
	now := time.Now()
	writeRollout(t, home, reported, cwd, now, now, event("task_started"), event("task_complete"))
	writeRollout(t, home, claimed, cwd, now, now.Add(-time.Minute), event("task_started"), event("task_complete"))
	index := `{"id":"` + claimed + `","thread_name":"Already open"}` + "\n"
	if err := os.WriteFile(filepath.Join(home, ".codex", "session_index.jsonl"), []byte(index), 0o644); err != nil {
		t.Fatal(err)
	}
	openPane := host.Agent{Agent: "codex", PaneID: "w4:p1", TabID: "w4:t1", Cwd: cwd, ForegroundCwd: cwd,
		Title: "Already open | repo", AgentStatus: proto.StatusIdle, AgentSession: &host.AgentSession{Kind: "id", Value: claimed}}
	resumedPane := host.Agent{Agent: "codex", PaneID: "w4:p2", TabID: "w4:t2", Cwd: cwd, ForegroundCwd: cwd,
		Title: "Already open | repo", AgentStatus: proto.StatusIdle, AgentSession: &host.AgentSession{Kind: "id", Value: reported}}
	n.setAgents([]host.Agent{openPane, resumedPane})
	if a, ok := n.agents[reported]; ok {
		t.Fatalf("pane whose title names another pane's thread stayed on reported thread %s: %+v; all agents: %+v", reported, a, n.agents)
	}
	if a, ok := n.agents[claimed]; !ok || a.PaneID != openPane.PaneID {
		t.Fatalf("the titled thread lost its original pane: %+v", n.agents)
	}
}
