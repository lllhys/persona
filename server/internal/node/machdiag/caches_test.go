package machdiag

import (
	"path/filepath"
	"slices"
	"testing"

	"github.com/nwverse/persona/server/internal/proto"
)

func noEnv(string) string { return "" }

// The node's catalog is the wire's: every id the manager accepts has an entry, and no other.
func TestCatalogIsTheWireList(t *testing.T) {
	var ids []string
	for _, c := range catalog {
		ids = append(ids, c.id)
	}
	if !slices.Equal(ids, proto.CacheIDs) {
		t.Fatalf("catalog %v, wire %v", ids, proto.CacheIDs)
	}
	var tids []string
	for _, s := range tools {
		tids = append(tids, s.id)
	}
	if !slices.Equal(tids, proto.ToolIDs) {
		t.Fatalf("tools %v, wire %v", tids, proto.ToolIDs)
	}
}

func cachePaths(goos, home string, env func(string) string) map[string]string {
	out := map[string]string{}
	for _, c := range resolveCaches(goos, home, env) {
		out[c.spec.id] = c.path
	}
	return out
}

func TestResolveCachesPerSystem(t *testing.T) {
	home := filepath.FromSlash("/home/someone")
	linux := cachePaths("linux", home, noEnv)
	for id, want := range map[string]string{
		"go-build":       "/home/someone/.cache/go-build",
		"go-mod":         "/home/someone/go/pkg/mod",
		"npm-cache":      "/home/someone/.npm/_cacache",
		"pip-cache":      "/home/someone/.cache/pip",
		"uv-cache":       "/home/someone/.cache/uv",
		"yarn-cache":     "/home/someone/.cache/yarn",
		"homebrew-cache": "/home/someone/.cache/Homebrew",
		"gradle-cache":   "/home/someone/.gradle/caches",
		"cargo-registry": "/home/someone/.cargo/registry",
	} {
		if linux[id] != filepath.FromSlash(want) {
			t.Errorf("linux %s = %q, want %q", id, linux[id], want)
		}
	}
	if _, ok := linux["xcode-derived-data"]; ok {
		t.Error("Linux has no Xcode")
	}
	mac := cachePaths("darwin", filepath.FromSlash("/Users/someone"), noEnv)
	for id, want := range map[string]string{
		"go-build":           "/Users/someone/Library/Caches/go-build",
		"pip-cache":          "/Users/someone/Library/Caches/pip",
		"uv-cache":           "/Users/someone/.cache/uv",
		"yarn-cache":         "/Users/someone/Library/Caches/Yarn",
		"homebrew-cache":     "/Users/someone/Library/Caches/Homebrew",
		"xcode-derived-data": "/Users/someone/Library/Developer/Xcode/DerivedData",
	} {
		if mac[id] != filepath.FromSlash(want) {
			t.Errorf("darwin %s = %q, want %q", id, mac[id], want)
		}
	}
	if got := cachePaths("windows", `C:\Users\someone`, noEnv); len(got) != 0 {
		t.Errorf("windows has caches: %v", got)
	}
}

// A toolchain's own variable moves its cache; one that points outside the home directory, or at the home directory
// itself, takes the cache out of the catalog.
func TestResolveCachesHonoursVariablesInsideHomeOnly(t *testing.T) {
	home := filepath.FromSlash("/home/someone")
	env := map[string]string{
		"GOCACHE":        filepath.FromSlash("/home/someone/build/go"),
		"GOMODCACHE":     filepath.FromSlash("/var/cache/gomod"),
		"PIP_CACHE_DIR":  filepath.FromSlash("/home/someone"),
		"UV_CACHE_DIR":   filepath.FromSlash("/home/someone/../other/uv"),
		"XDG_CACHE_HOME": filepath.FromSlash("/home/someone/xdg"),
		"CARGO_HOME":     "relative/cargo",
	}
	got := cachePaths("linux", home, func(k string) string { return env[k] })
	if got["go-build"] != filepath.FromSlash("/home/someone/build/go") {
		t.Errorf("GOCACHE: %q", got["go-build"])
	}
	if got["yarn-cache"] != filepath.FromSlash("/home/someone/xdg/yarn") {
		t.Errorf("XDG_CACHE_HOME: %q", got["yarn-cache"])
	}
	for _, id := range []string{"go-mod", "pip-cache", "uv-cache", "cargo-registry"} {
		if p, ok := got[id]; ok {
			t.Errorf("%s resolved to %q: outside home, home itself or relative", id, p)
		}
	}
}

func TestInUse(t *testing.T) {
	spec := func(id string) cacheSpec {
		i := slices.IndexFunc(catalog, func(c cacheSpec) bool { return c.id == id })
		return catalog[i]
	}
	running := map[string]bool{"python3.12": true, "gopher": true, "zsh": true}
	if !inUse(spec("pip-cache"), running) {
		t.Error("python3.12 uses pip's cache")
	}
	if inUse(spec("go-build"), running) {
		t.Error("gopher is not go")
	}
	if !inUse(spec("go-build"), map[string]bool{"gopls": true}) {
		t.Error("gopls uses the build cache")
	}
	if !inUse(spec("xcode-derived-data"), map[string]bool{"Xcode": true}) {
		t.Error("Xcode uses DerivedData")
	}
	if !inUse(spec("cargo-registry"), nil) {
		t.Error("without a process list a cache counts as in use")
	}
	if inUse(spec("cargo-registry"), map[string]bool{}) {
		t.Error("nothing runs: not in use")
	}
}

func TestTilde(t *testing.T) {
	home := filepath.FromSlash("/home/someone")
	for in, want := range map[string]string{
		"/home/someone/code":       "~/code",
		"/home/someone/.cache/pip": "~/.cache/pip",
		"/home/someone":            "~",
		"/home/someoneelse/x":      "/home/someoneelse/x",
	} {
		if got := tilde(home, filepath.FromSlash(in)); got != want && got != filepath.FromSlash(want) {
			t.Errorf("tilde(%q) = %q, want %q", in, got, want)
		}
	}
}
