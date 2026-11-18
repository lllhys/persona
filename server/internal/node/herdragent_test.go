package node

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/host"
	"github.com/nwverse/persona/server/internal/proto"
)

// withPaneShell makes the shell herdr opens panes with shell for the test (TestMain: zsh).
func withPaneShell(t *testing.T, shell string) {
	t.Helper()
	old := herdrPaneShell
	herdrPaneShell = func() string { return shell }
	t.Cleanup(func() { herdrPaneShell = old })
}

// HERDR_AGENT (hostherdr.go, #3454): herdr is told the agent only for what Persona starts in a herdr pane, Claude and
// Codex only, and typed only into a shell that takes `VAR=val cmd`; a launcher run without a pane (the assistant,
// launcher checks) never gets it.
func TestHerdrAgentOnlyInHerdrPanes(t *testing.T) {
	herdr, orca := &herdrHost{}, &orcaHost{}
	for _, tc := range []struct {
		launch host.Launcher
		kind   string
		want   string
	}{
		{herdr, "claude", "claude"},
		{herdr, "codex", "codex"},
		{herdr, traexAgent, ""},
		{herdr, piAgent, "pi"},
		{orca, "claude", ""},
		{orca, "codex", ""},
	} {
		if got := herdrAgentFor(tc.launch, tc.kind); got != tc.want {
			t.Errorf("%T %s: %q, want %q", tc.launch, tc.kind, got, tc.want)
		}
	}
	for _, tc := range []struct {
		shells []string
		want   string
	}{
		{[]string{"/bin/zsh"}, "claude"}, {[]string{"-bash"}, "claude"}, {[]string{"fish"}, "claude"}, {[]string{"/bin/sh"}, "claude"},
		{[]string{"dash"}, "claude"}, {[]string{"ksh"}, "claude"}, {[]string{"mksh"}, "claude"},
		{[]string{"-tcsh"}, ""}, {[]string{"csh"}, ""}, {[]string{"pwsh"}, ""}, {[]string{"nu"}, ""}, {[]string{"elvish"}, ""},
		{[]string{"xonsh"}, ""}, {[]string{"zsh", "tcsh"}, ""}, {[]string{""}, ""}, {nil, ""},
	} {
		if got := typedHerdrAgent(herdr, "claude", tc.shells...); got != tc.want {
			t.Errorf("typed into %q: %q, want %q", tc.shells, got, tc.want)
		}
	}
	if got := typedHerdrAgent(orca, "claude", "zsh"); got != "" {
		t.Errorf("typed into an Orca zsh: %q", got)
	}
	if got := withHerdrAgent("", "traex hi"); got != "traex hi" {
		t.Errorf("no agent: %q", got)
	}
	n, _ := testNode(t)
	d := "~/.claude-work"
	n.claudeStartDir.Store(&d)
	if got := n.launcherCommand("claude"); got != `CLAUDE_CONFIG_DIR="$HOME"/.claude-work claude` {
		t.Errorf("headless launcher command %q", got)
	}
}

// A launcher the hub says runs Codex (a wrapper) is typed into its herdr pane with HERDR_AGENT=codex in front, but
// not into a shell that cannot take it; a compatible CLI is left to herdr's own detection.
func TestOtherAgentStartTellsHerdrCodex(t *testing.T) {
	for _, tc := range []struct{ kind, launcher, shell, want string }{
		{"codex", "cxw", "/bin/zsh", "HERDR_AGENT=codex cxw hi"},
		{"codex", "cxw", "/bin/tcsh", "cxw hi"},
		{traexAgent, traexAgent, "/bin/zsh", "traex hi"},
	} {
		t.Run(tc.kind+" "+tc.shell, func(t *testing.T) {
			withPaneShell(t, tc.shell)
			n, f, home := herdrNode(t)
			f.reply("session.snapshot", snapshot(nil, nil, nil, nil))
			f.reply("tab.create", map[string]any{"tab": map[string]any{"tab_id": "w1:t2"}, "root_pane": map[string]any{"pane_id": "w1:p2"}})
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			f.after(func(c herdrCall) {
				if c.Method == "pane.send_input" {
					cancel() // what was typed is all this test looks at
				}
			})
			_, _ = n.startOtherAgent(ctx, n.hosts.Get(host.Herdr).(*herdrHost), home, tc.launcher, tc.kind, "hi", "", "w1", "")
			text := tc.want
			if tc.shell == "/bin/zsh" { // with the node's directory, without the assistant's environment (agentenv.go)
				text = inAgentEnv(n.dir, text, shellQuote)
			}
			want := call("pane.send_input", map[string]any{"pane_id": "w1:p2", "keys": []string{"Enter"}, "text": text})
			if got := f.trace("pane.send_input"); len(got) != 1 || got[0] != want {
				t.Errorf("typed %v, want %s", got, want)
			}
		})
	}
}

// herdr opens its panes with a shell that does not take `VAR=val cmd` (tcsh here; pwsh, nu…): a Claude start types
// its command as before, without HERDR_AGENT, and so does herdr's resume of it, which herdr types into such a shell.
func TestHerdrAgentLeftOutOfAShellThatCannotTakeIt(t *testing.T) {
	withPaneShell(t, "/bin/tcsh")
	n, f, home := herdrNode(t)
	f.reply("session.snapshot", snapshot(nil, nil, nil, nil))
	f.reply("tab.create", map[string]any{"tab": map[string]any{"tab_id": "w1:t2"}, "root_pane": map[string]any{"pane_id": "w1:p2"}})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	f.after(func(c herdrCall) {
		if c.Method == "pane.send_input" {
			cancel()
		}
	})
	_, _ = n.start(ctx, home, "claude", "", "", "w1", "", "", "", startID, "")
	typed := f.trace("pane.send_input")
	if len(typed) != 1 || !strings.Contains(typed[0], `"text":"claude --session-id `+startID+` `) {
		t.Errorf("typed %v", typed)
	}
	for _, spec := range []agentLaunch{{Launcher: "claude"}, {Launcher: "cx", Agent: "codex"}} {
		if argv, ok := n.agentResumeArgv(spec, startID); !ok || strings.Contains(argv[1], "HERDR_AGENT") {
			t.Errorf("%s resume_argv %q %v", spec.Launcher, argv, ok)
		}
	}
}

// Claude's input box comes up but herdr never reports a claude in the pane (a wrapper's binary under another name, a
// herdr too old for HERDR_AGENT): the start still fails after claudeStartWait and closes its tab, but says that, with
// what herdr sees in front of the pane, not that the shell never ran claude (#3454). herdr's own answer for a pane
// where it detects no agent is agent_not_found; an agent with no name says the same.
func TestHerdrStartSaysWhenHerdrDoesNotRecogniseClaude(t *testing.T) {
	for name, answer := range map[string]any{
		"agent_not_found": herdrFail{Code: "agent_not_found", Message: "agent target w1:p2 not found"},
		"no name":         map[string]any{"agent": map[string]any{"agent": "", "agent_status": "unknown", "pane_id": "w1:p2"}},
	} {
		t.Run(name, func(t *testing.T) {
			n, f, home := herdrNode(t)
			withStartWaits(n, 1500*time.Millisecond, time.Minute)
			f.reply("session.snapshot", snapshot(nil, nil, nil, nil))
			f.reply("tab.create", map[string]any{"tab": map[string]any{"tab_id": "w1:t2"}, "root_pane": map[string]any{"pane_id": "w1:p2"}})
			f.reply("agent.get", answer)
			f.reply("pane.list", map[string]any{"panes": []any{map[string]any{"pane_id": "w1:p2", "tab_id": "w1:t2"}}})
			f.reply("pane.process_info", foreground([2]string{"claude-2.1.318", ""}))
			f.setScreen("w1:p2", screenFixture(t, "claude-2.1.312-empty-162.txt"))
			_, err := request(t, n, "session.start", map[string]any{"cwd": home, "workspace_id": "w1", "prompt": "hi"})
			if err == nil || !strings.Contains(err.Error(), "claude's input box is on screen but herdr did not recognise the agent within 1.5s") ||
				!strings.Contains(err.Error(), "herdr sees claude-2.1.318 in front") || !strings.Contains(err.Error(), "HERDR_AGENT=claude") ||
				strings.Contains(err.Error(), "had not run it yet") {
				t.Fatalf("start: %v", err)
			}
			if got := f.trace("tab.close"); len(got) != 1 || got[0] != call("tab.close", map[string]any{"tab_id": "w1:t2"}) {
				t.Errorf("closed %v", got)
			}
			if got := f.trace("agent.rename", "pane.report_agent_session"); len(got) != 0 {
				t.Errorf("a failed start was bound: %v", got)
			}
		})
	}
}

// The same after the folder-trust dialog was answered: claude ran, so the message names the wait after it.
func TestHerdrStartPastTheTrustDialogSaysWhenHerdrDoesNotRecogniseClaude(t *testing.T) {
	n, f, home := herdrNode(t)
	withStartWaits(n, time.Minute, 1500*time.Millisecond)
	f.reply("session.snapshot", snapshot(nil, nil, nil, nil))
	f.reply("tab.create", map[string]any{"tab": map[string]any{"tab_id": "w1:t2"}, "root_pane": map[string]any{"pane_id": "w1:p2"}})
	f.reply("agent.get", herdrFail{Code: "agent_not_found", Message: "agent target w1:p2 not found"})
	f.reply("pane.list", map[string]any{"panes": []any{map[string]any{"pane_id": "w1:p2", "tab_id": "w1:t2"}}})
	f.screenSteps("w1:p2", isKeys(), screenFixture(t, "claude-2.1.312-trust-162.txt"), screenFixture(t, "claude-2.1.312-empty-162.txt"))
	_, err := request(t, n, "session.start", map[string]any{"cwd": home, "workspace_id": "w1", "prompt": "hi"})
	if err == nil || !strings.Contains(err.Error(), "claude's input box is on screen but herdr did not recognise the agent within 1.5s") {
		t.Fatalf("start: %v", err)
	}
}

// screenHost is a bareHost whose panes all show screen.
type screenHost struct {
	bareHost
	screen string
}

func (h *screenHost) Read(context.Context, host.PaneRef, host.ReadOpts) (string, error) {
	return h.screen, nil
}

// Another engine's pane with Claude's input box up and no claude reported: the wait ends as before, without herdr's
// message, since neither herdr nor HERDR_AGENT has a part there.
func TestUnrecognisedClaudeIsSaidOnlyOfHerdr(t *testing.T) {
	n := &Node{hosts: host.NewRegistry(&screenHost{bareHost: bareHost{kind: host.Orca}, screen: screenFixture(t, "claude-2.1.312-empty-162.txt")})}
	withStartWaits(n, time.Second, time.Minute)
	_, err := n.waitReady(context.Background(), "orca:t1:l1", nil)
	if err == nil || !strings.Contains(err.Error(), "the tab's shell had not run it yet") || strings.Contains(err.Error(), "herdr") {
		t.Fatalf("wait: %v", err)
	}
}

// stateless is a screenHost whose engine sees claude in front of every pane but reports no state for it, as an Orca
// fork does after the tab's rename until claude's first hook (#4438).
type stateless struct{ screenHost }

func (h *stateless) Agent(context.Context, host.PaneRef) (*host.Agent, error) {
	return &host.Agent{Agent: "claude", AgentStatus: proto.StatusUnknown}, nil
}

// With no state from the engine, claude's input box on screen is ready; its trust dialog, a first-run screen or a
// running turn is not.
func TestClaudeWithoutEngineStateIsReadyByItsInputBox(t *testing.T) {
	empty := screenFixture(t, "claude-2.1.312-empty-162.txt")
	for _, c := range []struct {
		name, screen string
		ready        bool
	}{
		{"input box", empty, true},
		{"trust dialog", screenFixture(t, "claude-2.1.312-trust-162.txt"), false},
		{"security notes", screenFixture(t, "claude-2.1.318-security-notes-120.txt"), false},
		{"turn running", empty + "\n✻ Working… (3s · esc to interrupt)\n", false},
	} {
		t.Run(c.name, func(t *testing.T) {
			n := &Node{hosts: host.NewRegistry(&stateless{screenHost{bareHost: bareHost{kind: host.Orca}, screen: c.screen}})}
			withStartWaits(n, time.Second, 2*time.Second)
			a, err := n.waitReady(context.Background(), "orca:t1:l1", nil)
			if c.ready && (err != nil || a == nil) {
				t.Fatalf("not ready: %v", err)
			}
			if !c.ready && err == nil {
				t.Fatalf("ready on %s", c.name)
			}
		})
	}
}
