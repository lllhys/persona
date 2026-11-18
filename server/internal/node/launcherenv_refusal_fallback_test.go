package node

import (
	"context"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/nwverse/persona/server/internal/proto"
)

// refusalFallbackOff is the variable that keeps Claude Code from moving a refused reply to a fallback model of its own.
const refusalFallbackOff = "CLAUDE_CODE_DISABLE_REFUSAL_FALLBACK"

// envEntries is every value env (KEY=value entries) gives k, in order: more than one would leave it to the reader.
func envEntries(env []string, k string) []string {
	var out []string
	for _, kv := range env {
		if v, ok := strings.CutPrefix(kv, k+"="); ok {
			out = append(out, v)
		}
	}
	return out
}

// lackingList is a model list without Claude Code's fallback models, servingList one with all of them (the gateway's
// own model besides).
var (
	lackingList = []proto.ProviderModel{{ID: "m1"}}
	servingList = []proto.ProviderModel{{ID: "m1"}, {ID: "claude-opus-4-8"}, {ID: "claude-opus-5"}, {ID: "claude-sonnet-5"}}
)

// The models Claude Code 2.1.322 was seen falling back to: a version that moves elsewhere changes refusalFallbackModels
// and this list together.
func TestRefusalFallbackModelsAsObserved(t *testing.T) {
	want := []string{"claude-opus-4-8", "claude-opus-5", "claude-sonnet-5"}
	if got := strings.Fields(refusalFallbackModels); !slices.Equal(got, want) {
		t.Fatalf("refusalFallbackModels = %q, want %q", got, want)
	}
}

// Claude Code's automatic switch after a refusal goes to a model ID of its own, which a gateway may not serve: a
// gateway-shaped provider that is not Anthropic's own API turns it off when its model list lacks one of those models.
// No list, or one with all of them (compared trimmed, in any case, without "[1m]"), keeps Claude Code's default, and
// the launcher's own environment, applied last, wins either way (#7319).
func TestProviderEnvTurnsRefusalFallbackOffForAnUnservedFallback(t *testing.T) {
	const gw = "https://gw.example.com"
	gateway := proto.ManagedLauncher{Name: "gw", Kind: "api", BaseURL: gw, APIKey: "sk-test-1", Model: "m1", Models: lackingList}
	with := func(f func(*proto.ManagedLauncher)) proto.ManagedLauncher {
		l := gateway
		f(&l)
		return l
	}
	listing := func(ids ...string) func(*proto.ManagedLauncher) {
		return func(l *proto.ManagedLauncher) {
			l.Models = nil
			for _, id := range ids {
				l.Models = append(l.Models, proto.ProviderModel{ID: id})
			}
		}
	}
	for _, tc := range []struct {
		name  string
		l     proto.ManagedLauncher
		value string // "-" for unset
	}{
		{"a list lacking all three", gateway, "1"},
		{"a list lacking claude-opus-4-8", with(listing("m1", "claude-opus-5", "claude-sonnet-5")), "1"},
		{"a list lacking claude-opus-5", with(listing("m1", "claude-opus-4-8", "claude-sonnet-5")), "1"},
		{"a list lacking claude-sonnet-5", with(listing("m1", "claude-opus-4-8", "claude-opus-5")), "1"},
		{"a list naming them only with a vendor prefix or a date", with(listing("anthropic/claude-opus-4-8", "claude-opus-5-20260101", "claude-sonnet-5")), "1"},
		{"a list with all three", with(func(l *proto.ManagedLauncher) { l.Models = servingList }), "-"},
		{"a list of only the three", with(listing("claude-sonnet-5", "claude-opus-5", "claude-opus-4-8")), "-"},
		{"a list with all three marked [1m] and in other cases", with(listing("m1", "Claude-Opus-4-8[1m]", " claude-opus-5 ", "CLAUDE-SONNET-5[1M]")), "-"},
		{"no list", with(func(l *proto.ManagedLauncher) { l.Models = nil }), "-"},
		{"an empty list", with(func(l *proto.ManagedLauncher) { l.Models = []proto.ProviderModel{} }), "-"},
		{"a list of blank IDs", with(listing(" ", "")), "-"},
		{"a gateway with a path and /v1", with(func(l *proto.ManagedLauncher) { l.BaseURL = gw + "/prefix/v1/" }), "1"},
		{"a gateway shaped by its list, not the kind", proto.ManagedLauncher{Name: "gw", BaseURL: gw, Model: "m1", Models: lackingList}, "1"},
		{"a host that only starts like the official one", with(func(l *proto.ManagedLauncher) { l.BaseURL = "https://api.anthropic.com.gw.example.com" }), "1"},
		{"the official API", with(func(l *proto.ManagedLauncher) { l.BaseURL = "https://api.anthropic.com" }), "-"},
		{"the official API in capitals with /v1", with(func(l *proto.ManagedLauncher) { l.BaseURL = " https://API.Anthropic.com/v1/ " }), "-"},
		{"no base URL", with(func(l *proto.ManagedLauncher) { l.BaseURL = "" }), "-"},
		// A launcher from before #2455 has no model list (a list would make it provider-shaped), so the no-list rule,
		// not the providerShaped guard, is what keeps its environment as it always was.
		{"a launcher from before #2455, which has no list", proto.ManagedLauncher{Name: "gw", BaseURL: gw, APIKey: "sk-test-1", Model: "m1"}, "-"},
		{"its own environment empties it", with(func(l *proto.ManagedLauncher) { l.Env = refusalFallbackOff + "=" }), ""},
		{"its own environment empties it in quotes", with(func(l *proto.ManagedLauncher) { l.Env = "export " + refusalFallbackOff + `=""` }), ""},
		{"its own environment sets it with all three listed", with(func(l *proto.ManagedLauncher) {
			l.Models, l.Env = servingList, refusalFallbackOff+"=1"
		}), "1"},
		{"its own environment sets it with no list", with(func(l *proto.ManagedLauncher) { l.Models, l.Env = nil, refusalFallbackOff+"=1" }), "1"},
		{"its own environment sets it on the official API", with(func(l *proto.ManagedLauncher) {
			l.BaseURL, l.Env = "https://api.anthropic.com", refusalFallbackOff+"=1"
		}), "1"},
	} {
		for _, main := range []bool{false, true} {
			got, set := providerEnv(tc.l, "", "", main)[refusalFallbackOff]
			if tc.value == "-" {
				if set {
					t.Errorf("%s (main %v): %s=%q, want unset", tc.name, main, refusalFallbackOff, got)
				}
				continue
			}
			if !set || got != tc.value {
				t.Errorf("%s (main %v): %s=%q (set %v), want %q", tc.name, main, refusalFallbackOff, got, set, tc.value)
			}
		}
	}
}

// refusalLaunchers are the providers the start-path tests below run: "gw" lacks the fallback models, "emptied" lacks
// them too but empties the variable in its own environment, "listed" serves them all and "official" is Anthropic's API.
func refusalLaunchers() []proto.ManagedLauncher {
	return []proto.ManagedLauncher{
		{Name: "gw", Kind: "api", BaseURL: "https://gw.example.com", APIKey: "sk-test-1", Model: "m1", Models: lackingList},
		{Name: "emptied", Kind: "api", BaseURL: "https://gw.example.com", APIKey: "sk-test-1", Model: "m1", Models: lackingList, Env: refusalFallbackOff + "="},
		{Name: "listed", Kind: "api", BaseURL: "https://gw.example.com", APIKey: "sk-test-1", Model: "m1", Models: servingList},
		{Name: "official", Kind: "api", BaseURL: "https://api.anthropic.com", APIKey: "sk-test-1", Model: "m1", Models: lackingList},
	}
}

// What `personad launch` gets for a provider (launcher.env) and does with it: for one whose list lacks a fallback model
// the variable reaches the claude it execs, a Persona conversation's too (PERSONA_ASSISTANT=1), after the shell's, and
// its settings env, which Claude Code applies over the process environment. An empty value from the launcher's own
// environment replaces the shell's, so claude sees it empty, once. A provider that sets none (one that lists every
// fallback model, Anthropic's API) leaves the shell's alone.
func TestRefusalFallbackReachesPersonadLaunch(t *testing.T) {
	n := agentNode(t, false)
	n.setManaged(refusalLaunchers())
	shell := []string{"PATH=/usr/bin", refusalFallbackOff + "=0"}
	for _, tc := range []struct {
		launcher, want string // want "-": what the shell gave stays
		replaced       bool   // the shell's value is dropped, not only followed
	}{
		{"gw", "1", false},
		{"emptied", "", true},
		{"listed", "-", false},
		{"official", "-", false},
	} {
		resp := n.launcherEnv(tc.launcher)
		if e, _ := resp["error"].(string); e != "" {
			t.Fatalf("%s: %s", tc.launcher, e)
		}
		env, _ := resp["env"].([]string)
		clear, _ := resp["clear"].([]string)
		if tc.want == "-" {
			if got := envEntries(env, refusalFallbackOff); len(got) != 0 {
				t.Errorf("%s: launch env sets %s=%q", tc.launcher, refusalFallbackOff, got)
			}
			c, e := launchEnvFor(clear, env, false)
			if got := envEntries(append(withoutEnv(shell, c), e...), refusalFallbackOff); len(got) != 1 || got[0] != "0" {
				t.Errorf("%s: the shell's %s became %q", tc.launcher, refusalFallbackOff, got)
			}
			if _, set := managedSettingsEnv(mustManaged(t, n, tc.launcher))[refusalFallbackOff]; set {
				t.Errorf("%s: settings env sets %s", tc.launcher, refusalFallbackOff)
			}
			continue
		}
		for _, assistant := range []bool{false, true} {
			c, e := launchEnvFor(clear, env, assistant)
			final := append(withoutEnv(shell, c), e...) // as Launch builds it
			got := envEntries(final, refusalFallbackOff)
			if len(got) == 0 || got[len(got)-1] != tc.want || tc.replaced && len(got) != 1 {
				t.Errorf("%s (assistant %v): claude gets %s=%q, want %q last (only: %v)", tc.launcher, assistant, refusalFallbackOff, got, tc.want, tc.replaced)
			}
		}
		if got, set := managedSettingsEnv(mustManaged(t, n, tc.launcher))[refusalFallbackOff]; !set || got != tc.want {
			t.Errorf("%s: settings env %s=%q (set %v), want %q", tc.launcher, refusalFallbackOff, got, set, tc.want)
		}
	}
}

func mustManaged(t *testing.T, n *Node, name string) proto.ManagedLauncher {
	t.Helper()
	l, ok := n.managedLauncher(name)
	if !ok {
		t.Fatalf("no managed launcher %s", name)
	}
	return l
}

// The settings files Persona generates carry it: an Agent's start (claude-settings, with Persona's hooks or alone), a
// start whose hooks are a plugin (the env alone), and a Persona conversation's file, which assistantCompactionSettings
// builds over the launcher's for the main conversation and every other. A provider that empties it pins it empty, over
// the user's settings.json.
func TestRefusalFallbackInGeneratedSettings(t *testing.T) {
	n := agentNode(t, false)
	n.setManaged(refusalLaunchers())
	writeFile(t, filepath.Join(n.dir, "claude-settings.json"), `{"hooks":{"Stop":[]}}`)
	profile := filepath.Join(n.home, "profile")
	writeFile(t, filepath.Join(profile, "settings.json"), `{"env":{"`+refusalFallbackOff+`":"1"}}`)
	cwd := t.TempDir()
	read := func(t *testing.T, path string) map[string]any {
		t.Helper()
		s, err := readLauncherSettings(path, cwd, n.home)
		if err != nil {
			t.Fatal(err)
		}
		return s
	}
	for _, tc := range []struct {
		launcher, want string // "-" for unset
	}{
		{"gw", "1"},
		{"emptied", ""},
		{"listed", "-"},
		{"official", "-"},
	} {
		check := func(what string, s map[string]any) {
			t.Helper()
			env, _ := s["env"].(map[string]any)
			got, set := env[refusalFallbackOff]
			if tc.want == "-" {
				if set {
					t.Errorf("%s, %s: %s=%v, want unset", tc.launcher, what, refusalFallbackOff, got)
				}
				return
			}
			if !set || got != tc.want {
				t.Errorf("%s, %s: %s=%v (set %v), want %q", tc.launcher, what, refusalFallbackOff, got, set, tc.want)
			}
		}
		agent, hooks := n.claudeStartSettings(context.Background(), tc.launcher, "claude", cwd)
		if hooks != "" {
			t.Fatalf("%s: hooks came as a plugin: %s", tc.launcher, hooks)
		}
		s := read(t, agent)
		if s["hooks"] == nil {
			t.Errorf("%s: the Agent's settings lost Persona's hooks: %v", tc.launcher, s)
		}
		check("an Agent's settings", s)
		l := mustManaged(t, n, tc.launcher)
		envOnly, err := n.mergeManagedSettings(tc.launcher, l, "", profile)
		if err != nil {
			t.Fatal(err)
		}
		check("the env-only settings", read(t, envOnly))
		for _, main := range []bool{true, false} {
			settings, _ := n.claudeStartSettings(context.Background(), tc.launcher, "assistant", cwd, profile)
			c := n.assistantCompactionPolicy(tc.launcher, cwd, settings, modelExecution{Model: "m1", IsMain: main})
			path, err := n.assistantCompactionSettings("11111111-2222-3333-4444-555555555555", cwd, settings, c)
			if err != nil {
				t.Fatal(err)
			}
			check("a Persona conversation's settings (main "+map[bool]string{true: "yes", false: "no"}[main]+")", read(t, path))
		}
	}
}
