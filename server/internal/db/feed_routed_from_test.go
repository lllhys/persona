package db

import (
	"context"
	"io/fs"
	"testing"
	"time"
)

// 0441 marks the routed items Persona wrote before routed bodies said so (#1542): an agent_interrupt's, a rework's,
// the hand-off agent_send wrote right after its task's card; the user's own stay as they were, and so does an item
// already marked. Applied again on top of a migrated database, as it would meet stored history.
func TestRoutedFromBackfill(t *testing.T) {
	ctx := context.Background()
	d, err := Open(ctx, migratedDatabase(t, ManagerSet), ManagerSet)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	at := time.Date(2026, 11, 8, 12, 0, 0, 0, time.UTC)
	rows := []FeedRow{
		{ID: "f_card", Conversation: "c1", At: at, Kind: "task", Ref: "todo_a"},
		{ID: "f_handoff", Conversation: "c1", At: at.Add(8 * time.Millisecond), Kind: "routed", Ref: "todo_a", Body: []byte(`{"text":"把内存降下来"}`)},
		{ID: "f_user", Conversation: "c1", At: at.Add(time.Minute), Kind: "routed", Ref: "todo_a", Body: []byte(`{"text":"顺便看看日志"}`)},
		{ID: "f_interrupt", Conversation: "c1", At: at.Add(2 * time.Minute), Kind: "routed", Ref: "todo_a", Body: []byte(`{"text":"先停一下","interrupted":false}`)},
		{ID: "f_rework", Conversation: "c1", At: at.Add(3 * time.Minute), Kind: "routed", Ref: "todo_a", Body: []byte(`{"text":"（督办：验收没通过，第 1 轮返工，最多 2 轮）\n补截图"}`)},
		// Another conversation's card of the same todo id, or another todo's card, is not this item's.
		{ID: "f_card_b", Conversation: "c1", At: at.Add(4 * time.Minute), Kind: "task", Ref: "todo_b"},
		{ID: "f_other_todo", Conversation: "c1", At: at.Add(4*time.Minute + time.Second), Kind: "routed", Ref: "todo_c", Body: []byte(`{"text":"别的任务"}`)},
		{ID: "f_card_c2", Conversation: "c2", At: at.Add(5 * time.Minute), Kind: "task", Ref: "todo_d"},
		{ID: "f_other_conv", Conversation: "c1", At: at.Add(5*time.Minute + time.Second), Kind: "routed", Ref: "todo_d", Body: []byte(`{"text":"另一个对话"}`)},
		// Long after its card: the user's.
		{ID: "f_late", Conversation: "c1", At: at.Add(time.Hour), Kind: "routed", Ref: "todo_b", Body: []byte(`{"text":"晚些说的"}`)},
		{ID: "f_marked", Conversation: "c1", At: at.Add(2 * time.Hour), Kind: "routed", Ref: "todo_a", Body: []byte(`{"text":"已标记","from":"assistant"}`)},
	}
	for _, r := range rows {
		if err := d.AppendFeed(ctx, r); err != nil {
			t.Fatal(err)
		}
	}
	sql, err := fs.ReadFile(migrations, "migrations/manager/0441_feed_routed_from.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.pool.Exec(ctx, string(sql)); err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"f_handoff": "assistant", "f_user": "", "f_interrupt": "assistant", "f_rework": "assistant",
		"f_other_todo": "", "f_other_conv": "", "f_late": "", "f_marked": "assistant"}
	for id, from := range want {
		var got string
		var text string
		if err := d.pool.QueryRow(ctx, `SELECT coalesce(body->>'from', ''), body->>'text' FROM assistant_feed WHERE id = $1`, id).Scan(&got, &text); err != nil {
			t.Fatalf("%s: %v", id, err)
		}
		if got != from || text == "" {
			t.Errorf("%s: from %q (text %q), want %q", id, got, text, from)
		}
	}
	// Run twice, it changes nothing more.
	if tag, err := d.pool.Exec(ctx, string(sql)); err != nil || tag.RowsAffected() != 0 {
		t.Fatalf("second run: %v rows, %v", tag.RowsAffected(), err)
	}
}
