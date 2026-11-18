package node

import (
	"context"
	"strings"
	"testing"
)

// A codex launcher's model catalog advertises ultra (#6181): the general execution check runs before the launcher
// dispatch, so Claude Code's own effort list, which stops at max, must not refuse a Codex start's effort.
func TestValidModesForLauncherEffort(t *testing.T) {
	ok := [][3]string{
		{"codex", "", "ultra"},
		{"codex", "", "minimal"},
		{"codex", "", "auto"},
		{"codex", "bypassPermissions", "ultra"},
		{"claude", "plan", "xhigh"},
		{"", "", "max"},
		{"", "", "auto"},
	}
	for _, c := range ok {
		if err := validModesFor(c[0], c[1], c[2]); err != nil {
			t.Errorf("validModesFor(%q, %q, %q): %v", c[0], c[1], c[2], err)
		}
	}
	bad := [][3]string{
		{"codex", "", "huge"},
		{"codex", "yolo", "ultra"},
		{"claude", "", "ultra"},
		{"", "", "ultra"},
		{"claude", "", "minimal"},
	}
	for _, c := range bad {
		if err := validModesFor(c[0], c[1], c[2]); err == nil {
			t.Errorf("validModesFor(%q, %q, %q) accepted", c[0], c[1], c[2])
		}
	}
}

func TestModelExecutionCheckCodexUltra(t *testing.T) {
	if err := (modelExecution{Model: "gpt-6.1-sol", Effort: "ultra"}).check("codex"); err != nil {
		t.Errorf("codex start with ultra refused: %v", err)
	}
	for _, launcher := range []string{"", "claude"} {
		err := (modelExecution{Effort: "ultra"}).check(launcher)
		if err == nil || !strings.Contains(err.Error(), "unknown effort level") {
			t.Errorf("check(%q) with ultra = %v, want unknown effort level", launcher, err)
		}
	}
	if err := (modelExecution{Model: "not a model"}).check("codex"); err == nil || err.Error() != "invalid model" {
		t.Errorf("model check changed: %v", err)
	}
}

func TestCheckStartCodexUltraEffort(t *testing.T) {
	n := &Node{home: t.TempDir()}
	if _, _, err := n.checkStart("", "codex", "", "", "ultra", ""); err != nil {
		t.Errorf("codex checkStart with ultra refused: %v", err)
	}
	if _, _, err := n.checkStart("", "claude", "", "", "ultra", ""); err == nil {
		t.Error("claude checkStart accepted ultra")
	}
	if _, _, err := n.checkStart("", "codex", "", "", "huge", ""); err == nil {
		t.Error("codex checkStart accepted an unknown effort")
	}
}

// An explicit agent_type wins over the launcher's name when start picks the agent (its kind): a Claude start on a
// launcher named codex still takes Claude's effort list, so ultra is refused before anything runs.
func TestStartEffortFollowsExplicitAgentType(t *testing.T) {
	_, err := (&Node{}).start(context.Background(), "", "codex", "", "", "", "", "ultra", "", "", "claude")
	if err == nil || !strings.Contains(err.Error(), "unknown effort level") {
		t.Errorf("start(agentType=claude, launcher=codex, effort=ultra) = %v, want unknown effort level", err)
	}
	// The same start as codex passes the effort check; what fails then is the empty directory, not the effort.
	_, err = (&Node{}).start(context.Background(), "", "codex", "", "", "", "", "ultra", "", "", "codex")
	if err != nil && strings.Contains(err.Error(), "unknown effort level") {
		t.Errorf("start(agentType=codex, launcher=codex, effort=ultra) refused on effort: %v", err)
	}
}
