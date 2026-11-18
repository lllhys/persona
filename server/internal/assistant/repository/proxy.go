package repository

import (
	"context"
	"net"
	"net/http"
	"net/url"
	"strings"

	"github.com/nwverse/persona/server/internal/netproxy"
	"github.com/nwverse/persona/server/internal/pinneddial"
)

// Match the extension market's priority for the public GitHub API. Enterprise
// and GitLab endpoints use their own environment rules, including NO_PROXY.
func (c Checker) proxyFor(req *http.Request) (*url.URL, error) {
	if req.URL.Hostname() == "api.github.com" && strings.TrimSpace(c.GitHubProxy) != "" {
		return netproxy.Parse(c.GitHubProxy)
	}
	return http.ProxyFromEnvironment(req)
}

// contextDial and dialThroughProxy are pinneddial's (the remote image route dials the same way).
type contextDial = pinneddial.ContextDial

// The proxy is operator-controlled and may itself be on loopback. Only the
// destination is subject to repository trust rules, and the proxy receives its
// validated numeric address, never a hostname it could resolve differently.
func dialThroughProxy(ctx context.Context, upstream *url.URL, network, target string, dial contextDial) (net.Conn, error) {
	return pinneddial.ThroughProxy(ctx, upstream, network, target, dial)
}
