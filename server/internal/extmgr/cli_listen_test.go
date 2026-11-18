package extmgr

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"runtime"
	"strings"
	"testing"
)

// The extension command reaches a hub whose listen names one of this machine's addresses other than loopback (as
// "<LAN address>:18660" in hub.json) where it listens, not on 127.0.0.1 where nothing answers, and does not take it
// for a hub that is not running; from loopback, which the hub wants for installs from a directory (#9182).
func TestExtensionCLIDialsTheListenAddress(t *testing.T) {
	var ip netip.Addr
	addrs, _ := net.InterfaceAddrs()
	for _, a := range addrs {
		if n, ok := a.(*net.IPNet); ok {
			if own, ok := netip.AddrFromSlice(n.IP); ok && own.Unmap().Is4() && !own.IsLoopback() && !own.IsLinkLocalUnicast() {
				ip = own.Unmap()
				break
			}
		}
	}
	if !ip.IsValid() {
		t.Skip("this machine has no IPv4 address other than loopback")
	}
	ln, err := net.Listen("tcp", net.JoinHostPort(ip.String(), "0"))
	if err != nil {
		t.Skip("cannot listen on ", ip, ": ", err)
	}
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if from, _ := netip.ParseAddrPort(r.RemoteAddr); runtime.GOOS == "linux" && !from.Addr().Unmap().IsLoopback() {
			http.Error(w, `{"error":"the master token over loopback only"}`, http.StatusForbidden)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"extensions":[]}` + "\n"))
	}))
	srv.Listener = ln
	srv.Start()
	defer srv.Close()
	c := &extensionClient{cfg: &Config{Listen: ln.Addr().String(), Token: "t"}}
	var out map[string]any
	if err := c.do(context.Background(), http.MethodGet, "/api/v1/extensions", nil, &out); err != nil {
		t.Fatalf("extension command: %v", err)
	}
	var logs strings.Builder
	if err := c.stream(context.Background(), "/api/v1/extensions/demo/logs", &logs); err != nil || !strings.Contains(logs.String(), "extensions") {
		t.Fatalf("extension logs: %q, %v", logs.String(), err)
	}
}
