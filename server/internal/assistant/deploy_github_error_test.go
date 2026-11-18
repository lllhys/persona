package assistant

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestGitHubReadFailureDiagnostics(t *testing.T) {
	t.Setenv("GH_TOKEN", "private-test-token")
	for _, tc := range []struct {
		status          int
		remaining, kind string
	}{
		{403, "10", "permission"},
		{403, "0", "rate_limit"},
		{429, "0", "rate_limit"},
		{503, "10", "upstream"},
		{404, "10", "http"},
		{422, "10", "http"},
	} {
		t.Run(fmt.Sprint(tc.status, tc.remaining), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("X-RateLimit-Remaining", tc.remaining)
				w.Header().Set("X-RateLimit-Reset", "1800000000")
				w.Header().Set("X-GitHub-Request-Id", "ABCD:1234:5678:90AB")
				w.WriteHeader(tc.status)
				fmt.Fprint(w, `{"message":"Access denied private-test-token ghp_secret123 https://private.example/path me@example.com 192.0.2.10 2001:db8::1"}`)
			}))
			defer server.Close()
			g := githubForge{base: server.URL, client: server.Client()}
			err := g.get(context.Background(), "/repos/o/r/pulls/9", &struct{}{})
			if err == nil {
				t.Fatal("expected failure")
			}
			text := err.Error()
			for _, want := range []string{fmt.Sprint(tc.status), tc.kind, "Access denied", "X-RateLimit-Remaining=" + tc.remaining, "X-RateLimit-Reset=1800000000", "X-GitHub-Request-Id=ABCD:1234:5678:90AB"} {
				if !strings.Contains(text, want) {
					t.Errorf("missing %q: %s", want, text)
				}
			}
			for _, secret := range []string{"private-test-token", "ghp_secret123", "private.example", "me@example.com", "192.0.2.10", "2001:db8::1"} {
				if strings.Contains(text, secret) {
					t.Errorf("leaked %q", secret)
				}
			}
		})
	}
}

func TestGitHubRetryAfterIsRetained(t *testing.T) {
	resp := &http.Response{
		StatusCode: 429,
		Header:     http.Header{"Retry-After": []string{"120"}, "X-Github-Request-Id": []string{"request-456"}},
		Body:       io.NopCloser(strings.NewReader(`{"message":"secondary rate limit"}`)),
	}
	err := githubResponseError(resp)
	if !strings.Contains(err.Error(), "Retry-After=120") || !strings.Contains(err.Error(), "X-GitHub-Request-Id=request-456") {
		t.Fatalf("missing retry metadata: %v", err)
	}
}

func TestGitHubPauseRetainsCauseAndResume(t *testing.T) {
	s, _, _, _, forge := deployService(t)
	forge.err = fmt.Errorf("network unavailable")
	first := s.compareGitHub(context.Background(), "o/r", 9, oldHub)
	second := s.compareGitHub(context.Background(), "o/r", 10, oldHub)
	if first.State != deployUnknown || second.State != deployUnknown || forge.calls != 1 {
		t.Fatalf("unexpected checks: %+v %+v calls=%d", first, second, forge.calls)
	}
	if !strings.Contains(second.Err.Error(), "network unavailable") ||
		!strings.Contains(second.Err.Error(), s.deploy.githubDown.UTC().Format(time.RFC3339)) {
		t.Fatalf("lost pause cause or recovery time: %v", second.Err)
	}
}

func TestGitHubRateLimitPauseReportsOriginalResponse(t *testing.T) {
	s, _, _, _, forge := deployService(t)
	forge.err = &githubReadError{status: 403, kind: "rate_limit",
		detail: "message=API rate limit exceeded; X-RateLimit-Limit=60; X-RateLimit-Remaining=0; X-RateLimit-Resource=core; X-GitHub-Request-Id=request-pause"}
	first := s.compareGitHub(context.Background(), "o/r", 9, oldHub)
	paused := s.compareGitHub(context.Background(), "o/r", 10, oldHub)
	if first.State != deployUnknown || paused.State != deployUnknown || forge.calls != 1 {
		t.Fatalf("unexpected requests or states: %+v %+v requests=%d", first, paused, forge.calls)
	}
	resume := s.deploy.githubDown.UTC().Format(time.RFC3339)
	for _, want := range []string{first.Err.Error(), resume, "403", "rate_limit", "X-RateLimit-Remaining=0", "request-pause"} {
		if !strings.Contains(paused.Err.Error(), want) {
			t.Errorf("paused response lost %q: %v", want, paused.Err)
		}
	}
	if strings.Contains(paused.Err.Error(), "did not answer a moment ago") {
		t.Fatalf("generic pause reason remains: %v", paused.Err)
	}
	t.Logf("first request failure: %v", first.Err)
	t.Logf("paused response: %v; actual GitHub requests=%d", paused.Err, forge.calls)
}

func TestGitHubPermissionDoesNotPauseOtherPRs(t *testing.T) {
	s, _, _, _, forge := deployService(t)
	forge.err = &githubReadError{status: 403, kind: "permission", detail: "message=forbidden"}
	s.compareGitHub(context.Background(), "o/r", 9, oldHub)
	s.compareGitHub(context.Background(), "o/r", 10, oldHub)
	if forge.calls != 2 || !s.deploy.githubDown.IsZero() {
		t.Fatalf("permission failure paused unrelated checks: calls=%d pause=%v", forge.calls, s.deploy.githubDown)
	}
}

func TestRepairedReceiptOverridesCachedOutage(t *testing.T) {
	s, _, _, _, forge := deployService(t)
	path := filepath.Join(t.TempDir(), deployedCommitsName)
	s.deploy.listPaths = []string{path}
	forge.err = fmt.Errorf("network unavailable")
	pr := "https://github.com/o/r/pull/9"
	if r := s.checkDeployed(context.Background(), pr); r.State != deployUnknown {
		t.Fatalf("expected outage, got %+v", r)
	}
	text := deployedCommitsHeader + "\n# repo: o/r\n" + oldHub + " build\n" + mergeSHA + " fix (#9)\n"
	if err := os.WriteFile(path, []byte(text), 0600); err != nil {
		t.Fatal(err)
	}
	if r := s.checkDeployed(context.Background(), pr); r.State != deployIn || forge.calls != 1 {
		t.Fatalf("fresh receipt did not bypass cached outage: %+v calls=%d", r, forge.calls)
	}
}
