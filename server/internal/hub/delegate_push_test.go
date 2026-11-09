package hub

import (
	"strings"
	"testing"

	"github.com/nwverse/persona/server/internal/i18n"
	"github.com/nwverse/persona/server/internal/proto"
)

// A question the assistant left to the user (docs/assistant/assistant.md "Assistant answers questions first") says what it recommends in the push.
func TestDelegatedQuestionPushSaysTheRecommendation(t *testing.T) {
	ix := proto.Interaction{ID: "ix_1", Kind: proto.IxQuestion, SessionTitle: "修登录",
		Questions: []proto.Question{{Question: "用哪个分支？"}}}
	if body := alertOf(t, interactionPayloadIn(i18n.ZhHans, ix, "m1"))["body"]; body != "用哪个分支？" {
		t.Fatalf("plain body %q", body)
	}
	ix.Delegate = &proto.Delegate{What: "修登录", Recommend: "main"}
	body, _ := alertOf(t, interactionPayloadIn(i18n.ZhHans, ix, "m1"))["body"].(string)
	if !strings.HasPrefix(body, "用哪个分支？\n") || !strings.Contains(body, "main") {
		t.Fatalf("body %q", body)
	}
}
