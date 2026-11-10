package db

import (
	"context"
	"fmt"
	"testing"
	"time"
)

func openSubscriptions(t testing.TB) *DB {
	t.Helper()
	d := open(t)
	if _, err := d.pool.Exec(context.Background(), `TRUNCATE subscriptions CASCADE`); err != nil {
		t.Fatal(err)
	}
	return d
}

// A subscription's revision guards updates, its lease keeps a second run out, and a run's keys, items and data are
// written together; deleting it takes its keys and items with it.
func TestSubscriptionsRevisionLeaseAndRun(t *testing.T) {
	d, ctx := openSubscriptions(t), context.Background()
	now := time.Date(2026, 11, 10, 9, 0, 0, 0, time.UTC)
	row := SubscriptionRow{ID: "sub_a", Connector: "example", Source: "notices", Revision: 1, Data: []byte(`{"label":"Notices"}`),
		CreatedAt: now, UpdatedAt: now}
	if err := d.CreateSubscription(ctx, row); err != nil {
		t.Fatal(err)
	}
	row.Revision, row.Data = 2, []byte(`{"label":"Office"}`)
	if ok, err := d.UpdateSubscription(ctx, row, 1); err != nil || !ok {
		t.Fatalf("update at its revision: %v %v", ok, err)
	}
	if ok, _ := d.UpdateSubscription(ctx, row, 1); ok {
		t.Fatal("an update at a stale revision was written")
	}
	if ok, _ := d.LeaseSubscription(ctx, "sub_a", now, now.Add(5*time.Minute)); !ok {
		t.Fatal("no lease")
	}
	if ok, _ := d.LeaseSubscription(ctx, "sub_a", now.Add(time.Minute), now.Add(6*time.Minute)); ok {
		t.Fatal("a second lease while the first holds")
	}
	merge := func(data []byte, revision int64) ([]byte, error) {
		if revision != 2 {
			return nil, fmt.Errorf("revision %d", revision)
		}
		return []byte(`{"label":"Office","since":"x"}`), nil
	}
	seen := []SubscriptionSeenRow{{UID: "n1", ContentHash: "h1"}, {UID: "n2", ContentHash: "h2"}}
	items := []SubscriptionItemRow{{UID: "n1", Data: []byte(`{"title":"One"}`)}, {UID: "n2", Data: []byte(`{"title":"Two"}`)}}
	if err := d.CommitSubscriptionRun(ctx, "sub_a", merge, seen, items, now); err != nil {
		t.Fatal(err)
	}
	got, err := d.Subscription(ctx, "sub_a")
	if err != nil || got == nil || got.LeaseUntil != nil || string(got.Data) != `{"label":"Office","since":"x"}` {
		t.Fatalf("after the run: %+v %v", got, err)
	}
	known, _ := d.SubscriptionSeen(ctx, "sub_a", []string{"n1", "n2", "n3"})
	if !known["n1"] || !known["n2"] || known["n3"] {
		t.Fatalf("seen: %v", known)
	}
	// The same keys again add nothing; the items are kept in order.
	if err := d.CommitSubscriptionRun(ctx, "sub_a", merge, seen[:1], nil, now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	seqs, kept, err := d.SubscriptionItems(ctx, "sub_a", 0, 10)
	if err != nil || len(kept) != 2 || kept[0].UID != "n1" || seqs[0] >= seqs[1] {
		t.Fatalf("items: %v %+v %v", seqs, kept, err)
	}
	// A run that fails to merge keeps nothing.
	bad := func([]byte, int64) ([]byte, error) { return nil, fmt.Errorf("no") }
	if err := d.CommitSubscriptionRun(ctx, "sub_a", bad, []SubscriptionSeenRow{{UID: "n9", ContentHash: "h"}}, nil, now); err == nil {
		t.Fatal("a failed merge committed")
	}
	if known, _ := d.SubscriptionSeen(ctx, "sub_a", []string{"n9"}); known["n9"] {
		t.Fatal("a failed run kept its keys")
	}
	if err := d.DeleteSubscription(ctx, "sub_a"); err != nil {
		t.Fatal(err)
	}
	if _, kept, _ := d.SubscriptionItems(ctx, "sub_a", 0, 10); len(kept) != 0 {
		t.Fatal("deleting kept the items")
	}
	if known, _ := d.SubscriptionSeen(ctx, "sub_a", []string{"n1"}); known["n1"] {
		t.Fatal("deleting kept the keys")
	}
	// A run of a deleted subscription keeps nothing and is no error.
	if err := d.CommitSubscriptionRun(ctx, "sub_a", merge, seen, items, now); err != nil {
		t.Fatal(err)
	}
}

// Old keys are dropped: past 400 days, and beyond the newest 5000.
func TestSubscriptionSeenIsPruned(t *testing.T) {
	d, ctx := openSubscriptions(t), context.Background()
	now := time.Date(2026, 11, 10, 9, 0, 0, 0, time.UTC)
	if err := d.CreateSubscription(ctx, SubscriptionRow{ID: "sub_b", Connector: "example", Source: "notices", Revision: 1,
		Data: []byte(`{}`), CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	keep := func(data []byte, _ int64) ([]byte, error) { return data, nil }
	if err := d.CommitSubscriptionRun(ctx, "sub_b", keep, []SubscriptionSeenRow{{UID: "old", ContentHash: "h"}}, nil,
		now.Add(-401*24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	var batch []SubscriptionSeenRow
	for i := range subscriptionSeenKeep + 10 {
		batch = append(batch, SubscriptionSeenRow{UID: fmt.Sprintf("k%05d", i), ContentHash: "h"})
	}
	if err := d.CommitSubscriptionRun(ctx, "sub_b", keep, batch, nil, now); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := d.pool.QueryRow(ctx, `SELECT count(*) FROM subscription_seen WHERE subscription_id = 'sub_b'`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != subscriptionSeenKeep {
		t.Fatalf("kept %d keys, want %d", n, subscriptionSeenKeep)
	}
	if known, _ := d.SubscriptionSeen(ctx, "sub_b", []string{"old"}); known["old"] {
		t.Fatal("a key older than 400 days was kept")
	}
}
