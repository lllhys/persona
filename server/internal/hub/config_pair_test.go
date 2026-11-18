package hub

import (
	"bytes"
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/mdp/qrterminal/v3"
)

func TestPairURLMetadataContract(t *testing.T) {
	p := pairCode{URL: "http://example.com:18660", Token: "temporary", HubID: "hub-example", FP: strings.Repeat("A", 43), TLSPort: 18662,
		Alt: []string{"https://[2001:db8::1]:18662", "https://example.com:18662"}}
	u, err := url.Parse(p.pairURL())
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	if u.Scheme != "persona" || u.Host != "pair" || q.Get("url") != p.URL || q.Get("token") != p.Token || q.Get("hub") != p.HubID || q.Get("fp") != p.FP || q.Get("tls") != "18662" || strings.Join(q["alt"], " ") != strings.Join(p.Alt, " ") {
		t.Fatal("pairing parameter contract changed")
	}
}

func TestPairCLIUsesLoopbackAndTemporaryToken(t *testing.T) {
	setPairLanguage(t, "zh-Hans")
	p := pairCode{URL: "http://example.com:18660", Token: "jp_temporary", ExpiresAt: time.Now().Add(pairLifetime), HubID: "hub-example", FP: strings.Repeat("A", 43), TLSPort: 18662, Alt: []string{"https://example.com:18662"}}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == "/" { // a node's port probe (node/ports.go), not this test's client
			http.NotFound(w, r)
			return
		}
		if r.Method != "POST" || r.URL.Path != "/api/v1/auth/pair-codes" || r.Header.Get("Authorization") != "Bearer master-secret" {
			t.Error("bad local pairing request")
		}
		if host, _, _ := net.SplitHostPort(r.Host); host != "127.0.0.1" {
			t.Error("pair request did not use IPv4 loopback")
		}
		_ = json.NewEncoder(w).Encode(p)
	}))
	defer srv.Close()
	_, port, _ := net.SplitHostPort(srv.Listener.Addr().String())
	cfg := &Config{Listen: net.JoinHostPort("192.0.2.1", port), Token: "master-secret", PublicURL: "http://example.com:18660"}
	var out bytes.Buffer
	if err := cfg.printPair(context.Background(), false, &out); err != nil {
		t.Fatal(err)
	}
	printed := out.String()
	for _, want := range []string{p.pairURL(), "有效期至：", "公钥指纹：" + p.FP} {
		if !strings.Contains(printed, want) {
			t.Fatalf("missing %s", want)
		}
	}
	if strings.Contains(printed, cfg.Token) {
		t.Fatal("default pairing printed the master credential")
	}
}

func TestPairCLIOfflineAndLegacy(t *testing.T) {
	setPairLanguage(t, "zh-Hans")
	srv := httptest.NewServer(http.NotFoundHandler())
	_, port, _ := net.SplitHostPort(srv.Listener.Addr().String())
	srv.Close()
	cfg := &Config{Listen: ":" + port, Token: "a&b=+ ?", PublicURL: "http://example.com:18660/path?a=1"}
	var out bytes.Buffer
	err := cfg.printPair(context.Background(), false, &out)
	if err == nil || !strings.Contains(err.Error(), "未运行") || !strings.Contains(err.Error(), "--legacy") || out.Len() != 0 {
		t.Fatalf("offline error %v", err)
	}
	if err := cfg.printPair(context.Background(), true, &out); err != nil {
		t.Fatal(err)
	}
	const oldURL = "persona://pair?token=a%26b%3D%2B+%3F&url=http%3A%2F%2Fexample.com%3A18660%2Fpath%3Fa%3D1"
	if cfg.PairURL() != oldURL {
		t.Fatalf("legacy URL changed: %s", cfg.PairURL())
	}
	var qr bytes.Buffer
	qrterminal.GenerateHalfBlock(oldURL, qrterminal.L, &qr)
	want := "Server: " + cfg.PublicURL + "\nToken:  " + cfg.Token + "\n\n用 Persona App 扫码配对：\n\n" + qr.String() +
		"\nMac 上拷贝这条链接，在 Persona 的「连接 hub」里粘贴：\n" + oldURL + "\n"
	if out.String() != want {
		t.Fatal("legacy output changed")
	}
}

func TestPairCLIRejectsRedirectAndInvalidResponse(t *testing.T) {
	for _, status := range []int{http.StatusTemporaryRedirect, http.StatusOK} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Location", "https://example.com")
			w.WriteHeader(status)
			_, _ = w.Write([]byte(`{}`))
		}))
		_, port, _ := net.SplitHostPort(srv.Listener.Addr().String())
		cfg := &Config{Listen: ":" + port, Token: "master"}
		if _, err := cfg.requestPairCode(context.Background()); err == nil {
			t.Fatal("bad pairing response accepted")
		}
		srv.Close()
	}
}

// A first Docker start leaves public_url empty: `hub pair` says what to set instead of "invalid pairing info".
func TestPairCLISaysWhenTheHubHasNoAddress(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(pairCode{Token: "jp_temporary", ExpiresAt: time.Now().Add(pairLifetime)})
	}))
	defer srv.Close()
	_, port, _ := net.SplitHostPort(srv.Listener.Addr().String())
	cfg := &Config{Listen: ":" + port, Token: "master"}
	_, err := cfg.requestPairCode(context.Background())
	if err == nil || !strings.Contains(err.Error(), "PERSONA_PUBLIC_URL") || !strings.Contains(err.Error(), "public_url") {
		t.Fatalf("empty address: %v", err)
	}
}
