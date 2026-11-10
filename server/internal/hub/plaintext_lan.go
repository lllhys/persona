package hub

import (
	"errors"
	"net"
	"net/http"
	"strings"
)

// plaintextLoopbackOnly is security.plaintext_loopback_only: plaintext from anywhere but loopback, the LAN included,
// never reaches a route. A page (GET or HEAD outside the API, the bus and the manager endpoints) is sent to the same
// path on the TLS listener, so links built from an http public_url (/s/...) and bookmarks keep working, and the
// browser loads the app over HTTPS before any script reads a stored token. Everything else, health included, gets
// 403 tls_required,
// as public plaintext does in gate. Loopback stays plaintext: the built-in manager, the local assistant, an SSH
// tunnel and a reverse proxy on this host that terminates TLS (docs/operations/network.md).
func (h *Hub) plaintextLoopbackOnly(next http.Handler) http.Handler {
	if !h.cfg.Security.PlaintextLoopbackOnly {
		return next
	}
	tlsPort := portOf(h.cfg.Public.TLSListen)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.TLS != nil || h.sourceOf(r) == sourceLoopback || h.plainPeerRequest(r) {
			next.ServeHTTP(w, r)
			return
		}
		if (r.Method == http.MethodGet || r.Method == http.MethodHead) && isPagePath(r.URL.Path) && tlsPort != "" {
			http.Redirect(w, r, "https://"+net.JoinHostPort(hostOnly(r.Host), tlsPort)+r.URL.RequestURI(),
				http.StatusPermanentRedirect)
			return
		}
		h.securityError(w, r, http.StatusForbidden, "tls_required", 0)
	})
}

// isPagePath: a path the web app serves, not one that carries a credential: the API (the bus and manager join
// included) or a manager's install code.
func isPagePath(path string) bool {
	return !strings.HasPrefix(path, "/api/") && !strings.HasPrefix(path, "/i/")
}

// hostOnly is a Host header without its port, IPv6 brackets removed (net.JoinHostPort puts them back).
func hostOnly(host string) string {
	if h, _, err := net.SplitHostPort(host); err == nil {
		return h
	}
	return strings.TrimSuffix(strings.TrimPrefix(host, "["), "]")
}

var errPlaintextLoopbackNoTLS = errors.New("security.plaintext_loopback_only needs public.tls_listen: " +
	"without a TLS listener nothing but this machine could reach the hub")

// checkPlaintextLoopbackOnly is the start's check of the switch against the TLS listener.
func (c *Config) checkPlaintextLoopbackOnly() error {
	if c.Security.PlaintextLoopbackOnly && c.Public.TLSListen == "" {
		return errPlaintextLoopbackNoTLS
	}
	return nil
}
