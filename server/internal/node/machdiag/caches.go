package machdiag

import (
	"path/filepath"
	"strings"
)

// cacheSpec is one entry of the cache catalog: where its toolchain keeps it, and which processes use it.
type cacheSpec struct {
	id string
	// path is the cache's folder on goos for a home directory and environment; "" where the system has none.
	path func(goos, home string, getenv func(string) string) string
	// users are the executables that read or write the cache while they run (usedBy matches versioned names too).
	users []string
}

// catalog is every cache the node will size or empty: rebuildable downloads and build outputs that the next build or
// install fetches or makes again. Its ids are proto.CacheIDs. A variable a toolchain itself honours moves the cache
// (GOCACHE, npm_config_cache, …); wherever it points, cleanup only ever empties a folder inside the home directory.
var catalog = []cacheSpec{
	{id: "go-build", users: []string{"go", "gopls", "compile"}, path: func(goos, home string, env func(string) string) string {
		if v := env("GOCACHE"); v != "" {
			return v
		}
		return filepath.Join(userCacheDir(goos, home, env), "go-build")
	}},
	{id: "go-mod", users: []string{"go", "gopls"}, path: func(goos, home string, env func(string) string) string {
		if v := env("GOMODCACHE"); v != "" {
			return v
		}
		if v := env("GOPATH"); v != "" {
			return filepath.Join(filepath.SplitList(v)[0], "pkg", "mod")
		}
		return filepath.Join(home, "go", "pkg", "mod")
	}},
	{id: "npm-cache", users: []string{"npm", "npx", "node"}, path: func(goos, home string, env func(string) string) string {
		if v := env("npm_config_cache"); v != "" {
			return filepath.Join(v, "_cacache")
		}
		return filepath.Join(home, ".npm", "_cacache") // only the downloads: ~/.npm also keeps logs and npx's installs
	}},
	{id: "pip-cache", users: []string{"pip", "python"}, path: func(goos, home string, env func(string) string) string {
		if v := env("PIP_CACHE_DIR"); v != "" {
			return v
		}
		return filepath.Join(userCacheDir(goos, home, env), "pip")
	}},
	{id: "uv-cache", users: []string{"uv", "uvx"}, path: func(goos, home string, env func(string) string) string {
		if v := env("UV_CACHE_DIR"); v != "" {
			return v
		}
		return filepath.Join(xdgCacheDir(home, env), "uv") // uv uses the XDG folder on macOS too
	}},
	{id: "yarn-cache", users: []string{"yarn", "node"}, path: func(goos, home string, env func(string) string) string {
		if v := env("YARN_CACHE_FOLDER"); v != "" {
			return v
		}
		if goos == "darwin" {
			return filepath.Join(home, "Library", "Caches", "Yarn")
		}
		return filepath.Join(xdgCacheDir(home, env), "yarn")
	}},
	{id: "homebrew-cache", users: []string{"brew"}, path: func(goos, home string, env func(string) string) string {
		if v := env("HOMEBREW_CACHE"); v != "" {
			return v
		}
		if goos == "darwin" {
			return filepath.Join(home, "Library", "Caches", "Homebrew")
		}
		return filepath.Join(xdgCacheDir(home, env), "Homebrew")
	}},
	{id: "xcode-derived-data", users: []string{"xcodebuild", "Xcode", "XCBBuildService", "SWBBuildService"}, path: func(goos, home string, _ func(string) string) string {
		if goos != "darwin" {
			return ""
		}
		return filepath.Join(home, "Library", "Developer", "Xcode", "DerivedData")
	}},
	{id: "gradle-cache", users: []string{"gradle", "java"}, path: func(goos, home string, env func(string) string) string {
		if v := env("GRADLE_USER_HOME"); v != "" {
			return filepath.Join(v, "caches")
		}
		return filepath.Join(home, ".gradle", "caches")
	}},
	{id: "cargo-registry", users: []string{"cargo", "rustc", "rust-analyzer"}, path: func(goos, home string, env func(string) string) string {
		if v := env("CARGO_HOME"); v != "" {
			return filepath.Join(v, "registry")
		}
		return filepath.Join(home, ".cargo", "registry")
	}},
}

// userCacheDir is where a toolchain that follows the system's convention keeps caches: ~/Library/Caches on macOS,
// the XDG folder elsewhere.
func userCacheDir(goos, home string, env func(string) string) string {
	if goos == "darwin" {
		return filepath.Join(home, "Library", "Caches")
	}
	return xdgCacheDir(home, env)
}

func xdgCacheDir(home string, env func(string) string) string {
	if v := env("XDG_CACHE_HOME"); filepath.IsAbs(v) {
		return v
	}
	return filepath.Join(home, ".cache")
}

// diskSupported: the systems whose disk the node describes and whose caches it empties.
func diskSupported(goos string) bool { return goos == "linux" || goos == "darwin" }

// cacheEntry is a catalog entry resolved on this computer.
type cacheEntry struct {
	spec cacheSpec
	path string // absolute and clean, inside home
}

// resolveCaches is the catalog on goos for home: the entries this system has whose folder is inside the home
// directory (and not the home directory itself). One a variable moved elsewhere is left out: it is not ours to empty.
func resolveCaches(goos, home string, env func(string) string) []cacheEntry {
	if !diskSupported(goos) || !filepath.IsAbs(home) {
		return nil
	}
	var out []cacheEntry
	for _, spec := range catalog {
		p := spec.path(goos, home, env)
		if p == "" || !filepath.IsAbs(p) {
			continue
		}
		p = filepath.Clean(p)
		if _, ok := insideHome(home, p); !ok {
			continue
		}
		out = append(out, cacheEntry{spec: spec, path: p})
	}
	return out
}

// insideHome is p relative to home, when p is strictly inside it (lexically; cleanup checks the real path again).
func insideHome(home, p string) (string, bool) {
	rel, err := filepath.Rel(filepath.Clean(home), p)
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return "", false
	}
	return rel, true
}

// tilde writes a path inside home as "~/…" (home itself as "~"), which is how paths leave the node.
func tilde(home, p string) string {
	if rel, ok := insideHome(home, p); ok {
		return "~/" + filepath.ToSlash(rel)
	}
	if filepath.Clean(p) == filepath.Clean(home) {
		return "~"
	}
	return p
}

// inUse says whether a process of the cache's toolchain runs. Without a process list (nil) every cache counts as in
// use: nothing says it is not.
func inUse(spec cacheSpec, running map[string]bool) bool {
	if running == nil {
		return true
	}
	for name := range running {
		for _, u := range spec.users {
			if usedBy(name, u) {
				return true
			}
		}
	}
	return false
}

// usedBy matches an executable's name to a toolchain's program, with a version after it ("python3", "python3.12",
// "java17" are python and java) but not another word ("gopher" is not go).
func usedBy(name, program string) bool {
	rest, ok := strings.CutPrefix(name, program)
	if !ok {
		return false
	}
	return strings.Trim(rest, "0123456789.") == ""
}
