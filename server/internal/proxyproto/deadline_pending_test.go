package proxyproto

import (
	"errors"
	"net"
	"os"
	"testing"
	"time"
)

// observedDeadlineConn signals each actual socket deadline change without delaying a pending read.
type observedDeadlineConn struct {
	net.Conn
	deadlines chan time.Time
	reads     chan struct{}
}

func (c *observedDeadlineConn) Read(p []byte) (int, error) {
	select {
	case c.reads <- struct{}{}:
	default:
	}
	return c.Conn.Read(p)
}

func (c *observedDeadlineConn) SetReadDeadline(deadline time.Time) error {
	err := c.Conn.SetReadDeadline(deadline)
	c.deadlines <- deadline
	return err
}

func pendingHeader(t *testing.T) (*Conn, net.Conn, time.Time, <-chan time.Time, <-chan error) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	client, err := net.Dial("tcp", listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	server, err := listener.Accept()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close(); _ = server.Close() })
	under := &observedDeadlineConn{Conn: server, deadlines: make(chan time.Time, 16), reads: make(chan struct{}, 1)}
	c := &Conn{Conn: under, timeout: 30 * time.Second}
	done := make(chan error, 1)
	go func() { var b [1]byte; _, err := c.Read(b[:]); done <- err }()
	var headerDeadline time.Time
	select {
	case headerDeadline = <-under.deadlines: // header parsing has installed its deadline
	case <-time.After(time.Second):
		t.Fatal("header read did not start")
	}
	select {
	case <-under.reads: // the header reader has reached the socket
	case <-time.After(time.Second):
		t.Fatal("header did not reach the socket")
	}
	return c, client, headerDeadline, under.deadlines, done
}

func TestPendingHeaderHonorsShortenedReadDeadline(t *testing.T) {
	for _, setter := range []string{"read", "both"} {
		t.Run(setter, func(t *testing.T) {
			c, _, _, _, done := pendingHeader(t)
			var err error
			if setter == "read" {
				err = c.SetReadDeadline(time.Now())
			} else {
				err = c.SetDeadline(time.Now())
			}
			if err != nil {
				t.Fatal(err)
			}
			select {
			case err := <-done:
				if !errors.Is(err, os.ErrDeadlineExceeded) {
					t.Fatalf("read: %v, want deadline", err)
				}
			case <-time.After(time.Second):
				t.Fatal("shortened deadline did not interrupt pending header")
			}
		})
	}
}

func TestPendingHeaderKeepsItsLimitAndRestoresCallerDeadline(t *testing.T) {
	c, client, headerDeadline, deadlines, done := pendingHeader(t)
	// A closer caller deadline must take effect, while extension or clearing cannot remove the header's original limit.
	short := time.Now().Add(5 * time.Second)
	if err := c.SetReadDeadline(short); err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-deadlines:
		if !got.Equal(short) {
			t.Fatalf("short deadline %v, want %v", got, short)
		}
	case <-time.After(time.Second):
		t.Fatal("short deadline was not applied")
	}
	long := time.Now().Add(time.Minute)
	if err := c.SetReadDeadline(long); err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-deadlines:
		if !got.Equal(headerDeadline) {
			t.Fatalf("extended deadline %v, want original header limit %v", got, headerDeadline)
		}
	case <-time.After(time.Second):
		t.Fatal("extended deadline was not applied")
	}
	if err := c.SetReadDeadline(time.Time{}); err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-deadlines:
		if !got.Equal(headerDeadline) {
			t.Fatalf("cleared deadline %v, want header limit %v", got, headerDeadline)
		}
	case <-time.After(time.Second):
		t.Fatal("cleared deadline was not applied")
	}
	written := make(chan error, 1)
	go func() { _, err := client.Write([]byte("PROXY TCP4 203.0.113.7 192.0.2.10 1 443\r\nx")); written <- err }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("read header: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("header did not complete")
	}
	if err := <-written; err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-deadlines:
		if !got.IsZero() {
			t.Fatalf("after header deadline %v, want cleared caller deadline", got)
		}
	case <-time.After(time.Second):
		t.Fatal("caller deadline was not restored")
	}
}

func TestPendingHeaderRestoresExtendedCallerDeadline(t *testing.T) {
	c, client, headerDeadline, deadlines, done := pendingHeader(t)
	callerDeadline := time.Now().Add(time.Minute)
	if err := c.SetReadDeadline(callerDeadline); err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-deadlines:
		if !got.Equal(headerDeadline) {
			t.Fatalf("during header: got %v, want %v", got, headerDeadline)
		}
	case <-time.After(time.Second):
		t.Fatal("header deadline was not applied")
	}
	written := make(chan error, 1)
	go func() { _, err := client.Write([]byte("PROXY TCP4 203.0.113.7 192.0.2.10 1 443\r\nx")); written <- err }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("read header: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("header did not complete")
	}
	if err := <-written; err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-deadlines:
		if !got.Equal(callerDeadline) {
			t.Fatalf("after header: got %v, want %v", got, callerDeadline)
		}
	case <-time.After(time.Second):
		t.Fatal("caller deadline was not restored")
	}
}

func TestHeaderTimeoutStillRefusesTheConnection(t *testing.T) {
	c, _, _, _, done := pendingHeader(t)
	if err := c.SetReadDeadline(time.Now()); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if !errors.Is(err, os.ErrDeadlineExceeded) {
			t.Fatalf("read: %v, want deadline", err)
		}
	case <-time.After(time.Second):
		t.Fatal("short deadline did not interrupt header")
	}
	// A header timeout has always refused this connection; extending a deadline cannot retry its header.
	_ = c.SetReadDeadline(time.Now().Add(time.Minute))
	var b [1]byte
	if _, err := c.Read(b[:]); !errors.Is(err, os.ErrDeadlineExceeded) {
		t.Fatalf("read after header timeout: %v, want the original deadline error", err)
	}
}
