package node

import (
	"context"
	"io"
	"log"
	"path/filepath"
	"testing"
	"testing/synctest"

	"github.com/nwverse/persona/server/internal/proto"
)

// Every connection is a new Node with an empty Codex registry; the hub hands it the inventory it keeps for the
// computer (the homes, the app-servers, the threads the user joined) in node.configure. The engine is settled before
// that (engine.use is the first call of the manager's handshake), and a report then would have the hub store the empty
// registry over what it kept. The node's reports wait for node.configure's codex, and the first one after it carries
// the hub's inventory with what the node found meanwhile.
func TestCodexInventoryReportWaitsForTheHubsInventory(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		n, home := testNode(t)
		n.codices = newCodexRegistry()
		n.codices.scan = func() []codexProcess { return nil }
		n.codices.checked = nil
		n.logger = log.New(io.Discard, "", 0)
		n.metas = map[string]quickMeta{}
		n.bgScans = map[string]*bgScan{}
		n.dir = filepath.Join(home, ".persona")
		reports := make(chan proto.CodexConfig, 64)
		n.codices.report = func(c proto.CodexConfig) { reports <- c }
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan struct{})
		go func() { defer close(done); n.runCodex(ctx) }()
		defer func() { cancel(); <-done }()

		n.configured() // engine.use's handler ends with this (node.go "engine.use")
		synctest.Wait()
		select {
		case c := <-reports:
			t.Fatalf("the node reported its Codex inventory before node.configure handed it the hub's: homes=%v joined=%v servers=%v",
				c.Homes, c.Joined, c.Servers)
		default:
		}

		// The manager's node.configure with what the hub kept (devices.connected).
		thread := "c0de-11111111222222223333333344444444"
		work := filepath.Join(home, ".codex-work")
		kept := proto.CodexConfig{Homes: []string{work}, Joined: map[string]string{thread: "one"}}
		if _, err := request(t, n, "node.configure", map[string]any{"machine_id": "box", "codex": kept}); err != nil {
			t.Fatal(err)
		}
		synctest.Wait()
		var first proto.CodexConfig
		select {
		case first = <-reports:
		default:
			t.Fatal("no Codex inventory reported after node.configure handed the hub's")
		}
		if first.Joined[thread] != "one" || len(first.Homes) < 2 {
			t.Fatalf("the first report lost what the hub kept: homes=%v joined=%v", first.Homes, first.Joined)
		}
	})
}
