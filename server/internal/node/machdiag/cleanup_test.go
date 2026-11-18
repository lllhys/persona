//go:build linux || darwin

package machdiag

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/proto"
	"github.com/nwverse/persona/server/internal/rpc"
)

// testSystem is a computer whose home is a temporary folder, with the given processes running and no environment.
func testSystem(t *testing.T, running ...string) *system {
	t.Helper()
	home := t.TempDir()
	if real, err := filepath.EvalSymlinks(home); err == nil {
		home = real // macOS's temporary folder is behind /var → /private/var
	}
	free := int64(1000)
	return &system{
		goos:   runtime.GOOS,
		home:   home,
		getenv: func(string) string { return "" },
		processes: func(context.Context) ([]process, error) {
			var out []process
			for _, n := range running {
				out = append(out, process{name: n, rss: 1})
			}
			return out, nil
		},
		memTotal:  func() int64 { return 8 << 30 },
		volumes:   func() []proto.DiskVolume { return []proto.DiskVolume{{Mount: "/", Used: 1, Total: 2}} },
		available: func(string) (int64, error) { free += 500; return free, nil },
		remove:    removeContents,
		tools:     &toolProber{goos: runtime.GOOS, lookPath: func(string) (string, error) { return "", errors.New("none") }, now: time.Now},
		now:       func() time.Time { return time.Unix(1794000000, 0) },
	}
}

// cacheDir is a catalog entry's folder under the test system's home.
func cacheDir(t *testing.T, s *system, id string) string {
	t.Helper()
	for _, c := range resolveCaches(s.goos, s.home, s.getenv) {
		if c.spec.id == id {
			return c.path
		}
	}
	t.Fatalf("no %s on %s", id, s.goos)
	return ""
}

func itemsByID(r proto.CleanupResult) map[string]proto.CleanupItem {
	out := map[string]proto.CleanupItem{}
	for _, it := range r.Items {
		out[it.ID] = it
	}
	return out
}

func TestCleanupEmptiesCachesAndKeepsTheirFolders(t *testing.T) {
	s := testSystem(t, "zsh")
	build := cacheDir(t, s, "go-build")
	writeFile(t, filepath.Join(build, "00", "a-d"), 8192)
	writeFile(t, filepath.Join(build, "README"), 100)
	// Go's module cache is read-only on purpose.
	mod := cacheDir(t, s, "go-mod")
	writeFile(t, filepath.Join(mod, "example.com", "m@v1.0.0", "go.mod"), 100)
	if err := os.Chmod(filepath.Join(mod, "example.com", "m@v1.0.0", "go.mod"), 0o444); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(filepath.Join(mod, "example.com", "m@v1.0.0"), 0o555); err != nil {
		t.Fatal(err)
	}
	// A link inside a cache is removed as a link: what it points at stays.
	keep := filepath.Join(s.home, "work", "keep.txt")
	writeFile(t, keep, 10)
	if err := os.Symlink(filepath.Dir(keep), filepath.Join(build, "link")); err != nil {
		t.Fatal(err)
	}

	r, err := s.cleanup(context.Background(), []string{"go-build", "go-mod", "go-build"})
	if err != nil {
		t.Fatal(err)
	}
	items := itemsByID(r)
	if len(r.Items) != 2 || items["go-build"].Status != proto.CleanupCleaned || items["go-mod"].Status != proto.CleanupCleaned {
		t.Fatalf("items = %+v", r.Items)
	}
	if items["go-build"].Bytes <= 0 {
		t.Fatalf("go-build held %d bytes", items["go-build"].Bytes)
	}
	for _, dir := range []string{build, mod} {
		if !isEmptyDir(dir) {
			t.Errorf("%s is not empty", dir)
		}
	}
	if _, err := os.Stat(keep); err != nil {
		t.Fatalf("the link's target was removed: %v", err)
	}
	if r.AvailableBefore != 1500 || r.AvailableAfter != 2000 || r.FreedBytes != 500 {
		t.Fatalf("measured %+v", r)
	}
}

// Official Yarn launched through Corepack runs as node, so an active Yarn cache must survive cleanup.
func TestCleanupYarnCacheWhileNodeRuns(t *testing.T) {
	s := testSystem(t, "node")
	entry := filepath.Join(cacheDir(t, s, "yarn-cache"), "v6", "npm-example-1.0.0", "node_modules", "example", "package.json")
	writeFile(t, entry, 4096)

	r, err := s.cleanup(context.Background(), []string{"yarn-cache"})
	if err != nil {
		t.Fatal(err)
	}
	item := itemsByID(r)["yarn-cache"]
	info, statErr := os.Stat(entry)
	if item.Status != proto.CleanupInUse || statErr != nil {
		t.Fatalf("Yarn running as node: item = %+v, cache entry stat = %v", item, statErr)
	}
	if info.Size() != 4096 {
		t.Fatalf("Yarn cache entry size = %d, want 4096", info.Size())
	}
}

func TestCleanupYarnCacheWhenUnused(t *testing.T) {
	s := testSystem(t, "zsh")
	entry := filepath.Join(cacheDir(t, s, "yarn-cache"), "v6", "npm-example-1.0.0", "node_modules", "example", "package.json")
	writeFile(t, entry, 4096)

	r, err := s.cleanup(context.Background(), []string{"yarn-cache"})
	if err != nil {
		t.Fatal(err)
	}
	if item := itemsByID(r)["yarn-cache"]; item.Status != proto.CleanupCleaned {
		t.Fatalf("unused Yarn cache: %+v", item)
	}
	if _, err := os.Stat(entry); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("unused Yarn cache entry still exists or stat failed unexpectedly: %v", err)
	}
}

func TestCleanupRefusesWhatItMayNotTouch(t *testing.T) {
	s := testSystem(t, "python3.12")
	pip := cacheDir(t, s, "pip-cache")
	writeFile(t, filepath.Join(pip, "http", "x"), 4096)

	// A cache reached through a link to a folder outside the home directory is not emptied.
	outside := t.TempDir()
	writeFile(t, filepath.Join(outside, "cargo", "registry", "index", "x"), 4096)
	if err := os.Symlink(filepath.Join(outside, "cargo"), filepath.Join(s.home, ".cargo")); err != nil {
		t.Fatal(err)
	}
	// A cache folder that is itself a link.
	writeFile(t, filepath.Join(outside, "gradle", "x"), 4096)
	if err := os.MkdirAll(filepath.Join(s.home, ".gradle"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(outside, "gradle"), filepath.Join(s.home, ".gradle", "caches")); err != nil {
		t.Fatal(err)
	}

	r, err := s.cleanup(context.Background(), []string{"pip-cache", "cargo-registry", "gradle-cache", "uv-cache"})
	if err != nil {
		t.Fatal(err)
	}
	items := itemsByID(r)
	if it := items["pip-cache"]; it.Status != proto.CleanupInUse || it.Bytes <= 0 {
		t.Errorf("pip while python runs: %+v", it)
	}
	if isEmptyDir(pip) {
		t.Error("an in-use cache was emptied")
	}
	for _, id := range []string{"cargo-registry", "gradle-cache"} {
		if it := items[id]; it.Status != proto.CleanupFailed || it.Error == "" {
			t.Errorf("%s through a link: %+v", id, it)
		}
	}
	if isEmptyDir(filepath.Join(outside, "cargo", "registry")) || isEmptyDir(filepath.Join(outside, "gradle")) {
		t.Fatal("a folder outside the home directory was emptied")
	}
	if it := items["uv-cache"]; it.Status != proto.CleanupMissing || it.Bytes != 0 {
		t.Errorf("uv, not there: %+v", it)
	}
	if r.FreedBytes < 0 {
		t.Fatalf("freed %d", r.FreedBytes)
	}
}

func TestCleanupRefusesUnknownIDs(t *testing.T) {
	s := testSystem(t)
	removed := false
	s.remove = func(context.Context, string) error { removed = true; return nil }
	for _, ids := range [][]string{nil, {}, {"go-build", "../../etc"}, {"/home/someone/.cache"}} {
		_, err := s.cleanup(context.Background(), ids)
		var re *rpc.Error
		if !errors.As(err, &re) || re.Code != codeBadRequest {
			t.Errorf("%v: %v", ids, err)
		}
	}
	if removed {
		t.Fatal("something was removed")
	}
}

// Freed space never reads below zero (something else wrote while the caches were emptied).
func TestCleanupFreedIsNeverNegative(t *testing.T) {
	s := testSystem(t)
	free := int64(5000)
	s.available = func(string) (int64, error) { free -= 100; return free, nil }
	writeFile(t, filepath.Join(cacheDir(t, s, "go-build"), "x"), 10)
	r, err := s.cleanup(context.Background(), []string{"go-build"})
	if err != nil || r.FreedBytes != 0 || r.AvailableBefore != 4900 || r.AvailableAfter != 4800 {
		t.Fatalf("%+v %v", r, err)
	}
}

// A remover that fails reports it in the item, with the home directory written as "~".
func TestCleanupReportsAFailedRemoval(t *testing.T) {
	s := testSystem(t)
	dir := cacheDir(t, s, "go-build")
	writeFile(t, filepath.Join(dir, "x"), 10)
	s.remove = func(_ context.Context, d string) error {
		return &os.PathError{Op: "unlinkat", Path: d, Err: os.ErrPermission}
	}
	r, _ := s.cleanup(context.Background(), []string{"go-build"})
	it := itemsByID(r)["go-build"]
	if it.Status != proto.CleanupFailed || !strings.Contains(it.Error, "~/") || strings.Contains(it.Error, s.home) {
		t.Fatalf("%+v", it)
	}
}

func TestGuardCacheDir(t *testing.T) {
	s := testSystem(t)
	inside := filepath.Join(s.home, ".cache", "pip")
	if err := os.MkdirAll(inside, 0o755); err != nil {
		t.Fatal(err)
	}
	if got, err := guardCacheDir(s.home, inside); err != nil || got != inside {
		t.Fatalf("inside: %q %v", got, err)
	}
	for _, p := range []string{s.home, filepath.Dir(s.home), filepath.Join(s.home, "..", "x"), "/"} {
		if _, err := guardCacheDir(s.home, p); !errors.Is(err, errOutsideHome) {
			t.Errorf("%s: %v", p, err)
		}
	}
	if _, err := guardCacheDir(s.home, filepath.Join(s.home, "missing")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("missing: %v", err)
	}
	writeFile(t, filepath.Join(s.home, "file"), 1)
	if _, err := guardCacheDir(s.home, filepath.Join(s.home, "file")); !errors.Is(err, errOutsideHome) {
		t.Errorf("a file: %v", err)
	}
}
