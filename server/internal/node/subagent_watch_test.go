package node

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/proto"
	"github.com/nwverse/persona/server/internal/rpc"
)

const (
	subParent = "aaaaaaaa-0000-0000-0000-000000000002"
	subAgent  = "a1b2c3d4e5f60718"
	subID     = subParent + "@" + subAgent
)

// subagentFixture lays out a parent transcript with one subagent file beside it and returns the subagent's path.
func subagentFixture(t *testing.T) (*Node, string) {
	t.Helper()
	n, home := testNode(t)
	tp := filepath.Join(home, ".claude", "projects", "-src-app", subParent+".jsonl")
	dir := filepath.Join(strings.TrimSuffix(tp, ".jsonl"), "subagents")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(tp, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	n.paths[subParent] = tp
	return n, filepath.Join(dir, "agent-"+subAgent+".jsonl")
}

func appendSubagentText(t *testing.T, path, text string) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	line := `{"type":"assistant","timestamp":"2026-11-05T09:01:00Z","isSidechain":true,"message":{"role":"assistant","content":[{"type":"text","text":"` + text + `"}]}}` + "\n"
	if _, err := f.WriteString(line); err != nil {
		t.Fatal(err)
	}
}

// servedNode connects n to a fake hub the way a real connection does: the hub's session.watch is a request whose ctx
// ends when it is answered (rpc.Peer), so a watch hanging off that ctx would die with its reply (#3139).
func servedNode(t *testing.T, n *Node) (call func(method string, params any) error, hub *hubEnd) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	n2hR, n2hW := io.Pipe()
	h2nR, h2nW := io.Pipe()
	n.peer = rpc.New("n", n2hW)
	n.peer.OnRequest = n.handle
	hp := rpc.New("h", h2nW)
	hub = &hubEnd{}
	hp.OnEvent = func(event string, params json.RawMessage) {
		if event != "session.items" {
			return
		}
		var p struct {
			Items   []proto.Item `json:"items"`
			History string       `json:"history"`
			FromSeq int          `json:"from_seq"`
			ToSeq   int          `json:"to_seq"`
		}
		_ = json.Unmarshal(params, &p)
		hub.mu.Lock()
		hub.items = append(hub.items, p.Items...)
		hub.batches = append(hub.batches, pushed{len(p.Items), p.History, p.FromSeq, p.ToSeq})
		hub.mu.Unlock()
	}
	go func() { _ = hp.Serve(ctx, n2hR) }()
	go func() { _ = n.peer.Serve(ctx, h2nR) }()
	t.Cleanup(func() { cancel(); _ = n2hW.Close(); _ = h2nW.Close() })
	return func(method string, params any) error { return hp.Call(ctx, method, params, nil) }, hub
}

// The hub's watch of a subagent keeps pushing what the file grows by after the reply to session.watch (#3139), and
// session.unwatch ends it.
func TestSubagentWatchOutlivesTheWatchRequest(t *testing.T) {
	n, jp := subagentFixture(t)
	call, hub := servedNode(t, n)
	appendSubagentText(t, jp, "first")
	if err := call("session.watch", map[string]any{"session_id": subID, "since_seq": 0}); err != nil {
		t.Fatal(err)
	}
	appendSubagentText(t, jp, "second")
	waitFor(t, "the line appended after the reply", func() bool { return hasText(hub, "second") })

	if err := call("session.unwatch", map[string]any{"session_id": subID}); err != nil {
		t.Fatal(err)
	}
	time.Sleep(400 * time.Millisecond) // the loop ticks every 250 ms
	appendSubagentText(t, jp, "third")
	time.Sleep(800 * time.Millisecond)
	if hasText(hub, "third") {
		t.Fatal("a line was pushed after session.unwatch")
	}
}

func hasText(h *hubEnd, text string) bool {
	for _, it := range h.got() {
		if it.Text == text {
			return true
		}
	}
	return false
}

// Followed subagent transcripts are bounded; a watch past the bound is refused (the page falls back to fetching), and
// the others keep going.
func TestSubagentWatchesAreBounded(t *testing.T) {
	n, jp := subagentFixture(t)
	hub := withHub(t, n)
	dir := filepath.Dir(jp)
	for i := 0; i < maxSubagentWatches; i++ {
		agent := "agent" + string(rune('a'+i))
		appendSubagentText(t, filepath.Join(dir, "agent-"+agent+".jsonl"), "x")
		if err := n.watch(subParent+"@"+agent, 0); err != nil {
			t.Fatalf("watch %d: %v", i, err)
		}
	}
	t.Cleanup(func() {
		for i := 0; i < maxSubagentWatches; i++ {
			n.unwatch(subParent + "@agent" + string(rune('a'+i)))
		}
	})
	appendSubagentText(t, jp, "over")
	if err := n.watch(subID, 0); err == nil {
		t.Fatal("a watch past the bound was taken")
	}
	// A repeated watch of one followed is not a new one, and a freed place can be taken.
	if err := n.watch(subParent+"@agenta", 0); err != nil {
		t.Fatalf("watching a followed one again: %v", err)
	}
	n.unwatch(subParent + "@agentb")
	appendSubagentText(t, jp, "after")
	if err := n.watch(subID, 0); err != nil {
		t.Fatalf("a freed place was not taken: %v", err)
	}
	t.Cleanup(func() { n.unwatch(subID) })
	appendSubagentText(t, jp, "live")
	waitFor(t, "the new watch to push", func() bool { return hasText(hub, "live") })
}

// A subagent file grows the way Claude Code writes one (its line kinds, in the order seen in real runs: the prompt,
// attachments chained by parentUuid, api-request lines that carry neither uuid nor isSidechain, a tool call and its
// result, the report), in several appends: every item reaches the hub once, in order, and the batches continue from
// each other (same seq / cursor rules as a session's).
func TestSubagentWatchFollowsAClaudeShapedGrowth(t *testing.T) {
	n, jp := subagentFixture(t)
	call, hub := servedNode(t, n)
	const ts = `"timestamp":"2026-11-05T09:01:0`
	growth := [][]string{
		{
			`{"type":"user","uuid":"u1","parentUuid":null,"isSidechain":true,"agentId":"` + subAgent + `",` + ts + `0Z","message":{"role":"user","content":"Read the temp file and report its first line."}}`,
			`{"type":"attachment","uuid":"t1","parentUuid":"u1","isSidechain":true,` + ts + `0Z","attachment":{"type":"date_change","newDate":"2026-11-05"}}`,
			`{"type":"api-request-shape","requestShape":"foreground"}`,
			`{"type":"api-request-blob","message":{"content":[{"type":"text","text":"request body"}]}}`,
		},
		{
			`{"type":"assistant","uuid":"a1","parentUuid":"t1","isSidechain":true,` + ts + `1Z","message":{"role":"assistant","content":[{"type":"tool_use","id":"toolu_1","name":"Read","input":{"file_path":"/tmp/example.txt"}}]}}`,
		},
		{
			`{"type":"user","uuid":"r1","parentUuid":"a1","isSidechain":true,` + ts + `2Z","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_1","content":"first line"}]}}`,
			`{"type":"api-request-shape","requestShape":"foreground"}`,
		},
		{
			`{"type":"assistant","uuid":"a2","parentUuid":"r1","isSidechain":true,` + ts + `3Z","message":{"role":"assistant","content":[{"type":"text","text":"The first line is: first line"}]}}`,
		},
	}
	write := func(lines []string) {
		t.Helper()
		f, err := os.OpenFile(jp, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		for _, l := range lines {
			if _, err := f.WriteString(l + "\n"); err != nil {
				t.Fatal(err)
			}
		}
	}
	write(growth[0])
	if err := call("session.watch", map[string]any{"session_id": subID, "since_seq": 0}); err != nil {
		t.Fatal(err)
	}
	for _, lines := range growth[1:] {
		time.Sleep(350 * time.Millisecond)
		write(lines)
	}
	waitFor(t, "the report to arrive", func() bool { return hasText(hub, "The first line is: first line") })
	time.Sleep(600 * time.Millisecond) // anything pushed twice would be in by now

	seen := map[string]bool{}
	last := 0
	for _, it := range hub.got() {
		if seen[it.ID] {
			t.Fatalf("item %s pushed twice", it.ID)
		}
		seen[it.ID] = true
		if it.Seq <= last {
			t.Fatalf("item %s (seq %d) after seq %d", it.ID, it.Seq, last)
		}
		last = it.Seq
	}
	var tools, results int
	for _, it := range hub.got() {
		switch it.Kind {
		case proto.KindToolUse:
			tools++
		case proto.KindToolResult:
			results++
		}
	}
	if tools != 1 || results != 1 {
		t.Fatalf("tool call %d, result %d; items %+v", tools, results, hub.got())
	}
	// Each batch continues from the one before (the first from the lines the page's own read had, written before the
	// watch): the pages' cursor never has a hole.
	to := len(growth[0])
	for i, b := range hub.spans() {
		if b.fromSeq != to {
			t.Fatalf("batch %d starts at %d, the one before ended at %d (%+v)", i, b.fromSeq, to, hub.spans())
		}
		to = b.toSeq
	}
	if want := 4 + 1 + 2 + 1; to != want {
		t.Fatalf("the cursor ends at line %d, the file has %d", to, want)
	}
}
