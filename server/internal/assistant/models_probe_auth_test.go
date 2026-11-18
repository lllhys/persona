package assistant

import (
	"context"
	"maps"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/nwverse/persona/server/internal/i18n"
	"github.com/nwverse/persona/server/internal/proto"
)

// authGateway is a Messages / model-list gateway that takes the key in the headers it accepts, refuses the rest with
// 401, and records which credential headers each request carried (sentAuth).
type authGateway struct {
	*httptest.Server
	mu   sync.Mutex
	sent []string
}

// newAuthGateway accepts the key as Bearer, as x-api-key, or both. Its model list has two pages (after_id).
func newAuthGateway(t *testing.T, bearer, apiKey bool) *authGateway {
	t.Helper()
	g := &authGateway{}
	g.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		g.mu.Lock()
		g.sent = append(g.sent, sentAuth(r))
		g.mu.Unlock()
		if !(bearer && r.Header.Get("Authorization") == "Bearer "+fakeKey || apiKey && r.Header.Get("x-api-key") == fakeKey) {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":{"message":"invalid key"}}`))
			return
		}
		switch {
		case strings.HasSuffix(r.URL.Path, "/models") && r.URL.Query().Get("after_id") == "":
			_, _ = w.Write([]byte(`{"data":[{"id":"model-a"}],"has_more":true,"last_id":"model-a"}`))
		case strings.HasSuffix(r.URL.Path, "/models"):
			_, _ = w.Write([]byte(`{"data":[{"id":"model-b"}],"has_more":false}`))
		default:
			_, _ = w.Write([]byte(`{"type":"message","content":[]}`))
		}
	}))
	t.Cleanup(g.Close)
	return g
}

// sentAuth names the credential headers on r: "bearer", "api_key", "both" or "none".
func sentAuth(r *http.Request) string {
	b, k := r.Header.Get("Authorization") != "", r.Header.Get("x-api-key") != ""
	switch {
	case b && k:
		return "both"
	case b:
		return probeAuthBearer
	case k:
		return probeAuthAPIKey
	}
	return "none"
}

// requests returns what each request carried since the last call, and forgets it.
func (g *authGateway) requests() []string {
	g.mu.Lock()
	defer g.mu.Unlock()
	out := g.sent
	g.sent = nil
	return out
}

// An explicit method sends that header alone, to test and discover alike (the model list's two pages and the
// capability report after them).
func TestProbeExplicitAuthSendsOneHeader(t *testing.T) {
	_, mux := launcherTestService(t)
	g := newAuthGateway(t, true, true)
	for _, auth := range []string{probeAuthAPIKey, probeAuthBearer} {
		out := probeResult(t, mux, "/api/v1/launchers/test", `{"base_url":"`+g.URL+`","api_key":"`+fakeKey+`","model":"m","auth":"`+auth+`"}`)
		if sent := g.requests(); out["ok"] != true || !slices.Equal(sent, []string{auth}) || out["auth"] != nil {
			t.Fatalf("test %s: %v sent %v", auth, out, sent)
		}
		out = probeResult(t, mux, "/api/v1/launchers/discover", `{"base_url":"`+g.URL+`","api_key":"`+fakeKey+`","auth":"`+auth+`"}`)
		if sent := g.requests(); out["error"] != nil || !slices.Equal(sent, []string{auth, auth, auth}) || out["auth"] != nil {
			t.Fatalf("discover %s: %v sent %v", auth, out, sent)
		}
	}
}

// auto tries Bearer, then x-api-key when the key is refused, and says which one the gateway took. A gateway taking
// both answers Bearer at once.
func TestProbeAutoFindsTheHeader(t *testing.T) {
	_, mux := launcherTestService(t)
	for name, c := range map[string]struct {
		bearer, apiKey bool
		sent           []string
		auth           any
	}{
		"both":      {true, true, []string{probeAuthBearer}, probeAuthBearer},
		"x-api-key": {false, true, []string{probeAuthBearer, probeAuthAPIKey}, probeAuthAPIKey},
		"bearer":    {true, false, []string{probeAuthBearer}, probeAuthBearer},
		"neither":   {false, false, []string{probeAuthBearer, probeAuthAPIKey}, nil},
	} {
		g := newAuthGateway(t, c.bearer, c.apiKey)
		out := probeResult(t, mux, "/api/v1/launchers/test", `{"base_url":"`+g.URL+`","api_key":"`+fakeKey+`","model":"m","auth":"auto"}`)
		if sent := g.requests(); out["ok"] != (c.auth != nil) || out["auth"] != c.auth || !slices.Equal(sent, c.sent) {
			t.Fatalf("%s: %v sent %v", name, out, sent)
		}
		if c.auth == nil && (out["code"] != "auth" || !strings.Contains(jsonText(out["error"]), "401")) {
			t.Fatalf("%s: both refused is the x-api-key attempt's auth failure: %v", name, out)
		}
	}
}

// A 403 that is not Anthropic's region block is a refused key too; the second answer is the one reported.
func TestProbeAutoRetriesAForbiddenKey(t *testing.T) {
	_, mux := launcherTestService(t)
	var sent []string
	gw := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sent = append(sent, sentAuth(r))
		if r.Header.Get("x-api-key") == "" {
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(anthropicPermission))
			return
		}
		_, _ = w.Write([]byte(`{"type":"message","content":[]}`))
	}))
	defer gw.Close()
	out := probeResult(t, mux, "/api/v1/launchers/test", `{"base_url":"`+gw.URL+`","api_key":"`+fakeKey+`","model":"m","auth":"auto"}`)
	if out["ok"] != true || out["auth"] != probeAuthAPIKey || !slices.Equal(sent, []string{probeAuthBearer, probeAuthAPIKey}) {
		t.Fatalf("%v sent %v", out, sent)
	}
}

// Anthropic's region block comes before the key is looked at, and other failures are not about the header: neither
// is retried, and neither reports a method.
func TestProbeAutoKeepsOtherFailures(t *testing.T) {
	_, mux := launcherTestService(t)
	for name, c := range map[string]struct {
		status int
		body   string
		code   any
	}{
		"region blocked": {http.StatusForbidden, anthropicRegionBlocked, probeCodeRegionBlocked},
		"not found":      {http.StatusNotFound, `{"error":{"message":"no route"}}`, "not_found"},
		"rate limited":   {http.StatusTooManyRequests, `{"error":{"message":"slow down"}}`, nil},
	} {
		var sent []string
		gw := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			sent = append(sent, sentAuth(r))
			w.Header().Set("content-type", "application/json")
			w.WriteHeader(c.status)
			_, _ = w.Write([]byte(c.body))
		}))
		out := probeResult(t, mux, "/api/v1/launchers/test", `{"base_url":"`+gw.URL+`","api_key":"`+fakeKey+`","model":"m","auth":"auto"}`)
		gw.Close()
		if out["ok"] != false || out["code"] != c.code || out["auth"] != nil || !slices.Equal(sent, []string{probeAuthBearer}) {
			t.Fatalf("%s: %v sent %v", name, out, sent)
		}
	}
}

// By name without auth a probe sends what the launch sets for the stored method (proto.ManagedLauncher.BearerAuth);
// an auth in the request wins.
func TestProbeByNameUsesTheStoredMethod(t *testing.T) {
	s, mux := launcherTestService(t)
	g := newAuthGateway(t, true, true)
	for _, c := range []struct{ name, auth string }{{"by-bearer", "bearer"}, {"by-key", "api_key"}, {"by-default", ""}} {
		if code, raw := launcherCall(t, mux, "POST", "/api/v1/launchers",
			`{"name":"`+c.name+`","base_url":"`+g.URL+`","model":"m","api_key":"`+fakeKey+`","auth":"`+c.auth+`"}`); code != 200 {
			t.Fatal(raw)
		}
	}
	launches := map[string]string{}
	for _, l := range s.storedLaunchers(context.Background()) {
		launch := probeAuthAPIKey
		if l.BearerAuth() {
			launch = probeAuthBearer
		}
		launches[l.Name] = launch
		out := probeResult(t, mux, "/api/v1/launchers/test", `{"name":"`+l.Name+`"}`)
		if sent := g.requests(); out["ok"] != true || !slices.Equal(sent, []string{launch}) || out["auth"] != nil {
			t.Fatalf("%s (stored %q): %v sent %v, launch sets %s", l.Name, l.Auth, out, sent, launch)
		}
		out = probeResult(t, mux, "/api/v1/launchers/discover", `{"name":"`+l.Name+`"}`)
		if sent := g.requests(); out["error"] != nil || !slices.Equal(sent, []string{launch, launch, launch}) {
			t.Fatalf("%s discover: %v sent %v", l.Name, out, sent)
		}
		other := probeAuthBearer
		if launch == probeAuthBearer {
			other = probeAuthAPIKey
		}
		out = probeResult(t, mux, "/api/v1/launchers/test", `{"name":"`+l.Name+`","auth":"`+other+`"}`)
		if sent := g.requests(); out["ok"] != true || !slices.Equal(sent, []string{other}) {
			t.Fatalf("%s with auth %s: %v sent %v", l.Name, other, out, sent)
		}
	}
	if want := map[string]string{"by-bearer": probeAuthBearer, "by-key": probeAuthAPIKey, "by-default": probeAuthAPIKey}; !maps.Equal(launches, want) {
		t.Fatalf("stored methods %v, want %v", launches, want)
	}
}

// Codex and Antigravity launch with their own key variables, which auth does not choose: by name they keep both
// headers, whatever auth the request names.
func TestProbeByNameKeepsBothForOtherAgents(t *testing.T) {
	_, mux := launcherTestService(t)
	g := newAuthGateway(t, true, true)
	for _, agent := range []string{"codex", "agy"} {
		if code, raw := launcherCall(t, mux, "POST", "/api/v1/launchers",
			`{"name":"by-`+agent+`","agent":"`+agent+`","base_url":"`+g.URL+`","model":"m","api_key":"`+fakeKey+`","auth":"bearer"}`); code != 200 {
			t.Fatal(raw)
		}
		for _, auth := range []string{"", probeAuthAPIKey, probeAuthBearer, probeAuthAuto} {
			out := probeResult(t, mux, "/api/v1/launchers/test", `{"name":"by-`+agent+`","auth":"`+auth+`"}`)
			if sent := g.requests(); out["ok"] != true || out["auth"] != nil || !slices.Equal(sent, []string{"both"}) {
				t.Fatalf("%s with auth %q: %v sent %v", agent, auth, out, sent)
			}
		}
	}
}

// Under auto, a Bearer key the gateway knew but forbade something (403) says more than the 401 of a header it does not
// read: that answer stands, and no method is reported.
func TestProbeAutoKeepsBearersForbiddenOverAMissingKey(t *testing.T) {
	_, mux := launcherTestService(t)
	var sent []string
	gw := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sent = append(sent, sentAuth(r))
		if r.Header.Get("Authorization") != "" {
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`{"error":{"message":"model not allowed for this key"}}`))
			return
		}
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":{"message":"missing Authorization header"}}`))
	}))
	defer gw.Close()
	out := probeResult(t, mux, "/api/v1/launchers/test", `{"base_url":"`+gw.URL+`","api_key":"`+fakeKey+`","model":"m","auth":"auto"}`)
	if msg, _ := out["error"].(string); out["ok"] != false || out["auth"] != nil || out["code"] != "auth" ||
		!strings.Contains(msg, "model not allowed") || !slices.Equal(sent, []string{probeAuthBearer, probeAuthAPIKey}) {
		t.Fatalf("%v sent %v", out, sent)
	}
}

// Without a key no credential header goes, so auto has nothing to find: one request, no method reported.
func TestProbeAutoWithoutAKeyFindsNothing(t *testing.T) {
	_, mux := launcherTestService(t)
	g := newAuthGateway(t, true, true)
	out := probeResult(t, mux, "/api/v1/launchers/discover", `{"base_url":"`+g.URL+`","auth":"auto"}`)
	if sent := g.requests(); out["auth"] != nil || !slices.Equal(sent, []string{"none"}) {
		t.Fatalf("%v sent %v", out, sent)
	}
}

// A preset without auth in the request sends the preset's method, what a provider saved from it launches with.
func TestProbePresetUsesItsMethod(t *testing.T) {
	_, mux := launcherTestService(t)
	g := newAuthGateway(t, true, true)
	for preset, want := range map[string]string{"builtin/glm": probeAuthBearer, "builtin/anthropic": probeAuthAPIKey} {
		out := probeResult(t, mux, "/api/v1/launchers/test", `{"preset":"`+preset+`","base_url":"`+g.URL+`","api_key":"`+fakeKey+`"}`)
		if sent := g.requests(); out["ok"] != true || !slices.Equal(sent, []string{want}) {
			t.Fatalf("%s: %v sent %v", preset, out, sent)
		}
	}
}

// Responses and Chat Completions send Bearer alone, whatever auth says, and never report a method.
func TestProbeResponsesAndChatSendBearer(t *testing.T) {
	s, _ := launcherTestService(t)
	var sent []string
	gw := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sent = append(sent, sentAuth(r))
		if strings.HasSuffix(r.URL.Path, "/chat/completions") {
			_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"pong\"},\"finish_reason\":\"stop\"}]}\n\n"))
			return
		}
		_, _ = w.Write([]byte("data: {\"type\":\"response.completed\"}\n\n"))
	}))
	defer gw.Close()
	ctx := i18n.With(context.Background(), i18n.En)
	for _, api := range []string{"responses", "chat"} {
		for _, auth := range []string{probeAuthAPIKey, probeAuthAuto, ""} {
			sent = nil
			out := s.providerTest(ctx, probeInput{BaseURL: gw.URL, APIKey: fakeKey, Model: "m", API: api, Auth: auth})
			if out["ok"] != true || out["auth"] != nil || !slices.Equal(sent, []string{probeAuthBearer}) {
				t.Fatalf("%s auth %q: %v sent %v", api, auth, out, sent)
			}
		}
	}
}

// An unknown method is refused before anything is sent.
func TestProbeUnknownAuth(t *testing.T) {
	s, _ := launcherTestService(t)
	g := newAuthGateway(t, true, true)
	ctx := i18n.With(context.Background(), i18n.En)
	want := i18n.Localize(i18n.Err("assistant-models_launchers.err_auth"), i18n.En)
	in := probeInput{BaseURL: g.URL, APIKey: fakeKey, Model: "m", Auth: "basic"}
	if out := s.providerTest(ctx, in); out["ok"] != false || out["error"] != want {
		t.Fatalf("test: %v", out)
	}
	out := s.providerDiscover(ctx, in)
	if models, _ := out["models"].([]proto.ProviderModel); out["error"] != want || models == nil || len(models) != 0 {
		t.Fatalf("discover: %v", out)
	}
	if sent := g.requests(); len(sent) != 0 {
		t.Fatalf("sent %v", sent)
	}
}

// Discover settles auto on the first page and sends only that header for the next ones and the capability report. It
// names no method: a model list may answer without looking at the key, so only the test says what a launch needs.
func TestProbeDiscoverAutoResolvesOnTheFirstPage(t *testing.T) {
	_, mux := launcherTestService(t)
	for name, c := range map[string]struct {
		bearer, apiKey bool
		sent           []string
	}{
		"x-api-key": {false, true, []string{probeAuthBearer, probeAuthAPIKey, probeAuthAPIKey, probeAuthAPIKey}},
		"both":      {true, true, []string{probeAuthBearer, probeAuthBearer, probeAuthBearer}},
	} {
		g := newAuthGateway(t, c.bearer, c.apiKey)
		out := probeResult(t, mux, "/api/v1/launchers/discover", `{"base_url":"`+g.URL+`","api_key":"`+fakeKey+`","auth":"auto"}`)
		if sent := g.requests(); out["error"] != nil || out["auth"] != nil || !slices.Equal(sent, c.sent) ||
			!strings.Contains(jsonText(out["models"]), "model-b") {
			t.Fatalf("%s: %v sent %v", name, out, sent)
		}
	}
	g := newAuthGateway(t, false, false)
	out := probeResult(t, mux, "/api/v1/launchers/discover", `{"base_url":"`+g.URL+`","api_key":"`+fakeKey+`","auth":"auto"}`)
	if sent := g.requests(); out["error"] == nil || out["code"] != "auth" || out["auth"] != nil ||
		!slices.Equal(sent, []string{probeAuthBearer, probeAuthAPIKey}) {
		t.Fatalf("both refused: %v sent %v", out, sent)
	}
}
