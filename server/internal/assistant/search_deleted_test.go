package assistant

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/db"
	"github.com/nwverse/persona/server/internal/proto"
)

type heldDeletedSearch struct {
	*memSearch
	entered, release chan struct{}
}

type failingDeletedDrop struct {
	*memSearch
	failure error
	calls   int
}

func (m *failingDeletedDrop) DropSearchConversation(ctx context.Context, machine, id string) error {
	m.calls++
	if m.failure != nil {
		return m.failure
	}
	return m.memSearch.DropSearchConversation(ctx, machine, id)
}

func TestSearchDeletedCleanupRetry(t *testing.T) {
	s, _, store := searchService(t)
	ctx := context.Background()
	s.searchSweep(ctx)
	broken := &failingDeletedDrop{memSearch: store, failure: errors.New("synthetic index unavailable")}
	s.search.store = broken
	if err := s.deleteConversation(ctx, "server", "trip"); err == nil {
		t.Fatal("failed index cleanup acknowledged")
	}
	if convCode(s.conversationAvailable(ctx, "server", "trip")) != "conversation_deleted" {
		t.Fatal("cleanup error undid tombstone")
	}
	w := serve(s, "GET", "/api/v1/assistant/search?q=BlueLane", "")
	if w.Code != 200 || strings.Contains(w.Body.String(), `"session_id":"trip"`) {
		t.Fatalf("failed cleanup leaked: %d %s", w.Code, w.Body.String())
	}
	broken.failure = nil
	if err := s.deleteConversation(ctx, "server", "trip"); err != nil {
		t.Fatal(err)
	}
	if broken.calls < 2 || len(store.rows[searchKey{"server", "trip"}]) != 0 {
		t.Fatal("idempotent retry did not finish index cleanup")
	}
}

func TestSearchDeletedLastConversation(t *testing.T) {
	s, _, store, list := searchFixture(t)
	ctx := context.Background()
	list.f.setSessions("server", proto.Session{MachineID: "server", SessionID: "trip", Assistant: true, Title: "Single", Rev: 1})
	setSettings(t, s, func(v *Settings) { v.Main = "" })
	s.searchSweep(ctx)
	if err := s.deleteConversation(ctx, "server", "trip"); err != nil {
		t.Fatal(err)
	}
	s.searchSweep(ctx)
	w := serve(s, "GET", "/api/v1/assistant/search?q=BlueLane", "")
	var answer searchAnswer
	if err := json.Unmarshal(w.Body.Bytes(), &answer); err != nil || w.Code != 200 || len(answer.Results) != 0 || answer.Indexed.Total != 0 || answer.Indexed.Done != 0 {
		t.Fatalf("last-chat search: %d %s %v", w.Code, w.Body.String(), err)
	}
	if len(store.rows) != 0 || len(store.cursors) != 0 {
		t.Fatal("last chat index retained")
	}
}

func TestSearchReindexPreservesDeletedConversation(t *testing.T) {
	s, _, store := searchService(t)
	ctx := context.Background()
	s.searchSweep(ctx)
	if err := s.deleteConversation(ctx, "server", "trip"); err != nil {
		t.Fatal(err)
	}
	store.resetIndex()
	s.searchSweep(ctx)
	store.mu.Lock()
	deleted := len(store.rows[searchKey{"server", "trip"}])
	control := len(store.rows[searchKey{"server", "old"}])
	store.mu.Unlock()
	if deleted != 0 || control == 0 {
		t.Fatalf("reindex revived deletion or lost control: %d %d", deleted, control)
	}
	if convCode(s.conversationAvailable(ctx, "server", "trip")) != "conversation_deleted" {
		t.Fatal("reindex removed tombstone")
	}
}

func TestSearchDeletedPostgresCleanup(t *testing.T) {
	database := todoTestDB(t)
	ctx := context.Background()
	if err := database.PurgeSearch(ctx); err != nil {
		t.Fatal(err)
	}
	f := healthHub()
	s := newServiceOn(t, f, database)
	setSettings(t, s, func(v *Settings) { v.MachineID = "server"; v.Main = "main-0000-0000" })
	for _, id := range []string{"work-0000-0000", "main-0000-0000"} {
		if err := database.PutSearchItems(ctx, "server", id, "synthetic-history", 1, []db.SearchItemRow{{Seq: 1, ItemID: "i1", Role: "user", Body: "marker " + id, Folded: "marker " + id}}, false); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.deleteConversation(ctx, "server", "work-0000-0000"); err != nil {
		t.Fatal(err)
	}
	hits, err := database.SearchAssistantItems(ctx, []string{"server", "server"}, []string{"work-0000-0000", "main-0000-0000"}, []string{"marker"}, 3)
	if err != nil || len(hits) != 1 || hits[0].SessionID != "main-0000-0000" {
		t.Fatalf("Postgres retained deleted body/lost control: %+v %v", hits, err)
	}
	cursors, err := database.SearchCursors(ctx)
	if err != nil || len(cursors) != 1 || cursors[0].SessionID != "main-0000-0000" {
		t.Fatalf("Postgres cursors: %+v %v", cursors, err)
	}
	restarted := newServiceOn(t, f, database)
	if err := restarted.deleteConversation(ctx, "server", "work-0000-0000"); err != nil {
		t.Fatalf("restart cleanup retry: %v", err)
	}
}

func (m *heldDeletedSearch) SearchAssistantItems(ctx context.Context, machines, sessions, words []string, per int) ([]db.SearchHitRow, error) {
	rows, err := m.memSearch.SearchAssistantItems(ctx, machines, sessions, words, per)
	close(m.entered)
	select {
	case <-m.release:
		return rows, err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func awaitDeletedSearch(t *testing.T, ch <-chan struct{}) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(5 * time.Second):
		t.Fatal("synthetic search barrier timed out")
	}
}

func TestSearchDeletedConcurrentQuery(t *testing.T) {
	s, feed, store := searchService(t)
	feed.items["trip"] = []proto.Item{msg(1, "user", "marker private-deleted-words")}
	feed.items["old"] = []proto.Item{msg(1, "user", "marker surviving-words")}
	s.searchSweep(context.Background())
	held := &heldDeletedSearch{memSearch: store, entered: make(chan struct{}), release: make(chan struct{})}
	s.search.store = held
	w := httptest.NewRecorder()
	done := make(chan struct{})
	go func() {
		defer close(done)
		s.ServeHTTP(w, httptest.NewRequest("GET", "/api/v1/assistant/search?q=marker", nil))
	}()
	awaitDeletedSearch(t, held.entered)
	if err := s.deleteConversation(context.Background(), "server", "trip"); err != nil {
		close(held.release)
		t.Fatal(err)
	}
	close(held.release)
	awaitDeletedSearch(t, done)
	if w.Code != 200 || strings.Contains(w.Body.String(), "private-deleted-words") || strings.Contains(w.Body.String(), `"session_id":"trip"`) || !strings.Contains(w.Body.String(), "surviving-words") {
		t.Fatalf("stale search response: %d %s", w.Code, w.Body.String())
	}
	var result searchAnswer
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil || result.Indexed.Total != 3 || result.Indexed.Done != 3 {
		t.Fatalf("deleted chat counted: %+v %v", result.Indexed, err)
	}
}

func TestSearchDeletedDropsRowsAndCursor(t *testing.T) {
	s, _, store := searchService(t)
	s.searchSweep(context.Background())
	if err := s.deleteConversation(context.Background(), "server", "trip"); err != nil {
		t.Fatal(err)
	}
	store.mu.Lock()
	rows, cursor := len(store.rows[searchKey{"server", "trip"}]), store.cursors[searchKey{"server", "trip"}]
	survivor := len(store.rows[searchKey{"server", "old"}])
	store.mu.Unlock()
	if rows != 0 || cursor.SessionID != "" || survivor == 0 {
		t.Fatalf("index retained deleted chat or lost control: rows=%d cursor=%+v survivor=%d", rows, cursor, survivor)
	}
}

func TestSearchDeletedCannotReindexLatePage(t *testing.T) {
	s, _, store := searchService(t)
	entered, release, done := make(chan struct{}), make(chan struct{}), make(chan struct{})
	s.search.fetch = func(context.Context, string, string, int) (searchPage, error) {
		close(entered)
		<-release
		return searchPage{History: "h", Items: []proto.Item{msg(1, "user", "private-late-page")}}, nil
	}
	go func() {
		defer close(done)
		_, _, _ = s.searchRead(context.Background(), store, proto.Session{MachineID: "server", SessionID: "trip"}, searchCursor{}, false)
	}()
	awaitDeletedSearch(t, entered)
	if err := s.deleteConversation(context.Background(), "server", "trip"); err != nil {
		close(release)
		t.Fatal(err)
	}
	close(release)
	awaitDeletedSearch(t, done)
	store.mu.Lock()
	defer store.mu.Unlock()
	if len(store.rows[searchKey{"server", "trip"}]) != 0 || store.cursors[searchKey{"server", "trip"}].SessionID != "" {
		t.Fatal("late page recreated deleted index")
	}
}

func TestSearchDeletedRestartPurgesWithEmptyInventory(t *testing.T) {
	s, feed, store, list := searchFixture(t)
	s.searchSweep(context.Background())
	ctx := context.Background()
	if err := s.store().PutSetting(ctx, deletedConversationsKey, map[string]deletedConversation{runtimeListID("server", "trip"): {Machine: "server", ID: "trip", At: time.Now()}}); err != nil {
		t.Fatal(err)
	}
	list.f.setSessions("server")
	restarted := newServiceOn(t, list.f, s.store())
	restarted.search.store, restarted.search.fetch = store, feed.fetch
	restarted.searchSweep(ctx)
	store.mu.Lock()
	defer store.mu.Unlock()
	if len(store.rows[searchKey{"server", "trip"}]) != 0 || store.cursors[searchKey{"server", "trip"}].SessionID != "" {
		t.Fatal("restart retained permanently deleted chat because inventory was empty")
	}
	if len(store.rows[searchKey{"server", "old"}]) == 0 {
		t.Fatal("offline control chat lost its index")
	}
}
