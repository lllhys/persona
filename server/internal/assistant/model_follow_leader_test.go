package assistant

import (
	"context"
	"testing"

	"github.com/nwverse/persona/server/internal/proto"
)

// A leader conversation and the background supervisor run on Model configuration › Main chat with every message, as
// the main conversation does: a model a failed turn's recovery left one on (Haiku after a run of 429s) is not carried
// over as what it runs. Another side conversation still keeps what it runs, and a pick still wins.
func TestLeaderAndBackgroundFollowTheMainChatModel(t *testing.T) {
	ctx := context.Background()
	s, f := mainChatFixture(t)
	setModel(t, s, "assistant", ModelBinding{Provider: "launcher.claude", Model: "claude-opus-5-5"})
	setSettings(t, s, func(v *Settings) {
		v.Main, v.Leaders, v.BackgroundSupervisor = "main", []string{"lead"}, "bg"
	})
	f.setSessions("server",
		proto.Session{SessionID: "main", Assistant: true},
		proto.Session{SessionID: "lead", Launcher: "claude", Model: "claude-haiku-5-5", Assistant: true},
		proto.Session{SessionID: "bg", Launcher: "claude", Model: "claude-haiku-5-5", Assistant: true},
		proto.Session{SessionID: "side", Launcher: "claude", Model: "claude-haiku-5-5", Assistant: true})
	for _, sid := range []string{"lead", "bg"} {
		p, err := s.SendParams(ctx, SendParams{MachineID: "server", SessionID: sid, Params: map[string]any{"text": "📋"}})
		if err != nil || p["launcher"] != "claude" || p["model"] != "claude-opus-5-5" {
			t.Fatalf("%s kept the model a recovery left it on: %v %v", sid, p, err)
		}
	}
	p, err := s.SendParams(ctx, SendParams{MachineID: "server", SessionID: "side", Params: map[string]any{"text": "hi"}})
	if err != nil || p["model"] != "claude-haiku-5-5" {
		t.Fatalf("another side conversation no longer keeps what it runs: %v %v", p, err)
	}
	p, err = s.SendParams(ctx, SendParams{MachineID: "server", SessionID: "lead", Params: map[string]any{"text": "hi", "model": "claude-sonnet-5-5"}})
	if err != nil || p["model"] != "claude-sonnet-5-5" {
		t.Fatalf("a model picked with the message no longer wins on a leader: %v %v", p, err)
	}
}
