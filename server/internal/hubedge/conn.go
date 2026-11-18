package hubedge

import (
	"bufio"
	"errors"
	"net"
	"net/netip"
	"os"
	"sync"

	"github.com/nwverse/persona/server/internal/osproc"
	"github.com/nwverse/persona/server/internal/proxyproto"
)

// SlotListener is a slot's end of one of the edge's ports: a Unix socket only the edge connects to. Every connection
// starts with the edge's PROXY v2 header naming the client and the address it connected to; Accept's connections
// answer RemoteAddr and LocalAddr with those, so the hub judges sources (loopback, LAN, VPN interface, a user's own
// PROXY protocol upstream behind the edge) exactly as when it listened itself. Addr is the edge's TCP address, the
// port the hub reports and pins.
type SlotListener struct {
	ln   net.Listener
	addr net.Addr
	path string
}

// ListenSlot listens on path (a stale socket file is removed first) for the edge's port addr.
func ListenSlot(path string, addr net.Addr) (*SlotListener, error) {
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	ln, err := osproc.Listen("unix", path)
	if err != nil {
		return nil, err
	}
	if err := os.Chmod(path, 0o600); err != nil {
		_ = ln.Close()
		return nil, err
	}
	return &SlotListener{ln: ln, addr: addr, path: path}, nil
}

func (l *SlotListener) Accept() (net.Conn, error) {
	c, err := l.ln.Accept()
	if err != nil {
		return nil, err
	}
	return &relayed{Conn: c}, nil
}

func (l *SlotListener) Close() error {
	err := l.ln.Close()
	_ = os.Remove(l.path)
	return err
}

func (l *SlotListener) Addr() net.Addr { return l.addr }

// relayed is one connection the edge relays. The header is read on first use, in the connection's own goroutine, so
// Accept never waits on it.
type relayed struct {
	net.Conn
	once          sync.Once
	br            *bufio.Reader
	remote, local net.Addr
	err           error
}

func (c *relayed) header() error {
	c.once.Do(func() {
		// The edge writes the header before anything it relays, so the caller's own deadlines (the HTTP server's
		// header timeout) bound this read too; none of its own would survive them.
		c.br = bufio.NewReaderSize(c.Conn, 512)
		h, err := proxyproto.Read(c.br)
		switch {
		case err != nil:
			c.err = err
		case h.Local || !h.Source.IsValid():
			c.err = proxyproto.ErrNoAddress
		default:
			c.remote = tcpAddr(h.Source)
			c.local = tcpAddr(h.Destination)
		}
		if c.err != nil {
			_ = c.Conn.Close()
		}
	})
	return c.err
}

func tcpAddr(ap netip.AddrPort) *net.TCPAddr {
	return net.TCPAddrFromAddrPort(netip.AddrPortFrom(ap.Addr().Unmap(), ap.Port()))
}

func (c *relayed) Read(p []byte) (int, error) {
	if err := c.header(); err != nil {
		return 0, err
	}
	if c.br.Buffered() > 0 {
		return c.br.Read(p)
	}
	return c.Conn.Read(p)
}

func (c *relayed) Write(p []byte) (int, error) {
	if err := c.header(); err != nil {
		return 0, err
	}
	return c.Conn.Write(p)
}

func (c *relayed) RemoteAddr() net.Addr {
	if c.header() != nil {
		return &net.TCPAddr{} // refused: nobody's address
	}
	return c.remote
}

func (c *relayed) LocalAddr() net.Addr {
	if c.header() != nil {
		return &net.TCPAddr{}
	}
	return c.local
}

// CloseWrite passes a half-close on (the edge relays it to the client).
func (c *relayed) CloseWrite() error {
	if cw, ok := c.Conn.(interface{ CloseWrite() error }); ok {
		return cw.CloseWrite()
	}
	return nil
}
