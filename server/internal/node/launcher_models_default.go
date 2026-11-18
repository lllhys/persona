package node

import (
	"context"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/nwverse/persona/server/internal/proto"
)

// Claude's initialize catalog marks its account recommendation as "default",
// even when the launcher's own --settings selects another model. Keep that
// native alias and its resolution, but identify the model a launch with no
// override uses. No own settings or an uncertain match leaves the catalog alone.
func (n *Node) launcherSettingsModelDefault(ctx context.Context, launcher, cwd string, models []proto.AgentModel) []proto.AgentModel {
	own := n.launcherOwnSettings(ctx, launcher)
	if own == "" {
		return models
	}
	settings, err := readLauncherSettings(own, cwd, n.home)
	if err != nil {
		return models
	}
	model, _ := settings["model"].(string)
	if !proto.ValidModelID(strings.TrimSpace(model)) {
		return models
	}
	// A wrapper we cannot inspect may add a model/effort flag or inherit a different environment. Its native
	// catalog stays authoritative; a settings file alone cannot establish what a new conversation will run.
	p, err := n.launcherProbe(ctx, launcher)
	if err != nil || p.Agent != "claude" || p.Shell == nil {
		return models
	}
	env, known := n.launcherModelEnvironment(p, cwd)
	if !known {
		return models
	}
	return configuredClaudeModelDefault(settings, env, p.Args, models)
}

// launcherModelEnvironment includes the shell's inherited variables, the launcher's changes and its profile's
// settings env. The explicit --settings env is applied last by configuredClaudeModelDefault.
func (n *Node) launcherModelEnvironment(p Probe, cwd string) (map[string]string, bool) {
	if p.Shell == nil {
		return nil, false
	}
	env := maps.Clone(p.Shell)
	for _, key := range p.Unset {
		delete(env, key)
	}
	maps.Copy(env, p.Env)
	dir := filepath.Join(n.home, ".claude")
	if value := env["CLAUDE_CONFIG_DIR"]; value != "" {
		var known bool
		dir, known = absConfigDir(value, n.home)
		if !known {
			return nil, false
		}
	}
	user, err := readLauncherSettings(filepath.Join(dir, "settings.json"), cwd, n.home)
	if err != nil && !os.IsNotExist(err) {
		return nil, false
	}
	maps.Copy(env, settingsEnv(user))
	return env, true
}

func configuredClaudeModelDefault(settings map[string]any, inherited map[string]string, args []string, models []proto.AgentModel) []proto.AgentModel {
	model, _ := settings["model"].(string)
	model = strings.TrimSpace(model)
	if model == "" || !proto.ValidModelID(model) {
		return models
	}
	env := make(map[string]string, len(inherited))
	maps.Copy(env, inherited)
	maps.Copy(env, settingsEnv(settings))
	override := modelArg(args)
	if override == "" {
		override = env["ANTHROPIC_MODEL"]
	}
	if override != "" && override != model {
		return models // the effective command or environment declares another default
	}
	// An exact ID is the explicit selection; only without one may a uniquely
	// resolved alias stand for it. Several aliases for one model are ambiguous.
	match := func(exact bool) int {
		found := -1
		for i, m := range models {
			if exact && m.ID == model || !exact && m.Resolved == model {
				if found >= 0 {
					return -2
				}
				found = i
			}
		}
		return found
	}
	selected := match(true)
	if selected == -1 {
		selected = match(false)
	}
	if selected < 0 {
		return models
	}
	out := slices.Clone(models)
	for i := range out {
		out[i].Default = i == selected
	}
	// Environment beats --effort, which beats the persisted default. An unknown or unsupported override is not
	// permission to display the lower-priority setting; keep the native catalog's effort in that case.
	effort := env["CLAUDE_CODE_EFFORT_LEVEL"]
	if effort == "" {
		effort = claudeCatalogEffortArg(args)
	}
	if effort == "" {
		// A saved per-model level beats effortLevel in the same file. Native matching also covers aliases, dated
		// snapshots and provider-specific IDs; keep its answer whenever that matching may apply.
		if value, present := settings["modelSettings"]; present {
			perModel, readable := value.(map[string]any)
			if !readable || len(perModel) > 0 {
				return out
			}
		}
		if configured, _ := settings["effortLevel"].(string); configured != effortAuto {
			for _, setting := range claudeSettings {
				if setting.Key == "effortLevel" && slices.Contains(setting.Options, configured) {
					effort = configured
				}
			}
		}
	}
	if slices.Contains(out[selected].Efforts, effort) {
		out[selected].DefaultEffort = effort
	}
	return out
}

// As modelArg does for --model, only flags before the prompt separator affect the invocation.
func claudeCatalogEffortArg(args []string) string {
	var effort string
	for i := 0; i < len(args); i++ {
		switch a := args[i]; {
		case a == "--":
			return effort
		case a == "--effort" && i+1 < len(args):
			i++
			effort = args[i]
		case strings.HasPrefix(a, "--effort="):
			effort = strings.TrimPrefix(a, "--effort=")
		}
	}
	return effort
}
