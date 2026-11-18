package node

// Golden test for the message the node writes into an assistant conversation (docs/assistant/prompts.md). Regenerate after an
// intended change with UPDATE_GOLDEN=1 go test ./internal/node -run Golden, and read the diff.

import (
	"encoding/json"
	"github.com/nwverse/persona/server/internal/prompts"
	"github.com/nwverse/persona/server/internal/proto"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func goldenNode(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", "prompts", name+".golden")
	if os.Getenv("UPDATE_GOLDEN") != "" {
		_ = os.MkdirAll(filepath.Dir(path), 0o755)
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil || got != string(want) {
		t.Errorf("%s: got\n%s\nwant (%v)\n%s", name, got, err, want)
	}
}

// The memory section of the Codex assistant's developer instructions: no index yet, an index, one too long to show.
func TestGoldenCodexMemory(t *testing.T) {
	dir := t.TempDir()
	goldenNode(t, "codex_memory.empty", strings.ReplaceAll(codexMemoryPrompt(dir), dir, "<dir>"))
	_ = os.WriteFile(filepath.Join(dir, "MEMORY.md"), []byte("\n- [邮箱](mail.md) — 收信在 hub 上\n- [习惯](habits.md) — 回复先给结论\n\n"), 0o600)
	goldenNode(t, "codex_memory.index", strings.ReplaceAll(codexMemoryPrompt(dir), dir, "<dir>"))
	_ = os.WriteFile(filepath.Join(dir, "MEMORY.md"), []byte("  "+strings.Repeat("- [条目](x.md) — 说明\n", 1200)), 0o600)
	goldenNode(t, "codex_memory.long", strings.ReplaceAll(codexMemoryPrompt(dir), dir, "<dir>"))
}

// What an assistant on an agent runtime gets with a new conversation's first message (assistant_runtime.go): the
// memory, without and with a gated command's skill on the machine.
func TestGoldenRuntimeInstructions(t *testing.T) {
	n, home := testNode(t)
	n.logger = log.New(io.Discard, "", 0)
	g := exampleGate("")
	g.Tool = &proto.GateTool{Name: "example_api", Description: "Run ex."}
	n.larkCfg, n.larkCfgAt = proto.AssistantLark{Gates: []proto.CommandGate{g}}, time.Now().Add(time.Hour)
	cwd := filepath.Join(home, ".persona", "assistant")
	mem := t.TempDir()
	_ = os.WriteFile(filepath.Join(mem, "MEMORY.md"), []byte("- [习惯](habits.md) — 回复先给结论\n"), 0o600)
	clean := func(s string) string { return strings.ReplaceAll(s, mem, "<dir>") }
	goldenNode(t, "runtime_instructions", clean(n.runtimeInstructions(cwd, mem)))
	script := filepath.Join(cwd, ".agents", "skills", "example", "scripts", "ex.sh")
	_ = os.MkdirAll(filepath.Dir(script), 0o755)
	_ = os.WriteFile(script, []byte("#!/bin/bash\n"), 0o755)
	goldenNode(t, "runtime_instructions.gate", clean(n.runtimeInstructions(cwd, mem)))
}

func TestGoldenLostTasksNotice(t *testing.T) {
	n := assistantNode(t)
	const id = "11111111-2222-3333-4444-000000000001"
	n.state.put(id, sessionState{Cwd: "/home/u/.persona/assistant", Tasks: []string{"tail the deploy log", "Sleep 23 minutes"}})
	var got string
	n.recoverAssistantTasks(func(_, text string) error { got = text; return nil })
	path := filepath.Join("testdata", "prompts", "lost_tasks.golden")
	if os.Getenv("UPDATE_GOLDEN") != "" {
		_ = os.MkdirAll(filepath.Dir(path), 0o755)
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil || got != string(want) {
		t.Fatalf("got\n%s\nwant (%v)\n%s", got, err, want)
	}
}

// The line the node writes into a Claude session's peer inbox for a message from the phone (claudepeer.go): Claude
// Code's envelope around agents/peer_message, which says whose the words are.
func TestGoldenPeerMessage(t *testing.T) {
	b, err := peerMessage("0d000000-0000-4000-8000-00000000000a", "reply with OK")
	if err != nil {
		t.Fatal(err)
	}
	var line struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	}
	if err := json.Unmarshal(b, &line); err != nil {
		t.Fatal(err)
	}
	goldenNode(t, "peer_message", line.Message.Content)
}

// What the phone's Stop writes into the peer inbox of a Claude session outside Persona's terminals (claudebg.go
// interruptOutside): agents/peer_stop as the user's words of a peer message.
func TestGoldenPeerStop(t *testing.T) {
	b, err := peerMessageAt("0d000000-0000-4000-8000-00000000000a", prompts.PeerStop(), peerNow, "")
	if err != nil {
		t.Fatal(err)
	}
	var line struct {
		Priority string `json:"priority"`
		Message  struct {
			Content string `json:"content"`
		} `json:"message"`
	}
	if err := json.Unmarshal(b, &line); err != nil || line.Priority != peerNow {
		t.Fatalf("%s: %v", b, err)
	}
	goldenNode(t, "peer_stop", line.Message.Content)
}
