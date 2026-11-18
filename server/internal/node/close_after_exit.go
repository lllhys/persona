package node

import (
	"context"
	"path/filepath"

	"github.com/nwverse/persona/server/internal/host"
)

// closeAfterExit finishes an explicit Close whose Agent exited before lookup.
// Its caller holds the session's recovery and typing locks. Only Herdr's marked
// retained shells are handled here; other engines keep their existing behavior.
func (n *Node) closeAfterExit(ctx context.Context, id string, st sessionState) error {
	pane := st.Pane
	if pane == "" {
		if p, ok := n.keptPane(id); ok {
			pane = p.PaneID // A discovery hint, never authority to close it.
		}
	}
	if pane == "" {
		return nil // Preserve idempotent Close for older state with no terminal identity.
	}
	if host.ParsePane(pane).Kind != host.Herdr {
		return nil
	}
	h, ref, err := n.paneHost(pane)
	if err != nil {
		return err
	}
	hh, ok := h.(*herdrHost)
	if !ok {
		return unsupported(h)
	}
	target, err := hh.target(ctx, ref.ID)
	if err != nil {
		return err
	}
	busy := func() error {
		return refusal("agent_busy", "the stopped terminal changed or is in use; check it before closing", nil)
	}
	owned := func() (host.Agent, bool, error) {
		// Host.Snapshot can reuse a disconnected named session's cached layout.
		// Read the selected server directly, including when proving absence.
		snap, err := target.c.Snapshot(ctx)
		if err != nil {
			return host.Agent{}, false, err
		}
		if snap.InventoryIncomplete {
			return host.Agent{}, false, busy()
		}
		for _, a := range fromHerdrAgents(target.session, snap.Agents) {
			if a.PaneID == pane || a.SessionID() == id {
				return host.Agent{}, false, busy()
			}
		}
		if current, _ := n.state.get(id); current.Pane != "" && current.Pane != pane {
			return host.Agent{}, false, busy()
		}
		n.mu.Lock()
		_, running := n.agents[id]
		inUse := running || n.panes[pane] != "" && n.panes[pane] != id
		for _, a := range n.agents {
			inUse = inUse || a.PaneID == pane
		}
		n.mu.Unlock()
		if inUse {
			return host.Agent{}, false, busy()
		}
		for _, p := range fromHerdrAgents(target.session, snap.Panes) {
			if p.PaneID != pane {
				continue
			}
			if p.Agent != "" || p.Tokens[personaTabToken] != id || p.TabID == "" || p.WorkspaceID == "" ||
				st.Cwd != "" && (paneDir(p) == "" || filepath.Clean(paneDir(p)) != filepath.Clean(st.Cwd)) {
				return host.Agent{}, false, busy()
			}
			return p, true, nil
		}
		return host.Agent{}, false, nil // The target server confirms the pane is gone.
	}
	before, present, err := owned()
	if err != nil || !present {
		return err
	}
	names, err := hh.PaneForeground(ctx, ref)
	if err != nil {
		return err
	}
	if len(names) == 0 || !allShells(names) {
		return busy()
	}
	after, present, err := owned()
	if err != nil || !present {
		return err
	}
	if after.TabID != before.TabID || after.WorkspaceID != before.WorkspaceID || paneDir(after) != paneDir(before) {
		return busy()
	}
	// Snapshot checks and close are separate calls, not an atomic engine claim.
	// Close just this pane; Herdr removes its parent tab only if it becomes empty.
	return target.c.PaneClose(ctx, target.id)
}
