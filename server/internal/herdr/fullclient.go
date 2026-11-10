package herdr

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"
)

// The full client is herdr's own interactive client (`herdr session attach <name>`) run in a PTY of ours, so the web
// can show herdr the way a terminal on the computer does: its sidebar, tabs and splits (docs/integrations/terminal.md "Full herdr
// UI"). The single-pane streams (observe, control) cannot: they are herdr redrawing one pane's grid. This one is a
// client like any other, so it also takes a slot in herdr's layout (the latest active client sizes it) and its copies
// come out as OSC 52.

// FullOptions says how to run a full client.
type FullOptions struct {
	// Session is the herdr session to attach to (SessionName).
	Session string
	// Cols and Rows are the grid of the PTY the client draws in.
	Cols, Rows int
}

// ErrFullUnsupported: no full client on this machine (Windows has no herdr TUI to run in a PTY).
var ErrFullUnsupported = errors.New("unsupported: a full herdr client needs a Unix PTY")

// SessionName is the herdr session this client talks to: the one of `herdr session list` whose socket is Socket (an
// empty Socket is the default session). An error says why the client cannot be run for it.
func (c *Client) SessionName(ctx context.Context) (string, error) {
	out, err := c.sessionList(ctx, 10*time.Second)
	if err != nil {
		return "", fmt.Errorf("unsupported: herdr session list: %w", err)
	}
	return sessionNameIn(out, c.Socket)
}

// sessionNameIn picks the session listening on socket out of `herdr session list --json`.
func sessionNameIn(list []byte, socket string) (string, error) {
	var res struct {
		Sessions []struct {
			Name       string `json:"name"`
			Default    bool   `json:"default"`
			SocketPath string `json:"socket_path"`
		} `json:"sessions"`
	}
	if err := json.Unmarshal(list, &res); err != nil {
		return "", fmt.Errorf("unsupported: herdr session list: %w", err)
	}
	for _, s := range res.Sessions {
		if socket != "" && filepath.Clean(s.SocketPath) == filepath.Clean(socket) {
			return s.Name, nil
		}
	}
	if socket == "" {
		for _, s := range res.Sessions {
			if s.Default {
				return s.Name, nil
			}
		}
	}
	return "", errors.New("unsupported: the herdr socket in use is not one of herdr's sessions")
}

// fullEnvAssignments are what the client runs with. TERM tells herdr what the browser's xterm can draw.
// SSH_CONNECTION (loopback placeholders, the trick of the systemd drop-in some people use for moshi-hook) makes herdr
// believe it is remote, when it should hand a copy to the terminal as OSC 52 rather than to a clipboard tool on the
// computer. Measured on herdr 0.9.5: with no display (or a dead one) it sends OSC 52 from a PTY with or without it;
// with a working display and clipboard tool, not measured, this is what is meant to keep the copy out of the
// computer's own clipboard.
var fullEnvAssignments = []string{
	"TERM=xterm-256color",
	"COLORTERM=truecolor",
	"SSH_CONNECTION=127.0.0.1 1 127.0.0.1 2",
}

// fullClientEnv is the environment the client starts with: the node's, minus what would make herdr think it runs
// inside a pane or another multiplexer, plus fullEnvAssignments.
func fullClientEnv(environ []string) []string {
	out := make([]string, 0, len(environ)+len(fullEnvAssignments))
	for _, kv := range environ {
		name, _, _ := strings.Cut(kv, "=")
		switch {
		case strings.HasPrefix(name, "HERDR_"), name == "TMUX", name == "TMUX_PANE", name == "STY",
			name == "TERM", name == "COLORTERM", name == "SSH_CONNECTION", name == "SSH_CLIENT", name == "SSH_TTY":
			continue
		}
		out = append(out, kv)
	}
	return append(out, fullEnvAssignments...)
}

// FocusPane makes the pane the one herdr shows (herdr 0.9.5's pane.focus: its tab and workspace too). That is herdr's
// state, shared by every client of the session, so the computer's own window moves too. A herdr without pane.focus
// gets its workspace and tab focused instead: the pane within the tab stays the one herdr last had focused there.
func (c *Client) FocusPane(ctx context.Context, pane string) error {
	err := c.rpc(ctx, "pane.focus", map[string]any{"pane_id": pane}, nil)
	if err == nil || !fallback(err) {
		return err
	}
	var res struct {
		Pane struct {
			TabID       string `json:"tab_id"`
			WorkspaceID string `json:"workspace_id"`
		} `json:"pane"`
	}
	if err := c.call(ctx, "pane.get", map[string]any{"pane_id": pane}, &res, "pane", "get", pane); err != nil {
		return err
	}
	if res.Pane.WorkspaceID != "" {
		if err := c.call(ctx, "workspace.focus", map[string]any{"workspace_id": res.Pane.WorkspaceID}, nil,
			"workspace", "focus", res.Pane.WorkspaceID); err != nil {
			return err
		}
	}
	if res.Pane.TabID != "" {
		return c.call(ctx, "tab.focus", map[string]any{"tab_id": res.Pane.TabID}, nil, "tab", "focus", res.Pane.TabID)
	}
	return nil
}

// Focus is what herdr shows: the workspace, tab and pane focused (shared by every client of the session).
type Focus struct {
	Workspace, Tab, Pane string
}

// Focused reads the focus from a snapshot. Empty when herdr does not say (a version without the focused_* fields).
func (c *Client) Focused(ctx context.Context) (Focus, error) {
	snap, err := c.Snapshot(ctx)
	if err != nil {
		return Focus{}, err
	}
	return Focus{Workspace: snap.FocusedWorkspaceID, Tab: snap.FocusedTabID, Pane: snap.FocusedPaneID}, nil
}

// WatchFocus keeps an events.subscribe open for the focus events (workspace, tab and pane focused) and calls changed
// for each, and once when the stream starts, until ctx ends or herdr closes it; it returns why. herdr names no client
// in these events, so who moved the focus is not known from them. A moved focus sends all three events, in a burst.
func (c *Client) WatchFocus(ctx context.Context, changed func()) error {
	subs := []Subscription{{"type": "workspace.focused"}, {"type": "tab.focused"}, {"type": "pane.focused"}}
	return c.Subscribe(ctx, subs, func(Event) { changed() })
}
