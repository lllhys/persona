package hub

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/assistantwire"
)

func TestPersonaBacklogQueueCommitOrdersDeletionAcknowledgement(t *testing.T) {
	h, n := conversationsHub(t)
	c := fsClient(h, roleFull)
	const id = "trip-0002"
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	checked, release := make(chan struct{}), make(chan struct{})
	resume := sync.OnceFunc(func() { close(release) })
	defer resume()
	var acknowledged, queuedAfterAck atomic.Bool
	queued, ack := make(chan bool, 1), make(chan error, 1)
	body, _ := json.Marshal(map[string]any{"type": "items", "machine_id": n.m.cfg.ID, "session_id": id, "items": []any{map[string]string{"text": "private pending backlog"}}})
	go func() {
		queued <- n.m.queueConversationBacklog(id, func() {
			// The final availability check is complete; the raw frame has not entered the client's queue.
			close(checked)
			select {
			case <-release:
			case <-ctx.Done():
				return
			}
			queuedAfterAck.Store(acknowledged.Load())
			c.queue(body)
		})
	}()
	select {
	case <-checked:
	case <-ctx.Done():
		t.Fatal("backlog did not finish its check")
	}
	go func() {
		request, _ := json.Marshal(map[string]string{"machine_id": n.m.cfg.ID})
		err := h.acceptConversationDeletionReply(assistantwire.AssistantID,
			httptest.NewRequest("DELETE", "/api/v1/assistant/conversations/"+id, nil), request,
			assistantwire.HTTPResponse{Status: 200, Body: []byte(`{"ok":true,"deleted":true}`)})
		acknowledged.Store(err == nil)
		ack <- err
	}()
	var ackDone bool
	var ackErr error
	// Either the unfenced deletion has already acknowledged (the regression), or
	// it is admitted behind the frame's commit fence. Observe that admission rather than sleeping.
	waitFor(t, "deletion at the backlog commit boundary", func() bool {
		select {
		case ackErr = <-ack:
			ackDone = true
			return true
		default:
		}
		n.m.responseMu.Lock()
		defer n.m.responseMu.Unlock()
		f := n.m.responseFences[id]
		return f != nil && f.users >= 2
	})
	resume()
	select {
	case committed := <-queued:
		if !committed {
			t.Error("the frame admitted before deletion never committed")
		}
	case <-ctx.Done():
		t.Fatal("backlog commit did not finish")
	}
	if !ackDone {
		select {
		case ackErr = <-ack:
		case <-ctx.Done():
			t.Fatal("deletion did not acknowledge after the queued frame")
		}
	}
	if ackErr != nil {
		t.Fatal(ackErr)
	}
	if queuedAfterAck.Load() {
		t.Fatal("raw transcript entered the client queue after deletion acknowledged")
	}
	if n.m.queueConversationBacklog(id, func() { t.Error("deleted conversation queued another frame") }) {
		t.Error("deleted conversation was admitted")
	}
	if !n.m.queueConversationBacklog("main-0001", func() { c.queue([]byte(`{"type":"synthetic control"}`)) }) {
		t.Error("ordinary conversation was blocked")
	}
}
