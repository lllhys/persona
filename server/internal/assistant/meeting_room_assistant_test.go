package assistant

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/kb"
	"github.com/nwverse/persona/server/internal/proto"
)

// Issue #6048: A meeting's room conversation runs in <state dir>/guest-meeting/<uuid>.
// The manager must count guest-meeting/<id> rows as the assistant's guest conversations:
// 1. isAssistantSession / m.isAssistant returns true for guest-meeting/<id> rows and OriginMeeting, false for normal agents.
// 2. Agent auto-naming never runs on it (no auto-naming).
// 3. Decorated inventory sets Assistant = true so the room never appears in apps' Agents lists or voice list_agents.
// 4. The attending rule in meeting_attendee applies and disallows it from attending another meeting.
func TestMeetingRoomCountedAsAssistant(t *testing.T) {
	roomCwd := filepath.Join("/home/me", ".persona", "guest-meeting", "550e8400-e29b-41d4-a716-446655440000")
	normalCwd := filepath.Join("/home/me", "code", "app")

	roomSess := proto.Session{
		SessionID: "room-sess-1",
		Cwd:       roomCwd,
		Status:    proto.StatusWorking,
		State:     proto.StateWorking,
		Origin:    OriginMeeting,
	}
	normalAgent := proto.Session{
		SessionID: "agent-sess-1",
		Cwd:       normalCwd,
		Status:    proto.StatusWorking,
		State:     proto.StateWorking,
	}

	m := fleetMachine{ID: "server", Home: "/home/me", manager: ""}

	// Path 1: isAssistantSession recognition.
	if !m.isAssistant(roomSess) {
		t.Errorf("m.isAssistant(roomSess) = false for meeting room cwd %s, want true", roomCwd)
	}
	if m.isAssistant(normalAgent) {
		t.Errorf("m.isAssistant(normalAgent) = true for %s, want false", normalCwd)
	}

	// Path 2: Auto-naming must never run on meeting room sessions.
	f := healthHub()
	f.devices = []Device{{ID: "server", Name: "服务器", Kind: "local", Home: "/home/me", Online: true, Reachable: true}}
	f.sessions["server"] = []proto.Session{roomSess, normalAgent}
	s := withSettings(t, f, New(Options{Hub: f, Store: func() Store { return &memStore{} }, Logf: testLogf(t)}))
	ctx := context.Background()
	s.originSet(ctx, "server", roomSess.SessionID, OriginMeeting, "rec-1", "")

	// Even if Assistant was not set in incoming raw session, tryAgentName checks m.isAssistant and rejects naming.
	rawRoomSess := roomSess
	rawRoomSess.Assistant = false
	if s.tryAgentName(ctx, "server", rawRoomSess, time.Now()) {
		t.Error("tryAgentName returned true for meeting room session, want false")
	}

	// Path 3: decorateInventory sets Assistant = true so the room never appears in apps' Agents lists or voice list_agents.
	decorated := s.decorateInventory("server", []proto.Session{rawRoomSess, normalAgent}, true)
	if len(decorated) != 2 {
		t.Fatalf("decorateInventory returned %d sessions, want 2", len(decorated))
	}
	if !decorated[0].Assistant {
		t.Errorf("decorated[0].Assistant = false for meeting room row, want true")
	}
	if decorated[1].Assistant {
		t.Errorf("decorated[1].Assistant = true for normal agent row, want false")
	}

	// Path 4: Attending rule in meeting_attendee disallows the room from attending another meeting.
	f.sessions["server"] = decorated
	ref := kb.SessionRef{MachineID: "server", SessionID: roomSess.SessionID}
	target, verdict := s.attendeeCheckFor(ctx, ref, time.Now(), "")
	_ = target
	if verdict != attendeeNotAllowed {
		t.Errorf("attendeeCheckFor verdict = %q for meeting room session, want %q", verdict, attendeeNotAllowed)
	}
}
