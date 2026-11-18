package osproc

import (
	"net"
	"os"
	"syscall"
	"time"
)

// Listen is net.Listen for a stream network ("tcp", "tcp4", "tcp6", "unix"), except that no process this one starts
// can inherit a connection the listener accepts. Where the system accepts a connection and makes it close-on-exec in
// one call (accept4 on Linux; Windows makes no socket inheritable) that is net.Listen's listener as it is. On macOS
// they are two calls, and a process started between them keeps the connection open for as long as it lives, whoever
// closes it here (accept_darwin.go, #7765). Every listener in the server module comes from Listen or FileListener
// (archguard's listen_test.go). The listener is a *TCPListener or a *UnixListener.
func Listen(network, address string) (net.Listener, error) {
	ln, err := net.Listen(network, address)
	if err != nil {
		return nil, err
	}
	return sealAccepts(ln)
}

// FileListener is net.FileListener (a listener on a copy of f's socket; f stays open) with Listen's accept.
func FileListener(f *os.File) (net.Listener, error) {
	ln, err := net.FileListener(f)
	if err != nil {
		return nil, err
	}
	return sealAccepts(ln)
}

// TCPListener is a TCP listener from Listen or FileListener, with net.TCPListener's methods: it accepts, takes
// deadlines and closes through its acceptor. It keeps net's listener to itself, so nothing accepts around the acceptor.
type TCPListener struct {
	ln *net.TCPListener
	a  acceptor
}

// UnixListener is a Unix listener from Listen or FileListener, with net.UnixListener's methods, as TCPListener is.
type UnixListener struct {
	ln *net.UnixListener
	a  acceptor
}

// acceptor accepts for a listener from Listen: the listener itself where the system makes a connection close-on-exec
// as it accepts it (accept_other.go), a copy of its socket on macOS (accept_darwin.go).
type acceptor interface {
	Accept() (net.Conn, error)
	SetDeadline(time.Time) error
	Close() error
}

// socketListener is what newAcceptor needs of a listener: *net.TCPListener and *net.UnixListener have it all.
type socketListener interface {
	acceptor
	Addr() net.Addr
	File() (*os.File, error)
}

// sealAccepts gives ln this system's acceptor, or closes it.
func sealAccepts(ln net.Listener) (net.Listener, error) {
	switch l := ln.(type) {
	case *net.TCPListener:
		a, err := newAcceptor(l)
		if err != nil {
			_ = l.Close()
			return nil, err
		}
		return &TCPListener{ln: l, a: a}, nil
	case *net.UnixListener:
		a, err := newAcceptor(l)
		if err != nil {
			_ = l.Close()
			return nil, err
		}
		return &UnixListener{ln: l, a: a}, nil
	}
	return ln, nil // net.Listen and net.FileListener make no other kind of listener
}

func (l *TCPListener) Accept() (net.Conn, error)             { return l.a.Accept() }
func (l *TCPListener) SetDeadline(t time.Time) error         { return l.a.SetDeadline(t) }
func (l *TCPListener) Close() error                          { return l.a.Close() }
func (l *TCPListener) Addr() net.Addr                        { return l.ln.Addr() }
func (l *TCPListener) File() (*os.File, error)               { return l.ln.File() }
func (l *TCPListener) SyscallConn() (syscall.RawConn, error) { return l.ln.SyscallConn() }

func (l *TCPListener) AcceptTCP() (*net.TCPConn, error) {
	c, err := l.a.Accept()
	if err != nil {
		return nil, err
	}
	return c.(*net.TCPConn), nil
}

func (l *UnixListener) Accept() (net.Conn, error)             { return l.a.Accept() }
func (l *UnixListener) SetDeadline(t time.Time) error         { return l.a.SetDeadline(t) }
func (l *UnixListener) Close() error                          { return l.a.Close() }
func (l *UnixListener) Addr() net.Addr                        { return l.ln.Addr() }
func (l *UnixListener) File() (*os.File, error)               { return l.ln.File() }
func (l *UnixListener) SyscallConn() (syscall.RawConn, error) { return l.ln.SyscallConn() }
func (l *UnixListener) SetUnlinkOnClose(unlink bool)          { l.ln.SetUnlinkOnClose(unlink) }

func (l *UnixListener) AcceptUnix() (*net.UnixConn, error) {
	c, err := l.a.Accept()
	if err != nil {
		return nil, err
	}
	return c.(*net.UnixConn), nil
}
