package assistant

import (
	"context"
	"testing"

	"github.com/nwverse/persona/server/internal/proto"
)

func TestMainCompactionScope(t *testing.T) {
	ctx := context.Background()
	f := newFakeHub().withMachine()
	s := newTestService(t, f)
	for _, main := range []bool{false, true} {
		sess, err := s.start(ctx, StartParams{Main: main, Text: "hello"})
		if err != nil {
			t.Fatal(err)
		}
		if got := f.lastSend(t)["is_main"]; got != main {
			t.Fatalf("start main=%v: is_main=%v", main, got)
		}
		f.setSessions("server", proto.Session{SessionID: sess.SessionID, Assistant: true})
		params, err := s.SendParams(ctx, SendParams{MachineID: "server", SessionID: sess.SessionID, Params: map[string]any{"text": "next", "is_main": !main}})
		if err != nil {
			t.Fatal(err)
		}
		if params["is_main"] != main {
			t.Fatalf("send main=%v: %v", main, params)
		}
		if main {
			wrong, err := s.SendParams(ctx, SendParams{MachineID: "other", SessionID: sess.SessionID, Params: map[string]any{"is_main": true}})
			if err != nil || wrong["is_main"] != false {
				t.Fatalf("wrong machine: %v %v", wrong, err)
			}
			setSettings(t, s, func(v *Settings) { v.Main = "replacement" })
			former, err := s.SendParams(ctx, SendParams{MachineID: "server", SessionID: sess.SessionID, Params: map[string]any{}})
			if err != nil || former["is_main"] != false {
				t.Fatalf("former main: %v %v", former, err)
			}
		}
	}
}

// The interject_background setting goes with every message to the node, and only while it is on (#3278).
func TestSendParamsInterjectBackground(t *testing.T) {
	ctx := context.Background()
	f := newFakeHub().withMachine()
	s := newTestService(t, f)
	sess, err := s.start(ctx, StartParams{Main: true, Text: "hello"})
	if err != nil {
		t.Fatal(err)
	}
	if _, set := f.lastSend(t)["interject_background"]; set {
		t.Fatal("sent while off")
	}
	f.setSessions("server", proto.Session{SessionID: sess.SessionID, Assistant: true})
	on := true
	setSettings(t, s, func(v *Settings) { v.InterjectBackground = &on })
	params, err := s.SendParams(ctx, SendParams{MachineID: "server", SessionID: sess.SessionID, Params: map[string]any{"text": "next"}})
	if err != nil || params["interject_background"] != true {
		t.Fatalf("app send while on: %v %v", params, err)
	}
	if _, err := s.start(ctx, StartParams{Text: "side"}); err != nil {
		t.Fatal(err)
	}
	if got := f.lastSend(t)["interject_background"]; got != true {
		t.Fatalf("assistant.send while on: %v", got)
	}
	if v := s.view(ctx, false); !v.InterjectBackground {
		t.Fatal("the view does not say it is on")
	}
}
