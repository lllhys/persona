package node

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/proto"
	"github.com/nwverse/persona/server/internal/rpc"
)

// runtimeGone: no connection of runtime rt is left.
func runtimeGone(n *Node, rt string) bool {
	n.runtimes.mu.Lock()
	defer n.runtimes.mu.Unlock()
	return n.runtimes.byRuntime(rt) == nil
}

// A runtime that leaves the node (Paseo stopped for more than half a minute, the computer switched to another engine)
// leaves its Claude conversations behind (#8131): each of its rows whose conversation is a transcript here stays
// listed under its own id, not running (one listed under its Claude session id and one under an id of its own alike),
// read from the transcript; a message, a rename or closing it is refused as away, nothing sent. A row with no
// transcript here (another agent's) leaves with the runtime. Once it is back, its list says what it has.
func TestRuntimeAwayKeepsItsClaudeConversations(t *testing.T) {
	const (
		claudeID = "5d510000-0000-4000-8000-0000000000c9" // started in Paseo's app: listed under its Claude session
		ownID    = "paseo-c1"                             // started from Persona: its own id, its transcript another
		ownT     = "5d510000-0000-4000-8000-0000000000c1"
		codex    = "paseo-c2" // another agent's: no transcript here
	)
	t.Setenv("PERSONA_HOME", "")
	n, _ := runtimeNode(t)
	t.Cleanup(freshOffline)
	dir := filepath.Join(n.home, ".claude", "projects", "-src-app")
	writeClaudeRun(t, dir, claudeID, time.Minute, "sdk-cli", "sdk-cli") // Paseo's transcripts read as one-shot runs
	writeClaudeRun(t, dir, ownT, 2*time.Minute, "sdk-cli", "sdk-cli")
	host := dialRuntime(t, n, paseoHello())
	rows := []map[string]any{
		{"session_id": claudeID, "transcript": claudeID, "agent": "claude", "title": "Fix the build", "cwd": "/src/app", "status": "idle"},
		{"session_id": ownID, "transcript": ownT, "agent": "claude", "title": "Write the docs", "cwd": "/src/app", "status": "working",
			"queued": []string{"and the tests"}, "background": []map[string]any{{"id": "agent-1", "kind": "agent", "description": "a reviewer"}}},
		{"session_id": codex, "agent": "codex", "title": "Review", "cwd": "/src/app", "status": "idle"},
	}
	_ = host.peer.Notify("runtime.sessions", map[string]any{"sessions": rows, "unlisted_transcripts": []string{}})
	waitFor(t, "listed by Paseo", func() bool { return listed(t, n)[codex].SourceKind == proto.SourceRuntime })

	_ = host.conn.Close()
	waitFor(t, "the runtime left", func() bool { return runtimeGone(n, "paseo") && listed(t, n)[ownID].Status == proto.StatusOffline })
	freshOffline()
	got := listed(t, n)
	for _, id := range []string{claudeID, ownID} {
		s := got[id]
		if s.Status != proto.StatusOffline || s.SourceKind != proto.SourceRuntime || s.LastText != "done" || s.Cwd != "/src/app" {
			t.Fatalf("%s while Paseo is away: %+v", id, s)
		}
		if c := s.Capabilities; c == nil || !c.Read || !c.Send || c.Interrupt || *c.Rename || *c.Close || *c.Modes || *c.Rewind {
			t.Fatalf("%s offers %+v while Paseo is away", id, c)
		}
	}
	if s := got[ownID]; len(s.Queued) != 0 || len(s.Background) != 0 || s.Title != "Write the docs" {
		t.Fatalf("what the runtime said it ran is still on the row: %+v", s)
	}
	if _, ok := got[codex]; ok {
		t.Fatal("a session with no transcript here stays without its runtime")
	}
	if _, ok := got[ownT]; ok {
		t.Fatal("a conversation is listed under its transcript's id too")
	}

	for _, id := range []string{claudeID, ownID} {
		raw, err := nodeCall(t, n, "session.items", map[string]any{"session_id": id, "limit": 20})
		if err != nil || !strings.Contains(string(raw), `"summarize the diff"`) {
			t.Fatalf("%s's content while Paseo is away: %s %v", id, raw, err)
		}
		if _, err := nodeCall(t, n, "session.info", map[string]any{"session_id": id}); err != nil {
			t.Fatalf("%s's info: %v", id, err)
		}
		_, err = nodeCall(t, n, "session.send", map[string]any{"session_id": id, "text": "go on", "client_msg_id": "m-" + id})
		var re *rpc.Error
		if !errors.As(err, &re) || re.Code != "runtime_away" || !strings.Contains(string(re.Data), `"none"`) {
			t.Fatalf("a message to %s while Paseo is away: %v", id, err)
		}
		for _, m := range []string{"session.rename", "session.close", "session.interrupt"} {
			if _, err := nodeCall(t, n, m, map[string]any{"session_id": id, "title": "x"}); !errors.As(err, &re) || re.Code != "runtime_away" {
				t.Fatalf("%s %s while Paseo is away: %v", m, id, err)
			}
		}
	}

	// Back, listing one of them: the list is what it has; the other leaves.
	host = dialRuntime(t, n, paseoHello())
	_ = host.peer.Notify("runtime.sessions", map[string]any{"sessions": rows[:1], "unlisted_transcripts": []string{}})
	waitFor(t, "listed by Paseo again", func() bool { return listed(t, n)[claudeID].Status == proto.StatusIdle })
	if _, ok := listed(t, n)[ownID]; ok {
		t.Fatal("a session the runtime no longer lists stays once it is back")
	}

	// Away again, and away for longer than the list of sessions not running goes back: they go.
	_ = host.conn.Close()
	waitFor(t, "away again", func() bool { return runtimeGone(n, "paseo") && listed(t, n)[claudeID].Status == proto.StatusOffline })
	n.runtimes.mu.Lock()
	a := n.runtimes.away["paseo"]
	a.gone = time.Now().Add(-unlistedKeep - time.Second)
	n.runtimes.away["paseo"] = a
	n.runtimes.mu.Unlock()
	freshOffline()
	if s, ok := listed(t, n)[claudeID]; ok {
		t.Fatalf("listed long after its runtime left: %+v", s)
	}
}

// An agent Paseo archived that it listed under its Claude session id (one started in Paseo's app, closed from Persona
// or in Paseo) is a Claude session not running once Paseo names its transcript as unlisted (#8131): its content is
// the transcript, read by the node, and a message has Paseo take it over (session.start with resume) before it is
// sent. Sent to the runtime that listed it, every call was refused until the node restarted and forgot the id.
func TestRuntimeArchivedTranscriptIsTheNodes(t *testing.T) {
	const id = "5d510000-0000-4000-8000-0000000000b7"
	t.Setenv("PERSONA_HOME", "")
	n, _ := runtimeNode(t)
	t.Cleanup(freshOffline)
	writeClaudeRun(t, filepath.Join(n.home, ".claude", "projects", "-src-app"), id, time.Minute, "sdk-cli", "sdk-cli")
	if err := n.useEngine(context.Background(), proto.EnginePaseo); err != nil {
		t.Fatal(err)
	}
	host := dialRuntime(t, n, paseoEngineHello())
	archived := false
	host.mu.Lock()
	host.answer = func(method string, params json.RawMessage) (any, error) {
		var p struct {
			SessionID string `json:"session_id"`
			Resume    string `json:"resume"`
		}
		_ = json.Unmarshal(params, &p)
		if method == "session.start" && p.Resume == id {
			archived = false // Paseo restores the archived agent
			return map[string]any{"session": map[string]any{"session_id": id, "transcript": id, "agent": "claude", "status": "idle", "cwd": "/src/app"}}, nil
		}
		if archived && p.SessionID == id {
			return nil, &rpc.Error{Message: "Paseo has no such session (" + id + "): it may have been archived or deleted", Code: "not_found"}
		}
		return map[string]any{"ok": true, "items": []any{}}, nil
	}
	host.mu.Unlock()
	row := map[string]any{"session_id": id, "transcript": id, "agent": "claude", "title": "Fix the build", "cwd": "/src/app", "status": "idle"}
	_ = host.peer.Notify("runtime.sessions", map[string]any{"sessions": []map[string]any{row}, "unlisted_transcripts": []string{}})
	waitFor(t, "listed by Paseo", func() bool { return listed(t, n)[id].SourceKind == proto.SourceRuntime })

	// Archived in Paseo: no row, its transcript named as unlisted.
	host.mu.Lock()
	archived = true
	host.mu.Unlock()
	_ = host.peer.Notify("runtime.sessions", map[string]any{"sessions": []map[string]any{}, "unlisted_transcripts": []string{id}})
	waitFor(t, "listed as not running", func() bool { s := listed(t, n)[id]; return s.Status == proto.StatusOffline && s.SourceKind == "" })

	raw, err := nodeCall(t, n, "session.items", map[string]any{"session_id": id, "limit": 20})
	if err != nil || !strings.Contains(string(raw), `"summarize the diff"`) || len(host.got("session.items")) != 0 {
		t.Fatalf("its content: %s %v (the runtime was asked %d times)", raw, err, len(host.got("session.items")))
	}
	if _, err := nodeCall(t, n, "session.send", map[string]any{"session_id": id, "text": "go on", "client_msg_id": "m1"}); err != nil {
		t.Fatalf("a message to it: %v", err)
	}
	starts, sends := host.got("session.start"), host.got("session.send")
	if len(starts) != 1 || !strings.Contains(string(starts[0]), `"resume":"`+id+`"`) || len(sends) != 1 || !strings.Contains(string(sends[0]), "go on") {
		t.Fatalf("taken over %d times (%s), sent %d times", len(starts), starts, len(sends))
	}
}

// A session none of the runtimes has, whose id is one a runtime gives its own sessions (Paseo's paseo-<agent id>: an
// agent of another provider archived in Paseo, opened from an old link after the node restarted), is refused with that
// runtime's not-found, which the manager and the apps read as "not on that computer", not with the node's transcript
// lookup failing (a 502 the iOS app showed as the computer failing, #8131). Asked again within the minute, the same
// answer, without asking the runtime again. An id of another runtime's shape is not answered for: the node's own way
// (its transcript lookup's not_found, #9551).
func TestRuntimeMissingSessionIsNotFound(t *testing.T) {
	n, _ := runtimeNode(t)
	host := dialRuntime(t, n, paseoHello())
	refuse := func(method string, params json.RawMessage) (any, error) {
		var p struct {
			SessionID string `json:"session_id"`
		}
		_ = json.Unmarshal(params, &p)
		return nil, &rpc.Error{Message: "Paseo has no such session (" + p.SessionID + "): it may have been archived or deleted", Code: "not_found"}
	}
	host.mu.Lock()
	host.answer = refuse
	host.mu.Unlock()
	for range 2 {
		for _, m := range []string{"session.items", "session.info"} {
			_, err := nodeCall(t, n, m, map[string]any{"session_id": "paseo-4b1d", "limit": 20})
			var re *rpc.Error
			if !errors.As(err, &re) || re.Code != "not_found" || !strings.Contains(re.Message, "Paseo has no such session") {
				t.Fatalf("%s of a session Paseo does not have: %v", m, err)
			}
		}
	}
	if got := len(host.got("session.items")); got != 1 {
		t.Fatalf("the runtime was asked %d times", got)
	}

	// Another runtime's refusal of an id it does not give: not its word to give. The node answers itself: it has no
	// transcript of it either, its own not_found (#9551).
	other := dialRuntime(t, n, dshHello())
	other.mu.Lock()
	other.answer = refuse
	other.mu.Unlock()
	_, err := nodeCall(t, n, "session.items", map[string]any{"session_id": "ses_4b1d", "limit": 20})
	var re *rpc.Error
	if !errors.As(err, &re) || re.Code != "not_found" || re.Message != "transcript for session ses_4b1d not found" {
		t.Fatalf("an id no runtime gives: %v", err)
	}
}

// writeFork writes Claude transcript id in the test home as a fork of session from ("" for none): every record names
// it in forkedFrom, as Claude Code's forkSession writes a copied record (its last key), padded past the size under
// which a transcript is not listed offline.
func writeFork(t *testing.T, n *Node, id, from string) {
	t.Helper()
	fork := ""
	if from != "" {
		fork = `,"forkedFrom":{"sessionId":"` + from + `","messageUuid":"u1"}`
	}
	writeTranscript(t, n.home, "/src/app", id, time.Now().Add(-time.Minute),
		`{"type":"user","uuid":"u1","timestamp":"2026-11-05T07:31:12Z","cwd":"/src/app","entrypoint":"sdk-cli","message":{"role":"user","content":"summarize the diff"}`+fork+`}`,
		`{"type":"assistant","uuid":"a1","timestamp":"2026-11-05T07:31:20Z","entrypoint":"sdk-cli","message":{"role":"assistant","content":[{"type":"text","text":"done"}]}`+fork+`}`,
		strings.Repeat(" ", 4096))
}

// A runtime's session listed under an id of its own names the other ids its conversation went by here (aliases): its
// transcript, and the sessions Paseo forked that from, back to its own id, and not past a session another row lists.
// None of them is listed beside it, not even one the runtime names as unlisted. Before #6709 the conversation was
// listed under those ids, and an archive record kept under one repeated it in the Archived list (#8131).
func TestRuntimeRowNamesTheIdsItsConversationWentBy(t *testing.T) {
	const (
		root   = "5d510000-0000-4000-8000-0000000000d0" // the id the bridge kept: the line's first session
		middle = "5d510000-0000-4000-8000-0000000000d1" // forked from root, then forked again
		now    = "5d510000-0000-4000-8000-0000000000d2" // the transcript the agent writes now
		other  = "5d510000-0000-4000-8000-0000000000d3" // another agent's own session, which a line passes
		mine   = "5d510000-0000-4000-8000-0000000000d4" // forked from the other agent's session
		plain  = "5d510000-0000-4000-8000-0000000000d5" // listed under its Claude session
	)
	t.Setenv("PERSONA_HOME", "")
	n, _ := runtimeNode(t)
	t.Cleanup(freshOffline)
	writeFork(t, n, root, "")
	writeFork(t, n, middle, root)
	writeFork(t, n, now, middle)
	writeFork(t, n, other, "")
	writeFork(t, n, mine, other)
	writeFork(t, n, plain, "")
	host := dialRuntime(t, n, paseoHello())
	row := func(id, transcript string) map[string]any {
		return map[string]any{"session_id": id, "transcript": transcript, "agent": "claude", "cwd": "/src/app", "status": "idle"}
	}
	_ = host.peer.Notify("runtime.sessions", map[string]any{"sessions": []map[string]any{
		row(root, now), row("paseo-d4", mine), row(other, other), row(plain, plain)}, "unlisted_transcripts": []string{middle}})
	waitFor(t, "listed", func() bool { return listed(t, n)["paseo-d4"].SourceKind == proto.SourceRuntime })
	freshOffline()
	got := listed(t, n)
	for id, want := range map[string][]string{root: {now, middle}, "paseo-d4": {mine}, other: nil, plain: nil} {
		if s := got[id]; !slices.Equal(s.Aliases(), want) {
			t.Fatalf("%s names %v, want %v", id, s.Aliases(), want)
		}
	}
	for _, id := range []string{now, middle, mine} {
		if _, ok := got[id]; ok {
			t.Fatalf("%s is listed beside the row of its conversation", id)
		}
	}
}
