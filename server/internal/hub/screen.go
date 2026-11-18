package hub

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/coder/websocket"

	"github.com/nwverse/persona/server/internal/rpc"
)

// screenVideoConnect is GET …/screen/video/connect?pair=…: a binary WebSocket carrying the live-screen hardware video
// channel (#3954). It is the same gate as …/ports/5900/connect (a full device, cua or ego on for the Mac) and uses
// the same per-machine stream budget. It calls screen.dial instead of port.dial; the node binds the stream to the
// signed-in RFB stream carrying the same `pair`. The hub never parses the bytes.
//
// A refused dial is an HTTP status on the handshake (403 not signed in, 410 session gone, 429 busy, 503 unavailable),
// which a browser can't read. With `refusals=close` (the web, #8320) the hub accepts the WebSocket instead and closes
// it at once with 4000 + that status and the node's code as the reason, so the page can say why it shows VNC.
func (h *Hub) screenVideoConnect(w http.ResponseWriter, r *http.Request, m *Machine) {
	if principalOf(r.Context()).Role != roleFull {
		writeErr(w, http.StatusForbidden, errors.New("this device cannot open the screen"))
		return
	}
	if !h.screenAllowed(m) {
		writeErr(w, http.StatusForbidden, errors.New("this machine's screen is not shared"))
		return
	}
	pair := r.URL.Query().Get("pair")
	if pair == "" {
		writeErr(w, http.StatusBadRequest, errors.New("a pair is required"))
		return
	}
	mux := m.portStreams()
	if mux == nil {
		writeErr(w, http.StatusServiceUnavailable, fmt.Errorf("%s: %w", m.name(), errOffline))
		return
	}
	if mux.Len() >= maxPortStreams {
		writeErr(w, http.StatusTooManyRequests, errors.New("too many connections to this machine"))
		return
	}
	r, untrack := h.trackRelayed(r)
	defer untrack()
	if err := r.Context().Err(); err != nil {
		callErr(w, err)
		return
	}
	id := hubStreamIDs.next("v")
	if err := mux.Open(id); err != nil {
		writeErr(w, http.StatusServiceUnavailable, err)
		return
	}
	dctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	dial := map[string]any{"id": id, "pair": pair}
	if h.extReport().ScreenKeepAwake {
		dial["keep_awake"] = true // someone is watching: the Mac stays awake until this closes (node/awake.go)
	}
	if c, _, _ := h.extrasOf(m); c != nil && c.ScreenX264 {
		dial["x264"] = true // the user's own ffmpeg may encode, on a direct LAN route (#8859, node/screenx264.go)
	}
	link := h.askScreenLink(r, dial) // the video joins the pair's screen session (#6398)
	var res struct {
		Link int `json:"link"`
	}
	err := m.call(dctx, "screen.dial", dial, &res)
	cancel()
	if err != nil {
		mux.Abort(id, err)
		var re *rpc.Error
		if errors.As(err, &re) {
			if status := screenRefusalStatus(re.Code); status != 0 {
				if r.URL.Query().Get("refusals") == "close" {
					refuseByClose(w, r, status, re.Code)
					return
				}
				writeErr(w, status, err) // the client falls back to RFB
				return
			}
		}
		callErr(w, err)
		return
	}
	h.bridgeStream(w, r, mux, id, link && res.Link == 1)
}

// screenRefusalStatus is the HTTP status for a node's screen.dial refusal, 0 for any other error.
func screenRefusalStatus(code string) int {
	switch code {
	case "not_signed_in":
		return http.StatusForbidden
	case "session_gone":
		return http.StatusGone
	case "busy":
		return http.StatusTooManyRequests
	case "video_unavailable":
		return http.StatusServiceUnavailable
	}
	return 0
}

// refuseByClose answers a refused dial for a browser: the WebSocket opens and closes at once with 4000 + status and
// the node's code as the reason (an application close code, which a page's onclose can read).
func refuseByClose(w http.ResponseWriter, r *http.Request, status int, code string) {
	c, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true, Subprotocols: []string{"binary"}})
	if err != nil {
		return // Accept has answered the request itself
	}
	_ = c.Close(websocket.StatusCode(4000+status), code)
}
