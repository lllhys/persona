package host

import (
	"context"
	"net"
	"net/http"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

// startHealth serves h on a unix socket and returns a Proc for it, with the HTTP client Start builds for that socket,
// and a count of the connections the server took. Nothing starts a program: the Proc is only there for Healthy.
func startHealth(t *testing.T, h http.HandlerFunc) (*Proc, *atomic.Int32) {
	t.Helper()
	sock := filepath.Join(farDir(t), "http.sock")
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	conns := new(atomic.Int32)
	srv := &http.Server{Handler: h, ConnState: func(_ net.Conn, s http.ConnState) {
		if s == http.StateNew {
			conns.Add(1)
		}
	}}
	go func() { _ = srv.Serve(ln) }()
	t.Cleanup(func() { _ = srv.Close() })
	p := Start(context.Background(), Spec{ID: "health", HTTP: true, Socket: sock})
	t.Cleanup(p.Stop)
	return p, conns
}

// A /health answer that never ends is read only up to healthBodyMost: the check returns right away with the status
// code's verdict instead of reading for the whole HealthTimeout (about 17 GB in 5 s before).
func TestHealthyReadsABoundedBody(t *testing.T) {
	var wrote atomic.Int64
	done := make(chan struct{})
	p, _ := startHealth(t, func(w http.ResponseWriter, r *http.Request) {
		defer close(done)
		chunk := make([]byte, 64<<10)
		w.WriteHeader(http.StatusOK)
		for r.Context().Err() == nil {
			n, err := w.Write(chunk)
			wrote.Add(int64(n))
			if err != nil {
				return
			}
		}
	})

	start := time.Now()
	ok := p.Healthy(context.Background())
	took := time.Since(start)
	select {
	case <-done: // the closed connection ends the answer
	case <-time.After(10 * time.Second):
		t.Fatal("the endless answer is still being written after Healthy returned")
	}
	t.Logf("Healthy returned in %s; the server wrote %d bytes", took, wrote.Load())
	if !ok {
		t.Error("a 200 answer is healthy, however long its body")
	}
	if took >= time.Second {
		t.Errorf("Healthy took %s on an endless body", took)
	}
	// The kernel's socket buffers let the server get a few hundred KB ahead before it sees the close; unbounded, it is
	// gigabytes.
	if n := wrote.Load(); n >= 4<<20 {
		t.Errorf("the server wrote %d bytes of one /health answer", n)
	}
}

// The verdict is still the status code alone.
func TestHealthyIsTheStatusCode(t *testing.T) {
	for _, c := range []struct {
		code int
		body string
		want bool
	}{
		{http.StatusOK, "ok", true},
		{http.StatusOK, "", true},
		{http.StatusServiceUnavailable, "starting", false},
		{http.StatusNotFound, "", false},
	} {
		p, _ := startHealth(t, func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/health" {
				http.NotFound(w, r)
				return
			}
			w.WriteHeader(c.code)
			_, _ = w.Write([]byte(c.body))
		})
		if got := p.Healthy(context.Background()); got != c.want {
			t.Errorf("/health answering %d %q: Healthy = %v, want %v", c.code, c.body, got, c.want)
		}
	}
}

// A short answer is still read to its end, so the next check reuses the connection instead of dialing again.
func TestHealthyReusesTheConnectionOfAShortAnswer(t *testing.T) {
	p, conns := startHealth(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("ok"))
	})
	for i := range 3 {
		if !p.Healthy(context.Background()) {
			t.Fatalf("check %d: not healthy", i+1)
		}
	}
	if n := conns.Load(); n != 1 {
		t.Errorf("three checks took %d connections, want 1", n)
	}
}
