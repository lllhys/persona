package extmgr

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/nwverse/persona/sdk/go/wire"
	"github.com/nwverse/persona/server/internal/assistantwire"
	"github.com/nwverse/persona/server/internal/connector"
	"github.com/nwverse/persona/server/internal/subscriptions"
	"github.com/nwverse/persona/server/internal/subscriptionsui"
)

// A built-in whose Inbox feed is switched to its default subscription is not read in the round; the subscription's
// rows come under its id instead, once (#6265). Switched back, the built-in is read again.
func TestAMigratedBuiltinIsReadThroughItsSubscription(t *testing.T) {
	h := newTestManager(t)
	ctx := context.Background()
	h.connectors = connector.NewRegistry(nil)
	c := &inboxConnector{things: []connector.InboxThing{{Thread: "chat-1", Kind: "review_requested", Title: "From the built-in", Last: "b1", At: time.Now()}}}
	_ = h.addConnector(c)
	store := subscriptions.NewMemStore()
	now := time.Now().UTC()
	at := now.Add(-time.Minute)
	sub := subscriptions.Subscription{ID: subscriptions.MigrationID(c.ID()), Connector: c.ID(), Source: "notices", Label: "Example",
		Recipe: subscriptions.Recipe{Mode: "items", Tool: "list", Kind: "sub_message", GroupBy: "thread"}, Migrated: &at,
		InboxCatchup: &now, Revision: 1, CreatedAt: now.Add(-time.Hour)}
	if err := store.CreateSubscription(ctx, sub); err != nil {
		t.Fatal(err)
	}
	items := []subscriptions.Item{{UID: "m1", Thread: "chat-1", Title: "From the subscription", Kind: "sub_message"}}
	if err := store.CommitRun(ctx, subscriptions.RunCommit{Sub: sub, Revision: 1, Items: items, Now: now.Add(-30 * time.Second)}); err != nil {
		t.Fatal(err)
	}
	h.subs = subscriptions.New(h.subscriptionOptions(store))
	h.subsLive.Store(h.subs)

	ofExample := func(r assistantwire.InboxRound) []assistantwire.InboxSource {
		var out []assistantwire.InboxSource
		for _, s := range r.Sources {
			if s.ID == c.ID() {
				out = append(out, s)
			}
		}
		return out
	}
	collected := c.collect.Load()
	got := ofExample(h.readInbox(ctx, roundPoll))
	if len(got) != 1 || !got[0].Counts || len(got[0].Things) == 0 || got[0].Things[0].Thread != "chat-1" || got[0].Things[0].Last != "m1" {
		t.Fatalf("the migrated source in the round: %+v", got)
	}
	if c.collect.Load() != collected {
		t.Fatal("the built-in was read while its feed is the subscription")
	}
	if _, err := h.subs.Migrate(ctx, subscriptions.Migration{Connector: c.ID(), Source: "notices", Recipe: sub.Recipe}, false, subscriptions.Actor{}); err != nil {
		t.Fatal(err)
	}
	got = ofExample(h.readInbox(ctx, roundPoll))
	if len(got) != 1 || got[0].Counts || len(got[0].Things) != 1 || got[0].Things[0].Last != "b1" {
		t.Fatalf("the built-in after switching back: %+v", got)
	}
}

// A built-in with a default subscription switches to it from its page and back (#6345): the page sees the switch and
// the subscription, which is made named as the source; iMessage's default reads its rows ready-made.
func TestABuiltinSwitchesToItsDefaultSubscription(t *testing.T) {
	h := newTestManager(t)
	ctx := context.Background()
	h.connectors = connector.NewRegistry(nil)
	c := &inboxConnector{}
	_ = h.addConnector(c)
	h.subs = subscriptions.New(h.subscriptionOptions(subscriptions.NewMemStore()))
	h.subsLive.Store(h.subs)
	migrations[c.ID()] = subscriptions.Migration{Connector: c.ID(), Source: "notices", Recipe: migrations["imessage"].Recipe}
	defer delete(migrations, c.ID())
	b := builtinSources{h}
	viaOf := func() *subscriptionsui.Via {
		for _, s := range b.List(ctx) {
			if s.ID == c.ID() {
				return s.Via
			}
		}
		t.Fatal("the built-in is not listed")
		return nil
	}
	if v := viaOf(); v == nil || v.On || v.Subscription != "" {
		t.Fatalf("before: %+v", v)
	}
	if err := b.SetMigrated(ctx, c.ID(), true, subscriptions.Actor{ID: "device:phone"}); err != nil {
		t.Fatal(err)
	}
	if v := viaOf(); !v.On || v.Subscription != subscriptions.MigrationID(c.ID()) {
		t.Fatalf("switched on: %+v", v)
	}
	s, _, err := h.subs.Get(ctx, subscriptions.MigrationID(c.ID()))
	if err != nil || s.Label != connector.DisplayName(ctx, c) || s.Recipe.Mode != wire.ConnectorSubscriptionModeThings || s.ToInbox ||
		s.RecipeOrigin != subscriptions.OriginPersona {
		t.Fatalf("its subscription: %+v %v", s, err)
	}
	if err := b.SetMigrated(ctx, c.ID(), false, subscriptions.Actor{ID: "device:phone"}); err != nil || viaOf().On {
		t.Fatalf("switched back: %v %+v", err, viaOf())
	}
	if err := b.SetMigrated(ctx, "lark", true, subscriptions.Actor{}); err == nil {
		t.Fatal("a source without a default switched")
	}
	// GitHub's default is known but withheld until the owner says (#6404): no switch, and switching on is refused.
	if m := migrations["github"]; m.Recipe.Tool != subscriptions.InboxThingsTool || m.Recipe.Mode != wire.ConnectorSubscriptionModeThings || !withheld["github"] {
		t.Fatalf("GitHub's default: %+v (withheld %v)", m.Recipe, withheld["github"])
	}
	withheld[c.ID()] = true // as GitHub's is: a default known, not offered
	if v := viaOf(); v != nil {
		t.Fatalf("a withheld default's switch is offered: %+v", v)
	}
	if err := b.SetMigrated(ctx, c.ID(), true, subscriptions.Actor{}); err == nil {
		t.Fatal("a withheld default switched on")
	}
	delete(withheld, c.ID())
	if withheld["imessage"] {
		t.Fatal("iMessage's default is withheld")
	}
	if m := migrations["imessage"]; m.Recipe.Tool != "inbox_things" || m.Recipe.Mode != wire.ConnectorSubscriptionModeThings || m.Recipe.ItemsPath != "$.things" {
		t.Fatalf("iMessage's default: %+v", m.Recipe)
	}
}

// A built-in read through its default subscription keeps what the Inbox does with its things (#6381): its round entry
// is the built-in's, grouping, event meta and settling included, with the connector's own rows as they came, no
// counts of the subscription's and no subscription named for the AI twin.
func TestASwitchedSourceKeepsItsBuiltinsAbilities(t *testing.T) {
	h := newTestManager(t)
	ctx := context.Background()
	h.connectors = connector.NewRegistry(nil)
	g := &groupingConnector{}
	_ = h.addConnector(g)
	store := subscriptions.NewMemStore()
	now := time.Now().UTC()
	at := now.Add(-time.Minute)
	sub := subscriptions.Subscription{ID: subscriptions.MigrationID(g.ID()), Connector: g.ID(), Source: g.ID(), Label: "Example chat",
		Recipe:   subscriptions.Recipe{Mode: wire.ConnectorSubscriptionModeThings, Tool: subscriptions.InboxThingsTool, Kind: "sub_message", GroupBy: "thread"},
		Migrated: &at, InboxCatchup: &now, Revision: 1, CreatedAt: now.Add(-time.Hour)}
	if err := store.CreateSubscription(ctx, sub); err != nil {
		t.Fatal(err)
	}
	row := connector.InboxThing{Thread: "chat:C1", Kind: "mention", Title: "Made-up: the plan", Last: "m7", At: now.Add(-2 * time.Minute)}
	raw, _ := json.Marshal(row)
	items := []subscriptions.Item{{UID: "t:1", Thread: row.Thread, Title: row.Title, Kind: row.Kind, Thing: raw}}
	if err := store.CommitRun(ctx, subscriptions.RunCommit{Sub: sub, Revision: 1, Items: items, Now: now.Add(-30 * time.Second)}); err != nil {
		t.Fatal(err)
	}
	h.subs = subscriptions.New(h.subscriptionOptions(store))
	h.subsLive.Store(h.subs)
	var src *assistantwire.InboxSource
	for _, s := range h.readInbox(ctx, roundPoll).Sources {
		if s.ID == g.ID() {
			if src != nil {
				t.Fatal("the source is in the round twice")
			}
			src = &s
		}
	}
	if src == nil || !src.Group || !src.Meta || !src.Settles || src.Counts || src.Via != "" || len(src.Things) == 0 {
		t.Fatalf("the switched source's entry: %+v", src)
	}
	if got := src.Things[0]; got.Thread != row.Thread || got.Last != row.Last || got.Kind != row.Kind {
		t.Fatalf("its row: %+v", got)
	}
}
