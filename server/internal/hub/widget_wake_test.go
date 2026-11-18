package hub

import (
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/nwverse/persona/server/internal/pushcrypto"
	"github.com/nwverse/persona/server/internal/relayproto"
)

// The pace of the widgets' wakes (#5721): Needs You at once, other changes after widgetGap, a heartbeat when nothing
// changed, and the first time always.
func TestWidgetWakePace(t *testing.T) {
	var w widgetWaker
	t0 := time.Date(2026, 11, 12, 9, 0, 0, 0, time.UTC)
	c := widgetCounts{needsYou: 1, working: 2}
	if !w.due("phone", c, t0) {
		t.Fatal("first time: not due")
	}
	w.wrote("phone", c, t0)
	cases := []struct {
		name  string
		c     widgetCounts
		after time.Duration
		due   bool
	}{
		{"same counts, soon", c, time.Minute, false},
		{"needs you changed, at once", widgetCounts{needsYou: 2, working: 2}, time.Second, true},
		{"needs you back to none, at once", widgetCounts{needsYou: 0, working: 2}, time.Second, true},
		{"working changed, within the gap", widgetCounts{needsYou: 1, working: 3}, widgetGap - time.Second, false},
		{"working changed, after the gap", widgetCounts{needsYou: 1, working: 3}, widgetGap, true},
		{"finished changed, after the gap", widgetCounts{needsYou: 1, working: 2, unread: 1}, widgetGap, true},
		{"nothing changed, before the heartbeat", c, widgetHeartbeat - time.Second, false},
		{"nothing changed, the heartbeat", c, widgetHeartbeat, true},
	}
	for _, tc := range cases {
		if got := w.due("phone", tc.c, t0.Add(tc.after)); got != tc.due {
			t.Errorf("%s: due = %v, want %v", tc.name, got, tc.due)
		}
	}
	w.forget("phone")
	if !w.due("phone", c, t0.Add(time.Second)) {
		t.Error("forgotten: not due again")
	}
}

// Through the hub: a phone that has widgets and is not open gets one silent "widgets" wake for a change, none for the
// same counts again, another at once when Needs You changes; one without widgets gets none.
func TestWidgetWakePushes(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		h := newTestHub(t)
		key, err := ecdh.P256().GenerateKey(rand.Reader)
		if err != nil {
			t.Fatal(err)
		}
		sink := &widgetWakeSink{key: key}
		h.push.relay = &http.Client{Transport: sink}
		publicKey := base64.StdEncoding.EncodeToString(key.PublicKey().Bytes())
		for _, id := range []string{"phone", "other"} {
			if err := h.push.register(Device{DeviceID: id, Name: id, APNSToken: id + "-token",
				Topic: "app.example", PushKey: publicKey, RelayURL: "https://relay.example", RelayGrant: "grant-" + id}); err != nil {
				t.Fatal(err)
			}
		}
		prefs := devicePrefs(nil)
		prefs.Widgets = true
		if err := h.push.setPrefsBy("phone", "", prefs); err != nil {
			t.Fatal(err)
		}
		clock := time.Date(2026, 11, 12, 9, 0, 0, 0, time.UTC)
		h.live.now = func() time.Time { return clock }
		wakes := func() (phone, other int) {
			return sink.count("phone"), sink.count("other")
		}
		checkWakes := func(want int) {
			t.Helper()
			synctest.Wait()
			if got, _ := wakes(); got != want {
				t.Fatalf("phone wakes = %d, want %d", got, want)
			}
		}

		h.live.wakeWidgets(FleetState{Phase: "working", Working: 1})
		checkWakes(1)
		clock = clock.Add(time.Minute)
		h.live.wakeWidgets(FleetState{Phase: "working", Working: 1})
		h.live.wakeWidgets(FleetState{Phase: "working", Working: 2}) // within widgetGap: held
		checkWakes(1)
		if h.live.widgets.due("phone", widgetCounts{working: 2}, clock) {
			t.Fatal("a second change bypassed the inter-wake gap")
		}
		h.live.wakeWidgets(FleetState{Phase: "needsYou", NeedsYou: 1, Working: 2})
		checkWakes(2)
		if _, other := wakes(); other != 0 {
			t.Fatalf("a phone without widgets was woken %d times", other)
		}
		// The app went to the background with these counts: it wrote them, so no wake for them after it.
		h.live.phoneLeft("phone", true)
		clock = clock.Add(widgetGap)
		h.live.wakeWidgets(h.live.shownState)
		checkWakes(2)
		if h.live.widgets.due("phone", widgetCountsOf(h.live.shownState), clock) {
			t.Fatal("the counts written when the phone left were sent again")
		}
	})
}

// widgetWakeSink is an in-memory relay: synctest can settle the push goroutines without a live HTTP server.
// It opens the same sealed payload as the phone and counts only the real silent "widgets" wake.
type widgetWakeSink struct {
	key *ecdh.PrivateKey
	mu  sync.Mutex
	got []string
}

func (s *widgetWakeSink) RoundTrip(r *http.Request) (*http.Response, error) {
	var req relayproto.PushRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		return nil, err
	}
	_ = r.Body.Close()
	ciphertext, err := base64.StdEncoding.DecodeString(req.Sealed)
	if err != nil {
		return nil, err
	}
	plain, err := pushcrypto.Open(s.key, ciphertext)
	if err != nil {
		return nil, err
	}
	var body map[string]any
	if err := json.Unmarshal(plain, &body); err != nil {
		return nil, err
	}
	if req.Type == "background" && body["persona_wake"] == "widgets" {
		device := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer grant-")
		s.mu.Lock()
		s.got = append(s.got, device)
		s.mu.Unlock()
	}
	return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"ok":true}`)), Request: r}, nil
}

func (s *widgetWakeSink) count(device string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for _, got := range s.got {
		if got == device {
			n++
		}
	}
	return n
}
