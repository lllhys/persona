//go:build darwin

package osproc

import (
	"errors"
	"net"
	"os"
	"sync/atomic"
	"syscall"
	"time"
)

// sealedAcceptor accepts for a listener on macOS. Go's own accept there (internal/poll/sys_cloexec.go) makes the
// connection close-on-exec in a second call after accept(2), without syscall.ForkLock, so a process os/exec starts in
// between (it forks with the lock held for writing) inherits the connection. This one holds the lock for reading from
// accept(2) until the connection is close-on-exec, as Go holds it while it makes a socket or a pipe: a start waits
// those few microseconds instead.
//
// Go leaves the lock out because a socket in blocking mode would hold it until the next connection came, and every
// start with it. Fd on any copy of the socket puts it, and so every copy, in that mode (the resident node's handover
// calls it, once its listener is closed or before it accepts); so the acceptor puts it back in non-blocking mode before
// each accept, as the poller expects it anyway. Only an Fd landing between the two would still leave accept waiting
// with the lock: nothing here calls Fd on a copy of a listener that is accepting. A listener's own raw connection cannot
// wait for a connection (its Read is refused), so the acceptor waits and accepts on a copy of the socket as an
// *os.File, whose raw connection can.
type sealedAcceptor struct {
	ln     socketListener
	dup    *os.File        // the socket again: close-on-exec, in the poller
	rc     syscall.RawConn // dup's
	closed atomic.Bool
}

func newAcceptor(ln socketListener) (acceptor, error) {
	f, err := ln.File()
	if err != nil {
		return nil, err
	}
	rc, err := f.SyscallConn()
	if err != nil {
		_ = f.Close()
		return nil, err
	}
	return &sealedAcceptor{ln: ln, dup: f, rc: rc}, nil
}

func (a *sealedAcceptor) Accept() (net.Conn, error) {
	for {
		var fd int
		var call string
		var errno error
		err := a.rc.Read(func(s uintptr) bool {
			call = "setnonblock"
			if errno = syscall.SetNonblock(int(s), true); errno != nil {
				return true
			}
			call = "accept"
			syscall.ForkLock.RLock()
			fd, _, errno = syscall.Accept(int(s))
			if errno == nil {
				syscall.CloseOnExec(fd)
			}
			syscall.ForkLock.RUnlock()
			return errno != syscall.EAGAIN
		})
		if err != nil { // closed, or past the deadline
			if a.closed.Load() {
				err = net.ErrClosed
			}
			return nil, a.opError(err)
		}
		switch errno {
		case nil:
		case syscall.EINTR, syscall.ECONNABORTED:
			continue // as Go's accept: interrupted, or the connection went away before it was taken
		default:
			return nil, a.opError(os.NewSyscallError(call, errno))
		}
		f := os.NewFile(uintptr(fd), "")
		c, err := net.FileConn(f) // a close-on-exec copy, non-blocking and in the poller like any accepted connection
		_ = f.Close()
		if err != nil {
			return nil, a.opError(err)
		}
		return c, nil
	}
}

// SetDeadline sets the listener's deadline, and the copy's, which Accept waits on.
func (a *sealedAcceptor) SetDeadline(t time.Time) error {
	if err := a.ln.SetDeadline(t); err != nil {
		return err
	}
	return a.dup.SetReadDeadline(t)
}

// Close closes the copy, which ends an Accept waiting on it, then the listener.
func (a *sealedAcceptor) Close() error {
	a.closed.Store(true)
	_ = a.dup.Close()
	return a.ln.Close()
}

// opError says err the way a listener's Accept does, so errors.Is finds net.ErrClosed and os.ErrDeadlineExceeded
// through it as before.
func (a *sealedAcceptor) opError(err error) error {
	var op *net.OpError
	if errors.As(err, &op) {
		err = op.Err // net.FileConn's
	}
	addr := a.ln.Addr()
	return &net.OpError{Op: "accept", Net: addr.Network(), Addr: addr, Err: err}
}
