package assistant

import (
	"context"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/nwverse/persona/server/internal/i18n"
	"github.com/nwverse/persona/server/internal/proto"
)

type probeRoundTrip func(*http.Request) (*http.Response, error)

func (f probeRoundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// Intercept every request, including those with the official hostname: no test contacts DeepSeek.
func interceptProbe(t *testing.T, f probeRoundTrip) {
	t.Helper()
	old := http.DefaultTransport
	http.DefaultTransport = f
	t.Cleanup(func() { http.DefaultTransport = old })
}

func probeResponse(r *http.Request, status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: r}
}

// The official Messages base is not the model-list base. The provider's name does not select this exception.
func TestProviderDeepSeekDiscoveryEndpoint(t *testing.T) {
	s, mux := launcherTestService(t)
	var requests []string
	interceptProbe(t, func(r *http.Request) (*http.Response, error) {
		requests = append(requests, r.Method+" "+r.URL.String())
		if r.Header.Get("Authorization") != "Bearer "+fakeKey || r.Header.Get("x-api-key") != fakeKey {
			t.Error("missing probe credentials")
		}
		switch r.Method + " " + r.URL.Path {
		case "POST /anthropic/v1/messages":
			var body strings.Builder
			_, _ = io.Copy(&body, r.Body)
			if !strings.Contains(body.String(), `"model":"deepseek-v4-pro"`) {
				t.Errorf("wrong message model: %s", body.String())
			}
			return probeResponse(r, 200, `{"type":"message","content":[]}`), nil
		case "GET /models":
			return probeResponse(r, 200, `{"object":"list","data":[{"id":"deepseek-v4-pro","object":"model","owned_by":"deepseek","name":"DeepSeek-V4-Pro","context_window":1048576}]}`), nil
		default:
			return probeResponse(r, 404, `{"error":{"message":"unknown endpoint"}}`), nil
		}
	})
	base := "https://api.deepseek.com/anthropic"
	if code, raw := launcherCall(t, mux, "POST", "/api/v1/launchers",
		`{"name":"custom-name","base_url":"`+base+`","model":"deepseek-v4-pro","api_key":"`+fakeKey+`"}`); code != 200 {
		t.Fatal(code, raw)
	}
	if out := probeResult(t, mux, "/api/v1/launchers/test", `{"name":"custom-name"}`); out["ok"] != true {
		t.Fatalf("Messages probe failed: %v", out)
	}
	for _, in := range []probeInput{
		{Name: "custom-name"},
		{BaseURL: base, APIKey: fakeKey, API: "messages"},
		{BaseURL: base + "/v1/", APIKey: fakeKey},
		{BaseURL: "https://api.deepseek.com", APIKey: fakeKey, API: "chat"},
		{BaseURL: "https://api.deepseek.com/v1", APIKey: fakeKey, API: "responses"},
		{BaseURL: "https://API.DeepSeek.COM:443/anthropic", APIKey: fakeKey},
	} {
		out := s.providerDiscover(i18n.With(context.Background(), i18n.En), in)
		models, _ := out["models"].([]proto.ProviderModel)
		if out["error"] != nil || len(models) != 1 || models[0].ID != "deepseek-v4-pro" || models[0].Name == "" || models[0].ContextWindow != 1048576 {
			t.Fatalf("discovery for %+v: %v", in, out)
		}
	}
	want := []string{"POST " + base + "/v1/messages"}
	for range 5 {
		want = append(want, "GET https://api.deepseek.com/models")
	}
	want = append(want, "GET https://API.DeepSeek.COM:443/models")
	if !reflect.DeepEqual(requests, want) {
		t.Fatalf("requests = %v, want %v", requests, want)
	}
	if stored := s.storedLaunchers(context.Background()); len(stored) != 1 || stored[0].BaseURL != base {
		t.Fatalf("discovery changed the stored Messages base: %+v", stored)
	}
}

func TestProviderDeepSeekDiscoveryStoredCredentialsStayBoundToBase(t *testing.T) {
	_, mux := launcherTestService(t)
	base := "https://api.deepseek.com/anthropic"
	if code, raw := launcherCall(t, mux, "POST", "/api/v1/launchers",
		`{"name":"deepseek","base_url":"`+base+`","model":"deepseek-v4-pro","api_key":"`+fakeKey+`","headers":"X-Team: hdr-secret-value"}`); code != 200 {
		t.Fatal(code, raw)
	}
	var gotURL, key, auth, team string
	interceptProbe(t, func(r *http.Request) (*http.Response, error) {
		gotURL, key, auth, team = r.URL.String(), r.Header.Get("x-api-key"), r.Header.Get("Authorization"), r.Header.Get("X-Team")
		return probeResponse(r, 200, `{"data":[]}`), nil
	})
	probeResult(t, mux, "/api/v1/launchers/discover", `{"name":"deepseek"}`)
	if gotURL != "https://api.deepseek.com/models" || key != fakeKey || auth != "Bearer "+fakeKey || team != "hdr-secret-value" {
		t.Fatal("same-origin discovery lost the saved credentials")
	}
	// Even another base on the same host must not receive the stored key or custom headers.
	for _, other := range []string{"https://api.deepseek.com", "https://gateway.example.com/anthropic"} {
		probeResult(t, mux, "/api/v1/launchers/discover", `{"name":"deepseek","base_url":"`+other+`"}`)
		if key != "" || auth != "" || team != "" {
			t.Fatal("a changed base received saved credentials")
		}
	}
}

// Unknown paths, lookalike hosts, insecure URLs and nonstandard ports retain the existing gateway rule.
func TestProviderDiscoveryPreservesCustomGateway(t *testing.T) {
	s, _ := launcherTestService(t)
	var got string
	interceptProbe(t, func(r *http.Request) (*http.Response, error) {
		got = r.URL.String()
		return probeResponse(r, 200, `{"data":[]}`), nil
	})
	for _, base := range []string{
		"https://gateway.example.com/anthropic",
		"https://gateway.example.com/team/v1",
		"https://api.deepseek.com.example.com/anthropic",
		"https://api.deepseek.com:8443/anthropic",
		"http://api.deepseek.com/anthropic",
		"https://api.deepseek.com/team/anthropic",
		"https://api.deepseek.com/%61nthropic",
	} {
		got = ""
		out := s.providerDiscover(context.Background(), probeInput{BaseURL: base, APIKey: fakeKey})
		want := base + "/v1/models"
		if strings.HasSuffix(base, "/v1") {
			want = base + "/models"
		}
		if out["error"] != nil || got != want {
			t.Errorf("%s: request %q, want %q; %v", base, got, want, out)
		}
	}
}

// A model-list 404 is not a failed Messages connection and must not suggest changing a working base.
func TestProviderDiscovery404ExplainsManualFallback(t *testing.T) {
	s, _ := launcherTestService(t)
	interceptProbe(t, func(r *http.Request) (*http.Response, error) {
		if r.Method == http.MethodPost {
			return probeResponse(r, 200, `{"type":"message","content":[]}`), nil
		}
		return probeResponse(r, 404, `{"error":{"message":"missing `+fakeKey+`"}}`), nil
	})
	in := probeInput{BaseURL: "https://gateway.example.com/anthropic", APIKey: fakeKey, Model: "custom-model"}
	ctx := i18n.With(context.Background(), i18n.En)
	if out := s.providerTest(ctx, in); out["ok"] != true {
		t.Fatalf("Messages probe: %v", out)
	}
	out := s.providerDiscover(ctx, in)
	msg, _ := out["error"].(string)
	if out["code"] != "not_found" || !strings.Contains(msg, "404") || !strings.Contains(msg, "manually") ||
		strings.Contains(msg, "check the Base URL") || strings.Contains(msg, fakeKey) {
		t.Fatalf("discovery failure: %v", out)
	}
}

// Pagination stays on the selected model-list endpoint, not the Messages base or an upstream-supplied path.
func TestProviderDeepSeekDiscoveryPagination(t *testing.T) {
	for _, next := range []string{"?page=2", "https://api.deepseek.com/anthropic/v1/models?page=2", "https://other.example.com/models?page=2"} {
		t.Run(next, func(t *testing.T) {
			s, _ := launcherTestService(t)
			calls := 0
			interceptProbe(t, func(r *http.Request) (*http.Response, error) {
				calls++
				if r.URL.Host != "api.deepseek.com" || r.URL.Path != "/models" {
					t.Errorf("unexpected endpoint: %s", r.URL)
				}
				if r.URL.RawQuery == "" {
					return probeResponse(r, 200, `{"data":["first"],"next":"`+next+`"}`), nil
				}
				if r.URL.RawQuery != "page=2" {
					t.Errorf("unexpected query: %s", r.URL.RawQuery)
				}
				return probeResponse(r, 200, `{"data":["second"]}`), nil
			})
			out := s.providerDiscover(context.Background(), probeInput{BaseURL: "https://api.deepseek.com/anthropic", APIKey: fakeKey})
			models := out["models"].([]proto.ProviderModel)
			if next == "?page=2" {
				if calls != 2 || out["error"] != nil || len(models) != 2 {
					t.Fatalf("pagination: %d calls, %v", calls, out)
				}
			} else if calls != 1 || out["error"] == nil || len(models) != 1 {
				t.Fatalf("unsafe next link accepted: %d calls, %v", calls, out)
			}
		})
	}
}
