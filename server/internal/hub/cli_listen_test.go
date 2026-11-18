package hub

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"runtime"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/i18n"
)

// oneAddressHub is a hub whose listen names one of this machine's addresses other than loopback, as a hub.json
// listen of "<LAN address>:18660" makes it: nothing answers on 127.0.0.1 at its port. Like the hub (pairing codes,
// admin, peers), it takes the master token from this machine's commands over loopback only (on Linux, where a
// command dials from loopback: managerconf.CLIDial).
func oneAddressHub(t *testing.T, h http.Handler) *Config {
	t.Helper()
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
		if runtime.GOOS == "linux" && !remoteIP(r).IsLoopback() {
			writeErrCode(w, http.StatusForbidden, "forbidden", fmt.Errorf("the master token over loopback only, not from %s", r.RemoteAddr))
			return
		}
		h.ServeHTTP(w, r)
	}))
	srv.Listener = ln
	srv.Start()
	t.Cleanup(srv.Close)
	return &Config{Listen: ln.Addr().String(), Token: "t"}
}

// The hub's own commands reach a hub whose listen names one address of this machine where it listens, not on
// 127.0.0.1 where nothing answers, so they do not take it for a hub that is not running; and from loopback, so the
// hub takes their master token (#9182).
func TestCLIDialsTheListenAddress(t *testing.T) {
	c := oneAddressHub(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v1/auth/pair-codes":
			fmt.Fprintf(w, `{"token":"pair","url":"https://hub.example.com","expires_at":%q}`, time.Now().Add(time.Hour).Format(time.RFC3339))
		case "/api/v1/managers":
			_, _ = w.Write([]byte(`{"managers":[{"id":"assistant","online":true,"deploy":"service"}]}`))
		default:
			_, _ = w.Write([]byte(`{}`))
		}
	}))
	ctx := context.Background()
	if err := c.adminCall(ctx, http.MethodGet, "/api/v1/admin/cli/peers", nil, nil); err != nil {
		t.Errorf("peer CLI: %v", err)
	}
	if resp, err := adminLoopbackPost(ctx, c, "/api/v1/admin/reload"); err != nil || resp.StatusCode != http.StatusOK {
		t.Errorf("admin CLI: %v, %v", resp, err)
	} else {
		resp.Body.Close()
	}
	if p, err := c.requestPairCodeWith(ctx, nil); err != nil || p.Token != "pair" {
		t.Errorf("pair code: %+v, %v", p, err)
	}
	e := &setupEnv{l: i18n.En}
	if !e.hubAnswers(ctx, c) {
		t.Error("setup: the hub does not answer")
	}
	if !e.builtinOnline(ctx, c) {
		t.Error("setup: the built-in manager is not online")
	}
	port, _ := doctorPort(c.Listen)
	if resp, err := doctorLoopback(c, port)(ctx, "/api/v1/health"); err != nil || resp.StatusCode != http.StatusOK {
		t.Errorf("doctor: %v, %v", resp, err)
	} else {
		resp.Body.Close()
	}
}
