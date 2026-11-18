package node

import (
	"context"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/nwverse/persona/server/internal/host"
	"github.com/nwverse/persona/server/internal/proto"
)

// The fixture must stop checks it owns before closing their socket and restoring
// package settings. Delivery tests still await firstWait explicitly before cleanup.
func TestHerdrFixtureCancelsUnfinishedPromptChecks(t *testing.T) {
	var n *Node
	t.Run("unfinished delivery", func(t *testing.T) {
		var f *fakeHerdr
		n, f, _ = herdrNode(t)
		reading := make(chan struct{})
		var once sync.Once
		f.after(func(c herdrCall) {
			if c.Method == "pane.read" {
				once.Do(func() { close(reading) })
			}
		})
		if err := n.firstPrompt(context.Background(), "w1:p2", startID, "fixture cleanup", &host.Agent{AgentStatus: proto.StatusIdle}, ""); err != nil {
			t.Fatal(err)
		}
		select { // observe an active check, rather than hoping it has started
		case <-reading:
		case <-time.After(5 * time.Second):
			t.Fatal("the prompt check never read its pane")
		}
	})
	lost, ok := n.lostFirst.get(startID)
	if !ok || lost.why != lostTimeout {
		t.Fatalf("unfinished check was not cancelled at fixture shutdown: %+v", lost)
	}
}

// Normal starts remain detached from the request that submitted them. A later
// receipt must still finish the check after that request has returned.
func TestFirstPromptStillOutlivesItsRequest(t *testing.T) {
	n, _, home := herdrNode(t)
	n.firstPromptParent = nil
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := n.firstPrompt(ctx, "w1:p2", startID, "later receipt", &host.Agent{AgentStatus: proto.StatusIdle}, ""); err != nil {
		t.Fatal(err)
	}
	cancel()
	writeUserLine(t, home, home, startID, "later receipt")
	n.firstWait.Wait()
	if lost, ok := n.lostFirst.get(startID); ok {
		t.Fatalf("request cancellation ended a detached delivery check: %+v", lost)
	}
}

// Advancing a fake clock exposes an unnecessary wait without a flaky wall-time budget.
func TestFixtureShellIsAlreadySettled(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		n, _ := testNode(t)
		began := time.Now()
		if err := n.settleShell(context.Background(), "unused"); err != nil {
			t.Fatal(err)
		}
		if elapsed := time.Since(began); elapsed != 0 {
			t.Fatalf("an already settled fixture shell waited %v", elapsed)
		}
	})
}
