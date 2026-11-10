package feishubot

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

// noBackoff replaces the retry backoff for the whole test (callSleep is package-level: no t.Parallel here).
func noBackoff(t *testing.T) {
	t.Helper()
	prev := callSleep
	callSleep = func(context.Context, time.Duration) error { return nil }
	t.Cleanup(func() { callSleep = prev })
}

// configAnswer is what the config or publish endpoint answers: an open-platform code and its message. The
// handler reads the request body first, so a test can check what was sent.
type configAnswer func(body map[string]any) (int, string)

// configureSrv serves the token endpoint plus the config and publish endpoints, counting each.
func configureSrv(t *testing.T, config, publish configAnswer) (*Client, *atomic.Int32, *atomic.Int32) {
	t.Helper()
	var configHits, publishHits atomic.Int32
	answer := func(w http.ResponseWriter, r *http.Request, fn configAnswer) {
		var in map[string]any
		_ = json.NewDecoder(r.Body).Decode(&in)
		code, msg := fn(in)
		out := map[string]any{"code": code, "msg": msg}
		if code == 0 {
			out["data"] = map[string]any{"version_id": "oav_1"}
		}
		okJSON(w, out)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/open-apis/auth/v3/tenant_access_token/internal":
			okJSON(w, map[string]any{"code": 0, "tenant_access_token": "tok", "expire": 7200})
		case "/open-apis/application/v7/applications/cli_x/config":
			if r.Method != http.MethodPatch {
				t.Errorf("config: got %s, want PATCH", r.Method)
			}
			configHits.Add(1)
			answer(w, r, config)
		case "/open-apis/application/v7/applications/cli_x/publish":
			if r.Method != http.MethodPost {
				t.Errorf("publish: got %s, want POST", r.Method)
			}
			publishHits.Add(1)
			answer(w, r, publish)
		default:
			t.Errorf("unexpected call %s %s", r.Method, r.URL.Path)
		}
	}))
	t.Cleanup(srv.Close)
	c := New("cli_x", "secret", srv.URL)
	c.console = srv.URL + "/app" // the console of the cloud under test
	return c, &configHits, &publishHits
}

func ok0(map[string]any) (int, string) { return 0, "" }

func TestAutoConfigureSetsScopesEventsAndLongConnection(t *testing.T) {
	noBackoff(t)
	var sent map[string]any
	c, configHits, publishHits := configureSrv(t,
		func(in map[string]any) (int, string) { sent = in; return 0, "" },
		ok0)

	rep := c.AutoConfigure(t.Context())

	if !rep.Configured || !rep.Published || rep.Issue != IssueNone || rep.VersionID != "oav_1" {
		t.Fatalf("report = %+v", rep)
	}
	if configHits.Load() != 1 || publishHits.Load() != 1 {
		t.Fatalf("config %d publish %d calls, want 1 and 1", configHits.Load(), publishHits.Load())
	}
	scope := sent["scope"].(map[string]any)
	scopes := scope["add_scopes"].([]any)
	if len(scopes) != len(configureScopes) {
		t.Fatalf("add_scopes = %v, want %v", scopes, configureScopes)
	}
	event := sent["event"].(map[string]any)
	if event["subscription_type"] != "websocket" {
		t.Fatalf("subscription_type = %v, want websocket", event["subscription_type"])
	}
	if len(event["add_events"].([]any)) != len(configureEvents) {
		t.Fatalf("add_events = %v, want %v", event["add_events"], configureEvents)
	}
}

func TestAutoConfigureStopsAtConfigFailure(t *testing.T) {
	noBackoff(t)
	c, _, publishHits := configureSrv(t,
		func(map[string]any) (int, string) { return 210041, "enable bot capability first" },
		ok0)

	rep := c.AutoConfigure(t.Context())

	if rep.Configured || rep.Published || rep.Issue != IssueBotCapability {
		t.Fatalf("report = %+v, want bot capability issue", rep)
	}
	if rep.Detail == "" {
		t.Fatal("Detail = \"\", want the platform's words for the log")
	}
	if publishHits.Load() != 0 {
		t.Fatalf("publish called %d times after a failed config", publishHits.Load())
	}
}

func TestAutoConfigureTakesUnderReviewAsPublished(t *testing.T) {
	noBackoff(t)
	c, _, publishHits := configureSrv(t, ok0,
		func(map[string]any) (int, string) { return 210302, "under review" })

	rep := c.AutoConfigure(t.Context())

	if !rep.Configured || !rep.Published || rep.Issue != IssueNone {
		t.Fatalf("report = %+v, want configured and published", rep)
	}
	if publishHits.Load() != 1 {
		t.Fatalf("publish called %d times", publishHits.Load())
	}
}

func TestAutoConfigureSkipsPrivateDeployment(t *testing.T) {
	if rep := (New("cli_x", "secret", "https://feishu.example.internal")).AutoConfigure(t.Context()); !rep.Skipped || rep.Issue != IssueNone {
		t.Fatalf("private: report = %+v, want skipped without an issue", rep)
	}
	if rep := (New("", "", "feishu")).AutoConfigure(t.Context()); !rep.Skipped || rep.Issue != IssueNoApp {
		t.Fatalf("no app id: report = %+v, want skipped no_app", rep)
	}
}

func TestConsoleURLs(t *testing.T) {
	if got := ConsoleURL("feishu"); got != DomainFeishu+"/app" {
		t.Errorf("ConsoleURL(feishu) = %q", got)
	}
	if got := ConsoleURL("lark"); got != DomainLark+"/app" {
		t.Errorf("ConsoleURL(lark) = %q", got)
	}
	if got := ConsoleURL("https://feishu.example.internal"); got != "" {
		t.Errorf("ConsoleURL(private) = %q, want \"\"", got)
	}
	if got := AppConsoleURL("feishu", "cli_x"); got != DomainFeishu+"/app/cli_x" {
		t.Errorf("AppConsoleURL = %q", got)
	}
	if got := AppConsoleURL("feishu", ""); got != "" {
		t.Errorf("AppConsoleURL(no app) = %q, want \"\"", got)
	}
}

func TestCredentialRejected(t *testing.T) {
	for _, code := range []int{10003, 10014} {
		if !CredentialRejected(&APIError{Op: "token", Code: code, Msg: "bad"}) {
			t.Errorf("code %d: want rejected", code)
		}
	}
	if CredentialRejected(&APIError{Op: "send", Code: 10014, Msg: "bad"}) {
		t.Error("a non-token call: want not rejected")
	}
	if CredentialRejected(&APIError{Op: "token", Code: 99991400, Msg: "rate"}) {
		t.Error("a rate-limited token call: want not rejected")
	}
}
