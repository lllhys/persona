package relay

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/apns"
)

type fakeAPNs struct {
	mu   sync.Mutex
	got  []apns.Request
	resp apns.Result
}

func (f *fakeAPNs) Send(_ context.Context, r apns.Request) (apns.Result, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.got = append(f.got, r)
	if f.resp.Status == 0 {
		return apns.Result{Status: 200}, nil
	}
	return f.resp, nil
}

const topic = "app.example"
const devTok = "a1b2c3d4e5f60718293a4b5c6d7e8f90a1b2c3d4e5f60718293a4b5c6d7e8f90"

func newTest(t *testing.T) (*Server, *fakeAPNs, http.Handler) {
	f := &fakeAPNs{}
	s, err := New(Config{Topics: []string{topic}}, f, bytes.Repeat([]byte{7}, 32), t.Logf)
	if err != nil {
		t.Fatal(err)
	}
	return s, f, s.Handler()
}

func call(h http.Handler, path, auth string, body any) (int, map[string]any) {
	b, _ := json.Marshal(body)
	r := httptest.NewRequest("POST", path, bytes.NewReader(b))
	r.RemoteAddr = "192.0.2.1:1234"
	if auth != "" {
		r.Header.Set("Authorization", "Bearer "+auth)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	var out map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &out)
	return w.Code, out
}

func register(t *testing.T, h http.Handler) string {
	code, out := call(h, "/relay/v1/register", "", map[string]string{"apns_token": devTok, "env": "sandbox", "topic": topic})
	if code != 200 {
		t.Fatalf("register: %d %v", code, out)
	}
	return out["grant"].(string)
}

var sealed = base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{1}, 120))

func TestAlertCarriesOnlyTheSealedPayload(t *testing.T) {
	_, f, h := newTest(t)
	g := register(t, h)
	if strings.Contains(g, devTok) {
		t.Fatal("grant shows the device token")
	}
	code, out := call(h, "/relay/v1/push", g, PushRequest{Type: "alert", Sealed: sealed, CollapseID: "abc123"})
	if code != 200 {
		t.Fatalf("push: %d %v", code, out)
	}
	r := f.got[0]
	if r.Token != devTok || r.Env != "sandbox" || r.Topic != topic || r.PushType != "alert" || r.Priority != "10" || r.CollapseID != "abc123" {
		t.Fatalf("request %+v", r)
	}
	var p map[string]any
	_ = json.Unmarshal(r.Body, &p)
	aps := p["aps"].(map[string]any)
	alert := aps["alert"].(map[string]any)
	if p["e"] != sealed || aps["mutable-content"] != 1.0 || alert["body"] != FallbackBody || alert["title"] != FallbackTitle || len(p) != 2 {
		t.Fatalf("payload %s", r.Body)
	}
}

func TestBackgroundIsLowPriority(t *testing.T) {
	_, f, h := newTest(t)
	g := register(t, h)
	if code, out := call(h, "/relay/v1/push", g, PushRequest{Type: "background", Priority: "10", Sealed: sealed}); code != 200 {
		t.Fatalf("push: %d %v", code, out)
	}
	r := f.got[0]
	if r.PushType != "background" || r.Priority != "5" || !bytes.Contains(r.Body, []byte(`"content-available":1`)) || bytes.Contains(r.Body, []byte("alert")) {
		t.Fatalf("request %+v %s", r, r.Body)
	}
}

func TestLiveActivityPassesItsAPS(t *testing.T) {
	_, f, h := newTest(t)
	g := register(t, h)
	aps := map[string]any{"event": "update", "timestamp": 1, "content-state": map[string]any{"phase": "working"}}
	if code, out := call(h, "/relay/v1/push", g, PushRequest{Type: "liveactivity", Token: "ABCDEF0123456789ABCDEF0123456789", Priority: "5", APS: aps}); code != 200 {
		t.Fatalf("push: %d %v", code, out)
	}
	r := f.got[0]
	if r.Token != "abcdef0123456789abcdef0123456789" || r.PushType != "liveactivity" || r.Priority != "5" || r.Topic != topic {
		t.Fatalf("request %+v", r)
	}
	// An alert only starts an activity; an update cannot carry one.
	aps["alert"] = map[string]any{"title": "x", "body": "y"}
	if code, _ := call(h, "/relay/v1/push", g, PushRequest{Type: "liveactivity", Token: "abcdef0123456789abcdef0123456789", APS: aps}); code != 400 {
		t.Fatalf("alert on update: %d", code)
	}
	aps["event"] = "start"
	if code, _ := call(h, "/relay/v1/push", g, PushRequest{Type: "liveactivity", Token: "abcdef0123456789abcdef0123456789", APS: aps}); code != 200 {
		t.Fatalf("alert on start: %d", code)
	}
	delete(aps, "alert")
	aps["event"] = "update"
	// Anything else than the Live Activity keys is refused (no smuggled alerts with sounds, categories…).
	aps["mutable-content"] = 1
	if code, _ := call(h, "/relay/v1/push", g, PushRequest{Type: "liveactivity", Token: "abcdef0123456789abcdef0123456789", APS: aps}); code != 400 {
		t.Fatalf("extra aps key: %d", code)
	}
}

func registerVoIP(t *testing.T, h http.Handler) string {
	code, out := call(h, "/relay/v1/register", "", map[string]string{"apns_token": devTok, "env": "sandbox", "topic": topic + ".voip"})
	if code != 200 {
		t.Fatalf("register voip: %d %v", code, out)
	}
	return out["grant"].(string)
}

// TestVoIPCarriesOnlyTheSealedPayload: an urgent call's push has no aps (PushKit hands it to the app, which names the
// call from the sealed payload), goes out at priority 10 on the ".voip" topic, and expires within a minute.
func TestVoIPCarriesOnlyTheSealedPayload(t *testing.T) {
	s, f, h := newTest(t)
	now := time.Unix(1_800_000_000, 0)
	s.now = func() time.Time { return now }
	g := registerVoIP(t, h)
	if code, out := call(h, "/relay/v1/push", g, PushRequest{Type: "voip", Priority: "5", Sealed: sealed, CollapseID: "c0ffee"}); code != 200 {
		t.Fatalf("push: %d %v", code, out)
	}
	r := f.got[0]
	if r.Token != devTok || r.Topic != topic+".voip" || r.PushType != "voip" || r.Priority != "10" || r.CollapseID != "c0ffee" {
		t.Fatalf("request %+v", r)
	}
	if r.Expiration != now.Unix()+30 {
		t.Fatalf("expiration %d, want now+30", r.Expiration-now.Unix())
	}
	var p map[string]any
	_ = json.Unmarshal(r.Body, &p)
	if p["e"] != sealed || len(p) != 1 {
		t.Fatalf("payload %s", r.Body)
	}
	for ttl, want := range map[int]int64{5: 5, 60: 60, 600: 60} {
		if code, _ := call(h, "/relay/v1/push", g, PushRequest{Type: "voip", Sealed: sealed, TTL: ttl}); code != 200 {
			t.Fatalf("ttl %d: %d", ttl, code)
		}
		if got := f.got[len(f.got)-1].Expiration - now.Unix(); got != want {
			t.Errorf("ttl %d: expires in %d s, want %d", ttl, got, want)
		}
	}
}

// TestVoIPGrantsDoNotMix: a VoIP grant carries only voip pushes and an ordinary grant none, and only the served
// bundle's ".voip" topic registers.
func TestVoIPGrantsDoNotMix(t *testing.T) {
	_, f, h := newTest(t)
	voip, plain := registerVoIP(t, h), register(t, h)
	for name, c := range map[string]struct {
		grant string
		req   PushRequest
	}{
		"alert on voip":      {voip, PushRequest{Type: "alert", Sealed: sealed}},
		"background on voip": {voip, PushRequest{Type: "background", Sealed: sealed}},
		"la on voip":         {voip, PushRequest{Type: "liveactivity", Token: "abcdef0123456789abcdef0123456789", APS: map[string]any{"event": "update", "content-state": map[string]any{}}}},
		"voip on alert":      {plain, PushRequest{Type: "voip", Sealed: sealed}},
		"voip plaintext":     {voip, PushRequest{Type: "voip", Sealed: "aGVsbG8="}},
	} {
		if code, _ := call(h, "/relay/v1/push", c.grant, c.req); code != 400 {
			t.Errorf("%s: %d", name, code)
		}
	}
	for _, tp := range []string{"other.app.voip", topic + ".voip.voip", topic + ".VOIP"} {
		if code, _ := call(h, "/relay/v1/register", "", map[string]string{"apns_token": devTok, "env": "sandbox", "topic": tp}); code != 403 {
			t.Errorf("register %s: %d", tp, code)
		}
	}
	if len(f.got) != 0 {
		t.Fatalf("sent %d", len(f.got))
	}
}

// TestVoIPRateLimit: a device's calls have their own small budget, apart from its notifications.
func TestVoIPRateLimit(t *testing.T) {
	s, _, h := newTest(t)
	now := time.Unix(1_800_000_000, 0)
	s.now = func() time.Time { return now }
	g := registerVoIP(t, h)
	ok := 0
	for range voipBurst + 4 {
		if code, _ := call(h, "/relay/v1/push", g, PushRequest{Type: "voip", Sealed: sealed}); code == 200 {
			ok++
		}
	}
	if ok != voipBurst {
		t.Fatalf("burst let %d calls through, want %d", ok, voipBurst)
	}
	now = now.Add(5 * time.Minute)
	if code, _ := call(h, "/relay/v1/push", g, PushRequest{Type: "voip", Sealed: sealed}); code != 200 {
		t.Fatalf("after five minutes: %d", code)
	}
}

func TestHealthListsPushTypes(t *testing.T) {
	_, _, h := newTest(t)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/relay/v1/health", nil))
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"voip"`) {
		t.Fatalf("health: %d %s", w.Code, w.Body)
	}
}

func TestRefusesWithoutAValidGrant(t *testing.T) {
	s, f, h := newTest(t)
	g := register(t, h)
	req := PushRequest{Type: "alert", Sealed: sealed}
	if code, _ := call(h, "/relay/v1/push", "", req); code != 401 {
		t.Errorf("no grant: %d", code)
	}
	b := []byte(g)
	b[len(b)/2] ^= 1
	if code, _ := call(h, "/relay/v1/push", string(b), req); code != 401 {
		t.Errorf("tampered grant: %d", code)
	}
	// Another relay's grant (other secret).
	other, _ := New(Config{Topics: []string{topic}}, f, bytes.Repeat([]byte{8}, 32), t.Logf)
	if code, _ := call(h, "/relay/v1/push", other.seal(grant{Token: devTok, Env: "sandbox", Topic: topic}), req); code != 401 {
		t.Errorf("foreign grant: %d", code)
	}
	// A topic this relay no longer serves.
	if code, _ := call(h, "/relay/v1/push", s.seal(grant{Token: devTok, Env: "sandbox", Topic: "other.app"}), req); code != 403 {
		t.Errorf("foreign topic: %d", code)
	}
	if len(f.got) != 0 {
		t.Fatalf("sent %d", len(f.got))
	}
}

func TestRegisterValidates(t *testing.T) {
	_, _, h := newTest(t)
	for name, body := range map[string]map[string]string{
		"topic": {"apns_token": devTok, "env": "sandbox", "topic": "other.app"},
		"env":   {"apns_token": devTok, "env": "dev", "topic": topic},
		"token": {"apns_token": "not hex", "env": "sandbox", "topic": topic},
	} {
		if code, _ := call(h, "/relay/v1/register", "", body); code == 200 {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestRefusesPlaintextAndBadEnvelopes(t *testing.T) {
	_, f, h := newTest(t)
	g := register(t, h)
	for name, req := range map[string]PushRequest{
		"plaintext":   {Type: "alert", Sealed: "aGVsbG8="},
		"not base64":  {Type: "alert", Sealed: "!!"},
		"type":        {Type: "voip", Sealed: sealed},
		"collapse":    {Type: "alert", Sealed: sealed, CollapseID: "ix_1"},
		"huge":        {Type: "alert", Sealed: base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{1}, 2900))},
		"la no token": {Type: "liveactivity", APS: map[string]any{"event": "update", "content-state": map[string]any{}}},
		"la no state": {Type: "liveactivity", Token: "abcdef0123456789abcdef0123456789", APS: map[string]any{"event": "update"}},
	} {
		if code, _ := call(h, "/relay/v1/push", g, req); code < 400 || code >= 500 {
			t.Errorf("%s: %d", name, code)
		}
	}
	if len(f.got) != 0 {
		t.Fatalf("sent %d", len(f.got))
	}
}

func TestDeadTokenIs410(t *testing.T) {
	_, f, h := newTest(t)
	f.resp = apns.Result{Status: 400, Reason: "BadDeviceToken"}
	g := register(t, h)
	code, out := call(h, "/relay/v1/push", g, PushRequest{Type: "alert", Sealed: sealed})
	if code != 410 || out["reason"] != "BadDeviceToken" {
		t.Fatalf("%d %v", code, out)
	}
}

// APNs' answers that may pass are 502 (the hub tries again); its permanent rejections are 422 with APNs' reason, so
// the hub neither tries them again nor reads them as a stale grant (401).
func TestAPNsRejectStatus(t *testing.T) {
	_, f, h := newTest(t)
	g := register(t, h)
	for _, c := range []struct {
		apns   int
		reason string
		want   int
	}{
		{400, "BadCollapseId", 422},
		{400, "DeviceTokenNotForTopic", 422},
		{403, "InvalidProviderToken", 422},
		{413, "PayloadTooLarge", 422},
		{429, "TooManyRequests", 502},
		{500, "InternalServerError", 502},
		{503, "ServiceUnavailable", 502},
	} {
		f.resp = apns.Result{Status: c.apns, Reason: c.reason}
		code, out := call(h, "/relay/v1/push", g, PushRequest{Type: "alert", Sealed: sealed})
		if code != c.want || out["reason"] != c.reason || out["error"] != fmt.Sprintf("apns %d", c.apns) {
			t.Errorf("APNs %d %s: %d %v, want %d", c.apns, c.reason, code, out, c.want)
		}
	}
}

func TestRateLimits(t *testing.T) {
	s, _, h := newTest(t)
	now := time.Unix(1_800_000_000, 0)
	s.now = func() time.Time { return now }
	g := register(t, h)
	ok := 0
	for range pushBurst + 10 {
		if code, _ := call(h, "/relay/v1/push", g, PushRequest{Type: "alert", Sealed: sealed}); code == 200 {
			ok++
		}
	}
	if ok != pushBurst {
		t.Fatalf("burst let %d through, want %d", ok, pushBurst)
	}
	// A fresh grant for the same device shares its budget.
	if code, _ := call(h, "/relay/v1/push", register(t, h), PushRequest{Type: "alert", Sealed: sealed}); code != 429 {
		t.Fatalf("new grant: %d", code)
	}
	now = now.Add(time.Minute)
	if code, _ := call(h, "/relay/v1/push", g, PushRequest{Type: "alert", Sealed: sealed}); code != 200 {
		t.Fatalf("after a minute: %d", code)
	}
	regs := 0
	for range regBurst + 5 {
		if code, _ := call(h, "/relay/v1/register", "", map[string]string{"apns_token": devTok, "env": "sandbox", "topic": topic}); code == 200 {
			regs++
		}
	}
	if regs >= regBurst+5 {
		t.Fatal("registration not limited")
	}
}

// TestLimiterSweepsAtMostOncePerRefill: past sweepAbove keys a new key forgets the buckets that have refilled, but not
// again within one refill time, so a flood of new keys does not scan the whole table for each. The answers stay the same.
func TestLimiterSweepsAtMostOncePerRefill(t *testing.T) {
	l := newLimiter(regPerMin, regBurst)
	add := func(prefix string, n int, at time.Time) {
		t.Helper()
		for i := range n {
			if !l.allow(prefix+strconv.Itoa(i), at) {
				t.Fatalf("new key %s%d refused", prefix, i)
			}
		}
	}
	burst := func(key string, at time.Time) (n int) {
		for range regBurst + 5 {
			if l.allow(key, at) {
				n++
			}
		}
		return n
	}
	check := func(when string, buckets int, swept time.Time) {
		t.Helper()
		if len(l.buckets) != buckets || !l.swept.Equal(swept) {
			t.Fatalf("%s: %d buckets, swept at %v; want %d, %v", when, len(l.buckets), l.swept, buckets, swept)
		}
	}
	t0 := time.Unix(1_800_000_000, 0)
	add("ip:old", sweepAbove+1, t0)
	check("filled", sweepAbove+1, time.Time{})

	// The first new key past the size sweeps; every bucket is fresh, so none goes.
	t1 := t0.Add(l.refill / 2)
	add("ip:a", 1, t1)
	check("first sweep", sweepAbove+2, t1)

	// The old buckets have refilled, but the last sweep was less than one refill time ago: new keys do not sweep.
	t2 := t0.Add(l.refill + time.Millisecond)
	add("ip:b", 1000, t2)
	check("within a refill time of the sweep", sweepAbove+1002, t1)
	// A refilled bucket that stayed answers like the new one it would have become: a full burst.
	if got := burst("ip:old0", t2); got != regBurst {
		t.Fatalf("kept refilled bucket allowed %d, want %d", got, regBurst)
	}

	// One refill time after the last sweep, a new key forgets every bucket that has refilled. Kept: ip:a0 (exactly one
	// refill time old), ip:old0 (spent at t2), the 1000 ip:b keys and ip:c0.
	t3 := t1.Add(l.refill)
	add("ip:c", 1, t3)
	check("second sweep", 1003, t3)
	if got := burst("ip:old1", t3); got != regBurst {
		t.Fatalf("forgotten key allowed %d, want a full burst of %d", got, regBurst)
	}
	if got := burst("ip:old0", t3); got >= regBurst {
		t.Fatalf("key spent half a refill time ago allowed %d, want fewer than %d", got, regBurst)
	}
}

// BenchmarkLimiterManyFreshKeys: new keys arriving while the table is past sweepAbove and every bucket is still fresh.
func BenchmarkLimiterManyFreshKeys(b *testing.B) {
	l := newLimiter(regPerMin, regBurst)
	now := time.Unix(1_800_000_000, 0)
	for i := range sweepAbove + 1 {
		l.allow("ip:seed"+strconv.Itoa(i), now)
	}
	b.ResetTimer()
	for i := range b.N {
		l.allow("ip:new"+strconv.Itoa(i), now)
	}
	b.ReportMetric(float64(len(l.buckets)), "buckets")
}

func TestProxySecret(t *testing.T) {
	f := &fakeAPNs{}
	s, _ := New(Config{Topics: []string{topic}, ProxySecret: "s3cret"}, f, bytes.Repeat([]byte{7}, 32), t.Logf)
	h := s.Handler()
	r := httptest.NewRequest("GET", "/relay/v1/health", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatalf("around the proxy: %d", w.Code)
	}
	r.Header.Set("X-Relay-Proxy-Secret", "s3cret")
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatalf("through the proxy: %d", w.Code)
	}
	r = httptest.NewRequest("POST", "/", nil)
	r.RemoteAddr = "192.0.2.1:1"
	r.Header.Set("X-Forwarded-For", "198.51.100.7, 203.0.113.9")
	if s.clientIP(r) != "198.51.100.7" {
		t.Fatalf("client %s", s.clientIP(r))
	}
	plain, _, _ := newTest(t)
	if plain.clientIP(r) != "192.0.2.1" {
		t.Fatal("trusted X-Forwarded-For without a proxy secret")
	}
}

// TestNewRefusesBlankTopic: a blank entry in relay.json's topics matches no app and hides a config mistake.
func TestNewRefusesBlankTopic(t *testing.T) {
	for _, topics := range [][]string{{""}, {" "}, {topic, ""}} {
		if _, err := New(Config{Topics: topics}, &fakeAPNs{}, bytes.Repeat([]byte{7}, 32), t.Logf); err == nil {
			t.Fatalf("New(topics %q) succeeded, want an error", topics)
		}
	}
}

// startServe runs serve on a loopback port with a handler that reports entry and blocks until release is closed.
func startServe(t *testing.T) (url string, entered, release chan struct{}, cancel context.CancelFunc, served chan error) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	entered, release = make(chan struct{}), make(chan struct{})
	hs := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		close(entered)
		<-release
		w.WriteHeader(http.StatusOK)
	})}
	ctx, cancel := context.WithCancel(context.Background())
	served = make(chan error, 1)
	go func() { served <- serve(ctx, ln, hs) }()
	return "http://" + ln.Addr().String() + "/relay/v1/push", entered, release, cancel, served
}

func TestServeShutdownDrainsInFlightRequest(t *testing.T) {
	url, entered, release, cancel, served := startServe(t)
	defer cancel()
	status := make(chan int, 1)
	go func() {
		resp, err := http.Post(url, "application/json", strings.NewReader("{}"))
		if err != nil {
			status <- -1
			return
		}
		resp.Body.Close()
		status <- resp.StatusCode
	}()
	<-entered
	cancel()
	select {
	case err := <-served:
		close(release)
		t.Fatalf("serve returned %v while a request was still in flight", err)
	case <-time.After(200 * time.Millisecond):
	}
	close(release)
	if got := <-status; got != http.StatusOK {
		t.Fatalf("in-flight request status = %d, want 200", got)
	}
	select {
	case err := <-served:
		if err != nil {
			t.Fatalf("serve = %v, want nil", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("serve did not return after the drain")
	}
}

func TestServeShutdownGraceExceeded(t *testing.T) {
	old := shutdownGrace
	shutdownGrace = 50 * time.Millisecond
	defer func() { shutdownGrace = old }()
	url, entered, release, cancel, served := startServe(t)
	defer close(release)
	go func() {
		if resp, err := http.Post(url, "application/json", strings.NewReader("{}")); err == nil {
			resp.Body.Close()
		}
	}()
	<-entered
	cancel()
	select {
	case err := <-served:
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("serve = %v, want a deadline error", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("serve did not give up after the grace period")
	}
}

// TestHTTPServerClosesStalledConnections: with proxy_secret unset the relay listens directly, and register reads the
// body before checking anything. A client that sends headers and stalls the body, or leaves a keep-alive connection
// idle, must not hold a goroutine and an fd for ever. Each case shortens only the limit it tests and waits less than
// the other one: net/http falls back to ReadTimeout when IdleTimeout is 0, so equal values would not tell them apart.
func TestHTTPServerClosesStalledConnections(t *testing.T) {
	oldRead, oldIdle := readTimeout, idleTimeout
	defer func() { readTimeout, idleTimeout = oldRead, oldIdle }()
	_, _, h := newTest(t)
	for _, tc := range []struct {
		name, req, want string
		read, idle      time.Duration
	}{
		{"stalled body", "POST /relay/v1/register HTTP/1.1\r\nHost: x\r\nContent-Length: 4000\r\n\r\n{\"apns_token\":", "HTTP/1.1 400 ",
			300 * time.Millisecond, 10 * time.Second},
		{"idle keep-alive", "GET /relay/v1/health HTTP/1.1\r\nHost: x\r\n\r\n", "HTTP/1.1 200 ", 10 * time.Second, 300 * time.Millisecond},
	} {
		readTimeout, idleTimeout = tc.read, tc.idle
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithCancel(context.Background())
		served := make(chan error, 1)
		go func() { served <- serve(ctx, ln, newHTTPServer(h)) }()
		c, err := net.Dial("tcp", ln.Addr().String())
		if err != nil {
			t.Fatal(err)
		}
		_ = c.SetDeadline(time.Now().Add(5 * time.Second))
		if _, err := io.WriteString(c, tc.req); err != nil {
			t.Fatalf("%s: write: %v", tc.name, err)
		}
		got, err := io.ReadAll(c)
		c.Close()
		cancel()
		<-served
		if errors.Is(err, os.ErrDeadlineExceeded) {
			t.Fatalf("%s: the relay kept the connection open: %v", tc.name, err)
		}
		if !strings.HasPrefix(string(got), tc.want) {
			t.Fatalf("%s: answer %q, want %q...", tc.name, got, tc.want)
		}
	}
}
