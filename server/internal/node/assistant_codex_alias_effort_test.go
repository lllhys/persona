package node

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nwverse/persona/server/internal/proto"
)

// The model menu advertises Codex's native levels for a custom launcher. Both a first message's explicit effort and
// an effort from Model Settings must reach turn/start unchanged; the launcher's spelling must not make it Claude.
func TestCodexAssistantAliasDeliversNativeEffort(t *testing.T) {
	for _, tc := range []struct {
		name, explicit, configured string
	}{
		{"explicit ultra", "ultra", "ultra"},
		{"explicit minimal", "minimal", "minimal"},
		{"configured ultra", "", "ultra"},
		{"configured minimal", "", "minimal"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			n := codexTestNode(t)
			probedLaunchers(t, map[string]string{"cx_test": "codex"})
			withFakeHub(t, n)
			cwd := t.TempDir()
			p, c, f := newFakeCodex(t, n, cwd)
			f.results["thread/start"] = map[string]any{"thread": map[string]any{"id": testThread}, "model": "example"}
			f.results["turn/start"] = map[string]any{"turn": map[string]any{"id": "turn-1", "status": "inProgress"}}
			if err := c.start(context.Background(), "", cwd, false); err != nil {
				t.Fatal(err)
			}
			execution := modelExecution{Effort: tc.configured, UseEffort: true, Fingerprint: "configured-effort"}
			p.model = execution
			p.cmd = &exec.Cmd{}
			p.fingerprint = n.executionFingerprint("cx_test", p.model)
			n.spare = p
			id, err := n.assistantSend(context.Background(), "", cwd, "cx_test", "hello", nil, proto.ModeBypass, tc.explicit,
				execution)
			if err != nil || id != testThread {
				t.Fatalf("start: id %q err %v", id, err)
			}
			turns := f.sent("turn/start")
			if len(turns) != 1 {
				t.Fatalf("turn/start calls: %d", len(turns))
			}
			var turn map[string]any
			if err := json.Unmarshal(turns[0].Params, &turn); err != nil {
				t.Fatal(err)
			}
			if turn["effort"] != tc.configured {
				t.Fatalf("native effort = %v, want %q", turn["effort"], tc.configured)
			}

			if err := f.push(map[string]any{"method": "turn/completed", "params": map[string]any{
				"threadId": testThread, "turn": map[string]any{"id": "turn-1", "status": "completed"},
			}}); err != nil {
				t.Fatal(err)
			}
			waitFor(t, "first turn completed", func() bool {
				p.mu.Lock()
				defer p.mu.Unlock()
				return !p.busy && c.turn() == ""
			})
			// A follow-up inherits both the custom launcher and effort from the saved conversation.
			if id, err := n.assistantSend(context.Background(), testThread, cwd, "", "Continue", nil, "", ""); err != nil || id != testThread {
				t.Fatalf("follow-up: id %q err %v", id, err)
			}
			turns = f.sent("turn/start")
			if len(turns) != 2 {
				t.Fatalf("follow-up turn/start calls: %d", len(turns))
			}
			if err := json.Unmarshal(turns[1].Params, &turn); err != nil {
				t.Fatal(err)
			}
			if turn["effort"] != tc.configured || len(f.sent("thread/start")) != 1 {
				t.Fatalf("follow-up lost the native effort or restarted the thread: %v", turn)
			}
		})
	}
}

func TestAssistantAliasEffortValidationKeepsAgentBoundaries(t *testing.T) {
	n := codexTestNode(t)
	probedLaunchers(t, map[string]string{"coding-gateway": "codex", "chat-gateway": "claude"})
	for _, tc := range []struct {
		launcher, mode, effort, errorText string
	}{
		{"coding-gateway", "", "ultra", ""},
		{"coding-gateway", "", "minimal", ""},
		{"chat-gateway", "", "ultra", "unknown effort level"},
		{"chat-gateway", "", "minimal", "unknown effort level"},
		{"coding-gateway", "", "made-up", "unknown effort level"},
		{"coding-gateway", "made-up", "ultra", "unknown permission mode"},
		{"chat-gateway", "plan", "max", ""},
	} {
		err := n.validAssistantModes(context.Background(), tc.launcher, tc.mode, tc.effort)
		if tc.errorText == "" && err != nil || tc.errorText != "" && (err == nil || !strings.Contains(err.Error(), tc.errorText)) {
			t.Errorf("%s/%s/%s: %v, want %q", tc.launcher, tc.mode, tc.effort, err, tc.errorText)
		}
	}
}

func TestCodexAssistantAliasColdSpawnAndWarmAcceptUltra(t *testing.T) {
	for _, warm := range []bool{false, true} {
		t.Run(map[bool]string{false: "cold spawn", true: "warm spawn"}[warm], func(t *testing.T) {
			fakeCodexHome(t, testThread)
			for _, rc := range []string{".zshrc", ".bashrc"} {
				writeFile(t, filepath.Join(os.Getenv("HOME"), rc), "cx_test() { command codex \"$@\"; }\n")
			}
			n := codexTestNode(t)
			probedLaunchers(t, map[string]string{"cx_test": "codex"})
			withFakeHub(t, n)
			cwd := t.TempDir()
			execution := modelExecution{Effort: "ultra", UseEffort: true, Fingerprint: "configured-effort"}
			var p *assistantProc
			var err error
			if warm {
				err = n.assistantWarm(cwd, "cx_test", execution)
				n.mu.Lock()
				p = n.spare
				n.mu.Unlock()
			} else {
				p, err = n.spawnCodexWithMode("", cwd, "cx_test", false, false, proto.ModeBypass, execution)
			}
			if err != nil || p == nil {
				t.Fatalf("spawn: %v, process %v", err, p != nil)
			}
			t.Cleanup(p.stop)
			if p.agent != "codex" || p.model.Effort != "ultra" || p.id != testThread {
				t.Fatalf("native process: agent=%q effort=%q id=%q", p.agent, p.model.Effort, p.id)
			}
		})
	}
}

func TestAssistantSpawnsKeepModelAndAgentValidation(t *testing.T) {
	n := codexTestNode(t)
	if err := n.assistantWarm(t.TempDir(), "invalid;launcher", modelExecution{Effort: "ultra"}); err == nil || !strings.Contains(err.Error(), "invalid launcher") {
		t.Fatalf("warmup must validate the launcher name before probing its kind: %v", err)
	}
	if _, err := n.spawnCodexWithMode("", t.TempDir(), "coding-gateway", false, false, "", modelExecution{Model: "not a model", Effort: "ultra"}); err == nil || err.Error() != "invalid model" {
		t.Fatalf("invalid Codex model: %v", err)
	}
	// A call already dispatched to Claude must not acquire Codex-only levels from the command's spelling.
	if _, err := n.spawnClaude("", t.TempDir(), "codex", false, false, modelExecution{Effort: "ultra"}); err == nil || !strings.Contains(err.Error(), "unknown effort level") {
		t.Fatalf("Claude spawn accepted Codex-only effort: %v", err)
	}
}
