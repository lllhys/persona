package assistant

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/nwverse/persona/server/internal/db"
)

// Hold the post's actual database availability answer after its read completed,
// before it can save, while another manager deletes the conversation.
type heldPostInboxStore struct {
	*db.DB
	entered, release chan struct{}
	saving, finish   chan struct{}
	once             sync.Once
}

func (st *heldPostInboxStore) PutConversationInboxItems(ctx context.Context, machine, session string, rows []db.InboxRow) error {
	if st.saving != nil {
		close(st.saving)
		select {
		case <-st.finish:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return st.DB.PutConversationInboxItems(ctx, machine, session, rows)
}

func TestInboxPostSharedDatabaseOrdersExistingCommit(t *testing.T) {
	blue, green, database := sharedDeletionServices(t)
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	// Keep the ledger row committed before the post takes its shared lock, so
	// the barrier observes deletion's UPDATE rather than its first-row INSERT.
	if err := database.PutSetting(ctx, deletedConversationsKey, map[string]any{}); err != nil {
		t.Fatal(err)
	}
	conn, err := pgx.Connect(ctx, os.Getenv("PERSONA_TEST_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(context.Background())
	var name string
	if err := conn.QueryRow(ctx, `SELECT current_database()`).Scan(&name); err != nil || !strings.HasPrefix(name, "persona_test_") {
		t.Fatalf("barrier requires dbtest's disposable database: %q %v", name, err)
	}
	_, err = conn.Exec(ctx, `CREATE FUNCTION test_pause_inbox_post() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN PERFORM pg_advisory_xact_lock(hashtext(current_database() || 'inbox-delete-test')); RETURN NEW; END $$;
		CREATE TRIGGER test_pause_inbox_post BEFORE INSERT ON inbox_items FOR EACH ROW EXECUTE FUNCTION test_pause_inbox_post()`)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		cleanup, stop := context.WithTimeout(context.Background(), 5*time.Second)
		defer stop()
		_, _ = conn.Exec(cleanup, `SELECT pg_advisory_unlock(hashtext(current_database() || 'inbox-delete-test'))`)
		_, _ = conn.Exec(cleanup, `DROP TRIGGER IF EXISTS test_pause_inbox_post ON inbox_items; DROP FUNCTION IF EXISTS test_pause_inbox_post()`)
	}()
	if _, err := conn.Exec(ctx, `SELECT pg_advisory_lock(hashtext(current_database() || 'inbox-delete-test'))`); err != nil {
		t.Fatal(err)
	}
	posted, deleted := make(chan error, 1), make(chan error, 1)
	go func() {
		_, err := blue.postToInbox(ctx, "server", "work-0000-0000", inboxPostArgs{Title: "Already committed result", Summary: "inbox-commit-control", Reason: "owner asked"}, time.Now())
		posted <- err
	}()
	waitFor(t, "Inbox transaction at its database barrier", func() bool {
		var waiting bool
		_ = conn.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_locks l JOIN pg_stat_activity a USING(pid) WHERE a.datname=current_database() AND l.locktype='advisory' AND NOT l.granted)`).Scan(&waiting)
		return waiting
	})
	go func() { deleted <- green.deleteConversation(ctx, "server", "work-0000-0000") }()
	waitFor(t, "deletion ordered behind the Inbox transaction", func() bool {
		var waiting bool
		_ = conn.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND wait_event_type='Lock' AND query LIKE 'UPDATE settings SET value=value%')`).Scan(&waiting)
		return waiting
	})
	if _, err := conn.Exec(ctx, `SELECT pg_advisory_unlock(hashtext(current_database() || 'inbox-delete-test'))`); err != nil {
		t.Fatal(err)
	}
	for _, done := range []<-chan error{posted, deleted} {
		select {
		case err := <-done:
			if err != nil {
				t.Fatal(err)
			}
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
	}
	var saved int
	if err := database.QueryRow(ctx, `SELECT count(*) FROM inbox_items WHERE data::text LIKE '%inbox-commit-control%'`).Scan(&saved); err != nil || saved != 1 {
		t.Fatalf("the derived row committed before deletion was not retained: %d %v", saved, err)
	}
}

func (st *heldPostInboxStore) ConversationDeletion(ctx context.Context, machine, session string) (json.RawMessage, bool, error) {
	raw, deleted, err := st.DB.ConversationDeletion(ctx, machine, session)
	st.once.Do(func() {
		close(st.entered)
		select {
		case <-st.release:
		case <-ctx.Done():
		}
	})
	return raw, deleted, err
}

func TestInboxPostSharedDatabaseCannotCommitAfterDeletion(t *testing.T) {
	for _, saved := range []bool{false, true} {
		name := "new row"
		if saved {
			name = "previously saved row stays unchanged"
		}
		t.Run(name, func(t *testing.T) {
			blue, green, database := sharedDeletionServices(t)
			ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
			defer cancel()
			const machine, id, private = "server", "work-0000-0000", "late-private-result"
			var prior inboxPosted
			if saved {
				var err error
				prior, err = green.postToInbox(ctx, machine, id, inboxPostArgs{Title: "Saved result", Summary: "saved before deletion", Reason: "owner asked"}, time.Now())
				if err != nil {
					t.Fatal(err)
				}
				blue.inbox.loaded.Store(false)
				if err := blue.inboxReady(ctx); err != nil {
					t.Fatal(err)
				}
			}
			st := &heldPostInboxStore{DB: database, entered: make(chan struct{}), release: make(chan struct{})}
			resume := sync.OnceFunc(func() { close(st.release) })
			defer resume()
			blue.store = func() Store { return st }
			done := make(chan error, 1)
			go func() {
				_, err := blue.postToInbox(ctx, machine, id, inboxPostArgs{Title: private, Summary: private, Reason: "late callback"}, time.Now())
				done <- err
			}()
			select {
			case <-st.entered:
			case <-ctx.Done():
				t.Fatal("post did not finish its database availability read")
			}
			if err := green.deleteConversation(ctx, machine, id); err != nil {
				t.Fatal(err)
			}
			resume() // deletion has acknowledged before the old process can save
			select {
			case err := <-done:
				if err == nil {
					t.Error("old manager accepted a post after another manager acknowledged deletion")
				}
			case <-ctx.Done():
				t.Fatal("post did not finish after the barrier")
			}
			var leaked int
			if err := database.QueryRow(ctx, `SELECT count(*) FROM inbox_items WHERE data::text LIKE '%' || $1 || '%'`, private).Scan(&leaked); err != nil || leaked != 0 {
				t.Errorf("deleted conversation reached the real Inbox table: rows=%d error=%v", leaked, err)
			}
			blue.inbox.mu.Lock()
			for _, item := range blue.inbox.items {
				if strings.Contains(item.Title+item.Summary, private) {
					t.Error("failed post remained in the manager's published Inbox")
				}
			}
			blue.inbox.mu.Unlock()
			if saved {
				var raw []byte
				if err := database.QueryRow(ctx, `SELECT data FROM inbox_items WHERE id=$1`, prior.id).Scan(&raw); err != nil || !strings.Contains(string(raw), "saved before deletion") {
					t.Errorf("previously saved derived row changed: %s %v", raw, err)
				}
			}
			if _, err := blue.postToInbox(ctx, machine, "main-0000-0000", inboxPostArgs{Title: "Live control", Summary: "ordinary post", Reason: "owner asked"}, time.Now()); err != nil {
				t.Fatalf("ordinary conversation was blocked: %v", err)
			}
		})
	}
}

func TestInboxPostSharedDatabaseDoesNotExposePendingPost(t *testing.T) {
	for _, saved := range []bool{false, true} {
		t.Run(map[bool]string{false: "new row", true: "existing derived row"}[saved], func(t *testing.T) {
			blue, green, database := sharedDeletionServices(t)
			ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
			defer cancel()
			const private = "uncommitted-private-post"
			if saved {
				if _, err := green.postToInbox(ctx, "server", "work-0000-0000", inboxPostArgs{Title: "Saved result", Summary: "previous derived result", Reason: "owner asked"}, time.Now()); err != nil {
					t.Fatal(err)
				}
				blue.inbox.loaded.Store(false)
				if err := blue.inboxReady(ctx); err != nil {
					t.Fatal(err)
				}
			}
			read := func() <-chan *httptest.ResponseRecorder {
				done := make(chan *httptest.ResponseRecorder, 1)
				go func() { done <- serve(blue, "GET", "/api/v1/inbox?sources=all", "") }()
				return done
			}
			before := <-read()
			if before.Code != 200 {
				t.Fatalf("Inbox control: %d %s", before.Code, before.Body.String())
			}
			var original struct{ Items []InboxItem }
			if err := json.Unmarshal(before.Body.Bytes(), &original); err != nil || saved && len(original.Items) == 0 {
				t.Fatalf("missing saved-row control: %s %v", before.Body.String(), err)
			}
			st := &heldPostInboxStore{DB: database, entered: make(chan struct{}), release: make(chan struct{}), saving: make(chan struct{}), finish: make(chan struct{})}
			resumeRead := sync.OnceFunc(func() { close(st.release) })
			resumeSave := sync.OnceFunc(func() { close(st.finish) })
			defer resumeRead()
			defer resumeSave()
			blue.store = func() Store { return st }
			posted := make(chan error, 1)
			go func() {
				_, err := blue.postToInbox(ctx, "server", "work-0000-0000", inboxPostArgs{Title: private, Summary: private, Reason: "late callback"}, time.Now())
				posted <- err
			}()
			select {
			case <-st.entered:
			case <-ctx.Done():
				t.Fatal("availability read did not reach its barrier")
			}
			if err := green.deleteConversation(ctx, "server", "work-0000-0000"); err != nil {
				t.Fatal(err)
			}
			resumeRead()
			select {
			case <-st.saving:
			case <-ctx.Done():
				t.Fatal("post did not reach its guarded save")
			}
			select {
			case pending := <-read():
				var visible struct{ Items []InboxItem }
				err := json.Unmarshal(pending.Body.Bytes(), &visible)
				if pending.Code != 200 || err != nil || strings.Contains(pending.Body.String(), private) || !reflect.DeepEqual(original.Items, visible.Items) {
					t.Errorf("GET exposed an uncommitted post after deletion acknowledged: %d %s %v", pending.Code, pending.Body.String(), err)
				}
			case <-ctx.Done():
				t.Fatal("Inbox GET blocked behind the slow post save")
			}
			var filed <-chan *httptest.ResponseRecorder
			if saved {
				id := original.Items[0].ID
				done := make(chan *httptest.ResponseRecorder, 1)
				go func() { done <- serve(blue, "POST", "/api/v1/inbox/"+id+"/done", `{"done":true}`) }()
				filed = done
				waitFor(t, "the concurrent owner's filing while the post is pending", func() bool {
					blue.inbox.mu.Lock()
					defer blue.inbox.mu.Unlock()
					return blue.inbox.items[id].DoneAt != nil
				})
			}
			resumeSave()
			select {
			case err := <-posted:
				if err == nil {
					t.Error("post succeeded after deletion")
				}
			case <-ctx.Done():
				t.Fatal("post did not finish")
			}
			if filed != nil {
				select {
				case result := <-filed:
					if result.Code != 200 || strings.Contains(result.Body.String(), private) {
						t.Errorf("the owner's queued save inherited a rejected post: %d %s", result.Code, result.Body.String())
					}
				case <-ctx.Done():
					t.Fatal("the owner's filing did not finish")
				}
				var raw []byte
				if err := database.QueryRow(ctx, `SELECT data FROM inbox_items WHERE id=$1`, original.Items[0].ID).Scan(&raw); err != nil || strings.Contains(string(raw), private) || !strings.Contains(string(raw), "previous derived result") {
					t.Errorf("concurrent filing persisted a rejected post: %s %v", raw, err)
				}
			}
		})
	}
}

func TestInboxPostDetachedCommitPreservesConcurrentReadAndNotes(t *testing.T) {
	for _, deleted := range []bool{false, true} {
		t.Run(map[bool]string{false: "successful post", true: "rejected post"}[deleted], func(t *testing.T) {
			blue, green, database := sharedDeletionServices(t)
			ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
			defer cancel()
			prior, err := green.postToInbox(ctx, "server", "work-0000-0000", inboxPostArgs{Title: "Saved result", Summary: "previous result", Reason: "owner asked"}, time.Now())
			if err != nil {
				t.Fatal(err)
			}
			blue.inbox.loaded.Store(false)
			if err := blue.inboxReady(ctx); err != nil {
				t.Fatal(err)
			}
			st := &heldPostInboxStore{DB: database, entered: make(chan struct{}), release: make(chan struct{}), saving: make(chan struct{}), finish: make(chan struct{})}
			resumeRead := sync.OnceFunc(func() { close(st.release) })
			resumeSave := sync.OnceFunc(func() { close(st.finish) })
			defer resumeRead()
			defer resumeSave()
			blue.store = func() Store { return st }
			posted := make(chan error, 1)
			go func() {
				_, err := blue.postToInbox(ctx, "server", "work-0000-0000", inboxPostArgs{Title: "New post", Summary: "next-result-body", Reason: "owner asked"}, time.Now())
				posted <- err
			}()
			select {
			case <-st.entered:
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			if deleted {
				if err := green.deleteConversation(ctx, "server", "work-0000-0000"); err != nil {
					t.Fatal(err)
				}
			}
			resumeRead()
			select {
			case <-st.saving:
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			blue.inbox.mu.Lock()
			live := blue.inbox.items[prior.id]
			if live == nil {
				blue.inbox.mu.Unlock()
				t.Fatal("missing existing derived row")
			}
			live.Read = true
			blue.inbox.chainAdd(live, InboxChainEntry{Kind: ChainInstructed, Text: "concurrent owner note", Via: ViaUser})
			blue.inbox.touched(live.ID)
			blue.inbox.mu.Unlock()
			saved := make(chan map[string]bool, 1)
			go func() { saved <- blue.saveInbox(ctx, []*InboxItem{live}) }()
			resumeSave()
			select {
			case err := <-posted:
				if (err != nil) != deleted {
					t.Errorf("post result: %v, deleted=%v", err, deleted)
				}
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			select {
			case failed := <-saved:
				if failed[live.ID] {
					t.Fatal("concurrent read/note was not saved")
				}
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			var raw []byte
			if err := database.QueryRow(ctx, `SELECT data FROM inbox_items WHERE id=$1`, prior.id).Scan(&raw); err != nil {
				t.Fatal(err)
			}
			var final InboxItem
			if err := json.Unmarshal(raw, &final); err != nil || !final.Read || !strings.Contains(string(raw), "concurrent owner note") || strings.Contains(string(raw), "next-result-body") == deleted {
				t.Errorf("post lost a concurrent read/note or persisted rejected words: %s %v", raw, err)
			}
		})
	}
}
