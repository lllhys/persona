package assistant

import (
	"context"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/connector"
)

// The things of a plugin that took Feishu over keep the items the hub made itself: the same item goes on (its start
// stays), new ones get the hub's own shape and id, read and replied end them as before, a re-read document's rows
// update the item; replies and status moves go to the plugin with the hub's client id.
func TestMigratedInbox(t *testing.T) {
	ctx := context.Background()
	h := newInboxHub(t)
	h.inboxReady(ctx)
	p := &extensionLark{}
	h.addConnector(p, false)
	now := time.Now()
	old := &InboxItem{ID: "lark:10:3", Source: "lark", Kind: KindDM, Account: "feishu", AccountName: "飞书", Title: "Alice", Last: "3", Count: 1,
		StartedAt: now.Add(-time.Hour), UpdatedAt: now.Add(-time.Hour), Lark: &InboxLark{AccountID: 1, FeedID: "10", StartPosition: "3"}}
	h.inbox.items[old.ID] = old
	extra := func(block string, v any, legacy string, count int) *connector.ThingExtra {
		x := &connector.ThingExtra{Source: "lark", Account: "feishu", AccountName: "飞书", LegacyID: legacy, Count: count, StartedAt: now}
		switch block {
		case "lark":
			x.Lark = rawJSON(v)
		case "doc":
			x.Doc = rawJSON(v)
		}
		return x
	}
	p.things = []connector.InboxThing{
		{Thread: "chat:10", Kind: KindDM, Title: "Alice", Preview: "还在吗", At: now, Last: "5", CanReply: true,
			Extra: extra("lark", InboxLark{AccountID: 1, FeedID: "10", StartPosition: "4", ReplyTo: "m5"}, "lark:10:4", 2)},
		{Thread: "doc:n1", Kind: KindDoc, Title: "周报", At: now, Last: "100",
			Extra: extra("doc", InboxDoc{NoticeID: "n1", Type: "sheet", Rows: []DocRow{{Ref: "t!1", Title: "行", Status: []DocStatus{{Field: "2", Name: "状态", Value: "进行中",
				Options: []DocOption{{Name: "进行中"}, {Name: "完成"}}}}}}}, "doc:n1:100", 1)},
	}
	h.collectInbox(ctx, roundPoll)
	if len(h.inbox.items) != 2 {
		t.Fatalf("items: %d", len(h.inbox.items))
	}
	if old.Last != "5" || old.Count != 2 || old.Lark.StartPosition != "3" || old.Lark.ReplyTo != "m5" || old.Connector != nil || old.Preview != "还在吗" {
		t.Fatalf("the hub's own item goes on: %+v %+v", old, old.Lark)
	}
	doc := h.inbox.items["doc:n1:100"]
	if doc == nil || doc.Doc == nil || doc.Connector != nil || doc.Account != "feishu" || len(doc.Doc.Rows) != 1 {
		t.Fatalf("a new item in the hub's own shape: %+v", doc)
	}

	// The same move, the document read again: its rows change.
	p.things[1].Extra = extra("doc", InboxDoc{NoticeID: "n1", Type: "sheet", Rows: []DocRow{{Ref: "t!1", Title: "行", Status: []DocStatus{{Field: "2", Name: "状态", Value: "完成",
		Options: []DocOption{{Name: "进行中"}, {Name: "完成"}}}}}}}, "doc:n1:100", 1)
	h.collectInbox(ctx, roundPoll)
	if doc.Doc.Rows[0].Status[0].Value != "完成" {
		t.Fatalf("rows not updated: %+v", doc.Doc.Rows)
	}

	// A reply goes to the plugin with the client id the hub would have used itself; a status move as changes.
	if _, err := h.replyInbox(ctx, "lark:10:3", "在的"); err != nil {
		t.Fatal(err)
	}
	want := namedClientID("lark:10:3" + "\n" + "在的")
	if len(p.got) != 1 || p.got[0].Thread != "chat:10" || p.got[0].ClientID != want || p.got[0].Text != "在的" || old.DoneBy != "replied" {
		t.Fatalf("reply: %+v, item done by %q", p.got, old.DoneBy)
	}
	if _, err := h.setDocStatus(ctx, "doc:n1:100", DocChange{Ref: "t!1", Field: "2", Value: "进行中"}); err != nil {
		t.Fatal(err)
	}
	if len(p.got) != 2 || p.got[1].Thread != "doc:n1" || len(p.got[1].Changes) != 1 || p.got[1].Changes[0].Value != "进行中" || p.got[1].Text != "" {
		t.Fatalf("status move: %+v", p.got)
	}
	if _, err := h.setDocStatus(ctx, "doc:n1:100", DocChange{Ref: "t!1", Field: "2", Value: "没有这个"}); err != errNoDocRow {
		t.Fatalf("a value the column does not offer: %v", err)
	}

	// Read in Feishu: the doc (untriaged) is read; replied: the chat's next item ends.
	p.things[1].Read = true
	h.collectInbox(ctx, roundPoll)
	if !doc.Read {
		t.Fatal("read elsewhere")
	}
}

// A plugin connector's thing with its own fallback (extra.rule_tier, capability fallback) and no tier is still sorted
// by the model; when the model cannot, it comes in with that fallback ("By rule") instead of being held back.
func TestExtensionThingFallbackTier(t *testing.T) {
	_, client := newTriageModel(t, "fail", `{"tier":"action","summary":"示例","reason":"示例"}`)
	th := genericThing("example-org/example-repo#8", "a", "示例：一条评论")
	th.Extra = &connector.ThingExtra{RuleTier: TierFYI}
	h, st := admitHub(t, client, &inboxConnector{things: []connector.InboxThing{th}})
	collectAndAdmit(h)
	it := itemOf(h, "example", th.Thread)
	if it == nil || it.Tier != TierFYI || it.Reason != "按规则" || it.TriagedAt != it.Last || !st.saved(it.ID) {
		t.Fatalf("item %+v", it)
	}
	if _, status := inboxTriageOf(t, h); status.Waiting != 0 {
		t.Fatalf("held back: %+v", status)
	}
}

// A migrated connector's thing whose rules decided its tier is filed with it, without the model: the same contract a
// generic connector thing has (InboxThing.Tier), for items that keep the hub's own blocks. A rule changed for the same
// move re-tiers the open item, the next move follows the rules again, and a user's policy for the chat still wins.
func TestMigratedThingRuleTier(t *testing.T) {
	ctx := context.Background()
	h := muteHub(t)
	p := &extensionLark{}
	h.addConnector(p, false)
	now := time.Now()
	larkExtra := func(legacy, start string) *connector.ThingExtra {
		return &connector.ThingExtra{Source: "lark", Account: "feishu", AccountName: "飞书", LegacyID: legacy, Count: 1, StartedAt: now,
			Lark: rawJSON(InboxLark{AccountID: 1, FeedID: "10", StartPosition: start})}
	}
	p.things = []connector.InboxThing{{Thread: "chat:10", Kind: KindDM, Title: "Alice", Preview: "P2 告警", At: now, Last: "5",
		Tier: connector.TierNoise, Why: "Muted in Feishu", Extra: larkExtra("lark:10:5", "5")}}
	h.collectInbox(ctx, roundPoll)
	it := h.inbox.items["lark:10:5"]
	if it == nil || it.Connector != nil || it.Lark == nil {
		t.Fatalf("item: %+v", it)
	}
	if it.Tier != TierNoise || it.Reason != "Muted in Feishu" || it.TriagedAt != it.Last {
		t.Fatalf("the rule tier is not applied: %+v", it)
	}

	// The rules changed since it was tiered (the chat was unmuted): the open item is re-tiered at once.
	p.things[0].Tier, p.things[0].Why = connector.TierAction, "Needs you"
	h.collectInbox(ctx, roundPoll)
	if it.Tier != TierAction || it.Reason != "Needs you" || it.TriagedAt != it.Last {
		t.Fatalf("the rule tier is not updated: %+v", it)
	}

	// The next move follows the rules, and the user's policy for the chat has the last word in the same round.
	setPolicyFor(t, h, InboxPolicy{Source: "lark", Tag: "chat", Value: "10", Name: "Alice", Tier: TierFYI})
	p.things[0].At, p.things[0].Last, p.things[0].Extra = now.Add(time.Minute), "6", larkExtra("lark:10:6", "6")
	p.things[0].Tier, p.things[0].Why = connector.TierNoise, "Muted in Feishu"
	h.collectInbox(ctx, roundPoll)
	if it.Tier != TierFYI || it.TriagedAt != it.Last {
		t.Fatalf("the policy does not win: %+v", it)
	}
}
