//go:build herdrlive

package node

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/herdr"
	"github.com/nwverse/persona/server/internal/host"
	"github.com/nwverse/persona/server/internal/rpc"
)

// Against a real herdr: lists its panes, opens a throwaway shell tab (not focused) in the first workspace, streams it
// by its pane address and closes the tab again. It adds and removes one tab on the machine, so it needs a say-so:
// PERSONA_LIVE_PANES=1 go test -tags herdrlive -run TestLivePanes ./internal/node/
func TestLivePanes(t *testing.T) {
	if os.Getenv("PERSONA_LIVE_PANES") != "1" {
		t.Skip("PERSONA_LIVE_PANES=1 opens and closes one tab in herdr")
	}
	client := herdr.New()
	n := &Node{hosts: herdrHosts(client), agents: map[string]host.Agent{}, procs: map[string]*assistantProc{}, state: newState()}
	ctx := context.Background()

	list, err := n.listPanes(ctx)
	if err != nil || len(list.Hosts) != 1 {
		t.Fatalf("list = %+v, %v", list, err)
	}
	h := list.Hosts[0]
	shells := 0
	for _, p := range h.Panes {
		if p.Agent == "" {
			shells++
		}
	}
	t.Logf("herdr: %d workspaces, %d tabs, %d panes (%d without an agent); stream=%v control=%v new_tab=%v",
		len(h.Workspaces), len(h.Tabs), len(h.Panes), shells, h.Stream, h.Control, h.NewTab)
	if len(h.Workspaces) == 0 || shells == 0 {
		t.Fatal("a herdr with workspaces and shells is expected")
	}

	res, err := n.newShell(ctx, host.Herdr, h.Workspaces[0].ID, "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.TabClose(context.Background(), res.TabID) })
	t.Logf("opened %s in tab %s", res.Terminal, res.TabID)

	n2hR, n2hW := io.Pipe()
	h2nR, h2nW := io.Pipe()
	n.peer = rpc.New("n", n2hW)
	hub := rpc.New("h", h2nW)
	var mu sync.Mutex
	frames := 0
	hub.OnEvent = func(event string, params json.RawMessage) {
		if event == "terminal.frame" {
			mu.Lock()
			frames++
			mu.Unlock()
		}
	}
	sctx, cancel := context.WithCancel(ctx)
	defer cancel()
	go func() { _ = hub.Serve(sctx, n2hR) }()
	go func() { _ = n.peer.Serve(sctx, h2nR) }()
	t.Cleanup(func() { cancel(); _ = n2hW.Close(); _ = h2nW.Close() })

	got, err := n.terminalWatch(ctx, res.Terminal, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("watch = %v", got)
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		k := frames
		mu.Unlock()
		if k > 0 {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	n.terminalUnwatch(res.Terminal)
	mu.Lock()
	defer mu.Unlock()
	if frames == 0 {
		t.Fatal("no frame from the new shell")
	}
	t.Logf("%d frame(s) from the shell's stream", frames)
}
