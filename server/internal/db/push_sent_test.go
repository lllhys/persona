package db

import (
	"context"
	"fmt"
	"slices"
	"testing"
	"time"
)

func sentKeys(rows []PushSent) []string {
	out := make([]string, len(rows))
	for i, e := range rows {
		out[i] = e.RelatedKey
	}
	return out
}

// Rows are appended in order (a batch in one statement), read back newest limit oldest first after a time, and swept
// by age and by count.
func TestPushSentAppendReadPrune(t *testing.T) {
	d, ctx := open(t), context.Background()
	now := time.Now().UTC().Truncate(time.Microsecond)
	var batch []PushSent
	for i := range 5 {
		batch = append(batch, PushSent{ID: fmt.Sprintf("push-%d", i), At: now.Add(time.Duration(i-5) * time.Hour), Level: "p1", RelatedKey: fmt.Sprintf("k-%d", i), Manager: "manager-a"})
	}
	if err := d.AddPushSent(ctx, batch); err != nil {
		t.Fatal(err)
	}
	// An older push appended later still reads after the earlier rows: the record keeps the order pushes were added.
	if err := d.AddPushSent(ctx, []PushSent{{ID: "push-p0", At: now.Add(-10 * time.Hour), Level: "p0", Manager: ""}}); err != nil {
		t.Fatal(err)
	}
	if err := d.AddPushSent(ctx, nil); err != nil {
		t.Fatal(err)
	}
	all, err := d.PushSentAfter(ctx, now.Add(-24*time.Hour), 100)
	if err != nil {
		t.Fatal(err)
	}
	if got := sentKeys(all); !slices.Equal(got, []string{"k-0", "k-1", "k-2", "k-3", "k-4", ""}) {
		t.Fatalf("rows = %q, want the order they were added in", got)
	}
	if !all[0].At.Equal(batch[0].At) || all[0].At.Location() != time.UTC || all[0].Level != "p1" || all[0].Manager != "manager-a" || all[5].Level != "p0" {
		t.Fatalf("row fields = %+v / %+v", all[0], all[5])
	}
	newest, err := d.PushSentAfter(ctx, now.Add(-24*time.Hour), 2)
	if err != nil || !slices.Equal(sentKeys(newest), []string{"k-4", ""}) {
		t.Fatalf("newest two = %q (%v), want the last two added, oldest first", sentKeys(newest), err)
	}
	// after is exclusive, as the hub's memory keeps only entries after its cutoff.
	after, err := d.PushSentAfter(ctx, batch[2].At, 100)
	if err != nil || !slices.Equal(sentKeys(after), []string{"k-3", "k-4"}) {
		t.Fatalf("after k-2 = %q (%v)", sentKeys(after), err)
	}

	// By age: at or before the cutoff goes (k-0 at now-5h, the p0 at now-10h).
	if n, err := d.PrunePushSent(ctx, batch[0].At, 100); err != nil || n != 2 {
		t.Fatalf("age sweep removed %d (%v), want 2", n, err)
	}
	// By count: only the newest two stay.
	if n, err := d.PrunePushSent(ctx, now.Add(-24*time.Hour), 2); err != nil || n != 2 {
		t.Fatalf("count sweep removed %d (%v), want 2", n, err)
	}
	left, err := d.PushSentAfter(ctx, time.Time{}, 100)
	if err != nil || !slices.Equal(sentKeys(left), []string{"k-3", "k-4"}) {
		t.Fatalf("after the sweeps = %q (%v), want k-3 k-4", sentKeys(left), err)
	}
	if n, err := d.PrunePushSent(ctx, now.Add(-24*time.Hour), 2); err != nil || n != 0 {
		t.Fatalf("a sweep within the limits removed %d (%v)", n, err)
	}
}

// Upgrade: the old whole-list setting's entries are added to the table after its rows, in one transaction, and the
// setting stays as it is for a build from before the table; a second call adds nothing; a value that is not a list
// stays where it is and adds nothing. On the hub's own database (hub_settings) and on one shared with a manager
// (settings).
func TestAdoptPushSentSetting(t *testing.T) {
	for _, tc := range []struct {
		name string
		open func(t *testing.T) *DB
	}{
		{"shared", func(t *testing.T) *DB { return open(t) }},
		{"hub", func(t *testing.T) *DB {
			d, err := Open(context.Background(), migratedDatabase(t, HubSet), HubSet)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(d.Close)
			return d
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d, ctx := tc.open(t), context.Background()
			const key = "hub.push_sent"
			now := time.Now().UTC().Truncate(time.Microsecond)
			if err := d.AddPushSent(ctx, []PushSent{{ID: "push-already", At: now.Add(-time.Hour), Level: "p1", RelatedKey: "already", Manager: "manager-a"}}); err != nil {
				t.Fatal(err)
			}
			// The setting as the hub wrote it: its pushSent JSON, related_key left out when empty.
			legacy := []map[string]any{
				{"at": now.Add(-3 * time.Hour), "level": "p0", "related_key": "legacy-1", "manager": "manager-a"},
				{"at": now.Add(-2 * time.Hour), "level": "p2", "manager": ""},
			}
			if err := d.PutSetting(ctx, key, legacy); err != nil {
				t.Fatal(err)
			}
			// behind: the table's own row is not in the setting, which an older build would read.
			if n, behind, err := d.AdoptPushSentSetting(ctx, key, time.Time{}, 2000); err != nil || n != 2 || !behind {
				t.Fatalf("added %d, behind %v (%v), want 2 and behind", n, behind, err)
			}
			var left []PushSent
			if ok, err := d.Setting(ctx, key, &left); err != nil || !ok || !slices.Equal(sentKeys(left), []string{"legacy-1", ""}) {
				t.Fatalf("the setting after the upgrade = %q (%v, %v), want it as the old build wrote it", sentKeys(left), ok, err)
			}
			rows, err := d.PushSentAfter(ctx, time.Time{}, 100)
			if err != nil || !slices.Equal(sentKeys(rows), []string{"already", "legacy-1", ""}) {
				t.Fatalf("rows = %q (%v), want the table's row then the setting's in order", sentKeys(rows), err)
			}
			if !rows[1].At.Equal(now.Add(-3*time.Hour)) || rows[1].Level != "p0" || rows[2].Level != "p2" {
				t.Fatalf("moved rows = %+v", rows[1:])
			}
			if n, behind, err := d.AdoptPushSentSetting(ctx, key, time.Time{}, 2000); err != nil || n != 0 || !behind {
				t.Fatalf("a second start: added %d, behind %v (%v), want nothing added", n, behind, err)
			}
			if err := d.PutSetting(ctx, key, map[string]any{"not": "a list"}); err != nil {
				t.Fatal(err)
			}
			if _, _, err := d.AdoptPushSentSetting(ctx, key, time.Time{}, 2000); err == nil {
				t.Fatal("a value that is not a list was taken")
			}
			var kept map[string]any
			if ok, err := d.Setting(ctx, key, &kept); err != nil || !ok {
				t.Fatalf("the unreadable setting was dropped (%v, %v)", ok, err)
			}
			if rows, _ := d.PushSentAfter(ctx, time.Time{}, 100); len(rows) != 3 {
				t.Fatalf("a failed move changed the table: %d rows", len(rows))
			}
		})
	}
}

// A push is one row however often it is written: the batch retried after a write that committed but reported an error
// adds nothing for the rows already there, a new entry in the same retry still lands, and an entry without an ID is
// refused rather than written without the guard.
func TestPushSentRetriedBatchAddsOnce(t *testing.T) {
	d, ctx := open(t), context.Background()
	now := time.Now().UTC().Truncate(time.Microsecond)
	batch := []PushSent{
		{ID: "push-a", At: now.Add(-2 * time.Minute), Level: "p1", RelatedKey: "a", Manager: "manager-a"},
		{ID: "push-b", At: now.Add(-time.Minute), Level: "p0", RelatedKey: "b", Manager: "manager-a"},
	}
	if err := d.AddPushSent(ctx, batch); err != nil {
		t.Fatal(err)
	}
	retry := append(slices.Clone(batch), PushSent{ID: "push-c", At: now, Level: "p1", RelatedKey: "c", Manager: "manager-b"})
	if err := d.AddPushSent(ctx, retry); err != nil {
		t.Fatal(err)
	}
	if err := d.AddPushSent(ctx, retry); err != nil {
		t.Fatal(err)
	}
	rows, err := d.PushSentAfter(ctx, time.Time{}, 100)
	if err != nil || !slices.Equal(sentKeys(rows), []string{"a", "b", "c"}) {
		t.Fatalf("rows = %q (%v), want each push once, in order", sentKeys(rows), err)
	}
	if rows[0].ID != "push-a" || rows[2].ID != "push-c" {
		t.Fatalf("ids read back = %q, %q", rows[0].ID, rows[2].ID)
	}
	if err := d.AddPushSent(ctx, []PushSent{{At: now, Level: "p1"}}); err == nil {
		t.Fatal("an entry without an id was written")
	}
	if rows, _ := d.PushSentAfter(ctx, time.Time{}, 100); len(rows) != 3 {
		t.Fatalf("the refused entry changed the table: %d rows", len(rows))
	}
}

// The upgrade import adds each of the setting's entries once: run again over the same entries (a build from before
// switched back to that wrote the setting anew) it adds only the ones not imported yet, and two equal entries in one
// list stay two rows.
func TestAdoptPushSentSettingTwiceAddsOnce(t *testing.T) {
	d, ctx := open(t), context.Background()
	const key = "hub.push_sent"
	now := time.Now().UTC().Truncate(time.Microsecond)
	twin := map[string]any{"at": now.Add(-2 * time.Hour), "level": "p2", "related_key": "twin", "manager": ""}
	legacy := []map[string]any{
		{"at": now.Add(-3 * time.Hour), "level": "p0", "related_key": "legacy-1", "manager": "manager-a"},
		twin, twin,
	}
	if err := d.PutSetting(ctx, key, legacy); err != nil {
		t.Fatal(err)
	}
	if n, behind, err := d.AdoptPushSentSetting(ctx, key, time.Time{}, 2000); err != nil || n != 3 || behind {
		t.Fatalf("first import added %d, behind %v (%v), want 3 and the setting complete", n, behind, err)
	}
	legacy = append(legacy, map[string]any{"at": now.Add(-time.Hour), "level": "p1", "related_key": "legacy-new", "manager": "manager-b"})
	if err := d.PutSetting(ctx, key, legacy); err != nil {
		t.Fatal(err)
	}
	if n, behind, err := d.AdoptPushSentSetting(ctx, key, time.Time{}, 2000); err != nil || n != 1 || behind {
		t.Fatalf("second import added %d, behind %v (%v), want only the new entry", n, behind, err)
	}
	rows, err := d.PushSentAfter(ctx, time.Time{}, 100)
	if err != nil || !slices.Equal(sentKeys(rows), []string{"legacy-1", "twin", "twin", "legacy-new"}) {
		t.Fatalf("rows = %q (%v), want one row per entry", sentKeys(rows), err)
	}
	if rows[1].ID == rows[2].ID {
		t.Fatalf("the two equal entries share id %q", rows[1].ID)
	}
}

// The setting and the table hold the same pushes under different names: the hub writes its rows back into the setting
// without their IDs (an older build reads it), with the time as it had it in memory, finer than the table keeps. A
// start after that adds only what an older build delivered meanwhile, and two equal entries are two pushes.
func TestMergePushSentSetting(t *testing.T) {
	at := time.Date(2026, 11, 15, 10, 0, 0, 123456789, time.UTC)
	row := func(id string, at time.Time, key string) PushSent {
		return PushSent{ID: id, At: at, Level: "p1", RelatedKey: key, Manager: "manager-a"}
	}
	table := []PushSent{
		row("push-1", at.Truncate(time.Microsecond), "a"), // as Postgres read it back
		row("push-2", at, "twin"),
		row("push-3", at, "twin"),
	}
	setting := []PushSent{row("", at, "a"), row("", at, "twin"), row("", at, "twin")}
	if missing, behind := MergePushSentSetting(setting, table, time.Time{}, 2000); len(missing) != 0 || behind {
		t.Fatalf("the hub's own rows written back: missing %+v, behind %v, want none", missing, behind)
	}
	older := append(slices.Clone(setting), row("", at, "twin"), row("", at.Add(time.Minute), "old-build"))
	missing, behind := MergePushSentSetting(older, table, time.Time{}, 2000)
	if behind || !slices.Equal(sentKeys(missing), []string{"twin", "old-build"}) {
		t.Fatalf("after an older build's pushes: missing %q, behind %v, want its two", sentKeys(missing), behind)
	}
	if missing[0].ID == "" || missing[0].ID == missing[1].ID {
		t.Fatalf("ids of the added entries = %q, %q, want one each", missing[0].ID, missing[1].ID)
	}
	if missing, behind := MergePushSentSetting(setting[:1], table, time.Time{}, 2000); len(missing) != 0 || !behind {
		t.Fatalf("a setting short of the table: missing %+v, behind %v, want behind", missing, behind)
	}
	if missing, behind := MergePushSentSetting(nil, nil, time.Time{}, 2000); len(missing) != 0 || behind {
		t.Fatalf("nothing anywhere: missing %+v, behind %v", missing, behind)
	}
	// What the sweep would take is not added: an entry past the age limit, and with the table at its count limit one
	// older than the rows it keeps (a row swept since the setting was written). A row past the age limit that the
	// setting lacks does not make the setting due a write either.
	aged := []PushSent{row("", at.Add(-time.Hour), "expired"), row("", at, "a")}
	if missing, behind := MergePushSentSetting(aged, table[:1], at.Add(-time.Minute), 2000); len(missing) != 0 || behind {
		t.Fatalf("an expired entry: missing %+v, behind %v, want none", missing, behind)
	}
	if missing, behind := MergePushSentSetting(nil, []PushSent{row("push-0", at.Add(-time.Hour), "expired")}, at.Add(-time.Minute), 2000); len(missing) != 0 || behind {
		t.Fatalf("an expired row: missing %+v, behind %v, want none", missing, behind)
	}
	full := []PushSent{row("push-1", at, "kept-1"), row("push-2", at.Add(time.Second), "kept-2")}
	stale := []PushSent{row("", at.Add(-time.Second), "swept"), row("", at, "kept-1"), row("", at.Add(time.Second), "kept-2"), row("", at.Add(time.Minute), "old-build")}
	if missing, _ := MergePushSentSetting(stale, full, time.Time{}, 2); !slices.Equal(sentKeys(missing), []string{"old-build"}) {
		t.Fatalf("a full table: missing %q, want only the entry later than its rows", sentKeys(missing))
	}
}

// The same on Postgres, through a switch back and forward: rows appended with their own IDs, written back to the
// setting by the hub (finer times, no IDs), an older build's pushes added to the list; the next start adds only those.
// With no setting at all, the table is ahead of it as soon as it has a row.
func TestAdoptPushSentSettingAfterSwitchBack(t *testing.T) {
	d, ctx := open(t), context.Background()
	const key = "hub.push_sent"
	if n, behind, err := d.AdoptPushSentSetting(ctx, key, time.Time{}, 2000); err != nil || n != 0 || behind {
		t.Fatalf("an empty hub: added %d, behind %v (%v)", n, behind, err)
	}
	at := time.Now().UTC().Add(-time.Hour).Truncate(time.Second).Add(123456789 * time.Nanosecond)
	rows := []PushSent{
		{ID: "push-1", At: at, Level: "p1", RelatedKey: "a", Manager: "manager-a"},
		{ID: "push-2", At: at, Level: "p0", RelatedKey: "twin", Manager: ""},
		{ID: "push-3", At: at, Level: "p0", RelatedKey: "twin", Manager: ""},
	}
	if err := d.AddPushSent(ctx, rows); err != nil {
		t.Fatal(err)
	}
	if n, behind, err := d.AdoptPushSentSetting(ctx, key, time.Time{}, 2000); err != nil || n != 0 || !behind {
		t.Fatalf("rows and no setting: added %d, behind %v (%v), want the setting due a write", n, behind, err)
	}
	if err := d.PutSetting(ctx, key, rows); err != nil { // the hub's write-back: PushSent's JSON has no ID
		t.Fatal(err)
	}
	if n, behind, err := d.AdoptPushSentSetting(ctx, key, time.Time{}, 2000); err != nil || n != 0 || behind {
		t.Fatalf("the hub's own write-back: added %d, behind %v (%v), want nothing", n, behind, err)
	}
	older := append(slices.Clone(rows), PushSent{At: at.Add(time.Minute), Level: "p1", RelatedKey: "old-build", Manager: "manager-b"})
	if err := d.PutSetting(ctx, key, older); err != nil {
		t.Fatal(err)
	}
	for i := range 2 { // forward again, then one more start
		want := 1 - i
		if n, behind, err := d.AdoptPushSentSetting(ctx, key, time.Time{}, 2000); err != nil || n != want || behind {
			t.Fatalf("start %d after the older build: added %d, behind %v (%v), want %d", i+1, n, behind, err, want)
		}
	}
	got, err := d.PushSentAfter(ctx, time.Time{}, 100)
	if err != nil || !slices.Equal(sentKeys(got), []string{"a", "twin", "twin", "old-build"}) {
		t.Fatalf("rows = %q (%v), want each push once", sentKeys(got), err)
	}
}
