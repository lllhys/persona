package assistant

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// GET /api/v1/assistant/worker-presets lists only the profile's presets whose launcher the computer has, with the
// launchers whose health check fails now marked, and 404 for a computer that does not exist.
func TestWorkerPresetsRoute(t *testing.T) {
	s, f := defaultsHub(t)
	ctx := context.Background()
	saveTiers(t, s, map[string]ModelTier{
		"deep":      {Launcher: "gateway", Model: "gw-deep", Effort: "high", Description: "the deep one"},
		"elsewhere": {Launcher: "other", Model: "o-1"}, // l4's launcher, not box's
		"plain":     {Launcher: "claude", Model: "claude-plain"},
	})
	resets := time.Now().Add(2 * time.Hour)
	s.recordHealth(ctx, "box", "gateway", healthResult{err: "usage limit reached", source: healthSourceSession,
		kind: errKindUsageLimit, resetsAt: &resets})

	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/assistant/worker-presets?machine=box", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("code %d: %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Presets []workerPreset `json:"presets"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	got := map[string]workerPreset{}
	for _, p := range body.Presets {
		got[p.Name] = p
	}
	if len(got) != 2 { // "elsewhere" is not offered on box
		t.Fatalf("presets = %+v", body.Presets)
	}
	deep, plain := got["deep"], got["plain"]
	if deep.Launcher != "gateway" || deep.Model != "gw-deep" || deep.Effort != "high" || !deep.Unavailable {
		t.Fatalf("deep = %+v", deep)
	}
	if plain.Launcher != "claude" || plain.Unavailable {
		t.Fatalf("plain = %+v", plain)
	}

	rec = httptest.NewRecorder()
	s.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/assistant/worker-presets?machine=nope", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("unknown machine: code %d", rec.Code)
	}

	// No profile in use: an empty list, not an error.
	if err := s.store().PutSetting(ctx, modelProfileStateKey, modelProfileState{}); err != nil {
		t.Fatal(err)
	}
	rec = httptest.NewRecorder()
	s.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/assistant/worker-presets?machine=box", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("no profile: code %d", rec.Code)
	}
	if got := strings.TrimSpace(rec.Body.String()); got != `{"presets":[]}` {
		t.Fatalf("body = %q", got)
	}
	_ = f
}
