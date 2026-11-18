package herdr

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
)

// Snapshot prefers the atomic session snapshot. When herdr loses a large socket answer, smaller reads rebuild
// the same fields. A failed read returns an error so callers can keep their old layout and fall back to agents.
func (c *Client) Snapshot(ctx context.Context) (*Snapshot, error) {
	var res struct {
		Snapshot json.RawMessage `json:"snapshot"`
	}
	err := c.call(ctx, "session.snapshot", nil, &res, "api", "snapshot")
	if err == nil {
		var snap Snapshot
		if len(res.Snapshot) > 0 {
			if err := json.Unmarshal(res.Snapshot, &snap); err != nil {
				return nil, err
			}
		}
		var fields map[string]json.RawMessage
		_ = json.Unmarshal(res.Snapshot, &fields)
		_, panes := fields["panes"]
		_, agents := fields["agents"]
		snap.InventoryIncomplete = !panes || !agents
		c.snapshotCut.Store(false)
		return &snap, nil
	}
	if !errors.Is(err, ErrTruncated) || ctx.Err() != nil {
		return nil, err
	}
	if !c.snapshotCut.Swap(true) && c.Log != nil {
		c.Log("herdr: session.snapshot answer cut short; rebuilding from smaller calls")
	}
	snap, smallErr := c.snapshotParts(ctx)
	if smallErr != nil {
		return nil, errors.Join(err, fmt.Errorf("rebuild snapshot: %w", smallErr))
	}
	return snap, nil
}

// snapshotParts reads each workspace separately and each tab's layout once, using any pane in that tab.
// These calls are not atomic; changes between successful reads settle on the next scan.
func (c *Client) snapshotParts(ctx context.Context) (*Snapshot, error) {
	var spaces struct {
		Workspaces json.RawMessage `json:"workspaces"`
	}
	if err := c.call(ctx, "workspace.list", nil, &spaces, "workspace", "list"); err != nil {
		return nil, err
	}
	snap := &Snapshot{InventoryIncomplete: len(spaces.Workspaces) == 0}
	if len(spaces.Workspaces) > 0 {
		if err := json.Unmarshal(spaces.Workspaces, &snap.Workspaces); err != nil {
			return nil, err
		}
	}
	for _, w := range snap.Workspaces {
		var tabs struct {
			Tabs []Tab `json:"tabs"`
		}
		if err := c.call(ctx, "tab.list", map[string]any{"workspace_id": w.ID}, &tabs, "tab", "list", "--workspace", w.ID); err != nil {
			return nil, err
		}
		var panes struct {
			Panes json.RawMessage `json:"panes"`
		}
		if err := c.call(ctx, "pane.list", map[string]any{"workspace_id": w.ID}, &panes, "pane", "list", "--workspace", w.ID); err != nil {
			return nil, err
		}
		var entries []Agent
		if len(panes.Panes) == 0 {
			snap.InventoryIncomplete = true
		} else if err := json.Unmarshal(panes.Panes, &entries); err != nil {
			return nil, err
		}
		snap.Tabs = append(snap.Tabs, tabs.Tabs...)
		snap.Panes = append(snap.Panes, entries...)
		firstPane := map[string]string{}
		for _, p := range entries {
			if firstPane[p.TabID] == "" {
				firstPane[p.TabID] = p.PaneID
			}
			if p.Agent != "" {
				// pane.list omits agent-only metadata such as the name. agent.list can be too large as well.
				a, err := c.AgentGet(ctx, p.PaneID)
				if err != nil {
					return nil, err
				}
				snap.Agents = append(snap.Agents, *a)
			}
		}
		if w.Focused {
			snap.FocusedWorkspaceID = w.ID
		}
		for _, t := range tabs.Tabs {
			if w.Focused && t.Focused {
				snap.FocusedTabID = t.ID
			}
			pane := firstPane[t.ID]
			if pane == "" {
				continue
			}
			var res struct {
				Layout TabLayout `json:"layout"`
			}
			if err := c.call(ctx, "pane.layout", map[string]any{"pane_id": pane}, &res, "pane", "layout", "--pane", pane); err != nil {
				return nil, err
			}
			if res.Layout.TabID != t.ID || res.Layout.WorkspaceID != w.ID {
				return nil, fmt.Errorf("pane moved while reading snapshot layout")
			}
			snap.Layouts = append(snap.Layouts, res.Layout)
			if w.Focused && t.Focused {
				snap.FocusedPaneID = res.Layout.FocusedPaneID
			}
		}
	}
	return snap, nil
}
