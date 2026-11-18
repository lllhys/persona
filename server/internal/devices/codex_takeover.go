package devices

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"time"

	"github.com/nwverse/persona/server/internal/apierr"
	"github.com/nwverse/persona/server/internal/assistantwire"
	"github.com/nwverse/persona/server/internal/i18n"
	"github.com/nwverse/persona/server/internal/proto"
)

func takeoverClient(w http.ResponseWriter, r *http.Request) bool {
	if r.Header.Get(assistantwire.RoleHeader) != "full" {
		writeErrCode(w, http.StatusForbidden, "user_confirmation_required", i18n.Err("codex-takeover.user_only"))
		return false
	}
	return true
}

func (d *Devices) prepareCodexTakeover(w http.ResponseWriter, r *http.Request, m *Machine) {
	if !takeoverClient(w, r) {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	var out json.RawMessage
	if err := m.call(ctx, "session.codex.takeover.prepare", map[string]any{"session_id": r.PathValue("sid")}, &out); err != nil {
		apierr.Call(w, err)
		return
	}
	writeJSON(w, out)
}

func (d *Devices) commitCodexTakeover(w http.ResponseWriter, r *http.Request, m *Machine) {
	if !takeoverClient(w, r) {
		return
	}
	var p struct {
		Token       string `json:"token"`
		ConfirmStop bool   `json:"confirm_stop"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&p); err != nil || p.Token == "" {
		writeErrCode(w, http.StatusBadRequest, "bad_request", i18n.Err("codex-takeover.token_required"))
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 100*time.Second)
	defer cancel()
	var out struct {
		Session proto.Session `json:"session"`
	}
	if err := m.call(ctx, "session.codex.takeover.commit", map[string]any{
		"session_id": r.PathValue("sid"), "token": p.Token, "confirm_stop": p.ConfirmStop,
	}, &out); err != nil {
		apierr.Call(w, err)
		return
	}
	out.Session.MachineID = m.cfg.ID
	writeJSON(w, out)
}
