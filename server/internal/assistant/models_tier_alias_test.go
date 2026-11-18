package assistant

import (
	"context"
	"errors"
	"testing"

	"github.com/nwverse/persona/server/internal/i18n"
	"github.com/nwverse/persona/server/internal/proto"
)

// A Claude Code tier alias stored as an API purpose's model is sent as the model its launcher's tier field names, and
// refused when the connection names none (#9165): a gateway has no model called "sonnet".
func TestAPIConfigResolvesTierAlias(t *testing.T) {
	s, _, _ := modelService(t)
	key, err := s.sealKey("synthetic-key")
	if err != nil {
		t.Fatal(err)
	}
	tiers := storedLauncher{ManagedLauncher: proto.ManagedLauncher{Name: "tiers", Kind: "launcher", API: "messages",
		BaseURL: "https://gateway.example.com", Model: "gw/claude-default",
		OpusModel: "gw/claude-opus-5-5", SonnetModel: "gw/claude-sonnet-5-5", HaikuModel: "gw/claude-haiku-5-5",
		FableModel: "gw/claude-fable-5"}, KeySealed: key}
	bare := storedLauncher{ManagedLauncher: proto.ManagedLauncher{Name: "bare", Kind: "api", API: "messages",
		BaseURL: "https://other.example.com", Model: "sonnet"}, KeySealed: key}
	loop := storedLauncher{ManagedLauncher: proto.ManagedLauncher{Name: "loop", Kind: "launcher", API: "messages",
		BaseURL: "https://loop.example.com", Model: "m", SonnetModel: "opus"}, KeySealed: key}
	small := storedLauncher{ManagedLauncher: proto.ManagedLauncher{Name: "small", Kind: "launcher", API: "messages",
		BaseURL: "https://small.example.com", Model: "m", SmallModel: "gw/fast"}, KeySealed: key}
	aliasDefault := storedLauncher{ManagedLauncher: proto.ManagedLauncher{Name: "aliasdefault", Kind: "launcher", API: "messages",
		BaseURL: "https://alias.example.com", Model: "sonnet", SonnetModel: "gw/claude-sonnet-5-5"}, KeySealed: key}
	putLaunchers(t, s, tiers, bare, loop, small, aliasDefault)

	for _, tc := range []struct {
		name    string
		b       ModelBinding
		want    string
		refused bool
	}{
		{name: "sonnet", b: ModelBinding{Provider: "launcher.tiers", Model: "sonnet"}, want: "gw/claude-sonnet-5-5"},
		{name: "case and 1m suffix", b: ModelBinding{Provider: "launcher.tiers", Model: "Opus[1m]"}, want: "gw/claude-opus-5-5"},
		{name: "haiku", b: ModelBinding{Provider: "launcher.tiers", Model: "haiku"}, want: "gw/claude-haiku-5-5"},
		{name: "fable", b: ModelBinding{Provider: "launcher.tiers", Model: "fable"}, want: "gw/claude-fable-5"},
		{name: "best runs opus", b: ModelBinding{Provider: "launcher.tiers", Model: "best"}, want: "gw/claude-opus-5-5"},
		{name: "opusplan runs opus", b: ModelBinding{Provider: "launcher.tiers", Model: "opusplan"}, want: "gw/claude-opus-5-5"},
		{name: "haiku falls back to the small model", b: ModelBinding{Provider: "launcher.small", Model: "haiku"}, want: "gw/fast"},
		{name: "launcher default is a resolvable alias", b: ModelBinding{Provider: "launcher.aliasdefault"}, want: "gw/claude-sonnet-5-5"},
		{name: "a real ID is left alone", b: ModelBinding{Provider: "launcher.tiers", Model: "claude-sonnet-5-5"}, want: "claude-sonnet-5-5"},
		{name: "the launcher's own model", b: ModelBinding{Provider: "launcher.tiers"}, want: "gw/claude-default"},
		{name: "launcher default is an alias with no tier field", b: ModelBinding{Provider: "launcher.bare"}, refused: true},
		{name: "no tier field", b: ModelBinding{Provider: "launcher.bare", Model: "haiku"}, refused: true},
		{name: "tier field is itself an alias", b: ModelBinding{Provider: "launcher.loop", Model: "sonnet"}, refused: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, err := s.apiConfig(context.Background(), tc.b)
			if tc.refused {
				var m *i18n.Message
				if !errors.As(err, &m) || m.Key != "assistant-models.err_tier_alias_unresolved" {
					t.Fatalf("model %q: err = %v, want the tier alias refusal", c.Model, err)
				}
				if s.modelUsable(context.Background(), tc.b) {
					t.Fatal("an unresolvable tier alias counts as usable")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if c.Model != tc.want {
				t.Fatalf("model = %q, want %q", c.Model, tc.want)
			}
			if !s.modelUsable(context.Background(), tc.b) {
				t.Fatal("a resolved binding is not usable")
			}
		})
	}
}

// A launcher the manager cannot call runs its CLI on a computer, whose node resolves the alias itself (launcherenv.go).
func TestAPIConfigLeavesTierAliasForCLIRun(t *testing.T) {
	s, _, _ := modelService(t)
	c, err := s.apiConfig(context.Background(), ModelBinding{Provider: "launcher.claude", Model: "sonnet"})
	if err != nil {
		t.Fatal(err)
	}
	if c.RunLauncher != "claude" || c.Model != "sonnet" {
		t.Fatalf("run=%q model=%q, want the alias passed to the CLI as is", c.RunLauncher, c.Model)
	}
}
