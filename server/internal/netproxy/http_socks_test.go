package netproxy

import (
	"strings"
	"testing"
)

func TestParseHTTPOrSOCKS5(t *testing.T) {
	for raw, want := range map[string]string{
		"":                                      "",
		"http://proxy.example":                  "http://proxy.example:80",
		"http://user:pass@proxy.example:3128":   "http://user:pass@proxy.example:3128",
		"socks5://proxy.example":                "socks5://proxy.example:1080",
		"socks5://user:pass@proxy.example:1080": "socks5://user:pass@proxy.example:1080",
		"socks5://[fd00::20]":                   "socks5://[fd00::20]:1080",
	} {
		u, err := ParseHTTPOrSOCKS5(raw)
		if err != nil {
			t.Errorf("%q: %v", raw, err)
			continue
		}
		got := ""
		if u != nil {
			got = u.String()
		}
		if got != want {
			t.Errorf("%q: got %q, want %q", raw, got, want)
		}
	}
	for _, raw := range []string{
		"https://proxy.example:3128", "proxy.example:3128", "socks5://", "socks5://proxy.example:0",
		"socks5://proxy.example:65536", "socks5://proxy.example:port", "socks5://proxy.example:1080/path",
		"socks5://proxy.example:1080?x=1", "socks5://proxy.example:1080#fragment", "socks5://proxy.example:",
	} {
		if u, err := ParseHTTPOrSOCKS5(raw); err == nil || u != nil {
			t.Errorf("%q: accepted %v", raw, u)
		}
	}
	if _, err := ParseHTTPOrSOCKS5("socks5://user:s3cret@proxy.example:1080/path"); err == nil || strings.Contains(err.Error(), "s3cret") {
		t.Fatalf("an invalid proxy reveals its password: %v", err)
	}
}
