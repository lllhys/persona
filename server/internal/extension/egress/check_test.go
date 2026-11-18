package egress

import (
	"bufio"
	"encoding/base64"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"path/filepath"
	"testing"
	"time"
)

// A settings check's credential (Check, #8459): the addresses the proposed settings name are reached with it and only
// with it, the program's ordinary credential keeps the saved ones, and it stops working when revoked, when its time
// is up, and when the program registers anew.

// tcpEcho is a server that sends back what it reads, for tunnels that stay open.
func tcpEcho(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func() { defer c.Close(); _, _ = io.Copy(c, c) }()
		}
	}()
	return ln.Addr().String()
}

// tunnel asks the proxy on c for a CONNECT to target, with password pass for User ("": no credential), and says the
// status; c is the tunnel when it is 200.
func tunnel(t *testing.T, c net.Conn, target, pass string) (int, *bufio.Reader) {
	t.Helper()
	auth := ""
	if pass != "" {
		auth = "Proxy-Authorization: Basic " + base64.StdEncoding.EncodeToString([]byte(User+":"+pass)) + "\r\n"
	}
	fmt.Fprintf(c, "CONNECT %s HTTP/1.1\r\nHost: %s\r\n%s\r\n", target, target, auth)
	br := bufio.NewReader(c)
	resp, err := http.ReadResponse(br, &http.Request{Method: http.MethodConnect})
	if err != nil {
		t.Fatal(err)
	}
	return resp.StatusCode, br
}

func dialProxy(t *testing.T, p *Proxy) net.Conn {
	t.Helper()
	c, err := net.Dial("tcp", p.Addr())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	return c
}

func credOf(purl string) string {
	u, _ := url.Parse(purl)
	pw, _ := u.User.Password()
	return pw
}

// echoes says whether the tunnel on c (read through br) still carries bytes both ways.
func echoes(c net.Conn, br *bufio.Reader) bool {
	_ = c.SetDeadline(time.Now().Add(5 * time.Second))
	defer c.SetDeadline(time.Time{})
	if _, err := io.WriteString(c, "ping\n"); err != nil {
		return false
	}
	line, err := br.ReadString('\n')
	return err == nil && line == "ping\n"
}

// closed says whether the tunnel on c ends (EOF or reset) within a few seconds.
func closed(c net.Conn, br *bufio.Reader) bool {
	_ = c.SetReadDeadline(time.Now().Add(5 * time.Second))
	_, err := br.ReadByte()
	ne, timeout := err.(net.Error)
	return err != nil && !(timeout && ne.Timeout())
}

func TestCheckReachesTheProposedAddressOnlyWithItsCredential(t *testing.T) {
	p := newProxy(t, tcpEcho(t), nil)
	purl := p.Register("aster", Rule{Hosts: []string{"api.example.com"}, Targets: []Target{{Host: "gateway.example.com", Port: "443"}}})
	own := credOf(purl)
	cred, revoke, ok := p.Check("aster", []Target{{Host: "gw2.example.com", Port: "443"}}, time.Minute)
	if !ok || cred == "" || cred == own {
		t.Fatalf("Check: %q %v (own %q)", cred, ok, own)
	}
	for _, c := range []struct {
		pass, target string
		want         int
	}{
		{cred, "gw2.example.com:443", http.StatusOK},            // the proposed address, with the check's credential
		{own, "gw2.example.com:443", http.StatusForbidden},      // not with the program's own, also while the check runs
		{cred, "other.example.org:443", http.StatusForbidden},   // neither saved nor proposed
		{cred, "gw2.example.com:8443", http.StatusForbidden},    // that port only
		{cred, "gateway.example.com:443", http.StatusForbidden}, // the proposed settings no longer name the saved one
		{cred, "api.example.com:443", http.StatusOK},            // its permissions.network hosts as ever
		{own, "gateway.example.com:443", http.StatusOK},         // the program's own reaches the saved address
	} {
		if got, _ := tunnel(t, dialProxy(t, p), c.target, c.pass); got != c.want {
			t.Errorf("CONNECT %s with the %s credential: %d, want %d", c.target, map[bool]string{true: "check's", false: "program's"}[c.pass == cred], got, c.want)
		}
	}

	revoke()
	if got, _ := tunnel(t, dialProxy(t, p), "gw2.example.com:443", cred); got != http.StatusProxyAuthRequired {
		t.Fatalf("after revoke: %d, want 407", got)
	}
	revoke() // twice is once
	if got, _ := tunnel(t, dialProxy(t, p), "gateway.example.com:443", own); got != http.StatusOK {
		t.Fatalf("the program's own after the check: %d", got)
	}
}

// Revoking a check closes the tunnels opened with its credential, and only those.
func TestCheckRevokeClosesItsTunnels(t *testing.T) {
	p := newProxy(t, tcpEcho(t), nil)
	purl := p.Register("aster", Rule{Targets: []Target{{Host: "gateway.example.com", Port: "443"}}})
	cred, revoke, _ := p.Check("aster", []Target{{Host: "gw2.example.com", Port: "443"}}, time.Minute)
	cc := dialProxy(t, p)
	if got, _ := tunnel(t, cc, "gw2.example.com:443", cred); got != http.StatusOK {
		t.Fatalf("check tunnel: %d", got)
	}
	cbr := bufio.NewReader(cc)
	oc := dialProxy(t, p)
	code, obr := tunnel(t, oc, "gateway.example.com:443", credOf(purl))
	if code != http.StatusOK {
		t.Fatalf("own tunnel: %d", code)
	}
	if !echoes(cc, cbr) || !echoes(oc, obr) {
		t.Fatal("tunnels carry nothing")
	}
	revoke()
	if !closed(cc, cbr) {
		t.Fatal("the check's tunnel stays open after revoke")
	}
	if !echoes(oc, obr) {
		t.Fatal("revoking the check closed the program's own tunnel")
	}
}

// A check not revoked stops working when its time is up, its open tunnels with it.
func TestCheckExpires(t *testing.T) {
	p := newProxy(t, tcpEcho(t), nil)
	p.Register("aster", Rule{})
	cred, _, _ := p.Check("aster", []Target{{Host: "gw2.example.com", Port: "443"}}, 300*time.Millisecond)
	c := dialProxy(t, p)
	if got, _ := tunnel(t, c, "gw2.example.com:443", cred); got != http.StatusOK {
		t.Fatalf("before its time is up: %d", got)
	}
	if br := bufio.NewReader(c); !closed(c, br) {
		t.Fatal("the tunnel outlives the check")
	}
	if got, _ := tunnel(t, dialProxy(t, p), "gw2.example.com:443", cred); got != http.StatusProxyAuthRequired {
		t.Fatalf("after its time is up: %d, want 407", got)
	}
	gone, _, _ := p.Check("aster", nil, -time.Second) // born expired
	if got, _ := tunnel(t, dialProxy(t, p), "gw2.example.com:443", gone); got != http.StatusProxyAuthRequired {
		t.Fatalf("an expired check: %d", got)
	}
}

// A program that registers anew (restarted) or is unregistered (stopped) loses its checks; a check is only for a
// registered extension.
func TestCheckEndsWithTheRegistration(t *testing.T) {
	p := newProxy(t, tcpEcho(t), nil)
	if _, _, ok := p.Check("aster", nil, time.Minute); ok {
		t.Fatal("a check for an extension not behind the proxy")
	}
	p.Register("aster", Rule{})
	cred, _, _ := p.Check("aster", []Target{{Host: "gw2.example.com", Port: "443"}}, time.Minute)
	p.Register("aster", Rule{})
	if got, _ := tunnel(t, dialProxy(t, p), "gw2.example.com:443", cred); got != http.StatusProxyAuthRequired {
		t.Fatalf("after a restart: %d, want 407", got)
	}
	cred, _, _ = p.Check("aster", []Target{{Host: "gw2.example.com", Port: "443"}}, time.Minute)
	p.Unregister("aster")
	if got, _ := tunnel(t, dialProxy(t, p), "gw2.example.com:443", cred); got != http.StatusProxyAuthRequired {
		t.Fatalf("after a stop: %d, want 407", got)
	}
}

// On a sandbox's own socket, a check's credential counts only when it is that extension's.
func TestCheckOnASandboxSocket(t *testing.T) {
	p := newProxy(t, tcpEcho(t), nil)
	p.Register("notes", Rule{})
	p.Register("other", Rule{})
	sock := filepath.Join(shortTempDir(t), "proxy.sock")
	if err := p.ListenUnix("notes", sock); err != nil {
		t.Fatal(err)
	}
	if !p.Socketed("notes") || p.Socketed("other") {
		t.Fatal("Socketed")
	}
	mine, _, _ := p.Check("notes", []Target{{Host: "gw2.example.com", Port: "443"}}, time.Minute)
	theirs, _, _ := p.Check("other", []Target{{Host: "gw2.example.com", Port: "443"}}, time.Minute)
	dial := func() net.Conn {
		c, err := net.Dial("unix", sock)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { c.Close() })
		return c
	}
	for pass, want := range map[string]int{mine: http.StatusOK, theirs: http.StatusForbidden, "": http.StatusForbidden} {
		if got, _ := tunnel(t, dial(), "gw2.example.com:443", pass); got != want {
			t.Errorf("credential %q on notes' socket: %d, want %d", pass, got, want)
		}
	}
}
