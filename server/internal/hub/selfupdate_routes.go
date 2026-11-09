package hub

import (
	"context"
	"errors"
	"net/http"
	"slices"

	"github.com/nwverse/persona/server/internal/i18n"
)

func (u *updater) routes(api *http.ServeMux) {
	api.HandleFunc("GET /api/v1/hub/update", func(w http.ResponseWriter, r *http.Request) {
		if err := u.load(r.Context()); err != nil {
			writeErr(w, http.StatusServiceUnavailable, i18n.Err("http-status.unavailable"))
			return
		}
		writeJSON(w, u.viewIn(u.h.requestLang(r)))
	})
	api.HandleFunc("POST /api/v1/hub/update/check", func(w http.ResponseWriter, r *http.Request) {
		if err := u.load(r.Context()); err != nil {
			writeErr(w, http.StatusServiceUnavailable, i18n.Err("http-status.unavailable"))
			return
		}
		if _, err := u.check(r.Context()); err != nil && !errors.Is(err, errNotRelease) {
			writeErr(w, http.StatusBadGateway, err)
			return
		}
		if u.wantsAuto() {
			go u.install(context.WithoutCancel(r.Context()), true)
		} else {
			u.noteHeld(r.Context())
		}
		writeJSON(w, u.viewIn(u.h.requestLang(r)))
	})
	api.HandleFunc("POST /api/v1/hub/update", func(w http.ResponseWriter, r *http.Request) {
		if err := u.load(r.Context()); err != nil {
			writeErr(w, http.StatusServiceUnavailable, i18n.Err("http-status.unavailable"))
			return
		}
		var body struct {
			Now bool `json:"now"`
		}
		// No body (or one that does not decode) is an ordinary update; only an oversized one is refused.
		if err := decodeJSONBody(w, r, &body, 4<<10); bodyTooLarge(err) {
			writeBodyErr(w, err, err)
			return
		}
		// The update outlives this request: it runs on the hub's own context.
		if err := u.begin(context.WithoutCancel(r.Context()), body.Now); err != nil {
			writeErr(w, http.StatusConflict, err)
			return
		}
		writeJSON(w, u.viewIn(u.h.requestLang(r)))
	})
	api.HandleFunc("PUT /api/v1/hub/update/settings", func(w http.ResponseWriter, r *http.Request) {
		if err := u.load(r.Context()); err != nil {
			writeErr(w, http.StatusServiceUnavailable, i18n.Err("http-status.unavailable"))
			return
		}
		var body struct {
			Auto            *bool `json:"auto"`
			AutoNoticeSeen  *bool `json:"auto_notice_seen"`
			AutoUpdateNodes *bool `json:"auto_update_nodes"`
		}
		if err := decodeJSONBody(w, r, &body, 4<<10); err != nil {
			writeBodyErr(w, err, err)
			return
		}
		u.mu.Lock()
		if err := u.loadLocked(r.Context()); err != nil {
			u.mu.Unlock()
			writeErr(w, http.StatusServiceUnavailable, i18n.Err("http-status.unavailable"))
			return
		}
		prev := u.settings
		if body.AutoUpdateNodes != nil {
			enabled := *body.AutoUpdateNodes
			u.settings.AutoUpdateNodes = &enabled
		}
		if body.Auto != nil {
			a := *body.Auto
			u.settings.Auto = &a
			u.settings.AutoNoticeSeen = true // they chose: no need to tell them again
		}
		if body.AutoNoticeSeen != nil {
			u.settings.AutoNoticeSeen = *body.AutoNoticeSeen
		}
		err := u.saveLocked(r.Context())
		if err != nil {
			// Not saved: an update waiting for idle and the view sent to the apps keep acting on the stored choice.
			u.settings = prev
		}
		u.mu.Unlock()
		if err != nil {
			writeErr(w, http.StatusServiceUnavailable, i18n.Err("http-status.unavailable"))
			return
		}
		if body.AutoUpdateNodes != nil {
			u.h.queueDevicesConfig(*body.AutoUpdateNodes)
		}
		writeJSON(w, u.viewIn(u.h.requestLang(r)))
	})
	api.HandleFunc("GET /api/v1/hub/update/history", func(w http.ResponseWriter, r *http.Request) {
		if err := u.load(r.Context()); err != nil {
			writeErr(w, http.StatusServiceUnavailable, i18n.Err("http-status.unavailable"))
			return
		}
		u.mu.Lock()
		out := slices.Clone(u.settings.History)
		u.mu.Unlock()
		slices.Reverse(out)
		if out == nil {
			out = []updateRecord{}
		}
		writeJSON(w, map[string]any{"history": out})
	})
}
