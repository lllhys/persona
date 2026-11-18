package claudeplugin

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/proto"
	"github.com/nwverse/persona/server/internal/runtimepkg"
)

// fakeClaude is Claude Code's plugin CLI as measured (2.1.291, docs/lessons/agents.md "Claude Code plugins from a
// folder marketplace"): per config directory, the marketplaces declared and the plugins installed at the user scope.
type fakeClaude struct {
	mu      sync.Mutex
	dirs    map[string]*fakeDir
	calls   []string
	failing map[string]string // a subcommand ("install", "update", …) that fails, with its message
}

type fakeDir struct {
	markets map[string]string // name -> path
	plugins map[string]*fakePlugin
}

type fakePlugin struct {
	version string
	enabled bool
}

func newFake() *fakeClaude {
	return &fakeClaude{dirs: map[string]*fakeDir{}, failing: map[string]string{}}
}

func (f *fakeClaude) dir(d string) *fakeDir {
	if f.dirs[d] == nil {
		f.dirs[d] = &fakeDir{markets: map[string]string{}, plugins: map[string]*fakePlugin{}}
	}
	return f.dirs[d]
}

// folderVersion is the version the plugin's folder says (its marketplace lists it from "./").
func folderVersion(market, _ string) string {
	b, err := os.ReadFile(filepath.Join(market, ".claude-plugin", "plugin.json"))
	if err != nil {
		return ""
	}
	var m struct{ Version string }
	_ = json.Unmarshal(b, &m)
	return m.Version
}

func (f *fakeClaude) run(_ context.Context, exe, dir string, args ...string) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, strings.Join(args, " "))
	if len(args) == 1 && args[0] == "--version" {
		return []byte("2.1.300 (Claude Code)\n"), nil
	}
	d := f.dir(dir)
	res := func(outcome, code, msg string) ([]byte, error) {
		b, _ := json.Marshal(map[string]string{"outcome": outcome, "failureCode": code, "message": msg})
		if outcome == "ok" {
			return b, nil
		}
		return b, fmt.Errorf("exit status 1")
	}
	if len(args) < 2 || args[0] != "plugin" {
		return nil, fmt.Errorf("unexpected %v", args)
	}
	sub := args[1]
	if msg, ok := f.failing[sub]; ok {
		return res("failed", "boom", msg)
	}
	switch sub {
	case "list":
		var out []map[string]any
		for id, p := range d.plugins {
			out = append(out, map[string]any{"id": id, "version": p.version, "scope": "user", "enabled": p.enabled})
		}
		b, _ := json.Marshal(out)
		return b, nil
	case "marketplace":
		switch args[2] {
		case "add":
			path := args[3]
			var m struct{ Name string }
			b, err := os.ReadFile(filepath.Join(path, ".claude-plugin", "marketplace.json"))
			if err != nil || json.Unmarshal(b, &m) != nil {
				return res("failed", "not_found", "Marketplace file not found")
			}
			d.markets[m.Name] = path
			return res("ok", "", "added")
		case "remove":
			if _, ok := d.markets[args[3]]; !ok {
				return res("failed", "not_configured", "not found")
			}
			delete(d.markets, args[3])
			return res("ok", "", "removed")
		}
	case "install", "update", "enable", "disable", "uninstall":
		id := args[2]
		name, market, _ := strings.Cut(id, "@")
		p := d.plugins[id]
		switch sub {
		case "install":
			path, ok := d.markets[market]
			if !ok {
				return res("failed", "not_found", "marketplace not found")
			}
			d.plugins[id] = &fakePlugin{version: folderVersion(path, name), enabled: true} // install enables, even a disabled one
			return res("ok", "", "installed")
		case "update":
			if p == nil {
				return res("failed", "not_found", "not found")
			}
			p.version = folderVersion(d.markets[market], name) // keeps it disabled
			return res("ok", "", "updated")
		case "enable", "disable":
			if p == nil {
				return res("failed", "not_found", "not found")
			}
			if p.enabled == (sub == "enable") {
				return res("failed", "already_in_goal_state", "already")
			}
			p.enabled = sub == "enable"
			return res("ok", "", sub+"d")
		case "uninstall":
			if p == nil {
				return res("failed", "not_installed", "not installed")
			}
			delete(d.plugins, id)
			return res("ok", "", "uninstalled")
		}
	}
	return nil, fmt.Errorf("unexpected %v", args)
}

func (f *fakeClaude) plugin(dir, id string) *fakePlugin {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.dir(dir).plugins[id]
}

func (f *fakeClaude) takeCalls() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	c := f.calls
	f.calls = nil
	return c
}

func bundle(t *testing.T, version string) *Bundle {
	t.Helper()
	b, err := NewBundle([]runtimepkg.File{
		{Path: ".claude-plugin/plugin.json", Data: []byte(`{"name":"persona","version":"` + version + `","description":"fixture"}`)},
		{Path: ".claude-plugin/marketplace.json", Data: []byte(`{"name":"persona","owner":{"name":"Persona"},"plugins":[{"name":"persona","source":"./"}]}`)},
		{Path: "hooks/hooks.json", Data: []byte(`{"modules":["./register.ts"]}`)},
		{Path: "hooks/register.ts", Data: []byte("export const register = () => {} // " + version)},
	})
	if err != nil {
		t.Fatal(err)
	}
	return b
}

type fixture struct {
	m    *Manager
	f    *fakeClaude
	dir  string // the default config directory
	data string
}

func newFixture(t *testing.T, version string) *fixture {
	t.Helper()
	root := t.TempDir()
	fx := &fixture{f: newFake(), dir: filepath.Join(root, "home", ".claude"), data: filepath.Join(root, "home", ".persona")}
	if err := os.MkdirAll(fx.dir, 0o755); err != nil {
		t.Fatal(err)
	}
	fx.m = &Manager{DataDir: fx.data, Bundle: bundle(t, version), Dirs: []string{fx.dir}, Run: fx.f.run, DefaultAuto: true,
		FindClaude: func(context.Context) (Claude, bool) { return Claude{Exe: "claude", Version: "2.1.300"}, true }}
	return fx
}

func (fx *fixture) do(t *testing.T, op Op) proto.ClaudePlugin {
	t.Helper()
	st, err := fx.m.Do(context.Background(), op)
	if err != nil {
		t.Fatalf("%s: %v", op, err)
	}
	return st
}

const id = "persona@persona"

func TestCheckInstallsWhereNotInstalled(t *testing.T) {
	fx := newFixture(t, "0.1.0")
	fx.f.dir(fx.dir).plugins["other@elsewhere"] = &fakePlugin{version: "9", enabled: true}
	st := fx.do(t, OpCheck)
	if st.State != proto.ClaudePluginInstalled || st.Version != "0.1.0" || !st.Changed || !st.Auto || st.ClaudeVersion != "2.1.300" {
		t.Fatalf("status %+v", st)
	}
	if p := fx.f.plugin(fx.dir, id); p == nil || !p.enabled || p.version != "0.1.0" {
		t.Fatalf("installed %+v", p)
	}
	if b, err := os.ReadFile(filepath.Join(fx.data, MarketDir, "hooks", "register.ts")); err != nil || !strings.Contains(string(b), "0.1.0") {
		t.Fatalf("marketplace folder: %q %v", b, err)
	}
	// Only this plugin and its marketplace are named; the other plugin is as it was.
	for _, c := range fx.f.takeCalls() {
		if strings.Contains(c, "other") {
			t.Fatalf("touched another plugin: %s", c)
		}
	}
	if p := fx.f.plugin(fx.dir, "other@elsewhere"); p == nil || p.version != "9" || !p.enabled {
		t.Fatalf("other plugin changed: %+v", p)
	}
	// Idempotent: a second check lists and does nothing else.
	st = fx.do(t, OpCheck)
	if calls := fx.f.takeCalls(); !slices.Equal(calls, []string{"plugin list --json"}) || st.Changed || st.State != proto.ClaudePluginInstalled {
		t.Fatalf("second check: %v %+v", calls, st)
	}
	// The status file says the same.
	if saved, err := ReadStatus(fx.data); err != nil || saved.State != proto.ClaudePluginInstalled {
		t.Fatalf("status file %+v %v", saved, err)
	}
}

func TestCheckUpdatesAnOutdatedPlugin(t *testing.T) {
	fx := newFixture(t, "0.1.0")
	fx.do(t, OpCheck)
	fx.m.Bundle = bundle(t, "0.2.0") // the node was updated
	if st := fx.do(t, OpStatus); st.State != proto.ClaudePluginOutdated || st.Bundled != "0.2.0" || st.Version != "0.1.0" {
		t.Fatalf("status before the check %+v", st)
	}
	fx.f.takeCalls()
	st := fx.do(t, OpCheck)
	if st.State != proto.ClaudePluginInstalled || st.Version != "0.2.0" || !st.Changed {
		t.Fatalf("after %+v", st)
	}
	calls := fx.f.takeCalls()
	if !slices.Contains(calls, "plugin update "+id+" --scope user --json") || slices.ContainsFunc(calls, func(c string) bool { return strings.HasPrefix(c, "plugin install") }) {
		t.Fatalf("calls %v", calls)
	}
	if b, _ := os.ReadFile(filepath.Join(fx.data, MarketDir, "hooks", "register.ts")); !strings.Contains(string(b), "0.2.0") {
		t.Fatalf("folder not updated: %s", b)
	}
}

func TestCheckRespectsAPluginTurnedOffInClaude(t *testing.T) {
	fx := newFixture(t, "0.1.0")
	fx.do(t, OpCheck)
	fx.f.dir(fx.dir).plugins[id].enabled = false // the user turned it off
	fx.m.Bundle = bundle(t, "0.2.0")
	fx.f.takeCalls()
	st := fx.do(t, OpCheck)
	if st.State != proto.ClaudePluginDisabled || st.ConfigDirs[0].OptedOut != proto.ClaudePluginOptedOutDisabled || st.Changed {
		t.Fatalf("status %+v", st)
	}
	if calls := fx.f.takeCalls(); !slices.Equal(calls, []string{"plugin list --json"}) {
		t.Fatalf("a disabled plugin was touched: %v", calls)
	}
	if p := fx.f.plugin(fx.dir, id); p.enabled || p.version != "0.1.0" {
		t.Fatalf("plugin %+v", p)
	}
	// Turned on again by the user: updated from then on.
	fx.f.dir(fx.dir).plugins[id].enabled = true
	if st = fx.do(t, OpCheck); st.State != proto.ClaudePluginInstalled || st.Version != "0.2.0" || st.ConfigDirs[0].OptedOut != "" {
		t.Fatalf("re-enabled %+v", st)
	}
}

func TestCheckDoesNotReinstallAPluginTheUserRemoved(t *testing.T) {
	fx := newFixture(t, "0.1.0")
	fx.do(t, OpCheck)
	delete(fx.f.dir(fx.dir).plugins, id) // `claude plugin uninstall` or + › Plugins
	for range 2 {
		st := fx.do(t, OpCheck)
		if st.State != proto.ClaudePluginNotInstalled || st.ConfigDirs[0].OptedOut != proto.ClaudePluginOptedOutUninstalled {
			t.Fatalf("status %+v", st)
		}
		if fx.f.plugin(fx.dir, id) != nil {
			t.Fatal("put back")
		}
	}
	// `persona-link plugin on` forgets the opt-out.
	if st := fx.do(t, OpOn); st.State != proto.ClaudePluginInstalled || !st.Changed {
		t.Fatalf("on %+v", st)
	}
}

func TestNothingWithoutClaude(t *testing.T) {
	fx := newFixture(t, "0.1.0")
	fx.m.FindClaude = func(context.Context) (Claude, bool) { return Claude{}, false }
	st := fx.do(t, OpCheck)
	if st.State != proto.ClaudePluginClaudeMissing || len(fx.f.takeCalls()) != 0 {
		t.Fatalf("status %+v", st)
	}
	if _, err := os.Stat(filepath.Join(fx.data, MarketDir)); !os.IsNotExist(err) {
		t.Fatalf("marketplace written without Claude: %v", err)
	}
	if _, err := fx.m.Do(context.Background(), OpInstall); err == nil {
		t.Fatal("an install without Claude succeeded")
	}
	// Uninstalling without Claude still takes the folder away (written earlier, when Claude Code was there).
	_ = os.MkdirAll(filepath.Join(fx.data, MarketDir, "hooks"), 0o755)
	if st := fx.do(t, OpUninstall); st.Auto || st.State != proto.ClaudePluginClaudeMissing {
		t.Fatalf("uninstall without Claude: %+v", st)
	}
	if _, err := os.Stat(filepath.Join(fx.data, MarketDir)); !os.IsNotExist(err) {
		t.Fatalf("folder left: %v", err)
	}
	// A CLI with no config directory yet (Claude Code never ran): nothing is created.
	fx = newFixture(t, "0.1.0")
	fx.m.Dirs = []string{filepath.Join(filepath.Dir(fx.dir), "missing")}
	if st = fx.do(t, OpCheck); st.State != proto.ClaudePluginClaudeMissing || len(fx.f.takeCalls()) != 0 {
		t.Fatalf("no config dir: %+v", st)
	}
	if _, err := os.Stat(fx.m.Dirs[0]); !os.IsNotExist(err) {
		t.Fatal("config directory created")
	}
}

func TestOffStopsInstallsAndUpdates(t *testing.T) {
	fx := newFixture(t, "0.1.0")
	if st := fx.do(t, OpOff); st.Auto || st.State != proto.ClaudePluginNotInstalled {
		t.Fatalf("off %+v", st)
	}
	if st := fx.do(t, OpCheck); st.State != proto.ClaudePluginNotInstalled || fx.f.plugin(fx.dir, id) != nil {
		t.Fatalf("installed while off: %+v", st)
	}
	fx.do(t, OpOn)
	fx.do(t, OpOff)
	fx.m.Bundle = bundle(t, "0.2.0")
	if st := fx.do(t, OpCheck); st.State != proto.ClaudePluginOutdated || fx.f.plugin(fx.dir, id).version != "0.1.0" {
		t.Fatalf("updated while off: %+v", st)
	}
	// The choice is the computer's, kept in claude-plugin.json.
	if ch, err := readChoices(fx.data); err != nil || ch.auto(true) {
		t.Fatalf("choices %+v %v", ch, err)
	}
}

func TestUninstallRemovesEverythingAndStaysOff(t *testing.T) {
	fx := newFixture(t, "0.1.0")
	fx.do(t, OpCheck)
	st := fx.do(t, OpUninstall)
	if st.Auto || st.State != proto.ClaudePluginNotInstalled || !st.Changed {
		t.Fatalf("uninstall %+v", st)
	}
	if fx.f.plugin(fx.dir, id) != nil || len(fx.f.dir(fx.dir).markets) != 0 {
		t.Fatalf("left behind: %+v", fx.f.dir(fx.dir))
	}
	if _, err := os.Stat(filepath.Join(fx.data, MarketDir)); !os.IsNotExist(err) {
		t.Fatalf("marketplace folder left: %v", err)
	}
	if st = fx.do(t, OpCheck); st.State != proto.ClaudePluginNotInstalled || fx.f.plugin(fx.dir, id) != nil {
		t.Fatalf("came back: %+v", st)
	}
	// Uninstalling again is fine.
	fx.do(t, OpUninstall)
}

func TestInstallTurnsAPluginOffInClaudeBackOn(t *testing.T) {
	fx := newFixture(t, "0.1.0")
	fx.do(t, OpCheck)
	fx.f.dir(fx.dir).plugins[id].enabled = false
	fx.do(t, OpCheck)
	fx.m.Bundle = bundle(t, "0.2.0")
	st := fx.do(t, OpInstall)
	if p := fx.f.plugin(fx.dir, id); !p.enabled || p.version != "0.2.0" || st.State != proto.ClaudePluginInstalled || st.ConfigDirs[0].OptedOut != "" {
		t.Fatalf("install %+v %+v", p, st)
	}
}

func TestEveryConfigDirectory(t *testing.T) {
	fx := newFixture(t, "0.1.0")
	other := filepath.Join(filepath.Dir(filepath.Dir(fx.dir)), "profile")
	_ = os.MkdirAll(other, 0o755)
	fx.m.Dirs = append(fx.m.Dirs, other, fx.dir) // the default listed twice counts once
	st := fx.do(t, OpCheck)
	if len(st.ConfigDirs) != 2 || st.State != proto.ClaudePluginInstalled || fx.f.plugin(other, id) == nil {
		t.Fatalf("status %+v", st)
	}
	// One directory off in Claude: the computer reads disabled, the other stays installed.
	fx.f.dir(other).plugins[id].enabled = false
	if st = fx.do(t, OpCheck); st.State != proto.ClaudePluginDisabled || st.ConfigDirs[0].State != proto.ClaudePluginInstalled {
		t.Fatalf("status %+v", st)
	}
}

func TestFailureIsAnErrorStatus(t *testing.T) {
	fx := newFixture(t, "0.1.0")
	fx.f.failing["install"] = "a policy blocks this marketplace"
	st, err := fx.m.Do(context.Background(), OpCheck)
	if err == nil || st.State != proto.ClaudePluginError || !strings.Contains(st.Error, "a policy blocks") {
		t.Fatalf("status %+v %v", st, err)
	}
	delete(fx.f.failing, "install")
	if st = fx.do(t, OpCheck); st.State != proto.ClaudePluginInstalled {
		t.Fatalf("retry %+v", st)
	}
}

func TestNoBundle(t *testing.T) {
	fx := newFixture(t, "0.1.0")
	fx.do(t, OpCheck)
	fx.m.Bundle = nil
	if st := fx.do(t, OpCheck); st.State != proto.ClaudePluginUnavailable || st.Bundled != "" {
		t.Fatalf("status %+v", st)
	}
	// A build without the plugin still removes it.
	if st := fx.do(t, OpUninstall); st.State != proto.ClaudePluginNotInstalled || fx.f.plugin(fx.dir, id) != nil {
		t.Fatalf("uninstall %+v", st)
	}
}

func TestCommandEnv(t *testing.T) {
	user := t.TempDir()
	base := []string{"PATH=/bin", "CLAUDE_CONFIG_DIR=/elsewhere", "CLAUDECODE=1"}
	env := commandEnv(base, user, user)
	if slices.ContainsFunc(env, func(kv string) bool {
		return strings.HasPrefix(kv, "CLAUDE_CONFIG_DIR=") || strings.HasPrefix(kv, "CLAUDECODE=")
	}) ||
		!slices.Contains(env, "CLAUDE_CODE_DISABLE_OFFICIAL_MARKETPLACE_AUTOINSTALL=1") || !slices.Contains(env, "PATH=/bin") {
		t.Fatalf("user's directory: %v", env)
	}
	other := filepath.Join(user, "profile")
	if env = commandEnv(base, user, other); !slices.Contains(env, "CLAUDE_CONFIG_DIR="+other) {
		t.Fatalf("another directory: %v", env)
	}
	if env = commandEnv(base, "", user); !slices.Contains(env, "CLAUDE_CONFIG_DIR="+user) {
		t.Fatalf("no user home: %v", env)
	}
}

func TestSyncDirKeepsOnlyTheBundle(t *testing.T) {
	root := t.TempDir()
	files := map[string][]byte{"a/b.txt": []byte("1"), "c.txt": []byte("2")}
	if changed, err := syncDir(root, files); err != nil || !changed {
		t.Fatal(changed, err)
	}
	if changed, err := syncDir(root, files); err != nil || changed {
		t.Fatal("unchanged files written again", err)
	}
	_ = os.WriteFile(filepath.Join(root, "stale.txt"), []byte("x"), 0o644)
	_ = os.MkdirAll(filepath.Join(root, "old", "deep"), 0o755)
	_ = os.WriteFile(filepath.Join(root, "old", "deep", "x"), []byte("x"), 0o644)
	if changed, err := syncDir(root, files); err != nil || !changed {
		t.Fatal(changed, err)
	}
	for _, gone := range []string{"stale.txt", "old"} {
		if _, err := os.Stat(filepath.Join(root, gone)); !os.IsNotExist(err) {
			t.Fatalf("%s left", gone)
		}
	}
}

func TestBundleNeedsAManifest(t *testing.T) {
	if _, err := NewBundle([]runtimepkg.File{{Path: "hooks/hooks.json", Data: []byte("{}")}}); err == nil {
		t.Fatal("no manifest accepted")
	}
	if _, err := NewBundle([]runtimepkg.File{{Path: ".claude-plugin/plugin.json", Data: []byte(`{"name":"../x","version":"1"}`)}}); err == nil {
		t.Fatal("bad name accepted")
	}
	b := bundle(t, "1.2.3")
	if b.Market != "persona" || b.ID() != "persona@persona" {
		t.Fatalf("bundle %s %s", b.Market, b.ID())
	}
	if _, ok := b.marketFiles()[".claude-plugin/marketplace.json"]; !ok {
		t.Fatal("no marketplace file in the folder")
	}
	// A marketplace that does not list the plugin from its own folder is refused.
	if _, err := NewBundle([]runtimepkg.File{{Path: ".claude-plugin/plugin.json", Data: []byte(`{"name":"persona","version":"1"}`)},
		{Path: ".claude-plugin/marketplace.json", Data: []byte(`{"name":"persona","plugins":[{"name":"persona","source":"github:x/y"}]}`)}}); err == nil {
		t.Fatal("a marketplace elsewhere accepted")
	}
	// A plugin folder without one gets one listing it from "./".
	nb, err := NewBundle([]runtimepkg.File{{Path: ".claude-plugin/plugin.json", Data: []byte(`{"name":"persona","version":"1"}`)}})
	if err != nil || nb.ID() != "persona@persona" {
		t.Fatalf("generated marketplace: %v", err)
	}
	var m struct {
		Name    string
		Plugins []struct{ Name, Source string }
	}
	if err := json.Unmarshal(nb.marketFiles()[".claude-plugin/marketplace.json"], &m); err != nil || m.Name != "persona" ||
		len(m.Plugins) != 1 || m.Plugins[0].Source != "./" {
		t.Fatalf("marketplace %+v %v", m, err)
	}
	// A build without the plugin still knows what to remove.
	var none *Bundle
	if none.ID() != "persona@persona" || none.market() != "persona" {
		t.Fatal("nil bundle ids")
	}
}

func TestKeeperChecksAndPassesOnCommands(t *testing.T) {
	fx := newFixture(t, "0.1.0")
	k := NewKeeper(fx.m)
	k.First, k.Every, k.Poll = time.Millisecond, time.Hour, 5*time.Millisecond
	got := make(chan proto.ClaudePlugin, 8)
	stop := k.Subscribe(func(st proto.ClaudePlugin) { got <- st })
	defer stop()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go k.Run(ctx)
	wait := func(state string) proto.ClaudePlugin {
		t.Helper()
		for {
			select {
			case st := <-got:
				if st.State == state {
					return st
				}
			case <-time.After(10 * time.Second):
				t.Fatalf("no %s status", state)
			}
		}
	}
	if st := wait(proto.ClaudePluginInstalled); st.NextCheck.IsZero() {
		t.Fatalf("first check %+v", st)
	}
	// A command (another process) uninstalls: the keeper passes its status on.
	other := *fx.m
	if _, err := other.Do(context.Background(), OpUninstall); err != nil {
		t.Fatal(err)
	}
	// A file time apart from the keeper's own write, however coarse the file system's clock.
	later := time.Now().Add(time.Minute)
	if err := os.Chtimes(filepath.Join(fx.data, StatusFile), later, later); err != nil {
		t.Fatal(err)
	}
	if st := wait(proto.ClaudePluginNotInstalled); st.Auto {
		t.Fatalf("after the command %+v", st)
	}
	if k.Status().State != proto.ClaudePluginNotInstalled {
		t.Fatalf("Status %+v", k.Status())
	}
}

func TestKeeperBacksOffAfterAFailure(t *testing.T) {
	fx := newFixture(t, "0.1.0")
	fx.f.failing["install"] = "offline"
	k := NewKeeper(fx.m)
	k.First, k.Every, k.Poll = time.Millisecond, time.Hour, time.Hour
	k.Backoff = []time.Duration{5 * time.Millisecond}
	got := make(chan proto.ClaudePlugin, 8)
	defer k.Subscribe(func(st proto.ClaudePlugin) { got <- st })()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go k.Run(ctx)
	select {
	case st := <-got:
		if st.State != proto.ClaudePluginError {
			t.Fatalf("first %+v", st)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("no first check")
	}
	fx.f.mu.Lock()
	delete(fx.f.failing, "install")
	fx.f.mu.Unlock()
	for {
		select {
		case st := <-got:
			if st.State == proto.ClaudePluginInstalled {
				return
			}
		case <-time.After(10 * time.Second):
			t.Fatal("not retried")
		}
	}
}

func TestDefaultOffWhereNotAsked(t *testing.T) {
	fx := newFixture(t, "0.1.0")
	fx.m.DefaultAuto = false // Linux
	if st := fx.do(t, OpCheck); st.Auto || st.State != proto.ClaudePluginNotInstalled || fx.f.plugin(fx.dir, id) != nil {
		t.Fatalf("installed by default: %+v", st)
	}
	if st := fx.do(t, OpOn); !st.Auto || st.State != proto.ClaudePluginInstalled {
		t.Fatalf("on %+v", st)
	}
	if st := fx.do(t, OpCheck); !st.Auto {
		t.Fatalf("on is kept: %+v", st)
	}
}

func TestNewer(t *testing.T) {
	for _, c := range []struct {
		a, b string
		want bool
	}{{"2.1.308", "2.1.291", true}, {"2.1.291 (Claude Code)", "2.1.308", false}, {"2.10.0", "2.9.9", true},
		{"1.0.0", "1.0.0", false}, {"1.0.0", "", true}, {"", "1.0.0", false}, {"2.1.3-beta", "2.1.2", true}} {
		if got := Newer(c.a, c.b); got != c.want {
			t.Errorf("Newer(%q, %q) = %v", c.a, c.b, got)
		}
	}
}
