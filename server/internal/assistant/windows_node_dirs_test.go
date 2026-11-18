package assistant

import (
	"testing"

	"github.com/nwverse/persona/server/internal/proto"
)

// A Windows node's main chat, health space and meeting room conversations are the assistant's on a hub of any
// system (#6419).
func TestWindowsNodeConversationsOnAnyHub(t *testing.T) {
	m := fleetMachine{ID: "pc", Home: `C:\Users\me`}
	for _, c := range []struct {
		cwd  string
		want bool
	}{
		{`C:\Users\me\.persona\assistant`, true},
		{`C:\Users\me\.persona\health`, true},
		{`C:\Users\me\.persona\guest-meeting\550e8400-e29b-41d4-a716-446655440000`, true},
		{`C:\Users\me\code\app`, false},
	} {
		if got := m.isAssistant(proto.Session{SessionID: "s", Cwd: c.cwd}); got != c.want {
			t.Errorf("isAssistant(%q) = %v, want %v", c.cwd, got, c.want)
		}
	}
}
