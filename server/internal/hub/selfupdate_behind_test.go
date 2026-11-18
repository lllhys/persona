package hub

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/nwverse/persona/server/internal/update"
)

// The hub of #8108: deployed from master 150 commits after v0.5.3, by deploy.sh with no publish.update_url.
const (
	behindHubVersion = "v0.5.3-150-gc0908bfa8-202611141806"
	behindHubBuild   = "202611141806"
)

// behindHub is a hub of behindHubVersion whose build reads its releases from p, logging into the returned lines.
func behindHub(t *testing.T, p *fakePublisher) (*Hub, *syncLog) {
	t.Helper()
	h, _, _, _ := updatingHub(t, p, update.Release)
	h.build = update.Make(behindHubVersion, update.Release, behindHubBuild, p.srv.URL+"/dl")
	h.version = h.build.Version
	logs := &syncLog{}
	h.logger = log.New(logs, "", 0)
	return h, logs
}

// getUpdate is GET /api/v1/hub/update as the app reads it: the view and the raw fields.
func getUpdate(t *testing.T, u *updater) (updateView, map[string]any) {
	t.Helper()
	api := http.NewServeMux()
	u.routes(api)
	w := httptest.NewRecorder()
	api.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/hub/update", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("GET /api/v1/hub/update = %d: %s", w.Code, w.Body.String())
	}
	var v updateView
	var raw map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &v); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(w.Body.Bytes(), &raw); err != nil {
		t.Fatal(err)
	}
	return v, raw
}

const behindLog = "looks stopped"

// A source whose newest release is older than the hub and than the release the hub's code contains (the publish
// site's dl/, frozen on 2026-11-07) says so in GET /api/v1/hub/update, with the source in effect and its newest
// release, instead of only "up to date" (#8108). The fields there before stay as they were.
func TestUpdateSourceBehind(t *testing.T) {
	p := newPublisher(t, "v0.1.17-177-g563f7fde5b-202611071005", 202611071005)
	h, logs := behindHub(t, p)
	if _, err := h.upd.check(context.Background()); err != nil {
		t.Fatal(err)
	}
	v, raw := getUpdate(t, h.upd)
	if !v.SourceBehind || raw["source_behind"] != true {
		t.Fatalf("source_behind = %v (raw %v), want true", v.SourceBehind, raw["source_behind"])
	}
	if v.Latest == nil || v.Latest.Version != "v0.1.17-177-g563f7fde5b-202611071005" || v.Latest.Build != 202611071005 || v.Latest.Newer {
		t.Fatalf("latest = %+v, want the source's newest release, not newer", v.Latest)
	}
	if v.UpdateURL != p.srv.URL+"/dl" || v.UpdateFrom != updateFromBuild || v.State != "idle" || !v.CanUpdate {
		t.Fatalf("update_url = %q from %q, state %q, can_update %v", v.UpdateURL, v.UpdateFrom, v.State, v.CanUpdate)
	}
	if n := logs.about(behindLog); n != 1 {
		t.Fatalf("logged the stopped source %d times: %v", n, logs.lines)
	}
	line := logs.lines[0]
	for _, want := range []string{p.srv.URL + "/dl", "(built in)", "v0.1.17-177-g563f7fde5b-202611071005", behindHubVersion, "hub.json update.url sets another"} {
		if !strings.Contains(line, want) {
			t.Errorf("the log line does not say %q: %s", want, line)
		}
	}
}

// A source as new as the hub, newer, or holding the release the hub's code starts from (GitHub Releases for a hub
// deployed from master after v0.5.3) reports and logs nothing new: no source_behind field at all.
func TestUpdateSourceNotBehind(t *testing.T) {
	for _, c := range []struct {
		name, version string
		build         int64
		newer         bool
	}{
		{"equal", behindHubVersion, 202611141806, false},
		{"newer", "0.5.4", 202611150900, true},
		{"the release the hub's code starts from", "0.5.3", 202611140822, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			p := newPublisher(t, c.version, c.build)
			h, logs := behindHub(t, p)
			for range 2 {
				if _, err := h.upd.check(context.Background()); err != nil {
					t.Fatal(err)
				}
			}
			v, raw := getUpdate(t, h.upd)
			if _, ok := raw["source_behind"]; ok || v.SourceBehind {
				t.Fatalf("source_behind = %v for a current source", raw["source_behind"])
			}
			if v.Latest == nil || v.Latest.Build != c.build || v.Latest.Newer != c.newer {
				t.Fatalf("latest = %+v, want build %d newer %v", v.Latest, c.build, c.newer)
			}
			if n := logs.about(behindLog); n != 0 {
				t.Fatalf("logged a current source as stopped: %v", logs.lines)
			}
		})
	}
}

// The log line comes once per source and newest release, not at every hourly check (or install script's re-read):
// again when the source's newest release or the source itself changes, and again after the source caught up and fell
// behind once more.
func TestUpdateSourceBehindLogsOnce(t *testing.T) {
	frozen := newPublisher(t, "v0.1.17-177-g563f7fde5b-202611071005", 202611071005)
	h, logs := behindHub(t, frozen)
	u := h.upd
	ctx := context.Background()
	check := func() {
		t.Helper()
		if _, err := u.check(ctx); err != nil {
			t.Fatal(err)
		}
	}
	for range 3 {
		check()
	}
	u.releaseRead = u.releaseRead.Add(-2 * releaseFreshFor) // an install script reads latest.json again
	u.freshRelease(ctx)
	if n := logs.about(behindLog); n != 1 {
		t.Fatalf("three checks and a re-read logged the stopped source %d times: %v", n, logs.lines)
	}

	// hub.json update.url names another copy, as stopped: said again, as hub.json's.
	other := newPublisher(t, "0.4.0", 202611080000)
	u.client = other.srv.Client()
	h.cfg.Update.URL = other.srv.URL + "/dl"
	check()
	check()
	if n := logs.about(behindLog); n != 2 {
		t.Fatalf("a new stopped source logged %d times in all: %v", n, logs.lines)
	}
	if last := logs.lines[len(logs.lines)-1]; !strings.Contains(last, other.srv.URL+"/dl (hub.json update.url)") || !strings.Contains(last, "0.4.0") {
		t.Fatalf("the second line does not name hub.json's source and its release: %s", last)
	}

	// The source catches up, then falls behind again on the same release: said again.
	current := newPublisher(t, "0.5.3", 202611140822)
	u.client = current.srv.Client()
	h.cfg.Update.URL = current.srv.URL + "/dl"
	check()
	if v := u.view(); v.SourceBehind {
		t.Fatal("source_behind stayed after the source caught up")
	}
	u.client = other.srv.Client()
	h.cfg.Update.URL = other.srv.URL + "/dl"
	check()
	if n := logs.about(behindLog); n != 3 {
		t.Fatalf("falling behind again logged %d times in all: %v", n, logs.lines)
	}
}
