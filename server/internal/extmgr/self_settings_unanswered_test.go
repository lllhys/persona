package extmgr

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/prompts"
	"github.com/nwverse/persona/server/internal/proto"
)

// A change of the manager's settings is the user's to approve (#3722): a card that expired or was closed, or one
// someone else answered for the user, changes nothing and says nobody answered, never that the user declined.
func TestSelfSettingsUnansweredCardIsNoDecline(t *testing.T) {
	h := sourcesHub(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	yes, no := true, false
	for name, r := range map[string]proto.Resolution{
		"expired or closed": {Dismiss: true},
		"allowed for them":  {Allow: &yes, AnsweredBy: proto.AnsweredByAssistant},
		"denied for them":   {Allow: &no, AnsweredBy: proto.AnsweredByAssistant},
		"cancelled":         {Cancel: true},
	} {
		th := hubOf(h)
		th.mu.Lock()
		th.answer = func(proto.Interaction) proto.Resolution { return r }
		th.mu.Unlock()
		out, err := h.toolPersonaSettings(ctx, "server", "s1", json.RawMessage(`{"action":"set","key":"inbox.rules.example-chat","value":"机器人发的都算噪音"}`))
		if err != nil || out != prompts.RenderSettingUnanswered() {
			t.Errorf("%s: %q %v", name, out, err)
		}
		var c map[string]any
		_ = json.Unmarshal(h.extensions.reg.Get("example-chat").Record.Config, &c)
		if c["triage_rules"] != nil {
			t.Errorf("%s: the rule was set: %v", name, c)
		}
	}
}
