package assistant

import (
	"context"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/assistantwire"
	"github.com/nwverse/persona/server/internal/connector"
	"github.com/nwverse/persona/server/internal/kb"
)

// A round's subscription says what it reads and its rows' knowledge-base chats (#6222): the book keeps them for the AI
// twin, and the next round's say replaces them (the knowledge base turned off: no chat).
func TestARoundSaysWhatASubscriptionReads(t *testing.T) {
	h := newInboxHub(t)
	ctx := context.Background()
	round := assistantwire.InboxRound{Round: roundPoll, Sources: []assistantwire.InboxSource{{ID: "sub_x", Name: "学校通知",
		Extension: true, Counts: true, Via: "example", ViaSource: "通知", Chats: map[string]string{"sub:sub_x:t1": "tabc"},
		Things: []connector.InboxThing{}}}}
	if err := h.InboxCollected(ctx, round); err != nil {
		t.Fatal(err)
	}
	it := &InboxItem{ID: "sub_x:1", Source: "sub_x", Connector: &InboxConnector{Thread: "sub:sub_x:t1"}, Agent: &InboxAgent{}}
	if got := h.inbox.kbChatOf(it); got != kb.ChatID("sub_x", "tabc") {
		t.Fatalf("the row's chat: %q", got)
	}
	a := h.inbox.inboxAgentFor(it, "示例用户", nil, 1, time.UTC)
	if c := a.Connector; c == nil || c.Via != "example" || c.ViaSource != "通知" || c.Name != "学校通知" {
		t.Fatalf("the twin's connector: %+v", a.Connector)
	}
	round.Sources[0].Chats = nil
	if err := h.InboxCollected(ctx, round); err != nil {
		t.Fatal(err)
	}
	if got := h.inbox.kbChatOf(it); got != "" {
		t.Fatalf("a chat after the knowledge base was turned off: %q", got)
	}
	if got := h.inbox.kbChatOf(&InboxItem{Source: "example", Connector: &InboxConnector{Thread: "x"}}); got != "" {
		t.Fatalf("a connector's own row has a chat: %q", got)
	}
}
