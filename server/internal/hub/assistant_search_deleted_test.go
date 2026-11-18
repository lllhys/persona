package hub

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/assistantwire"
	"github.com/nwverse/persona/server/internal/proto"
)

const deletedSearchReply = `{"results":[{"machine_id":"server","session_id":"trip","title":"private-search-title","hits":[{"snippet":"private-search-words"}]},{"machine_id":"server","session_id":"control","title":"control-title","hits":[{"snippet":"control-words"}]}],"partial":false,"indexed":{"done":2,"total":2}}`

type heldSearchBody struct {
	*httptest.ResponseRecorder
	entered, release chan struct{}
	ctx              context.Context
}

func (w *heldSearchBody) Write(body []byte) (int, error) {
	close(w.entered)
	select {
	case <-w.release:
		return w.ResponseRecorder.Write(body)
	case <-w.ctx.Done():
		return 0, w.ctx.Err()
	}
}

func TestAssistantSearchCommittedBodyDoesNotHoldDeletion(t *testing.T) {
	h, n := conversationsHub(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	w := &heldSearchBody{ResponseRecorder: httptest.NewRecorder(), entered: make(chan struct{}), release: make(chan struct{}), ctx: ctx}
	done := make(chan struct{})
	go func() {
		defer close(done)
		h.writeAssistantSearchReply(w, httptest.NewRequest("GET", "/api/v1/assistant/search?q=words", nil), assistantwire.AssistantID, assistantwire.HTTPResponse{Status: 200, Body: []byte(deletedSearchReply)})
	}()
	select {
	case <-w.entered:
	case <-ctx.Done():
		t.Fatal("search headers did not commit")
	}
	ack := make(chan error, 1)
	go func() {
		ack <- h.acceptConversationDeletionReply(assistantwire.AssistantID, httptest.NewRequest("DELETE", "/api/v1/assistant/conversations/trip", nil), []byte(`{"machine_id":"`+n.m.cfg.ID+`"}`), assistantwire.HTTPResponse{Status: 200, Body: []byte(`{"ok":true,"deleted":true}`)})
	}()
	select {
	case err := <-ack:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal("committed search body held deletion fence")
	}
	close(w.release)
	select {
	case <-done:
	case <-ctx.Done():
		t.Fatal("search body did not finish")
	}
	if w.Code != 200 || w.Body.String() != deletedSearchReply {
		t.Fatalf("already committed response changed: %d %s", w.Code, w.Body.String())
	}
}

func TestAssistantSearchDeletionReplyFence(t *testing.T) {
	for _, deleted := range []bool{false, true} {
		name := "unchanged multi-chat result"
		if deleted {
			name = "delete acknowledged before pending search headers"
		}
		t.Run(name, func(t *testing.T) {
			h, srv, _ := newBusHub(t)
			m := boardNode(t, h, "server", func(context.Context, string, json.RawMessage) (any, error) { return map[string]any{}, nil })
			m.setSessionList([]proto.Session{{MachineID: "server", SessionID: "trip", Assistant: true}, {MachineID: "server", SessionID: "control", Assistant: true}})
			entered, release := make(chan struct{}), make(chan struct{})
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			helloAssistant(t, h, srv, func(_ context.Context, method string, _ json.RawMessage) (any, error) {
				if method == assistantwire.MethodTools {
					return map[string]any{"tools": []any{}}, nil
				}
				close(entered)
				select {
				case <-release:
				case <-ctx.Done():
					return nil, ctx.Err()
				}
				return assistantwire.HTTPResponse{Status: http.StatusOK, Header: map[string][]string{"Content-Type": {"application/json"}, "X-Search-Meta": {"private-search-title"}}, Body: []byte(deletedSearchReply)}, nil
			})
			w := httptest.NewRecorder()
			done := make(chan struct{})
			go func() {
				defer close(done)
				h.relayHTTP(w, httptest.NewRequest("GET", "/api/v1/assistant/search?q=words", nil).WithContext(ctx), assistantwire.AssistantID, false, false)
			}()
			select {
			case <-entered:
			case <-ctx.Done():
				t.Fatal("search reply not prepared")
			}
			if deleted {
				err := h.acceptConversationDeletionReply(assistantwire.AssistantID, httptest.NewRequest("DELETE", "/api/v1/assistant/conversations/trip", nil), []byte(`{"machine_id":"server"}`), assistantwire.HTTPResponse{Status: http.StatusOK, Body: []byte(`{"ok":true,"deleted":true}`)})
				if err != nil {
					close(release)
					t.Fatal(err)
				}
			}
			close(release)
			select {
			case <-done:
			case <-ctx.Done():
				t.Fatal("search response did not finish")
			}
			if deleted {
				if w.Code != 409 || !strings.Contains(w.Body.String(), `"code":"search_changed"`) || !strings.Contains(w.Body.String(), `"retryable":true`) || strings.Contains(w.Body.String(), "private-search") || w.Header().Get("X-Search-Meta") != "" {
					t.Fatalf("deleted result escaped: %d %s %v", w.Code, w.Body.String(), w.Header())
				}
			} else if w.Code != 200 || w.Body.String() != deletedSearchReply {
				t.Fatalf("normal search changed: %d %s", w.Code, w.Body.String())
			}
		})
	}
}
