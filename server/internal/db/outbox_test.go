package db

import (
	"context"
	"slices"
	"testing"
	"time"
)

// The outbox keeps items in order, each id once, byte for byte; each kind is bounded on its own, and stale live
// updates go when the next item is kept.
func TestOutbox(t *testing.T) {
	ctx := context.Background()
	d, err := Open(ctx, migratedDatabase(t, ManagerSet), ManagerSet)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	now := time.Now()
	add := func(id, kind, body string, at time.Time) {
		t.Helper()
		if err := d.OutboxAdd(ctx, OutboxItem{ID: id, Kind: kind, Body: []byte(body), CreatedAt: at}, 2, now.Add(-10*time.Minute)); err != nil {
			t.Fatal(err)
		}
	}
	ids := func() []string {
		t.Helper()
		items, err := d.OutboxItems(ctx, 10)
		if err != nil {
			t.Fatal(err)
		}
		var out []string
		for _, it := range items {
			out = append(out, it.ID)
		}
		return out
	}
	dup := `{"type":"todos","a":1,"a":2}` // kept as it was (jsonb would fold it)
	add("bc_old", "broadcast", `{}`, now.Add(-time.Hour))
	add("ntf_1", "push", `{"id":"ntf_1"}`, now)
	if got := ids(); !slices.Equal(got, []string{"ntf_1"}) {
		t.Fatalf("a stale live update kept: %v", got)
	}
	add("bc_1", "broadcast", dup, now)
	add("ntf_1", "push", `{"id":"again"}`, now) // the same id: not twice
	add("bc_2", "broadcast", `{}`, now)
	add("bc_3", "broadcast", `{}`, now)
	if got := ids(); !slices.Equal(got, []string{"ntf_1", "bc_2", "bc_3"}) {
		t.Fatalf("kept %v: each kind bounded on its own, the oldest first", got)
	}
	add("bc_4", "broadcast", dup, now)
	items, _ := d.OutboxItems(ctx, 10)
	if string(items[0].Body) != `{"id":"ntf_1"}` || string(items[len(items)-1].Body) != dup {
		t.Fatalf("bodies %q / %q", items[0].Body, items[len(items)-1].Body)
	}
	if n, err := d.OutboxCount(ctx); err != nil || n != 3 {
		t.Fatalf("count %d %v", n, err)
	}
	if err := d.OutboxDone(ctx, []string{"ntf_1", "bc_3"}); err != nil {
		t.Fatal(err)
	}
	if got := ids(); !slices.Equal(got, []string{"bc_4"}) {
		t.Fatalf("after done: %v", got)
	}
}

// What managers handed the hub is recorded per manager, once, forgotten on request and after its time.
func TestHandOver(t *testing.T) {
	ctx := context.Background()
	d, err := Open(ctx, migratedDatabase(t, HubSet), HubSet)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	first := func(manager, id string) bool {
		t.Helper()
		ok, err := d.HandOverFirst(ctx, manager, id, time.Hour)
		if err != nil {
			t.Fatal(err)
		}
		return ok
	}
	if !first("assistant", "ntf_1") || first("assistant", "ntf_1") {
		t.Fatal("ntf_1 twice")
	}
	if !first("desk", "ntf_1") {
		t.Fatal("another manager's ntf_1 taken for the assistant's")
	}
	if err := d.HandOverForget(ctx, "assistant", "ntf_1"); err != nil {
		t.Fatal(err)
	}
	if !first("assistant", "ntf_1") {
		t.Fatal("not forgotten")
	}
	if _, err := d.pool.Exec(ctx, `UPDATE handed_over SET at = now() - interval '2 hours'`); err != nil {
		t.Fatal(err)
	}
	if !first("desk", "ntf_1") {
		t.Fatal("not forgotten after its time")
	}
}
