package assistant

import (
	"context"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/kb"
	"github.com/nwverse/persona/server/internal/proto"
)

// Which sessions never attend, which cannot take a batch now and which are busy, from the fields every client sees
// (docs/assistant/meeting-minutes.md "Feeding the attending session").
func TestAttendeeVerdict(t *testing.T) {
	ok := attendeeFacts{ours: true, online: true, found: true}
	chat := proto.Session{Assistant: true, Status: proto.StatusIdle, State: proto.StateIdle}
	agent := proto.Session{Status: proto.StatusDone, State: proto.StateUnread, PermissionMode: "acceptEdits"}
	with := func(s proto.Session, fn func(*proto.Session)) proto.Session { fn(&s); return s }
	facts := func(fn func(*attendeeFacts)) attendeeFacts { f := ok; fn(&f); return f }
	for _, c := range []struct {
		name string
		sess proto.Session
		f    attendeeFacts
		want string
	}{
		{"an idle Persona chat", chat, ok, ""},
		{"a done Agent", agent, ok, ""},
		{"a stopped Persona chat is resumed", with(chat, func(s *proto.Session) { s.Status, s.State = proto.StatusOffline, proto.StateOffline }), ok, ""},
		{"an interrupted Persona chat is resumed", with(chat, func(s *proto.Session) { s.Status = proto.StatusInterrupted }), ok, ""},

		{"a work computer", agent, facts(func(f *attendeeFacts) { f.work = true }), attendeeNotAllowed},
		{"another manager's computer", agent, facts(func(f *attendeeFacts) { f.ours = false }), attendeeNotAllowed},
		{"the health space", with(chat, func(s *proto.Session) { s.Space = SpaceHealth }), ok, attendeeNotAllowed},
		{"a Lark agent conversation", with(chat, func(s *proto.Session) { s.Origin = OriginLarkAgent }), ok, attendeeNotAllowed},
		{"a channel's conversation", with(chat, func(s *proto.Session) { s.Origin = OriginChannel }), ok, attendeeNotAllowed},
		{"an extension's conversation", with(chat, func(s *proto.Session) { s.Origin = OriginExtension }), ok, attendeeNotAllowed},
		{"the inbox's AI twin", with(chat, func(s *proto.Session) { s.Origin = OriginInbox }), ok, attendeeNotAllowed},
		{"opened by an extension", with(chat, func(s *proto.Session) { s.OpenedBy = "ext-example" }), ok, attendeeNotAllowed},
		{"a guest conversation", chat, facts(func(f *attendeeFacts) { f.guest = true }), attendeeNotAllowed},
		{"a task's own conversation may attend", with(chat, func(s *proto.Session) { s.Origin = OriginTask }), ok, ""},
		{"a chat that never asks", with(chat, func(s *proto.Session) { s.PermissionMode = proto.ModeBypass }), ok, attendeeNotAllowed},
		{"an Agent that never asks", with(agent, func(s *proto.Session) { s.PermissionMode = proto.ModeBypass }), ok, attendeeNotAllowed},
		{"not allowed before busy", with(agent, func(s *proto.Session) { s.PermissionMode, s.Status = proto.ModeBypass, proto.StatusWorking }), ok, attendeeNotAllowed},

		{"its computer offline", agent, facts(func(f *attendeeFacts) { f.online = false }), attendeeUnavailable},
		{"gone", agent, facts(func(f *attendeeFacts) { f.found = false }), attendeeUnavailable},
		{"a closed Agent is not resumed", with(agent, func(s *proto.Session) { s.Status, s.State = proto.StatusOffline, proto.StateOffline }), ok, attendeeUnavailable},
		{"an interrupted Agent", with(agent, func(s *proto.Session) { s.Status = proto.StatusInterrupted }), ok, attendeeUnavailable},
		{"an Agent that takes no messages", with(agent, func(s *proto.Session) { s.SourceKind, s.Capabilities = "codex_app", &proto.SessionCapabilities{} }), ok, attendeeUnavailable},
		{"a subagent", with(agent, func(s *proto.Session) { s.IsSubagent = true }), ok, attendeeUnavailable},
		{"where a chat comes from cannot be told", chat, facts(func(f *attendeeFacts) { f.unknown = true }), attendeeUnavailable},

		{"working", with(agent, func(s *proto.Session) { s.Status, s.State = proto.StatusWorking, proto.StateWorking }), ok, attendeeBusy},
		{"starting", with(chat, func(s *proto.Session) { s.Starting = true }), ok, attendeeBusy},
		{"waiting on a card", with(agent, func(s *proto.Session) { s.PendingInteractions = 1 }), ok, attendeeBusy},
		{"blocked on a dialog", with(agent, func(s *proto.Session) { s.Status, s.State = proto.StatusBlocked, proto.StateNeedsYou }), ok, attendeeBusy},
		{"background work", with(agent, func(s *proto.Session) { s.State, s.BackgroundWorking = proto.StateWorking, true }), ok, attendeeBusy},
		{"waiting for a usage limit", with(agent, func(s *proto.Session) { s.TurnErrorResumes = true }), ok, attendeeBusy},
		{"status unknown", with(agent, func(s *proto.Session) { s.Status = proto.StatusUnknown }), ok, attendeeBusy},
		{"a turn just ended", agent, facts(func(f *attendeeFacts) { f.settling = true }), attendeeBusy},
		{"a batch's turn under way", chat, facts(func(f *attendeeFacts) { f.running = true }), attendeeBusy},
	} {
		if got := attendeeVerdict(c.sess, c.f); got != c.want {
			t.Errorf("%s: %q, want %q", c.name, got, c.want)
		}
	}
}

// In the fleet the session's row and its computer are the manager's own; a turn that just ended keeps a batch back
// until its push is decided, a meeting turn's end does not.
func TestAttendeeCheckFleet(t *testing.T) {
	ft := newFleetTestNoStore(t)
	fastDone(ft)
	s, ctx := ft.s, context.Background()
	ref := kb.SessionRef{MachineID: "mac", SessionID: "s1"}
	work := proto.Session{SessionID: "s1", Cwd: "/code/app", Agent: "claude", Status: proto.StatusWorking, PaneID: "p1", UpdatedAt: time.Now()}
	done := work
	done.Status, done.LastText = proto.StatusDone, "Done."
	ft.sessions("mac", done)
	if _, v := s.attendeeCheck(ctx, ref, time.Now()); v != "" {
		t.Fatalf("a done Agent: %q", v)
	}
	if _, v := s.attendeeCheck(ctx, kb.SessionRef{MachineID: "elsewhere", SessionID: "s1"}, time.Now()); v != attendeeNotAllowed {
		t.Fatalf("a computer this manager does not carry: %q", v)
	}
	if _, v := s.attendeeCheck(ctx, kb.SessionRef{MachineID: "mac", SessionID: "gone"}, time.Now()); v != attendeeUnavailable {
		t.Fatalf("a session that is not listed: %q", v)
	}
	ft.sessions("mac", work)
	if _, v := s.attendeeCheck(ctx, ref, time.Now()); v != attendeeBusy {
		t.Fatalf("working: %q", v)
	}
	ft.sessions("mac", done)
	if _, v := s.attendeeCheck(ctx, ref, time.Now()); v != attendeeBusy {
		t.Fatalf("a turn that just ended: %q", v)
	}
	if _, v := s.attendeeCheck(ctx, ref, time.Now().Add(time.Minute)); v != "" {
		t.Fatalf("its push decided: %q", v)
	}
	ft.d.mu.Lock()
	ft.d.machines["mac"].trust = "work"
	ft.d.mu.Unlock()
	if _, v := s.attendeeCheck(ctx, ref, time.Now().Add(time.Minute)); v != attendeeNotAllowed {
		t.Fatalf("a work computer: %q", v)
	}
}
