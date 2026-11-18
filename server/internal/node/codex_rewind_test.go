package node

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/rpc"
	"github.com/nwverse/persona/server/internal/transcript"
)

func rewindTurn(id string, texts ...string) codexRewindTurn {
	items := []json.RawMessage{}
	for _, text := range texts {
		items = append(items, json.RawMessage(userItem("u-"+id, text)[1]))
	}
	b, _ := json.Marshal(map[string]any{"id": id, "status": "completed", "items": items})
	var turn codexRewindTurn
	_ = json.Unmarshal(b, &turn)
	return turn
}

func TestCodexHistoryLineageExcludesAbandonedDatabaseRows(t *testing.T) {
	home := t.TempDir()
	base := writeAppRollout(t, home, remoteRaw, home, time.Now())
	segmentID := "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"
	segment := strings.TrimSuffix(base, ".jsonl") + "_" + segmentID + ".jsonl"
	header := fmt.Sprintf(`{"type":"session_meta","payload":{"id":%q,"history_base":{"thread_id":%q,"end_ordinal_exclusive":14}}}`+"\n", remoteRaw, remoteRaw)
	if err := os.WriteFile(segment, []byte(header), 0o600); err != nil {
		t.Fatal(err)
	}
	writeCodexHistory(t, home, []historyTurn{
		{thread: remoteRaw, id: "a", ordinal: 1, status: "completed", items: [][2]string{userItem("a", "retained")}},
		{thread: remoteRaw, id: "b", ordinal: 14, status: "completed", items: [][2]string{userItem("b", "abandoned")}},
		{thread: segmentID, id: "c", ordinal: 16, status: "completed", items: [][2]string{userItem("c", "replacement")}},
	})
	p := filepath.Join(home, "canonical.jsonl")
	tt := codexAppThread{ID: remoteRaw, Cwd: home, RolloutPath: segment, Paginated: true}
	if _, err := writeCodexAppHistory(context.Background(), filepath.Join(home, "thread_history_1.sqlite"), tt, p, codexHistoryMark{Turn: -1}, false); err != nil {
		t.Fatal(err)
	}
	f := transcript.NewFile(p)
	items, err := f.Refresh()
	if text := historyText(items); err != nil || !strings.Contains(text, "retained") || !strings.Contains(text, "replacement") || strings.Contains(text, "abandoned") {
		t.Fatalf("lineage: %s %v", text, err)
	}
}

func TestCodexRewindTarget(t *testing.T) {
	turns := []codexRewindTurn{rewindTurn("a", "repeat"), rewindTurn("b", "image follows", ""), rewindTurn("c", "repeat"), rewindTurn("d", "last")}
	after := 4
	i, prompt, err := codexRewindTarget(turns, rewindOpts{Prompt: "re peat", Occurrence: 2, PromptsAfter: &after})
	if err != nil || i != 0 || prompt != "repeat" {
		t.Fatalf("target %d %q %v", i, prompt, err)
	}
	after = 3
	if _, _, err := codexRewindTarget(turns, rewindOpts{Prompt: "repeat", Occurrence: 2, PromptsAfter: &after}); err == nil {
		t.Fatal("a moved message was accepted")
	}
	if _, _, err := codexRewindTarget([]codexRewindTurn{rewindTurn("a", "initial", "steer")}, rewindOpts{Prompt: "steer", Occurrence: 1}); err == nil {
		t.Fatal("rewound across an earlier input in the same turn")
	}
	turns[3].Status = "inProgress"
	if _, _, err := codexRewindTarget(turns, rewindOpts{Prompt: "repeat", Occurrence: 1}); err == nil {
		t.Fatal("rewound during an unfinished turn")
	}
}

func paginatedCodex(t *testing.T, n *Node, home, raw string, turns []historyTurn) string {
	t.Helper()
	rollout := writeAppRollout(t, home, raw, home, time.Now())
	writeCodexApp(t, home, home, codexAppSchema+codexAppRow(raw, rollout, home, "Rewind example", time.Now(), 0)+
		`ALTER TABLE threads ADD COLUMN history_mode TEXT DEFAULT 'paginated';`)
	writeCodexHistory(t, home, turns)
	return n.registerCodex(home, raw, rollout)
}

func TestCodexRewindNativeAndLegacyMethod(t *testing.T) {
	for _, legacy := range []bool{false, true} {
		t.Run(map[bool]string{false: "revert", true: "rollback"}[legacy], func(t *testing.T) {
			n := remoteTestNode(t)
			home := t.TempDir()
			f, _ := connectRemoteFixture(t, n, home, "rewind", remoteRaw)
			turns := []historyTurn{
				{thread: remoteRaw, id: "a", ordinal: 0, status: "completed", items: [][2]string{userItem("ua", "first"), answerItem("aa", "First answer")}},
				{thread: remoteRaw, id: "b", ordinal: 1, status: "completed", items: [][2]string{userItem("ub", "second"), answerItem("ab", "Second answer")}},
			}
			id := paginatedCodex(t, n, home, remoteRaw, turns)
			f.mu.Lock()
			if legacy {
				f.reject, f.rejectCode = "thread/revert", -32601
			}
			f.response = func(method string, p map[string]any) (any, bool) {
				switch method {
				case "thread/turns/list":
					rows := []codexRewindTurn{}
					for _, turn := range turns {
						rows = append(rows, rewindTurn(turn.id, map[string]string{"a": "first", "b": "second"}[turn.id]))
					}
					// Exercise real cursor pagination with one turn per page.
					if p["cursor"] == "next" {
						return map[string]any{"data": rows[1:]}, true
					}
					if len(rows) > 1 {
						return map[string]any{"data": rows[:1], "nextCursor": "next"}, true
					}
					return map[string]any{"data": rows}, true
				case "thread/revert", "thread/rollback":
					keep := 1
					if p["beforeTurnId"] == "a" || p["numTurns"] == float64(1) && len(turns) == 1 {
						keep = 0
					}
					turns = turns[:keep]
					writeCodexHistory(t, home, turns)
					return map[string]any{}, true
				}
				return nil, false
			}
			f.mu.Unlock()
			before, err := n.file(id)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = before.Refresh(); err != nil {
				t.Fatal(err)
			}
			if !n.canRewindCodex(id) {
				t.Fatal("paginated native history was not supported")
			}
			for _, prompt := range []string{"second", "first"} {
				after := 0
				res, err := n.rewind(context.Background(), id, rewindOpts{Prompt: prompt, Occurrence: 1, PromptsAfter: &after, Restore: RestoreConversation})
				if err != nil || res["input_cleared"] != true || res["code_restored"] != false {
					t.Fatalf("%s: %v %v", prompt, res, err)
				}
				page, _, _ := before.Page(0, 100)
				if strings.Contains(historyText(page), prompt) {
					t.Fatalf("rewound history: %s", historyText(page))
				}
			}
			if f.count("thread/revert") != 2 || legacy && f.count("thread/rollback") != 2 || !legacy && f.count("thread/rollback") != 0 {
				t.Fatal("native method fallback")
			}
		})
	}
}

func TestCodexPaginatedHistoryKeepsThePrefixAndAnEmptyConversation(t *testing.T) {
	n := remoteTestNode(t)
	home := n.codexHome()
	turns := []historyTurn{{thread: remoteRaw, id: "a", ordinal: 0, status: "completed", items: [][2]string{userItem("u", "retained prefix")}}}
	id := paginatedCodex(t, n, home, remoteRaw, turns)
	src, _ := n.codexSource(id)
	segment := strings.TrimSuffix(src.path, ".jsonl") + "_" + remoteOther + ".jsonl"
	header := fmt.Sprintf(`{"type":"session_meta","payload":{"id":%q,"history_base":{"thread_id":%q,"end_ordinal_exclusive":1}}}`+"\n", remoteRaw, remoteRaw)
	if err := os.WriteFile(segment, []byte(header), 0o600); err != nil {
		t.Fatal(err)
	}
	writeCodexApp(t, home, home, fmt.Sprintf("UPDATE threads SET rollout_path = '%s'", segment))
	invalidateCodexPaginated(home)

	// The old rollout still exists and contains an abandoned branch.
	if err := os.WriteFile(src.path, []byte(`{"type":"session_meta","payload":{}}`+"\n"+`{"type":"event_msg","payload":{"type":"user_message","message":"abandoned raw rollout"}}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	f, err := n.file(id)
	if err != nil {
		t.Fatal(err)
	}
	if f.Where() == src.path || !strings.Contains(f.Where(), "codex-app-history") {
		t.Fatalf("read the old rollout: %s", f.Where())
	}
	got, err := f.Refresh()
	if err != nil || !strings.Contains(historyText(got), "retained prefix") {
		t.Fatalf("prefix: %s %v", historyText(got), err)
	}
	writeCodexHistory(t, home, nil)
	if err := n.refreshCodexAppHistory(id); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(f.Where()); err != nil {
		t.Fatalf("the empty conversation disappeared: %v", err)
	}
	if b, _ := os.ReadFile(f.Where()); strings.Count(string(b), "\n") != 1 {
		t.Fatalf("want a header-only canonical history: %s", b)
	}
	// The continuation writes new turns under its own rollout ID, leaving the base segment's rows unchanged.
	turns[0].thread = remoteOther
	turns[0].id, turns[0].items = "replacement", [][2]string{userItem("new", "new branch")}
	writeCodexHistory(t, home, turns)
	n.refreshCodexAppHistory(id)
	fresh, err := transcript.NewFile(f.Where()).Refresh()
	if err != nil || !strings.Contains(historyText(fresh), "new branch") || strings.Contains(historyText(fresh), "retained prefix") {
		t.Fatalf("replacement: %s %v", historyText(fresh), err)
	}
	if _, err := os.Stat(filepath.Join(home, "state_5.sqlite")); err != nil {
		t.Fatal(err)
	}
}

func TestRuntimeNativeRewindCapability(t *testing.T) {
	n, _ := runtimeNode(t)
	hello := dshHello()
	hello["caps"].(map[string]any)["rewind"] = true
	h := dialRuntime(t, n, hello)
	h.mu.Lock()
	h.answer = func(method string, _ json.RawMessage) (any, error) {
		if method == "session.rewind" {
			return map[string]any{"prompt": "example", "input_cleared": true}, nil
		}
		return nil, &rpc.Error{Code: "unsupported", Message: "unsupported"}
	}
	h.mu.Unlock()
	_ = h.peer.Notify("runtime.sessions", map[string]any{"sessions": []map[string]any{{"session_id": "rewind-example", "status": "idle"}}})
	waitFor(t, "rewind session listed", func() bool { _, ok := listed(t, n)["rewind-example"]; return ok })
	s := listed(t, n)["rewind-example"]
	if s.Capabilities.Rewind == nil || !*s.Capabilities.Rewind || s.Capabilities.RewindCode == nil || *s.Capabilities.RewindCode {
		t.Fatal("runtime rewind capabilities")
	}
	res, err := n.rewind(context.Background(), s.SessionID, rewindOpts{Prompt: "example", Occurrence: 1, Restore: RestoreConversation})
	if err != nil || res["input_cleared"] != true || len(h.got("session.rewind")) != 1 {
		t.Fatalf("runtime dispatch: %v %v", res, err)
	}
	if _, err := n.rewind(context.Background(), s.SessionID, rewindOpts{Prompt: "example", Occurrence: 1, Restore: RestoreCodeAndConv}); err == nil || len(h.got("session.rewind")) != 1 {
		t.Fatal("file rewind reached a conversation-only runtime")
	}

	// A row narrows the runtime's rewind for its own session, and offers the files with it (Paseo: what its provider can).
	_ = h.peer.Notify("runtime.sessions", map[string]any{"sessions": []map[string]any{
		{"session_id": "rewind-example", "status": "idle", "capabilities": map[string]any{"rewind": true, "rewind_code": true}},
		{"session_id": "rewind-none", "status": "idle", "capabilities": map[string]any{"rewind": false, "rewind_code": true}},
	}})
	waitFor(t, "rows with their own rewind", func() bool { _, ok := listed(t, n)["rewind-none"]; return ok })
	rows := listed(t, n)
	if c := rows["rewind-example"].Capabilities; !*c.Rewind || c.RewindCode == nil || !*c.RewindCode {
		t.Fatal("a row offering the files with its rewind")
	}
	if c := rows["rewind-none"].Capabilities; *c.Rewind || *c.RewindCode {
		t.Fatal("a row without rewind offers none, files included")
	}
	if _, err := n.rewind(context.Background(), "rewind-example", rewindOpts{Prompt: "example", Occurrence: 1, Restore: RestoreCodeAndConv}); err != nil || len(h.got("session.rewind")) != 2 {
		t.Fatalf("file rewind on a row offering it: %v", err)
	}
	if _, err := n.rewind(context.Background(), "rewind-none", rewindOpts{Prompt: "example", Occurrence: 1, Restore: RestoreConversation}); err == nil || len(h.got("session.rewind")) != 2 {
		t.Fatal("rewind reached a row without it")
	}
}

func TestCodexOrdinaryPaginatedHistoryKeepsItsRollout(t *testing.T) {
	n := remoteTestNode(t)
	home := n.codexHome()
	id := paginatedCodex(t, n, home, remoteRaw, []historyTurn{{thread: remoteRaw, id: "a", ordinal: 0, status: "completed", items: [][2]string{userItem("a", "Indexed input")}}})
	src, _ := n.codexSource(id)
	file, err := n.file(id)
	if err != nil {
		t.Fatal(err)
	}
	if file.Where() != src.path {
		t.Fatalf("ordinary rollout replaced by %s", file.Where())
	}
	if _, err := os.Stat(n.codexAppHistoryFile(src)); !os.IsNotExist(err) {
		t.Fatalf("ordinary history was materialized: %v", err)
	}
}
