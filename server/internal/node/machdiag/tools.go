package machdiag

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/nwverse/persona/server/internal/lazyre"
	"github.com/nwverse/persona/server/internal/proto"
)

const (
	// toolsFresh is how long a probe of the tools is reused: they change when someone installs one, not by the minute.
	toolsFresh = 10 * time.Minute
	// toolTimeout bounds one version command; toolsTimeout the whole probe; toolsAtOnce how many run together.
	toolTimeout  = 2 * time.Second
	toolsTimeout = 8 * time.Second
	toolsAtOnce  = 6
)

// toolSpec is one entry of the tool catalog (its ids are proto.ToolIDs): the commands that are it, the first found on
// the node's PATH, and how to ask its version.
type toolSpec struct {
	id      string
	cmds    []string
	args    []string
	version *lazyre.Regexp // its first group is the version; nil: the first dotted number
	darwin  bool           // only looked for on macOS
}

var dotted = lazyre.New(`(\d+(?:\.\d+)+)`)

var tools = []toolSpec{
	{id: "xcode", cmds: []string{"xcodebuild"}, args: []string{"-version"}, darwin: true},
	{id: "docker", cmds: []string{"docker"}, args: []string{"--version"}}, // the client's version: the daemon is not asked
	{id: "podman", cmds: []string{"podman"}, args: []string{"--version"}},
	{id: "go", cmds: []string{"go"}, args: []string{"version"}},
	{id: "node", cmds: []string{"node"}, args: []string{"--version"}},
	{id: "python", cmds: []string{"python3", "python"}, args: []string{"--version"}},
	{id: "rust", cmds: []string{"rustc"}, args: []string{"--version"}},
	{id: "java", cmds: []string{"java"}, args: []string{"-version"}},
	{id: "swift", cmds: []string{"swift"}, args: []string{"--version"}, version: lazyre.New(`Swift version (\d+(?:\.\d+)+)`)},
	{id: "git", cmds: []string{"git"}, args: []string{"--version"}},
	{id: "gh", cmds: []string{"gh"}, args: []string{"--version"}},
	{id: "brew", cmds: []string{"brew"}, args: []string{"--version"}},
	{id: "bun", cmds: []string{"bun"}, args: []string{"--version"}},
	{id: "pnpm", cmds: []string{"pnpm"}, args: []string{"--version"}},
	{id: "uv", cmds: []string{"uv"}, args: []string{"--version"}},
	{id: "tmux", cmds: []string{"tmux"}, args: []string{"-V"}, version: lazyre.New(`(\d+\.\d+[a-z]?)`)},
	{id: "claude", cmds: []string{"claude"}, args: []string{"--version"}},
	{id: "codex", cmds: []string{"codex"}, args: []string{"--version"}},
	{id: "opencode", cmds: []string{"opencode"}, args: []string{"--version"}},
	{id: "gemini", cmds: []string{"gemini"}, args: []string{"--version"}},
}

// macShims are the programs macOS puts in /usr/bin as stand-ins for the developer tools: run without the tools
// installed, each opens a dialog offering to install them on the user's screen. They count only with a developer
// directory (xcode-select -p). /usr/bin/java is the same for a Java runtime (java_home).
var macShims = map[string]bool{"xcodebuild": true, "git": true, "python3": true, "swift": true}

// toolProber finds the catalog's tools and keeps what it found for toolsFresh.
type toolProber struct {
	goos     string
	lookPath func(string) (string, error)
	// run is a command's combined output; err when it failed or ran out of time.
	run func(ctx context.Context, name string, args ...string) ([]byte, error)
	now func() time.Time

	mu  sync.Mutex
	at  time.Time
	got []proto.DiagnosedTool
}

func newToolProber() *toolProber {
	return &toolProber{goos: runtime.GOOS, lookPath: exec.LookPath, run: runCommand, now: time.Now}
}

// list is the tools found, from the last probe when it is fresh. Two callers at once share one probe.
func (t *toolProber) list(ctx context.Context) []proto.DiagnosedTool {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.got != nil && t.now().Sub(t.at) < toolsFresh {
		return t.got
	}
	t.got, t.at = t.probe(ctx), t.now()
	return t.got
}

func (t *toolProber) probe(ctx context.Context) []proto.DiagnosedTool {
	ctx, cancel := context.WithTimeout(ctx, toolsTimeout)
	defer cancel()
	var (
		devDir  = sync.OnceValue(func() bool { return t.macDeveloperDir(ctx) })
		javaRT  = sync.OnceValue(func() bool { return t.ok(ctx, "/usr/libexec/java_home") })
		found   = make([]*proto.DiagnosedTool, len(tools))
		limiter = make(chan struct{}, toolsAtOnce)
		wg      sync.WaitGroup
	)
	for i, spec := range tools {
		if spec.darwin && t.goos != "darwin" {
			continue
		}
		path := t.find(spec)
		if path == "" {
			continue
		}
		wg.Go(func() {
			limiter <- struct{}{}
			defer func() { <-limiter }()
			if t.goos == "darwin" && filepath.Dir(path) == "/usr/bin" {
				base := filepath.Base(path)
				if macShims[base] && !devDir() || base == "java" && !javaRT() {
					return
				}
			}
			cctx, cancel := context.WithTimeout(ctx, toolTimeout)
			defer cancel()
			out, err := t.run(cctx, path, spec.args...)
			if err != nil && spec.id == "xcode" {
				return // xcodebuild without Xcode (only the command line tools) is no Xcode
			}
			v := ""
			if err == nil {
				v = versionOf(spec, out)
			}
			found[i] = &proto.DiagnosedTool{ID: spec.id, Version: v}
		})
	}
	wg.Wait()
	out := []proto.DiagnosedTool{}
	for _, f := range found {
		if f != nil {
			out = append(out, *f)
		}
	}
	return out
}

func (t *toolProber) find(spec toolSpec) string {
	for _, c := range spec.cmds {
		if p, err := t.lookPath(c); err == nil {
			return p
		}
	}
	return ""
}

// macDeveloperDir: xcode-select names a developer directory that is there.
func (t *toolProber) macDeveloperDir(ctx context.Context) bool {
	cctx, cancel := context.WithTimeout(ctx, toolTimeout)
	defer cancel()
	out, err := t.run(cctx, "/usr/bin/xcode-select", "-p")
	if err != nil {
		return false
	}
	_, err = os.Stat(strings.TrimSpace(string(out)))
	return err == nil
}

func (t *toolProber) ok(ctx context.Context, name string) bool {
	cctx, cancel := context.WithTimeout(ctx, toolTimeout)
	defer cancel()
	_, err := t.run(cctx, name)
	return err == nil
}

// versionOf is the version in a tool's answer to its version command, "" when there is none.
func versionOf(spec toolSpec, out []byte) string {
	re := spec.version
	if re == nil {
		re = dotted
	}
	if m := re.FindStringSubmatch(string(out)); len(m) > 1 {
		return m[1]
	}
	return ""
}

// runCommand runs a version command with no input. WaitDelay: a CLI that leaves a child holding its output open
// (a node-based one) must not hold the probe past its time.
func runCommand(ctx context.Context, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.WaitDelay = 500 * time.Millisecond
	out, err := cmd.CombinedOutput()
	if len(out) > 4<<10 {
		out = out[:4<<10]
	}
	return out, err
}
