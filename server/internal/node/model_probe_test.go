package node

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/nwverse/persona/server/internal/proto"
)

func TestModelProbeVerifiesForwardingAfterEnvironmentUnset(t *testing.T) {
	zsh, err := exec.LookPath("zsh")
	if err != nil {
		t.Skip("no zsh")
	}
	definitions := `clearing() { unset ANTHROPIC_MODEL; command claude "$@" }
not_forwarding() { command claude }
fixed() { command claude --model fixed-model "$@" }
fixed_same() { command claude --model requested-model "$@" }
fixed_same_eq() { command claude --model=requested-model "$@" }
fixed_both() { command claude --model requested-model --model fixed-model "$@" }
`
	for _, tc := range []struct {
		name  string
		valid bool
	}{
		{"clearing", true}, {"not_forwarding", false}, {"fixed", false},
		// It pins the very model asked for and forwards it too: claude sees it twice and runs it (#6205).
		{"fixed_same", true}, {"fixed_same_eq", true},
		{"fixed_both", false}, // one of its own flags names another model
	} {
		out, err := exec.Command(zsh, "-fc", definitions+"\n"+probeScript(tc.name, "--model", "requested-model")).Output()
		if err != nil {
			t.Fatal(err)
		}
		probe, ok := parseProbe(string(out))
		if !ok {
			t.Fatal("probe did not run")
		}
		if err := forwardedModel(probe, "requested-model"); (err == nil) != tc.valid {
			t.Fatalf("%s: %+v %v", tc.name, probe.Args, err)
		}
	}
}

func TestManagedModelOverrideRemovesDuplicateFlags(t *testing.T) {
	got := withoutModelArgs([]string{"--model=old", "--verbose", "--model", "old", "--permission-mode", "acceptEdits"})
	if !slices.Equal(got, []string{"--verbose", "--permission-mode", "acceptEdits"}) {
		t.Fatalf("%v", got)
	}
}

// scriptLauncherHome is a home whose shells define scriptcc as the hub's `ca` is: a function that runs a script of its
// own, which execs the agent by its absolute path (/usr/bin/true stands in for claude: nothing starts). The probe's
// stubs never see it. fixedcc is seen and pins its own model.
func scriptLauncherHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, "bin", "claude-x"), []byte("#!/bin/sh\nexec /usr/bin/true \"$@\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	defs := "scriptcc() { \"$HOME/bin/claude-x\" \"$@\"; }\nfixedcc() { command claude --model fixed-model \"$@\"; }\n"
	for _, rc := range []string{".zshrc", ".bashrc"} {
		if err := os.WriteFile(filepath.Join(home, rc), []byte(defs), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("HOME", home)
	t.Setenv("ZDOTDIR", home)
	return home
}

// A send that names a model goes through a launcher the probe cannot see into, as it did before the check (the model
// as --model): it used to fail with "did not start an agent when tried", and the assistant on such a launcher could
// not be sent anything. Its page says what cannot be seen. A launcher that is seen and pins another model is still
// refused.
func TestModelCheckOfALauncherThroughAScript(t *testing.T) {
	home := scriptLauncherHome(t)
	n, _ := testNode(t)
	n.home = home
	if _, err := n.launcherProbe(context.Background(), "scriptcc"); !errors.Is(err, errProbeUnseen) {
		t.Fatalf("probe: %v", err)
	}
	if agent, err := n.launcherModelCheck(context.Background(), "scriptcc", "m-1"); err != nil || agent != "claude" {
		t.Fatalf("check: %q %v", agent, err)
	}
	if d := n.launcherProbeCached(context.Background(), "scriptcc"); d.Agent != "claude" || !strings.Contains(d.Error, "看不出") {
		t.Fatalf("page %+v", d)
	}
	if _, err := n.launcherModelCheck(context.Background(), "fixedcc", "m-1"); err == nil || !strings.Contains(err.Error(), "固定了模型") {
		t.Fatalf("a pinned model: %v", err)
	}
}

// An agent started with a model through such a launcher starts, the model passed to it.
func TestHerdrStartWithAModelThroughAScriptLauncher(t *testing.T) {
	home := scriptLauncherHome(t)
	n, f, nodeHome := herdrNode(t)
	n.home = home
	cwd := mkdir(t, filepath.Join(nodeHome, "proj"))
	f.reply("workspace.list", map[string]any{"workspaces": []any{map[string]any{"workspace_id": "w1", "label": "proj"}}})
	startHerdr(f, "w1:p2", proto.StatusIdle)
	f.reply("tab.create", map[string]any{"tab": map[string]any{"tab_id": "w1:t2"}, "root_pane": map[string]any{"pane_id": "w1:p2"}})
	f.screenSteps("w1:p2", isKeys(), screenFixture(t, "claude-2.1.312-empty-162.txt"))
	if _, err := request(t, n, "session.start", map[string]any{"session_id": startID, "cwd": cwd, "launcher": "scriptcc", "model": "m-1",
		"async": true}); err != nil {
		t.Fatalf("refused: %v", err)
	}
	waitCalls(t, f, "pane.send_input", 1)
	typed := strings.Join(f.trace("pane.send_input"), "\n")
	if !strings.Contains(typed, `"text":`+jsonPrefix(inAgentEnv(n.dir, "HERDR_AGENT=claude scriptcc --session-id "+startID, shellQuote))) ||
		!strings.Contains(typed, "--model m-1") {
		t.Fatalf("typed %s", typed)
	}
}
