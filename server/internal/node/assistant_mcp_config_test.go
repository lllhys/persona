package node

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/proto"
)

// Deliberately contains JSON escapes, whitespace, spaces and non-ASCII path components.
const windowsMCPJSON = "{\n" + `  "mcpServers": {"persona": {"command": "C:\\Program Files\\café\\personad.exe", "args": ["mcp"], "env": {"PERSONA_HOME": "C:\\Users\\café user\\.persona", "EXAMPLE_SECRET": "test-only-credential", "PATTERN": "\\w+\\d"}}}` + "\n}"

// HOME must already be isolated (sdkTest does this for launch tests).
func assistantMCPTestCache(t *testing.T) string {
	t.Helper()
	cache := filepath.Join(t.TempDir(), "cache space café")
	t.Setenv("XDG_CACHE_HOME", cache)
	t.Setenv("LocalAppData", cache)
	base, err := os.UserCacheDir()
	if err != nil {
		t.Fatal(err)
	}
	return filepath.Join(base, "persona", "assistant-mcp")
}

func windowsAssistantMCPTest(t *testing.T) {
	t.Helper()
	original := assistantCLIMCPFlags
	t.Cleanup(func() { assistantCLIMCPFlags = original })
	assistantCLIMCPFlags = func(flags []string) ([]string, func(), error) {
		return assistantCLIMCPFlagsForOS("windows", flags)
	}
}

func TestAssistantCLIMCPFlagsForOS(t *testing.T) {
	for _, goos := range []string{"darwin", "linux", "windows"} {
		t.Run(goos, func(t *testing.T) {
			t.Setenv("HOME", filepath.Join(t.TempDir(), "home space café"))
			dir := assistantMCPTestCache(t)
			flags := []string{"-p", "--mcp-config", windowsMCPJSON, "--strict-mcp-config", "--tools", "", "--allowedTools", "mcp__persona", "--disallowedTools", "Read(path with spaces)"}
			before := slices.Clone(flags)
			got, cleanup, err := assistantCLIMCPFlagsForOS(goos, flags)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(cleanup)
			if !slices.Equal(flags, before) {
				t.Fatal("mutated the original flags")
			}
			if goos != "windows" {
				if !slices.Equal(got, flags) {
					t.Fatal("changed inline flags on a non-Windows host")
				}
				cleanup()
				if _, err := os.Stat(dir); !os.IsNotExist(err) {
					t.Fatalf("inline config created a cache directory: %v", err)
				}
				return
			}
			path := mcpFlag(t, got, "--mcp-config")
			if !filepath.IsAbs(path) || strings.Contains(path, `\`) || !strings.Contains(path, "space café") {
				t.Fatalf("not an absolute slash path preserving spaces and Unicode: %q", path)
			}
			if filepath.Clean(filepath.Dir(path)) != filepath.Clean(dir) {
				t.Fatalf("config outside the private cache: %q", path)
			}
			want := slices.Clone(flags)
			want[2] = path
			if !slices.Equal(got, want) {
				t.Fatal("changed flags other than the MCP config value")
			}
			raw, err := os.ReadFile(path)
			if err != nil || string(raw) != windowsMCPJSON || !json.Valid(raw) {
				t.Fatalf("config bytes changed or could not be read: %v", err)
			}
			if runtime.GOOS != "windows" { // Windows privacy comes from LocalAppData's ACL, not Unix mode bits.
				for p, mode := range map[string]os.FileMode{path: 0o600, dir: 0o700} {
					st, err := os.Stat(p)
					if err != nil {
						t.Fatal(err)
					}
					if st.Mode().Perm() != mode {
						t.Fatalf("mode %v, want %v", st.Mode().Perm(), mode)
					}
				}
			}
			cleanup()
			cleanup() // Safe after an earlier failure already removed the file.
			if _, err := os.Stat(path); !os.IsNotExist(err) {
				t.Fatalf("config survived cleanup: %v", err)
			}
		})
	}
}

func TestAssistantCLIMCPFlagsConcurrent(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dir := assistantMCPTestCache(t)
	type result struct {
		flags   []string
		cleanup func()
		err     error
		json    string
	}
	const count = 16
	results := make(chan result, count)
	for i := range count {
		go func() {
			// Overlapping starts can carry different credentials for the same conversation.
			config := strings.Replace(windowsMCPJSON, "test-only-credential", "test-only-"+strconv.Itoa(i), 1)
			flags, cleanup, err := assistantCLIMCPFlagsForOS("windows", []string{"--mcp-config", config})
			results <- result{flags, cleanup, err, config}
		}()
	}
	seen := map[string]bool{}
	var cleanups []func()
	for range count {
		r := <-results
		t.Cleanup(r.cleanup)
		if r.err != nil {
			t.Fatal(r.err)
		}
		path := mcpFlag(t, r.flags, "--mcp-config")
		if seen[path] {
			t.Fatalf("concurrent starts shared %q", path)
		}
		seen[path] = true
		raw, err := os.ReadFile(path)
		if err != nil || string(raw) != r.json {
			t.Fatalf("concurrent config changed: %v", err)
		}
		cleanups = append(cleanups, r.cleanup)
	}
	for _, cleanup := range cleanups {
		cleanup()
	}
	if entries, err := os.ReadDir(dir); err != nil || len(entries) != 0 {
		t.Fatalf("configs left after cleanup: %d (%v)", len(entries), err)
	}
}

func TestAssistantCLIMCPFlagsRefusesUnavailableCache(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dir := assistantMCPTestCache(t)
	if err := os.MkdirAll(filepath.Dir(dir), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dir, []byte("not a directory"), 0o600); err != nil {
		t.Fatal(err)
	}
	flags, cleanup, err := assistantCLIMCPFlagsForOS("windows", []string{"--mcp-config", windowsMCPJSON})
	cleanup()
	if err == nil || flags != nil {
		t.Fatal("unavailable private cache did not fail closed")
	}
	if strings.Contains(err.Error(), "test-only-credential") || strings.Contains(err.Error(), windowsMCPJSON) {
		t.Fatal("config leaked through an error")
	}
	entries, err := os.ReadDir(filepath.Dir(dir))
	if err != nil || len(entries) != 1 {
		t.Fatalf("failed preparation left temporary files: %d (%v)", len(entries), err)
	}
}

func TestAssistantMCPFileStartFailure(t *testing.T) {
	f := sdkTest(t)
	assistantMCPTestCache(t)
	windowsAssistantMCPTest(t)
	cwd := t.TempDir()
	var path string
	assistantCLIMCPFlags = func(flags []string) ([]string, func(), error) {
		args, cleanup, err := assistantCLIMCPFlagsForOS("windows", flags)
		if err == nil {
			path = mcpFlag(t, args, "--mcp-config")
			// The file exists, but cmd.Start must fail: its cwd vanished after preparation.
			if removeErr := os.RemoveAll(cwd); removeErr != nil {
				t.Fatal(removeErr)
			}
		}
		return args, cleanup, err
	}
	p, err := f.n.spawnClaude("11111111-2222-3333-4444-555555555555", cwd, "claude", false, false)
	if p != nil {
		p.stop()
	}
	if err == nil || !strings.Contains(err.Error(), "start claude:") || path == "" {
		t.Fatalf("did not reach the process start failure: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("start failure left the MCP file: %v", err)
	}
}

func TestAssistantMCPFileProcessExit(t *testing.T) {
	for _, end := range []string{"stdin-close", "stop", "launcher-failure", "sdk-fallback"} {
		t.Run(end, func(t *testing.T) {
			f := sdkTest(t)
			assistantMCPTestCache(t)
			windowsAssistantMCPTest(t)
			launcher := "claude"
			execution := modelExecution{}
			if end == "launcher-failure" {
				launcher = "false"
			}
			if end == "sdk-fallback" {
				execution = sdkExec(proto.AssistantRuntimeAgentSDK)
				sdkClaude = func() (string, error) { return "", os.ErrNotExist }
			}
			p, err := f.n.spawnClaude("11111111-2222-3333-4444-555555555555", t.TempDir(), launcher, false, false, execution)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(p.stop)
			path := mcpFlag(t, p.cmd.Args, "--mcp-config")
			if !filepath.IsAbs(path) {
				t.Fatal("CLI did not receive a config file")
			}
			if end != "launcher-failure" {
				select {
				case <-p.ready:
				case <-p.exited:
					t.Fatal("fake CLI exited before initialization")
				case <-time.After(5 * time.Second):
					t.Fatal("fake CLI did not initialize")
				}
				if _, err := os.Stat(path); err != nil {
					t.Fatalf("config removed before process exit: %v", err)
				}
				if end == "stop" {
					p.stop()
				} else {
					_ = p.stdin.Close()
				}
			}
			select {
			case <-p.exited:
			case <-time.After(5 * time.Second):
				t.Fatal("fake CLI did not exit")
			}
			if _, err := os.Stat(path); !os.IsNotExist(err) {
				t.Fatalf("process exit left the MCP file: %v", err)
			}
		})
	}
}
