package assistant

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/connector"
)

// A lark-cli group item sorted FYI that then gets a request @-ing the owner stays the same item with new content, is
// asked about again with the owner's mark in its context, and the model's answer lifts it to Needs you (#9124). The
// stub model proves the scheduling and what is persisted, not how a real model reads the marks.
func TestLarkCLIFYIItemIsAskedAgainWhenTheOwnerIsMentioned(t *testing.T) {
	h := newInboxHub(t)
	now := time.Now()
	larkThing := func(last, context string, at time.Time) connector.InboxThing {
		return connector.InboxThing{Thread: "larkcli:chat:oc_example", Kind: "mention", Label: "Mention", Title: "Example Group",
			Sender: "Example peer", Preview: "preview", Context: context, At: at, Last: last, CanReply: true}
	}
	update := "Example peer（@所有人）: @_all the release went out"
	h.inbox.mu.Lock()
	h.inbox.applyConnector("larkcli", "Lark", []connector.InboxThing{larkThing("om_1", update, now.Add(-time.Hour))}, now)
	it := openItems(h)[0]
	h.inbox.mu.Unlock()

	h.in.model, _ = captureModel(t, `{"tier":"fyi","summary":"The release went out","reason":"An @all update"}`)
	if !h.triageOne(context.Background(), it) || it.Tier != TierFYI || it.TriagedAt != "om_1" {
		t.Fatalf("setup: tier %q, triaged at %q", it.Tier, it.TriagedAt)
	}

	request := update + "\nExample peer（@了我）: @Example user can you approve the budget today?"
	h.inbox.mu.Lock()
	h.inbox.applyConnector("larkcli", "Lark", []connector.InboxThing{larkThing("om_2", request, now)}, now)
	open := openItems(h)
	h.inbox.mu.Unlock()
	if len(open) != 1 || open[0] != it || it.Last != "om_2" || it.Context != request {
		t.Fatalf("the request did not join the FYI item: %d open, last %q, context %q", len(open), it.Last, it.Context)
	}
	if !it.open() || it.TriagedAt == it.Last || it.handled() {
		t.Fatalf("the FYI item's new content is not up for triage: open %v, triaged at %q, last %q", it.open(), it.TriagedAt, it.Last)
	}

	model, calls := captureModel(t, `{"tier":"action","summary":"Approve the budget today","reason":"Asked to approve","urgent":true}`)
	h.in.model = model
	if !h.triageOne(context.Background(), it) {
		t.Fatal("the new content was not triaged")
	}
	got := calls()
	if len(got) != 1 {
		t.Fatalf("model calls = %d", len(got))
	}
	if !strings.Contains(got[0].User, "Example peer（@了我）: @Example user can you approve the budget today?") {
		t.Fatalf("the triage input lost the owner's mark:\n%s", got[0].User)
	}
	if !strings.Contains(got[0].System, "lark-cli") || !strings.Contains(got[0].System, "「（@了我）」") {
		t.Fatalf("the criteria do not say lark-cli messages carry the marks:\n%s", got[0].System)
	}
	if it.Tier != TierAction || !it.Urgent || it.TriagedAt != "om_2" || it.Summary != "Approve the budget today" {
		t.Fatalf("not lifted to Needs you: tier %q, urgent %v, triaged at %q, summary %q", it.Tier, it.Urgent, it.TriagedAt, it.Summary)
	}
}
