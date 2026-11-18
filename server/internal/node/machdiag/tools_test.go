package machdiag

import (
	"context"
	"errors"
	"path"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/proto"
)

// fakeTools is a computer with some commands installed: each answers its version command with the text given, or
// fails when the text is "!".
type fakeTools struct {
	mu        sync.Mutex
	installed map[string]string // full path → answer
	ran       []string
}

func (f *fakeTools) prober(goos string, now *time.Time) *toolProber {
	return &toolProber{
		goos: goos,
		lookPath: func(name string) (string, error) {
			for p := range f.installed {
				if path.Base(p) == name {
					return p, nil
				}
			}
			return "", errors.New("not found")
		},
		run: func(_ context.Context, name string, _ ...string) ([]byte, error) {
			f.mu.Lock()
			defer f.mu.Unlock()
			f.ran = append(f.ran, name)
			out, ok := f.installed[name]
			if !ok || out == "!" {
				return []byte("error"), errors.New("exit status 1")
			}
			return []byte(out), nil
		},
		now: func() time.Time { return *now },
	}
}

func toolMap(list []proto.DiagnosedTool) map[string]string {
	out := map[string]string{}
	for _, t := range list {
		out[t.ID] = t.Version
	}
	return out
}

func TestToolsVersions(t *testing.T) {
	f := &fakeTools{installed: map[string]string{
		"/usr/local/go/bin/go":    "go version go1.27.1 linux/amd64\n",
		"/usr/bin/docker":         "Docker version 27.3.1, build ce12230\n",
		"/usr/bin/python3":        "Python 3.12.3\n",
		"/usr/bin/java":           "openjdk version \"21.0.2\" 2024-01-16\n",
		"/usr/bin/swift":          "Swift version 6.0.2 (swift-6.0.2-RELEASE)\nTarget: x86_64-unknown-linux-gnu\n",
		"/usr/bin/tmux":           "tmux 3.3a\n",
		"/home/x/.local/bin/uv":   "uv 0.4.20\n",
		"/home/x/.local/bin/gh":   "!", // found, but its version could not be read
		"/usr/bin/xcodebuild":     "Xcode 16.1\n",
		"/home/x/.local/bin/node": "v22.11.0\n",
	}}
	now := time.Unix(1794000000, 0)
	got := toolMap(f.prober("linux", &now).list(context.Background()))
	want := map[string]string{"go": "1.27.1", "docker": "27.3.1", "python": "3.12.3", "java": "21.0.2", "swift": "6.0.2",
		"tmux": "3.3a", "uv": "0.4.20", "gh": "", "node": "22.11.0"}
	if len(got) != len(want) {
		t.Fatalf("got %v", got)
	}
	for id, v := range want {
		if got[id] != v {
			t.Errorf("%s = %q, want %q", id, got[id], v)
		}
	}
	if _, ok := got["xcode"]; ok {
		t.Error("xcode is looked for on macOS only")
	}
}

// The tools are probed once per toolsFresh; the list is in the catalog's order.
func TestToolsAreCached(t *testing.T) {
	f := &fakeTools{installed: map[string]string{"/usr/bin/git": "git version 2.43.0", "/usr/bin/tmux": "tmux 3.4"}}
	now := time.Unix(1794000000, 0)
	p := f.prober("linux", &now)
	first := p.list(context.Background())
	if ids := []string{first[0].ID, first[1].ID}; !slices.Equal(ids, []string{"git", "tmux"}) {
		t.Fatalf("order: %v", ids)
	}
	runs := len(f.ran)
	now = now.Add(toolsFresh - time.Second)
	p.list(context.Background())
	if len(f.ran) != runs {
		t.Fatal("probed again while fresh")
	}
	now = now.Add(2 * time.Second)
	p.list(context.Background())
	if len(f.ran) != 2*runs {
		t.Fatalf("not probed again after %s: %d runs", toolsFresh, len(f.ran))
	}
	if empty := (&fakeTools{}).prober("linux", &now).list(context.Background()); empty == nil || len(empty) != 0 {
		t.Fatalf("nothing installed: %#v", empty)
	}
}

// macOS's stand-ins in /usr/bin would open an install dialog: without the developer tools they are not run, and
// xcodebuild that fails (command line tools only) is no Xcode.
func TestMacStandInsAreNotRunWithoutDeveloperTools(t *testing.T) {
	f := &fakeTools{installed: map[string]string{
		"/usr/bin/git":                "git version 2.39.5 (Apple Git-154)",
		"/usr/bin/java":               "never run",
		"/usr/bin/xcodebuild":         "never run",
		"/usr/bin/xcode-select":       "!",
		"/usr/libexec/java_home":      "!",
		"/opt/homebrew/bin/brew":      "Homebrew 4.4.2",
		"/opt/homebrew/bin/python3":   "Python 3.13.0",
		"/Users/x/.local/bin/claude":  "2.0.14 (Claude Code)",
		"/opt/homebrew/bin/opencode":  "0.15.3",
		"/opt/homebrew/bin/codex":     "codex-cli 0.46.0",
		"/opt/homebrew/bin/gemini":    "0.9.0",
		"/opt/homebrew/bin/bun":       "1.1.30",
		"/opt/homebrew/bin/pnpm":      "9.12.1",
		"/opt/homebrew/bin/podman":    "podman version 5.2.5",
		"/opt/homebrew/bin/rustc":     "rustc 1.82.0 (f6e511eec 2024-10-15)",
		"/opt/homebrew/bin/something": "not in the catalog",
	}}
	now := time.Unix(1794000000, 0)
	got := toolMap(f.prober("darwin", &now).list(context.Background()))
	for _, id := range []string{"git", "java", "xcode"} {
		if _, ok := got[id]; ok {
			t.Errorf("%s detected without the developer tools", id)
		}
	}
	for _, name := range f.ran {
		if strings.HasPrefix(name, "/usr/bin/") && name != "/usr/bin/xcode-select" {
			t.Errorf("ran %s", name)
		}
	}
	if got["python"] != "3.13.0" || got["brew"] != "4.4.2" || got["claude"] != "2.0.14" || got["codex"] != "0.46.0" || got["rust"] != "1.82.0" {
		t.Errorf("got %v", got)
	}
}

func TestXcodeWithDeveloperTools(t *testing.T) {
	dev := t.TempDir()
	f := &fakeTools{installed: map[string]string{
		"/usr/bin/xcodebuild":   "Xcode 16.1\nBuild version 16B40\n",
		"/usr/bin/xcode-select": dev + "\n",
		"/usr/bin/git":          "git version 2.39.5 (Apple Git-154)",
	}}
	now := time.Unix(1794000000, 0)
	got := toolMap(f.prober("darwin", &now).list(context.Background()))
	if got["xcode"] != "16.1" || got["git"] != "2.39.5" {
		t.Fatalf("got %v", got)
	}
}
