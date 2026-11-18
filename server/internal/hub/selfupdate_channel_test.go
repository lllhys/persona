package hub

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"

	"github.com/nwverse/persona/server/internal/update"
)

func TestBetaSettingsPersistAndInvalidateTheOtherChannel(t *testing.T) {
	h := newTestHub(t)
	h.build = update.Make("v1", update.Release, "202611171200", "https://github.com/nwverse/persona/releases/latest/download")
	h.store = &settingsStore{vals: map[string][]byte{}}
	h.upd.release = &update.Published{Manifest: &update.Manifest{Version: "0.7.0", Build: 1}}
	srv := httptest.NewServer(h.routes())
	defer srv.Close()
	call := func(body string) (int, updateView) {
		req := httptest.NewRequest(http.MethodPut, "/api/v1/hub/update/settings", strings.NewReader(body))
		req.RemoteAddr = net.JoinHostPort(netip.MustParseAddr("127.0.0.1").String(), "1")
		req.Header.Set("Authorization", "Bearer "+h.cfg.Token)
		w := httptest.NewRecorder()
		srv.Config.Handler.ServeHTTP(w, req)
		var view updateView
		_ = json.Unmarshal(w.Body.Bytes(), &view)
		return w.Code, view
	}
	code, view := call(`{"source":"beta"}`)
	if code != http.StatusOK || view.Source != update.ChannelBeta || !strings.HasSuffix(view.UpdateURL, "/releases/download/beta-channel") {
		t.Fatalf("beta selection: %d %+v", code, view)
	}
	if h.upd.release != nil {
		t.Fatal("kept the stable candidate after choosing beta")
	}
	var saved updateSettings
	if _, err := h.store.Setting(context.Background(), updateSettingsKey, &saved); err != nil || saved.Source != update.ChannelBeta {
		t.Fatalf("persisted selection: %+v, %v", saved, err)
	}
	if got := h.androidUpdateDir(); !strings.HasSuffix(got, "apps/android/beta") {
		t.Fatalf("Android beta cache: %q", got)
	}
	h.upd.release = &update.Published{Manifest: &update.Manifest{Version: "0.7.0-beta.1", Build: 2}}
	if code, view = call(`{"source":"release"}`); code != http.StatusOK || view.Source != "release" || !strings.HasSuffix(view.UpdateURL, "/releases/latest/download") {
		t.Fatalf("stable rollback: %d %+v", code, view)
	}
	if h.upd.release != nil || h.androidUpdateDir() == "" || strings.HasSuffix(h.androidUpdateDir(), "/beta") {
		t.Fatal("kept beta state after returning to stable")
	}
}

func TestBetaInstallSourcePinsTheSelectedImmutableTag(t *testing.T) {
	h := newTestHub(t)
	h.build = update.Make("v1", update.Release, "202611171200", "https://github.com/nwverse/persona/releases/latest/download")
	h.upd.settings.Source = update.ChannelBeta
	m := &update.Manifest{Version: "0.7.0-beta.1", Channel: update.ChannelBeta}
	source, err := h.upd.installSource(m)
	if err != nil || source.url != "https://github.com/nwverse/persona/releases/download/v0.7.0-beta.1" {
		t.Fatalf("beta install source %q, %v", source.url, err)
	}
	if _, err := h.upd.installSource(&update.Manifest{Version: "0.7.0"}); err == nil {
		t.Fatal("accepted a stable manifest for a beta install")
	}
}

func TestBetaSettingsRejectCustomSourcesAndBusySwitches(t *testing.T) {
	for _, custom := range []string{"file:///private/releases", "https://mirror.example.com/dl"} {
		h := newTestHub(t)
		h.build = update.Make("v1", update.Release, "202611171200", custom)
		srv := httptest.NewServer(h.routes())
		req := httptest.NewRequest(http.MethodPut, "/api/v1/hub/update/settings", strings.NewReader(`{"source":"beta"}`))
		req.RemoteAddr = net.JoinHostPort(netip.MustParseAddr("127.0.0.1").String(), "1")
		req.Header.Set("Authorization", "Bearer "+h.cfg.Token)
		w := httptest.NewRecorder()
		srv.Config.Handler.ServeHTTP(w, req)
		srv.Close()
		if w.Code != http.StatusConflict || h.upd.settings.Source == update.ChannelBeta {
			t.Fatalf("custom source %s: %d", custom, w.Code)
		}
	}
	h := newTestHub(t)
	h.build = update.Make("v1", update.Release, "202611171200", "https://github.com/nwverse/persona/releases/latest/download")
	h.upd.settings.Source, h.upd.running = update.ChannelBeta, true
	srv := httptest.NewServer(h.routes())
	defer srv.Close()
	req := httptest.NewRequest(http.MethodPut, "/api/v1/hub/update/settings", strings.NewReader(`{"source":"release"}`))
	req.RemoteAddr = net.JoinHostPort(netip.MustParseAddr("127.0.0.1").String(), "1")
	req.Header.Set("Authorization", "Bearer "+h.cfg.Token)
	w := httptest.NewRecorder()
	srv.Config.Handler.ServeHTTP(w, req)
	if w.Code != http.StatusConflict || h.upd.settings.Source != update.ChannelBeta {
		t.Fatalf("busy switch: %d", w.Code)
	}
}
