package db

import (
	"context"
	"testing"
	"time"
)

// TestUrgentCalls: a call is kept, found by id, listed by day and by its due step, and changed only from the version
// that was read, so a phone's report and the manager's loop never both act on one state.
func TestUrgentCalls(t *testing.T) {
	d, ctx := open(t), context.Background()
	now := time.Date(2026, 11, 16, 8, 0, 0, 0, time.UTC)
	timeout := now.Add(time.Minute)
	c := UrgentCall{ID: "call_1", MachineID: "m1", Conversation: "conv", Matter: "ix:1", Title: "t", Why: "rule", RuleID: "money_loss",
		RuleText: "资金损失", Happening: "扣费 2300 元还在涨", InteractionID: "ix_1", State: "ringing", Attempt: 1, CreatedAt: now,
		RangAt: &now, NextAt: &timeout, Step: "ring_timeout"}
	if err := d.InsertUrgentCall(ctx, c); err != nil {
		t.Fatal(err)
	}
	got, ok, err := d.UrgentCall(ctx, "call_1")
	if err != nil || !ok || got.RuleText != "资金损失" || got.Step != "ring_timeout" || got.Version != 0 || !got.NextAt.Equal(timeout) {
		t.Fatalf("UrgentCall = %+v %v %v", got, ok, err)
	}
	if _, ok, err := d.UrgentCall(ctx, "none"); ok || err != nil {
		t.Fatalf("missing call: %v %v", ok, err)
	}
	if list, err := d.UrgentCallsSince(ctx, now.Add(-time.Hour)); err != nil || len(list) != 1 {
		t.Fatalf("since: %v %v", list, err)
	}
	if list, err := d.UrgentCallsSince(ctx, now.Add(time.Second)); err != nil || len(list) != 0 {
		t.Fatalf("since later: %v %v", list, err)
	}
	due, next, err := d.UrgentCallsDue(ctx, now)
	if err != nil || len(due) != 0 || !next.Equal(timeout) {
		t.Fatalf("due now: %v %v %v", due, next, err)
	}
	due, next, err = d.UrgentCallsDue(ctx, timeout)
	if err != nil || len(due) != 1 || !next.IsZero() {
		t.Fatalf("due at timeout: %v %v %v", due, next, err)
	}
	got.State, got.Step, got.NextAt, got.Outcome = "answered", "", nil, ""
	if ok, err := d.UpdateUrgentCall(ctx, got); err != nil || !ok {
		t.Fatalf("update: %v %v", ok, err)
	}
	got.State = "missed" // read before the update above: refused
	if ok, err := d.UpdateUrgentCall(ctx, got); err != nil || ok {
		t.Fatalf("stale update: %v %v", ok, err)
	}
	if after, _, _ := d.UrgentCall(ctx, "call_1"); after.State != "answered" || after.Version != 1 || after.NextAt != nil {
		t.Fatalf("after %+v", after)
	}
}
