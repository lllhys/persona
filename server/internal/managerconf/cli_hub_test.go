package managerconf

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"runtime"
	"testing"
)

// The hub's own commands call the address listen names when this machine has it, loopback otherwise: a name is not
// resolved, and an address this machine does not have (a DHCP lease that moved on) never gets the token (#9182).
func TestCLIHubURL(t *testing.T) {
	saved := interfaceAddrs
	t.Cleanup(func() { interfaceAddrs = saved })
	interfaceAddrs = func() ([]net.Addr, error) {
		return []net.Addr{
			&net.IPNet{IP: net.IPv4(127, 0, 0, 1), Mask: net.CIDRMask(8, 32)},
			&net.IPNet{IP: net.ParseIP("192.0.2.10"), Mask: net.CIDRMask(24, 32)},
			&net.IPNet{IP: net.ParseIP("2001:db8::10"), Mask: net.CIDRMask(64, 128)},
		}, nil
	}
	for listen, want := range map[string]string{
		":18660":                 "http://127.0.0.1:18660",
		"0.0.0.0:18660":          "http://127.0.0.1:18660",
		"[::]:18660":             "http://127.0.0.1:18660",
		"127.0.0.1:18660":        "http://127.0.0.1:18660",
		"[::1]:18660":            "http://[::1]:18660",
		"192.0.2.10:18660":       "http://192.0.2.10:18660",
		"[::ffff:192.0.2.10]:1":  "http://192.0.2.10:1",
		"[2001:db8::10]:18660":   "http://[2001:db8::10]:18660",
		"192.0.2.11:18660":       "http://127.0.0.1:18660", // not this machine's
		"[2001:db8::11]:18660":   "http://127.0.0.1:18660",
		"[fe80::1%eth0]:18660":   "http://127.0.0.1:18660",
		"hub.example.com:18660":  "http://127.0.0.1:18660", // a name is not resolved
		"localhost:18660":        "http://127.0.0.1:18660",
		"not an address":         "http://127.0.0.1:18660",
		"192.0.2.10:18660:extra": "http://127.0.0.1:18660",
	} {
		if got := CLIHubURL(listen); got != want {
			t.Errorf("CLIHubURL(%q) = %q, want %q", listen, got, want)
		}
	}
	interfaceAddrs = func() ([]net.Addr, error) { return nil, &net.OpError{Op: "route"} }
	if got := CLIHubURL("192.0.2.10:18660"); got != "http://127.0.0.1:18660" {
		t.Errorf("without the interface list: %q", got)
	}
}

// ownAddressForTest is one of this machine's addresses other than loopback, or the test is skipped.
func ownAddressForTest(t *testing.T) netip.Addr {
	t.Helper()
	addrs, _ := net.InterfaceAddrs()
	for _, a := range addrs {
		if n, ok := a.(*net.IPNet); ok {
			if ip, ok := netip.AddrFromSlice(n.IP); ok && ip.Unmap().Is4() && !ip.IsLoopback() && !ip.IsLinkLocalUnicast() {
				return ip.Unmap()
			}
		}
	}
	t.Skip("this machine has no IPv4 address other than loopback")
	return netip.Addr{}
}

// A command reaches a hub listening on one of this machine's own addresses from loopback, which the hub wants for
// the master token from its own machine (#9182).
func TestCLIDialComesFromLoopback(t *testing.T) {
	ip := ownAddressForTest(t)
	ln, err := net.Listen("tcp", net.JoinHostPort(ip.String(), "0"))
	if err != nil {
		t.Skip("cannot listen on ", ip, ": ", err)
	}
	from := make(chan string, 1)
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		from <- r.RemoteAddr
	}))
	srv.Listener = ln
	srv.Start()
	defer srv.Close()
	base := CLIHubURL(ln.Addr().String())
	if base != "http://"+ln.Addr().String() {
		t.Fatalf("CLIHubURL(%s) = %s", ln.Addr(), base)
	}
	transport := &http.Transport{Proxy: nil, DialContext: CLIDial}
	defer transport.CloseIdleConnections()
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, base+"/api/v1/health", nil)
	resp, err := (&http.Client{Transport: transport}).Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	remote, _ := netip.ParseAddrPort(<-from)
	if runtime.GOOS == "linux" && !remote.Addr().Unmap().IsLoopback() {
		t.Fatalf("the hub saw the command come from %s, not loopback", remote)
	}
	t.Logf("from %s", remote)
}
