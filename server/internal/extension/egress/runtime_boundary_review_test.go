package egress

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestRuntimeBusyRejectDoesNotDrainBody(t *testing.T) {
	p := &Proxy{}
	p.Register("a", Rule{})
	defer p.Close()
	p.runtimeActive = map[string]int{"a": 4}
	p.runtimeTotal = 4
	socket := filepath.Join(shortTempDir(t), "proxy.sock")
	if err := p.ListenRuntimeUnix("a", socket); err != nil {
		t.Fatal(err)
	}
	c, err := net.Dial("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	_ = c.SetDeadline(time.Now().Add(300 * time.Millisecond))
	_, _ = io.WriteString(c, "POST "+RuntimeFetchPath+" HTTP/1.1\r\nHost: broker\r\nContent-Length: 1000\r\n\r\nx")
	resp, err := http.ReadResponse(bufio.NewReader(c), nil)
	if err != nil {
		t.Fatal("busy rejection drained incomplete body", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusTooManyRequests || !resp.Close {
		t.Fatal(resp.StatusCode, resp.Close)
	}
}

func TestRuntimeBlockedResponseReleasesBrokerSlot(t *testing.T) {
	for _, reason := range []string{"deadline", "reconfigure"} {
		t.Run(reason, func(t *testing.T) {
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, _ = io.WriteString(w, strings.Repeat("x", 4<<20))
			}))
			defer upstream.Close()
			p := newProxy(t, strings.TrimPrefix(upstream.URL, "http://"), nil)
			p.Register("a", Rule{Hosts: []string{"api.example.com"}})
			p.Register("b", Rule{Hosts: []string{"api.example.com"}})
			finished := make(chan struct{}, 1)
			writing := make(chan struct{}, 1)
			unblock := make(chan struct{})
			broker := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != RuntimeFetchPath {
					http.NotFound(w, r)
					return
				}
				defer func() { finished <- struct{}{} }()
				ctx, cancel := context.WithTimeout(r.Context(), 500*time.Millisecond)
				defer cancel()
				p.handler("a").ServeHTTP(&writeObserved{ResponseWriter: w, writing: writing, unblock: unblock}, r.WithContext(ctx))
			}))
			defer broker.Close()
			c, err := net.Dial("tcp", strings.TrimPrefix(broker.URL, "http://"))
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()
			if err := c.(*net.TCPConn).SetReadBuffer(1024); err != nil {
				t.Fatal(err)
			}
			_ = c.SetWriteDeadline(time.Now().Add(time.Second))
			raw := `{"url":"http://api.example.com/","method":"GET","headers":{},"body":""}`
			if _, err := fmt.Fprintf(c, "POST %s HTTP/1.1\r\nHost: broker\r\nContent-Length: %d\r\n\r\n%s", RuntimeFetchPath, len(raw), raw); err != nil {
				t.Fatal(err)
			}
			// The raw guest never reads the response. A small receive window and
			// multi-MiB upstream body force forwarding to wait in the socket writer.
			select {
			case <-writing:
			case <-time.After(time.Second):
				t.Fatal("broker never started forwarding")
			}
			if reason == "reconfigure" {
				select {
				case <-finished:
					t.Fatal("response completed before the stalled-writer cancellation fixture")
				default:
				}
				p.SetTargets("a", nil)
			}
			select {
			case <-finished:
			case <-time.After(time.Second):
				t.Fatal("blocked response outlived cancellation and retained broker admission")
			}
			p.mu.Lock()
			total, leases := p.runtimeTotal, len(p.active["a"])
			p.mu.Unlock()
			if total != 0 || leases != 0 {
				t.Fatal("blocked response retained broker resources", total, leases)
			}
			neighbor := httptest.NewRecorder()
			p.handler("b").ServeHTTP(neighbor, httptest.NewRequest("POST", RuntimeFetchPath, strings.NewReader(raw)))
			if neighbor.Code != http.StatusOK {
				t.Fatal("neighbor broker was affected", neighbor.Code)
			}
		})
	}
}

// Keep ResponseController's socket deadlines visible through the observer.
type writeObserved struct {
	http.ResponseWriter
	writing     chan struct{}
	unblock     chan struct{}
	unblockOnce sync.Once
}

func (w *writeObserved) Unwrap() http.ResponseWriter { return w.ResponseWriter }
func (w *writeObserved) Write(b []byte) (int, error) {
	select {
	case w.writing <- struct{}{}:
	default:
	}
	<-w.unblock
	return w.ResponseWriter.Write(b)
}

func (w *writeObserved) SetWriteDeadline(deadline time.Time) error {
	expired := !deadline.IsZero() && !deadline.After(time.Now())
	err := http.NewResponseController(w.ResponseWriter).SetWriteDeadline(deadline)
	if expired {
		w.unblockOnce.Do(func() { close(w.unblock) })
	}
	return err
}

func TestRuntimeCancelInterruptsPendingUpstreamCONNECT(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	accepted := make(chan net.Conn, 1)
	go func() {
		c, err := ln.Accept()
		if err == nil {
			accepted <- c
		}
	}()
	proxy, _ := url.Parse("http://" + ln.Addr().String())
	p := &Proxy{Upstream: func(*url.URL) (*url.URL, error) { return proxy, nil }, Resolve: func(context.Context, string) ([]net.IP, error) { return []net.IP{net.ParseIP("192.0.2.1")}, nil }}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		c, err := p.runtimeDial(ctx, "api.example.com", "443", false)
		if c != nil {
			c.Close()
		}
		done <- err
	}()
	var c net.Conn
	select {
	case c = <-accepted:
	case <-time.After(time.Second):
		t.Fatal("proxy was not dialed")
	}
	defer c.Close()
	// The trusted proxy receives the request but never answers CONNECT.
	_ = c.SetReadDeadline(time.Now().Add(time.Second))
	r, err := http.ReadRequest(bufio.NewReader(c))
	if err != nil {
		t.Fatal(err)
	}
	_ = r.Body.Close()
	cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("stalled CONNECT succeeded")
		}
	case <-time.After(300 * time.Millisecond):
		t.Fatal("canceled CONNECT retained its socket")
	}
}
