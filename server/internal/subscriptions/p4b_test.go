package subscriptions

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/nwverse/persona/sdk/go/wire"
	"github.com/nwverse/persona/server/internal/connector"
)

// collectConn is the env's connector with an Inbox whose Collect has rows, and no inbox_things of its own.
type collectConn struct {
	*fakeConn
	rows []connector.InboxThing
}

func (c *collectConn) Manifest() connector.Manifest {
	return connector.Manifest{Capabilities: []connector.Capability{connector.CapCatalog, connector.CapInbox}}
}
func (c *collectConn) Collect(context.Context) ([]connector.InboxThing, error) { return c.rows, nil }
func (c *collectConn) Reply(context.Context, connector.InboxReply) error       { return nil }

func genericRecipe() Recipe {
	return Recipe{Mode: wire.ConnectorSubscriptionModeThings, Tool: InboxThingsTool, ItemsPath: "$.things", Kind: "sub_message", GroupBy: "thread"}
}

// The generic inbox_things (#6381) is the connector's Collect: a built-in's default subscription reading it hands the
// Inbox the very rows the built-in's round gets. People neither see it nor make a subscription with it, and the
// default subscription's recipe is not changed.
func TestTheGenericInboxThingsIsTheConnectorsCollect(t *testing.T) {
	e := newEnv(t)
	e.conns.infos["example"].Conn = &collectConn{fakeConn: e.conn, rows: connectorRows(false)}
	cat, err := e.svc.Catalog(e.ctx, "example")
	if err != nil || len(cat.Sources) != 1 || cat.Sources[0].ID != "notices" {
		t.Fatalf("people's catalog: %+v %v", cat.Sources, err)
	}
	for _, tool := range cat.Sources[0].Tools {
		if tool.Name == InboxThingsTool {
			t.Fatal("people are offered inbox_things")
		}
	}
	r := genericRecipe()
	if _, err := e.svc.Create(e.ctx, CreateRequest{Connector: "example", Source: "example", Recipe: &r}, Actor{ID: "device:phone"}); err == nil {
		t.Fatal("a subscription made with inbox_things")
	}
	sub, err := e.svc.Migrate(e.ctx, Migration{Connector: "example", Source: "example", Label: "Example", Recipe: r}, true, Actor{})
	if err != nil {
		t.Fatal(err)
	}
	e.svc.RunDue(e.ctx)
	sameRows(t, e.connectorsRows("example"), connectorRows(false))
	if _, err := e.svc.Update(e.ctx, sub.ID, Patch{Revision: e.get(sub.ID).Revision, Recipe: ptr(thingsRecipe())}, Actor{}); err == nil {
		t.Fatal("a default subscription's recipe changed")
	}
}

// A switched built-in's default subscription runs 3 seconds after its connector's changed event, once a burst, as the
// Inbox's own kick does (#6381): outside the subscriptions' hourly budget and call gap, since it replaces the built-in's
// Collect calls. People's subscriptions keep P4a's minute.
func TestASwitchedSourceRunsSecondsAfterItsConnectorChanged(t *testing.T) {
	e := newEnv(t)
	one := 1
	e.conn.sources[0].MaxCallsPerHour = &one // the subscriptions' budget: one call an hour, which people's subscription takes
	var both map[string]any                  // people's items and the switched source's rows, from the one tool
	_ = json.Unmarshal([]byte(notices(1, 2, 3)), &both)
	both["things"] = connectorRows(false)
	raw, _ := json.Marshal(both)
	e.answer(string(raw))
	people := e.turnOn(e.create(itemsRecipe()))
	if _, err := e.svc.Migrate(e.ctx, Migration{Connector: "example", Source: "notices", Label: "Example", Recipe: thingsRecipe()}, true, Actor{}); err != nil {
		t.Fatal(err)
	}
	e.svc.RunDue(e.ctx)
	if _, runs, _ := e.svc.Get(e.ctx, people.ID); len(runs) == 0 {
		t.Fatal("people's subscription did not run")
	}
	calls := e.conn.callCount()
	e.clock.advance(5 * time.Second)
	e.svc.ConnectorChanged(e.ctx, "example")
	e.svc.RunDue(e.ctx)
	if got := e.conn.callCount() - calls; got != 0 {
		t.Fatalf("ran before the 3 seconds: %d calls", got)
	}
	e.clock.advance(changedDebounce)
	before := e.clock.Now()
	e.svc.RunDue(e.ctx)
	if got := e.conn.callCount() - calls; got != 1 || e.clock.Now().Sub(before) >= callGap {
		t.Fatalf("3 seconds after: %d calls (want the switched one only), waited %v", got, e.clock.Now().Sub(before))
	}
	for range 3 { // a burst
		e.svc.ConnectorChanged(e.ctx, "example")
		e.clock.advance(time.Second)
	}
	e.clock.advance(changedDebounce)
	e.svc.RunDue(e.ctx)
	if got := e.conn.callCount() - calls; got != 2 {
		t.Fatalf("after a burst: %d calls, want 2 (one more)", got)
	}
}
