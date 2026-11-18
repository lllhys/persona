package github

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/nwverse/persona/sdk/go/wire"
	"github.com/nwverse/persona/server/internal/connector"
	"github.com/nwverse/persona/server/internal/subscriptions"
)

// sameRows stops the test unless two non-empty lists of Inbox rows are the same field for field, as the Inbox reads
// them (their JSON).
func sameRows(t *testing.T, what string, got, want []connector.InboxThing) {
	t.Helper()
	g, _ := json.Marshal(got)
	w, _ := json.Marshal(want)
	if len(want) == 0 || string(g) != string(w) {
		t.Fatalf("%s:\n got %s\nwant %s", what, g, w)
	}
}

// oneConnector is the subscriptions' registry with only this GitHub in it.
type oneConnector struct{ c *Connector }

func (o oneConnector) Find(_ context.Context, id string) (subscriptions.ConnectorInfo, bool) {
	if id != o.c.ID() {
		return subscriptions.ConnectorInfo{}, false
	}
	return subscriptions.ConnectorInfo{ID: id, Conn: o.c, Enabled: true, Status: connector.StateOK}, true
}

func (o oneConnector) All(ctx context.Context) []subscriptions.ConnectorInfo {
	info, _ := o.Find(ctx, o.c.ID())
	return []subscriptions.ConnectorInfo{info}
}

// GitHub's Inbox feed switched to its default subscription hands the Inbox, under GitHub's own id, exactly the rows its
// Collect does (#6404, G1 of the migration): the same fixtures, through the real subscription service and the generic
// inbox_things (#6381), which answers GitHub's Collect. Collect has no side effects: read again, before and after the
// subscription read it, it has the same rows.
func TestTheSwitchedFeedHandsCollectsRows(t *testing.T) {
	ctx := context.Background()
	c := notificationsFixture().server(t)
	if _, _, err := c.poll(ctx); err != nil {
		t.Fatal(err)
	}
	want, _ := c.Collect(ctx)
	again, _ := c.Collect(ctx)
	sameRows(t, "Collect read again", again, want)
	svc := subscriptions.New(subscriptions.Options{Store: subscriptions.NewMemStore(), Connectors: oneConnector{c},
		Location: func() *time.Location { return time.UTC }})
	recipe := subscriptions.Recipe{Mode: wire.ConnectorSubscriptionModeThings, Tool: subscriptions.InboxThingsTool, ItemsPath: "$.things",
		Kind: wire.ConnectorSubscriptionKindSubNotice, GroupBy: wire.ConnectorSubscriptionGroupThread}
	if _, err := svc.Migrate(ctx, subscriptions.Migration{Connector: c.ID(), Source: c.ID(), Label: "GitHub", Recipe: recipe}, true,
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
		if s.ID != c.ID() {
			continue
		}
		for _, th := range s.Things {
			if th.Kind != subscriptions.KindFailing {
				got = append(got, th)
			}
		}
	}
	sameRows(t, "the switched feed", got, want)
	after, _ := c.Collect(ctx)
	sameRows(t, "Collect after the subscription read it", after, want)
}
