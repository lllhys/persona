package node

import (
	"context"
	"encoding/json"
	"io"
	"log"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/identity"
	"github.com/nwverse/persona/server/internal/proto"
)

func scopedTestNode(t *testing.T) *Node {
	t.Helper()
	n, home := testNode(t)
	n.dir = filepath.Join(home, "work-node")
	n.logger = log.New(io.Discard, "", 0)
	n.metas, n.bgScans = map[string]quickMeta{}, map[string]*bgScan{}
	scope := filepath.Join(n.dir, "assistant-m-work")
	claude, codex := filepath.Join(n.dir, "claude"), filepath.Join(n.dir, "codex")
	for _, p := range []string{scope, claude, codex} {
		if err := os.MkdirAll(p, 0700); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PERSONA_HOME", n.dir)
	t.Setenv(nodeSessionDirEnv, scope)
	t.Setenv("CLAUDE_CONFIG_DIR", claude)
	t.Setenv("CODEX_HOME", codex)
	var err error
	n.sessionScope, err = readNodeSessionScope(home, n.dir)
	if err != nil {
		t.Fatal(err)
	}
	n.cdirs, n.codices = newClaudeDirs(), newCodexRegistry()
	n.configureCodex(proto.CodexConfig{})
	n.cdirs.scan = func() map[int]procEnv { t.Fatal("scoped node scanned the user's Claude processes"); return nil }
	n.codices.scan = func() []codexProcess { t.Fatal("scoped node scanned the user's Codex processes"); return nil }
	return n
}

func scopeClaudeTranscript(t *testing.T, profile, id, cwd string) string {
	t.Helper()
	p := filepath.Join(profile, "projects", "-example", id+".jsonl")
	if err := os.MkdirAll(filepath.Dir(p), 0700); err != nil {
		t.Fatal(err)
	}
	line, err := json.Marshal(map[string]any{"type": "user", "cwd": cwd, "timestamp": time.Now(), "message": map[string]any{"role": "user", "content": "retained history"}})
	if err != nil {
		t.Fatal(err)
	}
	data := string(line) + "\n" + `{"type":"custom-title","customTitle":"retained"}` + "\n" + strings.Repeat(" ", 4096) + "\n"
	if err := os.WriteFile(p, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestNodeSessionScopeProfilesAndHistory(t *testing.T) {
	n := scopedTestNode(t)
	const original = "aaaaaaaa-0000-0000-0000-000000000001"
	const own = "bbbbbbbb-0000-0000-0000-000000000002"
	const other = "cccccccc-0000-0000-0000-000000000003"
	originalPath := scopeClaudeTranscript(t, filepath.Join(n.home, ".claude"), original, "/original/assistant")
	ownPath := scopeClaudeTranscript(t, n.sessionScope.claude, own, n.sessionScope.dir)
	scopeClaudeTranscript(t, n.sessionScope.claude, other, n.sessionScope.dir+"-other")
	n.setKnownClaudeDirs([]string{filepath.Join(n.home, ".claude")})
	n.refreshClaudeDirs()
	if got := n.claudeConfigDirs(); !reflect.DeepEqual(got, []string{n.sessionScope.claude}) {
		t.Fatalf("profiles: %v", got)
	}
	if got := n.transcriptPath(own); got != ownPath {
		t.Fatalf("own history: %q", got)
	}
	n.paths[original] = originalPath // a late hook must not introduce the primary node's history.
	if got := n.transcriptPath(original); got != "" {
		t.Fatalf("original history leaked: %q", got)
	}
	if got := n.transcriptPath(other); got != "" {
		t.Fatalf("another cwd leaked: %q", got)
	}
	n.setKept([]string{original, other})
	offlineCache.Lock()
	offlineCache.at, offlineCache.home = time.Now(), n.home
	offlineCache.list = []proto.Session{{SessionID: original, Cwd: "/original/assistant"}}
	offlineCache.Unlock()
	rows := n.appendOffline(nil, map[string]bool{})
	if len(rows) != 1 || rows[0].SessionID != own {
		t.Fatalf("history: %+v", rows)
	}
	if err := n.refreshHosts(context.Background()); err != nil {
		t.Fatal(err)
	} // no registry needed: no host is queried.
}

func TestNodeSessionScopeInternalAssistantSymlink(t *testing.T) {
	n := scopedTestNode(t)
	const manager = "m-0b0b0b"
	canonical := filepath.Join(n.dir, ".persona", "assistant-"+manager)
	entry := filepath.Join(n.dir, "assistant-"+manager)
	if err := os.MkdirAll(canonical, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(canonical, entry); err != nil {
		t.Fatal(err)
	}
	t.Setenv(nodeSessionDirEnv, entry)
	var err error
	n.sessionScope, err = readNodeSessionScope(n.home, n.dir)
	if err != nil {
		t.Fatal(err)
	}
	if !identity.IsAssistantDir(canonical) || !identity.IsAssistantDirOf(canonical, manager) {
		t.Fatal("canonical cwd is not recognized by the existing assistant scanner")
	}
	for _, cwd := range []string{entry, canonical, filepath.Join(canonical, "nested")} {
		if !n.sessionScopeAllows(cwd) {
			t.Fatalf("internal canonical cwd rejected: %s", cwd)
		}
	}
	if n.sessionScopeAllows(filepath.Join(n.dir, ".persona", "assistant-m-abcdef")) {
		t.Fatal("another manager's canonical cwd was allowed")
	}
	const own = "bbbbbbbb-0000-0000-0000-000000000004"
	path := scopeClaudeTranscript(t, n.sessionScope.claude, own, canonical)
	if got := n.transcriptPath(own); got != path {
		t.Fatalf("canonical history rejected: %q", got)
	}
	rows := n.appendOffline(nil, map[string]bool{})
	if len(rows) != 1 || rows[0].SessionID != own || rows[0].Cwd != canonical {
		t.Fatalf("canonical history was not discovered: %+v", rows)
	}
}

func TestNodeSessionScopeRejectsExternalCodexDiscovery(t *testing.T) {
	n := scopedTestNode(t)
	discovery := true
	n.configureCodex(proto.CodexConfig{Homes: []string{filepath.Join(n.home, ".codex")}, LegacyHome: filepath.Join(n.home, ".codex"), Discovery: &discovery,
		Servers: []proto.CodexServer{{ID: "original", Home: filepath.Join(n.home, ".codex"), Endpoint: "stdio://"}}})
	n.discoverCodex()
	cfg := n.codexInventory()
	// The inventory keeps homes with symlinks resolved (canonicalCodexHome).
	if !reflect.DeepEqual(cfg.Homes, []string{resolved(t, n.sessionScope.codex)}) || len(cfg.Servers) != 0 || cfg.Discovery == nil || *cfg.Discovery {
		t.Fatalf("inventory: %+v", cfg)
	}
	writeRollout(t, n.home, remoteRaw, "/original/assistant", time.Now(), time.Now())
	if got := n.registerCodex(filepath.Join(n.home, ".codex"), remoteRaw, rolloutPath(filepath.Join(n.home, ".codex"), remoteRaw)); got != "" {
		t.Fatalf("external rollout registered: %s", got)
	}
	if got := n.transcriptPath(remoteRaw); got != "" {
		t.Fatalf("original Codex history leaked: %s", got)
	}
	if got := n.codexSessions(map[string]bool{}); len(got) != 0 {
		t.Fatalf("original Codex sessions leaked: %+v", got)
	}
	// A private profile retains its own transcript over a node restart.
	privateHome := filepath.Join(n.dir, "fixture")
	writeRollout(t, privateHome, remoteOther, n.sessionScope.dir, time.Now(), time.Now())
	source := rolloutPath(filepath.Join(privateHome, ".codex"), remoteOther)
	destination := filepath.Join(n.sessionScope.codex, "sessions", time.Now().Format("2006/01/02"), filepath.Base(source))
	if err := os.MkdirAll(filepath.Dir(destination), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(source, destination); err != nil {
		t.Fatal(err)
	}
	n.codices = newCodexRegistry()
	n.configureCodex(proto.CodexConfig{})
	if got := n.transcriptPath(remoteOther); got != destination {
		t.Fatalf("private Codex history lost: %q", got)
	}
}

func TestNodeSessionScopeNeverChangesGlobalHooks(t *testing.T) {
	n := scopedTestNode(t)
	for _, profile := range []string{filepath.Join(n.home, ".claude"), n.sessionScope.claude} {
		p := filepath.Join(profile, "settings.json")
		if err := os.MkdirAll(profile, 0700); err != nil {
			t.Fatal(err)
		}
		before := []byte(`{"hooks":{"Stop":[{"hooks":[{"type":"command","command":"original-personad hook --persona-global"}]}]},"keep":true}`)
		if err := os.WriteFile(p, before, 0600); err != nil {
			t.Fatal(err)
		}
		for _, on := range []bool{true, false, true} {
			params, err := json.Marshal(map[string]any{"claude_global_hooks": on, "claude_start_dir": filepath.Join(n.home, ".claude")})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := n.handle(context.Background(), "node.configure", params); err != nil {
				t.Fatal(err)
			}
		}
		if start := n.claudeStartDir.Load(); start == nil || *start != n.sessionScope.claude {
			t.Fatal("configure escaped the private Claude profile")
		}
		after, err := os.ReadFile(p)
		if err != nil || string(after) != string(before) {
			t.Fatalf("settings changed: %s, %v", p, err)
		}
		entries, err := os.ReadDir(profile)
		if err != nil {
			t.Fatal(err)
		}
		if len(entries) != 1 {
			t.Fatalf("unexpected hook artifacts: %v", entries)
		}
	}
}

func TestNodeSessionScopeFailsClosed(t *testing.T) {
	n := scopedTestNode(t)
	for _, item := range []struct{ name, value string }{{"CLAUDE_CONFIG_DIR", ""}, {"CODEX_HOME", filepath.Join(n.home, ".codex")}, {nodeSessionDirEnv, n.dir + "-other"}, {"PERSONA_HOME", ""}, {"PERSONA_HOME", "relative-root"}, {"CLAUDE_CONFIG_DIR", filepath.Join(n.sessionScope.dir, "profile")}} {
		t.Run(item.name, func(t *testing.T) {
			t.Setenv(item.name, item.value)
			if _, err := readNodeSessionScope(n.home, n.dir); err == nil {
				t.Fatal("unsafe scope accepted")
			}
		})
	}
	link := filepath.Join(n.dir, "escape")
	if err := os.Symlink(n.home, link); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CODEX_HOME", filepath.Join(link, "not-created"))
	if _, err := readNodeSessionScope(n.home, n.dir); err == nil {
		t.Fatal("symlink escape accepted")
	}
	t.Setenv(nodeSessionDirEnv, "")
	if scope, err := readNodeSessionScope(n.home, n.dir); err != nil || scope != nil {
		t.Fatalf("default node changed: %v %v", scope, err)
	}
}

func TestNodeSessionScopePinsMCPEnvironment(t *testing.T) {
	n := scopedTestNode(t)
	const id = "work-session"
	n.larkCfgAt = time.Now().Add(time.Hour)
	for _, guest := range []bool{false, true} {
		if guest {
			n.state.put(id, sessionState{Guest: []string{"ask_user"}})
		}
		raw, _ := n.claudeAssistantMCPAndTools(id, n.sessionScope.dir, true)
		var config map[string]any
		if err := json.Unmarshal([]byte(raw), &config); err != nil {
			t.Fatal(err)
		}
		servers := config["mcpServers"].(map[string]any)
		for name, rawServer := range servers {
			env := rawServer.(map[string]any)["env"].(map[string]any)
			if env["PERSONA_HOME"] != n.dir || env["PERSONA_SESSION_ID"] != id || env["PERSONA_ASSISTANT"] != "1" {
				t.Fatalf("guest=%v %s env=%v", guest, name, env)
			}
		}
		// Agent SDK consumes this same map, rather than inheriting an MCP process's environment.
		options, err := (sdkStart{id: id, cwd: n.sessionScope.dir, mcp: config}).options("claude")
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(options["mcpServers"], servers) {
			t.Fatal("SDK dropped pinned MCP environment")
		}
	}
	quotedHome, err := json.Marshal(n.dir)
	if err != nil {
		t.Fatal(err)
	}
	args := strings.Join(codexArgsForHome("personad", "proc-work", n.dir), " ")
	if !strings.Contains(args, "PERSONA_HOME="+string(quotedHome)) || !strings.Contains(args, `PERSONA_SESSION_ID="proc-work"`) {
		t.Fatalf("Codex MCP environment: %s", args)
	}
}
