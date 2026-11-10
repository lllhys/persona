package extmgr

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/nwverse/persona/server/internal/subscriptions"
)

// The subscriptions' routes: anyone reads, only full access changes, and the service's errors keep the API's body.
func TestSubscriptionRoutes(t *testing.T) {
	h := newTestManager(t)
	mux := http.NewServeMux()
	h.subscriptionRoutes(mux)
	call := func(p principal, method, path, body string) (int, map[string]any) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		mux.ServeHTTP(rec, req.WithContext(withPrincipal(req.Context(), p)))
		var out map[string]any
		_ = json.Unmarshal(rec.Body.Bytes(), &out)
		return rec.Code, out
	}
	full, watch := principal{ID: "dev_phone", Role: roleFull}, principal{ID: "dev_watch", Role: roleAnswer}
	if code, out := call(full, "GET", "/api/v1/subscriptions", ""); code != http.StatusServiceUnavailable || out["code"] != "unavailable" {
		t.Fatalf("before the service runs: %d %v", code, out)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	h.startSubscriptions(ctx)
	if h.subscriptionsShown() {
		t.Fatal("the apps are told about subscriptions before they deliver (#2039)")
	}
	if !subscriptions.New(subscriptions.Options{Features: subscriptions.Features{Deliver: true}}).Features().Deliver {
		t.Fatal("Features does not say what the service was made with")
	}
	code, out := call(watch, "GET", "/api/v1/subscriptions", "")
	if code != http.StatusOK || out["subscriptions"] == nil || out["features"] == nil {
		t.Fatalf("list from an answer-only device: %d %v", code, out)
	}
	if code, out := call(watch, "GET", "/api/v1/subscriptions/summary", ""); code != http.StatusOK || out["connectors"] == nil {
		t.Fatalf("summary: %d %v", code, out)
	}
	for _, c := range []struct{ method, path string }{
		{"POST", "/api/v1/subscriptions"}, {"PUT", "/api/v1/subscriptions/sub_x"}, {"DELETE", "/api/v1/subscriptions/sub_x"},
		{"POST", "/api/v1/subscriptions/sub_x/run"}, {"POST", "/api/v1/subscriptions/-/dry-run"},
	} {
		if code, out := call(watch, c.method, c.path, `{}`); code != http.StatusForbidden || out["code"] != "read_only_device" {
			t.Errorf("%s %s from an answer-only device: %d %v", c.method, c.path, code, out)
		}
	}
	if code, out := call(full, "POST", "/api/v1/subscriptions", `{"connector":"nowhere","source":"x"}`); code != http.StatusNotFound || out["code"] != "not_found" || out["error"] == nil {
		t.Fatalf("an unknown connector: %d %v", code, out)
	}
	if code, out := call(full, "GET", "/api/v1/subscriptions/sub_x", ""); code != http.StatusNotFound || out["code"] != "not_found" {
		t.Fatalf("an unknown subscription: %d %v", code, out)
	}
	if code, out := call(full, "POST", "/api/v1/subscriptions", `{"connector":`); code != http.StatusBadRequest || out["code"] != "bad_request" {
		t.Fatalf("broken JSON: %d %v", code, out)
	}
}
