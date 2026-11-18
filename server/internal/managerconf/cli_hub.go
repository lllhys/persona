package managerconf

import (
	"context"
	"net"
	"net/netip"
)

// CLIHubURL is where the hub's own commands on its machine (`hub pair`, `hub admin`, `hub peer`, `hub setup`,
// `hub doctor`, `extension`) call it with the master token, from hub.json's listen (#9182): the address listen names
// when it is loopback or one of this machine's, else loopback (the hub listens on every address, or listen names a
// host, which is not resolved). An address this machine does not have is never called: it may be another machine's
// by now (a DHCP lease moved on, and the hub's socket binds it anyway: FreeBind), and the token must not leave this
// machine. Its port, or the default one when listen is not host:port. Dial with CLIDial.
func CLIHubURL(listen string) string {
	host, port, err := net.SplitHostPort(listen)
	if err != nil {
		return "http://127.0.0.1:18660"
	}
	if ip, err := netip.ParseAddr(host); err == nil && ip.Zone() == "" && !ip.IsUnspecified() {
		if ip = ip.Unmap(); ip.IsLoopback() || ownAddress(ip) {
			return "http://" + net.JoinHostPort(ip.String(), port)
		}
	}
	return "http://" + net.JoinHostPort("127.0.0.1", port)
}

// CLIDial dials CLIHubURL's address: one of this machine's other than loopback from loopback (127.0.0.1, or ::1 for
// IPv6), which the kernel delivers on this machine like any connection to its own address. The hub takes the master
// token from its own machine's commands over loopback only (pairing codes, admin, peers, extension installs), and a
// connection from this machine to its LAN address would come from that address, which is not loopback. A system that
// does not send from loopback to its own address gets the plain dial: the hub answers then too, and says why when it
// wants loopback.
func CLIDial(ctx context.Context, network, addr string) (net.Conn, error) {
	var plain net.Dialer
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return plain.DialContext(ctx, network, addr)
	}
	ip, err := netip.ParseAddr(host)
	if err != nil || ip.Unmap().IsLoopback() || !ownAddress(ip.Unmap()) {
		return plain.DialContext(ctx, network, addr)
	}
	from := &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1)}
	if ip.Unmap().Is6() {
		from = &net.TCPAddr{IP: net.IPv6loopback}
	}
	loopback := net.Dialer{LocalAddr: from}
	c, err := loopback.DialContext(ctx, network, addr)
	if err == nil || ctx.Err() != nil {
		return c, err
	}
	return plain.DialContext(ctx, network, addr)
}

// interfaceAddrs is net.InterfaceAddrs (tests replace it).
var interfaceAddrs = net.InterfaceAddrs

// ownAddress: ip is on one of this machine's interfaces.
func ownAddress(ip netip.Addr) bool {
	addrs, err := interfaceAddrs()
	if err != nil {
		return false
	}
	for _, a := range addrs {
		if n, ok := a.(*net.IPNet); ok {
			if own, ok := netip.AddrFromSlice(n.IP); ok && own.Unmap() == ip {
				return true
			}
		}
	}
	return false
}
