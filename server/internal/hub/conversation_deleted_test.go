package hub

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/nwverse/persona/server/internal/proto"
)

func TestPersonaDeletedTransport(t *testing.T) {
	h, n := conversationsHub(t)
	id := "trip-0002"
	c := fsClient(h, roleFull)
	c.subs[[2]string{n.m.cfg.ID, id}] = true
	n.m.watcherJoined(id)
	rows := n.m.sessionList()
	for i := range rows {
		if rows[i].SessionID == id {
			rows[i] = proto.Session{MachineID: n.m.cfg.ID, SessionID: id, Assistant: true, Status: proto.StatusOffline}
			rows[i].SetDeleted(true)
		}
	}
	reportSessions(h, n.m, rows...)
	if c.subs[[2]string{n.m.cfg.ID, id}] {
		t.Fatal("deleted chat retains subscription")
	}
	found := false
	for len(c.out) > 0 {
		var msg map[string]any
		_ = json.Unmarshal(<-c.out, &msg)
		if msg["type"] == "conversation_deleted" && msg["session_id"] == id {
			found = true
		}
	}
	if !found {
		t.Fatal("no deletion event")
	}
	reportSessions(h, n.m, rows...)
	for len(c.out) > 0 {
		if raw := <-c.out; strings.Contains(string(raw), `"type":"conversation_deleted"`) {
			t.Fatalf("unchanged tombstone rebroadcast: %s", raw)
		}
	}
	if strings.Contains(ids(h.shown(n.m.cfg.ID, n.m.keptSessions())), id) {
		t.Fatal("deleted conversation listed")
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/sessions/{mid}/{sid}/items", h.withMachine(h.sessionItems))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest("GET", "/api/v1/sessions/"+n.m.cfg.ID+"/"+id+"/items", nil))
	if w.Code != 410 || !strings.Contains(w.Body.String(), `"code":"conversation_deleted"`) {
		t.Fatalf("old link: %d %s", w.Code, w.Body.String())
	}
	for _, method := range []string{"session.items", "session.send", "session.resume", "assistant.send", "session.watch"} {
		if err := n.m.call(context.Background(), method, map[string]any{"session_id": id}, nil); err == nil {
			t.Errorf("%s reached deleted chat", method)
		}
	}
	say(h, c, map[string]any{"type": "subscribe", "machine_id": n.m.cfg.ID, "session_id": id})
	if c.subs[[2]string{n.m.cfg.ID, id}] {
		t.Fatal("deleted chat subscribed again")
	}
	for len(c.out) > 0 {
		<-c.out
	}
	h.itemsToSubscribers(n.m.cfg.ID, id, []proto.Item{{Text: "must not be sent"}}, itemSpan{}, false)
	h.sendBacklog(context.Background(), c, n.m, id, 0)
	for len(c.out) > 0 {
		if raw := <-c.out; strings.Contains(string(raw), `"type":"items"`) {
			t.Fatalf("deleted backlog: %s", raw)
		}
	}
}
