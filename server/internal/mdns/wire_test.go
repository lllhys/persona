package mdns

import (
	"bytes"
	"errors"
	"maps"
	"math/rand/v2"
	"net/netip"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"
)

// One whole message each way, for the wire tests: the responder sends served.response (mdns.go) and the browser reads
// messages with group into its cache (browse.go); these two use the same functions without the sockets or the cache.

// parseEntries reads the instances of the service type browse that one message describes, without any cache:
// an instance with only a PTR in this message comes back with an empty Host and Port 0. Queries, malformed
// messages and a bad browse type give nil.
func parseEntries(msg []byte, browse string) []Entry {
	typ, err := parseType(browse)
	if err != nil {
		return nil
	}
	m, err := unpackMessage(msg)
	if err != nil || !m.isResponse() {
		return nil
	}
	now := time.Now()
	insts, addrs := group(m, typ)
	out := make([]Entry, 0, len(insts))
	for _, g := range insts {
		e := Entry{Instance: g.label, Seen: now}
		switch {
		case g.ptr != nil:
			e.TTL = clampTTL(g.ptr.ttl)
		case g.srv != nil:
			e.TTL = clampTTL(g.srv.ttl)
		case g.txt != nil:
			e.TTL = clampTTL(g.txt.ttl)
		}
		if g.srv != nil {
			e.Host = g.srv.target.String()
			e.Port = int(g.srv.port)
			for _, a := range addrs {
				if a.name.equal(g.srv.target) && !slices.Contains(e.Addrs, a.addr) {
					e.Addrs = append(e.Addrs, a.addr)
				}
			}
			slices.SortFunc(e.Addrs, netip.Addr.Compare)
		}
		if g.txt != nil {
			e.TXT = decodeTXT(g.txt.txt)
		}
		out = append(out, e)
	}
	return out
}

// buildResponse packs the announcement of svc with addrs (ttl 0: the goodbye).
func buildResponse(svc Service, addrs []netip.Addr, ttl uint32) ([]byte, error) {
	s, err := newServed(svc)
	if err != nil {
		return nil, err
	}
	return s.response(addrs, ttl)
}

func testService() Service {
	return Service{
		Instance: "Hub. One 2",
		Type:     "_persona-hub._tcp",
		Port:     8443,
		TXT: map[string]string{
			"hub_id": "1a2b3c4d",
			"name":   "家里 hub",
			"fp":     "sha256:00ff",
			"tls":    "8443",
			"flag":   "",
		},
	}
}

func testAddrs() []netip.Addr {
	return []netip.Addr{
		netip.MustParseAddr("2001:db8::10"),
		netip.MustParseAddr("192.0.2.10"),
		netip.MustParseAddr("fe80::1%eth0"), // the zone is not on the wire
		netip.MustParseAddr("127.0.0.1"),    // loopback is never advertised
	}
}

func mustType(t *testing.T, s string) name {
	t.Helper()
	n, err := parseType(s)
	if err != nil {
		t.Fatal(err)
	}
	return n
}

func TestBuildParseRoundTrip(t *testing.T) {
	svc := testService()
	b, err := buildResponse(svc, testAddrs(), recordTTL)
	if err != nil {
		t.Fatal(err)
	}
	for _, browse := range []string{"_persona-hub._tcp", "_PERSONA-HUB._TCP.local.", "_persona-hub._tcp.LOCAL"} {
		es := parseEntries(b, browse)
		if len(es) != 1 {
			t.Fatalf("browse %q: got %d entries, want 1", browse, len(es))
		}
		e := es[0]
		if e.Instance != svc.Instance {
			t.Errorf("Instance = %q, want %q", e.Instance, svc.Instance)
		}
		if e.Host != "persona-hub-one-2.local." {
			t.Errorf("Host = %q", e.Host)
		}
		if e.Port != 8443 {
			t.Errorf("Port = %d", e.Port)
		}
		if !maps.Equal(e.TXT, svc.TXT) {
			t.Errorf("TXT = %v, want %v", e.TXT, svc.TXT)
		}
		want := []netip.Addr{
			netip.MustParseAddr("192.0.2.10"),
			netip.MustParseAddr("2001:db8::10"),
			netip.MustParseAddr("fe80::1"),
		}
		if !slices.Equal(e.Addrs, want) {
			t.Errorf("Addrs = %v, want %v", e.Addrs, want)
		}
		if e.TTL != recordTTL*time.Second {
			t.Errorf("TTL = %v", e.TTL)
		}
	}
	if es := parseEntries(b, "_other._tcp"); len(es) != 0 {
		t.Errorf("other type: got %v", es)
	}

	// The instance is one label on the wire, dots and spaces included.
	m, err := unpackMessage(b)
	if err != nil {
		t.Fatal(err)
	}
	if len(m.answers) != 1 || len(m.additionals) != 2+3 {
		t.Fatalf("sections: %d answers, %d additionals", len(m.answers), len(m.additionals))
	}
	target := m.answers[0].target
	if len(target) != 4 || target[0] != "Hub. One 2" {
		t.Fatalf("PTR target labels = %q", target)
	}
	if got := target.String(); got != `Hub\.\032One\0322._persona-hub._tcp.local.` {
		t.Errorf("PTR target = %s", got)
	}
	if m.answers[0].class != classIN {
		t.Errorf("PTR is a shared record, class = %#x", m.answers[0].class)
	}
	for _, r := range m.additionals {
		if r.class != classIN|topBit {
			t.Errorf("%d record should have the cache-flush bit, class = %#x", r.typ, r.class)
		}
	}
}

func TestBuildCompresses(t *testing.T) {
	b, err := buildResponse(testService(), testAddrs(), recordTTL)
	if err != nil {
		t.Fatal(err)
	}
	// "_persona-hub" is written once; later names point at it.
	if n := strings.Count(string(b), "_persona-hub"); n != 1 {
		t.Errorf("type label written %d times", n)
	}
}

func TestInstanceCut(t *testing.T) {
	svc := testService()
	svc.Instance = strings.Repeat("家", 30) // 90 bytes
	b, err := buildResponse(svc, nil, recordTTL)
	if err != nil {
		t.Fatal(err)
	}
	es := parseEntries(b, svc.Type)
	if len(es) != 1 || es[0].Instance != strings.Repeat("家", 21) {
		t.Fatalf("got %+v", es)
	}
	if !strings.HasPrefix(es[0].Host, "persona.") {
		t.Errorf("Host = %q", es[0].Host) // nothing usable in the instance: the bare prefix
	}
}

func TestExplicitHost(t *testing.T) {
	svc := testService()
	svc.Host = "persona-1a2b3c4d.local."
	es := parseEntries(must(buildResponse(svc, nil, recordTTL)), svc.Type)
	if len(es) != 1 || es[0].Host != "persona-1a2b3c4d.local." {
		t.Fatalf("got %+v", es)
	}
}

func TestHostFromInstance(t *testing.T) {
	for in, want := range map[string]string{
		"1A2B3C4D":                   "persona-1a2b3c4d",
		"Hub. One 2":                 "persona-hub-one-2",
		"--x--":                      "persona-x",
		"persona-1a2b":               "persona-1a2b",
		"家":                          "persona",
		strings.Repeat("ab-", 40):    "persona-" + strings.TrimSuffix(strings.Repeat("ab-", 19)[:maxLabel-len("persona-")], "-"),
		"0123456789abcdef0123456789": "persona-0123456789abcdef0123456789",
	} {
		got := hostFromInstance(in)
		if got != want {
			t.Errorf("hostFromInstance(%q) = %q, want %q", in, got, want)
		}
		if len(got) > maxLabel {
			t.Errorf("hostFromInstance(%q) is %d bytes", in, len(got))
		}
	}
}

func TestBuildErrors(t *testing.T) {
	for name, mut := range map[string]func(*Service){
		"empty instance": func(s *Service) { s.Instance = "" },
		"bad type":       func(s *Service) { s.Type = "persona" },
		"no proto":       func(s *Service) { s.Type = "_persona._sctp" },
		"three labels":   func(s *Service) { s.Type = "_a._b._tcp" },
		"port 0":         func(s *Service) { s.Port = 0 },
		"port too big":   func(s *Service) { s.Port = 70000 },
		"key with =":     func(s *Service) { s.TXT = map[string]string{"a=b": "c"} },
		"empty key":      func(s *Service) { s.TXT = map[string]string{"": "c"} },
		"long value":     func(s *Service) { s.TXT = map[string]string{"k": strings.Repeat("x", 300)} },
		"dotted host":    func(s *Service) { s.Host = "a.example" },
		"too much TXT": func(s *Service) {
			s.TXT = map[string]string{}
			for i := range 60 {
				s.TXT[strings.Repeat("k", 3)+string(rune('A'+i))] = strings.Repeat("v", 200)
			}
		},
	} {
		svc := testService()
		mut(&svc)
		if _, err := buildResponse(svc, nil, recordTTL); err == nil {
			t.Errorf("%s: no error", name)
		}
	}
}

func TestEmptyTXT(t *testing.T) {
	svc := testService()
	svc.TXT = nil
	m, err := unpackMessage(must(buildResponse(svc, nil, recordTTL)))
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range m.additionals {
		if r.typ == typeTXT && !slices.Equal(r.txt, []string{""}) {
			t.Errorf("empty TXT = %q, want one empty string", r.txt)
		}
	}
	es := parseEntries(must(buildResponse(svc, nil, recordTTL)), svc.Type)
	if len(es) != 1 || len(es[0].TXT) != 0 {
		t.Fatalf("got %+v", es)
	}
}

func TestPTROnly(t *testing.T) {
	typ := mustType(t, "_persona-hub._tcp")
	b, err := packMessage(&message{flags: flagResponse | flagAA, answers: []record{
		{name: typ, typ: typePTR, class: classIN, ttl: 4500, target: append(name{"Solo"}, typ...)},
	}})
	if err != nil {
		t.Fatal(err)
	}
	es := parseEntries(b, "_persona-hub._tcp")
	if len(es) != 1 {
		t.Fatalf("got %d entries", len(es))
	}
	e := es[0]
	if e.Instance != "Solo" || e.Host != "" || e.Port != 0 || e.Addrs != nil || e.TTL != 4500*time.Second {
		t.Errorf("got %+v", e)
	}
}

func TestGoodbye(t *testing.T) {
	es := parseEntries(must(buildResponse(testService(), testAddrs(), 0)), "_persona-hub._tcp")
	if len(es) != 1 || es[0].TTL != 0 || es[0].Instance != "Hub. One 2" {
		t.Fatalf("got %+v", es)
	}
}

func TestParseIgnoresQueries(t *testing.T) {
	typ := mustType(t, "_persona-hub._tcp")
	b, err := packMessage(&message{questions: []question{{name: typ, typ: typePTR, class: classIN}}})
	if err != nil {
		t.Fatal(err)
	}
	if es := parseEntries(b, "_persona-hub._tcp"); es != nil {
		t.Errorf("query parsed as %+v", es)
	}
	if es := parseEntries(b, "not a type"); es != nil {
		t.Errorf("bad browse type gave %+v", es)
	}
}

func TestDecodeTXT(t *testing.T) {
	got := decodeTXT([]string{"a=1", "B=2", "flag", "a=dup", "=x", "", "k=v=w"})
	want := map[string]string{"a": "1", "b": "2", "flag": "", "k": "v=w"}
	if !maps.Equal(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestParseType(t *testing.T) {
	for _, ok := range []string{"_persona-hub._tcp", "_persona-hub._tcp.", "_persona-hub._tcp.local", "_x._UDP.Local."} {
		if _, err := parseType(ok); err != nil {
			t.Errorf("%q: %v", ok, err)
		}
	}
	for _, bad := range []string{"", ".", "_tcp", "persona._tcp", "_persona.tcp", "_persona._tcp.example", "_._tcp", "_a._b._tcp"} {
		if _, err := parseType(bad); err == nil {
			t.Errorf("%q: no error", bad)
		}
	}
}

func TestNameString(t *testing.T) {
	for _, c := range []struct {
		n    name
		want string
	}{
		{nil, "."},
		{name{"a b.c", "local"}, `a\032b\.c.local.`},
		{name{`back\slash`, "local"}, `back\\slash.local.`},
		{name{"tab\there\x7f"}, `tab\009here\127.`},
		{name{"家里"}, "家里."},
	} {
		if got := c.n.String(); got != c.want {
			t.Errorf("%q: got %s, want %s", []string(c.n), got, c.want)
		}
	}
}

func TestAnswer(t *testing.T) {
	s, err := newServed(testService())
	if err != nil {
		t.Fatal(err)
	}
	addrs := []netip.Addr{netip.MustParseAddr("192.0.2.10"), netip.MustParseAddr("2001:db8::10")}
	types := func(rs []record) []uint16 {
		var out []uint16
		for _, r := range rs {
			out = append(out, r.typ)
		}
		return out
	}
	for _, c := range []struct {
		desc     string
		q        []question
		ans, add []uint16
	}{
		{"PTR", []question{{s.typ, typePTR, classIN}}, []uint16{typePTR}, []uint16{typeSRV, typeTXT, typeA, typeAAAA}},
		{"PTR with QU bit, other case", []question{{name{"_PERSONA-HUB", "_TCP", "LOCAL"}, typePTR, classIN | topBit}}, []uint16{typePTR}, []uint16{typeSRV, typeTXT, typeA, typeAAAA}},
		{"meta", []question{{metaName, typePTR, classIN}}, []uint16{typePTR}, nil},
		{"SRV", []question{{s.inst, typeSRV, classIN}}, []uint16{typeSRV}, []uint16{typeA, typeAAAA}},
		{"TXT", []question{{s.inst, typeTXT, classIN}}, []uint16{typeTXT}, nil},
		{"ANY instance", []question{{s.inst, typeANY, classIN}}, []uint16{typeSRV, typeTXT}, []uint16{typeA, typeAAAA}},
		{"A", []question{{name{"PERSONA-HUB-ONE-2", "local"}, typeA, classIN}}, []uint16{typeA}, []uint16{typeAAAA}},
		{"AAAA", []question{{s.host, typeAAAA, classIN}}, []uint16{typeAAAA}, []uint16{typeA}},
		{"ANY host", []question{{s.host, typeANY, classIN}}, []uint16{typeA, typeAAAA}, nil},
		{"SRV and A", []question{{s.inst, typeSRV, classIN}, {s.host, typeA, classIN}}, []uint16{typeSRV, typeA}, []uint16{typeAAAA}},
		{"unrelated", []question{{name{"other", "local"}, typeA, classIN}}, nil, nil},
		{"other class", []question{{s.typ, typePTR, 3}}, nil, nil},
		{"other type", []question{{s.typ, typeSRV, classIN}}, nil, nil},
	} {
		w := s.match(c.q)
		ans, add := s.build(w, addrs, recordTTL)
		if !slices.Equal(types(ans), c.ans) || !slices.Equal(types(add), c.add) {
			t.Errorf("%s: answers %v additionals %v, want %v %v", c.desc, types(ans), types(add), c.ans, c.add)
		}
		// Skipping the address read must not change the answer.
		if !w.needsAddrs() {
			bare, bareAdd := s.build(w, nil, recordTTL)
			if !slices.Equal(types(bare), c.ans) || !slices.Equal(types(bareAdd), c.add) {
				t.Errorf("%s: needs no addresses, but without them: answers %v additionals %v", c.desc, types(bare), types(bareAdd))
			}
		}
	}
	// Meta answer points at the type.
	ans, _ := s.build(s.match([]question{{metaName, typePTR, classIN}}), nil, recordTTL)
	if !ans[0].target.equal(s.typ) {
		t.Errorf("meta target = %s", ans[0].target)
	}
}

func TestCompressionPointers(t *testing.T) {
	hdr := []byte{0, 0, 0x84, 0, 0, 0, 0, 1, 0, 0, 0, 0} // response, one answer
	for desc, body := range map[string][]byte{
		"self loop":                          {0xC0, 12},
		"forward pointer":                    {0xC0, 20, 0, 0, 0, 0, 0, 0, 0, 0},
		"pointer to label start of own name": {1, 'a', 0xC0, 12},
		"reserved label":                     {0x40, 0},
		"truncated pointer":                  {0xC0},
		"label past end":                     {10, 'a'},
	} {
		msg := append(slices.Clone(hdr), body...)
		if _, err := unpackMessage(msg); err == nil {
			t.Errorf("%s: no error", desc)
		}
	}
	// A legal backward pointer: question "local." at 12, answer name points at it.
	msg := []byte{0, 0, 0x84, 0, 0, 1, 0, 1, 0, 0, 0, 0,
		5, 'l', 'o', 'c', 'a', 'l', 0, 0, 1, 0, 1, // question local. A IN
		0xC0, 12, 0, 1, 0, 1, 0, 0, 0, 10, 0, 4, 192, 0, 2, 1} // answer local. A 192.0.2.1
	m, err := unpackMessage(msg)
	if err != nil {
		t.Fatal(err)
	}
	if len(m.answers) != 1 || m.answers[0].name.String() != "local." || m.answers[0].addr != netip.MustParseAddr("192.0.2.1") {
		t.Errorf("got %+v", m.answers)
	}
}

// pointerChain is a 9000-byte query whose every question name runs through the names of all the questions before it:
// q0 is the root name at 12 and its TYPE field holds a pointer to 12; each later 6-byte question has a name and a
// TYPE field that both point at the previous question's TYPE field. Every pointer points backwards, yet question k
// follows k+1 pointers, so 1498 questions cost about 1.1M jumps.
func pointerChain() []byte {
	msg := []byte{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0xC0, 12, 0, 1} // header, q0: root, TYPE -> 12, CLASS IN
	prevType, qd := 13, 1
	for len(msg)+6 <= maxPacket {
		hi, lo := byte(0xC0|prevType>>8), byte(prevType)
		prevType = len(msg) + 2
		msg = append(msg, hi, lo, hi, lo, 0, 1)
		qd++
	}
	msg[4], msg[5] = byte(qd>>8), byte(qd)
	return msg
}

// labelFanOut is a 9000-byte query with no pointer chain at all: q0 is a 255-byte name of 127 one-byte labels, and each
// of the other 1454 questions is a single pointer to it. Every question follows one pointer but reads 127 labels again,
// about 185K labels in total.
func labelFanOut() []byte {
	msg := []byte{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}
	for range 127 {
		msg = append(msg, 1, 'a')
	}
	msg = append(msg, 0, 0, 1, 0, 1) // root, TYPE A, CLASS IN
	qd := 1
	for len(msg)+6 <= maxPacket {
		msg = append(msg, 0xC0, 12, 0, 1, 0, 1)
		qd++
	}
	msg[4], msg[5] = byte(qd>>8), byte(qd)
	return msg
}

func TestCompressionPointerChain(t *testing.T) {
	msg := pointerChain()
	if _, err := unpackMessage(msg); !errors.Is(err, errMalformed) {
		t.Errorf("pointer chain: err = %v, want errMalformed", err)
	}
	// Labels read through a pointer count too, or a packet of short pointers to one long name costs more than the chain.
	if _, err := unpackMessage(labelFanOut()); !errors.Is(err, errNameBudget) {
		t.Errorf("label fan-out: err = %v, want errNameBudget", err)
	}

	// Chained pointers of a real message still parse: the answer's PTR target runs 34 -> 23 -> 12.
	msg = []byte{0, 0, 0x84, 0, 0, 2, 0, 1, 0, 0, 0, 0,
		5, 'l', 'o', 'c', 'a', 'l', 0, 0, 1, 0, 1, // 12: question local. A IN
		4, '_', 't', 'c', 'p', 0xC0, 12, 0, 1, 0, 1, // 23: question _tcp.local. A IN
		3, 'h', 'u', 'b', 0xC0, 23, 0, 12, 0, 1, 0, 0, 0, 10, 0, 2, // 34: answer hub._tcp.local. PTR
		0xC0, 34} // its target, hub._tcp.local. again
	m, err := unpackMessage(msg)
	if err != nil {
		t.Fatal(err)
	}
	if len(m.answers) != 1 || m.answers[0].target.String() != "hub._tcp.local." {
		t.Errorf("got %+v", m.answers)
	}
	// The budget covers rdata names too: running out in the PTR target fails the message instead of skipping the record.
	budget := 10 // the answer's name takes 5 (3 labels, 2 pointers), its target 6
	if _, _, _, err := readRecord(msg, 34, &budget); !errors.Is(err, errNameBudget) {
		t.Errorf("PTR target over budget: err = %v, want errNameBudget", err)
	}
	budget = 11
	if r, ok, _, err := readRecord(msg, 34, &budget); err != nil || !ok || r.target.String() != "hub._tcp.local." || budget != 0 {
		t.Errorf("PTR within budget: %+v ok %v err %v, budget left %d", r, ok, err, budget)
	}
}

// reverseName is the reverse-mapping name of a: d.c.b.a.in-addr.arpa, or the 32 nibbles of an IPv6 address
// lowest first, then ip6.arpa (34 labels).
func reverseName(a netip.Addr) name {
	if a.Is4() {
		b := a.As4()
		return name{strconv.Itoa(int(b[3])), strconv.Itoa(int(b[2])), strconv.Itoa(int(b[1])), strconv.Itoa(int(b[0])), "in-addr", "arpa"}
	}
	const hex = "0123456789abcdef"
	b := a.As16()
	n := make(name, 0, 34)
	for i := 15; i >= 0; i-- {
		n = append(n, hex[b[i]&0xf:b[i]&0xf+1], hex[b[i]>>4:b[i]>>4+1])
	}
	return append(n, "ip6", "arpa")
}

// TestNameBudgetAdmitsRealResponses pins that the name budget (one per label read and pointer followed, len(msg) per
// message) lets real responses through even though their names expand to more bytes than the message has: a
// budget charged per name byte would drop them. It does not pin how close they come to len(msg).
func TestNameBudgetAdmitsRealResponses(t *testing.T) {
	host := name{"living-room-hub", "local"}
	inst := name{"Living Room Hub v1.2 (Den)", "_persona-hub", "_tcp", "local"}
	svcType := inst[1:]
	addrs := []netip.Addr{
		netip.MustParseAddr("192.0.2.10"),
		netip.MustParseAddr("192.0.2.11"),
		netip.MustParseAddr("2001:db8:1:2:a1b2:c3ff:fed4:e5f6"), // stable SLAAC address
		netip.MustParseAddr("2001:db8:1:2:7d3e:91a0:5b2c:4f18"), // temporary address in the same /64
		netip.MustParseAddr("fe80::a1b2:c3ff:fed4:e5f6"),
	}
	addrRecords := func() []record {
		var rs []record
		for _, a := range addrs {
			typ := typeAAAA
			if a.Is4() {
				typ = typeA
			}
			rs = append(rs, record{name: host, typ: typ, class: classIN | topBit, ttl: 120, addr: a})
		}
		return rs
	}
	// A Bonjour-style announcement: the service enumeration and instance PTRs, then SRV, TXT and every address.
	bonjour := &message{flags: flagResponse | flagAA,
		answers: []record{
			{name: name{"_services", "_dns-sd", "_udp", "local"}, typ: typePTR, class: classIN, ttl: 4500, target: svcType},
			{name: svcType, typ: typePTR, class: classIN, ttl: 4500, target: inst},
			{name: inst, typ: typeSRV, class: classIN | topBit, ttl: 120, target: host, port: 8443},
			{name: inst, typ: typeTXT, class: classIN | topBit, ttl: 4500, txt: []string{"txtvers=1", "id=3f9c2a71", "api=/v1"}},
		},
		additionals: addrRecords(),
	}
	// Reverse-mapping PTRs for the same addresses: the two in the /64 share 18 labels, every ip6.arpa name shares "arpa"
	// with the in-addr.arpa ones, so most owners are a few labels and a pointer into a deep name.
	reverse := &message{flags: flagResponse | flagAA, additionals: addrRecords()}
	for _, a := range addrs {
		reverse.answers = append(reverse.answers, record{name: reverseName(a), typ: typePTR, class: classIN | topBit, ttl: 120, target: host})
	}
	// The announcement plus NSEC records for the instance and the host, a type this package skips but whose owner
	// name is still read: owner and next domain are pointers to the name, then the type bitmap.
	withNSEC := func() []byte {
		msg := must(packMessage(bonjour))
		for _, n := range []struct {
			label  string
			bitmap []byte
		}{
			{inst[0], []byte{0, 5, 0, 0, 0x80, 0, 0x40}}, // TXT, SRV
			{host[0], []byte{0, 4, 0x40, 0, 0, 0x08}},    // A, AAAA
		} {
			off := bytes.Index(msg, append([]byte{byte(len(n.label))}, n.label...))
			ptr := []byte{byte(0xC0 | off>>8), byte(off)}
			msg = append(msg, ptr...)
			msg = append(msg, 0, 47, 0x80, 1, 0, 0, 0, 120, 0, byte(2+len(n.bitmap)))
			msg = append(msg, ptr...)
			msg = append(msg, n.bitmap...)
			msg[11]++ // one more additional
		}
		return msg
	}

	for _, c := range []struct {
		desc string
		msg  []byte
		want *message // the records the message must give back, in order
	}{
		{"bonjour announcement", must(packMessage(bonjour)), bonjour},
		{"reverse mapping", must(packMessage(reverse)), reverse},
		{"announcement with NSEC", withNSEC(), bonjour},
	} {
		m, err := unpackMessage(c.msg)
		if err != nil {
			t.Errorf("%s (%d bytes): %v", c.desc, len(c.msg), err)
			continue
		}
		got := slices.Concat(m.answers, m.authorities, m.additionals)
		want := slices.Concat(c.want.answers, c.want.authorities, c.want.additionals)
		if !slices.EqualFunc(got, want, func(g, w record) bool {
			return g.name.equal(w.name) && g.typ == w.typ && g.class == w.class && g.target.equal(w.target) &&
				g.port == w.port && slices.Equal(g.txt, w.txt) && g.addr == w.addr
		}) {
			t.Errorf("%s: got %+v, want %+v", c.desc, got, want)
		}
		// The names read expand to more bytes than the message holds, so a per-byte charge would refuse it.
		expanded := 0
		for _, r := range got {
			expanded += r.name.wireLen()
			if r.typ == typePTR || r.typ == typeSRV {
				expanded += r.target.wireLen()
			}
		}
		if expanded <= len(c.msg) {
			t.Errorf("%s: names expand to %d bytes, not more than the %d-byte message", c.desc, expanded, len(c.msg))
		}
		t.Logf("%s: %d bytes, names expand to %d", c.desc, len(c.msg), expanded)
	}
}

// labelByLabel is a response with a PTR to the host at every level of the ip6.arpa name of a, the last levels of it:
// each owner is one label and a pointer to the owner before it, the name compressed label by label.
func labelByLabel(a netip.Addr, levels int) []byte {
	msg := []byte{0, 0, 0x84, 0, 0, 0, 0, 0, 0, 0, 0, 0}
	host := len(msg)
	msg = append(msg, 3, 'h', 'u', 'b', 5, 'l', 'o', 'c', 'a', 'l', 0, 0, 1, 0x80, 1, 0, 0, 0, 120, 0, 4, 192, 0, 2, 10)
	full, prev := reverseName(a), 0
	for i := len(full) - 1; i >= len(full)-levels; i-- {
		start := len(msg)
		msg = append(msg, byte(len(full[i])))
		msg = append(msg, full[i]...)
		if prev == 0 {
			msg = append(msg, 0)
		} else {
			msg = append(msg, byte(0xC0|prev>>8), byte(prev))
		}
		prev = start
		msg = append(msg, 0, 12, 0x80, 1, 0, 0, 0, 120, 0, 2, byte(0xC0|host>>8), byte(host))
	}
	msg[7] = byte(1 + levels)
	return msg
}

// Each owner of labelByLabel reads every level under it again, so the message costs about levels squared against a
// budget that grows by about 16 a level: a shallow chain reads, a record at each of the 34 levels of an ip6.arpa name
// runs out. That is the cost the budget exists to refuse; responders compress by whole suffix instead, which reads
// each name once (TestNameBudgetAdmitsRealResponses).
func TestNameBudgetLabelByLabel(t *testing.T) {
	a := netip.MustParseAddr("2001:db8:1:2:a1b2:c3ff:fed4:e5f6")
	full := reverseName(a)
	m, err := unpackMessage(labelByLabel(a, 8))
	if err != nil || len(m.answers) != 9 || !m.answers[8].name.equal(full[len(full)-8:]) {
		t.Fatalf("8 levels: %+v, %v; want 9 answers, the last for %v", m, err, full[len(full)-8:])
	}
	if _, err := unpackMessage(labelByLabel(a, len(full))); !errors.Is(err, errNameBudget) {
		t.Errorf("%d levels: err = %v, want errNameBudget", len(full), err)
	}
}

func BenchmarkUnpackPointerChain(b *testing.B) {
	msg := pointerChain()
	b.ReportAllocs()
	for b.Loop() {
		_, _ = unpackMessage(msg)
	}
}

func BenchmarkUnpackLabelFanOut(b *testing.B) {
	msg := labelFanOut()
	b.ReportAllocs()
	for b.Loop() {
		_, _ = unpackMessage(msg)
	}
}

func TestNameTooLong(t *testing.T) {
	long := name{strings.Repeat("a", 63), strings.Repeat("b", 63), strings.Repeat("c", 63), strings.Repeat("d", 63)}
	if _, err := packMessage(&message{questions: []question{{long, typeA, classIN}}}); err == nil {
		t.Error("pack: no error for a 257-byte name")
	}
	// The same name on the wire is refused when reading.
	msg := []byte{0, 0, 0, 0, 0, 1, 0, 0, 0, 0, 0, 0}
	for _, l := range long {
		msg = append(msg, byte(len(l)))
		msg = append(msg, l...)
	}
	msg = append(msg, 0, 0, 1, 0, 1)
	if _, err := unpackMessage(msg); err == nil {
		t.Error("unpack: no error for a 257-byte name")
	}
}

func TestSkipsUnknownRecords(t *testing.T) {
	b := must(buildResponse(testService(), testAddrs(), recordTTL))
	// Append a record of a type this package does not read (NSEC, root name) and one of another class.
	msg := append(slices.Clone(b),
		0, 0, 47, 0, 1, 0, 0, 0, 120, 0, 3, 1, 2, 3,
		0, 0, 1, 0, 3, 0, 0, 0, 120, 0, 4, 192, 0, 2, 99)
	msg[11] += 2 // two more additionals
	es := parseEntries(msg, "_persona-hub._tcp")
	if len(es) != 1 || es[0].Port != 8443 || len(es[0].Addrs) != 3 {
		t.Fatalf("got %+v", es)
	}
}

// TestRandomBytes feeds garbage and mutated packets to every parser: nothing may panic.
func TestRandomBytes(t *testing.T) {
	rng := rand.New(rand.NewPCG(1, 2))
	valid := must(buildResponse(testService(), testAddrs(), recordTTL))
	typ := mustType(t, "_persona-hub._tcp")
	br := newBrowser(typ, "")
	now := time.Now()
	for i := range 20000 {
		var b []byte
		switch i % 3 {
		case 0:
			b = make([]byte, rng.IntN(600))
			for j := range b {
				b[j] = byte(rng.UintN(256))
			}
		case 1:
			b = slices.Clone(valid)
			for range 1 + rng.IntN(8) {
				b[rng.IntN(len(b))] = byte(rng.UintN(256))
			}
		default:
			b = slices.Clone(valid[:rng.IntN(len(valid)+1)])
		}
		parseEntries(b, "_persona-hub._tcp")
		if m, err := unpackMessage(b); err == nil {
			br.apply(m, "eth0", now)
			br.sweep(now.Add(time.Duration(rng.IntN(300)) * time.Second))
		}
	}
}

// FuzzParseEntries: nothing may panic (go test runs the seeds; go test -fuzz explores).
func FuzzParseEntries(f *testing.F) {
	f.Add(must(buildResponse(testService(), testAddrs(), recordTTL)))
	f.Add(must(buildResponse(testService(), nil, 0)))
	f.Add([]byte{0, 0, 0x84, 0, 0, 0, 0, 1, 0, 0, 0, 0, 0xC0, 12})
	f.Fuzz(func(t *testing.T, b []byte) {
		parseEntries(b, "_persona-hub._tcp")
		if m, err := unpackMessage(b); err == nil {
			newBrowser(name{"_persona-hub", "_tcp", "local"}, "").apply(m, "eth0", time.Now())
		}
	})
}

func must(b []byte, err error) []byte {
	if err != nil {
		panic(err)
	}
	return b
}
