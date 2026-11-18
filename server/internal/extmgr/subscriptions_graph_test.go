package extmgr

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// The relationship view (#5741): every device may read it, as every device may look at the pages, in the language
// asked for; the apps learn it is there from inbox_sources_graph.
func TestSubscriptionGraphRoute(t *testing.T) {
	h := newTestManager(t)
	mux := http.NewServeMux()
	h.subscriptionRoutes(mux)
	call := func(path string) (int, map[string]any) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest("GET", path, nil)
		mux.ServeHTTP(rec, req.WithContext(withPrincipal(req.Context(), principal{ID: "dev_watch", Role: roleAnswer})))
		var out map[string]any
		_ = json.Unmarshal(rec.Body.Bytes(), &out)
		return rec.Code, out
	}
	if code, out := call("/api/v1/subscriptions/-/graph"); code != http.StatusServiceUnavailable || out["code"] != "unavailable" {
		t.Fatalf("before the service runs: %d %v", code, out)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	h.startSubscriptions(ctx)
	code, out := call("/api/v1/subscriptions/-/graph?locale=zh-Hans")
	dests, _ := out["destinations"].([]any)
	if code != http.StatusOK || len(dests) != 2 || out["sources"] == nil || out["reads"] == nil || out["extensions"] == nil {
		t.Fatalf("an answer-only device reads the graph: %d %v", code, out)
	}
	if d, _ := dests[0].(map[string]any); d["id"] != "inbox" || d["name"] != "收件箱" {
		t.Errorf("in the language asked for: %v", dests[0])
	}
}
