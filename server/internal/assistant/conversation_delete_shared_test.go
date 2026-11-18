package assistant

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/nwverse/persona/server/internal/db"
)

// Service-to-child handover can overlap. Separate service caches and DB handles
// must not turn a permanent deletion back into a visible conversation.
func sharedDeletionServices(t *testing.T) (*Service, *Service, *db.DB) {
	t.Helper()
	ctx := context.Background()
	first := todoTestDB(t)
	if err := first.PurgeSearch(ctx); err != nil {
		t.Fatal(err)
	}
	second, err := db.Open(ctx, os.Getenv("PERSONA_TEST_DATABASE_URL"), db.ManagerSet)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(second.Close)
	f := healthHub()
	f.settings = &Settings{MachineID: "server", Main: "main-0000-0000"}
	f.sessions["server"][1].Title = "shared-private-title"
	blue, green := newServiceOn(t, f, first), newServiceOn(t, f, second)
	for _, s := range []*Service{blue, green} {
		if err := s.conversationAvailable(ctx, "server", "work-0000-0000"); err != nil {
			t.Fatal(err)
		}
	}
	return blue, green, first
}

func TestDeletedConversationOrdersExistingIndexTransaction(t *testing.T) {
	_, green, oldDB := sharedDeletionServices(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := oldDB.PutSetting(ctx, deletedConversationsKey, map[string]any{}); err != nil {
		t.Fatal(err)
	}
	conn, err := pgx.Connect(ctx, os.Getenv("PERSONA_TEST_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(context.Background())
	var name string
	if err := conn.QueryRow(ctx, `SELECT current_database()`).Scan(&name); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(name, "persona_test_") {
		t.Skip("fault trigger requires dbtest's disposable database")
	}
	_, err = conn.Exec(ctx, `CREATE FUNCTION test_pause_search_write() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN PERFORM pg_advisory_xact_lock(hashtext(current_database() || 'search-delete-test')); RETURN NEW; END $$;
		CREATE TRIGGER test_pause_search_write BEFORE INSERT ON assistant_search_items FOR EACH ROW EXECUTE FUNCTION test_pause_search_write()`)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		cleanup, stop := context.WithTimeout(context.Background(), 5*time.Second)
		defer stop()
		_, _ = conn.Exec(cleanup, `SELECT pg_advisory_unlock(hashtext(current_database() || 'search-delete-test'))`)
		_, _ = conn.Exec(cleanup, `DROP TRIGGER IF EXISTS test_pause_search_write ON assistant_search_items; DROP FUNCTION IF EXISTS test_pause_search_write()`)
	}()
	if _, err := conn.Exec(ctx, `SELECT pg_advisory_lock(hashtext(current_database() || 'search-delete-test'))`); err != nil {
		t.Fatal(err)
	}
	written, deleted := make(chan error, 1), make(chan error, 1)
	go func() {
		written <- oldDB.PutSearchItems(ctx, "server", "work-0000-0000", "h", 1, []db.SearchItemRow{{Seq: 1, ItemID: "i1", Role: "user", Body: "in-flight-index", Folded: "in-flight-index"}}, false)
	}()
	waitFor(t, "index transaction at synthetic barrier", func() bool {
		var waiting bool
		_ = conn.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_locks l JOIN pg_stat_activity a USING(pid) WHERE a.datname=current_database() AND l.locktype='advisory' AND NOT l.granted)`).Scan(&waiting)
		return waiting
	})
	go func() { deleted <- green.deleteConversation(ctx, "server", "work-0000-0000") }()
	waitFor(t, "deletion ordered behind index transaction", func() bool {
		var waiting bool
		_ = conn.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND wait_event_type='Lock' AND query LIKE 'UPDATE settings SET value=value%')`).Scan(&waiting)
		return waiting
	})
	if _, err := conn.Exec(ctx, `SELECT pg_advisory_unlock(hashtext(current_database() || 'search-delete-test'))`); err != nil {
		t.Fatal(err)
	}
	for _, done := range []<-chan error{written, deleted} {
		select {
		case err := <-done:
			if err != nil {
				t.Fatal(err)
			}
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
	}
	var count int
	if err := conn.QueryRow(ctx, `SELECT count(*) FROM assistant_search_items WHERE machine_id='server' AND session_id='work-0000-0000'`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("index survived ordered deletion: %d %v", count, err)
	}
}

func TestDeletedConversationSharedDatabaseVisibility(t *testing.T) {
	blue, green, _ := sharedDeletionServices(t)
	ctx := context.Background()
	if err := green.deleteConversation(ctx, "server", "work-0000-0000"); err != nil {
		t.Fatal(err)
	}
	w := serve(blue, "GET", "/api/v1/assistant/search?q=shared-private-title", "")
	if w.Code != 200 || strings.Contains(w.Body.String(), "shared-private-title") {
		t.Fatalf("old service cache exposed deleted title: %d %s", w.Code, w.Body.String())
	}
	if convCode(blue.conversationAvailable(ctx, "server", "work-0000-0000")) != "conversation_deleted" {
		t.Fatal("old read/send guard still permits deleted conversation")
	}
	for _, method := range []string{DeviceSessionItems, DeviceAssistantSend} {
		if err := blue.call(ctx, "server", method, map[string]any{"session_id": "work-0000-0000", "text": "synthetic must not send"}, nil); convCode(err) != "conversation_deleted" {
			t.Fatalf("old instance %s guard: %v", method, err)
		}
	}
}

func TestDeletedConversationSharedDatabaseIndexWriter(t *testing.T) {
	_, green, oldDB := sharedDeletionServices(t)
	ctx := context.Background()
	if err := green.deleteConversation(ctx, "server", "work-0000-0000"); err != nil {
		t.Fatal(err)
	}
	err := oldDB.PutSearchItems(ctx, "server", "work-0000-0000", "h", 1, []db.SearchItemRow{{Seq: 1, ItemID: "i1", Role: "user", Body: "shared-private-index", Folded: "shared-private-index"}}, false)
	conn, openErr := pgx.Connect(ctx, os.Getenv("PERSONA_TEST_DATABASE_URL"))
	if openErr != nil {
		t.Fatal(openErr)
	}
	defer conn.Close(ctx)
	var rows int
	if queryErr := conn.QueryRow(ctx, `SELECT count(*) FROM assistant_search_items WHERE machine_id='server' AND session_id='work-0000-0000'`).Scan(&rows); queryErr != nil {
		t.Fatal(queryErr)
	}
	if err == nil || rows != 0 {
		t.Fatalf("another DB handle rebuilt deleted transcript: rows=%d error=%v", rows, err)
	}
}

func TestDeletedConversationSharedDatabaseDoesNotLoseTombstones(t *testing.T) {
	blue, green, database := sharedDeletionServices(t)
	ctx := context.Background()
	if err := blue.deleteConversation(ctx, "server", "work-0000-0000"); err != nil {
		t.Fatal(err)
	}
	if err := green.deleteConversation(ctx, "server", "sleep-000-0000"); err != nil {
		t.Fatal(err)
	}
	var rows map[string]deletedConversation
	if _, err := database.Setting(ctx, deletedConversationsKey, &rows); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"work-0000-0000", "sleep-000-0000"} {
		if _, ok := rows[runtimeListID("server", id)]; !ok {
			t.Fatalf("stale map overwrote tombstone for %s: %+v", id, rows)
		}
	}
}
