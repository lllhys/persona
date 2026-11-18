package proxyproto

import (
	"bufio"
	"errors"
	"net"
	"net/netip"
	"sync"
	"time"
)

// DefaultTimeout is how long a trusted upstream gets to send its header when Listener.Timeout is zero. A policy
// value, not a measurement: proxies write the header together with, or right before, the client's first bytes.
const DefaultTimeout = 5 * time.Second

// ErrNoAddress: a trusted upstream relayed a connection without saying whose (v1 UNKNOWN, v2 PROXY with UNSPEC). The
// spec lets a receiver use the connection's own address then, but that is the upstream's: for an upstream on this
// machine, loopback, the very trust the header exists to take away from relayed connections. Refused instead.
var ErrNoAddress = errors.New("proxyproto: the header names no client address")

// Listener reads a PROXY protocol header first on every connection whose peer is in Trusted, and on no other.
//
//   - A connection from a trusted upstream must start with a valid header, or it is closed: were it served, anything
//     that reaches the hub through the upstream without a header (a forward that bypasses the proxy, a proxy with the
//     header switched off) would get the upstream's own address, loopback for one on this machine.
//   - From anyone else a header is not read at all: those bytes are the connection's first, and a TLS or HTTP server
//     rejects them as garbage. Nobody outside Trusted can name an address, and existing clients see no change.
//
// The header is read in the connection's first Read, Write or RemoteAddr call, in that connection's goroutine, under a
// deadline (Timeout): Accept never waits for it, so a slow or silent upstream connection holds up no other.
type Listener struct {
	net.Listener
	Trusted []netip.Prefix
	Timeout time.Duration
	// Report, when set, hears once about each connection from a trusted upstream: its header, or why it was refused
	// (err != nil; the connection is closed by then).
	Report func(peer netip.AddrPort, h Header, err error)
}

// Accept wraps connections from trusted upstreams; others are returned as they are.
func (l *Listener) Accept() (net.Conn, error) {
	c, err := l.Listener.Accept()
	if err != nil {
		return nil, err
	}
	peer := addrPort(c.RemoteAddr())
	if !peer.IsValid() || !l.trusts(peer.Addr()) {
		return c, nil
	}
	timeout := l.Timeout
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	return &Conn{Conn: c, peer: peer, timeout: timeout, report: l.Report}, nil
}

func (l *Listener) trusts(ip netip.Addr) bool {
	for _, p := range l.Trusted {
		if p.Contains(ip) {
			return true
		}
	}
	return false
}

func addrPort(a net.Addr) netip.AddrPort {
	t, ok := a.(*net.TCPAddr)
	if !ok {
		return netip.AddrPort{}
	}
	ap := t.AddrPort()
	return netip.AddrPortFrom(ap.Addr().Unmap().WithZone(""), ap.Port())
}

// Conn is a connection from a trusted upstream; RemoteAddr is the client its header names.
type Conn struct {
	net.Conn
	peer    netip.AddrPort
	timeout time.Duration
	report  func(netip.AddrPort, Header, error)

	once   sync.Once
	header Header
	remote net.Addr
	err    error

	mu             sync.Mutex
	rest           []byte    // read past the header along with it, handed out before the socket's
	parsing        bool      // the header's own deadline is on the socket
	headerDeadline time.Time // fixed limit while the header is being read
	readDeadline   time.Time // the caller's, applied when the header is in
}

// Header is the connection's PROXY header, once read (Read, Write or RemoteAddr), and why it was refused.
func (c *Conn) Header() (Header, error) {
	c.once.Do(c.readHeader)
	return c.header, c.err
}

func (c *Conn) readHeader() {
	limit := time.Now().Add(c.timeout)
	c.mu.Lock()
	c.parsing = true
	c.headerDeadline = limit
	if !c.readDeadline.IsZero() && c.readDeadline.Before(limit) {
		limit = c.readDeadline
	}
	_ = c.Conn.SetReadDeadline(limit)
	c.mu.Unlock()

	br := bufio.NewReaderSize(c.Conn, 512)
	h, err := Read(br)
	if err == nil && !h.Local && !h.Source.IsValid() {
		err = ErrNoAddress
	}

	c.mu.Lock()
	c.parsing = false
	_ = c.Conn.SetReadDeadline(c.readDeadline)
	if err == nil && br.Buffered() > 0 {
		c.rest, _ = br.Peek(br.Buffered())
	}
	c.mu.Unlock()

	c.remote = c.Conn.RemoteAddr()
	if err != nil {
		c.err = err
		_ = c.Conn.Close()
	} else {
		c.header = h
		if src := h.Source; !h.Local && (!src.Addr().Unmap().IsLoopback() || c.peer.Addr().IsLoopback()) {
			// A loopback client is the upstream's own: this machine's only when the upstream is on it. From an
			// upstream elsewhere it stays that upstream's address rather than gaining this machine's loopback trust.
			c.remote = net.TCPAddrFromAddrPort(src)
		}
	}
	if c.report != nil {
		c.report(c.peer, h, err)
	}
}

// RemoteAddr is the client the header names; the upstream's own address for a LOCAL header or a refused connection.
func (c *Conn) RemoteAddr() net.Addr {
	c.once.Do(c.readHeader)
	return c.remote
}

// LocalAddr stays the socket's own, not the header's destination: that address is on the upstream's side (the
// proxy's listener, possibly on another machine), while LocalAddr answers which of this machine's addresses the
// connection arrived on (the hub uses it to tell a VPN interface: ingress.go). Keeping it can only make a relayed
// connection count as less local, never more.
func (c *Conn) LocalAddr() net.Addr { return c.Conn.LocalAddr() }

func (c *Conn) Read(p []byte) (int, error) {
	if _, err := c.Header(); err != nil {
		return 0, err
	}
	c.mu.Lock()
	if len(c.rest) > 0 {
		n := copy(p, c.rest)
		c.rest = c.rest[n:]
		c.mu.Unlock()
		return n, nil
	}
	c.mu.Unlock()
	return c.Conn.Read(p)
}

// Write waits for the header too: nothing goes out before it is known whom to.
func (c *Conn) Write(p []byte) (int, error) {
	if _, err := c.Header(); err != nil {
		return 0, err
	}
	return c.Conn.Write(p)
}

func (c *Conn) SetDeadline(t time.Time) error {
	if err := c.SetReadDeadline(t); err != nil {
		return err
	}
	return c.Conn.SetWriteDeadline(t)
}

// SetReadDeadline applies to pending reads, including the header read, without extending the header's own limit.
// A timed-out header still refuses and closes the connection; extending the deadline afterwards cannot retry it.
func (c *Conn) SetReadDeadline(t time.Time) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.readDeadline = t
	if c.parsing && (t.IsZero() || c.headerDeadline.Before(t)) {
		t = c.headerDeadline
	}
	return c.Conn.SetReadDeadline(t)
}
