package devices

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/nwverse/persona/server/internal/devicewire"
	"github.com/nwverse/persona/server/internal/update"
)

// TestHelperDownload: the iPhone bridge for a computer comes from the release of the hub's version, else the newest,
// with the addresses to fetch it from in order; no source, or no build for that platform, says so (#8199).
func TestHelperDownload(t *testing.T) {
	r := newFakeRelease(t)
	bridge := func(name string) update.File {
		return update.File{Name: name, Size: 3, SHA256: strings.Repeat("a", 64), BinarySHA256: strings.Repeat("b", 64)}
	}
	put := func(path string, m update.Manifest) {
		m.Build, m.Files = 202611150000, map[string]update.File{"linux-amd64": bridge("personad-linux-amd64.gz")}
		b, _ := json.Marshal(m)
		r.mu.Lock()
		r.files[path] = b
		r.mu.Unlock()
	}
	put("/v1.2.0-202611150000/manifest.json", update.Manifest{Version: "v1.2.0-202611150000", Helpers: map[string]map[string]update.File{
		"persona-iphone-bridge": {"darwin-arm64": bridge("persona-iphone-bridge-darwin-arm64.gz")}}})
	put("/latest.json", update.Manifest{Version: "v1.3.0-202611160000", Helpers: map[string]map[string]update.File{
		"persona-iphone-bridge": {"linux-amd64": bridge("persona-iphone-bridge-linux-amd64.gz")}}})
	d := New(Options{})
	ctx := context.Background()

	d.cfg = devicewire.Config{HubVersion: "v1.2.0-202611150000", NodePackagesURL: r.srv.URL + "/"}
	urls, f, err := d.HelperDownload(ctx, "darwin", "arm64", "persona-iphone-bridge")
	if err != nil || f.Name != "persona-iphone-bridge-darwin-arm64.gz" ||
		strings.Join(urls, " ") != r.srv.URL+"/v1.2.0-202611150000/persona-iphone-bridge-darwin-arm64.gz" {
		t.Fatalf("the hub's release: %v %+v %v", urls, f, err)
	}
	// Not in the hub's release: the newest one's, from dl/<version>/ first, then beside latest.json.
	urls, f, err = d.HelperDownload(ctx, "linux", "amd64", "persona-iphone-bridge")
	if err != nil || f.Name != "persona-iphone-bridge-linux-amd64.gz" || strings.Join(urls, " ") !=
		r.srv.URL+"/v1.3.0-202611160000/persona-iphone-bridge-linux-amd64.gz "+r.srv.URL+"/persona-iphone-bridge-linux-amd64.gz" {
		t.Fatalf("the newest release: %v %+v %v", urls, f, err)
	}
	if _, _, err := d.HelperDownload(ctx, "linux", "arm64", "persona-iphone-bridge"); !errors.Is(err, ErrNoHelper) {
		t.Fatalf("no build for the platform: %v", err)
	}
	d.cfg = devicewire.Config{HubVersion: "dev"}
	if _, _, err := d.HelperDownload(ctx, "darwin", "arm64", "persona-iphone-bridge"); !errors.Is(err, ErrNoHelperSource) {
		t.Fatalf("no source: %v", err)
	}
	r.mu.Lock()
	r.down = true
	r.mu.Unlock()
	d.cfg = devicewire.Config{HubVersion: "dev", NodePackagesURL: r.srv.URL}
	if _, _, err := d.HelperDownload(ctx, "darwin", "arm64", "persona-iphone-bridge"); err == nil || errors.Is(err, ErrNoHelper) {
		t.Fatalf("the site away: %v", err)
	}
}
