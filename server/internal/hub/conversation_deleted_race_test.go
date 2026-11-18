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

// Finishing a read does not commit a response. The last boundary before headers
// must observe deletion; a response committed earlier is already in flight.
func TestPersonaDeletedHTTPHeaderCommitFence(t *testing.T) {
	for _, committed := range []bool{false, true} {
		name := "read finished before deletion, headers pending"
		if committed {
			name = "headers committed before deletion"
		}
		t.Run(name, func(t *testing.T) {
			h, n := conversationsHub(t)
			const id, secret = "trip-0002", "synthetic-private-response"
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			read, release, done := make(chan struct{}), make(chan struct{}, 1), make(chan struct{})
			defer close(release)
			mux := http.NewServeMux()
			mux.HandleFunc("GET /api/v1/sessions/{mid}/{sid}/items", h.withMachine(func(w http.ResponseWriter, r *http.Request, _ *Machine) {
				// The read is complete; only its response remains to be committed.
				body := []byte(secret)
				w.Header().Set("X-Synthetic-Private", secret)
				if committed {
					w.WriteHeader(http.StatusOK)
				}
				close(read)
				select {
				case <-release:
				case <-r.Context().Done():
					return
				}
				_, _ = w.Write(body)
			}))
			w := httptest.NewRecorder()
			go func() {
				defer close(done)
				mux.ServeHTTP(w, httptest.NewRequest("GET", "/api/v1/sessions/"+n.m.cfg.ID+"/"+id+"/items", nil).WithContext(ctx))
			}()
			select {
			case <-read:
			case <-ctx.Done():
				t.Fatal("synthetic read did not finish")
			}
			ack := make(chan error, 1)
			go func() {
				body, _ := json.Marshal(map[string]string{"machine_id": n.m.cfg.ID})
				ack <- h.acceptConversationDeletionReply(assistantwire.AssistantID,
					httptest.NewRequest("DELETE", "/api/v1/assistant/conversations/"+id, nil), body,
					assistantwire.HTTPResponse{Status: http.StatusOK, Body: []byte(`{"ok":true,"deleted":true}`)})
			}()
			select {
			case err := <-ack:
				if err != nil {
					t.Fatal(err)
				}
			case <-ctx.Done():
				t.Fatal("deletion waited for uncommitted reads or an already-committed response body")
			}
			// Acknowledge deletion before allowing the handler's first body write.
			release <- struct{}{}
			select {
			case <-done:
			case <-ctx.Done():
				t.Fatal("response did not finish")
			}
			if committed {
				if w.Code != http.StatusOK || w.Body.String() != secret || w.Header().Get("X-Synthetic-Private") != secret {
					t.Fatalf("already-committed control changed: %d %s", w.Code, w.Body.String())
				}
			} else if w.Code != http.StatusGone || !strings.Contains(w.Body.String(), `"code":"conversation_deleted"`) ||
				strings.Contains(w.Body.String(), secret) || w.Header().Get("X-Synthetic-Private") != "" {
				t.Fatalf("uncommitted private response escaped: status %d, headers %v, body %s", w.Code, w.Header(), w.Body.String())
			}
		})
	}
}

func applyDeletionReply(t *testing.T, h *Hub, machine, id string) {
	t.Helper()
	r := httptest.NewRequest("DELETE", "/api/v1/assistant/conversations/"+id, nil)
	body, _ := json.Marshal(map[string]string{"machine_id": machine})
	res := assistantwire.HTTPResponse{Status: http.StatusOK, Body: []byte(`{"ok":true,"deleted":true}`)}
	if err := h.acceptConversationDeletionReply(assistantwire.AssistantID, r, body, res); err != nil {
		t.Fatal(err)
	}
}

func TestPersonaDeleteReplyFencesBeforeFleetNotification(t *testing.T) {
	h, n := conversationsHub(t)
	const id = "trip-0002"
	stale := append([]proto.Session(nil), n.m.sessionList()...)
	c := fsClient(h, roleFull)
	key := [2]string{n.m.cfg.ID, id}
	c.subs[key] = true
	n.m.watcherJoined(id)
	h.rememberLiveText(n.m.cfg.ID, id, "synthetic retained live text")
	// No deleted fleet.sessions notification is delivered in this test.
	applyDeletionReply(t, h, n.m.cfg.ID, id)
	if !n.m.conversationDeleted(id) || c.subs[key] {
		t.Fatal("successful DELETE did not synchronously fence the old subscription")
	}
	h.mu.Lock()
	live := h.deltas[key]
	h.mu.Unlock()
	if live != "" {
		t.Fatal("successful DELETE retained live text")
	}
	assertGone := func() {
		t.Helper()
		mux := http.NewServeMux()
		mux.HandleFunc("GET /api/v1/sessions/{mid}/{sid}/items", h.withMachine(h.sessionItems))
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest("GET", "/api/v1/sessions/"+n.m.cfg.ID+"/"+id+"/items", nil))
		if w.Code != http.StatusGone || !strings.Contains(w.Body.String(), `"code":"conversation_deleted"`) {
			t.Fatalf("immediate old-link read: %d %s", w.Code, w.Body.String())
		}
	}
	assertGone()
	// An already encoded complete inventory may arrive after the reply.
	reportSessions(h, n.m, stale...)
	assertGone()
	row, ok := n.m.session(id)
	if !ok || !row.IsDeleted() || row.LastText != "" || row.Cwd != "" {
		t.Fatalf("stale fleet revived content: %+v", row)
	}
	if err := n.m.deletedCall("session.items", map[string]any{"session_id": "main-0001"}); err != nil {
		t.Fatalf("unrelated conversation blocked: %v", err)
	}
}

func TestPersonaDeleteReplyRequiresExactSuccessfulOwnedOperation(t *testing.T) {
	for _, tc := range []struct {
		name, method, path, manager, machine, response string
		status                                         int
	}{
		{"read", "GET", "/api/v1/assistant/conversations/trip-0002", assistantwire.AssistantID, "server", `{"ok":true,"deleted":true}`, 200},
		{"child route", "DELETE", "/api/v1/assistant/conversations/trip-0002/model", assistantwire.AssistantID, "server", `{"ok":true,"deleted":true}`, 200},
		{"failed", "DELETE", "/api/v1/assistant/conversations/trip-0002", assistantwire.AssistantID, "server", `{"ok":true,"deleted":true}`, 409},
		{"unconfirmed", "DELETE", "/api/v1/assistant/conversations/trip-0002", assistantwire.AssistantID, "server", `{"ok":true}`, 200},
		{"other manager", "DELETE", "/api/v1/assistant/conversations/trip-0002", "different-manager", "server", `{"ok":true,"deleted":true}`, 200},
		{"other machine", "DELETE", "/api/v1/assistant/conversations/trip-0002", assistantwire.AssistantID, "missing-machine", `{"ok":true,"deleted":true}`, 200},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h, n := conversationsHub(t)
			body, _ := json.Marshal(map[string]string{"machine_id": tc.machine})
			_ = h.acceptConversationDeletionReply(tc.manager, httptest.NewRequest(tc.method, tc.path, nil), body,
				assistantwire.HTTPResponse{Status: tc.status, Body: []byte(tc.response)})
			if n.m.conversationDeleted("trip-0002") {
				t.Fatal("an unrelated, failed or unauthenticated reply deleted the conversation")
			}
		})
	}
}

func TestPersonaDeletedBroadcastsAndReplayCannotExposeContent(t *testing.T) {
	h, n := conversationsHub(t)
	c := fsClient(h, roleFull)
	const id, secret = "trip-0002", "synthetic stored deleted feed text"
	message := func(kind, conversation, text string) json.RawMessage {
		b, _ := json.Marshal(map[string]any{"type": kind, "conversation": conversation, "text": text})
		return b
	}
	if err := h.relayBroadcastFrom(assistantwire.AssistantID, message("assistant_feed", id, secret)); err != nil {
		t.Fatal(err)
	}
	h.mu.Lock()
	var oldSequence int64
	for _, event := range h.events.events {
		if strings.Contains(string(event.Body), secret) {
			oldSequence = event.Seq
		}
	}
	h.mu.Unlock()
	if oldSequence == 0 {
		t.Fatal("the nondeleted feed control was never logged")
	}
	for len(c.out) > 0 {
		<-c.out
	}
	applyDeletionReply(t, h, n.m.cfg.ID, id)
	for len(c.out) > 0 {
		<-c.out
	}
	for _, kind := range []string{"assistant_feed", "assistant_view", "assistant_view_action", "assistant_view_block"} {
		_ = h.relayBroadcastFrom(assistantwire.AssistantID, message(kind, id, secret))
		if err := h.relayBroadcastFrom(assistantwire.AssistantID, message(kind, "main-0001", "unrelated control")); err != nil {
			t.Fatal(err)
		}
		seenControl := false
		for len(c.out) > 0 {
			raw := <-c.out
			if strings.Contains(string(raw), secret) {
				t.Errorf("late %s escaped: %s", kind, raw)
			}
			seenControl = seenControl || strings.Contains(string(raw), "unrelated control")
		}
		if !seenControl {
			t.Errorf("unrelated %s was suppressed", kind)
		}
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	keptSequence := false
	for _, event := range h.events.events {
		if strings.Contains(string(event.Body), secret) {
			t.Error("reconnect replay retains deleted feed text")
		}
		if event.Seq == oldSequence {
			keptSequence = true
			var marker struct {
				Type     string `json:"type"`
				Sequence int64  `json:"event_seq"`
			}
			if json.Unmarshal(event.Body, &marker) != nil || marker.Type != "conversation_deleted" || marker.Sequence != oldSequence {
				t.Errorf("redacted replay lost its sequence: %s", event.Body)
			}
		}
	}
	if !keptSequence {
		t.Error("deletion removed the replay sequence instead of retaining a tombstone")
	}
}
