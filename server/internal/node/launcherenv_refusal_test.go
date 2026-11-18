package node

import (
	"testing"

	"github.com/nwverse/persona/server/internal/proto"
)

func TestProviderEnvUsesEffectiveGatewayForRefusalFallback(t *testing.T) {
	for _, tc := range []struct {
		name, stored, override, want string
	}{
		{"custom gateway overrides official", "https://api.anthropic.com", "https://gw.example.com", "1"},
		{"official overrides custom gateway", "https://gw.example.com", "https://api.anthropic.com", ""},
		{"empty override uses official default", "https://gw.example.com", "", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			l := proto.ManagedLauncher{
				Name: "gw", Kind: "api", BaseURL: tc.stored, APIKey: "sk-test", Model: "m1",
				Models: []proto.ProviderModel{{ID: "m1"}}, Env: "ANTHROPIC_BASE_URL=" + tc.override,
			}
			env := providerEnv(l, "", "", false)
			if got := env[refusalFallbackOff]; got != tc.want {
				t.Fatalf("effective gateway %q: %s=%q, want %q", env["ANTHROPIC_BASE_URL"], refusalFallbackOff, got, tc.want)
			}
		})
	}
}
