package subscriptions

import (
	"testing"
	"time"
)

// A connector's changed event runs its subscriptions that are on now, not at their next schedule (#6247), as a scheduled
// run brought forward; again only a minute after the last run; an off subscription and another connector's are left.
func TestAChangedConnectorRunsItsSubscriptionsNow(t *testing.T) {
	e := newEnv(t)
	sub := e.turnOn(e.create(itemsRecipe()))
	e.create(itemsRecipe()) // off
	e.svc.RunDue(e.ctx)
	calls := e.conn.callCount()
	e.clock.advance(2 * time.Minute) // its next run is 15 minutes after the last
	e.svc.RunDue(e.ctx)
	if got := e.conn.callCount() - calls; got != 0 {
		t.Fatalf("ran before its schedule: %d calls", got)
	}
	e.svc.ConnectorChanged(e.ctx, "example")
	e.svc.RunDue(e.ctx)
	if got := e.conn.callCount() - calls; got != 1 {
		t.Fatalf("calls after the connector changed: %d, want 1 (only the one that is on)", got)
	}
	if _, runs, _ := e.svc.Get(e.ctx, sub.ID); len(runs) != 2 || runs[0].Trigger != TriggerSchedule {
		t.Fatalf("runs: %+v", runs)
	}
	e.clock.advance(30 * time.Second)
	e.svc.ConnectorChanged(e.ctx, "example")
	e.svc.RunDue(e.ctx)
	if got := e.conn.callCount() - calls; got != 1 {
		t.Fatalf("ran again within a minute: %d calls", got)
	}
	e.clock.advance(2 * time.Minute)
	e.svc.ConnectorChanged(e.ctx, "elsewhere")
	e.svc.RunDue(e.ctx)
	if got := e.conn.callCount() - calls; got != 1 {
		t.Fatalf("another connector's change ran it: %d calls", got)
	}
	e.svc.ConnectorChanged(e.ctx, "example")
	e.svc.RunDue(e.ctx)
	if got := e.conn.callCount() - calls; got != 2 {
		t.Fatalf("a change a minute later: %d calls, want 2", got)
	}
}

// A source's own minimum holds a changed event's run back as it holds the schedule (#6247).
func TestAChangedConnectorKeepsTheSourcesMinimum(t *testing.T) {
	e := newEnv(t)
	ten := 600
	e.conn.sources[0].MinIntervalSeconds = &ten
	e.turnOn(e.create(itemsRecipe()))
	e.svc.RunDue(e.ctx)
	calls := e.conn.callCount()
	e.clock.advance(5 * time.Minute)
	e.svc.ConnectorChanged(e.ctx, "example")
	e.svc.RunDue(e.ctx)
	if got := e.conn.callCount() - calls; got != 0 {
		t.Fatalf("ran within the source's ten minutes: %d calls", got)
	}
	e.clock.advance(6 * time.Minute)
	e.svc.ConnectorChanged(e.ctx, "example")
	e.svc.RunDue(e.ctx)
	if got := e.conn.callCount() - calls; got != 1 {
		t.Fatalf("after the source's ten minutes: %d calls, want 1", got)
	}
}
