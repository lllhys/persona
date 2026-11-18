package node

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/proto"
)

// Resume in Terminal (#5521, docs/integrations/engines.md "Agents run in a terminal"): a Claude Code session typed into
// a terminal of the user's own (outside Persona's engines: a plain terminal, an SSH login) and not running now. The
// clients offer to resume it in a terminal only where the node says it can (a stopped Agent's capabilities); these
// pin what the node says and does in the three cases: a computer with herdr, one with no terminal engine, and the
// session still open in that other terminal.

// outsideSession writes the transcript of a Claude Code run from a plain terminal (entrypoint cli), with nothing of
// Persona's about it (no state, no pane), last written an hour ago: what is left once that terminal closed.
func outsideSession(t *testing.T, home, id string) string {
	t.Helper()
	cwd := mkdir(t, filepath.Join(home, "notes"))
	at := `"entrypoint":"cli","sessionId":"` + id + `","cwd":"` + cwd + `"`
	writeTranscript(t, home, cwd, id, time.Now().Add(-time.Hour),
		`{"type":"user",`+at+`,"timestamp":"2026-11-12T08:00:00Z","message":{"role":"user","content":"tidy the release notes"},"uuid":"u1"}`,
		`{"type":"assistant",`+at+`,"timestamp":"2026-11-12T08:00:01Z","message":{"role":"assistant","content":[{"type":"text","text":"`+
			strings.Repeat("sorted by module. ", 150)+`"}]},"uuid":"a1","parentUuid":"u1"}`) // past the 2 KB a listed session has
	forgetOfflineScan()
	return cwd
}

// With herdr: the row is a stopped Agent (read and terminal, nothing else), and the user's Resume (session.start with
// only its id, as the web sends it) opens a herdr tab in the session's folder and types claude --resume there.
func TestOutsideSessionResumesInATerminalTab(t *testing.T) {
	n, f, home := herdrNode(t)
	noPaneEnv(t)
	cwd := outsideSession(t, home, startID)
	row := byID(listSessions(t, n))[startID]
	if row.SourceKind != "" || row.Status != proto.StatusOffline || row.AgentSurface != "cli" || row.Capabilities == nil ||
		!row.Capabilities.Read || !row.Capabilities.Terminal || row.Capabilities.Send || row.ReadOnlyReason != stoppedReason {
		t.Fatalf("closed terminal's session: source %q status %q surface %q capabilities %+v reason %q",
			row.SourceKind, row.Status, row.AgentSurface, row.Capabilities, row.ReadOnlyReason)
	}

	f.reset()
	f.reply("workspace.list", map[string]any{"workspaces": []any{}})
	f.reply("workspace.create", map[string]any{"workspace": map[string]any{"workspace_id": "w7"},
		"tab": map[string]any{"tab_id": "w7:t1"}, "root_pane": map[string]any{"pane_id": "w7:p1"}})
	startHerdr(f, "w7:p1", proto.StatusDone)
	f.setScreen("w7:p1", screenFixture(t, "claude-2.1.312-empty-162.txt"))
	res, err := request(t, n, "session.start", map[string]any{"resume_session_id": startID})
	if err != nil {
		t.Fatal(err)
	}
	if s := decodeSessions(t, []any{res["session"]})[0]; s.SessionID != startID || s.PaneID != "w7:p1" {
		t.Errorf("resumed %+v", s)
	}
	created, typed := false, false
	for _, c := range f.calls() {
		switch c.Method {
		case "workspace.create":
			created = created || str(c.Params["cwd"]) == cwd
		case "pane.send_input":
			typed = typed || str(c.Params["pane_id"]) == "w7:p1" && strings.Contains(str(c.Params["text"]), "--resume "+startID)
		}
	}
	if !created || !typed {
		t.Fatalf("no tab in %s running claude --resume: %v", cwd, f.trace())
	}
}

// No terminal engine (the empty set): the row offers no terminal (the clients say why instead), and a resume that
// comes anyway is refused with nothing opened.
func TestOutsideSessionWithoutATerminalEngine(t *testing.T) {
	n, f, home := herdrNode(t)
	noPaneEnv(t)
	outsideSession(t, home, startID)
	if err := n.useEngineChoice(context.Background(), proto.EngineChoice{Hosts: []string{}}); err != nil {
		t.Fatal(err)
	}
	row := byID(listSessions(t, n))[startID]
	if row.SessionID != startID || row.Status != proto.StatusOffline || row.Capabilities != nil && row.Capabilities.Terminal {
		t.Fatalf("no engine: status %q capabilities %+v", row.Status, row.Capabilities)
	}
	f.reset()
	_, err := request(t, n, "session.start", map[string]any{"resume_session_id": startID})
	if code, _ := refusalOf(t, err); code != engineMissing {
		t.Fatalf("resume with no terminal engine: %v", err)
	}
	for _, c := range f.trace() {
		if strings.Contains(c, "workspace.create") || strings.Contains(c, "tab.create") || strings.Contains(c, "pane.send_input") {
			t.Fatalf("something was started: %v", f.trace())
		}
	}
}

// Still open in that other terminal (a live claude with the session, entrypoint cli): the row is that claude's
// (claude_cli, running) with no terminal, and a resume is refused before anything is opened: a second claude would
// write the same transcript.
func TestOutsideSessionStillRunningElsewhere(t *testing.T) {
	n, f, home := herdrNode(t)
	noPaneEnv(t)
	cwd := outsideSession(t, home, startID)
	writeClaudeProc(t, home, os.Getpid(), map[string]any{"sessionId": startID, "cwd": cwd, "kind": "interactive",
		"entrypoint": "cli", "status": "idle"}) // this test's own pid: alive
	row := byID(listSessions(t, n))[startID]
	if row.SourceKind != proto.SourceClaudeTerminal || row.Status == proto.StatusOffline || row.Capabilities == nil ||
		row.Capabilities.Terminal {
		t.Fatalf("running elsewhere: source %q status %q capabilities %+v", row.SourceKind, row.Status, row.Capabilities)
	}
	f.reset()
	_, err := request(t, n, "session.start", map[string]any{"resume_session_id": startID})
	if !isRefusal(err, openElsewhere) {
		t.Fatalf("resume of a session open in another terminal: %v", err)
	}
	for _, c := range f.trace() {
		if strings.Contains(c, "workspace.create") || strings.Contains(c, "tab.create") || strings.Contains(c, "pane.send_input") {
			t.Fatalf("something was started: %v", f.trace())
		}
	}
}
