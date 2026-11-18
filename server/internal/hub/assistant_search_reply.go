package hub

import (
	"encoding/json"
	"errors"
	"net/http"
	"sort"

	"github.com/nwverse/persona/server/internal/apierr"
	"github.com/nwverse/persona/server/internal/assistantwire"
	"github.com/nwverse/persona/server/internal/i18n"
)

// Search is an aggregate reply, not a single conversation route. The manager
// filters before serializing; a deletion can still win while that reply travels
// over the bus. Refuse that stale reply rather than invent new aggregate counts.
func (h *Hub) writeAssistantSearchReply(w http.ResponseWriter, r *http.Request, manager string, res assistantwire.HTTPResponse) bool {
	if r.Method != http.MethodGet || r.URL.Path != "/api/v1/assistant/search" || res.Status != http.StatusOK {
		return false
	}
	var answer struct {
		Results []struct {
			Machine string `json:"machine_id"`
			ID      string `json:"session_id"`
		} `json:"results"`
	}
	if json.Unmarshal(res.Body, &answer) != nil {
		writeErr(w, http.StatusBadGateway, errors.New("invalid assistant search response"))
		return true
	}
	targets := map[[2]string]*Machine{}
	changed := false
	for _, result := range answer.Results {
		m := h.localMachine(result.Machine)
		if m == nil || result.ID == "" || h.machineOwner(m.cfg.ID) != manager {
			writeErr(w, http.StatusBadGateway, errors.New("invalid assistant search response"))
			return true
		}
		targets[[2]string{m.cfg.ID, result.ID}] = m
	}
	keys := make([][2]string, 0, len(targets))
	for key := range targets {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i][0] != keys[j][0] {
			return keys[i][0] < keys[j][0]
		}
		return keys[i][1] < keys[j][1]
	})
	body := res.Body
	func() {
		for _, key := range keys {
			defer targets[key].lockConversationResponse(key[1])()
		}
		for _, key := range keys {
			if targets[key].conversationDeleted(key[1]) {
				changed = true
			}
		}
		status := res.Status
		if changed {
			status = http.StatusConflict
			body, _ = json.Marshal(apierr.LocalizedBody(status, "search_changed", i18n.Err("assistant-search.changed"), i18n.OfWriter(w)))
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
		} else {
			for key, values := range res.Header {
				if !hopHeaders[http.CanonicalHeaderKey(key)] {
					w.Header()[http.CanonicalHeaderKey(key)] = values
				}
			}
		}
		w.WriteHeader(status)
	}()
	// Already-committed bodies can finish; slow readers hold no session fence.
	_, _ = w.Write(body)
	return true
}
