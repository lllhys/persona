package subscriptions

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/nwverse/persona/sdk/go/wire"
	"github.com/nwverse/persona/server/internal/connector"
)

// thingsRecipe reads the env connector's tool as Inbox rows made by the connector (#6296).
func thingsRecipe() Recipe {
	return Recipe{Mode: wire.ConnectorSubscriptionModeThings, Tool: "list_notices", Args: json.RawMessage(`{"since":"{{since}}"}`),
		ItemsPath: "$.things", Kind: "sub_message", GroupBy: "thread"}
}

// connectorRows are rows as a connector makes them, with what a recipe could not map: a done one, a read one, an sms:
// link, a reply line without a reply, a tier, tags and a context.
func connectorRows(read bool) []connector.InboxThing {
	at := time.Date(2026, 11, 13, 8, 0, 0, 0, time.UTC)
	return []connector.InboxThing{
		{Thread: "imessage-0123456789ab", Kind: "dm", Title: "Ann", Sender: "Ann", Preview: "Made-up: see you at three",
			Context: "Ann (11-13 08:00): Made-up: see you at three", At: at, Last: "1763020800000000000", OpenURL: "sms:+15550100",
			ReplyTo: "Reply in Messages (Persona doesn't send for you)", Tier: connector.TierAction, Read: read,
			Tags: []connector.Tag{{Key: connector.TagChat, Value: "imessage-0123456789ab", Name: "Ann"}}},
		{Thread: "imessage-ba9876543210", Kind: "group", Label: "Group", Title: "Hiking", Sender: "Bo", At: at.Add(-time.Hour),
			Last: "1763017200000000000", Tier: connector.TierFYI, Done: connector.DoneReplied},
	}
}

func rowsAnswer(rows []connector.InboxThing) string {
	b, _ := json.Marshal(map[string]any{"things": rows})
	return string(b)
}

// connectorsRows are a source's things but the failure notice's: every row the connector made, done ones too.
func (e *env) connectorsRows(id string) []connector.InboxThing {
	var out []connector.InboxThing
	for _, t := range e.inbox(id).Things {
		if t.Kind != KindFailing {
			out = append(out, t)
		}
	}
	return out
}

// sameRows says whether got and want are the same rows field for field, as the Inbox reads them.
func sameRows(t *testing.T, got, want []connector.InboxThing) {
	t.Helper()
	g, _ := json.Marshal(got)
	w, _ := json.Marshal(want)
	if string(g) != string(w) {
		t.Fatalf("rows:\n got %s\nwant %s", g, w)
	}
}

// A things recipe is checked as one: its rows are the connector's threads, it maps no fields and has no pages.
func TestAThingsRecipeIsChecked(t *testing.T) {
	if p := checkRecipe(thingsRecipe()); p != nil {
		t.Fatalf("a good one: %+v", p)
	}
	bySource := thingsRecipe()
	bySource.GroupBy = "source"
	fields := thingsRecipe()
	fields.Fields = &connector.SubscriptionFields{Title: "$.title"}
	paged := thingsRecipe()
	paged.PageArg = "page"
	for name, r := range map[string]Recipe{"group_by source": bySource, "fields": fields, "pages": paged} {
		if p := checkRecipe(r); p == nil || p.Code != CodeBadRecipe {
			t.Errorf("%s: %+v", name, p)
		}
	}
}

// The connector's rows go to the Inbox exactly as it made them (#6296); a row is kept again only when it changes, and
// the Inbox gets each thread's newest; the knowledge base takes none of them.
func TestThingsRowsGoToTheInboxAsTheyCame(t *testing.T) {
	e := newEnv(t)
	e.answer(rowsAnswer(connectorRows(false)))
	sub, err := e.svc.Create(e.ctx, CreateRequest{Connector: "example", Source: "notices", Recipe: ptr(thingsRecipe())}, Actor{ID: "device:phone"})
	if err != nil {
		t.Fatal(err)
	}
	e.turnOn(sub)
	e.svc.RunDue(e.ctx)
	sameRows(t, e.connectorsRows(sub.ID), connectorRows(false))
	kept := len(e.store.Items())
	e.clock.advance(20 * time.Minute)
	e.svc.RunDue(e.ctx)
	if n := len(e.store.Items()); n != kept {
		t.Fatalf("the same rows kept again: %d, was %d", n, kept)
	}
	e.answer(rowsAnswer(connectorRows(true))) // Ann's conversation read on the phone: the same move, now read
	e.clock.advance(20 * time.Minute)
	e.svc.RunDue(e.ctx)
	if n := len(e.store.Items()); n != kept+1 {
		t.Fatalf("a changed row: %d kept, want %d", len(e.store.Items()), kept+1)
	}
	sameRows(t, e.connectorsRows(sub.ID), connectorRows(true))
	if _, err := e.svc.Update(e.ctx, sub.ID, Patch{Revision: e.get(sub.ID).Revision, ToKB: ptr(true)}, Actor{ID: "device:phone"}); err == nil {
		t.Fatal("a things subscription's rows went to the knowledge base")
	}
	if ids, _ := e.svc.Archives(e.ctx); len(ids) != 0 {
		t.Fatalf("archives: %v", ids)
	}
}

// A built-in's default subscription of things hands the Inbox the built-in's rows under its id, unchanged (#6265, #6296).
func TestAMigratedThingsSubscriptionHandsTheBuiltinsRows(t *testing.T) {
	e := newEnv(t)
	e.answer(rowsAnswer(connectorRows(false)))
	if _, err := e.svc.Migrate(e.ctx, Migration{Connector: "example", Source: "notices", Label: "Example", Recipe: thingsRecipe()}, true, Actor{}); err != nil {
		t.Fatal(err)
	}
	e.svc.RunDue(e.ctx)
	sameRows(t, e.connectorsRows("example"), connectorRows(false))
}

// A things answer with an item that is no Inbox row fails the dry run's fields check (bad_thing).
func TestADryRunOfThingsWantsInboxRows(t *testing.T) {
	e := newEnv(t)
	e.answer(`{"things":[{"thread":"t1","title":"No last"}]}`)
	r := thingsRecipe()
	dr, err := e.svc.DryRun(e.ctx, DryRunRequest{Connector: "example", Source: "notices", Recipe: &r})
	if err == nil && dr.OK {
		t.Fatalf("passed: %+v", dr)
	}
	if dr.Checks[2].Error == nil || dr.Checks[2].Error.Code != CodeBadThing {
		t.Fatalf("checks: %+v (%v)", dr.Checks, err)
	}
}
