package netproxy

import (
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"
)

// ParseHTTPOrSOCKS5 reads an http:// or socks5:// proxy, with optional user:pass and a default port of 80 or 1080.
// It is for callers that support both schemes; Parse remains HTTP-only. Errors never reveal the proxy password.
func ParseHTTPOrSOCKS5(raw string) (*url.URL, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "http" {
		p, parseErr := Parse(raw)
		if parseErr != nil {
			return nil, fmt.Errorf("proxy must be http://[user:pass@]host:port or socks5://[user:pass@]host:port, got %q", redact(raw))
		}
		return p, nil
	}
	if u.Scheme != "socks5" || u.Hostname() == "" || (u.Path != "" && u.Path != "/") || u.RawQuery != "" || u.Fragment != "" || !validPort(u) {
		return nil, fmt.Errorf("proxy must be http://[user:pass@]host:port or socks5://[user:pass@]host:port, got %q", redact(raw))
	}
	if u.Port() == "" {
		u.Host = net.JoinHostPort(u.Hostname(), "1080")
	}
	return u, nil
}

func validPort(u *url.URL) bool {
	port := u.Port()
	if port == "" {
		return !strings.HasSuffix(u.Host, ":")
	}
	n, err := strconv.Atoi(port)
	return err == nil && n >= 1 && n <= 65535
}
