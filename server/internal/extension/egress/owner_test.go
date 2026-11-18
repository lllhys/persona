package egress

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"slices"
	"sync"
	"testing"
)

// A host the owner allowed (#5697) is that host only: on any port when written without one, on its port when written
// with one; not a subdomain; plain http too. Taking it away refuses it at once.
func TestOwnerAllowedHosts(t *testing.T) {
	srv := newEcho(t)
	p := newProxy(t, srv, nil)
	purl := p.Register("rsshub", Rule{Hosts: []string{"rsshub.app"}, Owner: []Target{{Host: "rss.example.org", HTTP: true}, {Host: "feeds.example.net", Port: "8443", HTTP: true}}})
	for target, want := range map[string]int{
		"rss.example.org:443":       http.StatusOK,
		"rss.example.org:8443":      http.StatusOK,
		"www.rss.example.org:443":   http.StatusForbidden,
		"feeds.example.net:8443":    http.StatusOK,
		"feeds.example.net:443":     http.StatusForbidden,
		"elsewhere.example.com:443": http.StatusForbidden,
	} {
		if got := connectStatus(t, purl, target); got != want {
			t.Errorf("CONNECT %s: %d, want %d", target, got, want)
		}
	}
	resp, err := client(purl).Get("http://rss.example.org/feed")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || string(body) != "ok" {
		t.Fatalf("plain http to an allowed host: %d %q", resp.StatusCode, body)
	}

	p.SetOwner("rsshub", []Target{{Host: "feeds.example.net", Port: "8443", HTTP: true}})
	if got := connectStatus(t, purl, "rss.example.org:443"); got != http.StatusForbidden {
		t.Fatalf("a revoked host: %d", got)
	}
	if got := connectStatus(t, purl, "feeds.example.net:8443"); got != http.StatusOK {
		t.Fatalf("the host still allowed: %d", got)
	}
	// An extension not running has no rule to change.
	p.SetOwner("nobody", []Target{{Host: "rss.example.org"}})
}

// An allowed host may resolve to a private LAN address (a service at home, the common case); one that resolves to
// loopback, link-local or a metadata address is refused even though the owner allowed it, and nothing is dialled
// (#5697).
func TestOwnerAllowedHostAddresses(t *testing.T) {
	// Private LAN addresses are built, not written: the repository's privacy scan refuses literal ones.
	home, ten, lab := net.IPv4(192, 168, 1, 20), net.IPv4(10, 0, 0, 5), net.IPv4(172, 20, 1, 9)
	answers := map[string][]net.IP{
		"home.example.org":  {home},
		"ten.example.org":   {ten},
		"lab.example.org":   {lab},
		"ula.example.org":   {net.ParseIP("fd12:3456::5")},
		"loop.example.org":  {net.ParseIP("127.0.0.1")},
		"loop6.example.org": {net.ParseIP("::1")},
		"meta.example.org":  {net.IPv4(169, 254, 169, 254)},
		"link.example.org":  {net.ParseIP("fe80::1")},
		"mixed.example.org": {net.IPv4(192, 168, 1, 21), net.ParseIP("127.0.0.1")},
	}
	var mu sync.Mutex
	var dialled []string
	p := &Proxy{HubAddr: "127.0.0.1:18660", LocalAddrs: func() []net.IP { return nil },
		Resolve: func(_ context.Context, name string) ([]net.IP, error) { return answers[name], nil },
		Dial: func(_ context.Context, _, addr string) (net.Conn, error) {
			mu.Lock()
			dialled = append(dialled, addr)
			mu.Unlock()
			a, b := net.Pipe()
			b.Close()
			return a, nil
		},
		Upstream: func(*url.URL) (*url.URL, error) { return nil, nil }}
	ctx := context.Background()
	for _, host := range []string{"home.example.org", "ten.example.org", "lab.example.org", "ula.example.org"} {
		c, err := p.dialConfigured(ctx, host, "1200")
		if err != nil {
			t.Fatalf("%s: %v", host, err)
		}
		c.Close()
	}
	for _, host := range []string{"loop.example.org", "loop6.example.org", "meta.example.org", "link.example.org", "mixed.example.org"} {
		if _, err := p.dialConfigured(ctx, host, "1200"); !errors.Is(err, errNotForSettings) {
			t.Errorf("%s: %v", host, err)
		}
	}
	want := []string{net.JoinHostPort(home.String(), "1200"), net.JoinHostPort(ten.String(), "1200"), net.JoinHostPort(lab.String(), "1200"), "[fd12:3456::5]:1200"}
	if !slices.Equal(dialled, want) {
		t.Fatalf("dialled %v, want %v", dialled, want)
	}
}

// DNS rebinding: an allowed name that answered a LAN address once and loopback the next time is refused the second
// time, through the proxy, and loopback is never dialled (#5697).
func TestOwnerAllowedHostRebinding(t *testing.T) {
	srv := newEcho(t)
	p := newProxy(t, srv, nil)
	var mu sync.Mutex
	calls := 0
	var dialled []string
	p.Resolve = func(_ context.Context, name string) ([]net.IP, error) {
		mu.Lock()
		defer mu.Unlock()
		calls++
		if calls == 1 {
			return []net.IP{net.ParseIP("192.0.2.1")}, nil
		}
		return []net.IP{net.ParseIP("127.0.0.1")}, nil
	}
	dial := p.Dial
	p.Dial = func(ctx context.Context, network, addr string) (net.Conn, error) {
		mu.Lock()
		dialled = append(dialled, addr)
		mu.Unlock()
		return dial(ctx, network, addr)
	}
	purl := p.Register("rsshub", Rule{Owner: []Target{{Host: "home.example.org", HTTP: true}}})
	if got := connectStatus(t, purl, "home.example.org:443"); got != http.StatusOK {
		t.Fatalf("first answer (a LAN address): %d", got)
	}
	if got := connectStatus(t, purl, "home.example.org:443"); got == http.StatusOK {
		t.Fatal("the name now resolving to loopback was let through")
	}
	for _, a := range dialled {
		if h, _, _ := net.SplitHostPort(a); net.ParseIP(h).IsLoopback() {
			t.Fatalf("dialled %s", a)
		}
	}
}

// The proxy keeps the last 5 different refused hosts, the latest first, with how often each was refused.
func TestRecentDenied(t *testing.T) {
	srv := newEcho(t)
	p := newProxy(t, srv, nil)
	purl := p.Register("rsshub", Rule{Hosts: []string{"rsshub.app"}})
	for _, h := range []string{"a.example.org", "b.example.org", "a.example.org", "c.example.org", "d.example.org", "e.example.org", "f.example.org", "c.example.org"} {
		connectStatus(t, purl, h+":443")
	}
	connectStatus(t, purl, "rsshub.app:443")
	got := p.Stats("rsshub")
	var hosts []string
	for _, d := range got.Recent {
		hosts = append(hosts, fmt.Sprintf("%s×%d", d.Host, d.Count))
	}
	if want := []string{"c.example.org×2", "f.example.org×1", "e.example.org×1", "d.example.org×1", "a.example.org×2"}; !slices.Equal(hosts, want) {
		t.Fatalf("recent %v, want %v", hosts, want)
	}
	if got.Denied != 8 || got.Allowed != 1 || got.LastDenied != "c.example.org" {
		t.Fatalf("counts %+v", got)
	}
	// Stats is a copy.
	got.Recent[0].Count = 99
	if p.Stats("rsshub").Recent[0].Count != 2 {
		t.Fatal("Stats shares its slice")
	}
}
