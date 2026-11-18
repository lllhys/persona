package node

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/proto"
)

// writeClaudeProc writes sessions/<pid>.json as a running claude does, with the given fields.
func writeClaudeProc(t *testing.T, home string, pid int, fields map[string]any) {
	t.Helper()
	dir := filepath.Join(home, ".claude", "sessions")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	fields["pid"] = pid
	b, _ := json.Marshal(fields)
	if err := os.WriteFile(filepath.Join(dir, strconv.Itoa(pid)+".json"), b, 0o600); err != nil {
		t.Fatal(err)
	}
}

// bgTranscript writes a Claude transcript with a title and one exchange, padded past what the offline list skips.
func bgTranscript(t *testing.T, home, cwd, id, title string) string {
	t.Helper()
	return writeTranscript(t, home, cwd, id, time.Now(),
		`{"type":"ai-title","aiTitle":"`+title+`","sessionId":"`+id+`"}`,
		`{"type":"user","uuid":"u1","timestamp":"2026-11-04T09:00:00Z","cwd":"`+cwd+`","sessionId":"`+id+`","message":{"role":"user","content":"deploy it"}}`,
		`{"type":"assistant","uuid":"a1","timestamp":"2026-11-04T09:00:05Z","sessionId":"`+id+`","message":{"role":"assistant","content":[{"type":"text","text":"on it"}]}}`,
		strings.Repeat(" ", 4096))
}

// A /fork in Claude Code 2.1.30x: the fork runs in Claude's daemon (sessions/<pid>.json kind "bg"), no pane; the
// interactive claude in the pane parked its own session and shows the agents view, while herdr still names the pane
// after that session and calls it working off the fork's terminal title. The fork lists live, read-only, with its
// subagents and its status from the registry; the parked session keeps its tab, idle (not the fork's state), with
// its terminal but no messages, until its claude comes back to it. A background session whose claude exited is not live.
func TestClaudeBackgroundSessionListedLive(t *testing.T) {
	n, f, home := herdrNode(t)
	cwd := home
	const dead = "0d000000-0000-4000-8000-00000000000d"
	bgTranscript(t, home, cwd, sidA, "deploy")
	fork := bgTranscript(t, home, cwd, sidB, "deploy ⑂ try it")
	bgTranscript(t, home, cwd, dead, "gone")
	sub := filepath.Join(strings.TrimSuffix(fork, ".jsonl"), "subagents")
	if err := os.MkdirAll(sub, 0o700); err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(filepath.Join(sub, "agent-a1.jsonl"), []byte(`{"type":"assistant","timestamp":"2026-11-04T09:01:00Z","isSidechain":true,"message":{"role":"assistant","content":[{"type":"text","text":"looking"}]}}`+"\n"), 0o600)
	_ = os.WriteFile(filepath.Join(sub, "agent-a1.meta.json"), []byte(`{"agentType":"Explore","description":"look around","toolUseId":"tu1"}`), 0o600)

	parent := map[string]any{"sessionId": sidA, "cwd": cwd, "kind": "interactive", "entrypoint": "cli", "status": "idle", "parkedJobId": "0d0000aa"}
	writeClaudeProc(t, home, os.Getppid(), parent)
	bg := func(status string) {
		writeClaudeProc(t, home, os.Getpid(), map[string]any{"sessionId": sidB, "cwd": cwd, "kind": "bg", "entrypoint": "cli",
			"status": status, "name": "deploy ⑂ try it"})
	}
	bg("busy")
	writeClaudeProc(t, home, 999999, map[string]any{"sessionId": dead, "cwd": cwd, "kind": "bg", "status": "busy"}) // no such process
	f.reply("session.snapshot", snapshot(nil, nil, []any{
		claudeAgent(sidA, proto.StatusWorking, "w1:p1", "w1:t1", "w1", cwd, "deploy ⑂ try it")}, nil))

	m := byID(listSessions(t, n))
	b := m[sidB]
	if b.Status != proto.StatusWorking || b.SourceKind != sourceClaudeBg || b.PaneID != "" || b.Title != "deploy ⑂ try it" ||
		b.Agent != "claude" || b.Cwd != cwd || b.ReadOnlyReason == "" || b.LastText != "on it" {
		t.Errorf("fork = %+v", b)
	}
	if c := b.Capabilities; c == nil || !c.Read || c.Send || c.Interrupt || c.Approve || c.Terminal || c.Attach {
		t.Errorf("fork capabilities = %+v", c)
	}
	if len(b.Subagents) != 1 || b.Subagents[0].SessionID != sidB+"@a1" || !b.Subagents[0].Running {
		t.Errorf("fork subagents = %+v", b.Subagents)
	}
	if a := m[sidA]; a.Status != proto.StatusIdle || a.PaneID != "w1:p1" || a.TabID != "w1:t1" || a.ReadOnlyReason == "" {
		t.Errorf("parked parent = %+v", a)
	}
	if s, ok := m[dead]; ok && s.Status != proto.StatusOffline {
		t.Errorf("exited background session = %+v", s)
	}

	for status, want := range map[string]string{"idle": proto.StatusIdle, "shell": proto.StatusIdle, "waiting": proto.StatusBlocked, "later": proto.StatusUnknown} {
		bg(status)
		if got := byID(listSessions(t, n))[sidB].Status; got != want {
			t.Errorf("registry status %s: listed %s, want %s", status, got, want)
		}
	}

	// The claude comes back to its own session: the pane is that session's again.
	delete(parent, "parkedJobId")
	writeClaudeProc(t, home, os.Getppid(), parent)
	if a := byID(listSessions(t, n))[sidA]; a.Status != proto.StatusWorking || a.PaneID != "w1:p1" || a.ReadOnlyReason != "" {
		t.Errorf("parent back in its pane = %+v", a)
	}
}

// Nothing is typed for a background session: a send is refused before anything starts (the claude running it has it
// open), and herdr is never asked to make a tab.
func TestClaudeBackgroundSessionSendRefused(t *testing.T) {
	n, f, home := herdrNode(t)
	bgTranscript(t, home, home, sidB, "fork")
	writeClaudeProc(t, home, os.Getpid(), map[string]any{"sessionId": sidB, "cwd": home, "kind": "bg", "entrypoint": "cli", "status": "idle"})
	f.reset()
	_, err := request(t, n, "session.send", map[string]any{"session_id": sidB, "text": "still there?", "client_msg_id": "m-1"})
	if !isRefusal(err, openElsewhere) {
		t.Fatalf("send to a background session: %v", err)
	}
	for _, c := range f.trace() {
		if strings.Contains(c, "workspace") || strings.Contains(c, "tab.create") || strings.Contains(c, "pane.") {
			t.Fatalf("herdr was asked to start something: %v", f.trace())
		}
	}
}

// A session sent to the background with /background: the agents view opens the job it went to, so the pane shows
// that job's conversation under its name. The pane is the background session's — listed in its herdr tab, not apart
// and read-only — while the parked session is not in it. On the agents list (another title, or the job ended) the
// pane is the parked session's tab again, with its terminal to pick from the list: a message is refused, nothing typed.
func TestParkedPaneShowingItsJob(t *testing.T) {
	n, f, home := herdrNode(t)
	cwd := home
	bgTranscript(t, home, cwd, sidA, "lookup")
	bgTranscript(t, home, cwd, sidB, "lark lookup")
	writeClaudeProc(t, home, os.Getppid(), map[string]any{"sessionId": sidA, "cwd": cwd, "kind": "interactive",
		"entrypoint": "cli", "status": "idle", "parkedJobId": sidB[:8]})
	writeClaudeProc(t, home, os.Getpid(), map[string]any{"sessionId": sidB, "cwd": cwd, "kind": "bg", "entrypoint": "cli",
		"status": "idle", "jobId": sidB[:8], "name": "lark lookup"})
	shown := func(title string) map[string]proto.Session {
		f.reply("session.snapshot", snapshot(nil, nil, []any{
			claudeAgent(sidA, proto.StatusIdle, "w1:p4", "w1:t4", "w1", cwd, title)}, nil))
		return byID(listSessions(t, n))
	}

	m := shown("lark lookup")
	if b := m[sidB]; b.PaneID != "w1:p4" || b.TabID != "w1:t4" || b.SourceKind == sourceClaudeBg || b.ReadOnlyReason != "" {
		t.Errorf("job shown in the pane = %+v", b)
	}
	if a := m[sidA]; a.PaneID != "" || a.Status != proto.StatusOffline {
		t.Errorf("parked session = %+v", a)
	}

	m = shown("agents")
	if b := m[sidB]; b.PaneID != "" || b.SourceKind != sourceClaudeBg {
		t.Errorf("job, pane on the agents list = %+v", b)
	}
	if a := m[sidA]; a.PaneID != "w1:p4" || a.TabID != "w1:t4" || a.Status != proto.StatusIdle || a.ReadOnlyReason == "" {
		t.Errorf("parked session, pane on the agents list = %+v", a)
	} else if c := a.Capabilities; c == nil || !c.Read || !c.Terminal || c.Send {
		t.Errorf("parked session capabilities = %+v", c)
	}

	// The job ended: no background process, the pane on the agents list (2026-11-05: tabs 2 and 4 gone).
	_ = os.Remove(filepath.Join(home, ".claude", "sessions", strconv.Itoa(os.Getpid())+".json"))
	m = shown("2 awaiting input · claude agents")
	if a := m[sidA]; a.PaneID != "w1:p4" || a.TabID != "w1:t4" || a.ReadOnlyReason == "" {
		t.Errorf("parked session, job ended = %+v", a)
	}
	f.reset()
	_, err := request(t, n, "session.send", map[string]any{"session_id": sidA, "text": "hi", "client_msg_id": "m-1"})
	if !isRefusal(err, openElsewhere) {
		t.Fatalf("send to a pane on the agents list: %v", err)
	}
	for _, c := range f.trace() {
		if strings.Contains(c, "send") || strings.Contains(c, "tab.create") {
			t.Fatalf("something was typed or started: %v", f.trace())
		}
	}
	// Picking from the list, as on the computer: the keys reach the pane.
	f.reset()
	if _, err := request(t, n, "session.keys", map[string]any{"session_id": sidA, "keys": []string{"down", "enter"}}); err != nil {
		t.Fatalf("keys to a pane on the agents list: %v", err)
	}
	if tr := strings.Join(f.trace(), "\n"); !strings.Contains(tr, "w1:p4") {
		t.Fatalf("keys did not reach the pane: %v", f.trace())
	}
}

// A claude in another terminal is titled like any session: by its transcript's title, else by its first message. The
// name Claude Code derives for it (nameSource "derived": the directory's base name and two hex digits) stands in only
// while there is neither; a name something chose for it is its title, as before.
func TestOutsideClaudeTitleNotItsDerivedName(t *testing.T) {
	n, _, home := herdrNode(t)
	noPaneEnv(t)
	writeTranscript(t, home, home, sidA, time.Now(),
		`{"type":"user","uuid":"u1","timestamp":"2026-11-11T09:00:00Z","cwd":"`+home+`","sessionId":"`+sidA+`","message":{"role":"user","content":"what is this repository for"}}`,
		strings.Repeat(" ", 4096))
	proc := map[string]any{"sessionId": sidA, "cwd": home, "kind": "interactive", "entrypoint": "cli", "status": "idle",
		"name": "home-a8", "nameSource": "derived"}
	writeClaudeProc(t, home, os.Getpid(), proc)
	if s := byID(listSessions(t, n))[sidA]; s.SourceKind == "" || s.Title != "what is this repository for" {
		t.Errorf("derived name over the first message: %q (%+v)", s.Title, s)
	}

	// A name something chose (claude -n, /rename, a peer, a hook), or one from a Claude Code that does not say where
	// its name came from.
	for _, source := range []string{"user", "peer", "auto", ""} {
		proc["name"], proc["nameSource"] = "release-check", source
		writeClaudeProc(t, home, os.Getpid(), proc)
		if s := byID(listSessions(t, n))[sidA]; s.Title != "release-check" {
			t.Errorf("nameSource %q: title %q", source, s.Title)
		}
	}

	// Nothing said yet: the derived name rather than no title.
	writeTranscript(t, home, home, sidB, time.Now(),
		`{"type":"permission-mode","permissionMode":"default","sessionId":"`+sidB+`"}`, strings.Repeat(" ", 4096))
	writeClaudeProc(t, home, os.Getppid(), map[string]any{"sessionId": sidB, "cwd": home, "kind": "interactive",
		"entrypoint": "cli", "status": "idle", "name": "home-b1", "nameSource": "derived"})
	if s := byID(listSessions(t, n))[sidB]; s.SourceKind == "" || s.Title != "home-b1" {
		t.Errorf("a claude that said nothing yet: %q (%+v)", s.Title, s)
	}
}
