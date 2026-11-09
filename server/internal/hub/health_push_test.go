package hub

import (
	"strings"
	"testing"

	"github.com/nwverse/persona/server/internal/proto"
)

// A health conversation's card says on the lock screen that the health space wants you, not what it is about nor the
// conversation's title (docs/assistant/health.md "Outbound"); other cards are as before.
func TestHealthCardPushHidesContent(t *testing.T) {
	alert := func(ix proto.Interaction) map[string]any {
		return interactionPayload(ix, "server")["aps"].(map[string]any)["alert"].(map[string]any)
	}
	a := alert(proto.Interaction{Kind: proto.IxPermission, ToolName: "mail_send", SessionTitle: "血压", Cwd: "/home/me/.persona/health",
		Summary: "发给 doctor@example.com：最近一周血压 150/95"})
	if body, sub := a["body"].(string), a["subtitle"].(string); strings.Contains(body, "150") || strings.Contains(sub, "血压") || !strings.Contains(sub, "健康") {
		t.Fatalf("health card: %v", a)
	}
	q := alert(proto.Interaction{Kind: proto.IxQuestion, ToolName: "ask_user", SessionTitle: "睡眠", Cwd: "/home/me/.persona/health",
		Questions: []proto.Question{{Question: "昨晚几点睡的？"}}})
	if strings.Contains(q["body"].(string), "几点") {
		t.Fatalf("health question: %v", q)
	}
	if o := alert(proto.Interaction{Kind: proto.IxPermission, SessionTitle: "Persona", Cwd: "/home/me/.persona/assistant", Summary: "发周报"}); o["body"] != "发周报" {
		t.Fatalf("assistant's own card: %v", o)
	}
	// A card from a folder of the health space (a hook after the model's `cd`, #2368) is the space's too: no content,
	// no answer buttons. A directory that only starts the same, or a folder named health elsewhere, is not.
	ask := func(cwd string) proto.Interaction {
		return proto.Interaction{Kind: proto.IxQuestion, ToolName: "AskUserQuestion", SessionTitle: "报告", Cwd: cwd,
			Questions: []proto.Question{{Question: "血糖偏高要记下吗？", Options: []proto.QuestionOption{{Label: "记"}, {Label: "不记"}}}}}
	}
	for cwd, hidden := range map[string]bool{
		"/home/me/.persona/health/reports": true,
		"/home/me/.persona/health-bin":     false,
		"/tmp/health":                      false,
	} {
		payload := interactionPayload(ask(cwd), "server")
		body := payload["aps"].(map[string]any)["alert"].(map[string]any)["body"].(string)
		_, buttons := payload["ask"]
		if hidden != !strings.Contains(body, "血糖") || hidden == buttons {
			t.Errorf("%s: body %q, answer buttons %v; want hidden %v", cwd, body, buttons, hidden)
		}
	}
}
