package extmgr

// Phone Screen for the live screen (#9314, docs/extensions/computer-use.md "Phone Screen"): POST {pair, op, …} asks
// the Mac's node (screen.phone) to start the portrait virtual display for the live screen whose RFB stream carries
// `pair`, list the Mac's windows, put one on the display, or stop. The node keeps the session bound to that signed-in
// stream; this only relays, with the same gate as the clipboard (the plugin cua or ego on for the Mac, a full device).
// A refusal keeps the node's code (busy, not_signed_in, permission, unavailable, display, window_gone…) for the app.

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/nwverse/persona/server/internal/apierr"
	"github.com/nwverse/persona/server/internal/i18n"
	"github.com/nwverse/persona/server/internal/rpc"
)

// phoneScreenBodyMost caps the request: a handful of numbers and a pair.
const phoneScreenBodyMost = 4 << 10

// phoneScreenStatus is the HTTP status for a node refusal's code.
func phoneScreenStatus(code string) int {
	switch code {
	case "bad_request":
		return http.StatusBadRequest
	case "not_signed_in", "permission":
		return http.StatusForbidden
	case "busy":
		return http.StatusTooManyRequests
	case "no_session", "no_display", "window_gone", "not_found":
		return http.StatusNotFound
	case "unavailable":
		return http.StatusServiceUnavailable
	}
	return http.StatusConflict
}

func (h *Manager) computerPhoneScreen(w http.ResponseWriter, r *http.Request, m *Machine) {
	if !h.portAllowed(m, screenPort) {
		writeErr(w, http.StatusNotFound, i18n.Err("hub-computer.err_clipboard_off"))
		return
	}
	if !m.isOnline() {
		writeErr(w, http.StatusConflict, i18n.Err("hub-computer.err_offline"))
		return
	}
	var in struct {
		Pair   string  `json:"pair"`
		Op     string  `json:"op"`
		Width  float64 `json:"width,omitempty"`
		Height float64 `json:"height,omitempty"`
		PID    int32   `json:"pid,omitempty"`
		Window uint32  `json:"window,omitempty"`
	}
	if err := decodeJSONBody(w, r, &in, phoneScreenBodyMost); err != nil {
		h.writeComputerBodyErr(w, r, err)
		return
	}
	switch in.Op {
	case "start", "windows", "place", "stop":
	default:
		writeErrCode(w, http.StatusBadRequest, "bad_request", errors.New("op must be start, windows, place or stop"))
		return
	}
	if in.Pair == "" {
		writeErrCode(w, http.StatusBadRequest, "bad_request", errors.New("pair is required"))
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 45*time.Second)
	defer cancel()
	var out map[string]any
	if err := m.call(ctx, "screen.phone", in, &out); err != nil {
		var re *rpc.Error
		switch {
		case errors.As(err, &re) && re.Code != "":
			msg := re.Message
			if msg == "" {
				msg = re.Code
			}
			writeErrorBody(w, phoneScreenStatus(re.Code), apierr.Body(phoneScreenStatus(re.Code), re.Code, errors.New(msg)))
		case strings.Contains(err.Error(), "unknown method"): // a node from before Phone Screen
			writeErrorBody(w, http.StatusServiceUnavailable, apierr.Body(http.StatusServiceUnavailable, "unavailable", err))
		default:
			h.writeComputerErr(w, r, http.StatusConflict, err)
		}
		return
	}
	if in.Op != "windows" { // window titles are the user's: never logged
		h.logf("[%s] phone screen %s", m.cfg.ID, in.Op)
	}
	if out == nil {
		out = map[string]any{}
	}
	writeJSON(w, out)
}
