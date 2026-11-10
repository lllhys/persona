package node

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/nwverse/persona/server/internal/host"
	"github.com/nwverse/persona/server/internal/lazyre"
	"github.com/nwverse/persona/server/internal/nodeclient"
	"github.com/nwverse/persona/server/internal/proto"
	"github.com/nwverse/persona/server/internal/terncli"
)

// ternHost is Tern, Stencil's multiplexing terminal (docs/integrations/engines.md "Tern"), through its `tern` command
// line. A pane is a terminal block, its id the block's number — what Tern puts in the block's processes as TERN_PANE.
// Spaces are Tern sessions and slots their tabs, from every open window (each command names its window).
//
// Listing, colored reads, typing, keys, prompts, process → session, events, starting, renaming and closing. No live
// stream and no worktrees. Tern does not know which agent runs in a block: the node finds it from the block's
// foreground process (`tern process`), and its state from the transcript and hooks, as on the other hosts.
type ternHost struct {
	c *terncli.Client

	mu       sync.Mutex
	failedAt time.Time // when the last look failed (failErr), for Agents right after a failed Snapshot
	failErr  error
	view     *ternView // the last look, reused for a moment (ternViewReuse) and for the windows of ids
	viewAt   time.Time
}

var (
	_ host.Host            = (*ternHost)(nil)
	_ host.Watcher         = (*ternHost)(nil)
	_ host.Launcher        = (*ternHost)(nil)
	_ host.Organizer       = (*ternHost)(nil)
	_ host.ForegroundNamer = (*ternHost)(nil)
)

func newTernHost(c *terncli.Client) *ternHost { return &ternHost{c: c} }

// ternPaneEnv is the variable Tern sets in every block's processes: the block's number.
const ternPaneEnv = nodeclient.TernPaneEnv

const (
	// ternViewReuse: Agents right after a Snapshot, and the panes' windows, reuse the last look this long.
	ternViewReuse = 500 * time.Millisecond
	// ternProcsParallel: how many `tern process` calls a look runs at once (each is a short-lived tern, ~30 ms).
	ternProcsParallel = 4
)

// ternEvents are the events that can change what the node lists: blocks coming and going, layouts, titles (an agent
// starting in a shell sets one), working directories and bells. Each tern command is a client of its own, so the
// "client_*" events the host's own calls cause are left out.
var ternEvents = []string{"pane_spawned", "pane_exited", "pane_closed", "layout_changed", "title_changed", "cwd_changed", "bell"}

func (h *ternHost) Kind() string    { return host.Tern }
func (h *ternHost) PaneEnv() string { return ternPaneEnv }

func (h *ternHost) Version(ctx context.Context) string { return h.c.Version(ctx) }

// ternErr marks Tern not installed or not running as host.ErrNotRunning: it then has no blocks.
func ternErr(err error) error {
	if errors.Is(err, terncli.ErrNotRunning) || errors.Is(err, terncli.ErrNotInstalled) {
		return host.Mark(err, host.ErrNotRunning)
	}
	return err
}

// ternInputErr marks the input refusals that typed nothing (host.ErrNotDelivered): Tern gone, or the block gone.
func ternInputErr(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, terncli.ErrNotFound):
		return host.Mark(err, host.ErrNotDelivered)
	case errors.Is(err, terncli.ErrNotRunning), errors.Is(err, terncli.ErrNotInstalled):
		return host.Mark(ternErr(err), host.ErrNotDelivered)
	}
	return err
}

// ternView is one look at Tern: the layout of every open window, with each terminal block's processes.
type ternView struct {
	layout *host.Layout
	// window is the window each session, tab and block id is in.
	window map[string]string
	// tabBlock is a block of each tab: Tern renames a tab through one of its blocks.
	tabBlock map[string]string
	// procs are the terminal blocks' processes (nil when reading them failed).
	procs map[string]*terncli.Process
	// sessions are Tern's sessions by id, for what Close leaves behind.
	sessions map[string]terncli.Session
}

// look reads every open window (the first one, then the others its clients name) and the terminal blocks'
// processes. A look that failed a moment ago fails again at once (the node asks for Agents right after a failed
// Snapshot, and a wedged Tern must not cost it twice); one a moment old is reused unless fresh.
func (h *ternHost) look(ctx context.Context, fresh bool) (*ternView, error) {
	h.mu.Lock()
	if h.failErr != nil && time.Since(h.failedAt) < time.Second {
		err := h.failErr
		h.mu.Unlock()
		return nil, err
	}
	if !fresh && h.view != nil && time.Since(h.viewAt) < ternViewReuse {
		v := h.view
		h.mu.Unlock()
		return v, nil
	}
	h.mu.Unlock()

	first, err := h.c.Inspect(ctx, "")
	if err != nil {
		err = ternErr(err)
		h.mu.Lock()
		h.failedAt, h.failErr = time.Now(), err
		h.mu.Unlock()
		return nil, err
	}
	ins := []windowInspect{{"", first}}
	for _, w := range first.Windows()[1:] {
		if in, err := h.c.Inspect(ctx, w); err == nil { // a window closed meanwhile is left out
			ins = append(ins, windowInspect{w, in})
		}
	}
	v := h.build(ctx, ins)
	h.mu.Lock()
	h.failErr = nil
	h.view, h.viewAt = v, time.Now()
	h.mu.Unlock()
	return v, nil
}

type windowInspect struct {
	window string
	in     *terncli.Inspect
}

// build turns windows' layouts into one view: sessions as spaces (a session two windows show counts once, in the
// first), tabs as slots, terminal blocks as panes with their agents.
func (h *ternHost) build(ctx context.Context, ins []windowInspect) *ternView {
	v := &ternView{layout: &host.Layout{}, window: map[string]string{}, tabBlock: map[string]string{},
		sessions: map[string]terncli.Session{}}
	type place struct {
		window string
		s      terncli.Session
		t      terncli.Tab
		b      terncli.Block
	}
	var blocks []place
	for _, wi := range ins {
		for _, s := range wi.in.Sessions {
			sid := string(s.ID)
			if _, dup := v.sessions[sid]; dup || sid == "" {
				continue
			}
			v.sessions[sid] = s
			v.window[sid] = wi.window
			v.layout.Spaces = append(v.layout.Spaces, host.Space{ID: sid, Label: s.Name, Number: len(v.layout.Spaces) + 1})
			for j, t := range s.Tabs {
				tid := string(t.ID)
				v.window[tid] = wi.window
				label := ""
				if t.Name != nil {
					label = *t.Name
				}
				num := t.Number
				if num <= 0 {
					num = j + 1
				}
				if label == "" {
					label = strconv.Itoa(num)
				}
				v.layout.Slots = append(v.layout.Slots, host.Slot{ID: tid, SpaceID: sid, Label: label, Number: num})
				for _, b := range t.Blocks {
					bid := string(b.ID)
					v.window[bid] = wi.window
					if _, ok := v.tabBlock[tid]; !ok {
						v.tabBlock[tid] = bid
					}
					if b.Terminal() {
						blocks = append(blocks, place{wi.window, s, t, b})
					}
				}
			}
		}
	}
	v.procs = make(map[string]*terncli.Process, len(blocks))
	var (
		mu  sync.Mutex
		wg  sync.WaitGroup
		sem = make(chan struct{}, ternProcsParallel)
	)
	for _, p := range blocks {
		wg.Add(1)
		sem <- struct{}{}
		go func(window string, id terncli.ID) {
			defer func() { <-sem; wg.Done() }()
			pr, err := h.c.Process(ctx, window, id)
			if err != nil {
				pr = nil // a block that closed meanwhile, or a process tern could not read: no agent known there
			}
			mu.Lock()
			v.procs[string(id)] = pr
			mu.Unlock()
		}(p.window, p.b.ID)
	}
	wg.Wait()
	for _, p := range blocks {
		a := ternAgent(p.s, p.t, p.b, v.procs[string(p.b.ID)])
		v.layout.Panes = append(v.layout.Panes, a)
		if a.Agent != "" {
			v.layout.Agents = append(v.layout.Agents, a)
		}
	}
	fillSpaceDirectories(v.layout.Spaces, v.layout.Panes)
	return v
}

// ternAgent is a terminal block as an agent pane. Its agent is what runs in its foreground (claude, codex, also
// behind node or bun); none when a shell, classic herdr's client (whatever runs inside belongs to herdr) or nothing is
// in front. Tern does not report an agent's state: the node judges it from the transcript and hooks.
func ternAgent(s terncli.Session, t terncli.Tab, b terncli.Block, p *terncli.Process) host.Agent {
	a := host.Agent{
		Cwd: b.Cwd, PaneID: host.PaneRef{Kind: host.Tern, ID: string(b.ID)}.String(),
		WorkspaceID: string(s.ID), TabID: string(t.ID),
		Title: stripSpinner(b.Title), RawTitle: b.Title,
	}
	if p == nil || p.Foreground == nil {
		return a
	}
	fg := p.Foreground
	a.ForegroundCwd = fg.Cwd
	if ternShellOrHerdr(fg) {
		return a
	}
	if k := agentOfCmd(fg.Argv); k != "" {
		a.Agent, a.AgentStatus = k, proto.StatusUnknown
	}
	return a
}

// ternShellOrHerdr: the program in front is a shell (the agent has exited) or classic herdr's client.
func ternShellOrHerdr(fg *terncli.Proc) bool {
	argv0 := ""
	if len(fg.Argv) > 0 {
		argv0 = fg.Argv[0]
	}
	return shellProgram(fg.Name) || shellProgram(argv0) || fg.Name == "herdr" || filepath.Base(argv0) == "herdr"
}

func (h *ternHost) Snapshot(ctx context.Context) (*host.Layout, error) {
	v, err := h.look(ctx, true)
	if err != nil {
		return nil, err
	}
	return v.layout, nil
}

func (h *ternHost) Spaces(ctx context.Context) ([]host.Space, error) {
	v, err := h.look(ctx, true)
	if err != nil {
		return nil, err
	}
	return v.layout.Spaces, nil
}

func (h *ternHost) Agents(ctx context.Context) ([]host.Agent, error) {
	v, err := h.look(ctx, false)
	if err != nil {
		return nil, err
	}
	return v.layout.Agents, nil
}

// Agent is one block as an agent pane, read afresh (its tab and session from the last look).
func (h *ternHost) Agent(ctx context.Context, pane host.PaneRef) (*host.Agent, error) {
	v, err := h.look(ctx, false)
	if err != nil {
		return nil, err
	}
	for _, a := range v.layout.Panes {
		if a.PaneID == pane.String() {
			p, err := h.c.Process(ctx, v.window[pane.ID], terncli.ID(pane.ID))
			if err != nil {
				return nil, ternErr(err)
			}
			s, t, b := v.sessions[a.WorkspaceID], terncli.Tab{ID: terncli.ID(a.TabID)}, terncli.Block{ID: terncli.ID(pane.ID), Cwd: a.Cwd, Title: a.RawTitle}
			out := ternAgent(s, t, b, p)
			return &out, nil
		}
	}
	return nil, fmt.Errorf("tern: no terminal block %s", pane.ID)
}

// windowOf is the window a session, tab or block is in, from the last look ("", the first window, when unknown).
func (h *ternHost) windowOf(id string) string {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.view == nil {
		return ""
	}
	return h.view.window[id]
}

// ternSGR matches SGR escapes, to tell a row that only resets colors from one with text.
var ternSGR = lazyre.New(`\x1b\[[0-9;:]*m`)

// Read is `tern capture`: the screen, or back into the scrollback for "recent", with colors as SGR escapes when asked
// (rows then end in \r\n, as the node expects; tern ends them in \n). Blank rows at the bottom (a shell's empty screen)
// are dropped before Lines cuts it to its last rows.
func (h *ternHost) Read(ctx context.Context, pane host.PaneRef, o host.ReadOpts) (string, error) {
	scrollback := o.Source == "recent" || o.Source == "recent-unwrapped"
	text, err := h.c.Capture(ctx, h.windowOf(pane.ID), terncli.ID(pane.ID), o.ANSI, scrollback)
	if err != nil {
		return "", ternErr(err)
	}
	rows := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	for len(rows) > 0 && strings.TrimSpace(ternSGR.ReplaceAllString(rows[len(rows)-1], "")) == "" {
		rows = rows[:len(rows)-1]
	}
	if o.Lines > 0 && len(rows) > o.Lines {
		rows = rows[len(rows)-o.Lines:]
	}
	if len(rows) == 0 {
		return "", nil
	}
	eol := "\n"
	if o.ANSI {
		eol = "\r\n"
	}
	return strings.Join(rows, eol) + eol, nil
}

// SendText types text as keystrokes, without enter.
func (h *ternHost) SendText(ctx context.Context, pane host.PaneRef, text string) error {
	return ternInputErr(h.c.Text(ctx, h.windowOf(pane.ID), terncli.ID(pane.ID), text))
}

// SendKeys passes Persona's key names through: tern takes the same spelling ("enter", "esc", "shift+tab", "ctrl+u",
// "up", "backspace"…).
func (h *ternHost) SendKeys(ctx context.Context, pane host.PaneRef, keys ...string) error {
	return ternInputErr(h.c.Keys(ctx, h.windowOf(pane.ID), terncli.ID(pane.ID), keys...))
}

// front checks that an agent is what runs in front of a block: once it has exited, the shell under it would run the
// text as a command, so a prompt is refused with nothing typed.
func (h *ternHost) front(ctx context.Context, pane host.PaneRef) error {
	p, err := h.c.Process(ctx, h.windowOf(pane.ID), terncli.ID(pane.ID))
	if err != nil {
		return ternInputErr(err)
	}
	if p.Foreground == nil || ternShellOrHerdr(p.Foreground) || agentOfCmd(p.Foreground.Argv) == "" {
		return host.Mark(fmt.Errorf("tern: no agent runs in front of block %s", pane.ID), host.ErrNotDelivered)
	}
	return nil
}

// Prompt pastes the text (Tern brackets it when the agent asked for bracketed pastes) and presses enter after a gap
// that grows with its lines, as for Orca: an agent may take a pasted line break for a submit if enter comes with it.
// Escapes in the text are made inert so it cannot end the paste early. The block is checked before each step.
func (h *ternHost) Prompt(ctx context.Context, pane host.PaneRef, text string) error {
	if err := h.front(ctx, pane); err != nil {
		return err
	}
	w, id := h.windowOf(pane.ID), terncli.ID(pane.ID)
	if err := h.c.Paste(ctx, w, id, strings.ReplaceAll(text, "\x1b", inertEscape)); err != nil {
		return ternInputErr(err)
	}
	afterPaste := func(err error) error {
		return fmt.Errorf("tern: the text was pasted into block %s but not submitted: %v", pane.ID, err) // not marked: unknown
	}
	select {
	case <-ctx.Done():
		return afterPaste(ctx.Err())
	case <-time.After(orcaPasteGap(text)):
	}
	if err := h.front(ctx, pane); err != nil {
		return afterPaste(err)
	}
	if err := h.c.Keys(ctx, w, id, "enter"); err != nil {
		return afterPaste(err)
	}
	return nil
}

// PaneProcesses: the block's foreground process (claude's own pid, which names its sessions/<pid>.json), then the
// block's own program (its shell).
func (h *ternHost) PaneProcesses(ctx context.Context, pane host.PaneRef) ([]int, error) {
	p, err := h.c.Process(ctx, h.windowOf(pane.ID), terncli.ID(pane.ID))
	if err != nil {
		return nil, ternErr(err)
	}
	var pids []int
	if p.Foreground != nil && p.Foreground.Pid > 0 {
		pids = append(pids, p.Foreground.Pid)
	}
	if p.Child != nil && p.Child.Pid > 0 && (len(pids) == 0 || pids[0] != p.Child.Pid) {
		pids = append(pids, p.Child.Pid)
	}
	return pids, nil
}

// PaneForeground names the program in front of a block (its name, and its argv[0] when that differs).
func (h *ternHost) PaneForeground(ctx context.Context, pane host.PaneRef) ([]string, error) {
	p, err := h.c.Process(ctx, h.windowOf(pane.ID), terncli.ID(pane.ID))
	if err != nil {
		return nil, ternErr(err)
	}
	if p.Foreground == nil {
		return nil, nil
	}
	out := []string{p.Foreground.Name}
	if len(p.Foreground.Argv) > 0 && p.Foreground.Argv[0] != p.Foreground.Name {
		out = append(out, p.Foreground.Argv[0])
	}
	return out, nil
}

// Watch is one `tern events` for the first window (the node still snapshots every host now and then, which covers
// the other windows). A bell is urgent: an agent may be asking for the user.
func (h *ternHost) Watch(ctx context.Context, _ []host.PaneRef, changed func(urgent bool)) error {
	return ternErr(h.c.Events(ctx, ternEvents, func() { changed(false) }, func(ev terncli.Event) {
		changed(ev.Event == "bell")
	}))
}

// CreateSpace opens a Tern session named label (the directory's name when empty) in cwd, with one tab running the
// user's shell. Tern does not show it in a window: nothing switches under the user.
func (h *ternHost) CreateSpace(ctx context.Context, cwd, label string) (*host.Created, error) {
	if label == "" {
		label = filepath.Base(cwd)
	}
	cr, err := h.c.NewSession(ctx, "", label, cwd)
	if err != nil {
		return nil, ternErr(err)
	}
	return &host.Created{SpaceID: string(cr.Session), SlotID: string(cr.Tab), Pane: host.PaneRef{Kind: host.Tern, ID: string(cr.Block)}}, nil
}

// CreateSlot opens a tab in a session, in cwd, running the user's shell; a label names the tab.
func (h *ternHost) CreateSlot(ctx context.Context, space, cwd, label string) (*host.Created, error) {
	w := h.windowOf(space)
	cr, err := h.c.NewTab(ctx, w, terncli.ID(space), cwd)
	if err != nil {
		return nil, ternErr(err)
	}
	if label != "" {
		if err := h.c.Rename(ctx, w, string(cr.Block), label); err != nil {
			h.logf("tern: naming tab %s %q: %v", cr.Tab, label, err)
		}
	}
	return &host.Created{SpaceID: space, SlotID: string(cr.Tab), Pane: host.PaneRef{Kind: host.Tern, ID: string(cr.Block)}}, nil
}

// Run types command into a block and presses enter.
func (h *ternHost) Run(ctx context.Context, pane host.PaneRef, command string) error {
	return ternInputErr(h.c.RunCommand(ctx, h.windowOf(pane.ID), terncli.ID(pane.ID), command))
}

// NameAgent: Tern shows the title the agent sets itself; the tab keeps the name it was opened with.
func (h *ternHost) NameAgent(context.Context, host.PaneRef, string) error { return nil }

// RenameSpace names a Tern session.
func (h *ternHost) RenameSpace(ctx context.Context, space, name string) error {
	return ternErr(h.c.Rename(ctx, h.windowOf(space), space, name))
}

// RenameSlot names a tab, through one of its blocks (tern renames a block's tab).
func (h *ternHost) RenameSlot(ctx context.Context, slot, name string) error {
	v, err := h.look(ctx, false)
	if err != nil {
		return err
	}
	b, ok := v.tabBlock[slot]
	if !ok {
		if v, err = h.look(ctx, true); err != nil {
			return err
		}
		if b, ok = v.tabBlock[slot]; !ok {
			return fmt.Errorf("tern: no tab %s", slot)
		}
	}
	return ternErr(h.c.Rename(ctx, v.window[slot], b, name))
}

// Close closes a block (its tab with it when it was the only one). A session left with no blocks that no window shows
// (one Persona opened) is ended too: tern keeps an empty session otherwise. A block already gone counts as closed.
func (h *ternHost) Close(ctx context.Context, pane host.PaneRef, space, _ string) error {
	w := h.windowOf(pane.ID)
	err := h.c.Close(ctx, w, terncli.ID(pane.ID))
	if err != nil && !errors.Is(err, terncli.ErrNotFound) {
		return ternErr(err)
	}
	if space == "" {
		return nil
	}
	v, err := h.look(ctx, true)
	if err != nil {
		return nil // closed; what is left of the session is looked at next time
	}
	if s, ok := v.sessions[space]; ok && !s.Shown && ternEmpty(s) {
		if err := h.c.KillSession(ctx, v.window[space], s.ID); err != nil && !errors.Is(err, terncli.ErrNotFound) {
			h.logf("tern: ending empty session %s: %v", space, err)
		}
	}
	return nil
}

// ternEmpty: a session without a block left.
func ternEmpty(s terncli.Session) bool {
	for _, t := range s.Tabs {
		if len(t.Blocks) > 0 {
			return false
		}
	}
	return true
}

func (h *ternHost) logf(format string, args ...any) {
	if h.c.Log != nil {
		h.c.Log(format, args...)
	}
}
