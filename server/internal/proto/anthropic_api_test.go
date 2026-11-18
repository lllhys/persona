package proto

import "testing"

// Only a Base URL whose host is Anthropic's own API counts: a relay under another host, one whose name only starts
// like it, or an address that does not parse is not it.
func TestAnthropicAPI(t *testing.T) {
	for base, want := range map[string]bool{
		"https://api.anthropic.com":                    true,
		" https://API.Anthropic.com/v1/ ":              true,
		"https://api.anthropic.com:443/v1":             true,
		"https://relay.example.net":                    false,
		"https://api.anthropic.com.relay.example.net":  false,
		"https://relay.example.net/api.anthropic.com":  false,
		"https://user@relay.example.net?api.anthropic": false,
		"":                     false,
		"://api.anthropic.com": false,
	} {
		if got := AnthropicAPI(base); got != want {
			t.Errorf("AnthropicAPI(%q) = %v, want %v", base, got, want)
		}
	}
}
