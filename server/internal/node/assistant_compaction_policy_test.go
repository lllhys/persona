package node

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/nwverse/persona/server/internal/proto"
)

func TestAssistantCompactionLimits(t *testing.T) {
	for _, tt := range []struct {
		name                   string
		main                   bool
		capacity, window, idle int
	}{
		{"main", true, 1_000_000, 400_000, 300_000},
		{"ordinary", false, 1_000_000, 200_000, 150_000},
		{"legacy ordinary", false, 0, 200_000, 150_000},
		{"main 200K backend", true, 200_000, 200_000, 150_000},
		{"main 300K backend", true, 300_000, 300_000, 225_000},
		{"main 128K backend", true, 128_000, 128_000, 96_000},
		{"ordinary 128K backend", false, 128_000, 128_000, 96_000},
	} {
		t.Run(tt.name, func(t *testing.T) {
			c := compactionLimits(tt.main, tt.capacity)
			if c.window != tt.window || c.idle != tt.idle {
				t.Fatalf("limits: %+v, want %d/%d", c, tt.window, tt.idle)
			}
			p := &assistantProc{agent: "claude", compactIdleAt: c.idle, context: c.idle - 1}
			if p.idleCompactable() {
				t.Fatal("compacts below idle threshold")
			}
			p.context++
			if !p.idleCompactable() {
				t.Fatal("does not compact at idle threshold")
			}
			for _, args := range [][]string{c.env(), codexArgs("personad", "test", c.window)} {
				if !strings.Contains(strings.Join(args, "\n"), "="+strconv.Itoa(tt.window)) {
					t.Fatalf("runtime window not set: %q", args)
				}
			}
		})
	}
}

func TestAssistantProviderCapacity(t *testing.T) {
	base := proto.ManagedLauncher{Name: "gateway", Model: "big", Models: []proto.ProviderModel{
		{ID: "big", ContextWindow: 1_000_000}, {ID: "small", ContextWindow: 128_000},
	}, CompactWindow: 200_000}
	for _, tt := range []struct {
		name, model, env string
		max, want        int
	}{
		{"real capacity not compact policy", "big", "", 0, 1_000_000},
		{"selected small model", "small", "", 0, 128_000},
		{"explicit max", "big", "", 200_000, 200_000},
		{"model beats inflated max", "small", "", 1_000_000, 128_000},
		{"env max", "big", "CLAUDE_CODE_MAX_CONTEXT_TOKENS=128000", 0, 128_000},
		{"auto env does not mean capacity", "big", "CLAUDE_CODE_AUTO_COMPACT_WINDOW=150000", 0, 1_000_000},
	} {
		t.Run(tt.name, func(t *testing.T) {
			l := base
			l.MaxContext, l.Env = tt.max, tt.env
			if got := providerCapacity(l, tt.model); got != tt.want {
				t.Fatalf("capacity %d, want %d", got, tt.want)
			}
			if got := providerEnv(l, tt.model, "", true)["CLAUDE_CODE_MAX_CONTEXT_TOKENS"]; got != strconv.Itoa(tt.want) {
				t.Fatalf("MAX_CONTEXT_TOKENS %s, want %d", got, tt.want)
			}
		})
	}
}

func TestAssistantCompactionRPCAndSpare(t *testing.T) {
	for _, main := range []bool{true, false} {
		t.Run(strconv.FormatBool(main), func(t *testing.T) {
			fakeClaudeHome(t, "read line\nprintf '%s\\n' '{\"type\":\"system\",\"subtype\":\"init\"}'\nsleep 30\n")
			t.Setenv("CLAUDE_CODE_MAX_CONTEXT_TOKENS", "1000000")
			n := codexTestNode(t)
			n.dir = t.TempDir()
			withFakeHub(t, n)
			params := map[string]any{"launcher": "claude", "cwd": t.TempDir(), "text": "hello"}
			if main {
				params["is_main"] = true
			} else {
				params["session_id"] = "11111111-2222-3333-4444-555555555555"
				n.state.put(params["session_id"].(string), sessionState{IsMain: true})
			}
			raw, err := nodeCall(t, n, "assistant.send", params)
			if err != nil {
				t.Fatal(err)
			}
			var out struct {
				SessionID string `json:"session_id"`
			}
			if err := json.Unmarshal(raw, &out); err != nil {
				t.Fatal(err)
			}
			defer n.stopAssistant(out.SessionID)
			p := n.procs[out.SessionID]
			c := compactionLimits(main, 1_000_000)
			if p.compactIdleAt != c.idle || envValue(p.cmd.Env, "CLAUDE_CODE_AUTO_COMPACT_WINDOW") != strconv.Itoa(c.window) {
				t.Fatalf("process limits: idle %d, env %v", p.compactIdleAt, p.cmd.Env)
			}
			st, _ := n.state.get(out.SessionID)
			if st.IsMain != main {
				t.Fatalf("stored scope %v", st.IsMain)
			}
			n.spare = p
			if main && n.adoptSpare(p.cwd, p.launcher, modelExecution{}) != nil {
				t.Fatal("a main process was adopted for an ordinary conversation")
			}
		})
	}
}

func TestAssistantCompactionSettingsAndSDK(t *testing.T) {
	n := assistantNode(t)
	n.dir = t.TempDir()
	settings := filepath.Join(t.TempDir(), "settings.json")
	if err := os.WriteFile(settings, []byte(`{"hooks":{"Stop":[]},"env":{"KEEP":"yes","CLAUDE_CODE_AUTO_COMPACT_WINDOW":"200000","CLAUDE_CODE_MAX_CONTEXT_TOKENS":"200000"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	c := compactionLimits(true, 1_000_000)
	c.ownSettings = `{"env":{"LAUNCHER":"yes"},"permissions":{"allow":["Read"]}}`
	path, err := n.assistantCompactionSettings("test-id", n.home, settings, c)
	if err != nil {
		t.Fatal(err)
	}
	s, err := readLauncherSettings(path, n.home, n.home)
	if err != nil {
		t.Fatal(err)
	}
	env := s["env"].(map[string]any)
	if env["KEEP"] != "yes" || env["LAUNCHER"] != "yes" || env["CLAUDE_CODE_AUTO_COMPACT_WINDOW"] != "400000" || env["CLAUDE_CODE_MAX_CONTEXT_TOKENS"] != "1000000" || s["hooks"] == nil || s["permissions"] == nil {
		t.Fatalf("merged settings %v", s)
	}
	start := sdkStart{settings: path, mcp: map[string]any{}, compaction: &c, env: map[string]string{"CLAUDE_CODE_MAX_CONTEXT_TOKENS": "200000", "CLAUDE_CODE_AUTO_COMPACT_WINDOW": "200000"}}
	opts, err := start.options("claude")
	if err != nil {
		t.Fatal(err)
	}
	env = opts["settings"].(map[string]any)["env"].(map[string]any)
	if env["CLAUDE_CODE_AUTO_COMPACT_WINDOW"] != "400000" || env["CLAUDE_CODE_MAX_CONTEXT_TOKENS"] != "1000000" {
		t.Fatalf("SDK clamped policy back down: %v", env)
	}
	if mode, err := os.Stat(path); err != nil || mode.Mode().Perm() != 0o600 {
		t.Fatalf("settings permissions: %v %v", mode, err)
	}
}

func TestAssistantCodexCapacity(t *testing.T) {
	for _, tt := range []struct {
		args []string
		want int
	}{
		{[]string{"-c", "model_context_window=128000"}, 128000},
		{[]string{"--config=model_context_window=200_000"}, 200000},
		{[]string{"model_context_window=1000000\n[profiles.small]\nmodel_context_window=128000 # cap\n"}, 128000},
		{[]string{"-c", "model_auto_compact_token_limit=200000"}, 0},
	} {
		if got := codexCapacity(tt.args); got != tt.want {
			t.Fatalf("%q: %d, want %d", tt.args, got, tt.want)
		}
	}
}

// A main conversation started through a shell launcher (not a managed one): a declared capacity caps it; one that
// declares none gets the main target and no MAX_CONTEXT_TOKENS (#3008), since Claude Code holds the automatic window
// to the window it knows the model to have. Its idle threshold waits for that window (TestAssistantIdleCompactFromWindow).
func TestAssistantShellLauncherCompaction(t *testing.T) {
	for _, tt := range []struct {
		name, rc     string
		window, idle int
		max          string // the MAX_CONTEXT_TOKENS written, "" for none
		fromWindow   bool
	}{
		{"large backend", `function testproxy { export CLAUDE_CODE_MAX_CONTEXT_TOKENS=1000000 CLAUDE_CODE_AUTO_COMPACT_WINDOW=200000; command claude "$@"; }`, 400000, 300000, "1000000", false},
		{"declared 200K backend", `function testproxy { export CLAUDE_CODE_MAX_CONTEXT_TOKENS=200000; command claude "$@"; }`, 200000, 150000, "200000", false},
		{"small backend", `function testproxy { export CLAUDE_CODE_MAX_CONTEXT_TOKENS=128000 CLAUDE_CODE_AUTO_COMPACT_WINDOW=128000; command claude "$@"; }`, 128000, 96000, "128000", false},
		{"unknown backend", `function testproxy { export CLAUDE_CODE_AUTO_COMPACT_WINDOW=1000000; command claude "$@"; }`, 400000, 300000, "", true},
		{"gateway function declaring nothing", `function testproxy { ( export ANTHROPIC_BASE_URL=http://127.0.0.1:9; command claude --model opus "$@" ) }`, 400000, 300000, "", true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			fakeClaudeHome(t, "read line\nprintf '%s\\n' '{\"type\":\"system\",\"subtype\":\"init\"}'\nsleep 30\n")
			t.Setenv("CLAUDE_CODE_MAX_CONTEXT_TOKENS", "")
			home := os.Getenv("HOME")
			if err := os.WriteFile(filepath.Join(home, ".zshrc"), []byte(tt.rc+"\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			n := codexTestNode(t)
			n.dir = t.TempDir()
			withFakeHub(t, n)
			p, err := n.spawnClaude("11111111-2222-3333-4444-555555555555", t.TempDir(), "testproxy", false, false, modelExecution{IsMain: true})
			if err != nil {
				t.Fatal(err)
			}
			defer n.stopAssistant(p.id)
			settings, err := readLauncherSettings(settingsArg(p.cmd.Args), p.cwd, home)
			if err != nil {
				entries, _ := os.ReadDir(filepath.Join(n.dir, "launcher-settings"))
				t.Fatalf("%v; args=%q; settings files=%v", err, p.cmd.Args, entries)
			}
			env := settings["env"].(map[string]any)
			if env["CLAUDE_CODE_AUTO_COMPACT_WINDOW"] != strconv.Itoa(tt.window) || p.compactIdleAt != tt.idle {
				t.Fatalf("settings %v, idle %d", env, p.compactIdleAt)
			}
			if max, _ := env["CLAUDE_CODE_MAX_CONTEXT_TOKENS"].(string); max != tt.max {
				t.Fatalf("MAX_CONTEXT_TOKENS %q, want %q (settings %v)", max, tt.max, env)
			}
			if p.idleFromWindow != tt.fromWindow {
				t.Fatalf("idle threshold from the reported window: %v, want %v", p.idleFromWindow, tt.fromWindow)
			}
		})
	}
}

// Nothing declared: a Claude main conversation gets the main target without a capacity; an ordinary one keeps its
// limits; a Codex main conversation keeps the ordinary limit, since Codex takes its compact limit as given.
func TestAssistantUnknownCapacityPolicy(t *testing.T) {
	fakeClaudeHome(t, "exit 0\n")
	t.Setenv("CLAUDE_CODE_MAX_CONTEXT_TOKENS", "")
	t.Setenv("CODEX_HOME", t.TempDir())
	home := os.Getenv("HOME")
	rc := "function testproxy { command claude \"$@\"; }\nfunction testcodex { command codex \"$@\"; }\n"
	if err := os.WriteFile(filepath.Join(home, ".zshrc"), []byte(rc), 0o600); err != nil {
		t.Fatal(err)
	}
	n := codexTestNode(t)
	n.dir = t.TempDir()
	cwd := t.TempDir()
	for _, tt := range []struct {
		name, launcher string
		main           bool
		window, idle   int
		env            []string
		fromWindow     bool
	}{
		{"claude main", "testproxy", true, 400_000, 300_000, []string{"CLAUDE_CODE_AUTO_COMPACT_WINDOW=400000"}, true},
		{"claude ordinary", "testproxy", false, 200_000, 150_000, []string{"CLAUDE_CODE_AUTO_COMPACT_WINDOW=200000"}, false},
		{"codex main", "testcodex", true, 200_000, 150_000, nil, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			c := n.assistantCompactionPolicy(tt.launcher, cwd, "", modelExecution{Model: "opus", IsMain: tt.main})
			if c.window != tt.window || c.idle != tt.idle || c.idleFromWindow != tt.fromWindow {
				t.Fatalf("policy %+v, want %d/%d from window %v", c, tt.window, tt.idle, tt.fromWindow)
			}
			if tt.env != nil && strings.Join(c.env(), " ") != strings.Join(tt.env, " ") {
				t.Fatalf("env %q, want %q", c.env(), tt.env)
			}
		})
	}
}

// The window Claude Code reports at the end of a turn caps the idle threshold of a main conversation whose capacity
// nothing declared: a 200K model compacts at 150K while its cache is warm, a 1M one keeps 300K. A declared capacity
// is not second-guessed, and a subagent's or helper model's entry is not the conversation's window.
func TestAssistantIdleCompactFromWindow(t *testing.T) {
	result := func(usage string) string { return `{"type":"result","subtype":"success","modelUsage":` + usage + `}` }
	start := func(model string) string {
		return `{"type":"stream_event","event":{"type":"message_start","message":{"model":"` + model +
			`","usage":{"input_tokens":2,"cache_read_input_tokens":170000}}},"parent_tool_use_id":null}`
	}
	for _, tt := range []struct {
		name       string
		fromWindow bool
		lines      []string
		want       int
	}{
		{"1M model", true, []string{start("claude-opus-5-5"), result(`{"claude-opus-5-5":{"inputTokens":2,"cacheReadInputTokens":170000,"contextWindow":1000000}}`)}, 300_000},
		{"200K model", true, []string{start("small-model"), result(`{"small-model":{"inputTokens":2,"cacheReadInputTokens":170000,"contextWindow":200000}}`)}, 150_000},
		{"the conversation's model, not a helper's", true, []string{start("claude-opus-5-5"), result(`{"claude-opus-5-5":{"inputTokens":2,"cacheReadInputTokens":170000,"contextWindow":1000000},"claude-haiku-4-5":{"inputTokens":900000,"contextWindow":200000}}`)}, 300_000},
		{"gateway renames the model: the one that read most", true, []string{start("gw/opus"), result(`{"claude-opus-5-5":{"inputTokens":2,"cacheReadInputTokens":170000,"contextWindow":1000000},"claude-haiku-4-5":{"inputTokens":3000,"contextWindow":200000}}`)}, 300_000},
		{"no window reported", true, []string{start("x"), result(`{"x":{"inputTokens":2}}`), `{"type":"result","subtype":"success"}`}, 300_000},
		{"declared capacity", false, []string{start("small-model"), result(`{"small-model":{"inputTokens":2,"contextWindow":200000}}`)}, 300_000},
	} {
		t.Run(tt.name, func(t *testing.T) {
			n := assistantNode(t)
			const id = "11111111-2222-3333-4444-555555555555"
			p := &assistantProc{id: id, agent: "claude", stdin: &stdinBuf{}, compactIdleAt: 300_000, idleFromWindow: tt.fromWindow}
			n.procs[id] = p
			n.readAssistant(p, strings.NewReader(strings.Join(tt.lines, "\n")+"\n"))
			p.mu.Lock()
			got := p.compactIdleAt
			p.mu.Unlock()
			if got != tt.want {
				t.Fatalf("idle threshold %d, want %d", got, tt.want)
			}
		})
	}
	// It only ever goes down: a later turn reporting a larger window does not raise it again.
	p := &assistantProc{compactIdleAt: 150_000, idleFromWindow: true}
	if p.capIdleCompact(1_000_000) || p.compactIdleAt != 150_000 {
		t.Fatalf("raised to %d", p.compactIdleAt)
	}
}

func TestAssistantSDKCompactionStart(t *testing.T) {
	f := sdkTest(t)
	f.n.setManaged([]proto.ManagedLauncher{{Name: "gw", Model: "big", CompactWindow: 200000, Models: []proto.ProviderModel{{ID: "big", ContextWindow: 1000000}}}})
	execution := sdkExec(proto.AssistantRuntimeAgentSDK)
	execution.IsMain = true
	id, err := f.n.assistantSend(context.Background(), "", t.TempDir(), "gw", "hello", nil, "", "", execution)
	if err != nil {
		t.Fatal(err)
	}
	opts := f.start(t)
	env := opts["settings"].(map[string]any)["env"].(map[string]any)
	if env["CLAUDE_CODE_AUTO_COMPACT_WINDOW"] != "400000" || env["CLAUDE_CODE_MAX_CONTEXT_TOKENS"] != "1000000" || f.proc(id).compactIdleAt != 300000 {
		t.Fatalf("SDK start: %v, idle %d", env, f.proc(id).compactIdleAt)
	}
}
