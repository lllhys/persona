package mdns

import (
	"fmt"
	"net"
	"net/netip"
	"testing"
	"time"
)

// legacyReply is a unicast reply the runner would have sent.
type legacyReply struct {
	dst string
	id  uint16
}

// legacyRunner is an advertising runner with one link on 192.0.2.0/24 and 2001:db8:1::/64 whose unicast replies are
// recorded instead of sent, so nothing leaves the process.
func legacyRunner(t *testing.T) (*runner, *link, *[]legacyReply) {
	t.Helper()
	svc := testService()
	r, err := newRunner(Options{Advertise: &svc, Logf: t.Logf})
	if err != nil {
		t.Fatal(err)
	}
	l := &link{ifi: net.Interface{Index: 1 << 30, Name: "test0", Flags: net.FlagUp | net.FlagRunning | net.FlagMulticast}, on4: true, on6: true}
	r.links[l.ifi.Index] = l
	r.prefixes = func(*net.Interface) []netip.Prefix {
		return []netip.Prefix{netip.MustParsePrefix("192.0.2.10/24"), netip.MustParsePrefix("2001:db8:1::10/64")}
	}
	sent := new([]legacyReply)
	r.unicast = func(b []byte, _ bool, dst *net.UDPAddr) error {
		m, err := unpackMessage(b)
		if err != nil {
			t.Fatalf("reply to %v does not parse: %v", dst, err)
		}
		*sent = append(*sent, legacyReply{dst: dst.String(), id: m.id})
		return nil
	}
	return r, l, sent
}

// oneShotQuery is a one-shot query (source port not 5353) for n's PTR records from src.
func oneShotQuery(t *testing.T, l *link, n name, id uint16, src string) packet {
	t.Helper()
	b, err := packMessage(&message{id: id, questions: []question{{name: n, typ: typePTR, class: classIN}}})
	if err != nil {
		t.Fatal(err)
	}
	ap := netip.MustParseAddrPort(src)
	return packet{data: b, ifindex: l.ifi.Index, src: net.UDPAddrFromAddrPort(ap), v6: ap.Addr().Is6()}
}

// TestLegacyUnicastOnlyOnLink: a legacy query gets its unicast reply only when the sender is on the receiving link,
// so the service enumeration query, whose reply is several times its size, cannot be reflected to a spoofed address.
func TestLegacyUnicastOnlyOnLink(t *testing.T) {
	for _, c := range []struct {
		src     string
		answers bool
	}{
		{"192.0.2.7:40000", true},
		{"198.51.100.7:40000", false},
		{"[2001:db8:1::7]:40000", true},
		{"[fe80::7%test0]:40000", true},
		{"[2001:db8:2::7]:40000", false},
		{"[::ffff:198.51.100.7]:40000", false},
	} {
		r, l, sent := legacyRunner(t)
		r.handle(oneShotQuery(t, l, metaName, 0x4a52, c.src), time.Now())
		switch {
		case !c.answers && len(*sent) != 0:
			t.Errorf("%s is off the link, but got %v", c.src, *sent)
		case c.answers && (len(*sent) != 1 || (*sent)[0] != legacyReply{dst: c.src, id: 0x4a52}):
			t.Errorf("%s is on the link: replies %v, want one with id 0x4a52", c.src, *sent)
		}
	}
}

// TestLegacyUnicastOncePerSecond: one sender gets the same answer at most once a second, whatever its port; another
// answer set or another sender is not held back by it.
func TestLegacyUnicastOncePerSecond(t *testing.T) {
	r, l, sent := legacyRunner(t)
	now := time.Now()
	check := func(desc string, want int) {
		t.Helper()
		if len(*sent) != want {
			t.Errorf("%s: %d replies %v, want %d", desc, len(*sent), *sent, want)
		}
		*sent = nil
	}

	for i := range 5 {
		src := fmt.Sprintf("192.0.2.7:%d", 40000+i)
		r.handle(oneShotQuery(t, l, r.svc.typ, uint16(i), src), now.Add(time.Duration(i)*200*time.Millisecond))
	}
	check("5 queries within 1 s", 1)

	r.handle(oneShotQuery(t, l, metaName, 9, "192.0.2.7:40000"), now.Add(900*time.Millisecond))
	check("another answer set", 1)

	r.handle(oneShotQuery(t, l, r.svc.typ, 10, "192.0.2.8:40000"), now.Add(900*time.Millisecond))
	check("another sender", 1)

	r.handle(oneShotQuery(t, l, r.svc.typ, 11, "192.0.2.7:40000"), now.Add(1100*time.Millisecond))
	check("1.1 s after the first reply", 1)
}

func TestIfacePrefixes(t *testing.T) {
	ifs, err := net.Interfaces()
	if err != nil {
		t.Skipf("no interfaces: %v", err)
	}
	for i := range ifs {
		if ifs[i].Flags&net.FlagLoopback == 0 || ifs[i].Flags&net.FlagUp == 0 {
			continue
		}
		ps := ifacePrefixes(&ifs[i])
		if !onLink(netip.MustParseAddr("127.0.0.1"), ps) && !onLink(netip.MustParseAddr("::1"), ps) {
			t.Errorf("%s: prefixes %v hold neither 127.0.0.1 nor ::1", ifs[i].Name, ps)
		}
		if onLink(netip.MustParseAddr("198.51.100.7"), ps) {
			t.Errorf("%s: prefixes %v hold 198.51.100.7", ifs[i].Name, ps)
		}
		return
	}
	t.Skip("no loopback interface that is up")
}
