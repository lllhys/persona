// Package mdns is a minimal Multicast DNS (RFC 6762) / DNS-SD (RFC 6763) responder and browser: enough for hubs on
// the same LAN to see each other. It is not a general-purpose implementation; the subset is:
//
// Responder (Options.Advertise):
//   - answers PTR queries for its service type and for the "_services._dns-sd._udp.local." enumeration, SRV / TXT
//     queries for its instance and A / AAAA queries for its host, with the addresses of the interface the query came
//     in on; answers go to the multicast group (the QU bit is ignored), legacy unicast queries (source port not
//     5353, §6.7) get a unicast reply with the query id, the questions and TTLs of at most 10 s, but only when the
//     sender is on the receiving interface's link (inside one of its prefixes, or IPv6 link-local; §5.5, §11) and
//     at most once a second per sender address and answer set, so a spoofed query cannot turn the host into a
//     reflector towards some other address;
//   - the asked-for records are answers, the ones a querier needs next (SRV, TXT, addresses) are additional records;
//   - announces twice on start, 1 s apart (§8.3), once more on an interface that appears later, and sends goodbyes
//     (TTL 0, §10.1) when it stops;
//   - the same answer is multicast at most once a second per interface (§6, simplified).
//
// Browser (Options.Browse):
//   - sends a PTR query at once, after 1 s and then every Options.QueryEvery;
//   - caches PTR / SRV / TXT / A / AAAA per instance across packets, asks for SRV / TXT or addresses still missing,
//     reports an instance once its SRV is known, again when it changes, and with TTL 0 on a goodbye or when its
//     records expire;
//   - ignores responses whose source port is not 5353 (§11) and the records of its own advertised instance.
//
// Not implemented: probing and conflict resolution (§8.1, §8.2, §9), known-answer suppression (§7.1), duplicate
// question / answer suppression (§7.3, §7.4), randomized response delays (§6), NSEC negative answers (§6.1), the
// cache-flush bit's one-second rule (§10.2: the bit is set on unique records but ignored when reading), truncated
// (TC) continuation, querier-side refresh at 80% of the TTL (§5.2; the periodic browse query does that job),
// subtypes, and unicast DNS. It binds 5353 next to the system's responder (mDNSResponder, avahi) with
// SO_REUSEADDR + SO_REUSEPORT instead of registering with it. Multicast does not cross a Docker bridge network.
package mdns

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"slices"
	"strconv"
	"sync"
	"syscall"
	"time"

	"golang.org/x/net/ipv4"
	"golang.org/x/net/ipv6"
)

// Service is what this host advertises.
type Service struct {
	Instance string            // unique instance label, e.g. the hub_id (UTF-8, <= 63 bytes; longer is cut)
	Type     string            // e.g. "_persona-hub._tcp" (without ".local.")
	Host     string            // host label without ".local.", e.g. "persona-1a2b3c4d"; empty: derived from Instance
	Port     int               // 1..65535
	TXT      map[string]string // key=value pairs; keep it small (each pair <= 255 bytes, keys printable ASCII without '=')
}

// Entry is one instance seen on the network.
type Entry struct {
	Instance string            // the raw instance label (may contain spaces and dots; nothing escaped)
	Host     string            // e.g. "persona-1a2b3c4d.local." (presentation format: '.' and '\' escaped, space as \032)
	Addrs    []netip.Addr      // A / AAAA seen for Host, sorted (IPv6 link-local carries the receiving interface as zone)
	Port     int               // from the SRV record
	TXT      map[string]string // keys lowercased; a string without '=' is a key with an empty value
	TTL      time.Duration     // from the records; 0 means a goodbye (the instance left or its records expired)
	Seen     time.Time         // when its records last arrived
}

// Options configures Run.
type Options struct {
	Advertise  *Service                         // nil: browse only
	Browse     string                           // service type to look for; "" = don't browse
	Found      func(Entry)                      // called (from Run's goroutine; must not block long) whenever an instance's records arrive or change, and with TTL 0 on goodbye
	Interfaces []net.Interface                  // nil: every up, running, multicast-capable interface that is neither loopback nor point-to-point (re-checked every minute)
	QueryEvery time.Duration                    // how often to send a browse query; default 60s, at least 1s (first query at once, a second after 1s)
	Logf       func(format string, args ...any) // nil: silent; may be called from several goroutines
}

const (
	mdnsPort    = 5353
	closeLinger = 50 * time.Millisecond
)

var (
	groupV4 = &net.UDPAddr{IP: net.IPv4(224, 0, 0, 251), Port: mdnsPort}
	groupV6 = &net.UDPAddr{IP: net.ParseIP("ff02::fb"), Port: mdnsPort}
)

// Run advertises and browses until ctx ends. It answers PTR queries for Type (and the "_services._dns-sd._udp.local."
// meta query), SRV/TXT queries for the instance and A/AAAA queries for Host with the interface's own addresses,
// announces on start (twice, 1 s apart) and sends goodbyes (TTL 0) when ctx ends. Errors binding one family (IPv6
// off) are logged, not fatal; Run returns an error when neither IPv4 nor IPv6 multicast could be set up, or at once
// when the options are invalid (bad Service, bad Browse type, nothing to advertise or browse). When it returns, its
// sockets are closed and its goroutines have exited.
func Run(ctx context.Context, o Options) error {
	r, err := newRunner(o)
	if err != nil {
		return err
	}
	if err := r.listen(); err != nil {
		return err
	}
	defer r.stop()
	if r.p4 != nil {
		r.wg.Go(func() { r.read(false) })
	}
	if r.p6 != nil {
		r.wg.Go(func() { r.read(true) })
	}
	r.loop(ctx)
	return nil
}

type runner struct {
	o    Options
	svc  *served  // nil: not advertising
	br   *browser // nil: not browsing
	logf func(string, ...any)

	readAddrs func(*net.Interface) []netip.Addr // ifaceAddrs; tests count the reads

	c4, c6 net.PacketConn
	p4     *ipv4.PacketConn
	p6     *ipv6.PacketConn

	pkts chan packet
	done chan struct{}
	wg   sync.WaitGroup

	// The legacy unicast reply path; tests replace both.
	prefixes func(*net.Interface) []netip.Prefix             // ifacePrefixes
	unicast  func(b []byte, v6 bool, dst *net.UDPAddr) error // writeUnicast

	// Owned by the loop goroutine.
	links    map[int]*link           // by interface index
	logged   map[string]string       // last error logged per key, so a persistent one is logged once
	recent   map[recentKey]time.Time // last answer per interface / family / asked set, or per sender / asked set (legacy)
	lastSend time.Time
}

// link is an interface the group is joined on.
type link struct {
	ifi      net.Interface
	on4, on6 bool
}

// recentKey names an answer for the once-a-second limit: a multicast answer on one interface and family, or a legacy
// unicast reply to one sender.
type recentKey struct {
	ifindex int
	v6      bool
	want    asked
	src     netip.Addr // the sender of a legacy unicast query; zero for multicast
}

type packet struct {
	data    []byte
	ifindex int // 0: unknown
	src     *net.UDPAddr
	v6      bool
}

func newRunner(o Options) (*runner, error) {
	r := &runner{
		o:         o,
		logf:      o.Logf,
		readAddrs: ifaceAddrs,
		pkts:      make(chan packet, 64),
		done:      make(chan struct{}),
		links:     map[int]*link{},
		logged:    map[string]string{},
		recent:    map[recentKey]time.Time{},
	}
	if r.logf == nil {
		r.logf = func(string, ...any) {}
	}
	r.prefixes, r.unicast = ifacePrefixes, r.writeUnicast
	if o.Advertise != nil {
		s, err := newServed(*o.Advertise)
		if err != nil {
			return nil, err
		}
		r.svc = s
	}
	if o.Browse != "" {
		typ, err := parseType(o.Browse)
		if err != nil {
			return nil, err
		}
		self := ""
		if r.svc != nil && r.svc.typ.equal(typ) {
			self = r.svc.instance
		}
		r.br = newBrowser(typ, self)
	}
	if r.svc == nil && r.br == nil {
		return nil, errors.New("mdns: nothing to advertise or browse")
	}
	if r.o.QueryEvery <= 0 {
		r.o.QueryEvery = time.Minute
	}
	r.o.QueryEvery = max(r.o.QueryEvery, time.Second)
	return r, nil
}

func (r *runner) listen() error {
	lc := net.ListenConfig{Control: reuseControl}
	ctx := context.Background()
	port := ":" + strconv.Itoa(mdnsPort)
	c4, err4 := lc.ListenPacket(ctx, "udp4", "0.0.0.0"+port)
	if err4 == nil {
		p := ipv4.NewPacketConn(c4)
		if err := p.SetControlMessage(ipv4.FlagInterface, true); err != nil {
			r.logf("mdns: ipv4: no receiving interface, queries go unanswered: %v", err)
		}
		r.note("ipv4 ttl", p.SetMulticastTTL(255))
		r.note("ipv4 loopback", p.SetMulticastLoopback(true))
		r.c4, r.p4 = c4, p
	} else {
		r.logf("mdns: ipv4: %v", err4)
	}
	c6, err6 := lc.ListenPacket(ctx, "udp6", "[::]"+port)
	if err6 == nil {
		p := ipv6.NewPacketConn(c6)
		if err := p.SetControlMessage(ipv6.FlagInterface, true); err != nil {
			r.logf("mdns: ipv6: no receiving interface, queries go unanswered: %v", err)
		}
		r.note("ipv6 hop limit", p.SetMulticastHopLimit(255))
		r.note("ipv6 loopback", p.SetMulticastLoopback(true))
		r.c6, r.p6 = c6, p
	} else {
		r.logf("mdns: ipv6: %v", err6)
	}
	if r.p4 == nil && r.p6 == nil {
		return fmt.Errorf("mdns: no multicast socket (ipv4: %v; ipv6: %v)", err4, err6)
	}
	return nil
}

func (r *runner) stop() {
	close(r.done)
	if r.c4 != nil {
		r.c4.Close()
	}
	if r.c6 != nil {
		r.c6.Close()
	}
	r.wg.Wait()
}

// read hands every packet of one socket to the loop until the socket is closed.
func (r *runner) read(v6 bool) {
	buf := make([]byte, maxPacket+1) // one spare byte tells an oversized datagram from a full one
	for {
		var (
			n, ifindex int
			src        net.Addr
			err        error
		)
		if v6 {
			var cm *ipv6.ControlMessage
			n, cm, src, err = r.p6.ReadFrom(buf)
			if cm != nil {
				ifindex = cm.IfIndex
			}
		} else {
			var cm *ipv4.ControlMessage
			n, cm, src, err = r.p4.ReadFrom(buf)
			if cm != nil {
				ifindex = cm.IfIndex
			}
		}
		if err != nil {
			select {
			case <-r.done:
				return
			default:
			}
			if errors.Is(err, net.ErrClosed) {
				return
			}
			r.logf("mdns: read %s: %v", family(v6), err)
			select {
			case <-r.done:
				return
			case <-time.After(100 * time.Millisecond):
			}
			continue
		}
		ua, ok := src.(*net.UDPAddr)
		if !ok || n < 12 || n > maxPacket {
			continue
		}
		p := packet{data: slices.Clone(buf[:n]), ifindex: ifindex, src: ua, v6: v6}
		select {
		case r.pkts <- p:
		case <-r.done:
			return
		default: // the loop is behind: drop rather than stall the socket
		}
	}
}

func (r *runner) loop(ctx context.Context) {
	r.refreshLinks()
	now := time.Now()
	nextLinks := now.Add(time.Minute)
	announces, nextAnnounce := 0, now
	if r.svc != nil {
		announces = 2
	}
	queries, nextQuery := 0, now
	timer := time.NewTimer(0)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			if r.svc != nil {
				r.announce(r.allLinks(), 0)
			}
			// On macOS, closing the socket while its own multicast is still being looped back stalls close(2) for
			// about a second; a short pause after the last send avoids it.
			if d := closeLinger - time.Since(r.lastSend); d > 0 {
				time.Sleep(d)
			}
			return
		case p := <-r.pkts:
			r.handle(p, time.Now())
		case <-timer.C:
		}
		now = time.Now()
		if announces > 0 && !now.Before(nextAnnounce) {
			r.announce(r.allLinks(), recordTTL)
			announces--
			nextAnnounce = now.Add(time.Second)
		}
		if r.br != nil && !now.Before(nextQuery) {
			r.query(r.allLinks(), r.browseQuestion())
			queries++
			if queries == 1 {
				nextQuery = now.Add(time.Second)
			} else {
				nextQuery = now.Add(r.o.QueryEvery)
			}
		}
		if !now.Before(nextLinks) {
			if fresh := r.refreshLinks(); len(fresh) > 0 {
				if r.svc != nil {
					r.announce(fresh, recordTTL)
				}
				if r.br != nil {
					r.query(fresh, r.browseQuestion())
				}
			}
			nextLinks = now.Add(time.Minute)
		}
		next := nextLinks
		if announces > 0 {
			next = earlier(next, nextAnnounce)
		}
		if r.br != nil {
			r.report(r.br.sweep(now))
			next = earlier(next, nextQuery)
			next = earlier(next, r.br.next())
		}
		timer.Reset(max(time.Until(next), 0))
	}
}

func (r *runner) browseQuestion() []question {
	return []question{{name: r.br.typ, typ: typePTR, class: classIN}}
}

func (r *runner) handle(p packet, now time.Time) {
	l := r.links[p.ifindex]
	if l == nil && p.ifindex != 0 {
		return // an interface we do not serve (Linux delivers every interface's multicast to a wildcard socket)
	}
	m, err := unpackMessage(p.data)
	if err != nil || m.flags&opcodeMask != 0 {
		return
	}
	if !m.isResponse() {
		if r.svc != nil && l != nil {
			r.answer(l, p, m, now)
		}
		return
	}
	if r.br == nil || p.src.Port != mdnsPort {
		return
	}
	zone := ""
	if l != nil {
		zone = l.ifi.Name
	}
	found, follow := r.br.apply(m, zone, now)
	r.report(found)
	if len(follow) > 0 {
		links := r.allLinks()
		if l != nil {
			links = []*link{l}
		}
		r.query(links, follow)
	}
}

func (r *runner) answer(l *link, p packet, m *message, now time.Time) {
	want := r.svc.match(m.questions)
	if want == (asked{}) {
		return
	}
	if p.src.Port == mdnsPort && r.answeredRecently(recentKey{ifindex: l.ifi.Index, v6: p.v6, want: want}, now) {
		return
	}
	if p.src.Port != mdnsPort && !r.legacyReplyAllowed(l, p, want, now) {
		return
	}
	var addrs []netip.Addr
	if want.needsAddrs() {
		addrs = r.readAddrs(&l.ifi)
	}
	ans, add := r.svc.build(want, addrs, recordTTL)
	if len(ans) == 0 {
		return
	}
	if p.src.Port != mdnsPort {
		// Legacy unicast query (§6.7): reply to the sender only, with its id and questions, short TTLs and no
		// cache-flush bits.
		for _, sec := range [][]record{ans, add} {
			for i := range sec {
				sec[i].ttl = min(sec[i].ttl, 10)
				sec[i].class &^= topBit
			}
		}
		qs := slices.Clone(m.questions)
		for i := range qs {
			qs[i].class &^= topBit
		}
		b, err := packMessage(&message{id: m.id, flags: flagResponse | flagAA, questions: qs, answers: ans, additionals: add})
		if err != nil {
			r.note("pack reply", err)
			return
		}
		r.note("unicast reply", r.unicast(b, p.v6, p.src))
		return
	}
	b, err := packMessage(&message{flags: flagResponse | flagAA, answers: ans, additionals: add})
	if err != nil {
		r.note("pack answer", err)
		return
	}
	r.send(l, p.v6, b)
}

// legacyReplyAllowed reports whether a legacy unicast reply to the records in want may go to p.src. The reply goes to
// whatever source address the query claims, so the sender must be on the link the query came in on (RFC 6762 §5.5,
// using the §11 on-link test); otherwise anyone who can reach port 5353 could spoof a small query and have the much
// larger reply sent to a third party. A sender on the link gets the same answer at most once a second.
func (r *runner) legacyReplyAllowed(l *link, p packet, want asked, now time.Time) bool {
	src, ok := netip.AddrFromSlice(p.src.IP)
	if !ok {
		return false
	}
	src = src.Unmap()
	if !onLink(src, r.prefixes(&l.ifi)) {
		return false
	}
	return !r.answeredRecently(recentKey{want: want, src: src}, now)
}

// onLink reports whether src is on the link whose prefixes are given (RFC 6762 §11): inside one of them, or an IPv6
// link-local address, which is always on-link.
func onLink(src netip.Addr, prefixes []netip.Prefix) bool {
	if src.Is6() && src.IsLinkLocalUnicast() {
		return true
	}
	for _, pfx := range prefixes {
		if pfx.Contains(src) {
			return true
		}
	}
	return false
}

// writeUnicast sends b to dst from the socket of its family.
func (r *runner) writeUnicast(b []byte, v6 bool, dst *net.UDPAddr) error {
	var err error
	if v6 {
		_, err = r.p6.WriteTo(b, nil, dst)
	} else {
		_, err = r.p4.WriteTo(b, nil, dst)
	}
	return err
}

// answeredRecently reports whether the answer key went out less than a second ago, and otherwise counts it as sent
// now (§6). It runs before the interface's addresses are read, so a burst of the same query costs one read.
func (r *runner) answeredRecently(key recentKey, now time.Time) bool {
	if t, ok := r.recent[key]; ok && now.Sub(t) < time.Second {
		return true
	}
	if len(r.recent) > 64 {
		for k, t := range r.recent {
			if now.Sub(t) >= time.Second {
				delete(r.recent, k)
			}
		}
	}
	r.recent[key] = now
	return false
}

func (r *runner) report(es []Entry) {
	if r.o.Found == nil {
		return
	}
	for _, e := range es {
		r.o.Found(e)
	}
}

// announce multicasts the service on links, each with its own addresses (ttl 0: goodbye).
func (r *runner) announce(links []*link, ttl uint32) {
	r.sendLinks(links, func(l *link) []byte {
		b, err := r.svc.response(r.readAddrs(&l.ifi), ttl)
		r.note("pack announcement", err)
		return b
	})
}

func (r *runner) query(links []*link, qs []question) {
	b, err := packMessage(&message{questions: qs})
	r.note("pack query", err)
	if err != nil {
		return
	}
	r.sendLinks(links, func(*link) []byte { return b })
}

func (r *runner) sendLinks(links []*link, build func(*link) []byte) {
	for _, l := range links {
		if !l.on4 && !l.on6 {
			continue
		}
		b := build(l)
		if b == nil {
			continue
		}
		if l.on4 {
			r.send(l, false, b)
		}
		if l.on6 {
			r.send(l, true, b)
		}
	}
}

func (r *runner) send(l *link, v6 bool, b []byte) {
	var err error
	if v6 {
		if err = r.p6.SetMulticastInterface(&l.ifi); err == nil {
			_, err = r.p6.WriteTo(b, nil, groupV6)
		}
	} else {
		if err = r.p4.SetMulticastInterface(&l.ifi); err == nil {
			_, err = r.p4.WriteTo(b, nil, groupV4)
		}
	}
	r.lastSend = time.Now()
	r.note("send "+family(v6)+" on "+l.ifi.Name, err)
}

func (r *runner) allLinks() []*link {
	out := make([]*link, 0, len(r.links))
	for _, l := range r.links {
		out = append(out, l)
	}
	slices.SortFunc(out, func(a, b *link) int { return a.ifi.Index - b.ifi.Index })
	return out
}

func (r *runner) candidates() []net.Interface {
	if r.o.Interfaces != nil {
		return r.o.Interfaces
	}
	all, err := net.Interfaces()
	r.note("list interfaces", err)
	var out []net.Interface
	for _, ifi := range all {
		f := ifi.Flags
		if f&net.FlagUp == 0 || f&net.FlagRunning == 0 || f&net.FlagMulticast == 0 ||
			f&net.FlagLoopback != 0 || f&net.FlagPointToPoint != 0 {
			continue
		}
		out = append(out, ifi)
	}
	return out
}

// refreshLinks joins the group on interfaces that appeared (or got an address of a family) and leaves the ones
// that went away. It returns the links with a new membership.
func (r *runner) refreshLinks() (fresh []*link) {
	seen := map[int]bool{}
	for _, ifi := range r.candidates() {
		seen[ifi.Index] = true
		l := r.links[ifi.Index]
		if l == nil {
			l = &link{}
			r.links[ifi.Index] = l
		}
		l.ifi = ifi
		has4, has6 := false, false
		for _, a := range r.readAddrs(&ifi) {
			if a.Is4() {
				has4 = true
			} else {
				has6 = true
			}
		}
		n4 := r.join(l, false, has4)
		n6 := r.join(l, true, has6)
		if n4 || n6 {
			fresh = append(fresh, l)
		}
	}
	for idx, l := range r.links {
		if !seen[idx] {
			r.leave(l, false)
			r.leave(l, true)
			delete(r.links, idx)
		}
	}
	return fresh
}

// join keeps the group joined on l for one family while want (the interface has an address of that family) and
// reports whether the membership is new. Joining again every minute also repairs a membership the system dropped.
func (r *runner) join(l *link, v6, want bool) bool {
	if (v6 && r.p6 == nil) || (!v6 && r.p4 == nil) {
		return false
	}
	if !want {
		r.leave(l, v6)
		return false
	}
	var err error
	if v6 {
		err = r.p6.JoinGroup(&l.ifi, groupV6)
	} else {
		err = r.p4.JoinGroup(&l.ifi, groupV4)
	}
	on := &l.on4
	if v6 {
		on = &l.on6
	}
	key := "join " + family(v6) + " on " + l.ifi.Name
	switch {
	case err == nil:
		*on = true
		r.note(key, nil)
		return true
	case errors.Is(err, syscall.EADDRINUSE): // already a member
		*on = true
		r.note(key, nil)
		return false
	default:
		*on = false
		r.note(key, err)
		return false
	}
}

func (r *runner) leave(l *link, v6 bool) {
	switch {
	case v6 && l.on6:
		_ = r.p6.LeaveGroup(&l.ifi, groupV6)
		l.on6 = false
	case !v6 && l.on4:
		_ = r.p4.LeaveGroup(&l.ifi, groupV4)
		l.on4 = false
	}
}

// note logs err under key unless the same error was the last one logged for it; nil clears the key.
func (r *runner) note(key string, err error) {
	if err == nil {
		delete(r.logged, key)
		return
	}
	msg := err.Error()
	if r.logged[key] == msg {
		return
	}
	r.logged[key] = msg
	r.logf("mdns: %s: %v", key, err)
}

// ifaceAddrs lists the interface's addresses without loopback / unspecified ones and without zones.
func ifaceAddrs(ifi *net.Interface) []netip.Addr {
	as, err := ifi.Addrs()
	if err != nil {
		return nil
	}
	var out []netip.Addr
	for _, a := range as {
		var ip net.IP
		switch v := a.(type) {
		case *net.IPNet:
			ip = v.IP
		case *net.IPAddr:
			ip = v.IP
		}
		addr, ok := netip.AddrFromSlice(ip)
		if !ok {
			continue
		}
		addr = addr.Unmap()
		if addr.IsLoopback() || addr.IsUnspecified() {
			continue
		}
		out = append(out, addr)
	}
	return out
}

// ifacePrefixes lists the interface's address prefixes (address and mask), loopback ones included; the legacy
// unicast reply path checks senders against them.
func ifacePrefixes(ifi *net.Interface) []netip.Prefix {
	as, err := ifi.Addrs()
	if err != nil {
		return nil
	}
	var out []netip.Prefix
	for _, a := range as {
		n, ok := a.(*net.IPNet)
		if !ok {
			continue
		}
		addr, ok := netip.AddrFromSlice(n.IP)
		if !ok {
			continue
		}
		addr = addr.Unmap()
		ones, bits := n.Mask.Size()
		if bits != addr.BitLen() { // a non-canonical mask, or one of the other family
			continue
		}
		out = append(out, netip.PrefixFrom(addr, ones))
	}
	return out
}

func family(v6 bool) string {
	if v6 {
		return "ipv6"
	}
	return "ipv4"
}
