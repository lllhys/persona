package extmgr

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// The pages (#2041): every device draws them, unlike an extension page; only a full-access device may act on them.
func TestSubscriptionPagesRoute(t *testing.T) {
	h := newTestManager(t)
	mux := http.NewServeMux()
	h.subscriptionRoutes(mux)
	call := func(p principal, body string) (int, map[string]any) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest("POST", "/api/v1/subscriptions/-/ui", strings.NewReader(body))
		mux.ServeHTTP(rec, req.WithContext(withPrincipal(req.Context(), p)))
		var out map[string]any
		_ = json.Unmarshal(rec.Body.Bytes(), &out)
		return rec.Code, out
	}
	full, watch := principal{ID: "dev_phone", Role: roleFull}, principal{ID: "dev_watch", Role: roleAnswer}
	if code, out := call(full, `{}`); code != http.StatusServiceUnavailable || out["code"] != "unavailable" {
		t.Fatalf("before the service runs: %d %v", code, out)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	h.startSubscriptions(ctx)
	code, out := call(watch, `{"context":{"ui_version":6,"locale":"en"}}`)
	view, _ := out["view"].(map[string]any)
	if code != http.StatusOK || view == nil || view["title"] != "Subscriptions" {
		t.Fatalf("an answer-only device draws the list: %d %v", code, out)
	}
	if b, _ := json.Marshal(view); !strings.Contains(string(b), "can only view subscriptions") {
		t.Errorf("the read-only line is missing: %s", b)
	}
	if code, out := call(watch, `{"state":"{\"p\":\"source\",\"id\":\"sub_x\"}","action":"run_now"}`); code != http.StatusForbidden || out["code"] != "read_only_device" {
		t.Fatalf("an action from an answer-only device: %d %v", code, out)
	}
	code, out = call(full, `{"state":"{\"p\":\"source\",\"id\":\"sub_x\"}","action":"run_now","context":{"ui_version":6,"locale":"en"}}`)
	if code != http.StatusOK || out["toast"] == nil {
		t.Fatalf("an action on a subscription that isn't there says so: %d %v", code, out)
	}
}
