package assistant

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/db"
)

// The feed response reads stored content, independently of session.items. Hold
// that read after its request passed the entry guard and before it can reply.
type deletionReadStore struct {
	fakeViewsStore
	entered chan struct{}
	release chan struct{}
}

func (st *deletionReadStore) Feed(ctx context.Context, conversation string, since time.Time, limit int) ([]db.FeedRow, error) {
	rows, err := st.fakeViewsStore.Feed(ctx, conversation, since, limit)
	if st.entered != nil {
		close(st.entered)
		select {
		case <-st.release:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return rows, err
}

func TestDeletePersonaDiscardsStoredFeedAlreadyInFlight(t *testing.T) {
	const id = "work-0000-0000"
	const secret = "synthetic retained conversation words"
	st := &deletionReadStore{}
	st.feed = []db.FeedRow{{ID: "feed-example", Conversation: id, At: time.Now().UTC(), Kind: "synthetic", Body: json.RawMessage(`{"text":"` + secret + `"}`)}}
	st.views = map[string]db.AssistantViewRow{"view-example": {
		ID: "view-example", Conversation: id, MachineID: "server", Version: 1,
		View: json.RawMessage(`{"fallback":"synthetic retained card","sections":[]}`),
	}}
	s := newServiceOn(t, healthHub(), st)
	setSettings(t, s, func(v *Settings) { v.MachineID, v.Main = "server", "main-0000-0000" })
	path := "/api/v1/assistant/conversations/" + id + "/feed"
	// A nonempty control keeps a failed fixture from passing as a safe empty reply.
	before := serve(s, "GET", path, "")
	if before.Code != http.StatusOK || !strings.Contains(before.Body.String(), secret) || !strings.Contains(before.Body.String(), "synthetic retained card") {
		t.Fatalf("readable control: %d %s", before.Code, before.Body.String())
	}
	st.entered, st.release = make(chan struct{}), make(chan struct{}, 1)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	defer close(st.release)
	done := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		w := httptest.NewRecorder()
		s.ServeHTTP(w, httptest.NewRequest("GET", path, nil).WithContext(ctx))
		done <- w
	}()
	select {
	case <-st.entered:
	case <-ctx.Done():
		t.Fatal("feed read never reached its stored snapshot")
	}
	if err := s.deleteConversation(ctx, "server", id); err != nil {
		t.Fatal(err)
	}
	// Release without closing: the deferred close also unblocks a failing test.
	st.release <- struct{}{}
	select {
	case w := <-done:
		if w.Code != http.StatusGone || !strings.Contains(w.Body.String(), `"code":"conversation_deleted"`) {
			t.Fatalf("late feed reply: %d %s", w.Code, w.Body.String())
		}
		for _, word := range []string{secret, "synthetic retained card", `"items"`, `"views"`, `"view_actions"`, `"view_blocks"`} {
			if strings.Contains(w.Body.String(), word) {
				t.Errorf("deleted feed response retains %q", word)
			}
		}
	case <-ctx.Done():
		t.Fatal("feed read did not finish after deletion")
	}
}

func TestDeletePersonaSuppressesStoredConversationBroadcasts(t *testing.T) {
	f := healthHub()
	s := newTestService(t, f)
	ctx := context.Background()
	const deleted, other = "work-0000-0000", "steps-000-0000"
	if err := s.deleteConversation(ctx, "server", deleted); err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"assistant_feed", "assistant_view", "assistant_view_action", "assistant_view_block"} {
		t.Run(kind, func(t *testing.T) {
			before := strings.Count(f.eventTypes(), kind)
			s.event(ctx, map[string]any{"type": kind, "conversation": deleted, "text": "synthetic deleted payload"})
			if got := strings.Count(f.eventTypes(), kind); got != before {
				t.Errorf("deleted conversation broadcast escaped: %s", kind)
			}
			before = strings.Count(f.eventTypes(), kind)
			s.event(ctx, map[string]any{"type": kind, "conversation": other, "text": "synthetic unrelated payload"})
			if got := strings.Count(f.eventTypes(), kind); got != before+1 {
				t.Errorf("unrelated conversation broadcast lost: %s (got %d, want %d)", kind, got, before+1)
			}
		})
	}
}
