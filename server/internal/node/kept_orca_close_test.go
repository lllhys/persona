package node

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/nwverse/persona/server/internal/host"
	"github.com/nwverse/persona/server/internal/orcart"
	"github.com/nwverse/persona/server/internal/orcart/orcarttest"
)

// A recovery during the closer's terminal.list must protect the original pane, including when the first close
// found a genuinely stale handle and onTerminal is refreshing it. Both session.start entry points use this path.
func TestOrcaKeptCloseRechecksAfterLookup(t *testing.T) {
	for _, stale := range []bool{false, true} {
		for _, async := range []bool{false, true} {
			t.Run(fmt.Sprintf("stale=%v/async=%v", stale, async), func(t *testing.T) {
				n, h, o, c, root := keptOrcaStart(t, true)
				until, _ := isKept(n, c.Pane.String())
				h.mu.Lock()
				original := h.made[c.Pane.ID]
				h.terms = nil // make the first close look up its terminal
				if stale {
					h.terms = map[string]orcart.Terminal{c.Pane.ID: original}
				}
				h.mu.Unlock()
				handle := original.Handle
				if stale {
					handle = "term_recovered"
				}
				term := orcarttest.Terminal(handle, original.TabID, original.LeafID, "wt-proj", root, "refs/heads/main", "kept", "")
				term["incarnationId"] = original.IncarnationID
				// The old handle really is absent: the fake's default close returns stale before any override runs.
				o.SetTerminals(term)
				var looked, held atomic.Bool
				o.Handle("terminal.list", func(map[string]any) any {
					o.Handle("terminal.list", nil)
					n.mu.Lock()
					held.Store(n.keptStarts[c.Pane.String()].taken)
					n.mu.Unlock()
					n.bindPane(c.Pane.String(), sidA)
					looked.Store(true)
					return map[string]any{"terminals": []any{term}, "totalCount": 1, "truncated": false}
				})
				_, err := request(t, n, "session.start", map[string]any{"session_id": startID, "cwd": root, "in_pane": c.Pane.String(), "async": async})
				if async && err == nil {
					err = waitAsyncStart(t, n)
				}
				if !looked.Load() || !held.Load() {
					t.Fatalf("the closer did not look up a claimed tab: looked %v, held %v, err %v", looked.Load(), held.Load(), err)
				}
				var closes []string
				for _, call := range o.Trace("terminal.close") {
					if strings.Contains(call, `"`+original.Handle+`"`) || strings.Contains(call, `"`+handle+`"`) {
						closes = append(closes, call)
					}
				}
				wantCloses := 0
				if stale {
					wantCloses = 1 // the old, unknown handle; no request to the refreshed terminal
				}
				if len(closes) != wantCloses || stale && !strings.Contains(closes[0], `"`+original.Handle+`"`) {
					t.Errorf("closed a recovered terminal: %v", closes)
				}
				for _, w := range o.Writes() {
					if w.Close && (w.Handle == original.Handle || w.Handle == handle) {
						t.Errorf("a close reached a live terminal: %+v", w)
					}
				}
				if got := startLineIn(o); got == "" || got == handle || got == original.Handle {
					t.Errorf("the retry did not launch in a fresh tab: handle %q, err %v", got, err)
				}
				if got := o.Count("terminal.create"); got != 2 {
					t.Errorf("created %d terminals, want original plus fresh retry", got)
				}
				if after, kept := isKept(n, c.Pane.String()); !kept || !after.Equal(until) {
					t.Errorf("recovery did not release the original expiry: kept %v, until %v, was %v", kept, after, until)
				}
				n.mu.Lock()
				bound := n.panes[c.Pane.String()]
				n.mu.Unlock()
				if bound != sidA {
					t.Errorf("lost recovered binding: %q", bound)
				}
				// Exercise the existing expiry callback: a released, bound pane remains open at expiry as well.
				beforeExpiry := len(o.Trace("terminal.close"))
				n.expireKeptStart(c.Pane.String(), until)
				if got := len(o.Trace("terminal.close")); got != beforeExpiry {
					t.Errorf("expiry closed the recovered pane: %v", o.Trace("terminal.close"))
				}
			})
		}
	}
}

// A cached target needs the same final check. Neither a hook binding nor an agent-inventory observation may be
// closed; the latter has no n.panes entry. Calling the closer directly isolates its check from tab's earlier check.
func TestOrcaKeptCloseRechecksCachedTerminal(t *testing.T) {
	for _, inventory := range []bool{false, true} {
		t.Run(fmt.Sprintf("inventory=%v", inventory), func(t *testing.T) {
			n, h, o, created, _ := keptOrcaStart(t, true)
			claim, err := n.claimKeptTab(created.Pane.String())
			if err != nil || claim == nil {
				t.Fatalf("claim: %v", err)
			}
			defer claim.release()
			h.mu.Lock()
			original := h.made[created.Pane.ID]
			h.terms = map[string]orcart.Terminal{created.Pane.ID: original}
			h.mu.Unlock()
			if inventory {
				n.mu.Lock()
				n.agents[sidA] = host.Agent{Agent: "claude", PaneID: created.Pane.String()}
				n.mu.Unlock()
			} else {
				n.bindPane(created.Pane.String(), sidA)
			}
			listed := o.Count("terminal.list")
			if err := claim.closeStalledTab(context.Background()); err == nil {
				t.Error("closing an occupied cached terminal was not refused")
			}
			if got := o.Trace("terminal.close"); len(got) != 0 {
				t.Errorf("closed an occupied cached terminal: %v", got)
			}
			if got := o.Count("terminal.list"); got != listed {
				t.Errorf("cached close unexpectedly listed terminals: %d, was %d", got, listed)
			}
			h.mu.Lock()
			_, tracked := h.made[created.Pane.ID]
			h.mu.Unlock()
			if !tracked {
				t.Error("a refused close removed creation tracking")
			}
		})
	}
}

// A transport failure after resolving the cached terminal is a real close failure, not a recovery refusal. The
// failed start must release its original claim and expiry, open no second tab, and let a later retry close it.
func TestOrcaKeptCloseFailureReleasesClaim(t *testing.T) {
	n, h, o, created, _ := keptOrcaStart(t, true)
	until, _ := isKept(n, created.Pane.String())
	h.mu.Lock()
	original := h.made[created.Pane.ID]
	h.terms = map[string]orcart.Terminal{created.Pane.ID: original}
	h.mu.Unlock()
	claim, err := n.claimKeptTab(created.Pane.String())
	if err != nil || claim == nil {
		t.Fatalf("claim: %v", err)
	}
	defer claim.release()
	o.Stop()
	if got, err := claim.tab(context.Background()); got != nil || err == nil || errors.Is(err, errKeptTabUnavailable) {
		t.Fatalf("failed close: tab %v, err %v", got, err)
	}
	if after, kept := isKept(n, created.Pane.String()); !kept || !after.Equal(until) {
		t.Fatalf("failed close did not release the original expiry: kept %v, until %v, was %v", kept, after, until)
	}
	if got := o.Count("terminal.create"); got != 1 {
		t.Fatalf("failed close created another tab: %d", got)
	}
	h.mu.Lock()
	_, tracked := h.made[created.Pane.ID]
	h.mu.Unlock()
	if !tracked {
		t.Error("failed close removed creation tracking")
	}
	if err := o.Start(); err != nil {
		t.Fatal(err)
	}
	next, err := n.claimKeptTab(created.Pane.String())
	if err != nil || next == nil {
		t.Fatalf("claim after failed close: %v", err)
	}
	defer next.release()
	if got, err := next.tab(context.Background()); got != nil || err != nil {
		t.Fatalf("later close: tab %v, err %v", got, err)
	}
	n.mu.Lock()
	_, kept := n.keptStarts[created.Pane.String()]
	n.mu.Unlock()
	h.mu.Lock()
	_, tracked = h.made[created.Pane.ID]
	h.mu.Unlock()
	if kept || tracked || o.Count("terminal.close") != 1 {
		t.Errorf("successful later close did not drop the tab: kept %v, tracked %v, closes %v", kept, tracked, o.Trace("terminal.close"))
	}
}
