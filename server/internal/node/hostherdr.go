package node

import (
	"context"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/nwverse/persona/server/internal/herdr"
	"github.com/nwverse/persona/server/internal/host"
	"github.com/nwverse/persona/server/internal/nodeclient"
)

// herdrHost is the herdr host: every method is one herdr client call (Close: the tab's pane count, then a close), on
// the client of the session the id names (hostherdr_sessions.go). The hostherdr.go and hostherdr_*.go files and
// engine.go are the only places in the node that know the herdr client.
type herdrHost struct {
	c *herdr.Client // the node's own server: its ids are herdr's as they are
	// named are the account's other running herdr sessions, driven too; nil: c alone (newHerdrSessions).
	named *herdrSessions
}

// newHerdrHost is the herdr host on c, with the account's named sessions when c is the default server.
func newHerdrHost(c *herdr.Client, logf func(format string, args ...any)) *herdrHost {
	return &herdrHost{c: c, named: newHerdrSessions(c, logf)}
}

// herdrHosts is a registry holding only the herdr host on c.
func herdrHosts(c *herdr.Client) *host.Registry { return host.NewRegistry(&herdrHost{c: c}) }

var (
	_ host.Host         = (*herdrHost)(nil)
	_ host.Watcher      = (*herdrHost)(nil)
	_ host.Launcher     = (*herdrHost)(nil)
	_ host.Organizer    = (*herdrHost)(nil)
	_ host.SlotCloser   = (*herdrHost)(nil)
	_ host.Splitter     = (*herdrHost)(nil)
	_ host.Streamer     = (*herdrHost)(nil)
	_ host.Controller   = (*herdrHost)(nil)
	_ host.FullClienter = (*herdrHost)(nil)
	_ host.Worktreer    = (*herdrHost)(nil)
)

// herdrPaneEnv is the variable herdr sets in every pane; herdrSessionEnv and herdrSocketEnv say which server's
// (herdr.EnvSession, #2196).
const (
	herdrPaneEnv    = nodeclient.HerdrPaneEnv
	herdrSessionEnv = nodeclient.HerdrSessionEnv
	herdrSocketEnv  = nodeclient.HerdrSocketEnv
)

// herdrAgentEnv tells herdr which agent a pane runs. herdr knows Claude and Codex by the foreground program's name
// (basename claude / claude-code, codex), so a wrapper that execs a binary of another name ("claude-2.1.318") was never
// seen as Claude: the start waited out claudeStartWait with Claude's input box on screen (#3454). herdr reads this
// variable from the foreground process's environment as it was exec'd (/proc/<pid>/environ, macOS KERN_PROCARGS2)
// before it looks at the name: herdr 0.7.1 on Linux, 0.7.5 on macOS; older ones and Windows ignore it. So the node
// puts it on the agent's own command only, `HERDR_AGENT=claude <launcher> …` where the pane's shell takes that form
// (typedHerdrAgent; codex-start sets it in the environment it execs): set on the pane's shell, or the tab's
// environment, it would be authoritative for everything run there later.
const herdrAgentEnv = "HERDR_AGENT"

// herdrAgentName is what herdr calls an agent of kind, "" for one it has no integration for (a compatible CLI, pi: left to its
// own detection) and on Windows, where herdr does not read HERDR_AGENT.
func herdrAgentName(kind string) string {
	if runtime.GOOS != "windows" && (kind == "claude" || kind == "codex") {
		return kind
	}
	return ""
}

// herdrAgentFor is the HERDR_AGENT value for an agent of kind started in launch's panes: herdrAgentName on herdr, ""
// on other hosts.
func herdrAgentFor(launch host.Launcher, kind string) string {
	if _, ok := launch.(*herdrHost); !ok {
		return ""
	}
	return herdrAgentName(kind)
}

// typedHerdrAgent is herdrAgentFor for a command typed into a pane whose shell is shells (the programs in front of
// it): "" unless they take `VAR=val cmd`, so csh, pwsh, nu, xonsh, elvish and a shell not known get the command as
// before, without it.
func typedHerdrAgent(launch host.Launcher, kind string, shells ...string) string {
	if !takesAssign(shells...) {
		return ""
	}
	return herdrAgentFor(launch, kind)
}

// takesAssign: shells (names or paths, "-zsh" for a login shell) are there and each is one of assignShells.
func takesAssign(shells ...string) bool {
	for _, s := range shells {
		if !assignShells[strings.TrimPrefix(filepath.Base(s), "-")] {
			return false
		}
	}
	return len(shells) > 0
}

// assignShells are the shells whose `VAR=val cmd` runs cmd with VAR set and leaves the shell without it (fish from
// 3.1): the ones the node types HERDR_AGENT in front for.
var assignShells = map[string]bool{"sh": true, "bash": true, "zsh": true, "dash": true, "ksh": true, "mksh": true, "fish": true}

// withHerdrAgent is the command line cmd with HERDR_AGENT=agent in front, for the shell that runs it to put in the
// agent's environment alone; cmd as it is when agent is "".
func withHerdrAgent(agent, cmd string) string {
	if agent == "" {
		return cmd
	}
	return herdrAgentEnv + "=" + agent + " " + cmd
}

func (h *herdrHost) Kind() string    { return host.Herdr }
func (h *herdrHost) PaneEnv() string { return herdrPaneEnv }

func (h *herdrHost) Version(ctx context.Context) string { return h.c.Version(ctx) }

// Agent is agent.get; herdr answers *_not_found for a pane where it detects no agent (a claude it does not recognise,
// #3454), marked ErrNoAgent.
func (h *herdrHost) Agent(ctx context.Context, pane host.PaneRef) (*host.Agent, error) {
	t, err := h.target(ctx, pane.ID)
	if err != nil {
		return nil, err
	}
	a, err := t.c.AgentGet(ctx, t.id)
	if herdr.IsNotFound(err) {
		return nil, host.Mark(err, host.ErrNoAgent)
	}
	if err != nil {
		return nil, err
	}
	out := fromHerdrAgent(t.session, *a)
	return &out, nil
}

func (h *herdrHost) Read(ctx context.Context, pane host.PaneRef, o host.ReadOpts) (string, error) {
	t, err := h.target(ctx, pane.ID)
	if err != nil {
		return "", err
	}
	if o.ANSI {
		return t.c.PaneReadANSI(ctx, t.id, o.Source, o.Lines)
	}
	return t.c.PaneRead(ctx, t.id, o.Source, o.Lines)
}

func (h *herdrHost) SendText(ctx context.Context, pane host.PaneRef, text string) error {
	t, err := h.target(ctx, pane.ID)
	if err != nil {
		return err
	}
	return t.c.PaneSendText(ctx, t.id, text)
}

func (h *herdrHost) SendKeys(ctx context.Context, pane host.PaneRef, keys ...string) error {
	t, err := h.target(ctx, pane.ID)
	if err != nil {
		return err
	}
	return t.c.PaneSendKeys(ctx, t.id, keys...)
}

// Prompt is agent.prompt; herdr answers *_not_found for a pane whose agent it does not detect (a compatible CLI).
func (h *herdrHost) Prompt(ctx context.Context, pane host.PaneRef, text string) error {
	t, err := h.target(ctx, pane.ID)
	if err != nil {
		return err
	}
	err = t.c.AgentPrompt(ctx, t.id, text)
	if herdr.IsNotFound(err) {
		return host.Mark(err, host.ErrNoAgent)
	}
	if herdr.IsBlocked(err) {
		return host.Mark(err, host.ErrAgentBlocked)
	}
	return err
}

func (h *herdrHost) PaneProcesses(ctx context.Context, pane host.PaneRef) ([]int, error) {
	t, err := h.target(ctx, pane.ID)
	if err != nil {
		return nil, err
	}
	return t.c.PaneProcesses(ctx, t.id)
}

// PaneForeground names each foreground program by what it was started as (argv0), else its process name.
func (h *herdrHost) PaneForeground(ctx context.Context, pane host.PaneRef) ([]string, error) {
	t, err := h.target(ctx, pane.ID)
	if err != nil {
		return nil, err
	}
	procs, err := t.c.PaneForeground(ctx, t.id)
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(procs))
	for _, p := range procs {
		if p.Argv0 != "" {
			names = append(names, p.Argv0)
		} else {
			names = append(names, p.Name)
		}
	}
	return names, nil
}

func (h *herdrHost) ReportMetadata(ctx context.Context, pane host.PaneRef, source string, tokens map[string]*string) error {
	t, err := h.target(ctx, pane.ID)
	if err != nil {
		return err
	}
	return t.c.PaneReportMetadata(ctx, t.id, source, tokens)
}

// PaneReportResume is herdr.Client.PaneReportResume on the pane's session (herdrresume.go).
func (h *herdrHost) PaneReportResume(ctx context.Context, pane host.PaneRef, source, agent, sessionID string, argv []string) error {
	t, err := h.target(ctx, pane.ID)
	if err != nil {
		return err
	}
	return t.c.PaneReportResume(ctx, t.id, source, agent, sessionID, argv)
}

// has runs `herdr <args> --help` once: whether this herdr knows the command.
func (h *herdrHost) has(cache *struct {
	once sync.Once
	ok   bool
}, args ...string) bool {
	cache.once.Do(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		cache.ok = exec.CommandContext(ctx, h.c.Bin, append(args, "--help")...).Run() == nil
	})
	return cache.ok
}
