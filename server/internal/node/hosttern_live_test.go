//go:build ternlive

package node

// A live check against the user's running Tern (go test -tags ternlive -run TernLive ./internal/node/ -v): it opens a
// session of its own that no window shows, drives it the way a node does — listing, colored reads, typing, keys, a
// prompt refused while only a shell runs, the event stream, renames, a second tab — and closes it again, which ends
// the session. The user's own sessions are only counted, never printed.

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/host"
	"github.com/nwverse/persona/server/internal/terncli"
)

func TestTernLive(t *testing.T) {
	c := terncli.New()
	if c.Bin() == "" {
		t.Skip("tern not installed")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	if _, err := c.Inspect(ctx, ""); err != nil {
		t.Skipf("tern not running: %v", err)
	}
	h := newTernHost(c)
	t.Logf("tern %s", h.Version(ctx))

	dir := t.TempDir()
	cr, err := h.CreateSpace(ctx, dir, "persona-ternlive")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.KillSession(context.Background(), "", terncli.ID(cr.SpaceID)) }) // whatever a step left
	pane := cr.Pane

	l, err := h.Snapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	listed := false
	for _, p := range l.Panes {
		if p.PaneID == pane.String() {
			listed = p.Agent == "" && p.WorkspaceID == cr.SpaceID && p.TabID == cr.SlotID
		}
	}
	if !listed {
		t.Fatalf("new block %s not listed as a shell pane of %s/%s", pane, cr.SpaceID, cr.SlotID)
	}
	t.Logf("listed %d spaces, %d slots, %d terminal panes, %d agents (the user's included)", len(l.Spaces), len(l.Slots),
		len(l.Panes), len(l.Agents))

	// The event stream goes live and reports the tab opened below.
	var events atomic.Int32
	wctx, wcancel := context.WithCancel(ctx)
	defer wcancel()
	go func() { _ = h.Watch(wctx, nil, func(bool) { events.Add(1) }) }()

	if err := h.Run(ctx, pane, `printf '\033[32mPERSONA-LIVE\033[0m\n'`); err != nil {
		t.Fatal(err)
	}
	var screen string
	for deadline := time.Now().Add(15 * time.Second); time.Now().Before(deadline); time.Sleep(200 * time.Millisecond) {
		if screen, err = h.Read(ctx, pane, host.ReadOpts{Source: "visible", ANSI: true}); err == nil &&
			strings.Contains(screen, "PERSONA-LIVE\x1b[") {
			break
		}
	}
	if !strings.Contains(screen, "PERSONA-LIVE") || !strings.Contains(screen, "\x1b[") || !strings.HasSuffix(screen, "\r\n") {
		t.Fatalf("colored read: %q", screen)
	}
	if plain, err := h.Read(ctx, pane, host.ReadOpts{Source: "recent", Lines: 3}); err != nil || strings.Contains(plain, "\x1b[") ||
		strings.Count(plain, "\n") > 3 {
		t.Errorf("plain read: %q %v", plain, err)
	}

	if err := h.SendText(ctx, pane, "typed-not-run"); err != nil {
		t.Fatal(err)
	}
	if err := h.SendKeys(ctx, pane, "ctrl+u"); err != nil {
		t.Fatal(err)
	}
	if err := h.Prompt(ctx, pane, "hello"); !errors.Is(err, host.ErrNotDelivered) {
		t.Errorf("prompt with a shell in front: %v", err)
	}
	if pids, err := h.PaneProcesses(ctx, pane); err != nil || len(pids) == 0 {
		t.Errorf("processes %v %v", pids, err)
	}
	if fg, err := h.PaneForeground(ctx, pane); err != nil || len(fg) == 0 || !shellProgram(fg[0]) {
		t.Errorf("foreground %v %v", fg, err)
	}

	if err := h.RenameSpace(ctx, cr.SpaceID, "persona-ternlive-2"); err != nil {
		t.Fatal(err)
	}
	if err := h.RenameSlot(ctx, cr.SlotID, "first"); err != nil {
		t.Fatal(err)
	}
	second, err := h.CreateSlot(ctx, cr.SpaceID, dir, "second")
	if err != nil {
		t.Fatal(err)
	}
	if l, err = h.Snapshot(ctx); err != nil {
		t.Fatal(err)
	}
	labels := map[string]string{}
	for _, s := range l.Spaces {
		labels[s.ID] = s.Label
	}
	for _, s := range l.Slots {
		labels[s.ID] = s.Label
	}
	if labels[cr.SpaceID] != "persona-ternlive-2" || labels[cr.SlotID] != "first" || labels[second.SlotID] != "second" {
		t.Errorf("labels: space %q, tabs %q / %q", labels[cr.SpaceID], labels[cr.SlotID], labels[second.SlotID])
	}
	for deadline := time.Now().Add(5 * time.Second); events.Load() < 2 && time.Now().Before(deadline); time.Sleep(100 * time.Millisecond) {
	}
	if events.Load() < 2 { // live, then at least one change
		t.Errorf("events: %d callbacks", events.Load())
	}

	// Closing one of two tabs keeps the session; closing the last block ends it (no window shows it).
	if err := h.Close(ctx, second.Pane, cr.SpaceID, second.SlotID); err != nil {
		t.Fatal(err)
	}
	if l, _ = h.Snapshot(ctx); !hasSpace(l, cr.SpaceID) {
		t.Fatal("session ended with a tab left")
	}
	if err := h.Close(ctx, pane, cr.SpaceID, cr.SlotID); err != nil {
		t.Fatal(err)
	}
	if l, _ = h.Snapshot(ctx); hasSpace(l, cr.SpaceID) {
		t.Error("empty session left behind")
	}
}

func hasSpace(l *host.Layout, id string) bool {
	if l == nil {
		return false
	}
	for _, s := range l.Spaces {
		if s.ID == id {
			return true
		}
	}
	return false
}
