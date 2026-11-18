//go:build unix

package osproc

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

// shortTempDir is a directory for sockets under /tmp: a socket under macOS's temporary directory has too long a path.
func shortTempDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "osproc")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return dir
}

// closeOnExec says whether c's descriptor is close-on-exec.
func closeOnExec(t *testing.T, c net.Conn) bool {
	t.Helper()
	rc, err := c.(syscall.Conn).SyscallConn()
	if err != nil {
		t.Fatal(err)
	}
	var flags uintptr
	var errno syscall.Errno
	if err := rc.Control(func(fd uintptr) {
		flags, _, errno = syscall.Syscall(syscall.SYS_FCNTL, fd, syscall.F_GETFD, 0)
	}); err != nil {
		t.Fatal(err)
	}
	if errno != 0 {
		t.Fatal(errno)
	}
	return flags&syscall.FD_CLOEXEC != 0
}

// TestListenAcceptsUnix: Listen's Unix listener is a *UnixListener, and what it accepts is net's own *net.UnixConn,
// with the addresses net's listener gives; the rest of net.UnixListener (SetUnlinkOnClose) is there as before.
func TestListenAcceptsUnix(t *testing.T) {
	dir := shortTempDir(t)
	path := filepath.Join(dir, "s")
	ln, err := Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	ul, ok := ln.(*UnixListener)
	if !ok {
		t.Fatalf("Listen(unix) gave %T, want *UnixListener", ln)
	}
	plain, err := net.Listen("unix", filepath.Join(dir, "p"))
	if err != nil {
		t.Fatal(err)
	}
	defer plain.Close()
	server, client := acceptOne(t, ln, "unix")
	plainServer, _ := acceptOne(t, plain, "unix")
	if _, ok := server.(*net.UnixConn); !ok {
		t.Fatalf("accepted %T, want *net.UnixConn", server)
	}
	if server.LocalAddr().String() != path {
		t.Fatalf("local address %v; want %s", server.LocalAddr(), path)
	}
	if got, want := fmt.Sprintf("%#v", server.RemoteAddr()), fmt.Sprintf("%#v", plainServer.RemoteAddr()); got != want {
		t.Fatalf("remote address %s; net's listener gives %s", got, want)
	}
	roundTrip(t, server, client)

	client2, err := net.Dial("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	defer client2.Close()
	c, err := ul.AcceptUnix()
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	roundTrip(t, c, client2)

	ul.SetUnlinkOnClose(false)
	if err := ln.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("the socket is gone after Close with SetUnlinkOnClose(false): %v", err)
	}
}

// TestAcceptedConnectionsAreCloseOnExec: whatever Listen and FileListener accept is close-on-exec.
func TestAcceptedConnectionsAreCloseOnExec(t *testing.T) {
	tcp := listenTCP(t)
	unix, err := Listen("unix", filepath.Join(shortTempDir(t), "s"))
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close()
	src, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	f, err := src.(*net.TCPListener).File()
	_ = src.Close() // the socket stays open in f
	if err != nil {
		t.Fatal(err)
	}
	fromFile, err := FileListener(f)
	_ = f.Close() // and in fromFile's own copy
	if err != nil {
		t.Fatal(err)
	}
	defer fromFile.Close()
	if _, ok := fromFile.(*TCPListener); !ok {
		t.Fatalf("FileListener gave %T, want *TCPListener", fromFile)
	}
	for _, c := range []struct {
		name    string
		ln      net.Listener
		network string
	}{{"tcp", tcp, "tcp"}, {"unix", unix, "unix"}, {"file", fromFile, "tcp"}} {
		server, client := acceptOne(t, c.ln, c.network)
		roundTrip(t, server, client)
		if !closeOnExec(t, server) {
			t.Errorf("%s: an accepted connection is not close-on-exec", c.name)
		}
	}
}
