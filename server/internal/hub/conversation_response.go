package hub

import (
	"net/http"
	"sync"

	"github.com/nwverse/persona/server/internal/apierr"
	"github.com/nwverse/persona/server/internal/i18n"
)

type conversationResponseFence struct {
	mu    sync.Mutex
	users int
}

// queueConversationBacklog commits one already-read transcript frame. The
// callback only enqueues locally; no network/body write holds this fence.
func (m *Machine) queueConversationBacklog(id string, queue func()) bool {
	unlock := m.lockConversationResponse(id)
	defer unlock()
	if m.conversationDeleted(id) {
		return false
	}
	queue()
	return true
}

// Fences exist only while a header commit or a deletion uses them, not for
// every id ever requested. RPC reads and response bodies hold no fence.
func (m *Machine) lockConversationResponse(id string) func() {
	m.responseMu.Lock()
	if m.responseFences == nil {
		m.responseFences = map[string]*conversationResponseFence{}
	}
	f := m.responseFences[id]
	if f == nil {
		f = &conversationResponseFence{}
		m.responseFences[id] = f
	}
	f.users++
	m.responseMu.Unlock()
	f.mu.Lock()
	return func() {
		f.mu.Unlock()
		m.responseMu.Lock()
		f.users--
		if f.users == 0 {
			delete(m.responseFences, id)
		}
		m.responseMu.Unlock()
	}
}

// A response is committed when its headers are written. A deletion cannot be
// acknowledged ahead of that commit and then let it start exposing old data.
// Bytes of a response already committed before deletion may still be in flight.
type conversationResponseWriter struct {
	http.ResponseWriter
	machine           *Machine
	id                string
	header            http.Header
	committed, denied bool
}

func (w *conversationResponseWriter) Header() http.Header         { return w.header }
func (w *conversationResponseWriter) Lang() i18n.Lang             { return i18n.OfWriter(w.ResponseWriter) }
func (w *conversationResponseWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }
func (w *conversationResponseWriter) WriteHeader(status int) {
	if w.committed {
		return
	}
	unlock := w.machine.lockConversationResponse(w.id)
	defer unlock()
	w.committed = true
	if w.machine.conversationDeleted(w.id) {
		w.denied = true
		apierr.WriteCode(w.ResponseWriter, http.StatusGone, "conversation_deleted", i18n.Err("assistant-conversation_delete.deleted"))
		return
	}
	for key, values := range w.header {
		w.ResponseWriter.Header()[key] = values
	}
	w.ResponseWriter.WriteHeader(status)
}
func (w *conversationResponseWriter) Write(body []byte) (int, error) {
	if !w.committed {
		w.WriteHeader(http.StatusOK)
	}
	if w.denied {
		return len(body), nil
	}
	return w.ResponseWriter.Write(body)
}
func (w *conversationResponseWriter) Flush() {
	if !w.committed {
		w.WriteHeader(http.StatusOK)
	}
	if !w.denied {
		_ = http.NewResponseController(w.ResponseWriter).Flush()
	}
}
