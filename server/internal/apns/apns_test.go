package apns

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// writeKey writes a fresh P-256 key as a PKCS#8 PEM, the shape of Apple's .p8 file.
func writeKey(t *testing.T) string {
	t.Helper()
	k, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	der, err := x509.MarshalPKCS8PrivateKey(k)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "AuthKey.p8")
	if err := os.WriteFile(path, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// TestNewRequiresKeyIDAndTeamID: a config without key_id or team_id would sign tokens Apple refuses with 403
// InvalidProviderToken, so New refuses it at boot.
func TestNewRequiresKeyIDAndTeamID(t *testing.T) {
	key := writeKey(t)
	for _, tc := range []struct {
		name, keyID, teamID, want string
	}{
		{"empty key_id", "", "TEAM123456", "key_id is empty"},
		{"empty team_id", "KEY1234567", "", "team_id is empty"},
		{"blank key_id", "  \t", "TEAM123456", "key_id is empty"},
		{"blank team_id", "KEY1234567", " \n", "team_id is empty"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := New(Config{KeyID: tc.keyID, TeamID: tc.teamID, KeyPath: key})
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("New() error = %v, want %q", err, tc.want)
			}
		})
	}
}

// TestNewTrimsIDs: surrounding whitespace in the ids is dropped, so the token carries the bare ids.
func TestNewTrimsIDs(t *testing.T) {
	c, err := New(Config{KeyID: " KEY1234567 ", TeamID: "TEAM123456\n", KeyPath: writeKey(t)})
	if err != nil {
		t.Fatal(err)
	}
	if c.Cfg.KeyID != "KEY1234567" || c.Cfg.TeamID != "TEAM123456" {
		t.Fatalf("Cfg = %+v, want trimmed ids", c.Cfg)
	}
	if _, err := c.token(); err != nil {
		t.Fatal(err)
	}
}

// testClient is a client with a fresh key that posts to an HTTP/2 TLS test server answering with answer(n), n
// being the 1-based request number. It returns the authorization headers the server saw.
func testClient(t *testing.T, answer func(n int, w http.ResponseWriter)) (*Client, func() []string) {
	t.Helper()
	k, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var auths []string
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		auths = append(auths, r.Header.Get("authorization"))
		n := len(auths)
		mu.Unlock()
		answer(n, w)
	}))
	srv.EnableHTTP2 = true
	srv.StartTLS()
	t.Cleanup(srv.Close)
	c := NewWithKey(Config{KeyID: "KEY1234567", TeamID: "TEAM123456", Topic: "app.test"}, k)
	c.http, c.Base = srv.Client(), srv.URL
	return c, func() []string {
		mu.Lock()
		defer mu.Unlock()
		return append([]string(nil), auths...)
	}
}

func reject(w http.ResponseWriter, reason string) {
	w.WriteHeader(http.StatusForbidden)
	_, _ = w.Write([]byte(`{"reason":"` + reason + `"}`))
}

// TestSendResignsOnExpiredProviderToken: APNs calling the cached token expired (a hub that slept past its hour)
// makes Send sign a new token and post again once, instead of failing every push until the cache ages out.
func TestSendResignsOnExpiredProviderToken(t *testing.T) {
	c, auths := testClient(t, func(n int, w http.ResponseWriter) {
		if n == 1 {
			reject(w, "ExpiredProviderToken")
		}
	})
	res, err := c.Send(context.Background(), Request{Token: "abcd", PushType: "alert", Priority: "10", Body: []byte(`{}`)})
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != 200 {
		t.Fatalf("Send() = %+v, want 200 after re-signing", res)
	}
	got := auths()
	if len(got) != 2 {
		t.Fatalf("requests = %d, want 2", len(got))
	}
	if got[0] == got[1] {
		t.Fatal("retry reused the expired token")
	}
}

// TestSendExpiredTwiceRetriesOnce: a second ExpiredProviderToken is returned as is, not retried in a loop.
func TestSendExpiredTwiceRetriesOnce(t *testing.T) {
	c, auths := testClient(t, func(_ int, w http.ResponseWriter) { reject(w, "ExpiredProviderToken") })
	res, err := c.Send(context.Background(), Request{Token: "abcd", PushType: "alert", Priority: "10"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != 403 || res.Reason != "ExpiredProviderToken" || len(auths()) != 2 {
		t.Fatalf("Send() = %+v after %d requests, want 403 ExpiredProviderToken after 2", res, len(auths()))
	}
}

// TestSendInvalidProviderTokenNotRetried: a bad key or id stays bad, and re-signing too often earns a 429
// TooManyProviderTokenUpdates, so InvalidProviderToken is returned after one request and the token is kept.
func TestSendInvalidProviderTokenNotRetried(t *testing.T) {
	c, auths := testClient(t, func(_ int, w http.ResponseWriter) { reject(w, "InvalidProviderToken") })
	before, err := c.token()
	if err != nil {
		t.Fatal(err)
	}
	res, err := c.Send(context.Background(), Request{Token: "abcd", PushType: "alert", Priority: "10"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != 403 || res.Reason != "InvalidProviderToken" {
		t.Fatalf("Send() = %+v, want 403 InvalidProviderToken", res)
	}
	if n := len(auths()); n != 1 {
		t.Fatalf("requests = %d, want 1", n)
	}
	if after, _ := c.token(); after != before {
		t.Fatal("InvalidProviderToken re-signed the token")
	}
}

// TestTokenAgeIsWallClock: freshness is the token's iat against the wall clock, so a token whose iat is 50
// minutes old is re-signed even when no monotonic time passed (the machine slept), and a young one is reused.
func TestTokenAgeIsWallClock(t *testing.T) {
	c, _ := testClient(t, func(int, http.ResponseWriter) {})
	first, err := c.token()
	if err != nil {
		t.Fatal(err)
	}
	if again, _ := c.token(); again != first {
		t.Fatal("a fresh token was re-signed")
	}
	c.mu.Lock()
	c.jwtIat = time.Now().Add(-50 * time.Minute).Unix()
	c.mu.Unlock()
	next, err := c.token()
	if err != nil {
		t.Fatal(err)
	}
	if next == first {
		t.Fatal("a token with a 50-minute-old iat was reused")
	}
}

// TestResignKeepsNewerToken: when concurrent pushes all hit ExpiredProviderToken, only the first re-signs; the
// rest get that new token, so one wake-up does not trigger a burst of token updates.
func TestResignKeepsNewerToken(t *testing.T) {
	c, _ := testClient(t, func(int, http.ResponseWriter) {})
	stale, _ := c.token()
	fresh, err := c.resign(stale)
	if err != nil || fresh == stale {
		t.Fatalf("resign(stale) = %q, %v; want a new token", fresh, err)
	}
	if again, _ := c.resign(stale); again != fresh {
		t.Fatal("a second resign of the same stale token signed again")
	}
}

// TestVoIPTopicAndExpiration: a voip push goes to the bundle's ".voip" topic (added once) with its expiration.
func TestVoIPTopicAndExpiration(t *testing.T) {
	var mu sync.Mutex
	var topics, exps []string
	k, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		topics = append(topics, r.Header.Get("apns-topic"))
		exps = append(exps, r.Header.Get("apns-expiration"))
	}))
	srv.EnableHTTP2 = true
	srv.StartTLS()
	t.Cleanup(srv.Close)
	c := NewWithKey(Config{KeyID: "KEY1234567", TeamID: "TEAM123456", Topic: "app.test"}, k)
	c.http, c.Base = srv.Client(), srv.URL
	for _, r := range []Request{
		{Token: "abcd", PushType: "voip", Priority: "10", Expiration: 1_800_000_030},
		{Token: "abcd", Topic: "app.test.voip", PushType: "voip", Priority: "10"},
		{Token: "abcd", PushType: "alert", Priority: "10"},
	} {
		if res, err := c.Send(context.Background(), r); err != nil || res.Status != 200 {
			t.Fatalf("Send(%+v) = %+v, %v", r, res, err)
		}
	}
	mu.Lock()
	defer mu.Unlock()
	if strings.Join(topics, " ") != "app.test.voip app.test.voip app.test" {
		t.Errorf("topics %q", topics)
	}
	if exps[0] != "1800000030" || exps[1] != "" || exps[2] != "" {
		t.Errorf("expirations %q", exps)
	}
}
