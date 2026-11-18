package imessage

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/nwverse/persona/sdk/go/wire"
	"github.com/nwverse/persona/server/internal/connector"
	"github.com/nwverse/persona/server/internal/subscriptions"
)

// sameRows says whether two lists of Inbox rows are the same field for field, as the Inbox reads them (their JSON).
func sameRows(t *testing.T, what string, got, want []connector.InboxThing) {
	t.Helper()
	g, _ := json.Marshal(got)
	w, _ := json.Marshal(want)
	if len(want) == 0 || string(g) != string(w) {
		t.Fatalf("%s:\n got %s\nwant %s", what, g, w)
	}
}

// inboxThingsRecipe is iMessage's default subscription's recipe (#6317): its rows, ready-made.
func inboxThingsRecipe() subscriptions.Recipe {
	return subscriptions.Recipe{Mode: wire.ConnectorSubscriptionModeThings, Tool: inboxThings, ItemsPath: "$.things",
		Kind: wire.ConnectorSubscriptionKindSubMessage, GroupBy: wire.ConnectorSubscriptionGroupThread}
}

// On the same Mac, inbox_things answers exactly the rows Collect has (#6317): one computation. Through the catalog
// (its read-only check) as a subscription calls it, the existing read tools are still in it.
func TestInboxThingsAreCollectsRows(t *testing.T) {
	ctx := context.Background()
	c, _ := personaRemote(t, map[string]string{"connector.chats": chatsPage(time.Now()), "connector.older": olderPage})
	c.look(ctx, func(connector.Scope) {})
	want, _ := c.Collect(ctx)
	out, err := connector.CallCatalog(ctx, c, connector.CatalogQuery{Source: ID, Tool: inboxThings, Args: json.RawMessage(`{}`)})
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Things []connector.InboxThing `json:"things"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatal(err)
	}
	sameRows(t, "inbox_things", got.Things, want)
	sources, err := connector.ReadCatalog(ctx, c)
	if err != nil || len(sources) != 1 {
		t.Fatalf("catalog: %+v %v", sources, err)
	}
	var names []string
	for _, tool := range sources[0].Tools {
		names = append(names, tool.Name)
	}
	if got := json.RawMessage(mustJSON(names)); string(got) != `["chats","read","search","inbox_things"]` {
		t.Fatalf("the catalog's tools: %s", got)
	}
	for _, tool := range c.Tools() {
		if tool.Name == "imessage_inbox_things" {
			t.Fatal("inbox_things is one of the assistant's tools")
		}
	}
}

// oneConnector is the subscriptions' registry with only this Mac's iMessage in it.
type oneConnector struct{ c *Remote }

func (o oneConnector) Find(_ context.Context, id string) (subscriptions.ConnectorInfo, bool) {
	if id != ID {
		return subscriptions.ConnectorInfo{}, false
	}
	return subscriptions.ConnectorInfo{ID: ID, Conn: o.c, Enabled: true, Status: connector.StateOK}, true
}

func (o oneConnector) All(ctx context.Context) []subscriptions.ConnectorInfo {
	info, _ := o.Find(ctx, ID)
	return []subscriptions.ConnectorInfo{info}
}

// iMessage's Inbox feed switched to its default things subscription hands the Inbox, under iMessage's own id, exactly
// the rows its Collect does (#6317, G1 of the migration): the same fixtures, through the real subscription service.
func TestTheSwitchedFeedHandsCollectsRows(t *testing.T) {
	ctx := context.Background()
	c, _ := personaRemote(t, map[string]string{"connector.chats": chatsPage(time.Now()), "connector.older": olderPage})
	c.look(ctx, func(connector.Scope) {})
	want, _ := c.Collect(ctx)
	svc := subscriptions.New(subscriptions.Options{Store: subscriptions.NewMemStore(), Connectors: oneConnector{c},
		Location: func() *time.Location { return time.UTC }})
	if _, err := svc.Migrate(ctx, subscriptions.Migration{Connector: ID, Source: ID, Label: c.Name(), Recipe: inboxThingsRecipe()}, true,
		subscriptions.Actor{ID: "device:phone"}); err != nil {
		t.Fatal(err)
	}
	svc.RunDue(ctx)
	srcs, err := svc.InboxSources(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var got []connector.InboxThing
	for _, s := range srcs {
		if s.ID != ID {
			continue
		}
		for _, th := range s.Things {
			if th.Kind != subscriptions.KindFailing {
				got = append(got, th)
			}
		}
	}
	sameRows(t, "the switched feed", got, want)
}

func mustJSON(v any) []byte {
	b, _ := json.Marshal(v)
	return b
}
