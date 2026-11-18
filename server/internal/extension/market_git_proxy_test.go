package extension

import (
	"net/url"
	"strings"
	"testing"
)

func TestGitEnvironmentMarketplaceProxy(t *testing.T) {
	t.Setenv("HTTP_PROXY", "http://environment.example:3128")
	t.Setenv("HTTPS_PROXY", "http://environment.example:3128")
	t.Setenv("http_proxy", "http://environment.example:3128")
	t.Setenv("https_proxy", "http://environment.example:3128")
	t.Setenv("NO_PROXY", "market.example.com")
	t.Setenv("no_proxy", "market.example.com")
	proxy, _ := url.Parse("socks5://user:pass@proxy.example:1080")
	m := &Market{Spec: MarketSpec{Kind: "git", URL: "https://git.example.com/market.git"}, UpstreamProxy: proxy}
	env := map[string]string{}
	for _, entry := range m.gitEnvironment() {
		key, value, _ := strings.Cut(entry, "=")
		env[key] = value
	}
	for _, key := range []string{"HTTP_PROXY", "HTTPS_PROXY", "http_proxy", "https_proxy"} {
		if env[key] != "socks5://user:pass@proxy.example:1080" {
			t.Errorf("%s = %q", key, env[key])
		}
	}
	for _, key := range []string{"NO_PROXY", "no_proxy"} {
		if env[key] != "" {
			t.Errorf("%s = %q", key, env[key])
		}
	}
	if env["GIT_TERMINAL_PROMPT"] != "0" || env["GIT_ASKPASS"] != "/bin/false" {
		t.Fatalf("git prompts are not disabled: %v", env)
	}

	m.Spec.URL = "ssh://git.example.com/market.git"
	for _, entry := range m.gitEnvironment() {
		if entry == "HTTPS_PROXY=socks5://user:pass@proxy.example:1080" {
			t.Fatal("an HTTP proxy was applied to an SSH clone")
		}
	}
}
