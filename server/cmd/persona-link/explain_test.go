package main

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"syscall"
	"testing"

	"github.com/nwverse/persona/server/internal/link"
)

// pairLinkTo is a pairing link for a hub whose TLS port is srv's, pinned to srv's key.
func pairLinkTo(t *testing.T, srv *httptest.Server) string {
	t.Helper()
	u, _ := url.Parse(srv.URL)
	sum := sha256.Sum256(srv.Certificate().RawSubjectPublicKeyInfo)
	q := url.Values{"token": {"jp_old"}, "machine": {"pi"}, "url": {"https://" + u.Hostname()}, "tls": {u.Port()},
		"fp": {base64.RawURLEncoding.EncodeToString(sum[:])}}
	return "persona://link?" + q.Encode()
}

func TestExplainARejectedPairingLink(t *testing.T) {
	for _, tc := range []struct{ name, contentType, body string }{
		{"the hub's answer", "application/json", `{"error":"invalid token","code":"invalid_token"}`},
		{"a proxy's page", "text/plain", "Unauthorized"},
	} {
		srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", tc.contentType)
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(tc.body))
		}))
		f := newFakeEnv(t)
		_, err := pair(f.env, pairLinkTo(t, srv), "")
		srv.Close()
		if err == nil {
			t.Fatalf("%s: a 401 paired", tc.name)
		}
		got := explain(err)
		lines := strings.Split(got, "\n")
		if len(lines) != 2 || !strings.HasPrefix(lines[0], "hub 不认这条配对链接（") || !strings.Contains(lines[0], "personad hub pair --machine <这台的 id> --add 拿") ||
			lines[1] != "（原始错误："+err.Error()+"）" {
			t.Fatalf("%s: explain:\n%s", tc.name, got)
		}
	}
}

func TestExplainPrefix(t *testing.T) {
	for _, tc := range []struct {
		name   string
		err    error
		prefix string
	}{
		// One address refused the connection, the other was the hub saying no: the token is what to fix.
		{"rejected when one candidate answered",
			errors.Join(refused("https://192.0.2.10:18662"), errors.New("https://hub.example.com:18662: hub 拒绝了：invalid token")),
			"hub 不认这条配对链接"},
		{"a name that does not resolve",
			fmt.Errorf("配对失败：%w", &url.Error{Op: "Post", URL: "https://hub.example.invalid:443/api/v1/auth/tokens",
				Err: &net.OpError{Op: "dial", Net: "tcp", Err: &net.DNSError{Err: "no such host", Name: "hub.example.invalid", IsNotFound: true}}}),
			"连不上 hub（hub.example.invalid:443）："},
		{"a hub that times out",
			fmt.Errorf("配对失败：%w", &url.Error{Op: "Post", URL: "https://192.0.2.10:18662/api/v1/auth/tokens", Err: context.DeadlineExceeded}),
			"连不上 hub（192.0.2.10:18662）："},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := explain(tc.err); !strings.HasPrefix(got, tc.prefix) {
				t.Fatalf("explain:\n%s", got)
			}
		})
	}
}

func TestExplainAHubThatIsNotThere(t *testing.T) {
	// A port nothing listens on: taken and closed again.
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := l.Addr().String()
	l.Close()
	_, port, _ := net.SplitHostPort(addr)
	q := url.Values{"token": {"jp_x"}, "machine": {"pi"}, "url": {"https://127.0.0.1"}, "tls": {port}, "fp": {"pin"}}
	f := newFakeEnv(t)
	_, err = pair(f.env, "persona://link?"+q.Encode(), "")
	if err == nil {
		t.Fatal("paired with nothing")
	}
	got := explain(err)
	want := "连不上 hub（" + addr + "）：检查 hub 在不在跑、地址对不对、这台电脑的网络通不通，再试一次\n（原始错误：" + err.Error() + "）"
	if got != want {
		t.Fatalf("explain:\n%s\nwant:\n%s", got, want)
	}
}

func TestExplainLeavesOtherErrorsAlone(t *testing.T) {
	for _, err := range []error{
		errors.New("还没配对"),
		// One candidate answered something else: not "cannot reach the hub".
		errors.Join(refused("https://192.0.2.10:18662"), errors.New("hub 没给出链路凭据：升级 hub")),
		// A 403 next to an address with 401 in it is still a 403.
		errors.Join(errors.New("https://hub.example.com:18662: hub 拒绝了：this pairing code is for machine \"nas\""), refused("https://192.0.2.10:14010")),
		errors.New("配对失败：https://192.0.2.10:18662: hub 拒绝了：too many authentication failures"),
		// No request to a hub: a network error here is not the hub's.
		fmt.Errorf("在局域网里找不了 hub（mDNS）：%w", &net.OpError{Op: "dial", Net: "udp", Err: syscall.ENETUNREACH}),
		context.DeadlineExceeded,
		// A 403 is a link for another machine, not a stale one.
		errors.New("配对失败：https://192.0.2.10:18662: hub 拒绝了：this pairing code is for machine \"nas\""),
	} {
		if got := explain(err); got != err.Error() {
			t.Errorf("explain(%q) = %q", err, got)
		}
	}
}

// refused is a candidate whose TCP dial was refused, as http.Client reports it.
func refused(base string) error {
	return &url.Error{Op: "Post", URL: base + "/api/v1/auth/tokens", Err: &net.OpError{Op: "dial", Net: "tcp", Err: syscall.ECONNREFUSED}}
}

func TestNodeHint(t *testing.T) {
	if got := nodeHint("dial unix /x/node.sock: connect: connection refused"); !strings.Contains(got, "persona-link node status") {
		t.Fatalf("hint %q", got)
	}
	// The link's own words already say how to run the node.
	if got := nodeHint("本机的" + link.NodeDownMarker + "（/x/node.sock 连不上）：persona-link node install（systemd 用户服务）"); got != "" {
		t.Fatalf("hint %q", got)
	}
}

func TestStatusSaysHowToCheckAnOfflineNode(t *testing.T) {
	f := newFakeEnv(t)
	f.pairHere(t)
	release, err := link.LockRun(f.configPath())
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	for _, tc := range []struct {
		nodeErr string
		hint    bool
	}{
		{"dial unix /x/node.sock: connect: connection refused", true},
		{"本机的" + link.NodeDownMarker + "（/x/node.sock 连不上）：persona-link node install（systemd 用户服务）", false},
	} {
		f.saveStatus(t, link.Status{State: "connected", Kind: "lan", Address: "https://192.0.2.10:18662", Node: "offline", NodeError: tc.nodeErr}, 4321)
		f.out.Reset()
		_ = status(f.env, nil)
		if got := strings.Count(f.out.String(), "persona-link node status 看常驻 node"); got != map[bool]int{true: 1, false: 0}[tc.hint] {
			t.Errorf("%q: hint %d times:\n%s", tc.nodeErr, got, f.out)
		}
	}
}
