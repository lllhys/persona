package ext

import (
	"encoding/base64"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/nwverse/persona/sdk/go/wire"
)

// A settings check's client goes through the check's proxy, with its credential (#8459).
func TestSettingsCheckClientUsesTheChecksProxy(t *testing.T) {
	var got struct{ auth, url string }
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got.auth, got.url = r.Header.Get("Proxy-Authorization"), r.URL.String()
		_, _ = io.WriteString(w, "through")
	}))
	defer proxy.Close()
	params := wire.SettingsCheckParams{Proxy: "http://ext:check-cred@" + strings.TrimPrefix(proxy.URL, "http://")}
	res, err := SettingsCheckClient(params).Get("http://gateway.example.com/health")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(res.Body)
	_ = res.Body.Close()
	if string(body) != "through" {
		t.Fatalf("body %q: not through the proxy", body)
	}
	if want := "Basic " + base64.StdEncoding.EncodeToString([]byte("ext:check-cred")); got.auth != want {
		t.Fatalf("Proxy-Authorization %q, want the check's credential", got.auth)
	}
	if got.url != "http://gateway.example.com/health" {
		t.Fatalf("proxy got %q", got.url)
	}
}

// Without a check proxy (an older hub, a program outside the proxy) it is an ordinary client.
func TestSettingsCheckClientWithoutProxy(t *testing.T) {
	for _, proxy := range []string{"", "::not a url"} {
		c := SettingsCheckClient(wire.SettingsCheckParams{Proxy: proxy})
		if c == nil || c.Transport != nil || c == http.DefaultClient {
			t.Fatalf("proxy %q: want a client of its own on the default transport, got %+v", proxy, c)
		}
	}
}
