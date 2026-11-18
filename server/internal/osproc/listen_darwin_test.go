//go:build darwin

package osproc

import (
	"errors"
	"net"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

// forkLockHold is how long TestAcceptHoldsTheForkLock holds the fork lock: an Accept that does not wait for it takes
// the queued connection well within that.
const forkLockHold = 100 * time.Millisecond

// TestAcceptHoldsTheForkLock: a connection is taken only while no process can be starting. With syscall.ForkLock held
// for writing, as os/exec holds it to fork, Accept waits even though a connection is queued, and takes it once the
// lock is free, close-on-exec.
func TestAcceptHoldsTheForkLock(t *testing.T) {
	ln := listenTCP(t)
	client, err := net.Dial("tcp", ln.Addr().String()) // before the lock: making a socket takes it for reading
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	type result struct {
		c   net.Conn
		err error
	}
	got := make(chan result, 1)
	syscall.ForkLock.Lock()
	locked := true
	defer func() {
		if locked {
			syscall.ForkLock.Unlock()
		}
	}()
	go func() {
		c, err := ln.Accept()
		got <- result{c, err}
	}()
	select {
	case r := <-got:
		if r.c != nil {
			_ = r.c.Close()
		}
		t.Fatalf("Accept returned (%v) while a process could be starting (ForkLock held for writing)", r.err)
	case <-time.After(forkLockHold):
	}
	syscall.ForkLock.Unlock()
	locked = false
	select {
	case r := <-got:
		if r.err != nil {
			t.Fatal(r.err)
		}
		defer r.c.Close()
		if !closeOnExec(t, r.c) {
			t.Fatal("the accepted connection is not close-on-exec")
		}
	case <-time.After(waitLimit):
		t.Fatal("Accept did not take the connection once the lock was free")
	}
}

// TestAcceptLetsProcessesStart: Accept never waits for a connection with the fork lock held, even on a socket that
// File and Fd put in blocking mode (the resident node's handover): it puts the socket back in non-blocking mode, and a
// process starts while it waits.
func TestAcceptLetsProcessesStart(t *testing.T) {
	path := filepath.Join(shortTempDir(t), "s")
	ln, err := Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	// release connects without syscall.ForkLock (net.Dial takes it for reading, behind a start waiting to take it for
	// writing), so that an Accept stuck in blocking mode returns and the test can end.
	release := func() {
		s, err := syscall.Socket(syscall.AF_UNIX, syscall.SOCK_STREAM, 0)
		if err != nil {
			return
		}
		_ = syscall.Connect(s, &syscall.SockaddrUnix{Name: path})
		_ = syscall.Close(s)
	}
	f, err := ln.(*UnixListener).File()
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	fd := f.Fd() // blocking mode, for the socket every copy shares
	nonblocking := func() bool {
		flags, _, errno := syscall.Syscall(syscall.SYS_FCNTL, fd, syscall.F_GETFL, 0)
		if errno != 0 {
			t.Fatal(errno)
		}
		return flags&syscall.O_NONBLOCK != 0
	}
	if nonblocking() {
		t.Fatal("Fd left the socket non-blocking: the test would show nothing")
	}
	errc := make(chan error, 1)
	go func() {
		c, err := ln.Accept()
		if c != nil {
			_ = c.Close()
		}
		errc <- err
	}()
	tick := time.NewTicker(time.Millisecond)
	defer tick.Stop()
	limit := time.After(waitLimit)
	for !nonblocking() {
		select {
		case <-tick.C:
		case <-limit:
			release()
			t.Fatal("Accept left the socket in blocking mode")
		}
	}
	started := make(chan error, 1)
	go func() { started <- exec.Command("/usr/bin/true").Run() }()
	select {
	case err := <-started:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(waitLimit):
		release()
		t.Fatal("a process start waited on Accept")
	}
	if err := ln.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-errc:
		if !errors.Is(err, net.ErrClosed) {
			t.Fatalf("Accept on a closed listener: %v; want net.ErrClosed", err)
		}
	case <-time.After(waitLimit):
		t.Fatal("Close left Accept waiting")
	}
}
