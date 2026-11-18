package node

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/host"
	"github.com/nwverse/persona/server/internal/proto"
)

func TestCodexSessionEffortDoesNotFollowComputerDefault(t *testing.T) {
	for _, terminal := range []bool{false, true} {
		t.Run(map[bool]string{false: "history", true: "terminal"}[terminal], func(t *testing.T) {
			n := remoteTestNode(t)
			const id = "11111111-2222-4333-8444-555555555555"
			config := filepath.Join(n.home, ".codex", "config.toml")
			writeFile(t, config, "model_reasoning_effort = \"low\"\n")
			now := time.Now()
			writeRollout(t, n.home, id, n.home, now, now,
				`{"timestamp":"2026-11-13T12:00:00Z","type":"turn_context","payload":{"model":"gpt-test","effort":"high"}}`,
				`{"timestamp":"2026-11-13T12:01:00Z","type":"turn_context","payload":{"model":"gpt-test","effort":"max"}}`)
			rollout := filepath.Join(n.home, ".codex", "sessions", now.Format("2006/01/02"), "rollout-"+now.Format("2006-01-02T15-04-05")+"-"+id+".jsonl")
			n.registerCodex(filepath.Join(n.home, ".codex"), id, rollout)
			if terminal {
				n.mu.Lock()
				n.agents[id] = host.Agent{Agent: "codex", PaneID: "w1:p1", AgentStatus: proto.StatusIdle}
				n.mu.Unlock()
			}
			s := paneRow(t, n, id)
			if s.Effort != "max" || len(s.Efforts) != 0 {
				t.Fatalf("actual session effort must remain visible without offering unsupported edits: effort=%q choices=%v", s.Effort, s.Efforts)
			}
			got, err := os.ReadFile(config)
			if err != nil || string(got) != "model_reasoning_effort = \"low\"\n" {
				t.Fatalf("reading a session changed the computer default: %q, %v", got, err)
			}
			if terminal {
				if err := n.setModes(context.Background(), id, "", "high"); err == nil {
					t.Fatal("a terminal without a shared app-server accepted an effort change")
				}
			}
		})
	}
}

func TestCodexRecordedEffortPrecedesModelDefault(t *testing.T) {
	n := remoteTestNode(t)
	c := &codexRemote{models: map[string]codexModelEfforts{"gpt-test": {def: "low"}}}
	thread := remoteThread{model: "gpt-test"}
	if got := n.codexEffortNow("session", c, thread, "max"); got != "max" {
		t.Fatalf("recorded session effort replaced by model default: %q", got)
	}
	if got := n.codexEffortNow("session", c, thread, ""); got != "low" {
		t.Fatalf("a thread without recorded effort lost its known model default: %q", got)
	}
	thread.effort = "high"
	if got := n.codexEffortNow("session", c, thread, "max"); got != "high" {
		t.Fatalf("explicit live thread settings must still win: %q", got)
	}
}

func TestCodexSessionEffortBeyondQuickTail(t *testing.T) {
	n := remoteTestNode(t)
	const id = "11111111-2222-4333-8444-555555555555"
	now := time.Now()
	padding := `{"type":"response_item","payload":{"type":"function_call_output","call_id":"c","output":"` + strings.Repeat("x", 64<<10) + `"}}`
	events := []string{
		`{"timestamp":"2026-11-13T12:00:00Z","type":"turn_context","payload":{"cwd":"` + n.home + `","model":"gpt-test","effort":"high"}}`,
		`{"type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"hello"}]}}`,
	}
	for range 5 {
		events = append(events, padding)
	}
	events = append(events, `{"timestamp":"2026-11-13T12:01:00Z","type":"turn_context","payload":{"model":"gpt-test","effort":"max"}}`)
	for range 8 {
		events = append(events, padding)
	}
	writeRollout(t, n.home, id, n.home, now, now, events...)
	rollout := filepath.Join(n.home, ".codex", "sessions", now.Format("2006/01/02"), "rollout-"+now.Format("2006-01-02T15-04-05")+"-"+id+".jsonl")
	n.registerCodex(filepath.Join(n.home, ".codex"), id, rollout)
	waitFor(t, "latest recorded effort outside the quick tail", func() bool {
		return n.metaOf(id).effort == "max"
	})
	if got := paneRow(t, n, id).Effort; got != "max" {
		t.Fatalf("session effort after the full metadata scan: %q", got)
	}
}
