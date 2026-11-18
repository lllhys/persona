package node

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"testing"

	"github.com/nwverse/persona/server/internal/proto"
)

func configuredDefaultCatalog() []proto.AgentModel {
	return []proto.AgentModel{
		{ID: "default", Name: "Default (recommended)", Resolved: "claude-opus-5-5", Default: true},
		{ID: "fable", Name: "Fable alias", Resolved: "claude-fable-5-2[1m]"},
		{ID: "claude-fable-5-2[1m]", Name: "Fable", Efforts: []string{"low", "high", "max"}},
	}
}

func TestConfiguredClaudeModelDefaultPreservesNativeAlias(t *testing.T) {
	catalog := configuredDefaultCatalog()
	before := slices.Clone(catalog)
	got := configuredClaudeModelDefault(map[string]any{"model": "claude-fable-5-2[1m]", "effortLevel": "high"}, nil, nil, catalog)
	if got[0].Default || got[1].Default || !got[2].Default || got[2].DefaultEffort != "high" {
		t.Fatalf("configured default: %+v", got)
	}
	if got[0].ID != "default" || got[0].Resolved != "claude-opus-5-5" || got[0].Name != "Default (recommended)" {
		t.Fatalf("native recommendation was rewritten: %+v", got[0])
	}
	if !reflect.DeepEqual(catalog, before) {
		t.Fatal("changed the input catalog")
	}
	if !slices.Equal(got[2].Efforts, catalog[2].Efforts) {
		t.Fatal("invented model capabilities")
	}
}

func TestConfiguredClaudeModelDefaultLeavesUnknownOrAmbiguousDefaults(t *testing.T) {
	for _, test := range []struct {
		name     string
		settings map[string]any
		catalog  []proto.AgentModel
	}{
		{"no model", map[string]any{"effortLevel": "max"}, configuredDefaultCatalog()},
		{"unlisted model", map[string]any{"model": "not-listed"}, configuredDefaultCatalog()},
		{"invalid model", map[string]any{"model": "not a model"}, configuredDefaultCatalog()},
		{"competing env", map[string]any{"model": "fable", "env": map[string]any{"ANTHROPIC_MODEL": "opus"}}, configuredDefaultCatalog()},
		{"ambiguous aliases", map[string]any{"model": "same-model"}, []proto.AgentModel{{ID: "default", Resolved: "same-model", Default: true}, {ID: "alias", Resolved: "same-model"}}},
		{"duplicate IDs", map[string]any{"model": "same-model"}, []proto.AgentModel{{ID: "same-model", Default: true}, {ID: "same-model"}}},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := configuredClaudeModelDefault(test.settings, nil, nil, test.catalog); !reflect.DeepEqual(got, test.catalog) {
				t.Fatalf("uncertain default changed: %+v", got)
			}
		})
	}
}

func TestConfiguredClaudeModelDefaultUsesUniqueResolvedAliasAndSupportedEffort(t *testing.T) {
	catalog := []proto.AgentModel{{ID: "default", Resolved: "recommended", Default: true}, {ID: "chosen", Resolved: "configured-model", Efforts: []string{"low", "high"}, DefaultEffort: "low"}}
	got := configuredClaudeModelDefault(map[string]any{"model": "configured-model", "effortLevel": "max"}, nil, nil, catalog)
	if got[0].Default || !got[1].Default || got[1].DefaultEffort != "low" || got[1].Resolved != "configured-model" {
		t.Fatalf("resolved alias/unsupported effort: %+v", got)
	}
}

func TestConfiguredClaudeModelDefaultKeepsEffortPrecedence(t *testing.T) {
	for _, tc := range []struct {
		name, configured, inherited, own, want string
		args                                   []string
	}{
		{name: "configured", configured: "high", want: "high"},
		{name: "inherited environment", configured: "high", inherited: "low", want: "low"},
		{name: "settings environment", configured: "high", inherited: "high", own: "low", args: []string{"--effort", "max"}, want: "low"},
		{name: "command line", configured: "high", args: []string{"--effort=max"}, want: "max"},
		{name: "environment beats command line", configured: "high", inherited: "low", args: []string{"--effort", "max"}, want: "low"},
		{name: "unknown environment keeps native", configured: "high", inherited: "unknown", args: []string{"--effort", "max"}, want: "low"},
		{name: "unsupported environment keeps native", configured: "high", inherited: "xhigh", want: "low"},
		{name: "settings max ignored", configured: "max", want: "low"},
		{name: "settings auto ignored", configured: "auto", want: "low"},
		{name: "prompt flags ignored", configured: "high", args: []string{"--", "--effort", "max"}, want: "high"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			catalog := []proto.AgentModel{{ID: "default", Default: true}, {ID: "chosen", Efforts: []string{"low", "high", "max"}, DefaultEffort: "low"}}
			settings := map[string]any{"model": "chosen", "effortLevel": tc.configured}
			if tc.own != "" {
				settings["env"] = map[string]any{"CLAUDE_CODE_EFFORT_LEVEL": tc.own}
			}
			env := map[string]string{"CLAUDE_CODE_EFFORT_LEVEL": tc.inherited}
			got := configuredClaudeModelDefault(settings, env, tc.args, catalog)
			if got[0].Default || !got[1].Default || got[1].DefaultEffort != tc.want {
				t.Fatalf("effective default: %+v, want effort %s", got, tc.want)
			}
			if catalog[1].DefaultEffort != "low" || catalog[1].Default || !catalog[0].Default {
				t.Fatal("mutated the native catalog")
			}
		})
	}
}

func TestConfiguredClaudeModelDefaultKeepsHigherPriorityModels(t *testing.T) {
	for _, tc := range []struct {
		name string
		env  map[string]string
		args []string
	}{
		{"inherited environment", map[string]string{"ANTHROPIC_MODEL": "other"}, nil},
		{"command flag", nil, []string{"--model", "other"}},
		{"inline command flag", nil, []string{"--model=other"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			catalog := configuredDefaultCatalog()
			got := configuredClaudeModelDefault(map[string]any{"model": "claude-fable-5-2[1m]"}, tc.env, tc.args, catalog)
			if !reflect.DeepEqual(got, catalog) {
				t.Fatalf("a lower-priority setting replaced the native default: %+v", got)
			}
		})
	}
	// A command-line model wins over a competing environment model, as it does in the actual invocation.
	got := configuredClaudeModelDefault(map[string]any{"model": "claude-fable-5-2[1m]"},
		map[string]string{"ANTHROPIC_MODEL": "other"}, []string{"--model", "claude-fable-5-2[1m]"}, configuredDefaultCatalog())
	if got[0].Default || !got[2].Default {
		t.Fatalf("environment replaced the command-line model: %+v", got)
	}
}

func TestConfiguredClaudeModelDefaultKeepsPerModelEffort(t *testing.T) {
	for _, tc := range []struct {
		name     string
		perModel any
		env      map[string]string
		args     []string
		want     string
	}{
		{"saved level", map[string]any{"chosen": map[string]any{"effortLevel": "low"}}, nil, nil, "low"},
		{"canonical alias", map[string]any{"claude-fable-5-2": map[string]any{"effortLevel": "low"}}, nil, nil, "low"},
		{"empty map", map[string]any{}, nil, nil, "high"},
		{"unknown format", "unreadable", nil, nil, "low"},
		{"environment wins", map[string]any{"chosen": map[string]any{"effortLevel": "low"}}, map[string]string{"CLAUDE_CODE_EFFORT_LEVEL": "max"}, nil, "max"},
		{"command line wins", map[string]any{"chosen": map[string]any{"effortLevel": "low"}}, nil, []string{"--effort", "high"}, "high"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			catalog := []proto.AgentModel{{ID: "default", Default: true},
				{ID: "chosen", Resolved: "claude-fable-5-2[1m]", Efforts: []string{"low", "high", "max"}, DefaultEffort: "low"}}
			settings := map[string]any{"model": "chosen", "effortLevel": "high", "modelSettings": tc.perModel}
			got := configuredClaudeModelDefault(settings, tc.env, tc.args, catalog)
			if got[0].Default || !got[1].Default || got[1].DefaultEffort != tc.want {
				t.Fatalf("per-model effort changed: %+v, want %s", got, tc.want)
			}
		})
	}
}

func TestLauncherModelEnvironmentIncludesProfileAndUnset(t *testing.T) {
	home := t.TempDir()
	n := &Node{home: home}
	profile := filepath.Join(home, "profile")
	if err := os.MkdirAll(profile, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(profile, "settings.json"), []byte(`{"env":{"CLAUDE_CODE_EFFORT_LEVEL":"low"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	p := Probe{Agent: "claude", Shell: map[string]string{"CLAUDE_CONFIG_DIR": profile, "CLAUDE_CODE_EFFORT_LEVEL": "max", "ANTHROPIC_MODEL": "removed"},
		Env: map[string]string{"CLAUDE_CODE_EFFORT_LEVEL": "high"}, Unset: []string{"ANTHROPIC_MODEL"}}
	env, known := n.launcherModelEnvironment(p, home)
	if !known || env["CLAUDE_CODE_EFFORT_LEVEL"] != "low" || env["ANTHROPIC_MODEL"] != "" {
		t.Fatalf("effective profile environment: %v (known %v)", env, known)
	}
	if p.Shell["ANTHROPIC_MODEL"] != "removed" || p.Env["CLAUDE_CODE_EFFORT_LEVEL"] != "high" {
		t.Fatal("mutated the probe")
	}
	got := configuredClaudeModelDefault(map[string]any{"model": "chosen", "env": map[string]any{"CLAUDE_CODE_EFFORT_LEVEL": "max"}}, env, nil,
		[]proto.AgentModel{{ID: "chosen", Efforts: []string{"low", "max"}}})
	if got[0].DefaultEffort != "max" {
		t.Fatalf("explicit settings environment lost precedence: %+v", got)
	}
	p.Unset = append(p.Unset, "CLAUDE_CONFIG_DIR")
	if env, known := n.launcherModelEnvironment(p, home); !known || env["CLAUDE_CODE_EFFORT_LEVEL"] != "high" {
		t.Fatalf("unset config directory still read another profile: %v (known %v)", env, known)
	}
	if _, known := n.launcherModelEnvironment(Probe{Agent: "claude"}, home); known {
		t.Fatal("a probe without inherited environment was treated as complete")
	}
	p.Env["CLAUDE_CONFIG_DIR"] = "relative-profile"
	if _, known := n.launcherModelEnvironment(p, home); known {
		t.Fatal("a relative config directory was treated as complete")
	}
}

func TestClaudeModelDiscoveryProjectsOwnSettingsDefault(t *testing.T) {
	home := fakeCLIHome(t)
	n, _ := testNode(t)
	n.home = home
	n.dir = t.TempDir()
	settings := filepath.Join(home, "profile.json")
	if err := os.WriteFile(settings, []byte(`{"model":"claude-fable-5-2[1m]","effortLevel":"high"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	launcherCache.Lock()
	launcherCache.settings["mine"] = settings
	launcherCache.Unlock()
	cli := "#!/bin/sh\nread line\nprintf '%s\\n' '" +
		`{"type":"control_response","response":{"subtype":"success","request_id":"persona-models","response":{"models":[` +
		`{"value":"default","resolvedModel":"claude-opus-5-5","displayName":"Default (recommended)"},` +
		`{"value":"claude-fable-5-2[1m]","resolvedModel":"claude-fable-5-2[1m]","displayName":"Fable","supportsEffort":true,"supportedEffortLevels":["low","high","max"]}]}}}` + "'\n"
	if err := os.WriteFile(filepath.Join(home, "bin", "claude"), []byte(cli), 0o700); err != nil {
		t.Fatal(err)
	}
	got, err := n.listAgentModels(context.Background(), "mine", "claude")
	if err != nil || len(got) != 2 || got[0].Default || got[0].Resolved != "claude-opus-5-5" || !got[1].Default || got[1].DefaultEffort != "high" {
		t.Fatalf("discovery: %+v (%v)", got, err)
	}
	// The probe's differential Env excludes an inherited shell variable. That variable still beats effortLevel.
	t.Setenv("CLAUDE_CODE_EFFORT_LEVEL", "low")
	inherited, err := n.listAgentModels(context.Background(), "mine", "claude")
	if err != nil || !inherited[1].Default || inherited[1].DefaultEffort != "low" {
		t.Fatalf("inherited shell effort: %+v (%v)", inherited, err)
	}
	if err := os.WriteFile(filepath.Join(home, ".zshrc"), []byte("mine() { command claude --model claude-opus-5-5 \"$@\"; }\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	overridden, err := n.listAgentModels(context.Background(), "mine", "claude")
	if err != nil || !overridden[0].Default || overridden[1].Default {
		t.Fatalf("settings replaced a known command-line model: %+v (%v)", overridden, err)
	}
	// A plain Claude launcher has no own settings; account recommendations stay intact.
	plain, err := n.listAgentModels(context.Background(), "claude", "claude")
	if err != nil || !plain[0].Default || plain[1].Default {
		t.Fatalf("plain Claude: %+v (%v)", plain, err)
	}
}
