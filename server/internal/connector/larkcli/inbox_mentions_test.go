package larkcli

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	adapter "github.com/nwverse/persona/server/internal/larkcli"
	"github.com/nwverse/persona/server/internal/prompts"
)

// The context marks whom each message @'s from its structured mentions, as the tier criteria read them
// (prompts/inbox/tiers.md): a Lark group message without a mark did not @ the owner (#9124).
func TestContextMarksWhomEachMessageMentions(t *testing.T) {
	const me = "ou_example_user"
	message := func(text string, mentions ...adapter.Mention) adapter.Message {
		raw, _ := json.Marshal(text)
		return adapter.Message{ChatType: "group", Sender: adapter.Sender{ID: "ou_example_peer", Name: "Example peer"},
			Content: raw, Mentions: mentions}
	}
	owner := adapter.Mention{ID: me, Key: "@_user_1", Name: "Example user"}
	all := adapter.Mention{ID: "all", Key: "@_all", Name: "everyone"}
	for _, tc := range []struct {
		name    string
		message adapter.Message
		want    string
	}{
		{"owner by id", message("@Example user please review the plan today", owner), "Example peer（@了我）: @Example user please review the plan today"},
		{"another person with the owner's name", message("@Example user please review", adapter.Mention{ID: "ou_example_namesake", Name: "Example user"}),
			"Example peer: @Example user please review"},
		{"another person", message("@Example other please review", adapter.Mention{ID: "ou_example_other", Name: "Example other"}),
			"Example peer: @Example other please review"},
		{"a bot", message("@Example bot deploy it", adapter.Mention{ID: "ou_example_bot", Name: "Example bot"}), "Example peer: @Example bot deploy it"},
		{"everyone", message("@_all fill in the weekly report", all), "Example peer（@所有人）: @_all fill in the weekly report"},
		{"everyone written in the text", message("@_all fill in the weekly report"), "Example peer（@所有人）: @_all fill in the weekly report"},
		{"owner and everyone", message("@_all @Example user please confirm", all, owner), "Example peer（@了我）（@所有人）: @_all @Example user please confirm"},
		{"no one", message("the release went out"), "Example peer: the release went out"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := contextText([]adapter.Message{tc.message}, me); got != tc.want {
				t.Fatalf("context = %q, want %q", got, tc.want)
			}
		})
	}
	// Without an owner id nothing is marked as the owner's; a sender known only by id still gets its marks.
	if got := contextText([]adapter.Message{message("@Example user hi", owner)}, ""); got != "Example peer: @Example user hi" {
		t.Fatalf("no owner id: %q", got)
	}
	bare := message("@Example user hi", owner)
	bare.Sender = adapter.Sender{}
	if got := contextText([]adapter.Message{bare}, me); got != "（@了我）: @Example user hi" {
		t.Fatalf("no sender: %q", got)
	}
}

// The marks are spelled exactly as the tier criteria name them.
func TestContextMarksMatchTheTierCriteria(t *testing.T) {
	criteria := prompts.RenderInboxTriage(prompts.InboxTriage{})
	for _, mark := range []string{markOwner, markAll} {
		if !strings.Contains(criteria, "「"+mark+"」") {
			t.Errorf("the tier criteria do not name the mark %q", mark)
		}
	}
}

// A collected group item carries the marks per message: the request that @'s the owner is marked, the message in the
// same chat that @'s someone else is not.
func TestInboxItemContextMarksTheOwnerMention(t *testing.T) {
	request := adapter.Message{MessageID: "om_request", MessagePosition: "10", ChatID: "oc_example", ChatType: "group",
		CreateTime: "2030-01-01T00:00:00Z", Mentions: []adapter.Mention{{ID: "ou_example_user", Name: "Example user"}},
		Sender: adapter.Sender{ID: "ou_example_peer", Name: "Example peer"}, Content: json.RawMessage(`"@Example user can you approve the budget today?"`)}
	other := request
	other.MessageID, other.MessagePosition = "om_other", "9"
	other.Mentions = []adapter.Mention{{ID: "ou_example_other", Name: "Example other"}}
	other.Content = json.RawMessage(`"@Example other please send the numbers"`)
	c := followupConnector(t, []adapter.Message{request}, []adapter.Message{other, request})
	if _, err := c.Poll(context.Background()); err != nil {
		t.Fatal(err)
	}
	got := inboxThings(t, c)["larkcli:chat:oc_example"]
	if got.Kind != "mention" {
		t.Fatalf("kind = %q: %+v", got.Kind, got)
	}
	want := "Example peer: @Example other please send the numbers\nExample peer（@了我）: @Example user can you approve the budget today?"
	if got.Context != want {
		t.Fatalf("context = %q, want %q", got.Context, want)
	}
}
