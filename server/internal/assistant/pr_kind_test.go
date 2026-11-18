package assistant

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

func TestGitHubForgeIsPR(t *testing.T) {
	tests := []struct {
		name      string
		issueCode int
		issueBody string
		pullCode  int
		pullBody  string
		repoCode  int
		wantPR    bool
		wantErr   bool
		wantPaths []string
	}{
		{name: "pull request", issueBody: `{"pull_request":{"url":"https://api.github.com/repos/o/r/pulls/7"}}`, wantPR: true, wantPaths: []string{"/repos/o/r/issues/7"}},
		{name: "issue", issueBody: `{"title":"ordinary issue"}`, wantPaths: []string{"/repos/o/r/issues/7"}},
		{name: "null marker", issueBody: `{"pull_request":null}`, wantPaths: []string{"/repos/o/r/issues/7"}},
		{name: "missing issue with readable PR list", issueCode: http.StatusNotFound, repoCode: http.StatusOK, wantPaths: []string{"/repos/o/r/issues/7", "/repos/o/r/pulls/7", "/repos/o/r/pulls"}},
		{name: "issues unavailable but PR readable", issueCode: http.StatusNotFound, pullCode: http.StatusOK, pullBody: `{"number":7}`, wantPR: true, wantPaths: []string{"/repos/o/r/issues/7", "/repos/o/r/pulls/7"}},
		{name: "PR access forbidden", issueCode: http.StatusNotFound, repoCode: http.StatusForbidden, wantErr: true, wantPaths: []string{"/repos/o/r/issues/7", "/repos/o/r/pulls/7", "/repos/o/r/pulls"}},
		{name: "repository missing", issueCode: http.StatusNotFound, repoCode: http.StatusNotFound, wantErr: true, wantPaths: []string{"/repos/o/r/issues/7", "/repos/o/r/pulls/7", "/repos/o/r/pulls"}},
		{name: "PR list server error", issueCode: http.StatusNotFound, repoCode: http.StatusInternalServerError, wantErr: true, wantPaths: []string{"/repos/o/r/issues/7", "/repos/o/r/pulls/7", "/repos/o/r/pulls"}},
		{name: "rate limit", issueCode: http.StatusTooManyRequests, wantErr: true, wantPaths: []string{"/repos/o/r/issues/7"}},
		{name: "unauthorized", issueCode: http.StatusUnauthorized, wantErr: true, wantPaths: []string{"/repos/o/r/issues/7"}},
		{name: "forbidden", issueCode: http.StatusForbidden, wantErr: true, wantPaths: []string{"/repos/o/r/issues/7"}},
		{name: "unprocessable entity", issueCode: http.StatusUnprocessableEntity, wantErr: true, wantPaths: []string{"/repos/o/r/issues/7"}},
		{name: "server error", issueCode: http.StatusInternalServerError, wantErr: true, wantPaths: []string{"/repos/o/r/issues/7"}},
		{name: "malformed JSON", issueBody: `{"pull_request":`, wantErr: true, wantPaths: []string{"/repos/o/r/issues/7"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var paths []string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				paths = append(paths, r.URL.Path)
				switch r.URL.Path {
				case "/repos/o/r/issues/7":
					code := tt.issueCode
					if code == 0 {
						code = http.StatusOK
					}
					w.WriteHeader(code)
					_, _ = w.Write([]byte(tt.issueBody))
				case "/repos/o/r/pulls/7":
					code := tt.pullCode
					if code == 0 {
						code = http.StatusNotFound
					}
					w.WriteHeader(code)
					_, _ = w.Write([]byte(tt.pullBody))
				case "/repos/o/r/pulls":
					code := tt.repoCode
					if code == 0 {
						code = http.StatusOK
					}
					w.WriteHeader(code)
					_, _ = w.Write([]byte(`[]`))
				default:
					http.NotFound(w, r)
				}
			}))
			defer srv.Close()

			g := githubForge{base: srv.URL, client: srv.Client()}
			got, err := g.IsPR(context.Background(), "o/r", 7)
			if got != tt.wantPR || (err != nil) != tt.wantErr {
				t.Errorf("IsPR() = (%v, %v), want (%v, error=%v)", got, err, tt.wantPR, tt.wantErr)
			}
			if !reflect.DeepEqual(paths, tt.wantPaths) {
				t.Errorf("request paths = %v, want %v", paths, tt.wantPaths)
			}
		})
	}
}

func TestGitHubForgeIsPRViaConnector(t *testing.T) {
	tests := []struct {
		name      string
		responses map[string]error
		wantPR    bool
		wantErr   bool
		wantPaths []string
	}{
		{
			name: "successful issue metadata",
			responses: map[string]error{
				"/repos/o/r/issues/7": nil,
			},
			wantPR:    true,
			wantPaths: []string{"/repos/o/r/issues/7"},
		},
		{
			name: "connector 404 then readable PR list",
			responses: map[string]error{
				"/repos/o/r/issues/7":                   fmt.Errorf("GitHub GET /repos/o/r/issues/7: 404 Not Found"),
				"/repos/o/r/pulls/7":                    fmt.Errorf("GitHub GET /repos/o/r/pulls/7: 404 Not Found"),
				"/repos/o/r/pulls?state=all&per_page=1": nil,
			},
			wantPaths: []string{"/repos/o/r/issues/7", "/repos/o/r/pulls/7", "/repos/o/r/pulls?state=all&per_page=1"},
		},
		{
			name: "connector 422 is not missing",
			responses: map[string]error{
				"/repos/o/r/issues/7": fmt.Errorf("GitHub GET /repos/o/r/issues/7: 422 Unprocessable Entity"),
			},
			wantErr:   true,
			wantPaths: []string{"/repos/o/r/issues/7"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var paths []string
			g := githubForge{
				base:   "http://127.0.0.1:1",
				client: http.DefaultClient,
				via: func(_ context.Context, path string, out any) (bool, error) {
					paths = append(paths, path)
					err, ok := tt.responses[path]
					if !ok {
						return false, nil
					}
					if err != nil {
						return true, err
					}
					if path == "/repos/o/r/issues/7" {
						if err := json.Unmarshal([]byte(`{"pull_request":{"url":"pull"}}`), out); err != nil {
							return true, err
						}
					} else if path == "/repos/o/r/pulls?state=all&per_page=1" {
						if err := json.Unmarshal([]byte(`[]`), out); err != nil {
							return true, err
						}
					}
					return true, nil
				},
			}
			got, err := g.IsPR(context.Background(), "o/r", 7)
			if got != tt.wantPR || (err != nil) != tt.wantErr {
				t.Errorf("IsPR() = (%v, %v), want (%v, error=%v)", got, err, tt.wantPR, tt.wantErr)
			}
			if !reflect.DeepEqual(paths, tt.wantPaths) {
				t.Errorf("connector paths = %v, want %v", paths, tt.wantPaths)
			}
		})
	}
}
