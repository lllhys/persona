package node

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/nwverse/persona/server/internal/lazyre"
	"github.com/nwverse/persona/server/internal/proto"
)

type assistantCompaction struct {
	window, idle, capacity int
	ownSettings            string
	// idleFromWindow: a Claude main conversation whose capacity nothing declares. Its automatic window stays the
	// main target (Claude Code holds it to the window it knows the model to have), and its idle threshold is capped
	// once Claude Code reports that window (assistantProc.capIdleCompact).
	idleFromWindow bool
}

func compactionLimits(main bool, capacity int) assistantCompaction {
	window, idle := assistantCompactWindow, assistantIdleCompact
	if main {
		window, idle = assistantMainCompactWindow, assistantMainIdleCompact
	}
	if capacity > 0 {
		window = min(window, capacity)
		idle = min(idle, window/4*3)
	}
	return assistantCompaction{window: window, idle: idle, capacity: capacity}
}

func smallerCapacity(a, b int) int {
	if b > 0 && (a <= 0 || b < a) {
		return b
	}
	return a
}

func contextLimit(s string) int {
	n, _ := strconv.Atoi(strings.TrimSpace(s))
	if n < 0 {
		return 0
	}
	return n
}

func providerCapacity(l proto.ManagedLauncher, model string) int {
	if model == "" {
		model = l.Model
	}
	pm, _ := providerModel(l, model)
	capacity := smallerCapacity(pm.ContextWindow, l.MaxContext)
	for _, kv := range launcherEnvVars(l.Env) {
		if kv[0] == "CLAUDE_CODE_MAX_CONTEXT_TOKENS" {
			capacity = smallerCapacity(capacity, contextLimit(kv[1]))
		}
	}
	return smallerCapacity(capacity, codexCapacity(l.Args))
}

var codexContextLimit = lazyre.New(`(?m)^\s*model_context_window\s*=\s*([0-9][0-9_]*)\s*(?:#.*)?$`)

func codexCapacity(args []string) int {
	capacity := 0
	for i, arg := range args {
		if (arg == "-c" || arg == "--config") && i+1 < len(args) {
			arg = args[i+1]
		} else if v, ok := strings.CutPrefix(arg, "--config="); ok {
			arg = v
		}
		for _, m := range codexContextLimit.FindAllStringSubmatch(arg, -1) {
			capacity = smallerCapacity(capacity, contextLimit(strings.ReplaceAll(m[1], "_", "")))
		}
	}
	return capacity
}

func settingsCapacity(arg, cwd, home string) int {
	if arg == "" {
		return 0
	}
	s, err := readLauncherSettings(arg, cwd, home)
	if err != nil {
		return 0
	}
	env, _ := s["env"].(map[string]any)
	switch v := env["CLAUDE_CODE_MAX_CONTEXT_TOKENS"].(type) {
	case string:
		return contextLimit(v)
	case float64:
		return int(v)
	}
	return 0
}

func (n *Node) assistantCompactionPolicy(launcher, cwd, settings string, execution modelExecution) assistantCompaction {
	capacity := 0
	isCodex := n.agentOf(launcher) == "codex"
	ownSettings := ""
	configDir, codexHome := os.Getenv("CLAUDE_CONFIG_DIR"), os.Getenv("CODEX_HOME")
	if dir := n.claudeStartDir.Load(); dir != nil && *dir != "" {
		configDir = proto.ExpandHome(*dir, n.home)
	}
	if l, managed := n.managedLauncher(launcher); managed {
		capacity = providerCapacity(l, execution.Model)
		if isCodex {
			for _, kv := range launcherEnvVars(l.Env) {
				if kv[0] == "CODEX_HOME" {
					codexHome = kv[1]
				}
			}
		} else {
			ownSettings = settingsArg(l.Args)
			capacity = smallerCapacity(capacity, settingsCapacity(ownSettings, cwd, n.home))
		}
	} else {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if p, err := n.launcherProbe(ctx, launcher); err == nil {
			capacity = contextLimit(p.Env["CLAUDE_CODE_MAX_CONTEXT_TOKENS"])
			capacity = smallerCapacity(capacity, codexCapacity(p.Args))
			ownSettings = settingsArg(p.Args)
			capacity = smallerCapacity(capacity, settingsCapacity(ownSettings, cwd, n.home))
			if dir := p.Env["CLAUDE_CONFIG_DIR"]; dir != "" {
				configDir = dir
			}
			if dir := p.Env["CODEX_HOME"]; dir != "" {
				codexHome = dir
			}
		}
		if !isCodex {
			capacity = smallerCapacity(capacity, contextLimit(os.Getenv("CLAUDE_CODE_MAX_CONTEXT_TOKENS")))
			if configDir == "" {
				configDir = filepath.Join(n.home, ".claude")
			}
			capacity = smallerCapacity(capacity, settingsCapacity(filepath.Join(configDir, "settings.json"), cwd, n.home))
			for _, file := range []string{"settings.json", "settings.local.json"} {
				capacity = smallerCapacity(capacity, settingsCapacity(filepath.Join(cwd, ".claude", file), cwd, n.home))
			}
		}
	}
	if isCodex {
		if codexHome == "" {
			codexHome = n.codexHome()
		}
		if b, err := os.ReadFile(filepath.Join(codexHome, "config.toml")); err == nil {
			capacity = smallerCapacity(capacity, codexCapacity([]string{string(b)}))
		}
	}
	capacity = smallerCapacity(capacity, settingsCapacity(settings, cwd, n.home))
	unknown := capacity == 0
	if unknown && execution.IsMain && isCodex {
		// Codex takes model_auto_compact_token_limit as given, past the model's window if need be: a main conversation on
		// a backend nobody declared keeps the ordinary limit.
		capacity = assistantCompactWindow
	}
	// A Claude main conversation on an undeclared backend gets the main target and no MAX_CONTEXT_TOKENS (#3008): Claude
	// Code clamps CLAUDE_CODE_AUTO_COMPACT_WINDOW to the window it knows the model to have, so 400K cannot outgrow a
	// 200K model, while a guessed 200K capacity made a 1M model compact at about 167K.
	c := compactionLimits(execution.IsMain, capacity)
	c.ownSettings = ownSettings
	c.idleFromWindow = unknown && execution.IsMain && !isCodex
	return c
}

// claudeModelUsage is one model's entry in a stream-json result's modelUsage: what this process read with it and the
// context window Claude Code holds it to.
type claudeModelUsage struct {
	Input         int `json:"inputTokens"`
	CacheRead     int `json:"cacheReadInputTokens"`
	CacheCreate   int `json:"cacheCreationInputTokens"`
	ContextWindow int `json:"contextWindow"`
}

// reportedWindow is the context window Claude Code holds the conversation's model to, 0 when it reports none: the
// entry of model (the model of the conversation's last reply), else the entry that read the most context (a subagent
// or a helper model reads less than the conversation).
func reportedWindow(usage map[string]claudeModelUsage, model string) int {
	if u, ok := usage[model]; ok && model != "" {
		return u.ContextWindow
	}
	most, window := -1, 0
	for _, u := range usage {
		read := u.Input + u.CacheRead + u.CacheCreate
		if read > most || (read == most && u.ContextWindow < window) {
			most, window = read, u.ContextWindow
		}
	}
	return window
}

// capIdleCompact caps the idle threshold of a conversation whose capacity nothing declared at 75% of the window Claude
// Code reports, as compactionLimits does for a declared capacity: on a 200K backend the main conversation still
// compacts at 150K while its cache is warm instead of waiting for 300K it never reaches. It reports whether the
// threshold went down; p.mu is held.
func (p *assistantProc) capIdleCompact(window int) bool {
	if !p.idleFromWindow || window <= 0 {
		return false
	}
	limit := window / 4 * 3
	current := p.compactIdleAt
	if current == 0 {
		current = assistantIdleCompact
	}
	if limit >= current {
		return false
	}
	p.compactIdleAt = limit
	return true
}

func (c assistantCompaction) env() []string {
	env := []string{"CLAUDE_CODE_AUTO_COMPACT_WINDOW=" + strconv.Itoa(c.window)}
	if c.capacity > 0 {
		env = append(env, "CLAUDE_CODE_MAX_CONTEXT_TOKENS="+strconv.Itoa(c.capacity))
	}
	return env
}

func (n *Node) assistantCompactionSettings(id, cwd, settings string, c assistantCompaction) (string, error) {
	s := map[string]any{}
	if settings != "" {
		var err error
		s, err = readLauncherSettings(settings, cwd, n.home)
		if err != nil {
			if !os.IsNotExist(err) {
				return "", err
			}
			s = map[string]any{}
		}
	}
	if c.ownSettings != "" {
		theirs, err := readLauncherSettings(c.ownSettings, cwd, n.home)
		if err != nil {
			return "", err
		}
		mergedEnv := map[string]any{}
		if env, ok := theirs["env"].(map[string]any); ok {
			for k, v := range env {
				mergedEnv[k] = v
			}
		}
		if env, ok := s["env"].(map[string]any); ok {
			for k, v := range env {
				mergedEnv[k] = v
			}
		}
		s = mergeClaudeSettings(theirs, s)
		s["env"] = mergedEnv
	}
	env, _ := s["env"].(map[string]any)
	if env == nil {
		env = map[string]any{}
	}
	for _, kv := range c.env() {
		k, v, _ := strings.Cut(kv, "=")
		env[k] = v
	}
	s["env"] = env
	b, err := json.Marshal(s)
	if err != nil {
		return "", err
	}
	dir := n.dir
	if dir == "" {
		dir = filepath.Join(n.home, ".persona")
	}
	return writeSettings(dir, "assistant-compact-"+id+".json", b)
}
