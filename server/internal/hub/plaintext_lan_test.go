package hub

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

// With security.plaintext_loopback_only, plaintext from the LAN (a trusted range included) never reaches a route: a
// page is sent to the same path on the TLS listener, anything else is 403 tls_required. Loopback and TLS are as before.
func TestPlaintextLoopbackOnly(t *testing.T) {
	for _, tc := range []struct {
		name, method, path, remote, host string
		tls                              bool
		status                           int
		code, location                   string
	}{
		{"LAN api", "GET", "/api/v1/machines", "[fd00::5]:1", "hub.lan:18660", false, 403, "tls_required", ""},
		{"LAN health", "GET", "/api/v1/health", "[fd00::5]:1", "hub.lan:18660", false, 403, "tls_required", ""},
		{"LAN ws", "GET", "/api/v1/ws?token=x", "[fd00::5]:1", "hub.lan:18660", false, 403, "tls_required", ""},
		{"LAN bus", "GET", "/api/v1/bus", "[fd00::5]:1", "hub.lan:18660", false, 403, "tls_required", ""},
		{"LAN install code", "GET", "/i/abc", "[fd00::5]:1", "hub.lan:18660", false, 403, "tls_required", ""},
		{"LAN post page", "POST", "/s/mac/1", "[fd00::5]:1", "hub.lan:18660", false, 403, "tls_required", ""},
		{"trusted api", "GET", "/api/v1/machines", "192.0.2.1:1", "hub.lan:18660", false, 403, "tls_required", ""},
		{"public api", "GET", "/api/v1/machines", "198.51.100.1:1", "hub.lan:18660", false, 403, "tls_required", ""},
		{"LAN session link", "GET", "/s/mac/abc?x=1", "[fd00::5]:1", "hub.lan:18660", false, 308, "",
			"https://hub.lan:18669/s/mac/abc?x=1"},
		{"LAN root, no port", "HEAD", "/", "[fd00::5]:1", "hub.lan", false, 308, "", "https://hub.lan:18669/"},
		{"LAN IPv6 literal", "GET", "/todos", "[fd00::5]:1", "[fd00::1]:18660", false, 308, "",
			"https://[fd00::1]:18669/todos"},
		{"loopback api", "GET", "/api/v1/machines", "127.0.0.1:1", "127.0.0.1:18660", false, 200, "", ""},
		{"TLS LAN api", "GET", "/api/v1/machines", "[fd00::5]:1", "hub.lan:18669", true, 200, "", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newTestHub(t)
			h.cfg.Public.TLSListen = ":18669"
			h.cfg.Security = SecurityConfig{TrustedCIDRs: []string{"192.0.2.0/24"}, PlaintextLoopbackOnly: true}
			r := securityRequestHost(h, tc.method, tc.path, h.cfg.Token, tc.remote, tc.host, tc.tls)
			if tc.status == 308 {
				if r.Code != 308 || r.Header().Get("Location") != tc.location {
					t.Fatalf("response %d Location %q, want 308 %q", r.Code, r.Header().Get("Location"), tc.location)
				}
				return
			}
			requireCode(t, r, tc.status, tc.code)
		})
	}
}

// Off (the default), plaintext from the LAN works as before.
func TestPlaintextLoopbackOnlyOff(t *testing.T) {
	h := newTestHub(t)
	h.cfg.Public.TLSListen = ":18669"
	requireCode(t, securityRequestHost(h, "GET", "/api/v1/machines", h.cfg.Token, "[fd00::5]:1", "hub.lan:18660", false), 200, "")
}

func TestPlaintextLoopbackOnlyNeedsTLS(t *testing.T) {
	cfg := defaultConfig()
	cfg.Security.PlaintextLoopbackOnly = true
	cfg.Public.TLSListen = ""
	if err := cfg.checkPlaintextLoopbackOnly(); !errors.Is(err, errPlaintextLoopbackNoTLS) {
		t.Fatalf("err %v, want errPlaintextLoopbackNoTLS", err)
	}
	cfg.Public.TLSListen = "192.0.2.1:18669"
	if err := cfg.checkPlaintextLoopbackOnly(); err != nil {
		t.Fatal(err)
	}
}

// securityRequestHost is securityRequest with the Host header a client's URL gives.
func securityRequestHost(h *Hub, method, path, token, remote, host string, tlsOn bool) *httptest.ResponseRecorder {
	routes := h.routes()
	return securityRequest(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.Host = host
		routes.ServeHTTP(w, r)
	}), method, path, token, "", remote, tlsOn)
}
