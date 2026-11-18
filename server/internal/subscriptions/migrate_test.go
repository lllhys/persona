package subscriptions

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/connector"
)

// migration is the env connector's notices as a built-in's default subscription: threads and ids of its own.
func migration() Migration {
	r := threadRecipe()
	return Migration{Connector: "example", Source: "notices", SourceName: "Notices", Label: "Example", Icon: "chat", Recipe: r}
}

// many is an answer of n notices, each a thread of its own, as a built-in names them.
func many(n int) string {
	var items []map[string]any
	for i := 1; i <= n; i++ {
		items = append(items, map[string]any{"id": fmt.Sprint("m", i), "thread": fmt.Sprint("chat-", i), "title": fmt.Sprint("Message ", i),
			"from": map[string]any{"name": "Ann"}, "at": time.Date(2026, 11, 10, 8, i%60, 0, 0, time.UTC).Format(time.RFC3339)})
	}
	b, _ := json.Marshal(map[string]any{"items": items})
	return string(b)
}

// Switched on (#6265): the default subscription is made with both destinations off (so a manager that does not know
// Migrated leaves it off), pulled at once, and its rows go to the Inbox under the built-in's id with the items' own
// threads and ids; its catch-up comes as rows, never a digest. Switched off: no rows, no pulls, the built-in back.
// Switched on again: the same rows, so nothing new.
func TestASwitchedBuiltinFeedIsItsDefaultSubscriptionAndBack(t *testing.T) {
	e := newEnv(t)
	e.answer(many(digestOver + 5))
	sub, err := e.svc.Migrate(e.ctx, migration(), true, Actor{ID: "device:phone"})
	if err != nil || sub.ID != MigrationID("example") || sub.State != StateOK {
		t.Fatalf("switched on: %+v %v", sub, err)
	}
	stored, _ := e.store.GetSubscription(e.ctx, sub.ID)
	if stored.ToInbox || stored.ToKB || stored.Migrated == nil {
		t.Fatalf("stored: inbox %v kb %v migrated %v", stored.ToInbox, stored.ToKB, stored.Migrated)
	}
	if m, _ := e.svc.Migrated(e.ctx); !m["example"] || len(m) != 1 {
		t.Fatalf("migrated: %v", m)
	}
	e.svc.RunDue(e.ctx)
	src := e.inbox("example")
	rows := e.rows("example")
	if len(rows) != digestOver+5 || src.Connector != "" || src.ReadError != "" {
		t.Fatalf("the built-in's feed: %d rows (want each item, no digest), %+v", len(rows), src)
	}
	for _, r := range rows {
		if strings.HasPrefix(r.Thread, "sub:") || !strings.HasPrefix(r.Thread, "chat-") || !strings.HasPrefix(r.Last, "m") {
			t.Fatalf("a row not under the item's own thread and id: %+v", r)
		}
	}
	first := threadsOf(rows)

	if _, err := e.svc.Migrate(e.ctx, migration(), false, Actor{ID: "device:phone"}); err != nil {
		t.Fatal(err)
	}
	if m, _ := e.svc.Migrated(e.ctx); len(m) != 0 {
		t.Fatalf("migrated after switching back: %v", m)
	}
	srcs, _ := e.svc.InboxSources(e.ctx)
	for _, s := range srcs {
		if s.ID == "example" || s.ID == sub.ID && len(s.Things) > 1 {
			t.Fatalf("still the built-in's feed after switching back: %+v", s)
		}
	}
	calls := e.conn.callCount()
	e.clock.advance(2 * time.Hour)
	e.svc.RunDue(e.ctx)
	if e.conn.callCount() != calls {
		t.Fatal("pulled after switching back")
	}

	if _, err := e.svc.Migrate(e.ctx, migration(), true, Actor{ID: "device:phone"}); err != nil {
		t.Fatal(err)
	}
	e.svc.RunDue(e.ctx)
	if again := threadsOf(e.rows("example")); !slices.Equal(again, first) {
		t.Fatalf("switched on again: rows %v, want the same %v", again, first)
	}
}

func threadsOf(rows []connector.InboxThing) []string {
	var out []string
	for _, r := range rows {
		out = append(out, r.Thread+"@"+r.Last)
	}
	slices.Sort(out)
	return out
}

// Migrated is kept with the subscription; a subscription stored by a manager before it has none (#6265).
func TestMigratedIsKept(t *testing.T) {
	at := time.Date(2026, 11, 13, 9, 0, 0, 0, time.UTC)
	data, err := encode(Subscription{ID: MigrationID("example"), Migrated: &at})
	if err != nil {
		t.Fatal(err)
	}
	back, err := decode(data)
	if err != nil || back.Migrated == nil || !back.Migrated.Equal(at) {
		t.Fatalf("decoded: %v %v", back.Migrated, err)
	}
	old, _ := decode([]byte(`{"id":"sub_m_example","keep":{"tool":"list"}}`))
	if old.Migrated != nil || on(old) {
		t.Fatalf("an older one: %+v", old)
	}
	if _, ok := MigratedFrom(Subscription{ID: "sub_abc", Migrated: &at}); ok {
		t.Error("a subscription that is no built-in's default is migrated from one")
	}
}
