package subscriptions

import (
	"strings"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/connector"
	"github.com/nwverse/persona/server/internal/i18n"
)

// The Inbox's rows follow the recipe's group_by (#5671), as the knowledge base's chats do: a subscription grouped by
// source is one row, sub:<subscription>:all; one grouped by thread a row a thread (an item without one its own). Rows
// made before the grouping changed stay as they came in.

// sourceRecipe is threadRecipe grouped by source: its thread field is mapped, and the Inbox and the knowledge base
// leave it be.
func sourceRecipe() Recipe {
	r := threadRecipe()
	r.GroupBy = "source"
	return r
}

// kept is subscription id as the store holds it, the marks the API does not show included.
func (e *env) kept(id string) Subscription {
	e.t.Helper()
	e.store.mu.Lock()
	defer e.store.mu.Unlock()
	return e.store.subs[id]
}

// fromBefore makes id's rows ones a build from before #5671 made: no grouping kept.
func (e *env) fromBefore(id string) {
	e.store.mu.Lock()
	defer e.store.mu.Unlock()
	s := e.store.subs[id]
	s.InboxGroup, s.InboxGroupFrom = "", nil
	e.store.subs[id] = s
}

func countOf(t connector.InboxThing) int {
	if t.Extra == nil {
		return 0
	}
	return t.Extra.Count
}

func TestASourceGroupedSubscriptionIsOneRow(t *testing.T) {
	e := newEnv(t)
	e.answer(notesJSON(
		note{id: "n1", thread: "t1", title: "Office hours moved", at: "2026-11-10T08:00:00Z"},
		note{id: "n2", thread: "t2", title: "Room booked", at: "2026-11-10T08:30:00Z", link: "https://example.com/n2"},
		note{id: "n3", title: "Form due", at: "2026-11-10T07:00:00Z", due: "2026-11-12T18:00:00Z"},
	))
	sub := e.turnOn(e.create(sourceRecipe()))
	e.svc.RunDue(e.ctx)
	rows := e.rows(sub.ID)
	if len(rows) != 1 {
		t.Fatalf("three caught-up items of a subscription grouped by source: %d rows, want one: %+v", len(rows), rows)
	}
	r := rows[0]
	// Titled by the subscription, as a group chat's row, with no tag saying it again; the rest is its newest item.
	if r.Thread != "sub:"+sub.ID+":all" || r.Title != "Notices" || r.Label != "" || r.Sender != "Office" || r.Last != "n2" ||
		r.Preview != "Made-up notice text" || !r.At.Equal(time.Date(2026, 11, 10, 8, 30, 0, 0, time.UTC)) || r.URL != "https://example.com/n2" {
		t.Fatalf("the row is its newest item: %+v", r)
	}
	if !strings.HasPrefix(r.Context, "11-10 07:00 · Office · Form due\n11-10 08:00 · Office · Office hours moved\n\nRoom booked\n") {
		t.Fatalf("the earlier items above the newest:\n%s", r.Context)
	}
	if countOf(r) != 3 || ruleTier(r) != "action" {
		t.Fatalf("the catch-up came in at once (3), and one item is due (Needs you): count %d, tier %q", countOf(r), ruleTier(r))
	}
	if k := e.kept(sub.ID); k.InboxGroup != "source" || k.InboxGroupFrom != nil {
		t.Fatalf("the catch-up starts the rows with the recipe's grouping: %q %v", k.InboxGroup, k.InboxGroupFrom)
	}
	// A later pull of two moves the same row by its count; the rows of the earlier thread keys never come.
	e.answer(notesJSON(
		note{id: "n4", thread: "t1", title: "Office hours moved back", at: "2026-11-10T09:10:00Z"},
		note{id: "n5", title: "Lunch menu", at: "2026-11-10T09:15:00Z"},
	))
	e.clock.advance(20 * time.Minute)
	e.svc.RunDue(e.ctx)
	rows = e.rows(sub.ID)
	if len(rows) != 1 || rows[0].Thread != "sub:"+sub.ID+":all" || rows[0].Last != "n5" || countOf(rows[0]) != 2 {
		t.Fatalf("after a pull of two: %+v", rows)
	}
	// One more item: one more.
	e.answer(notesJSON(note{id: "n6", title: "Lunch moved", at: "2026-11-10T09:30:00Z"}))
	e.clock.advance(20 * time.Minute)
	e.svc.RunDue(e.ctx)
	if rows = e.rows(sub.ID); len(rows) != 1 || rows[0].Last != "n6" || countOf(rows[0]) != 0 || strings.Count(rows[0].Context, "\n") < 5 {
		t.Fatalf("after a pull of one (no count: one is the default): %+v", rows)
	}
}

func TestASourceGroupedCatchUpOfManyIsADigestAndLaterItemsOneRow(t *testing.T) {
	e := newEnv(t)
	e.answer(notesJSON(manyNotes("m", 10)...))
	sub := e.turnOn(e.create(sourceRecipe()))
	e.svc.RunDue(e.ctx)
	rows := e.rows(sub.ID)
	if len(rows) != 1 || rows[0].Thread != "sub:"+sub.ID+":catchup" {
		t.Fatalf("ten caught-up items: %+v", rows)
	}
	e.answer(notesJSON(note{id: "x1", title: "New one", at: "2026-11-10T09:10:00Z"}, note{id: "x2", title: "New two", at: "2026-11-10T09:11:00Z"}))
	e.clock.advance(20 * time.Minute)
	e.svc.RunDue(e.ctx)
	rows = e.rows(sub.ID)
	if len(rows) != 2 {
		t.Fatalf("the digest and the row: %+v", rows)
	}
	r := rowOf(t, rows, "sub:"+sub.ID+":all")
	if r.Last != "x2" || countOf(r) != 2 || strings.Contains(r.Context, "Mail ") {
		t.Fatalf("the row has what came after the digest only: %+v", r)
	}
}

func TestAThreadGroupedItemWithoutAThreadIsARowOfItsOwn(t *testing.T) {
	e := newEnv(t)
	e.answer(notesJSON(
		note{id: "n1", thread: "t1", title: "One", at: "2026-11-10T08:00:00Z"},
		note{id: "n2", thread: "t1", title: "Two", at: "2026-11-10T08:10:00Z"},
		note{id: "n3", title: "Alone", at: "2026-11-10T08:20:00Z"},
	))
	sub := e.turnOn(e.create(threadRecipe()))
	e.svc.RunDue(e.ctx)
	rows := e.rows(sub.ID)
	if len(rows) != 2 || rowOf(t, rows, "sub:"+sub.ID+":t1").Last != "n2" || rowOf(t, rows, "sub:"+sub.ID+":n3").Last != "n3" {
		t.Fatalf("by thread: %+v", rows)
	}
	if k := e.kept(sub.ID); k.InboxGroup != "thread" {
		t.Fatalf("the rows' grouping: %q", k.InboxGroup)
	}
}

// Rows from before #5671 were an item's each, whatever the recipe said. They stay as they came in: the one row starts
// with the first pull after the upgrade, so nothing comes in twice and no burst of rows appears.
func TestRowsFromBeforeTheUpgradeStayAndTheRowStartsWithTheNextPull(t *testing.T) {
	e := newEnv(t)
	e.answer(notesJSON(note{id: "a1", title: "Old one", at: "2026-11-10T08:00:00Z"}, note{id: "a2", title: "Old two", at: "2026-11-10T08:10:00Z"}))
	sub := e.turnOn(e.create(sourceRecipe()))
	e.svc.RunDue(e.ctx)
	e.fromBefore(sub.ID)
	if rows := e.rows(sub.ID); len(rows) != 0 {
		t.Fatalf("before the next pull nothing is regrouped: %+v", rows)
	}
	// A pull that finds nothing new starts the grouping too: still no row.
	e.clock.advance(20 * time.Minute)
	e.svc.RunDue(e.ctx)
	k := e.kept(sub.ID)
	if k.InboxGroup != "source" || k.InboxGroupFrom == nil || !k.InboxGroupFrom.Equal(e.clock.Now()) {
		t.Fatalf("the pull marks the grouping: %q %v", k.InboxGroup, k.InboxGroupFrom)
	}
	if rows := e.rows(sub.ID); len(rows) != 0 {
		t.Fatalf("the items delivered before come in again: %+v", rows)
	}
	e.answer(notesJSON(note{id: "b1", title: "New one", at: "2026-11-10T09:30:00Z"}))
	e.clock.advance(20 * time.Minute)
	e.svc.RunDue(e.ctx)
	rows := e.rows(sub.ID)
	if len(rows) != 1 || rows[0].Thread != "sub:"+sub.ID+":all" || rows[0].Last != "b1" || strings.Contains(rows[0].Context, "Old") {
		t.Fatalf("the row starts with what came after: %+v", rows)
	}
	// A subscription grouped by thread made the same rows before: they go on as they were.
	other := e.turnOn(e.create(threadRecipe()))
	e.svc.RunDue(e.ctx)
	before := e.rows(other.ID)
	e.fromBefore(other.ID)
	if after := e.rows(other.ID); len(after) != len(before) || len(after) == 0 {
		t.Fatalf("thread rows from before: %d, then %d", len(before), len(after))
	}
}

// A recipe changed to another grouping without a new catch-up (existing baseline or deliver) starts it with its next
// pull; with digest (the default) the new catch-up holds nothing delivered before.
func TestAChangedGroupingStartsWithTheNextPull(t *testing.T) {
	for _, existing := range []string{ExistingBaseline, ExistingDeliver, ExistingDigest} {
		t.Run(existing, func(t *testing.T) {
			e := newEnv(t)
			sub := e.started() // two rows by thread
			e.answer(notesJSON(note{id: "a1", title: "Old one", at: "2026-11-10T08:00:00Z"}, note{id: "a2", title: "Old two", at: "2026-11-10T08:10:00Z"},
				note{id: "c1", title: "New one", at: "2026-11-10T09:15:00Z"}))
			r := sourceRecipe()
			r.Args = changedRecipe().Args
			e.patch(sub.ID, Patch{Recipe: &r, Existing: existing})
			if rows := e.rows(sub.ID); len(rows) != 0 {
				t.Fatalf("before the next pull: %+v", rows)
			}
			e.svc.RunDue(e.ctx)
			rows := e.rows(sub.ID)
			switch existing {
			case ExistingBaseline: // what it found is seen, not kept
				if len(rows) != 0 {
					t.Fatalf("a baseline: %+v", rows)
				}
			case ExistingDeliver, ExistingDigest: // the one it had not seen, without the rows delivered before
				if len(rows) != 1 || rows[0].Thread != "sub:"+sub.ID+":all" || rows[0].Last != "c1" || strings.Contains(rows[0].Context, "Old") {
					t.Fatalf("the first pull's items: %+v", rows)
				}
			}
			e.answer(notesJSON(note{id: "d1", title: "Later", at: "2026-11-10T09:40:00Z"}))
			e.clock.advance(20 * time.Minute)
			e.svc.RunDue(e.ctx)
			rows = e.rows(sub.ID)
			if len(rows) != 1 || rows[0].Last != "d1" {
				t.Fatalf("a later item: %+v", rows)
			}
			if strings.Contains(rows[0].Context, "Old") {
				t.Fatalf("the items delivered before the change are in the row: %s", rows[0].Context)
			}
		})
	}
}

// The rows' grouping is kept in the database's document, and a run's commit keeps it only while the recipe still
// groups that way and the Inbox is the one it saw.
func TestTheRowsGroupingIsKept(t *testing.T) {
	from, mark, start := time.Date(2026, 11, 10, 9, 0, 0, 0, time.UTC), time.Date(2026, 11, 10, 9, 5, 0, 0, time.UTC), time.Date(2026, 11, 10, 10, 0, 0, 0, time.UTC)
	sub := Subscription{ID: "sub_a", ToInbox: true, InboxFrom: &from, InboxCatchup: &mark, Recipe: sourceRecipe(), InboxGroup: "source", InboxGroupFrom: &start}
	raw, err := encode(sub)
	if err != nil {
		t.Fatal(err)
	}
	if back, err := decode(raw); err != nil || back.InboxGroup != "source" || !sameTime(back.InboxGroupFrom, &start) {
		t.Fatalf("decoded: %q %v %v", back.InboxGroup, back.InboxGroupFrom, err)
	}
	cur := Subscription{ID: "sub_a", ToInbox: true, InboxFrom: &from, Recipe: threadRecipe()}
	applyRun(&cur, RunCommit{Sub: sub}, true)
	if cur.InboxGroup != "" || cur.InboxGroupFrom != nil {
		t.Fatalf("the recipe was changed to group by thread during the run, and the run's grouping was kept: %q", cur.InboxGroup)
	}
	cur.Recipe = sourceRecipe()
	applyRun(&cur, RunCommit{Sub: sub}, false)
	if cur.InboxGroup != "source" || !sameTime(cur.InboxGroupFrom, &start) {
		t.Fatalf("the run's grouping was not kept: %q %v", cur.InboxGroup, cur.InboxGroupFrom)
	}
}

// A preview hands a row an item, without a count.
func TestAPreviewIsARowAnItem(t *testing.T) {
	e := newEnv(t)
	sub := Subscription{ID: "sub_p", Label: "Notices", Recipe: sourceRecipe()}
	p := e.svc.PreviewThings(sub, []Preview{{UID: "p1", Title: "One"}, {UID: "p2", Title: "Two"}})
	if len(p.Things) != 2 || countOf(p.Things[0]) != 0 || p.Things[0].Thread != "sub:sub_p:p1" {
		t.Fatalf("the preview: %+v", p.Things)
	}
}

// A chat message read as both its title and its body is said once in the row's context.
func TestABodyThatIsTheTitleIsSaidOnce(t *testing.T) {
	d := deliverer{sub: Subscription{ID: "sub_x", Label: "Group"}, loc: time.UTC, lang: i18n.En}
	at := time.Date(2026, 11, 10, 8, 0, 0, 0, time.UTC)
	if got := d.block(Item{Title: "See you at two", Sender: "Example", Body: " See you at two "}, at); got != "See you at two\nExample · 11-10 08:00" {
		t.Fatalf("the same text twice: %q", got)
	}
	if got := d.block(Item{Title: "Lunch", Body: "At noon"}, at); !strings.HasSuffix(got, "\n\nAt noon") {
		t.Fatalf("a body of its own: %q", got)
	}
}

// The database keeps an item's stored time to the microsecond: the items of the run that started a grouping are in its
// row all the same.
func TestTheRunThatMarksAGroupingIsInItsRow(t *testing.T) {
	from, mark := time.Date(2026, 11, 10, 9, 0, 0, 0, time.UTC), time.Date(2026, 11, 10, 9, 5, 0, 0, time.UTC)
	start := time.Date(2026, 11, 10, 10, 0, 0, 123456789, time.UTC)
	sub := markRows(Subscription{ID: "sub_a", Label: "Group", ToInbox: true, InboxFrom: &from, InboxCatchup: &mark, Recipe: sourceRecipe()}, false, start)
	if sub.InboxGroup != "source" || sub.InboxGroupFrom == nil {
		t.Fatalf("not marked: %q", sub.InboxGroup)
	}
	d := deliverer{sub: sub, loc: time.UTC, lang: i18n.En}
	at := start.Add(-time.Minute)
	stored := start.Truncate(time.Microsecond) // as Postgres hands it back
	things := d.inboxThings(nil, []Held{
		{Seq: 1, Stored: mark.Add(time.Minute), Item: Item{UID: "old", Title: "Before", Time: &at}},
		{Seq: 2, Stored: stored, Item: Item{UID: "new", Title: "After", Time: &at}},
	})
	if len(things) != 1 || things[0].Last != "new" || strings.Contains(things[0].Context, "Before") {
		t.Fatalf("the row of the marking run: %+v", things)
	}
}
