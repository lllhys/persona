package subscriptions

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/connector"
	"github.com/nwverse/persona/server/internal/i18n"
)

// What a recipe's seen field reads as seen (#6176): true, a non-zero number, or true / yes / read / seen in any case.
func TestSeenValues(t *testing.T) {
	for _, s := range []string{"true", "TRUE", "1", "2.5", "-1", "yes", "Read", "seen"} {
		if !seenValue(s) {
			t.Errorf("%q is not seen", s)
		}
	}
	for _, s := range []string{"", "false", "0", "0.0", "no", "unread", "null", `{"read":true}`} {
		if seenValue(s) {
			t.Errorf("%q is seen", s)
		}
	}
	var v any
	_ = json.Unmarshal([]byte(`{"id":"m1","title":"Hi","flags":{"read":true}}`), &v)
	f := connector.SubscriptionFields{ID: "$.id", Title: "$.title", Seen: "$.flags.read"}
	plain := connector.SubscriptionFields{ID: "$.id", Title: "$.title"}
	m := mapItem(v, &f, "sub_message", time.UTC)
	if !m.item.Seen || m.hash != mapItem(v, &plain, "sub_message", time.UTC).hash {
		t.Fatalf("mapped seen %v, or it changed the content", m.item.Seen)
	}
}

// An item pulled already seen is held seen; one pulled unseen and read in the source since is marked seen by the run
// that reads it so, without being stored again; and seen never turns off (#6176).
func TestARunMarksHeldItemsSeen(t *testing.T) {
	e := newEnv(t)
	answer := func(read map[int]bool, ids ...int) func(connector.CatalogQuery) (string, error) {
		var items []map[string]any
		for _, id := range ids {
			items = append(items, map[string]any{"id": fmt.Sprint("n", id), "title": fmt.Sprint("Notice ", id),
				"from": map[string]any{"name": "Office"}, "at": fmt.Sprintf("2026-11-10T0%d:00:00Z", id), "read": read[id]})
		}
		b, _ := json.Marshal(map[string]any{"items": items})
		return func(connector.CatalogQuery) (string, error) { return string(b), nil }
	}
	r := itemsRecipe()
	r.Fields.Seen = "$.read"
	e.conn.setAnswer(answer(map[int]bool{1: true}, 1, 2))
	e.turnOn(e.create(r))
	e.svc.RunDue(e.ctx)
	seen := func() map[string]bool {
		out := map[string]bool{}
		for _, it := range e.store.Items() {
			out[it.UID] = it.Seen
		}
		return out
	}
	if s := seen(); len(s) != 2 || !s["n1"] || s["n2"] {
		t.Fatalf("after the first run: %v", s)
	}
	e.conn.setAnswer(answer(map[int]bool{2: true}, 1, 2)) // n2 read since; n1 answered unread again
	e.clock.advance(20 * time.Minute)
	e.svc.RunDue(e.ctx)
	if s := seen(); len(s) != 2 || !s["n1"] || !s["n2"] {
		t.Fatalf("after the second run: %v", s)
	}
}

// A row is read when its newest item is, as a built-in's thing is read elsewhere; a digest when all its items are.
func TestRowsAreReadByTheirNewestItem(t *testing.T) {
	d := deliverer{sub: Subscription{ID: "sub_x", Label: "Notices", Recipe: Recipe{GroupBy: "thread"}}, loc: time.UTC, lang: i18n.En}
	at := time.Date(2026, 11, 13, 9, 0, 0, 0, time.UTC)
	row := func(seen1, seen2 bool) []Held {
		return []Held{{Seq: 1, Stored: at, Item: Item{UID: "a", Title: "A", Seen: seen1}},
			{Seq: 2, Stored: at.Add(time.Minute), Item: Item{UID: "b", Title: "B", Seen: seen2}}}
	}
	if !d.threadThing("t", row(false, true)).Read || d.threadThing("t", row(true, false)).Read {
		t.Error("a row's read state is not its newest item's")
	}
	if !d.digestThing(row(true, true)).Read || d.digestThing(row(true, false)).Read {
		t.Error("a digest is read only when all its items are")
	}
}
