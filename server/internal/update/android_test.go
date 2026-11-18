package update

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

const testPackage = "app.example.persona"

var testCert = strings.Repeat("c", 64)

// androidRelease is a release's android.json and the APK it names, as scripts/release/android_tag_release.py
// publishes them.
type androidRelease struct {
	apk      []byte
	manifest map[string]any
	missing  bool // the release has no file under the name android.json gives
}

func newAndroidRelease(version string, code int64) androidRelease {
	apk := []byte("APK " + version + " " + strings.Repeat("x", 4096))
	sum := sha256.Sum256(apk)
	return androidRelease{apk: apk, manifest: map[string]any{
		"version": version, "name": "Persona-" + version + "-android.apk", "package_name": testPackage,
		"version_code": code, "version_name": version, "min_sdk": 29, "certificate_sha256": testCert,
		"sha256": hex.EncodeToString(sum[:]), "size": len(apk), "source_sha": strings.Repeat("a", 40), "notes": "",
	}}
}

func (r androidRelease) sha() string { return r.manifest["sha256"].(string) }

// serveAndroid serves r under /dl/ the way GitHub's releases/latest/download does: through a redirect. served counts
// the APK downloads.
func serveAndroid(t *testing.T, r *androidRelease) (base string, served *int) {
	t.Helper()
	served = new(int)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if strings.HasPrefix(req.URL.Path, "/dl/") {
			http.Redirect(w, req, "/assets/"+strings.TrimPrefix(req.URL.Path, "/dl/"), http.StatusFound)
			return
		}
		if r == nil {
			http.NotFound(w, req)
			return
		}
		switch req.URL.Path {
		case "/assets/android.json":
			json.NewEncoder(w).Encode(r.manifest)
		case "/assets/" + r.manifest["name"].(string):
			if r.missing {
				http.NotFound(w, req)
				return
			}
			*served++
			w.Write(r.apk)
		default:
			http.NotFound(w, req)
		}
	}))
	t.Cleanup(srv.Close)
	return srv.URL + "/dl", served
}

func readManifest(t *testing.T, dir string) map[string]any {
	t.Helper()
	var m map[string]any
	data, err := os.ReadFile(filepath.Join(dir, "latest.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatal(err)
	}
	return m
}

func names(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, e := range entries {
		out = append(out, e.Name())
	}
	return out
}

// A release's APK becomes the directory's, in the publisher's layout: <sha256>.apk, a gzip copy that decodes to it,
// and latest.json with everything android.json says (the phones read min_sdk and notes from it).
func TestSyncAndroidPublishesTheRelease(t *testing.T) {
	r := newAndroidRelease("0.3.0", 105000)
	base, served := serveAndroid(t, &r)
	dir := filepath.Join(t.TempDir(), "apps", "android")
	got, rel, err := SyncAndroid(context.Background(), http.DefaultClient, base, dir)
	if err != nil || got != AndroidUpdated || rel.VersionCode != 105000 || *served != 1 {
		t.Fatalf("sync: %v %+v %v, %d downloads", got, rel, err, *served)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, r.sha()+".apk")); !bytes.Equal(b, r.apk) {
		t.Fatal("the APK is not the release's")
	}
	f, err := os.Open(filepath.Join(dir, r.sha()+".apk.gz"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	zr, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	if b, _ := io.ReadAll(zr); !bytes.Equal(b, r.apk) || zr.Name != "" || !zr.ModTime.IsZero() {
		t.Fatalf("the gzip copy: name %q, time %v", zr.Name, zr.ModTime)
	}
	m := readManifest(t, dir)
	for _, k := range []string{"sha256", "version_name", "certificate_sha256", "package_name", "notes"} {
		if m[k] != r.manifest[k] {
			t.Fatalf("latest.json %s = %v, want %v", k, m[k], r.manifest[k])
		}
	}
	if m["min_sdk"] != float64(29) || m["version_code"] != float64(105000) {
		t.Fatalf("latest.json: %v", m)
	}
	want := []string{".publish.lock", r.sha() + ".apk", r.sha() + ".apk.gz", "latest.json"}
	if got := names(t, dir); !slices.Equal(got, want) {
		t.Fatalf("files %v, want %v", got, want)
	}

	// The next check finds it there and downloads nothing.
	if got, _, err := SyncAndroid(context.Background(), http.DefaultClient, base, dir); err != nil || got != AndroidCurrent || *served != 1 {
		t.Fatalf("again: %v %v, %d downloads", got, err, *served)
	}
}

type androidGitHubTransport struct{ host string }

func (t androidGitHubTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req = req.Clone(req.Context())
	req.URL.Scheme = "http"
	req.URL.Host = t.host
	return http.DefaultTransport.RoundTrip(req)
}

// GitHub's latest release can have no Android assets. Its newest earlier stable release that has them is the one to
// publish; draft and prerelease assets do not replace the stable channel.
func TestSyncAndroidFindsPreviousGitHubRelease(t *testing.T) {
	r := newAndroidRelease("0.3.0", 300)
	incomplete := newAndroidRelease("0.3.1", 301)
	served := 0
	download := "https://github.com/nwverse/persona/releases/download/"
	assets := func(tag string, withAPK bool) []map[string]any {
		out := []map[string]any{{"name": "android.json", "browser_download_url": download + tag + "/android.json"}}
		if withAPK {
			out = append(out, map[string]any{"name": r.manifest["name"], "browser_download_url": download + tag + "/" + r.manifest["name"].(string)})
		}
		return out
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		switch req.URL.Path {
		case "/repos/nwverse/persona/releases":
			if req.URL.Query().Get("per_page") != "100" {
				http.Error(w, "missing page size", http.StatusBadRequest)
				return
			}
			switch req.URL.Query().Get("page") {
			case "1":
				// Force the fallback past the first 100 stable releases without Android assets.
				releases := make([]map[string]any, 100)
				for i := range releases {
					releases[i] = map[string]any{"assets": []map[string]any{}}
				}
				json.NewEncoder(w).Encode(releases)
			case "2":
				json.NewEncoder(w).Encode([]map[string]any{
					{"tag_name": "v0.3.2-draft", "draft": true, "assets": assets("v0.3.2-draft", true)},
					{"tag_name": "v0.3.2-rc.1", "prerelease": true, "assets": assets("v0.3.2-rc.1", true)},
					{"tag_name": "v0.3.1", "assets": assets("v0.3.1", false)},
					{"tag_name": "v0.3.0", "assets": assets("v0.3.0", true)},
				})
			default:
				http.Error(w, "unexpected page", http.StatusBadRequest)
			}
		case "/nwverse/persona/releases/download/v0.3.1/android.json":
			json.NewEncoder(w).Encode(incomplete.manifest)
		case "/nwverse/persona/releases/download/v0.3.0/android.json":
			json.NewEncoder(w).Encode(r.manifest)
		case "/nwverse/persona/releases/download/v0.3.0/" + r.manifest["name"].(string):
			served++
			w.Write(r.apk)
		default:
			http.NotFound(w, req)
		}
	}))
	defer srv.Close()
	client := &http.Client{Transport: androidGitHubTransport{host: strings.TrimPrefix(srv.URL, "http://")}}
	dir := t.TempDir()
	got, rel, err := SyncAndroid(context.Background(), client, "https://github.com/nwverse/persona/releases/latest/download", dir)
	if err != nil || got != AndroidUpdated || rel == nil || rel.VersionCode != 300 || served != 1 {
		t.Fatalf("sync: %v %+v %v, %d downloads", got, rel, err, served)
	}
	if m := readManifest(t, dir); m["version_name"] != "0.3.0" {
		t.Fatalf("latest.json: %v", m)
	}
}

// A newer release replaces the published one; the one it replaces stays for phones that read it, the ones before go.
// What an interrupted sync left goes too; a publisher's staging directory stays.
func TestSyncAndroidReplacesAndPrunes(t *testing.T) {
	dir := t.TempDir()
	sync := func(r androidRelease) {
		t.Helper()
		base, _ := serveAndroid(t, &r)
		if got, _, err := SyncAndroid(context.Background(), http.DefaultClient, base, dir); err != nil || got != AndroidUpdated {
			t.Fatalf("sync %v: %v %v", r.manifest["version"], got, err)
		}
	}
	first, second, third := newAndroidRelease("0.1.0", 100), newAndroidRelease("0.2.0", 200), newAndroidRelease("0.3.0", 300)
	sync(first)
	sync(second)
	for _, leftover := range []string{"latest.json.new", third.sha() + ".apk.part"} {
		if err := os.WriteFile(filepath.Join(dir, leftover), []byte("half"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Mkdir(filepath.Join(dir, ".upload-abcdefgh"), 0o700); err != nil {
		t.Fatal(err)
	}
	sync(third)
	want := []string{".publish.lock", ".upload-abcdefgh", second.sha() + ".apk", second.sha() + ".apk.gz",
		third.sha() + ".apk", third.sha() + ".apk.gz", "latest.json"}
	slices.Sort(want)
	if got := names(t, dir); !slices.Equal(got, want) {
		t.Fatalf("files %v, want %v", got, want)
	}
	if readManifest(t, dir)["sha256"] != third.sha() {
		t.Fatal("latest.json is not the newest release's")
	}
}

// It never goes back: a release with the version code published here, or a lower one, changes nothing (the publisher
// over SSH may be ahead of the releases, building every commit on main).
func TestSyncAndroidNeverGoesBack(t *testing.T) {
	for _, code := range []int64{500, 400} {
		dir := t.TempDir()
		published := newAndroidRelease("main", 500)
		base, _ := serveAndroid(t, &published)
		if _, _, err := SyncAndroid(context.Background(), http.DefaultClient, base, dir); err != nil {
			t.Fatal(err)
		}
		before := readManifest(t, dir)
		r := newAndroidRelease("0.3.0", code)
		base, served := serveAndroid(t, &r)
		got, _, err := SyncAndroid(context.Background(), http.DefaultClient, base, dir)
		if err != nil || got != AndroidCurrent || *served != 0 {
			t.Fatalf("code %d: %v %v, %d downloads", code, got, err, *served)
		}
		if after := readManifest(t, dir); after["sha256"] != before["sha256"] {
			t.Fatalf("code %d replaced the published APK", code)
		}
	}
}

// An APK of another package, or signed with another key, is never put over the published one: the phones could not
// install it over the app they have. Neither is anything put over a latest.json that does not read.
func TestSyncAndroidRefusesAnotherApp(t *testing.T) {
	for _, c := range []struct {
		name    string
		current string
		want    string
	}{
		{"package", `{"package_name":"app.example.other","version_code":1,"sha256":"%s","certificate_sha256":"` + testCert + `"}`, "app.example.other"},
		{"key", `{"package_name":"` + testPackage + `","version_code":1,"sha256":"%s","certificate_sha256":"` + strings.Repeat("D", 64) + `"}`, "another key"},
		{"unreadable", `{"version_code":"one"}`, "does not read"},
	} {
		t.Run(c.name, func(t *testing.T) {
			dir := t.TempDir()
			old := strings.Repeat("e", 64)
			current := c.current
			if strings.Contains(current, "%s") {
				current = strings.Replace(current, "%s", old, 1)
			}
			if err := os.WriteFile(filepath.Join(dir, "latest.json"), []byte(current), 0o600); err != nil {
				t.Fatal(err)
			}
			r := newAndroidRelease("0.3.0", 300)
			base, served := serveAndroid(t, &r)
			got, _, err := SyncAndroid(context.Background(), http.DefaultClient, base, dir)
			if err == nil || !strings.Contains(err.Error(), c.want) || got != AndroidNone || *served != 0 {
				t.Fatalf("%v %v, %d downloads", got, err, *served)
			}
			if b, _ := os.ReadFile(filepath.Join(dir, "latest.json")); string(b) != current {
				t.Fatal("latest.json changed")
			}
		})
	}
	// A latest.json from before certificates were recorded (no package_name, no certificate_sha256) is replaced.
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "latest.json"), []byte(`{"version_code":1,"sha256":"`+strings.Repeat("e", 64)+`"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	r := newAndroidRelease("0.3.0", 300)
	base, _ := serveAndroid(t, &r)
	if got, _, err := SyncAndroid(context.Background(), http.DefaultClient, base, dir); err != nil || got != AndroidUpdated {
		t.Fatalf("an older manifest: %v %v", got, err)
	}
}

// A download that is not the APK android.json names (other bytes, cut short, longer, missing) leaves nothing behind
// and publishes nothing.
func TestSyncAndroidChecksTheDownload(t *testing.T) {
	for name, edit := range map[string]func(*androidRelease){
		"other bytes": func(r *androidRelease) { r.apk = bytes.Repeat([]byte{0}, len(r.apk)) },
		"short":       func(r *androidRelease) { r.apk = r.apk[:10] },
		"longer":      func(r *androidRelease) { r.apk = append(r.apk, "and more"...) },
		"missing":     func(r *androidRelease) { r.missing = true },
	} {
		t.Run(name, func(t *testing.T) {
			r := newAndroidRelease("0.3.0", 300)
			edit(&r)
			base, _ := serveAndroid(t, &r)
			dir := t.TempDir()
			got, _, err := SyncAndroid(context.Background(), http.DefaultClient, base, dir)
			if err == nil || got != AndroidNone {
				t.Fatalf("%v %v", got, err)
			}
			if got := names(t, dir); !slices.Equal(got, []string{".publish.lock"}) {
				t.Fatalf("left %v", got)
			}
		})
	}
}

// No android.json: the release carries no Android app, which is no error and changes nothing. One that does not say
// what the sync needs is an error, before anything is written.
func TestSyncAndroidManifest(t *testing.T) {
	base, _ := serveAndroid(t, nil)
	dir := filepath.Join(t.TempDir(), "android")
	if got, rel, err := SyncAndroid(context.Background(), http.DefaultClient, base, dir); err != nil || got != AndroidNone || rel != nil {
		t.Fatalf("no android.json: %v %+v %v", got, rel, err)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatal("made the directory for nothing")
	}
	for field, value := range map[string]any{
		"name": "../persona.apk", "package_name": "", "version_code": 0, "sha256": strings.Repeat("A", 64),
		"certificate_sha256": "", "size": androidAPKLimit + 1,
	} {
		r := newAndroidRelease("0.3.0", 300)
		r.manifest[field] = value
		base, served := serveAndroid(t, &r)
		if got, _, err := SyncAndroid(context.Background(), http.DefaultClient, base, dir); err == nil || got != AndroidNone || *served != 0 {
			t.Fatalf("%s %v: %v %v", field, value, got, err)
		}
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatal("wrote something for a manifest that does not read")
	}
}
