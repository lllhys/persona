package osproc

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"os"
	"testing"
	"time"
)

// waitLimit bounds every wait in these tests: what they wait for comes at once unless the code under test is wrong.
const waitLimit = 10 * time.Second

// shortDeadline is a listener deadline that passes while Accept waits.
const shortDeadline = 50 * time.Millisecond

func listenTCP(t *testing.T) net.Listener {
	t.Helper()
	ln, err := Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	return ln
}

// acceptOne dials ln on network and returns both ends of the connection, closed when the test ends.
func acceptOne(t *testing.T, ln net.Listener, network string) (server, client net.Conn) {
	t.Helper()
	client, err := net.Dial(network, ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })
	server, err = ln.Accept()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = server.Close() })
	return server, client
}

// roundTrip checks that what client writes reaches server.
func roundTrip(t *testing.T, server, client net.Conn) {
	t.Helper()
	if _, err := client.Write([]byte("ping")); err != nil {
		t.Fatal(err)
	}
	_ = server.SetReadDeadline(time.Now().Add(waitLimit))
	buf := make([]byte, 4)
	if _, err := io.ReadFull(server, buf); err != nil || string(buf) != "ping" {
		t.Fatalf("read %q, %v; want ping", buf, err)
	}
}

// TestListenAcceptsTCP: Listen's TCP listener is a *TCPListener, and what it accepts is net's own *net.TCPConn, with
// both addresses, in the poller (a read deadline ends a read).
func TestListenAcceptsTCP(t *testing.T) {
	ln := listenTCP(t)
	if _, ok := ln.(*TCPListener); !ok {
		t.Fatalf("Listen(tcp) gave %T, want *TCPListener", ln)
	}
	server, client := acceptOne(t, ln, "tcp")
	if _, ok := server.(*net.TCPConn); !ok {
		t.Fatalf("accepted %T, want *net.TCPConn", server)
	}
	if server.LocalAddr().String() != ln.Addr().String() || server.RemoteAddr().String() != client.LocalAddr().String() {
		t.Fatalf("addresses %v -> %v; want %v -> %v", server.RemoteAddr(), server.LocalAddr(), client.LocalAddr(), ln.Addr())
	}
	roundTrip(t, server, client)
	_ = server.SetReadDeadline(time.Now().Add(-time.Second))
	if _, err := server.Read(make([]byte, 1)); !errors.Is(err, os.ErrDeadlineExceeded) {
		t.Fatalf("read past its deadline: %v; want os.ErrDeadlineExceeded", err)
	}

	client2, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer client2.Close()
	c, err := ln.(*TCPListener).AcceptTCP()
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	roundTrip(t, c, client2)
}

// TestCloseEndsAccept: closing the listener ends an Accept waiting on it with net.ErrClosed, as net's listener does,
// and a second Close says it is closed.
func TestCloseEndsAccept(t *testing.T) {
	ln := listenTCP(t)
	accepted := make(chan net.Conn)
	errc := make(chan error, 1)
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				errc <- err
				return
			}
			accepted <- c
		}
	}()
	client, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	select {
	case c := <-accepted: // the loop is back in Accept, waiting
		_ = c.Close()
	case err := <-errc:
		t.Fatal(err)
	case <-time.After(waitLimit):
		t.Fatal("no connection accepted")
	}
	if err := ln.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-errc:
		var op *net.OpError
		if !errors.Is(err, net.ErrClosed) || !errors.As(err, &op) || op.Op != "accept" {
			t.Fatalf("Accept on a closed listener: %v; want an accept error that is net.ErrClosed", err)
		}
	case <-time.After(waitLimit):
		t.Fatal("Close left Accept waiting")
	}
	if err := ln.Close(); err == nil {
		t.Fatal("a second Close said nothing; want that the listener is closed")
	}
}

// TestDeadlineEndsAccept: a deadline that has passed, or passes while Accept waits, ends Accept with a timeout, as
// net's listener does; a cleared one lets it accept again.
func TestDeadlineEndsAccept(t *testing.T) {
	ln := listenTCP(t).(*TCPListener)
	for _, deadline := range []time.Time{time.Now().Add(-time.Second), time.Now().Add(shortDeadline)} {
		if err := ln.SetDeadline(deadline); err != nil {
			t.Fatal(err)
		}
		done := make(chan error, 1)
		go func() {
			c, err := ln.Accept()
			if c != nil {
				_ = c.Close()
			}
			done <- err
		}()
		select {
		case err := <-done:
			var ne net.Error
			if !errors.Is(err, os.ErrDeadlineExceeded) || !errors.As(err, &ne) || !ne.Timeout() {
				t.Fatalf("Accept past its deadline: %v; want a timeout", err)
			}
		case <-time.After(waitLimit):
			t.Fatal("the deadline did not end Accept")
		}
	}
	if err := ln.SetDeadline(time.Time{}); err != nil {
		t.Fatal(err)
	}
	server, client := acceptOne(t, ln, "tcp")
	roundTrip(t, server, client)
}

// TestServesHTTP: an http.Server serves on the listener and shuts down as on net's.
func TestServesHTTP(t *testing.T) {
	ln := listenTCP(t)
	srv := &http.Server{
		Handler:           http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, "ok") }),
		ReadHeaderTimeout: waitLimit,
	}
	served := make(chan error, 1)
	go func() { served <- srv.Serve(ln) }()
	client := &http.Client{Transport: &http.Transport{DisableKeepAlives: true}, Timeout: waitLimit}
	resp, err := client.Get("http://" + ln.Addr().String() + "/")
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if err != nil || string(body) != "ok" {
		t.Fatalf("got %q, %v; want ok", body, err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), waitLimit)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-served:
		if !errors.Is(err, http.ErrServerClosed) {
			t.Fatalf("Serve returned %v; want http.ErrServerClosed", err)
		}
	case <-time.After(waitLimit):
		t.Fatal("Serve did not return after Shutdown")
	}
}
