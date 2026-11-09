package db

import (
	"context"
	"fmt"
	"maps"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/proto"
)

func TestMachineUsage(t *testing.T) {
	d, ctx := open(t), context.Background()
	now := time.Now().Truncate(time.Hour)
	minute := func(ago time.Duration) int64 { return now.Add(-ago).Unix() }
	b := func(ago time.Duration, model, launcher string, out int64) proto.UsageBucket {
		return proto.UsageBucket{Minute: minute(ago), Model: model, Launcher: launcher, Input: 1, CacheWrite: 2, CacheRead: 3, Output: out, Requests: 1}
	}
	sum := func(rows []UsageRow) (out int64, req int) {
		for _, r := range rows {
			out += r.Output
			req += r.Requests
		}
		return
	}

	// An older node's rows (no launcher), then the new node's whole window: it replaces the window's minutes, so the
	// same replies are not there twice under two launchers.
	if err := d.PutUsage(ctx, "m1", []proto.UsageBucket{b(time.Hour, "opus", "", 10), b(30*time.Hour, "opus", "", 100)}, time.Time{}); err != nil {
		t.Fatal(err)
	}
	if err := d.PutUsage(ctx, "m1", []proto.UsageBucket{b(time.Hour, "opus", "claude", 10), b(time.Hour, "gpt", "codex", 5)},
		now.Add(-26*time.Hour)); err != nil {
		t.Fatal(err)
	}
	// What changed later replaces its minute's total.
	if err := d.PutUsage(ctx, "m1", []proto.UsageBucket{b(time.Hour, "gpt", "codex", 7)}, time.Time{}); err != nil {
		t.Fatal(err)
	}
	if err := d.PutUsage(ctx, "m2", []proto.UsageBucket{b(time.Hour, "opus", "claude", 1000)}, time.Time{}); err != nil {
		t.Fatal(err)
	}
	rows, err := d.UsageSeries(ctx, "m1", now.Add(-2*time.Hour), now, 60)
	if err != nil {
		t.Fatal(err)
	}
	if out, req := sum(rows); len(rows) != 2 || out != 17 || req != 2 {
		t.Fatalf("m1's last 2 hours = %+v", rows)
	}
	// The minute before the window (30 h ago) is kept: the node no longer has it.
	if rows, _ := d.UsageSeries(ctx, "m1", now.Add(-31*time.Hour), now.Add(-29*time.Hour), 60); len(rows) != 1 || rows[0].Output != 100 {
		t.Fatalf("before the window = %+v", rows)
	}

	// Rolled up into ten minutes past 27 hours: the 30-hour-old minute moves, the sums stay; a minute sent again
	// for a rolled-up time adds to its row.
	if n, err := d.RollupUsage(ctx, now.Add(-27*time.Hour)); err != nil || n != 1 {
		t.Fatalf("rollup = %d, %v", n, err)
	}
	if err := d.PutUsage(ctx, "m1", []proto.UsageBucket{b(30*time.Hour-time.Minute, "opus", "", 1)}, time.Time{}); err != nil {
		t.Fatal(err)
	}
	if _, err := d.RollupUsage(ctx, now.Add(-27*time.Hour)); err != nil {
		t.Fatal(err)
	}
	rows, _ = d.UsageSeries(ctx, "m1", now.Add(-31*time.Hour), now.Add(-29*time.Hour), 600)
	if out, req := sum(rows); len(rows) != 1 || out != 101 || req != 2 {
		t.Fatalf("rolled up = %+v", rows)
	}
	// A week's chart by hour reads both kinds of rows.
	rows, _ = d.UsageSeries(ctx, "m1", now.Add(-7*24*time.Hour), now.Add(time.Minute), 3600)
	if out, _ := sum(rows); out != 118 {
		t.Fatalf("the week = %+v", rows)
	}

	if n, err := d.PruneUsage(ctx, now.Add(-28*time.Hour)); err != nil || n != 1 {
		t.Fatalf("prune = %d, %v", n, err)
	}
	if rows, _ := d.UsageSeries(ctx, "m2", now.Add(-2*time.Hour), now, 60); len(rows) != 1 || rows[0].Output != 1000 {
		t.Fatalf("m2 = %+v", rows)
	}
}

// Several nodes' writes in one transaction (PutUsages) come out as one at a time, in order: a whole window replaces
// the minutes an older write left, what changed after it replaces its minute. A write Postgres rejects costs only
// itself.
func TestPutUsagesOneTransaction(t *testing.T) {
	d, ctx := open(t), context.Background()
	now := time.Now().Truncate(time.Hour)
	b := func(ago time.Duration, model, launcher string, out int64) proto.UsageBucket {
		return proto.UsageBucket{Minute: now.Add(-ago).Unix(), Model: model, Launcher: launcher, Output: out, Requests: 1}
	}
	writes := []UsageWrite{
		{Machine: "m1", Buckets: []proto.UsageBucket{b(time.Hour, "opus", "", 10), b(30*time.Hour, "opus", "", 100)}},
		{Machine: "m2", Buckets: []proto.UsageBucket{b(time.Hour, "opus", "claude", 1000)}},
		{Machine: "m1", Buckets: []proto.UsageBucket{b(time.Hour, "opus", "claude", 10), b(time.Hour, "gpt", "codex", 5)},
			ReplaceFrom: now.Add(-26 * time.Hour)},
		{Machine: "m1", Buckets: []proto.UsageBucket{b(time.Hour, "gpt", "codex", 7)}},
	}
	if rejected, err := d.PutUsages(ctx, writes); err != nil || rejected != nil {
		t.Fatal(rejected, err)
	}
	var rows map[string]int64
	read := func() {
		rows = map[string]int64{}
		for _, m := range []string{"m1", "m2"} {
			list, err := d.UsageSeries(ctx, m, now.Add(-31*time.Hour), now, 60)
			if err != nil {
				t.Fatal(err)
			}
			for _, r := range list {
				rows[fmt.Sprint(m, " ", (r.Minute-now.Unix())/3600, "h ", r.Model, " ", r.Launcher)] += r.Output
			}
		}
	}
	read()
	want := map[string]int64{"m1 -30h opus ": 100, "m1 -1h opus claude": 10, "m1 -1h gpt codex": 7, "m2 -1h opus claude": 1000}
	if !maps.Equal(rows, want) {
		t.Fatalf("rows = %v, want %v", rows, want)
	}
	var xmins int
	if err := d.pool.QueryRow(ctx, `SELECT count(DISTINCT xmin::text) FROM machine_usage`).Scan(&xmins); err != nil || xmins != 1 {
		t.Fatalf("%d transactions (%v), want one", xmins, err)
	}

	bad := proto.UsageBucket{Minute: 1 << 50, Model: "opus", Launcher: "claude", Output: 1, Requests: 1} // no such time
	rejected, err := d.PutUsages(ctx, []UsageWrite{
		{Machine: "m1", Buckets: []proto.UsageBucket{b(time.Hour, "gpt", "codex", 8)}},
		{Machine: "m2", Buckets: []proto.UsageBucket{b(time.Hour, "opus", "claude", 2000), bad}},
		{Machine: "m2", Buckets: []proto.UsageBucket{b(2*time.Hour, "opus", "claude", 3)}},
	})
	if err != nil || len(rejected) != 3 || rejected[0] != nil || rejected[1] == nil || rejected[2] != nil {
		t.Fatal(rejected, err)
	}
	read()
	want["m1 -1h gpt codex"], want["m2 -2h opus claude"] = 8, 3
	if !maps.Equal(rows, want) {
		t.Fatalf("rows = %v, want %v", rows, want)
	}
}
