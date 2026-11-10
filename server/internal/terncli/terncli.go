// Package terncli drives Tern, Stencil's multiplexing terminal, through its `tern` command line
// (docs/integrations/engines.md "Tern"): the layout (`tern inspect`), a block's foreground process (`tern process`),
// screen captures, input, opening and closing blocks, renames and the event stream.
//
// Tern scopes every command to one window's sessions. Every call here names its window with --window (the first
// window's key is ""), so a node that happens to run inside a Tern pane does not end up scoped to that pane's window.
// The flag goes right after the command's first word: before it, tern takes "" for a file to open; after the
// positional arguments, `tern run` and `tern send` would take it for more of the command or text.
package terncli

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

// ErrNotInstalled: no tern binary on this machine.
var ErrNotInstalled = errors.New("tern is not installed")

// ErrNotRunning: tern is installed but its session daemon is not running ("no Tern is running").
var ErrNotRunning = errors.New("tern is not running")

// ErrNotFound: the block, session or window named does not exist (any more).
var ErrNotFound = errors.New("tern: not found")

// callTimeout bounds one command; a capture of a long scrollback is the slowest (a few tens of ms otherwise).
const callTimeout = 5 * time.Second

// streamLiveAfter: a `tern events` that has not failed this long after starting is live.
var streamLiveAfter = 300 * time.Millisecond

// Client runs the tern binary. The zero value is not usable: New sets the binary lookup and the runner.
type Client struct {
	// Bin finds the binary each call ("" when Tern is not installed), so installing Tern later needs no restart.
	Bin func() string
	// Run runs the binary with args and returns its standard output; a failure carries the standard error text
	// (tests swap it for a fake tern).
	Run func(ctx context.Context, bin string, args ...string) ([]byte, error)
	// Stream starts the binary with args and returns its standard output as a stream (tern events).
	Stream func(ctx context.Context, bin string, args ...string) (stream, error)
	Log    func(format string, args ...any)
}

// New is a client of the tern binary FindBin finds.
func New() *Client { return &Client{Bin: FindBin, Run: run, Stream: startStream} }

// FindBin is tern on PATH, else ~/.local/bin/tern (where a user links it), else, on a Mac, the binary inside the app
// bundle: a resident node (launchd, systemd) has only the system's PATH.
func FindBin() string {
	if p, err := exec.LookPath("tern"); err == nil {
		return p
	}
	var cands []string
	if home, err := os.UserHomeDir(); err == nil {
		cands = append(cands, filepath.Join(home, ".local", "bin", "tern"))
	}
	if runtime.GOOS == "darwin" {
		cands = append(cands, "/Applications/Tern.app/Contents/MacOS/tern")
		if home, err := os.UserHomeDir(); err == nil {
			cands = append(cands, filepath.Join(home, "Applications", "Tern.app", "Contents", "MacOS", "tern"))
		}
	}
	for _, p := range cands {
		if st, err := os.Stat(p); err == nil && !st.IsDir() && st.Mode()&0o111 != 0 {
			return p
		}
	}
	return ""
}

// run is the real runner: the binary's standard output, or an error carrying its standard error.
func run(ctx context.Context, bin string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, bin, args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			return out, fmt.Errorf("%w: %s", err, msg)
		}
		return out, err
	}
	return out, nil
}

// classify turns tern's error text into this package's errors (the text is all tern gives: exit status 1 for all).
func classify(err error) error {
	if err == nil {
		return nil
	}
	msg := err.Error()
	switch {
	case strings.Contains(msg, "no Tern is running"):
		return fmt.Errorf("%w: %v", ErrNotRunning, err)
	case strings.Contains(msg, "no block is called"), strings.Contains(msg, "no session is called"),
		strings.Contains(msg, "no tab is called"), strings.Contains(msg, "no window has key"):
		return fmt.Errorf("%w: %v", ErrNotFound, err)
	}
	return err
}

// call runs one command (args[0] its first word) in a window and returns its output.
func (c *Client) call(ctx context.Context, window string, args ...string) ([]byte, error) {
	bin := c.Bin()
	if bin == "" {
		return nil, ErrNotInstalled
	}
	ctx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()
	out, err := c.Run(ctx, bin, windowArgs(window, args...)...)
	return out, classify(err)
}

// windowArgs puts --window right after the command's first word ("send --window W BLOCK keys enter").
func windowArgs(window string, args ...string) []string {
	out := make([]string, 0, len(args)+2)
	out = append(out, args[0], "--window", window)
	return append(out, args[1:]...)
}

// Version is the binary's version ("0.9.13"), "" when Tern is not installed or does not answer. It needs no running
// Tern.
func (c *Client) Version(ctx context.Context) string {
	bin := c.Bin()
	if bin == "" {
		return ""
	}
	ctx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()
	out, err := c.Run(ctx, bin, "--version")
	if err != nil {
		return ""
	}
	return parseVersion(string(out))
}

// parseVersion takes "tern 0.9.13 (8c6dfbd)" to "0.9.13".
func parseVersion(s string) string {
	f := strings.Fields(s)
	if len(f) >= 2 && f[0] == "tern" {
		return f[1]
	}
	if len(f) == 1 {
		return f[0]
	}
	return ""
}

// ID is one of Tern's numeric ids (sessions, tabs, blocks; a block's id is also its pane's), kept as the decimal
// string the commands take and the node stores.
type ID string

// UnmarshalJSON takes a number (or a string) as its decimal digits, without a float's rounding.
func (i *ID) UnmarshalJSON(b []byte) error {
	s := strings.Trim(string(b), `"`)
	if s == "null" {
		*i = ""
		return nil
	}
	if _, err := strconv.ParseInt(s, 10, 64); err != nil {
		return fmt.Errorf("tern id %s: %w", b, err)
	}
	*i = ID(s)
	return nil
}

// Inspect is `tern inspect --json` of one window: its sessions with their tabs and blocks, every live pane and every
// client (a window, a command line).
type Inspect struct {
	Sessions []Session   `json:"sessions"`
	Panes    []Pane      `json:"panes"`
	Clients  []ClientRef `json:"clients"`
}

// Session is a Tern session: tabs of blocks.
type Session struct {
	ID    ID     `json:"id"`
	Name  string `json:"name"`
	Shown bool   `json:"shown"`
	Tabs  []Tab  `json:"tabs"`
}

// Tab is a session's tab; Name is null in Tern until someone names it.
type Tab struct {
	ID     ID      `json:"id"`
	Name   *string `json:"name"`
	Number int     `json:"number"`
	Shown  bool    `json:"shown"`
	Blocks []Block `json:"blocks"`
}

// Block is a tab's block: a terminal (Live while its program runs) or another kind (a browser, a file viewer), which
// has no program.
type Block struct {
	ID      ID       `json:"id"`
	Program string   `json:"program"`
	Args    []string `json:"args"`
	Cwd     string   `json:"cwd"`
	Title   string   `json:"title"`
	Focused bool     `json:"focused"`
	Live    bool     `json:"live"`
	Browser *string  `json:"browser"`
}

// Terminal reports whether the block is a terminal whose program still runs.
func (b Block) Terminal() bool { return b.Live && b.Browser == nil && b.Program != "" }

// Pane is a live terminal pane: Working is Tern's own "output is coming" signal (it misses an agent thinking quietly).
type Pane struct {
	Pane    ID     `json:"pane"`
	Program string `json:"program"`
	Cwd     string `json:"cwd"`
	Title   string `json:"title"`
	Working bool   `json:"working"`
	QuietMS int64  `json:"quiet_ms"`
}

// ClientRef is one client of the daemon: a window ("window") or a command line ("cli"), with the window it serves.
type ClientRef struct {
	Kind   string `json:"kind"`
	Window string `json:"window"`
}

// Windows are the keys of the windows open now, the first window's ("") first; just "" when none reports.
func (in *Inspect) Windows() []string {
	out := []string{""}
	for _, c := range in.Clients {
		if c.Kind == "window" && c.Window != "" && !contains(out, c.Window) {
			out = append(out, c.Window)
		}
	}
	return out
}

func contains(s []string, v string) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}

// Inspect is one window's layout.
func (c *Client) Inspect(ctx context.Context, window string) (*Inspect, error) {
	out, err := c.call(ctx, window, "inspect", "--json")
	if err != nil {
		return nil, err
	}
	var in Inspect
	if err := json.Unmarshal(out, &in); err != nil {
		return nil, fmt.Errorf("tern inspect: %w", err)
	}
	return &in, nil
}

// Proc is a process of a block.
type Proc struct {
	Pid  int      `json:"pid"`
	Name string   `json:"name"`
	Argv []string `json:"argv"`
	Cwd  string   `json:"cwd"`
}

// Process is `tern process`: the block's own program (Child, usually a shell) and the program in its foreground.
type Process struct {
	Pane       ID    `json:"pane"`
	Child      *Proc `json:"child"`
	Group      int   `json:"group"`
	Foreground *Proc `json:"foreground"`
}

// Process is a block's processes.
func (c *Client) Process(ctx context.Context, window string, block ID) (*Process, error) {
	out, err := c.call(ctx, window, "process", string(block), "--json")
	if err != nil {
		return nil, err
	}
	var p Process
	if err := json.Unmarshal(out, &p); err != nil {
		return nil, fmt.Errorf("tern process: %w", err)
	}
	return &p, nil
}

// Capture is a block's screen as text, with its colors as ANSI escapes when ansi is set, from its scrollback on when
// scrollback is set.
func (c *Client) Capture(ctx context.Context, window string, block ID, ansi, scrollback bool) (string, error) {
	args := []string{"capture", string(block)}
	if ansi {
		args = append(args, "--ansi")
	}
	if scrollback {
		args = append(args, "--scrollback")
	}
	out, err := c.call(ctx, window, args...)
	return string(out), err
}

// Keys presses keys in a block; Tern takes Persona's spelling ("enter", "esc", "shift+tab", "ctrl+u", "up",
// "backspace"…) as is.
func (c *Client) Keys(ctx context.Context, window string, block ID, keys ...string) error {
	if len(keys) == 0 {
		return nil
	}
	_, err := c.call(ctx, window, append([]string{"send", string(block), "keys"}, keys...)...)
	return err
}

// Text types text into a block as keystrokes, without enter.
func (c *Client) Text(ctx context.Context, window string, block ID, text string) error {
	_, err := c.call(ctx, window, "send", string(block), "text", text)
	return err
}

// Paste pastes text into a block (a bracketed paste when its program asked for one), without enter.
func (c *Client) Paste(ctx context.Context, window string, block ID, text string) error {
	_, err := c.call(ctx, window, "send", string(block), "paste", text)
	return err
}

// RunCommand types command into a block and presses enter.
func (c *Client) RunCommand(ctx context.Context, window string, block ID, command string) error {
	_, err := c.call(ctx, window, "run", string(block), command)
	return err
}

// Created is what `tern new … --json` opened.
type Created struct {
	Session ID `json:"session"`
	Tab     ID `json:"tab"`
	Block   ID `json:"block"`
}

// NewSession opens a session called name with one tab in cwd running the user's shell. It is not shown: no window
// switches to it.
func (c *Client) NewSession(ctx context.Context, window, name, cwd string) (*Created, error) {
	return c.created(ctx, window, "new", "session", name, "--cwd", cwd, "--json")
}

// NewTab opens a tab in session with one block in cwd running the user's shell.
func (c *Client) NewTab(ctx context.Context, window string, session ID, cwd string) (*Created, error) {
	return c.created(ctx, window, "new", "tab", string(session), "--cwd", cwd, "--json")
}

func (c *Client) created(ctx context.Context, window string, args ...string) (*Created, error) {
	out, err := c.call(ctx, window, args...)
	if err != nil {
		return nil, err
	}
	var cr Created
	if err := json.Unmarshal(out, &cr); err != nil {
		return nil, fmt.Errorf("tern %s: %w", strings.Join(args[:2], " "), err)
	}
	if cr.Block == "" {
		return nil, fmt.Errorf("tern %s: no block in %q", strings.Join(args[:2], " "), bytes.TrimSpace(out))
	}
	return &cr, nil
}

// Close closes a block (its tab with it when it was the tab's only block).
func (c *Client) Close(ctx context.Context, window string, block ID) error {
	_, err := c.call(ctx, window, "close", string(block))
	return err
}

// KillSession ends a session with everything in it.
func (c *Client) KillSession(ctx context.Context, window string, session ID) error {
	_, err := c.call(ctx, window, "kill", "session", string(session))
	return err
}

// Rename names a session (target a session id) or the tab of a block (target a block id).
func (c *Client) Rename(ctx context.Context, window, target, name string) error {
	_, err := c.call(ctx, window, "rename", target, name)
	return err
}

// Event is one line of `tern events`: Event names it ("pane_spawned", "layout_changed", "title_changed"…), Pane the
// pane it is about when it is about one.
type Event struct {
	Event string `json:"event"`
	Pane  ID     `json:"pane"`
}

// stream is a running `tern events`: its output, and the wait for its end (with its standard error in the error).
type stream struct {
	out  *bufio.Scanner
	wait func() error
}

func startStream(ctx context.Context, bin string, args ...string) (stream, error) {
	cmd := exec.CommandContext(ctx, bin, args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.StdoutPipe()
	if err != nil {
		return stream{}, err
	}
	if err := cmd.Start(); err != nil {
		return stream{}, err
	}
	sc := bufio.NewScanner(out)
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
	return stream{out: sc, wait: func() error {
		err := cmd.Wait()
		if msg := strings.TrimSpace(stderr.String()); err != nil && msg != "" {
			return fmt.Errorf("%w: %s", err, msg)
		}
		return err
	}}, nil
}

// Events streams the events named in kinds (all when empty) to fn until ctx ends or tern stops, and returns why it
// stopped. live is called once the stream has started.
func (c *Client) Events(ctx context.Context, kinds []string, live func(), fn func(Event)) error {
	bin := c.Bin()
	if bin == "" {
		return ErrNotInstalled
	}
	args := windowArgs("", "events")
	if len(kinds) > 0 {
		args = append(args, "--filter", strings.Join(kinds, ","))
	}
	s, err := c.Stream(ctx, bin, args...)
	if err != nil {
		return err
	}
	// tern says nothing when a stream starts: it counts as live once it has run a moment without failing (a Tern that
	// is not running ends it at once), or at its first event.
	var once sync.Once
	goLive := func() { once.Do(live) }
	timer := time.AfterFunc(streamLiveAfter, goLive)
	defer timer.Stop()
	for s.out.Scan() {
		goLive()
		var ev Event
		if json.Unmarshal(s.out.Bytes(), &ev) == nil && ev.Event != "" {
			fn(ev)
		}
	}
	err = s.wait()
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return classify(err)
}
