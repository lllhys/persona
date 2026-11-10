package extmgr

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/extension"
	"github.com/nwverse/persona/server/internal/i18n"
)

// TestPreviewDownloadOutlivesTheApp (#3692): on a slow link a phone's synchronous preview timed out, the package's
// download went with it, and asking again downloaded it a second time beside the first. Now the operation the app
// left says why, the download goes on, the next preview joins it (one request) and reaches the consent; the user's
// Cancel while it downloads stops the download.
func TestPreviewDownloadOutlivesTheApp(t *testing.T) {
	h, phone, _ := marketHub(t)
	mux := h.routes()
	dir := uiPackage(t)
	v1 := tarDir(t, dir)
	gh := &fakeGitHub{release: "v1", files: map[string][]byte{}, assets: map[string][]byte{"example-ui-1.0.0.tar.gz": v1}, hold: make(chan struct{})}
	srv := httptest.NewTLSServer(gh)
	defer srv.Close()
	storage := httptest.NewTLSServer(gh)
	defer storage.Close()
	gh.storage = "https://storage.example.com"
	tr := srv.Client().Transport.(*http.Transport).Clone()
	tr.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
		if addr == "storage.example.com:443" {
			addr = storage.Listener.Addr().String()
		}
		return (&net.Dialer{}).DialContext(ctx, network, addr)
	}
	h.extensions.markets.client, h.extensions.markets.githubAPI = &http.Client{Transport: tr}, srv.URL
	gh.files[extension.IndexFile] = marketIndex(t, "Example plugins", marketEntry(t, dir, "release:v1/example-ui-1.0.0.tar.gz", v1))
	_, mk := call(t, mux, "POST", "/api/v1/extensions/-/markets", phone, `{"spec": "github:example/plugins"}`)
	market := mk["id"].(string)
	downloading := func(id string) func() bool {
		return func() bool {
			op := h.opByID(id)
			return op != nil && stepState(h.snapshot(op), extension.StepDownload).Bytes > 0
		}
	}
	latest := func() string {
		var id string
		var at time.Time
		for _, v := range h.opsView() {
			if v.StartedAt.After(at) {
				id, at = v.ID, v.StartedAt
			}
		}
		return id
	}

	// The app's synchronous preview, given up on with half of the package in.
	ctx, leave := context.WithCancel(context.Background())
	answered := make(chan int, 1)
	go func() {
		req := httptest.NewRequestWithContext(ctx, "POST", "/api/v1/extensions/-/catalog/preview", strings.NewReader(fmt.Sprintf(`{"market": %q, "extension": "example-ui"}`, market)))
		req.RemoteAddr = "127.0.0.1:12345"
		req.Header.Set("Authorization", "Bearer "+phone)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)
		answered <- w.Code
	}()
	eventually(t, 10*time.Second, "the first download", func() bool { id := latest(); return id != "" && downloading(id)() })
	first := latest()
	leave()
	<-answered
	v := waitOp(t, h, first, opFailed)
	var texts []string
	for _, l := range []i18n.Lang{i18n.En, "zh-Hans", "ja"} {
		texts = append(texts, i18n.T(l, "hub-extension_operations.client_left"))
	}
	if !slices.Contains(texts, v.Error) || !v.Retryable {
		t.Fatalf("the operation the app left: %+v", v)
	}

	// Asked again: it joins the download still going and reaches the consent, from one request.
	next := opCall(t, mux, "POST", "/api/v1/extensions/-/catalog/preview", phone, fmt.Sprintf(`{"market": %q, "extension": "example-ui", "async": true}`, market))
	eventually(t, 10*time.Second, "the second to join", downloading(next))
	close(gh.hold)
	if v = waitOp(t, h, next, opNeedsConsent, opFailed); v.State != opNeedsConsent {
		t.Fatalf("asked again: %+v", v)
	}
	gh.mu.Lock()
	gets, stops := gh.assetGets, gh.assetStops
	gh.mu.Unlock()
	if gets != 1 || stops != 0 {
		t.Fatalf("downloaded %d times, stopped %d", gets, stops)
	}
	if code, _ := call(t, mux, "DELETE", "/api/v1/extensions/-/operations/"+next, phone, ""); code != http.StatusNoContent {
		t.Fatalf("turned down: %d", code)
	}

	// Canceled while it downloads: the operation is canceled and the download stops (nothing in the cache).
	_ = os.RemoveAll(h.packageCache())
	gh.mu.Lock()
	gh.hold = make(chan struct{})
	gh.mu.Unlock()
	third := opCall(t, mux, "POST", "/api/v1/extensions/-/catalog/preview", phone, fmt.Sprintf(`{"market": %q, "extension": "example-ui", "async": true}`, market))
	eventually(t, 10*time.Second, "the third download", downloading(third))
	if code, _ := call(t, mux, "DELETE", "/api/v1/extensions/-/operations/"+third, phone, ""); code != http.StatusNoContent {
		t.Fatalf("cancel: %d", code)
	}
	waitOp(t, h, third, opCanceled)
	eventually(t, 10*time.Second, "the download to stop", func() bool { gh.mu.Lock(); defer gh.mu.Unlock(); return gh.assetStops == 1 })
	eventually(t, 10*time.Second, "nothing left in the cache", func() bool { entries, _ := os.ReadDir(h.packageCache()); return len(entries) == 0 })
}
