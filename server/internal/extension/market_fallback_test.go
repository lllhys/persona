package extension

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

// rateLimitedAPI answers every request with GitHub's 403 for an hour used up.
func rateLimitedAPI(t *testing.T, calls *atomic.Int32) *httptest.Server {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("X-RateLimit-Remaining", "0")
		w.WriteHeader(http.StatusForbidden)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// fileHost serves files by path; anything else is a 404. It also counts what it was asked and whether a token came.
type fileHost struct {
	srv    *httptest.Server
	files  map[string][]byte
	asked  []string
	tokens atomic.Int32
}

func newFileHost(t *testing.T, files map[string][]byte) *fileHost {
	f := &fileHost{files: files}
	f.srv = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.asked = append(f.asked, r.URL.Path)
		if r.Header.Get("Authorization") != "" {
			f.tokens.Add(1)
		}
		b, ok := f.files[r.URL.Path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write(b)
	}))
	t.Cleanup(f.srv.Close)
	return f
}

func signedIndexFiles(t *testing.T, extra map[string]any) ([]byte, []byte) {
	key, sign := testKey(t)
	ix := map[string]any{"market_version": 1, "name": "Example", "signing_key": key, "extensions": []any{}}
	for k, v := range extra {
		ix[k] = v
	}
	raw, _ := json.Marshal(ix)
	return sign(raw), nil
}

// A public GitHub market without a token that the API rate-limits is read from raw.githubusercontent.com: the same
// index and signature, nothing sent there but the request.
func TestGitHubRateLimitFallsBackToRaw(t *testing.T) {
	var apiCalls atomic.Int32
	api := rateLimitedAPI(t, &apiCalls)
	raw, _ := signedIndexFiles(t, nil)
	host := newFileHost(t, map[string][]byte{
		"/example/market/HEAD/" + IndexFile: raw,
	})
	s, _ := ParseMarket("github:example/market")
	mk := &Market{Spec: s, Client: host.srv.Client(), GitHubAPI: api.URL, GitHubRaw: host.srv.URL}
	mk.Client.Transport.(*http.Transport).TLSClientConfig.InsecureSkipVerify = true // two test servers, one client
	f, err := mk.FetchSigned(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if string(f.Raw) != string(raw) || f.Index.Name != "Example" {
		t.Fatalf("read %s", f.Raw)
	}
	if apiCalls.Load() == 0 {
		t.Fatal("the API was not asked first")
	}
	// A branch is its path on raw.
	host.asked = nil
	s, _ = ParseMarket("github:example/market@dev")
	host.files["/example/market/dev/"+IndexFile] = raw
	mk = &Market{Spec: s, Client: mk.Client, GitHubAPI: api.URL, GitHubRaw: host.srv.URL}
	if _, err := mk.FetchSigned(context.Background()); err != nil || host.asked[0] != "/example/market/dev/"+IndexFile {
		t.Fatalf("branch: %v %q", err, host.asked)
	}
}

// What the fallback does not change: a market with a token stays on the API (its limit is the token's, and the
// token goes nowhere else); a private repository, not on raw, still says it is rate-limited; a test's API never
// reaches the real raw host.
func TestGitHubRateLimitFallbackLimits(t *testing.T) {
	var apiCalls atomic.Int32
	api := rateLimitedAPI(t, &apiCalls)
	raw, _ := signedIndexFiles(t, nil)
	host := newFileHost(t, map[string][]byte{
		"/example/market/HEAD/" + IndexFile: raw,
	})
	s, _ := ParseMarket("github:example/market")
	client := host.srv.Client()
	client.Transport.(*http.Transport).TLSClientConfig.InsecureSkipVerify = true

	mk := &Market{Spec: s, Token: "secret", Client: client, GitHubAPI: api.URL, GitHubRaw: host.srv.URL}
	if _, err := mk.FetchSigned(context.Background()); !errors.Is(err, ErrRateLimited) || len(host.asked) != 0 || host.tokens.Load() != 0 {
		t.Fatalf("with a token: %v, raw asked %q", err, host.asked)
	}
	s, _ = ParseMarket("github:example/private")
	mk = &Market{Spec: s, Client: client, GitHubAPI: api.URL, GitHubRaw: host.srv.URL}
	if _, err := mk.FetchSigned(context.Background()); err == nil || !strings.Contains(err.Error(), "404") {
		t.Fatalf("private repository: %v", err)
	}
	if raw, _, ok := (&Market{Spec: s, GitHubAPI: api.URL}).githubFallback(); ok {
		t.Fatalf("a test API falls back to %q", raw)
	}
	if raw, web, ok := (&Market{Spec: s}).githubFallback(); !ok || raw != "https://raw.githubusercontent.com" || web != "https://github.com" {
		t.Fatalf("real hosts: %q %q %v", raw, web, ok)
	}
	https, _ := ParseMarket("https://example.com/extensions/")
	if _, _, ok := (&Market{Spec: https}).githubFallback(); ok {
		t.Fatal("an https market falls back")
	}
}

// A raw-host response is one envelope: no detached signature race exists.
func TestGitHubRawSingleDocument(t *testing.T) {
	var apiCalls atomic.Int32
	api := rateLimitedAPI(t, &apiCalls)
	raw, _ := signedIndexFiles(t, nil)
	host := newFileHost(t, map[string][]byte{"/example/market/HEAD/" + IndexFile: raw})
	spec, _ := ParseMarket("github:example/market")
	client := host.srv.Client()
	client.Transport.(*http.Transport).TLSClientConfig.InsecureSkipVerify = true
	mk := &Market{Spec: spec, Client: client, GitHubAPI: api.URL, GitHubRaw: host.srv.URL}
	if _, err := mk.FetchSigned(context.Background()); err != nil || len(host.asked) != 1 {
		t.Fatalf("single document: %v %v", err, host.asked)
	}
}

// Packages come along: a relative path from raw, a release asset from github.com's download link; both still
// checked against the index's sha256.
func TestGitHubRateLimitedDownloads(t *testing.T) {
	var apiCalls atomic.Int32
	api := rateLimitedAPI(t, &apiCalls)
	pkg := []byte("package bytes")
	sum := sha256.Sum256(pkg)
	host := newFileHost(t, map[string][]byte{
		"/example/market/HEAD/dist/x.tar.gz":                pkg,
		"/example/market/releases/download/v1.0.0/x.tar.gz": pkg,
	})
	s, _ := ParseMarket("github:example/market")
	client := host.srv.Client()
	client.Transport.(*http.Transport).TLSClientConfig.InsecureSkipVerify = true
	mk := &Market{Spec: s, Client: client, GitHubAPI: api.URL, GitHubRaw: host.srv.URL, GitHubWeb: host.srv.URL}
	for _, ref := range []string{"dist/x.tar.gz", "release:v1.0.0/x.tar.gz"} {
		file, _, err := mk.Download(context.Background(), &MarketPackage{URL: ref, SHA256: hex.EncodeToString(sum[:])}, filepath.Join(t.TempDir(), "d"))
		if err != nil {
			t.Fatalf("%s: %v", ref, err)
		}
		if b, _ := os.ReadFile(file); string(b) != string(pkg) {
			t.Fatalf("%s: %q", ref, b)
		}
	}
	other := sha256.Sum256([]byte("something else"))
	if _, _, err := mk.Download(context.Background(), &MarketPackage{URL: "dist/x.tar.gz", SHA256: hex.EncodeToString(other[:])}, filepath.Join(t.TempDir(), "d")); !errors.Is(err, ErrChecksum) {
		t.Fatalf("a package from raw is not checked: %v", err)
	}
}

// Committed archives and plain nonofficial catalogs use the same credential and
// configured ref. A commit ref pins both reads without a new package locator.
func TestGitHubRepositoryArchiveDistribution(t *testing.T) {
	archive := tarGz(t, map[string]string{ManifestFile: exampleManifest, "bin/notes-linux": "#!/bin/sh\n"})
	pkg := MarketPackage{URL: "packages/example-notes/1.2.0/example-notes-1.2.0.tar.gz", SHA256: ociSum(archive), Size: int64(len(archive))}
	catalog, err := json.Marshal(MarketIndex{MarketVersion: 1, Name: "Example market", Extensions: []MarketEntry{{Manifest: json.RawMessage(exampleManifest), Packages: []MarketPackage{pkg}}}})
	if err != nil {
		t.Fatal(err)
	}
	for _, ref := range []string{"", "main", strings.Repeat("a", 40)} {
		t.Run("ref="+ref, func(t *testing.T) {
			var catalogs, archives atomic.Int32
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet || r.Header.Get("Authorization") != "Bearer fixture-token" || r.Header.Get("Accept") != "application/vnd.github.raw" || r.Header.Get("X-GitHub-Api-Version") != "2022-11-28" || r.URL.Query().Get("ref") != ref {
					t.Error("repository read did not preserve credential, raw media type or ref")
					w.WriteHeader(http.StatusBadRequest)
					return
				}
				switch r.URL.Path {
				case "/repos/example/market/contents/" + IndexFile:
					catalogs.Add(1)
					_, _ = w.Write(catalog)
				case "/repos/example/market/contents/" + pkg.URL:
					archives.Add(1)
					_, _ = w.Write(archive)
				default:
					t.Error("unexpected repository request")
					http.NotFound(w, r)
				}
			}))
			defer server.Close()
			market := &Market{Spec: MarketSpec{Kind: "github", Owner: "example", Repo: "market", Ref: ref}, Token: "fixture-token", Client: server.Client(), GitHubAPI: server.URL}
			f, err := market.FetchSigned(t.Context())
			if err != nil || len(f.Warnings) != 0 || len(f.Index.Extensions) != 1 {
				t.Fatalf("plain nonofficial catalog: %v", err)
			}
			if f.Index.Extensions[0].verifiedKey != "" {
				t.Fatal("unsigned catalog acquired verified provenance")
			}
			// Installation creates a new Market; no fetch-time state is needed.
			installer := *market
			file, name, err := installer.Download(t.Context(), f.Index.Extensions[0].PackageFor("linux-amd64"), t.TempDir())
			if err != nil || name != pkg.URL {
				t.Fatalf("repository archive: name=%q, error=%v", name, err)
			}
			if got, err := fileSum(file); err != nil || got != pkg.SHA256 || catalogs.Load() != 1 || archives.Load() != 1 {
				t.Fatal("archive was not read and checksum-verified through the API")
			}
		})
	}
}

func TestGitHubRepositoryArchiveFailures(t *testing.T) {
	data := []byte("repository archive fixture")
	var calls atomic.Int32
	api := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Header.Get("Authorization") != "Bearer fixture-token" {
			t.Error("repository archive missing its market credential")
		}
		_, _ = w.Write(data)
	}))
	defer api.Close()
	market := &Market{Spec: MarketSpec{Kind: "github", Owner: "example", Repo: "market"}, Token: "fixture-token", Client: api.Client(), GitHubAPI: api.URL}
	for _, path := range []string{"../archive.tar.gz", "packages/../archive.tar.gz", "/archive.tar.gz", "//other.example.com/archive.tar.gz", "packages//archive.tar.gz", "packages/./archive.tar.gz"} {
		if _, _, err := market.Download(t.Context(), &MarketPackage{URL: path, SHA256: ociSum(data)}, t.TempDir()); err == nil {
			t.Fatalf("unsafe relative path accepted: %q", path)
		}
	}
	if calls.Load() != 0 {
		t.Fatal("unsafe relative path reached the repository API")
	}
	for _, tc := range []struct {
		name string
		pkg  MarketPackage
	}{
		{"checksum", MarketPackage{URL: "packages/archive.tar.gz", SHA256: strings.Repeat("0", 64)}},
		{"size", MarketPackage{URL: "packages/archive.tar.gz", SHA256: ociSum(data), Size: int64(len(data) + 1)}},
		{"declared-limit", MarketPackage{URL: "packages/archive.tar.gz", SHA256: ociSum(data), Size: PackageMost + 1}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			_, _, err := market.Download(t.Context(), &tc.pkg, dir)
			if err == nil || tc.name == "checksum" && !errors.Is(err, ErrChecksum) {
				t.Fatalf("invalid repository archive accepted: %v", err)
			}
			if _, err := os.Stat(filepath.Join(dir, "download")); !os.IsNotExist(err) {
				t.Fatal("failed archive left a staging download")
			}
		})
	}
	if calls.Load() != 2 {
		t.Fatal("declared oversize archive should fail before the API request")
	}
	var bounded strings.Builder
	if err := market.getRepoFile(t.Context(), "packages/archive.tar.gz", &bounded, 8); err == nil || bounded.Len() != 9 {
		t.Fatal("repository read did not stop at limit plus one")
	}
}

// Tokens survive same-origin redirects, but not an external hop or its return
// to the API. Unsafe redirects fail before reaching their destination.
func TestGitHubRepositoryArchiveRedirectIsolation(t *testing.T) {
	data := []byte("redirected repository archive")
	for _, mode := range []string{"same-origin", "external-return", "downgrade", "userinfo"} {
		t.Run(mode, func(t *testing.T) {
			var apiCalls, anonymousCalls atomic.Int32
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if strings.HasPrefix(r.URL.Path, "/repos/") {
					apiCalls.Add(1)
					if r.Host != "api.example.com" || r.Header.Get("Authorization") != "Bearer fixture-token" {
						t.Error("initial API read missing its origin-bound token")
					}
					target := "https://api.example.com/archive"
					switch mode {
					case "external-return":
						target = "https://cdn.example.com/archive"
					case "downgrade":
						target = "http://api.example.com/archive"
					case "userinfo":
						target = "https://user:pass@cdn.example.com/archive"
					}
					http.Redirect(w, r, target, http.StatusFound)
					return
				}
				if mode == "same-origin" {
					if r.Header.Get("Authorization") != "Bearer fixture-token" {
						t.Error("same-origin redirect dropped the token")
					}
				} else {
					anonymousCalls.Add(1)
					if r.Header.Get("Authorization") != "" {
						t.Error("redirect leaked or restored the token")
					}
				}
				if r.Host == "cdn.example.com" {
					http.Redirect(w, r, "https://api.example.com/archive", http.StatusFound)
					return
				}
				_, _ = w.Write(data)
			}))
			defer server.Close()
			target, _ := url.Parse(server.URL)
			client := &http.Client{Transport: mappedRegistryTransport{target, server.Client().Transport}}
			market := &Market{Spec: MarketSpec{Kind: "github", Owner: "example", Repo: "market"}, Token: "fixture-token", Client: client, GitHubAPI: "https://api.example.com"}
			pkg := &MarketPackage{URL: "packages/archive.tar.gz", SHA256: ociSum(data), Size: int64(len(data))}
			_, _, err := market.Download(t.Context(), pkg, t.TempDir())
			blocked := mode == "downgrade" || mode == "userinfo"
			if (err != nil) != blocked || apiCalls.Load() != 1 {
				t.Fatalf("redirect outcome: %v", err)
			}
			wantAnonymous := int32(0)
			if mode == "external-return" {
				wantAnonymous = 2
			}
			if anonymousCalls.Load() != wantAnonymous {
				t.Fatal("unexpected redirect reached a destination")
			}
		})
	}
}
