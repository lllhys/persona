package assistant

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/remoteimage"
)

// The remote image route (#9473): full devices only, addresses refused before anything is fetched, every refusal a
// code with a sentence, a picture answered so that nothing runs or sniffs it.
func TestRemoteImageRoute(t *testing.T) {
	s := newTestService(t, newFakeHub())
	get := func(role, addr string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodGet, "/api/v1/remote-image?size=512&url="+url.QueryEscape(addr), nil)
		if role != "" {
			r.Header.Set(RoleHeader, role)
		}
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		return w
	}
	codeOf := func(w *httptest.ResponseRecorder) string {
		var body struct{ Code, Error string }
		_ = json.Unmarshal(w.Body.Bytes(), &body)
		if body.Error == "" {
			t.Errorf("%d %s: no sentence", w.Code, w.Body)
		}
		return body.Code
	}
	for _, role := range []string{roleExtension, "limited"} {
		if w := get(role, "https://img.example.com/a.png"); w.Code != http.StatusForbidden {
			t.Errorf("as %s: %d, want 403", role, w.Code)
		}
	}
	for addr, want := range map[string]string{
		"":                           remoteimage.CodeBadURL,
		"file:///etc/passwd":         remoteimage.CodeBadURL,
		"https://u:p@img.test/a.png": remoteimage.CodeBadURL,
		"http://127.0.0.1:9/a.png":   remoteimage.CodeNotAllowed,
		"http://[::1]/a.png":         remoteimage.CodeNotAllowed,
	} {
		for _, role := range []string{"", roleFull} {
			w := get(role, addr)
			if got := codeOf(w); got != want || w.Code >= 500 {
				t.Errorf("%q as %q: %d %s, want %s", addr, role, w.Code, got, want)
			}
		}
	}
}

// The address is read as written: escapes undone, plus signs kept (iOS leaves "+" unescaped in a query value).
func TestRemoteImageQueryURL(t *testing.T) {
	for raw, want := range map[string]string{
		"url=https://img.test/a+b.jpg?x%3D1%26y%3D2%23f&size=512":     "https://img.test/a+b.jpg?x=1&y=2#f",
		"size=512&url=https%3A%2F%2Fimg.test%2Fa%2Bb%20c.jpg&token=t": "https://img.test/a+b c.jpg",
		"size=512":                    "",
		"url=%zz":                     "",
		"urlx=https://img.test/a.jpg": "",
	} {
		if got := queryURL(raw); got != want {
			t.Errorf("queryURL(%q) = %q, want %q", raw, got, want)
		}
	}
}

func TestRemoteImageAnswers(t *testing.T) {
	w := httptest.NewRecorder()
	writeRemoteImage(w, &remoteimage.Image{Type: "image/jpeg", Data: []byte{0xff, 0xd8, 0xff, 0}})
	h := w.Result().Header
	if w.Code != http.StatusOK || h.Get("Content-Type") != "image/jpeg" || h.Get("X-Content-Type-Options") != "nosniff" ||
		h.Get("Content-Security-Policy") != "default-src 'none'; sandbox" || h.Get("Cache-Control") != "private, max-age=86400" || w.Body.Len() != 4 {
		t.Fatalf("picture answered %d %v", w.Code, h)
	}
	w = httptest.NewRecorder()
	writeRemoteImageError(w, &remoteimage.Error{Code: remoteimage.CodeRateLimited, Status: http.StatusTooManyRequests, RetryAfter: 1500 * time.Millisecond})
	if w.Code != http.StatusTooManyRequests || w.Header().Get("Retry-After") != "2" {
		t.Fatalf("limit answered %d, Retry-After %q", w.Code, w.Header().Get("Retry-After"))
	}
}

// manager.json's remote_images.doh (#9550): the default when it names none, off when empty, else what it names.
func TestRemoteImageDoHFromManagerConfig(t *testing.T) {
	s := newTestService(t, newFakeHub())
	if got := s.remoteImageDoH(); got != remoteimage.DefaultDoH {
		t.Fatalf("no manager.json: %q, want the default", got)
	}
	file := filepath.Join(t.TempDir(), "manager.json")
	s.repositoryConfig = file
	for body, want := range map[string]string{
		`{}`:                           remoteimage.DefaultDoH,
		`{"remote_images":{}}`:         remoteimage.DefaultDoH,
		`{"remote_images":{"doh":""}}`: "",
		`{"remote_images":{"doh":" https://doh.example.org/resolve "}}`: "https://doh.example.org/resolve",
	} {
		if err := os.WriteFile(file, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		if got := s.remoteImageDoH(); got != want {
			t.Errorf("%s: %q, want %q", body, got, want)
		}
	}
}
