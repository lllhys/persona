package assistant

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

type noAnonymousGitHub struct{ calls int }

func (f *noAnonymousGitHub) RoundTrip(*http.Request) (*http.Response, error) {
	f.calls++
	return nil, fmt.Errorf("anonymous HTTP must not be used")
}

func fakeDeliveryGH(t *testing.T, response string, exit int) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("shell fixture")
	}
	dir := t.TempDir()
	args := filepath.Join(dir, "args")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" > \"$PERSONA_TEST_GH_ARGS\"\n/bin/cat <<'RESPONSE'\n" + response + "\nRESPONSE\n" + fmt.Sprintf("exit %d\n", exit)
	if err := os.WriteFile(filepath.Join(dir, "gh"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	t.Setenv("PERSONA_TEST_GH_ARGS", args)
	return args
}

func TestGitHubUsesAuthorizedCLIWhenConnectorUnavailable(t *testing.T) {
	argsPath := fakeDeliveryGH(t, "HTTP/2.0 200 OK\nX-RateLimit-Limit: 5000\nTransfer-Encoding: chunked\n\n"+
		`{"merged":true,"merge_commit_sha":"`+mergeSHA+`"}`, 0)
	transport := &noAnonymousGitHub{}
	g := githubForge{base: "https://api.github.com", client: &http.Client{Transport: transport},
		via: func(context.Context, string, any) (bool, error) { return false, nil }}
	merged, sha, err := g.PRMerge(context.Background(), "o/r", 9)
	if err != nil || !merged || sha != mergeSHA || transport.calls != 0 {
		t.Fatalf("authorized route: merged=%v sha=%s error=%v anonymous_calls=%d", merged, sha, err, transport.calls)
	}
	args, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(args) != "api\n--include\n--method\nGET\n--hostname\ngithub.com\n/repos/o/r/pulls/9\n" {
		t.Fatalf("not the fixed read-only authenticated CLI request: %q", args)
	}
}

func TestGitHubCLIUnavailableNeverFallsBackAnonymously(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	t.Setenv("GH_TOKEN", "")
	t.Setenv("GITHUB_TOKEN", "")
	transport := &noAnonymousGitHub{}
	g := githubForge{base: "https://api.github.com", client: &http.Client{Transport: transport}}
	_, _, err := g.PRMerge(context.Background(), "o/r", 9)
	if err == nil || !strings.Contains(err.Error(), "no anonymous fallback") || transport.calls != 0 {
		t.Fatalf("unavailable auth: %v anonymous_calls=%d", err, transport.calls)
	}
}

func TestGitHubWithoutCLIUsesExplicitEnvironmentAuthorization(t *testing.T) {
	for _, name := range []string{"GH_TOKEN", "GITHUB_TOKEN"} {
		t.Run(name, func(t *testing.T) {
			t.Setenv("PATH", t.TempDir())
			t.Setenv("GH_TOKEN", "")
			t.Setenv("GITHUB_TOKEN", "")
			t.Setenv(name, "synthetic-test-credential")
			calls := 0
			client := &http.Client{Transport: probeRoundTrip(func(r *http.Request) (*http.Response, error) {
				calls++
				if r.URL.Host != "api.github.com" || r.Method != http.MethodGet ||
					r.Header.Get("Authorization") != "Bearer synthetic-test-credential" {
					t.Fatal("request must use explicit authorization on the fixed GitHub host")
				}
				return &http.Response{StatusCode: 200, Header: make(http.Header),
					Body: io.NopCloser(strings.NewReader(`{"merged":true,"merge_commit_sha":"` + mergeSHA + `"}`))}, nil
			})}
			g := githubForge{base: "https://api.github.com", client: client}
			merged, sha, err := g.PRMerge(context.Background(), "o/r", 9)
			if err != nil || !merged || sha != mergeSHA || calls != 1 {
				t.Fatalf("environment authorization: merged=%v sha=%s error=%v calls=%d", merged, sha, err, calls)
			}
		})
	}
}

func TestGitHubCLIFailureRetainsHTTPDiagnostics(t *testing.T) {
	fakeDeliveryGH(t, "HTTP/2.0 403 Forbidden\nX-RateLimit-Limit: 5000\nX-RateLimit-Remaining: 0\n"+
		"X-RateLimit-Resource: core\nX-GitHub-Request-Id: ABCD:1234:5678:90AB\nRetry-After: 120\n\n"+
		`{"message":"API rate limit exceeded for 192.0.2.10"}`, 1)
	var out json.RawMessage
	err := githubCLIGet(context.Background(), "/repos/o/r/pulls/9", &out)
	if err == nil {
		t.Fatal("accepted a failed CLI request")
	}
	for _, want := range []string{"403", "rate_limit", "API rate limit exceeded", "X-RateLimit-Limit=5000", "X-RateLimit-Remaining=0",
		"X-RateLimit-Resource=core", "X-GitHub-Request-Id=ABCD:1234:5678:90AB", "Retry-After=120"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("missing %q: %v", want, err)
		}
	}
	if strings.Contains(err.Error(), "192.0.2.10") {
		t.Fatalf("leaked client address: %v", err)
	}
}

func TestGitHubCLIAuthenticationFailureDoesNotUseHTTP(t *testing.T) {
	fakeDeliveryGH(t, "", 1)
	t.Setenv("GH_TOKEN", "synthetic-test-credential")
	transport := &noAnonymousGitHub{}
	g := githubForge{base: "https://api.github.com", client: &http.Client{Transport: transport}}
	_, _, err := g.PRMerge(context.Background(), "o/r", 9)
	if err == nil || !strings.Contains(err.Error(), "authenticated GitHub CLI unavailable") || transport.calls != 0 {
		t.Fatalf("authentication failure: %v anonymous_calls=%d", err, transport.calls)
	}
}

// gh printed the headers of a 200, then the context killed it: that is a timeout, not an authentication or CLI failure.
func TestGitHubCLITimeoutAfterHeadersIsReportedAsTimeout(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixture")
	}
	dir := t.TempDir()
	script := "#!/bin/sh\nprintf 'HTTP/2.0 200 OK\\nX-RateLimit-Limit: 5000\\n\\n{\"status\":\"ahead\"'\nexec /bin/sleep 30\n"
	if err := os.WriteFile(filepath.Join(dir, "gh"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+":/bin:/usr/bin")
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	var out json.RawMessage
	err := githubCLIGet(ctx, "/repos/o/r/compare/a...b", &out)
	if err == nil {
		t.Fatal("accepted a response cut off by the deadline")
	}
	if !errors.Is(err, context.DeadlineExceeded) || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("not reported as a timeout: %v", err)
	}
	if strings.Contains(err.Error(), "CLI failed") {
		t.Fatalf("a timeout reported as a CLI failure: %v", err)
	}
}

// fakeCompareGH answers like GitHub's compare on a large main: the full payload (files and commits) takes longer than
// the budget to arrive, while a request for a high page number carries the status alone, at once.
func fakeCompareGH(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("shell fixture")
	}
	dir := t.TempDir()
	script := `#!/bin/sh
for path; do :; done # gh's last argument is the API path
printf 'HTTP/2.0 200 OK\nX-RateLimit-Limit: 5000\n\n'
case "$path" in
*'&page='*) printf '{"status":"ahead","ahead_by":159,"behind_by":0,"commits":[],"files":[]}\n' ;;
*) printf '{"status":"ahead","ahead_by":159,"behind_by":0,"files":[{"patch":"'; exec /bin/sleep 30 ;;
esac
`
	if err := os.WriteFile(filepath.Join(dir, "gh"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
}

func TestGitHubContainsDoesNotDownloadTheCompareContent(t *testing.T) {
	fakeCompareGH(t)
	g := githubForge{base: "https://api.github.com", client: &http.Client{Transport: &noAnonymousGitHub{}},
		via: func(context.Context, string, any) (bool, error) { return false, nil }}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	contains, found, err := g.Contains(ctx, "o/r", mergeSHA, "0123456789abcdef0123456789abcdef01234567")
	if err != nil || !contains || !found {
		t.Fatalf("ancestry of a merged PR behind a large main: contains=%v found=%v error=%v", contains, found, err)
	}
}
