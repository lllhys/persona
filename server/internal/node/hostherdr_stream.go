package node

import (
	"context"
	"errors"
	"runtime"
	"sync"
	"time"

	"github.com/nwverse/persona/server/internal/herdr"
	"github.com/nwverse/persona/server/internal/host"
)

// observeSupport, controlSupport cache whether this herdr has `terminal session observe` / `control` (herdr 0.9 on
// Unix).
var observeSupport, controlSupport struct {
	once sync.Once
	ok   bool
}

func (h *herdrHost) CanObserve() bool {
	return h.has(&observeSupport, "terminal", "session", "observe")
}
func (h *herdrHost) CanControl() bool {
	return h.has(&controlSupport, "terminal", "session", "control")
}

func (h *herdrHost) PaneSize(ctx context.Context, pane host.PaneRef) (int, int, error) {
	t, err := h.target(ctx, pane.ID)
	if err != nil {
		return 0, 0, err
	}
	return t.c.PaneSize(ctx, t.id)
}

func (h *herdrHost) Observe(ctx context.Context, pane host.PaneRef, cols, rows int, fn func(host.Frame)) error {
	t, err := h.target(ctx, pane.ID)
	if err != nil {
		return err
	}
	return t.c.Observe(ctx, t.id, cols, rows, func(f herdr.Frame) { fn(host.Frame(f)) })
}

func (h *herdrHost) Control(ctx context.Context, pane host.PaneRef, cols, rows int, takeover bool, fn func(host.Frame)) (host.Control, error) {
	t, err := h.target(ctx, pane.ID)
	if err != nil {
		return nil, err
	}
	c, err := t.c.StartControl(ctx, t.id, cols, rows, takeover, func(f herdr.Frame) { fn(host.Frame(f)) })
	if err != nil {
		if errors.Is(err, herdr.ErrAttachBusy) {
			err = host.Mark(err, host.ErrAttachBusy)
		}
		return nil, err
	}
	return herdrControl{c}, nil
}

// herdrControl is herdr's control with its errors marked for the host package.
type herdrControl struct{ *herdr.Control }

func (c herdrControl) Mouse(e host.MouseEvent) error { return c.Control.Mouse(herdr.MouseEvent(e)) }

func (c herdrControl) Err() error {
	err := c.Control.Err()
	if errors.Is(err, herdr.ErrTakenOver) {
		return host.Mark(err, host.ErrTakenOver)
	}
	return err
}

// CanFullClient: herdr on Unix, with this client's socket one of herdr's sessions.
func (h *herdrHost) CanFullClient() bool {
	if runtime.GOOS == "windows" {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, err := h.c.SessionName(ctx)
	return err == nil
}

// StartFullClient attaches to the session of opts.Pane (the own server's without one) and shows that pane.
func (h *herdrHost) StartFullClient(ctx context.Context, opts host.FullClientOptions, onData func([]byte)) (host.FullClient, error) {
	t := herdrRoute{c: h.c}
	if opts.Pane != nil && opts.Pane.ID != "" {
		var err error
		if t, err = h.target(ctx, opts.Pane.ID); err != nil {
			return nil, err
		}
	}
	session := t.session
	if session == "" {
		var err error
		if session, err = h.c.SessionName(ctx); err != nil {
			return nil, err
		}
	}
	if t.id != "" {
		_ = t.c.FocusPane(ctx, t.id) // the client then opens on it; not worth failing the open for
	}
	fc, err := t.c.StartFull(herdr.FullOptions{Session: session, Cols: opts.Cols, Rows: opts.Rows}, onData)
	if err != nil {
		return nil, err
	}
	return fc, nil
}

// Focused is the focus the own server shows now (host.Focuser): the computer's own herdr window, which the named
// sessions' focus is not.
func (h *herdrHost) Focused(ctx context.Context) (host.FocusState, error) {
	f, err := h.c.Focused(ctx)
	return host.FocusState{Space: f.Workspace, Slot: f.Tab, Pane: f.Pane}, err
}

func (h *herdrHost) WatchFocus(ctx context.Context, changed func()) error {
	return h.c.WatchFocus(ctx, changed)
}

func (h *herdrHost) FocusPane(ctx context.Context, pane host.PaneRef) error {
	t, err := h.target(ctx, pane.ID)
	if err != nil {
		return err
	}
	return t.c.FocusPane(ctx, t.id)
}
