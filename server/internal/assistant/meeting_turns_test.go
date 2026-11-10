package assistant

import (
	"context"
	"encoding/json"
	"maps"
	"strings"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/assistantwire"
	"github.com/nwverse/persona/server/internal/kb"
	"github.com/nwverse/persona/server/internal/proto"
)

// A meeting turn's mark: sent, started, ended, then gone when the next turn starts; the user speaking during it makes
// the rest of the turn theirs; an end before its start is an earlier turn's; it survives a restart, and a batch whose
// turn never started runs out.
func TestMeetingTurnLifecycle(t *testing.T) {
	st := &memStore{}
	f := newFakeHub()
	s := newServiceOn(t, f, st)
	now := time.Date(2026, 11, 10, 7, 0, 0, 0, time.UTC)
	s.attend.turns.now = func() time.Time { return now }
	running := func(s *Service) bool {
		t.Helper()
		on, err := s.meetingTurnRunning(context.Background(), "server", "c1")
		if err != nil {
			t.Fatal(err)
		}
		return on
	}
	if _, err := s.meetingTurnSent("server", "c1", "rec", false); err != nil {
		t.Fatal(err)
	}
	if !running(s) || s.meetingTurnNow("server", "c1") {
		t.Fatal("a batch sent: its turn may run (tools), but the hooks of the turn before are not its")
	}
	if s.meetingTurnEnded("server", "c1") {
		t.Fatal("an end before the batch's turn started is the turn before's")
	}
	if !s.meetingTurnStarted("server", "c1", false) || !s.meetingTurnNow("server", "c1") || !running(s) {
		t.Fatal("the batch's turn started")
	}
	if !s.meetingTurnStarted("server", "c1", false) {
		t.Fatal("the same turn seen again (a reconnect) is still the meeting's")
	}
	// A restart: the mark is read back.
	again := newServiceOn(t, f, st)
	again.attend.turns.now = s.attend.turns.now
	if !again.meetingTurnNow("server", "c1") || !running(again) {
		t.Fatal("the mark did not survive a restart")
	}
	if !s.meetingTurnEnded("server", "c1") || running(s) || !s.meetingTurnNow("server", "c1") {
		t.Fatal("ended: no tools are its any more, the hooks of its end still find it")
	}
	if !s.meetingTurnEnded("server", "c1") {
		t.Fatal("the same end, told twice, says the same")
	}
	s.meetingTurnUserSpoke("server", "c1") // the user's message after it: the hooks of its end still find it
	if !s.meetingTurnNow("server", "c1") {
		t.Fatal("the user's next message dropped an ended mark before its turn started")
	}
	if s.meetingTurnStarted("server", "c1", true) || s.meetingTurnNow("server", "c1") {
		t.Fatal("the turn after a meeting turn is not one")
	}

	// The user speaks while the batch's turn runs: from there on it is theirs.
	_, _ = s.meetingTurnSent("server", "c1", "rec", false)
	s.meetingTurnStarted("server", "c1", true)
	s.meetingTurnUserSpoke("server", "c1")
	if running(s) || s.meetingTurnNow("server", "c1") {
		t.Fatal("the user's own message did not end the meeting turn")
	}

	// A send that was refused takes its mark back; one whose turn never starts runs out.
	at, _ := s.meetingTurnSent("server", "c1", "rec", false)
	s.meetingTurnUnsent("server", "c1", at)
	if running(s) {
		t.Fatal("a refused batch left its mark")
	}
	_, _ = s.meetingTurnSent("server", "c1", "rec", false)
	now = now.Add(meetingTurnStartWithin + time.Second)
	if running(s) || s.meetingTurnStarted("server", "c1", true) {
		t.Fatal("a batch whose turn never started still marks the next one")
	}
}

// During a meeting turn of the calling session only the reading tools work; every other hub tool says it needs the
// user's own message. Another session, or the same once the turn ended, is not affected.
func TestMeetingTurnToolsOnlyRead(t *testing.T) {
	f := newFakeHub()
	f.hello.Locale = "en"
	s := newTestService(t, f)
	ctx := context.Background()
	_, _ = s.meetingTurnSent("server", "c1", "rec", true)
	for _, name := range meetingTurnTools {
		if _, refused := s.meetingRefuses(ctx, "server", "c1", name, false); refused {
			t.Errorf("%s refused during a meeting turn", name)
		}
	}
	for _, name := range []string{"todo_add", "agent_send", "conversation_send", "notify_user", "phone_notify", "mail_send", "kb_update", "task_plan"} {
		res, refused := s.meetingRefuses(ctx, "server", "c1", name, false)
		if !refused || !res.IsError || !strings.Contains(res.Content[0].Text, name) || !strings.Contains(res.Content[0].Text, "user's own message") {
			t.Errorf("%s: %v %+v", name, refused, res)
		}
	}
	if res := s.mcpCall(ctx, "server", "c1", "notify_user", json.RawMessage(`{}`), true); !res.IsError || !strings.Contains(res.Content[0].Text, "user's own message") {
		t.Errorf("mcp.call during a meeting turn: %+v", res)
	}
	if res := s.Tool(ctx, ToolCall{Name: "todo_add", MachineID: "server", SessionID: "c1", Arguments: json.RawMessage(`{}`)}); !res.IsError ||
		!strings.Contains(res.Content[0].Text, "user's own message") {
		t.Errorf("assistant.tool during a meeting turn: %+v", res)
	}
	if _, refused := s.meetingRefuses(ctx, "server", "c2", "todo_add", false); refused {
		t.Error("another session's tools refused")
	}
	s.meetingTurnStarted("server", "c1", false)
	s.meetingTurnEnded("server", "c1")
	if _, refused := s.meetingRefuses(ctx, "server", "c1", "todo_add", false); refused {
		t.Error("tools refused after the meeting turn ended")
	}
}

// An Agent's MCP server names no session of its own (the node names it only for the tools that need their caller):
// while a meeting turn of an Agent runs on the machine, a call that names none and is not an assistant conversation's
// gets only the reading tools, mail_save_attachment's bytes included. Nothing changes with no Agent's meeting turn
// running: none, a Persona chat's (its calls name it), another machine's, or one that ended.
func TestMeetingTurnSessionlessAgentCalls(t *testing.T) {
	f := newFakeHub()
	f.hello.Locale = "en"
	s := newTestService(t, f)
	ctx := context.Background()
	needsUser := func(text string) bool { return strings.Contains(text, "user's own message") }
	call := func(name string) proto.ToolResult {
		t.Helper()
		return s.mcpCall(ctx, "server", "", name, json.RawMessage(`{}`), false)
	}
	// toPhone: the call went on to the phone (the fake hub has none, and says so).
	toPhone := func(res proto.ToolResult) bool {
		return len(res.Content) > 0 && strings.Contains(res.Content[0].Text, "phone.call")
	}
	attachment := func() error {
		_, err := s.mailAttachment(ctx, "server", proto.MailAttachmentRequest{})
		return err
	}
	if res := call("phone_notify"); !toPhone(res) {
		t.Fatalf("no meeting turn: %+v", res)
	}
	_, _ = s.meetingTurnSent("server", "c1", "rec", true)
	s.meetingTurnStarted("server", "c1", true)
	if res := call("phone_notify"); !toPhone(res) {
		t.Fatalf("a Persona chat's meeting turn refused a call that is not its: %+v", res)
	}

	_, _ = s.meetingTurnSent("server", "a1", "rec", false)
	for _, name := range []string{"phone_notify", "mail_send", "todo_add", "dispatch_start", "report_to_supervisor"} {
		if res := call(name); !res.IsError || !needsUser(res.Content[0].Text) {
			t.Errorf("%s with no session during an Agent's meeting turn (not yet seen starting): %+v", name, res)
		}
	}
	s.meetingTurnStarted("server", "a1", true)
	if res := call("phone_notify"); !res.IsError || !needsUser(res.Content[0].Text) {
		t.Errorf("phone_notify with no session during an Agent's meeting turn: %+v", res)
	}
	if err := attachment(); err == nil || !needsUser(err.Error()) {
		t.Errorf("mail_save_attachment with no session during an Agent's meeting turn: %v", err)
	}
	for _, name := range meetingTurnTools {
		if _, refused := s.meetingRefuses(ctx, "server", "", name, false); refused {
			t.Errorf("%s refused", name)
		}
	}
	if res := call("kb_search"); len(res.Content) > 0 && needsUser(res.Content[0].Text) {
		t.Errorf("a reading tool: %+v", res)
	}
	if _, refused := s.meetingRefuses(ctx, "mac2", "", "phone_notify", false); refused {
		t.Error("another machine's call refused")
	}
	if _, refused := s.meetingRefuses(ctx, "server", "", "phone_notify", true); refused {
		t.Error("an assistant conversation's call refused for an Agent's meeting turn")
	}
	if _, refused := s.meetingRefuses(ctx, "server", "a2", "phone_notify", false); refused {
		t.Error("another session that names itself refused")
	}

	s.meetingTurnEnded("server", "a1")
	if res := call("phone_notify"); !toPhone(res) {
		t.Fatalf("after the meeting turn ended: %+v", res)
	}
	if err := attachment(); err != nil && needsUser(err.Error()) {
		t.Fatalf("mail_save_attachment after the meeting turn ended: %v", err)
	}
}

// The user's own message to an attending Agent ends its meeting turn: the hub says so only for assistant
// conversations, so the Agent's hooks do (a UserPromptSubmit whose prompt the node says is the user's). The batch's
// own prompt, a relay's, a node too old to say and a replayed event from before the batch leave it; so does any prompt
// to a Persona chat, whose user's messages the hub tells (UserSpoke).
func TestMeetingTurnAgentUserPrompt(t *testing.T) {
	ft := newFleetTestNoStore(t)
	fastDone(ft)
	s, ctx := ft.s, context.Background()
	work := proto.Session{SessionID: "s1", Title: "Fix login", Cwd: "/code/app", Agent: "claude", Status: proto.StatusWorking, PaneID: "p1",
		UpdatedAt: time.Now()}
	done := work
	done.Status = proto.StatusDone
	ft.sessions("mac", done)
	sent, err := s.meetingTurnSent("mac", "s1", "rec", false)
	if err != nil {
		t.Fatal(err)
	}
	ft.sessions("mac", work)
	running := func(session string) bool {
		t.Helper()
		on, err := s.meetingTurnRunning(ctx, "mac", session)
		if err != nil {
			t.Fatal(err)
		}
		return on
	}
	prompt := func(session string, fields map[string]any) {
		p := map[string]any{"session_id": session, "event": "UserPromptSubmit"}
		maps.Copy(p, fields)
		ft.event("mac", "hook.event", p)
	}
	prompt("s1", map[string]any{"prompt_source": "meeting"})
	prompt("s1", map[string]any{"prompt_source": "conversation"})
	prompt("s1", map[string]any{"prompt_source": "notification"})
	prompt("s1", nil)
	prompt("s1", map[string]any{"prompt_source": "user", "spooled_at": sent.Add(-time.Second), "spool_id": "e1"})
	ft.event("mac", "hook.event", map[string]any{"session_id": "s1", "event": "Stop", "prompt_source": "user"})
	if !running("s1") || !s.meetingTurnNow("mac", "s1") {
		t.Fatal("a prompt that is not the user's own ended the meeting turn")
	}
	if _, refused := s.meetingRefuses(ctx, "mac", "s1", "todo_add", false); !refused {
		t.Fatal("the meeting turn's tools are not refused")
	}
	prompt("s1", map[string]any{"prompt_source": "user"})
	if running("s1") || s.meetingTurnNow("mac", "s1") {
		t.Fatal("the user's own message did not end the Agent's meeting turn")
	}
	if _, refused := s.meetingRefuses(ctx, "mac", "s1", "todo_add", false); refused {
		t.Fatal("the user's turn is refused tools")
	}

	_, _ = s.meetingTurnSent("mac", "c1", "rec", true)
	s.meetingTurnStarted("mac", "c1", true)
	prompt("c1", map[string]any{"prompt_source": "user"})
	if !running("c1") {
		t.Fatal("a Persona chat's hook ended its meeting turn (the hub tells its user's messages)")
	}
}

// A meeting turn whose end was not seen ends all the same, or its attendee stays busy for the rest of the meeting:
// the manager restarted while it ran and the first list shows the session stopped (whenever that was); it was
// interrupted, or stopped on a card; and none runs past meetingTurnRunMax. A row from before the batch, or one still
// waiting on a card, ends nothing.
func TestMeetingTurnStuckEnds(t *testing.T) {
	ft := newFleetTest(t)
	fastDone(ft)
	s, ctx := ft.s, context.Background()
	ref := kb.SessionRef{MachineID: "mac", SessionID: "s1"}
	work := proto.Session{SessionID: "s1", Title: "Fix login", Cwd: "/code/app", Agent: "claude", Status: proto.StatusWorking, PaneID: "p1",
		UpdatedAt: time.Now()}
	done := work
	done.Status, done.LastText = proto.StatusDone, "Notes."
	running := func(s *Service) bool {
		t.Helper()
		on, err := s.meetingTurnRunning(ctx, "mac", "s1")
		if err != nil {
			t.Fatal(err)
		}
		return on
	}

	// A restart while the meeting turn ran; it ended meanwhile.
	ft.sessions("mac", done)
	if _, err := s.meetingTurnSent("mac", "s1", "rec", false); err != nil {
		t.Fatal(err)
	}
	ft.sessions("mac", work)
	if !running(s) {
		t.Fatal("the batch's turn is not running")
	}
	again := &fleetTest{t: t, d: ft.d, f: ft.f, st: ft.st,
		s: New(Options{Hub: ft.f, Store: func() Store { return ft.st }, Logf: testLogf(t), Devices: ft.d})}
	fastDone(again)
	if !running(again.s) {
		t.Fatal("the mark did not survive the restart")
	}
	old := done
	old.UpdatedAt = time.Now().Add(-time.Hour) // the row says nothing new: the first list after a restart is enough
	again.sessions("mac", old)
	if running(again.s) || !again.s.meetingTurnNow("mac", "s1") {
		t.Fatal("a meeting turn that ended during a restart still runs (or its end was dropped before its hooks ran)")
	}
	if _, v := again.s.attendeeCheck(ctx, ref, time.Now().Add(time.Minute)); v != "" {
		t.Fatalf("the attendee after the restart: %q", v)
	}

	// Interrupted, and stopped on a card: the end the lists show is not working → done.
	s = again.s
	for name, stops := range map[string][]string{
		"interrupted":  {proto.StatusInterrupted},
		"card, denied": {proto.StatusBlocked, proto.StatusDone},
		"offline":      {proto.StatusOffline},
	} {
		again.sessions("mac", done)
		if _, err := s.meetingTurnSent("mac", "s1", "rec", false); err != nil {
			t.Fatal(err)
		}
		row := work
		row.UpdatedAt = time.Now()
		again.sessions("mac", row)
		for _, st := range stops {
			if st == proto.StatusBlocked && !running(s) {
				t.Fatalf("%s: not running before its stop", name)
			}
			row.Status, row.UpdatedAt = st, time.Now()
			again.sessions("mac", row)
			if st == proto.StatusBlocked && !running(s) {
				t.Fatalf("%s: a turn waiting on a card ended", name)
			}
		}
		if running(s) {
			t.Fatalf("%s: the meeting turn still runs", name)
		}
	}

	// Rows that end nothing: one from before the batch (not a restart), one still waiting on a card.
	again.sessions("mac", done)
	sent, _ := s.meetingTurnSent("mac", "s1", "rec", false)
	s.meetingTurnStarted("mac", "s1", true)
	stale := done
	stale.UpdatedAt = sent.Add(-time.Second)
	blocked := work
	blocked.Status, blocked.UpdatedAt = proto.StatusBlocked, time.Now()
	if s.meetingTurnStopped("mac", "s1", stale, false) || s.meetingTurnStopped("mac", "s1", blocked, true) || !running(s) {
		t.Fatal("a row from before the batch, or one waiting on a card, ended the meeting turn")
	}
	fresh := done
	fresh.UpdatedAt = sent.Add(time.Second)
	if !s.meetingTurnStopped("mac", "s1", fresh, false) || running(s) {
		t.Fatal("a stopped row from after the batch did not end it")
	}

	// None runs past meetingTurnRunMax.
	now := time.Now()
	s.attend.turns.now = func() time.Time { return now }
	_, _ = s.meetingTurnSent("mac", "s1", "rec", false)
	s.meetingTurnStarted("mac", "s1", true)
	now = now.Add(meetingTurnRunMax - time.Minute)
	if !running(s) {
		t.Fatal("a long meeting turn ended early")
	}
	now = now.Add(2 * time.Minute)
	if running(s) || !s.meetingTurnNow("mac", "s1") {
		t.Fatal("a meeting turn stuck running past its bound")
	}
}

// A meeting's batch is no one speaking in the conversation: a supervision hold stays (its turns stay unpushed), as it
// does not for another send; and the batch carries its source in the text.
func TestMeetingSendKeepsSupervisionHold(t *testing.T) {
	f := newFakeHub().withMachine()
	f.known["c1"] = true
	s := newTestService(t, f)
	ctx := context.Background()
	s.holdPushes(ctx, "c1")
	src := &proto.MessageSource{Kind: proto.SourceMeeting, Title: "Design review"}
	if err := s.call(ctx, "server", DeviceAssistantSend, map[string]any{"session_id": "c1", "text": "lines", "message_source": src, "resume_only": true}, nil); err != nil {
		t.Fatal(err)
	}
	if !s.supervising("c1") {
		t.Fatal("a meeting's batch released the supervision hold")
	}
	f.mu.Lock()
	sent := f.sends[len(f.sends)-1]
	f.mu.Unlock()
	if text, _ := sent["text"].(string); !strings.Contains(text, `kind="message-source"`) || !strings.Contains(text, `"kind":"meeting"`) || sent["client_msg_id"] != nil {
		t.Fatalf("sent %+v", sent)
	}
	if err := s.call(ctx, "server", DeviceAssistantSend, map[string]any{"session_id": "c1", "text": "hi", "resume_only": true}, nil); err != nil {
		t.Fatal(err)
	}
	if s.supervising("c1") {
		t.Fatal("another send kept the hold")
	}
}

// A meeting turn in the fleet: no finished push and nothing of it left in the turn tracker, so the next turn of the
// user's is timed and pushed as ever; no triage, no channel mirror, never the task's.
func TestMeetingTurnIsSideExchange(t *testing.T) {
	ft := newFleetTestNoStore(t)
	fastDone(ft)
	s := ft.s
	work := proto.Session{SessionID: "s1", Title: "Fix login", Cwd: "/code/app", Agent: "claude", Status: proto.StatusWorking, PaneID: "p1",
		UpdatedAt: time.Now()}
	done := work
	done.Status, done.LastText = proto.StatusDone, "Notes: Ada keeps the cap."
	ft.sessions("mac", done)
	if _, err := s.meetingTurnSent("mac", "s1", "rec", false); err != nil {
		t.Fatal(err)
	}
	ft.sessions("mac", work)
	if !s.meetingTurnNow("mac", "s1") || !s.quietTaskTurn(context.Background(), "mac", "s1") {
		t.Fatal("the batch's turn is not a meeting turn")
	}
	ft.sessions("mac", done)
	if got := ft.pushesOf(assistantwire.NotifyDone, 1, 300*time.Millisecond); len(got) != 0 {
		t.Fatalf("a meeting turn was pushed: %+v", got)
	}
	if !s.turnsBook().startOf("mac/s1").IsZero() {
		t.Fatal("the meeting turn is in the turn tracker")
	}
	s.triageTurn(context.Background(), s.newWorkView(context.Background()), "mac", done)
	s.work.mu.Lock()
	_, tried := s.work.tried[[2]string{"mac", "s1"}]
	s.work.mu.Unlock()
	if tried {
		t.Fatal("the meeting turn was triaged")
	}
	// The user's own turn after it is pushed as ever.
	user := work
	user.UpdatedAt = time.Now()
	ft.sessions("mac", user)
	if s.meetingTurnNow("mac", "s1") {
		t.Fatal("the turn after the meeting turn is one")
	}
	answer := user
	answer.Status, answer.LastText = proto.StatusDone, "Fixed."
	ft.sessions("mac", answer)
	if got := ft.pushesOf(assistantwire.NotifyDone, 1, 2*time.Second); len(got) != 1 || got[0].Body != "Fixed." {
		t.Fatalf("the user's turn: %+v", got)
	}
}

// A turn started by a meeting's batch is a side exchange to the task's completion: it never hands the task over.
func TestTaskTurnSourceMeeting(t *testing.T) {
	text := "<persona-context kind=\"message-source\" title=\"\">\n{\"kind\":\"meeting\",\"title\":\"Design review\"}\n</persona-context>\nNew lines"
	_, source := taskTurnSource(Todo{}, []proto.Item{{Role: "user", Kind: proto.KindText, Text: text}})
	if source != "meeting" {
		t.Fatalf("source %q", source)
	}
}
