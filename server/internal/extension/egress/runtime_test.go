package egress

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestRuntimeBrokerUsesOnlyFixedRegistration(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "ok") }))
	defer srv.Close()
	p := newProxy(t, strings.TrimPrefix(srv.URL, "http://"), nil)
	p.Register("a", Rule{Hosts: []string{"api.example.com"}})
	p.Register("b", Rule{Hosts: []string{"other.example.com"}})
	request := func(id, target string) int {
		raw, _ := json.Marshal(map[string]any{"url": target, "method": "GET", "headers": map[string]string{"X-Extension-ID": "a"}, "body": ""})
		r := httptest.NewRequest("POST", RuntimeFetchPath, bytes.NewReader(raw))
		w := httptest.NewRecorder()
		p.handler(id).ServeHTTP(w, r)
		return w.Code
	}
	if code := request("a", "http://api.example.com/"); code != 200 {
		t.Fatal(code)
	}
	if code := request("b", "http://api.example.com/"); code != 403 {
		t.Fatal("guest identity overrode registration", code)
	}
	if code := request("", "http://api.example.com/"); code != 407 {
		t.Fatal("unbound broker accepted", code)
	}
	if code := request("a", "http://inside.example.com/"); code != 403 {
		t.Fatal("undeclared host accepted", code)
	}
	p.Register("a", Rule{Hosts: []string{"inside.example.com"}})
	if code := request("a", "http://inside.example.com/"); code != 502 {
		t.Fatal("DNS local-address escape accepted", code)
	}
	p.Unregister("a")
	if code := request("a", "http://api.example.com/"); code != 403 {
		t.Fatal("revoked broker accepted", code)
	}
}
func TestRuntimeBrokerRejectsBodyAndProtocol(t *testing.T) {
	p := &Proxy{}
	p.Register("a", Rule{Hosts: []string{"api.example.com"}})
	for _, tc := range []struct {
		url, body string
		status    int
	}{{"file:///etc/passwd", "", 400}, {"http://me:secret@api.example.com/", "", 400}, {"http://api.example.com/", base64.StdEncoding.EncodeToString(make([]byte, RuntimeBodyMost+1)), 413}} {
		raw, _ := json.Marshal(map[string]any{"url": tc.url, "method": "GET", "headers": map[string]string{}, "body": tc.body})
		r := httptest.NewRequest("POST", RuntimeFetchPath, bytes.NewReader(raw))
		w := httptest.NewRecorder()
		p.handler("a").ServeHTTP(w, r)
		if w.Code != tc.status {
			t.Fatalf("%s: %d", tc.url, w.Code)
		}
	}
}
func TestRuntimeDialPinsUpstreamDNS(t *testing.T) {
	var connectTarget string
	p := &Proxy{Resolve: func(context.Context, string) ([]net.IP, error) { return []net.IP{net.ParseIP("192.0.2.1")}, nil }, Upstream: func(*url.URL) (*url.URL, error) { return url.Parse("http://proxy.example.com:80") }, Dial: func(context.Context, string, string) (net.Conn, error) {
		near, far := net.Pipe()
		go func() {
			defer far.Close()
			buf := make([]byte, 4096)
			n, _ := far.Read(buf)
			connectTarget = string(buf[:n])
			io.WriteString(far, "HTTP/1.1 200 Connection established\r\n\r\n")
		}()
		return near, nil
	}}
	c, err := p.runtimeDial(context.Background(), "api.example.com", "443", false)
	if err != nil {
		t.Fatal(err)
	}
	c.Close()
	if !strings.Contains(connectTarget, "CONNECT 192.0.2.1:443") {
		t.Fatal("upstream could resolve a different hostname", connectTarget)
	}
}
func TestRevocationAndReconfigurationCancelOnlyOwnIO(t *testing.T) {
	p := &Proxy{}
	p.Register("a", Rule{})
	p.Register("b", Rule{})
	ctxA, doneA, ok := p.requestLifetime(context.Background(), "a", nil)
	if !ok {
		t.Fatal("a missing")
	}
	defer doneA()
	ctxB, doneB, _ := p.requestLifetime(context.Background(), "b", nil)
	defer doneB()
	p.SetTargets("a", []Target{{Host: "api.example.com", Port: "443"}})
	select {
	case <-ctxA.Done():
	case <-time.After(time.Second):
		t.Fatal("reconfiguration did not cancel A")
	}
	if ctxB.Err() != nil {
		t.Fatal("B canceled by A reconfiguration")
	}
	ctxA, doneA, _ = p.requestLifetime(context.Background(), "a", nil)
	defer doneA()
	p.Unregister("a")
	select {
	case <-ctxA.Done():
	case <-time.After(time.Second):
		t.Fatal("revocation did not cancel A")
	}
	if ctxB.Err() != nil {
		t.Fatal("B canceled by A revocation")
	}
}
func TestRuntimeIdleSocketsAreBoundedAndRevoked(t *testing.T) {
	p := &Proxy{}
	if err := p.Start(); err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	dir := shortTempDir(t)
	p.Register("a", Rule{})
	p.Register("b", Rule{})
	for _, id := range []string{"a", "b"} {
		if err := p.ListenRuntimeUnix(id, filepath.Join(dir, id+".sock")); err != nil {
			t.Fatal(err)
		}
	}
	var conns []net.Conn
	for i := 0; i < 20; i++ {
		c, err := net.Dial("unix", filepath.Join(dir, "a.sock"))
		if err != nil {
			t.Fatal(err)
		}
		conns = append(conns, c)
		defer c.Close()
	}
	b, err := net.Dial("unix", filepath.Join(dir, "b.sock"))
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	b.SetDeadline(time.Now().Add(time.Second))
	io.WriteString(b, "GET / HTTP/1.1\r\nHost: example.com\r\n\r\n")
	data := make([]byte, 1024)
	if _, err := b.Read(data); err != nil {
		t.Fatal("A idle sockets blocked B", err)
	}
	p.mu.Lock()
	l := p.unix["a"].(*runtimeListener)
	p.mu.Unlock()
	l.mu.Lock()
	count := len(l.connections)
	l.mu.Unlock()
	if count > runtimeConnsPerListener {
		t.Fatal("unbounded pre-header sockets", count)
	}
	p.Unregister("a")
	for _, c := range conns {
		c.SetReadDeadline(time.Now().Add(time.Second))
		if _, err := c.Read(data); err != io.EOF && !errors.Is(err, syscall.ECONNRESET) {
			t.Fatal("revocation left pre-header socket open", err)
		}
	}
}

func TestRevocationClosesEstablishedTunnel(t *testing.T) {
	far := make(chan net.Conn, 1)
	// Straight, not the hub's environment: an HTTPS_PROXY there would make the tunnel CONNECT to it over the pipe.
	p := &Proxy{Upstream: func(*url.URL) (*url.URL, error) { return nil, nil }, Dial: func(context.Context, string, string) (net.Conn, error) { a, b := net.Pipe(); far <- b; return a, nil }}
	if err := p.Start(); err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	proxyURL := p.Register("a", Rule{Hosts: []string{"192.0.2.1"}})
	u, _ := url.Parse(proxyURL)
	c, err := net.Dial("tcp", u.Host)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	pass, _ := u.User.Password()
	auth := base64.StdEncoding.EncodeToString([]byte(u.User.Username() + ":" + pass))
	io.WriteString(c, "CONNECT 192.0.2.1:443 HTTP/1.1\r\nHost: 192.0.2.1:443\r\nProxy-Authorization: Basic "+auth+"\r\n\r\n")
	c.SetReadDeadline(time.Now().Add(time.Second))
	buf := make([]byte, 4096)
	if _, err := c.Read(buf); err != nil {
		t.Fatal(err)
	}
	up := <-far
	defer up.Close()
	p.Unregister("a")
	up.SetReadDeadline(time.Now().Add(time.Second))
	if _, err := up.Read(buf); err != io.EOF {
		t.Fatal("upstream tunnel survived revocation", err)
	}
}

func TestRuntimeListenerRejectsRawProxyRequests(t *testing.T) {
	p := &Proxy{}
	p.Register("a", Rule{Hosts: []string{"api.example.com"}})
	defer p.Close()
	socket := filepath.Join(shortTempDir(t), "proxy.sock")
	if err := p.ListenRuntimeUnix("a", socket); err != nil {
		t.Fatal(err)
	}
	for _, target := range []string{
		"CONNECT api.example.com:443",
		"GET http://api.example.com/",
		"POST http://api.example.com" + RuntimeFetchPath,
		"GET /",
	} {
		c, err := net.Dial("unix", socket)
		if err != nil {
			t.Fatal(err)
		}
		c.SetDeadline(time.Now().Add(time.Second))
		io.WriteString(c, target+" HTTP/1.1\r\nHost: api.example.com\r\nContent-Length: 0\r\nConnection: close\r\n\r\n")
		resp, err := http.ReadResponse(bufio.NewReader(c), nil)
		if err != nil {
			c.Close()
			t.Fatal(err)
		}
		resp.Body.Close()
		c.Close()
		if resp.StatusCode != http.StatusForbidden {
			t.Fatalf("%s bypassed the source broker: %d", target, resp.StatusCode)
		}
	}
	if p.Stats("a").Allowed != 0 {
		t.Fatal("raw proxy requests reached egress policy")
	}
}

func TestRuntimeRejectedRequestDoesNotWaitForBody(t *testing.T) {
	p := &Proxy{}
	p.Register("a", Rule{})
	defer p.Close()
	socket := filepath.Join(shortTempDir(t), "proxy.sock")
	if err := p.ListenRuntimeUnix("a", socket); err != nil {
		t.Fatal(err)
	}
	c, err := net.Dial("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	c.SetDeadline(time.Now().Add(time.Second))
	// No Connection: close and an incomplete body: net/http must not drain it
	// before flushing the rejection, outside the bounded runtime-fetch handler.
	io.WriteString(c, "POST /unsupported HTTP/1.1\r\nHost: broker\r\nContent-Length: 1000\r\n\r\nx")
	resp, err := http.ReadResponse(bufio.NewReader(c), nil)
	if err != nil {
		t.Fatal("rejected request retained a socket while waiting for its body", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden || !resp.Close {
		t.Fatalf("invalid runtime request must reject and close: %d close=%v", resp.StatusCode, resp.Close)
	}
}

func TestRuntimeBodyTimeoutReleasesBrokerSlot(t *testing.T) {
	p := &Proxy{}
	p.Register("a", Rule{})
	finished := make(chan struct{}, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == "/" { // a node's port probe (node/ports.go), not this test's client
			http.NotFound(w, r)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 100*time.Millisecond)
		defer cancel()
		p.handler("a").ServeHTTP(w, r.WithContext(ctx))
		finished <- struct{}{}
	}))
	defer srv.Close()
	c, err := net.Dial("tcp", strings.TrimPrefix(srv.URL, "http://"))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	c.SetDeadline(time.Now().Add(2 * time.Second))
	io.WriteString(c, "POST "+RuntimeFetchPath+" HTTP/1.1\r\nHost: broker\r\nContent-Length: 1000\r\n\r\n{\"url\":")
	select {
	case <-finished:
	case <-time.After(time.Second):
		t.Fatal("stalled body outlived its request deadline")
	}
	p.mu.Lock()
	total := p.runtimeTotal
	p.mu.Unlock()
	if total != 0 {
		t.Fatal("timed-out body retained a broker slot", total)
	}
}

func TestRuntimeHEADPreservesMetadataWithoutTruncatingPOST(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == "/" { // a node's port probe (node/ports.go), not this test's client
			http.NotFound(w, r)
			return
		}
		if r.Method != http.MethodHead {
			t.Error("upstream did not receive HEAD")
		}
		w.Header().Set("Content-Length", "1000")
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()
	p := newProxy(t, strings.TrimPrefix(upstream.URL, "http://"), nil)
	p.Register("a", Rule{Hosts: []string{"api.example.com"}})
	broker := httptest.NewServer(p.handler("a"))
	defer broker.Close()
	raw := `{"url":"http://api.example.com/","method":"HEAD","headers":{},"body":""}`
	resp, err := broker.Client().Post(broker.URL+RuntimeFetchPath, "application/json", strings.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil || len(body) != 0 {
		t.Fatalf("HEAD broker body: %q %v", body, err)
	}
	if resp.Header.Get(RuntimeHeadLengthHeader) != "1000" || resp.Header.Get("Content-Length") == "1000" {
		t.Fatal("entity metadata used as POST framing", resp.Header)
	}
}
