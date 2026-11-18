package assistant

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/nwverse/persona/server/internal/proto"
)

func TestFeishuModelCommands(t *testing.T) {
	ctx := context.Background()
	s, f := pickFixture(t)
	f.agentModels = map[string][]proto.AgentModel{"gw": {{ID: "m1"}, {ID: "m2"}}}
	var note string
	f.answerApp(AppFeishu, FeishuText, func(raw json.RawMessage) (any, error) {
		var p struct {
			Text string `json:"text"`
		}
		_ = json.Unmarshal(raw, &p)
		note = p.Text
		return map[string]any{}, nil
	})
	s.setBotCurrent(botTarget{Kind: targetMain})
	command := func(text string) {
		t.Helper()
		note = ""
		if handled, rest := s.botCommand(ctx, FeishuFeatures{Main: true}, "om", text, "", ""); !handled || rest != "" {
			t.Fatalf("%s: handled=%v rest=%q", text, handled, rest)
		}
	}
	command("/modellist")
	if !strings.Contains(note, "m1") || !strings.Contains(note, "m2") {
		t.Fatalf("list: %q", note)
	}
	command("/modelchange absent")
	if _, ok := s.modelPickOf(ctx, "server", "main"); ok {
		t.Fatal("unknown model was saved")
	}
	command("/modelchange m2")
	pick, ok := s.modelPickOf(ctx, "server", "main")
	if !ok || pick.Launcher != "gw" || pick.Model != "m2" {
		t.Fatalf("pick: %+v %v; note %q", pick, ok, note)
	}
	p, err := send(t, s, "main", map[string]any{})
	if err != nil || p["model"] != "m2" {
		t.Fatalf("next message: %v %v", p, err)
	}
	s.setBotCurrent(botTarget{Kind: targetAgent, MachineID: "server", SessionID: "agent"})
	command("/modelchange m1")
	pick, _ = s.modelPickOf(ctx, "server", "main")
	if pick.Model != "m2" {
		t.Fatalf("agent target changed main: %+v", pick)
	}
	s.setBotCurrent(botTarget{Kind: targetPersona, MachineID: "server", SessionID: "side"})
	command("/modelchange m1")
	pick, _ = s.modelPickOf(ctx, "server", "main")
	if pick.Model != "m2" {
		t.Fatalf("side target changed main: %+v", pick)
	}
}
