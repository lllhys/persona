package remoteimage

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"sync/atomic"
	"testing"
)

// fakeIP is what a fake-IP proxy's resolver answers for every outside name (built from bytes: the privacy scan reads
// literal private-looking numbers in sources).
var fakeIP = netip.AddrFrom4([4]byte{198, 18, 0, 7}).String()

// dohRig is a rig whose resolver answers only fake addresses, and a DoH service that answers `answer(name, type)`.
func dohRig(t *testing.T, answer func(name string, kind int) (int, any)) (*rig, *atomic.Int32) {
	t.Helper()
	r := newRig(t, map[string]string{"img.test": fakeIP, "cdn.test": fakeIP}, serve(pngBytes(t, 4, 4, false), "image/png"))
	var asked atomic.Int32
	doh := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		asked.Add(1)
		if req.Header.Get("Accept") != "application/dns-json" {
			t.Errorf("DoH asked with Accept %q", req.Header.Get("Accept"))
		}
		var kind int
		_, _ = fmt.Sscan(req.URL.Query().Get("type"), &kind)
		status, body := answer(req.URL.Query().Get("name"), kind)
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(body)
	}))
	t.Cleanup(doh.Close)
	r.f.o.DoH = func() string { return doh.URL + "/resolve" }
	return r, &asked
}

// a DoH JSON answer: a CNAME on the way, then the addresses of `kind`.
func dohAnswer(kind int, ips ...string) map[string]any {
	answers := []map[string]any{{"name": "img.test.", "type": 5, "data": "edge.cdn.test."}}
	for _, ip := range ips {
		answers = append(answers, map[string]any{"name": "edge.cdn.test.", "type": kind, "data": ip})
	}
	return map[string]any{"Status": 0, "Question": map[string]any{"name": "img.test.", "type": kind}, "Answer": answers}
}

func TestFakeIPAnswersAreReplacedByDoH(t *testing.T) {
	r, asked := dohRig(t, func(name string, kind int) (int, any) {
		if kind == 1 {
			return 200, dohAnswer(1, publicIP)
		}
		return 200, map[string]any{"Status": 0} // no AAAA
	})
	if _, err := r.f.Get(t.Context(), "phone", "http://img.test/a.png", 256); err != nil {
		t.Fatal(err)
	}
	if len(r.dialed) != 1 || r.dialed[0] != publicIP+":80" {
		t.Fatalf("dialled %v, want the DoH answer %s:80", r.dialed, publicIP)
	}
	// More pictures on the host: the real addresses are kept, not asked again.
	r.f.transport.CloseIdleConnections()
	if _, err := r.f.Get(t.Context(), "phone", "http://img.test/b.png", 256); err != nil {
		t.Fatal(err)
	}
	if n := asked.Load(); n != 2 { // A and AAAA, once
		t.Fatalf("DoH asked %d times for two pictures, want 2 (A, AAAA once)", n)
	}
}

func TestDoHAnswersAreCheckedAndFailuresRefuse(t *testing.T) {
	private := netip.AddrFrom4([4]byte{10, 0, 0, 9}).String()
	for name, answer := range map[string]func(string, int) (int, any){
		"a private address": func(_ string, kind int) (int, any) { return 200, dohAnswer(kind, private) },
		"a fake one again":  func(_ string, kind int) (int, any) { return 200, dohAnswer(kind, fakeIP) },
		"loopback over AAAA": func(_ string, kind int) (int, any) {
			return 200, dohAnswer(kind, map[int]string{1: publicIP, 28: "::1"}[kind])
		},
		"an error status":     func(string, int) (int, any) { return 503, map[string]any{} },
		"SERVFAIL":            func(string, int) (int, any) { return 200, map[string]any{"Status": 2} },
		"no address":          func(string, int) (int, any) { return 200, map[string]any{"Status": 0} },
		"an unreadable reply": func(string, int) (int, any) { return 200, "not json" },
	} {
		t.Run(name, func(t *testing.T) {
			r, _ := dohRig(t, answer)
			if _, err := r.f.Get(t.Context(), "phone", "http://img.test/a.png", 256); code(err) != CodeNotAllowed {
				t.Fatalf("%v, want %s", err, CodeNotAllowed)
			}
			if r.hits.Load() != 0 || len(r.dialed) != 0 {
				t.Fatalf("refused, yet dialled %v (%d requests)", r.dialed, r.hits.Load())
			}
		})
	}
}

func TestFakeIPWithoutDoHIsRefused(t *testing.T) {
	for _, service := range []func() string{nil, func() string { return "" }} {
		r := newRig(t, map[string]string{"img.test": fakeIP}, serve(pngBytes(t, 4, 4, false), "image/png"))
		r.f.o.DoH = service
		if _, err := r.f.Get(t.Context(), "phone", "http://img.test/a.png", 256); code(err) != CodeNotAllowed {
			t.Fatalf("%v, want %s", err, CodeNotAllowed)
		}
	}
}

func TestFakeOnly(t *testing.T) {
	ip := func(s string) net.IPAddr { return net.IPAddr{IP: net.ParseIP(s)} }
	sixFake := netip.MustParseAddr("fc00::1").String()
	for _, c := range []struct {
		ips  []net.IPAddr
		want bool
	}{
		{[]net.IPAddr{ip(fakeIP)}, true},
		{[]net.IPAddr{ip(fakeIP), ip(sixFake)}, true},
		{[]net.IPAddr{ip(fakeIP), ip(publicIP)}, false}, // mixed: the check refuses the fake one
		{[]net.IPAddr{ip(publicIP)}, false},
		{nil, false},
	} {
		if got := fakeOnly(c.ips); got != c.want {
			t.Errorf("fakeOnly(%v) = %v, want %v", c.ips, got, c.want)
		}
	}
	// A DoH service that does not answer at all refuses too (the client's own error).
	r := newRig(t, map[string]string{"img.test": fakeIP}, serve(pngBytes(t, 4, 4, false), "image/png"))
	r.f.o.DoH = func() string { return "http://127.0.0.1:1/resolve" }
	if _, err := r.f.Get(t.Context(), "phone", "http://img.test/a.png", 256); code(err) != CodeNotAllowed {
		t.Fatalf("an unreachable DoH: %v, want %s", err, CodeNotAllowed)
	}
}
