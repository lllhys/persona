package assistant

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strings"

	"github.com/nwverse/persona/server/internal/lazyre"
)

// Only allowlisted response metadata is retained, never request headers or the raw body.
type githubReadError struct {
	status int
	kind   string
	detail string
}

func (e *githubReadError) Error() string {
	return fmt.Sprintf("GitHub answered %d (%s): %s", e.status, e.kind, e.detail)
}

var githubDiagnosticSecrets = lazyre.New(`(?i)(?:bearer\s+\S+|(?:github_pat_|gh[pousr]_)[a-z0-9_]+|https?://[^\s"<>]+|[a-z0-9._%+-]+@[a-z0-9.-]+\.[a-z]{2,})`)
var githubDiagnosticAddresses = lazyre.New(`(?i)\b(?:(?:[0-9]{1,3}\.){3}[0-9]{1,3}|(?:[a-f0-9]{0,4}:){2,}[a-f0-9:]*)\b`)

func githubDiagnostic(s string) string {
	for _, name := range []string{"GH_TOKEN", "GITHUB_TOKEN"} {
		if token := strings.TrimSpace(os.Getenv(name)); token != "" {
			s = strings.ReplaceAll(s, token, "[redacted]")
		}
	}
	s = githubDiagnosticSecrets.ReplaceAllString(s, "[redacted]")
	s = githubDiagnosticAddresses.ReplaceAllStringFunc(s, func(value string) string {
		if net.ParseIP(value) != nil {
			return "[redacted]"
		}
		return value
	})
	s = strings.Join(strings.Fields(s), " ")
	if len(s) > 512 {
		s = s[:512] + "..."
	}
	return s
}

func githubResponseError(resp *http.Response) error {
	var body struct {
		Message string `json:"message"`
	}
	_ = json.NewDecoder(io.LimitReader(resp.Body, 16<<10)).Decode(&body)
	kind := "http"
	switch {
	case resp.StatusCode == 429 || resp.StatusCode == 403 && (resp.Header.Get("X-RateLimit-Remaining") == "0" || resp.Header.Get("Retry-After") != "" ||
		strings.Contains(strings.ToLower(body.Message), "rate limit")):
		kind = "rate_limit"
	case resp.StatusCode == 401 || resp.StatusCode == 403:
		kind = "permission"
	case resp.StatusCode >= 500:
		kind = "upstream"
	}
	fields := []string{"message=" + githubDiagnostic(body.Message)}
	for _, name := range []string{"X-RateLimit-Limit", "X-RateLimit-Remaining", "X-RateLimit-Used", "X-RateLimit-Reset", "X-RateLimit-Resource", "Retry-After", "X-GitHub-Request-Id"} {
		if value := resp.Header.Get(name); value != "" {
			fields = append(fields, name+"="+githubDiagnostic(value))
		}
	}
	return &githubReadError{status: resp.StatusCode, kind: kind, detail: strings.Join(fields, "; ")}
}
