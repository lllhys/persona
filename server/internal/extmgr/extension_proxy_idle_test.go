package extmgr

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/extension/host"
)

// openConnsListener counts the connections it accepted that are still open.
type openConnsListener struct {
	net.Listener
	mu   sync.Mutex
	open int
}

func (l *openConnsListener) Accept() (net.Conn, error) {
	c, err := l.Listener.Accept()
	if err != nil {
		return nil, err
	}
	l.mu.Lock()
	l.open++
	l.mu.Unlock()
	return &openConn{Conn: c, l: l}, nil
}

func (l *openConnsListener) count() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.open
}

type openConn struct {
	net.Conn
	l    *openConnsListener
	once sync.Once
}

func (c *openConn) Close() error {
	c.once.Do(func() {
		c.l.mu.Lock()
		c.l.open--
		c.l.mu.Unlock()
	})
	return c.Conn.Close()
}

// farExtension serves an extension from elsewhere (a socket the hub did not start) and returns its hubExtension
// after one call each through its client and its proxy, which leave one keep-alive connection each.
func farExtension(t *testing.T) (*hubExtension, *openConnsListener) {
	t.Helper()
	dir, err := os.MkdirTemp("", "pxi") // a unix socket path stays short
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	sock := filepath.Join(dir, "ext.sock")
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	cl := &openConnsListener{Listener: ln}
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "ok") })}
	go srv.Serve(cl)
	t.Cleanup(func() { srv.Close() })

	hp := &hubExtension{id: "far", socket: sock}
	hp.setProxy("far")
	res, err := hp.client.Get("http://plugin/health")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.Copy(io.Discard, res.Body)
	res.Body.Close()
	rec := httptest.NewRecorder()
	hp.proxy.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/extensions/far/x", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("proxy: HTTP %d %s", rec.Code, rec.Body)
	}
	if n := cl.count(); n != 2 {
		t.Fatalf("%d connection(s) open, want one each for the client and the proxy", n)
	}
	return hp, cl
}

// waitNoConns fails the test unless every connection to the extension closes soon.
func waitNoConns(t *testing.T, cl *openConnsListener, after string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for cl.count() != 0 {
		if time.Now().After(deadline) {
			t.Fatalf("%d connection(s) to the extension still open after %s", cl.count(), after)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// apply replaces the hubExtension of an extension it runs again (here: one no longer installed): the old one's
// connections close.
func TestApplyClosesTheReplacedExtensionsConnections(t *testing.T) {
	h := newTestManager(t)
	h.extensions = newExtensions(t.TempDir(), nil, t.Logf)
	h.extensions.h = h
	hp, cl := farExtension(t)
	ps := h.extensions
	ps.mu.Lock()
	ps.byID["far"] = hp
	ps.mu.Unlock()
	ps.apply(context.Background(), "far")
	waitNoConns(t, cl, "apply")
}

// The hub going down closes the connections of every extension, with a Proc watching it or not.
func TestStopExtensionsClosesConnections(t *testing.T) {
	for _, watched := range []bool{false, true} {
		t.Run(map[bool]string{false: "no proc", true: "proc"}[watched], func(t *testing.T) {
			h := newTestManager(t)
			h.extensions = newExtensions(t.TempDir(), nil, t.Logf)
			h.extensions.h = h
			hp, cl := farExtension(t)
			if watched {
				// A remote HTTP extension: the Proc only checks it, so Stop leaves the far side up.
				hp.proc = host.Start(context.Background(), host.Spec{ID: "far", HTTP: true, Socket: hp.socket, Logf: t.Logf})
			}
			ps := h.extensions
			ps.mu.Lock()
			ps.byID["far"] = hp
			ps.mu.Unlock()
			h.stopExtensions()
			waitNoConns(t, cl, "stopExtensions")
		})
	}
}
