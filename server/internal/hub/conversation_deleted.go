package hub

import (
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"sort"
	"strings"

	"github.com/nwverse/persona/server/internal/assistantwire"
	"github.com/nwverse/persona/server/internal/i18n"
	"github.com/nwverse/persona/server/internal/proto"
	"github.com/nwverse/persona/server/internal/rpc"
)

// The assistant manager owns deletion. The hub enforces the scrubbed tombstone
// it receives through fleet.sessions at its transport boundaries.
func (m *Machine) conversationDeleted(id string) bool {
	row, ok := m.session(id)
	return ok && row.IsDeleted()
}

func (m *Machine) deletedCall(method string, params any) error {
	if method == "session.unwatch" {
		return nil
	}
	marked := false
	for _, row := range m.keptSessions() {
		if row.IsDeleted() {
			marked = true
			break
		}
	}
	if !marked {
		return nil
	}
	raw, _ := json.Marshal(params)
	var p struct {
		SessionID string `json:"session_id"`
		Resume    string `json:"resume_session_id"`
		New       string `json:"new_session_id"`
	}
	_ = json.Unmarshal(raw, &p)
	for _, id := range [...]string{p.SessionID, p.Resume, p.New} {
		if id != "" && m.conversationDeleted(id) {
			return &rpc.Error{Code: "conversation_deleted", Message: i18n.T("en", "assistant-conversation_delete.deleted")}
		}
	}
	return nil
}

func (h *Hub) conversationDeleted(machine, id string) bool {
	m := h.localMachine(machine)
	return m != nil && m.conversationDeleted(id)
}

func deletedConversationEvent(machine, id string) map[string]any {
	return map[string]any{"type": "conversation_deleted", "machine_id": machine, "session_id": id}
}

func (h *Hub) conversationDeletedPush(m *Machine, id string) {
	key := [2]string{m.cfg.ID, id}
	owner := h.machineOwner(m.cfg.ID)
	h.mu.Lock()
	if h.deletedConversationContent == nil {
		h.deletedConversationContent = map[[2]string]bool{}
	}
	h.deletedConversationContent[[2]string{owner, id}] = true
	if h.events != nil {
		for i := range h.events.events {
			e := &h.events.events[i]
			var v struct {
				Type         string `json:"type"`
				Conversation string `json:"conversation"`
				Manager      string `json:"manager"`
			}
			_ = json.Unmarshal(e.Body, &v)
			if v.Manager == "" {
				v.Manager = assistantwire.AssistantID
			}
			if v.Conversation == id && v.Manager == owner && h.deletedConversationEventLocked(v.Type, e.Body) {
				replacement, _ := json.Marshal(deletedConversationEvent(m.cfg.ID, id))
				replacement = stampEvent(replacement, e.Seq)
				h.events.bytes += len(replacement) - len(e.Body)
				e.Body = replacement
			}
		}
	}
	delete(h.deltas, key)
	watchers := 0
	for c := range h.clients {
		c.mu.Lock()
		if c.subs[key] {
			delete(c.subs, key)
			watchers++
		}
		c.mu.Unlock()
	}
	h.mu.Unlock()
	for i := 0; i < watchers; i++ {
		m.watcherLeft(id)
	}
	if watchers > 0 {
		go m.syncUnwatch(id)
	}
	h.broadcast(deletedConversationEvent(m.cfg.ID, id))
}

func (h *Hub) deletedConversationEventLocked(kind string, raw []byte) bool {
	switch kind {
	case "assistant_feed", "assistant_view", "assistant_view_action", "assistant_view_block":
	default:
		return false
	}
	var event struct {
		Conversation string `json:"conversation"`
		Manager      string `json:"manager"`
	}
	if json.Unmarshal(raw, &event) != nil {
		return false
	}
	if event.Manager == "" {
		event.Manager = assistantwire.AssistantID
	}
	return h.deletedConversationContent[[2]string{event.Manager, event.Conversation}]
}

// A delayed pre-delete fleet snapshot must not undo an acknowledged deletion.
func (m *Machine) overlayDeletedLocked(list []proto.Session) []proto.Session {
	list = slices.Clone(list)
	seen := map[string]bool{}
	for i := range list {
		if row, ok := m.deletedConversations[list[i].SessionID]; ok {
			list[i] = row
			seen[row.SessionID] = true
		}
	}
	var missing []string
	for id := range m.deletedConversations {
		if !seen[id] {
			missing = append(missing, id)
		}
	}
	sort.Strings(missing)
	for _, id := range missing {
		list = append(list, m.deletedConversations[id])
	}
	return list
}

func (m *Machine) rememberConversationDeleted(row proto.Session) bool {
	unlock := m.lockConversationResponse(row.SessionID)
	defer unlock()
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.deletedConversations == nil {
		m.deletedConversations = map[string]proto.Session{}
	}
	_, known := m.deletedConversations[row.SessionID]
	m.deletedConversations[row.SessionID] = row
	m.sessions = m.overlayDeletedLocked(m.sessions)
	m.raws, m.rawsOf = nil, keptList{}
	return !known
}

// Apply the owning manager's successful deletion to the hub's transport mirror
// before returning 200. fleet.sessions is a notification, not an applied ack.
func (h *Hub) acceptConversationDeletionReply(manager string, r *http.Request, requestBody []byte, res assistantwire.HTTPResponse) error {
	id, ok := strings.CutPrefix(r.URL.Path, "/api/v1/assistant/conversations/")
	if !ok || id == "" || strings.Contains(id, "/") || r.Method != http.MethodDelete || res.Status != http.StatusOK {
		return nil
	}
	var request struct {
		Machine string `json:"machine_id"`
	}
	var result struct {
		OK      bool `json:"ok"`
		Deleted bool `json:"deleted"`
	}
	if json.Unmarshal(requestBody, &request) != nil || json.Unmarshal(res.Body, &result) != nil || !result.OK || !result.Deleted {
		return errors.New("invalid conversation deletion acknowledgement")
	}
	m := h.localMachine(request.Machine)
	if m == nil || h.machineOwner(m.cfg.ID) != manager {
		return errors.New("conversation deletion acknowledgement belongs to another manager")
	}
	row := proto.Session{MachineID: m.cfg.ID, SessionID: id, Assistant: true, Status: proto.StatusOffline, Title: i18n.T(h.requestLang(r), "assistant-conversation_delete.deleted")}
	row.SetDeleted(true)
	m.rememberConversationDeleted(row)
	h.conversationDeletedPush(m, id)
	h.broadcastSessions(m, m.keptSessions())
	return nil
}
