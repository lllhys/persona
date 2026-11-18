package hub

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/db"
	"github.com/nwverse/persona/server/internal/i18n"
)

type registrationErrorStore struct {
	Store
	device   func(context.Context, db.Device) error
	activity func(context.Context, db.LiveActivity) error
	prefs    func(context.Context, string, string, json.RawMessage) error
}

func (s *registrationErrorStore) SetDevicePrefs(ctx context.Context, id, credential string, prefs json.RawMessage) error {
	return s.prefs(ctx, id, credential, prefs)
}

func (s *registrationErrorStore) UpsertDevice(ctx context.Context, d db.Device) error {
	return s.device(ctx, d)
}

func (s *registrationErrorStore) SaveLiveActivity(ctx context.Context, v db.LiveActivity) error {
	return s.activity(ctx, v)
}

// A reported token also lifts the hold on the next push-start (#7641): not what these tests fail.
func (s *registrationErrorStore) HoldLiveActivityStart(context.Context, string, time.Time, time.Duration) (bool, error) {
	return false, nil
}

func TestDeviceRegistrationSaveFailure(t *testing.T) {
	h := newTestHub(t)
	mux := h.routes()
	failure := errors.New("database write failed")
	var saved Device
	h.push.store = &registrationErrorStore{device: func(ctx context.Context, d Device) error {
		if _, ok := ctx.Deadline(); !ok {
			t.Error("device write has no deadline")
		}
		if failure == nil {
			saved = d
		}
		return failure
	}}
	const body = `{"device_id":"phone","apns_token":"token","relay_url":"https://relay.example.com","relay_grant":"grant","push_key":"key"}`
	for _, lang := range []i18n.Lang{i18n.En, i18n.ZhHans, i18n.Ja} {
		req := httptest.NewRequest("POST", "/api/v1/devices", strings.NewReader(body))
		req.RemoteAddr = "127.0.0.1:12345"
		req.Header.Set("Authorization", "Bearer "+h.cfg.Token)
		req.Header.Set("Accept-Language", string(lang))
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)
		var out map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
			t.Fatal(err)
		}
		if w.Code != 503 || out["code"] != "internal" || out["ok"] != nil || out["error"] != i18n.T(lang, "http-status.unavailable") {
			t.Fatalf("%s: %d %v", lang, w.Code, out)
		}
	}
	failure = nil
	status, out := call(t, mux, "POST", "/api/v1/devices", h.cfg.Token, body)
	push, _ := out["push"].(map[string]any)
	if status != 200 || out["ok"] != true || push["route"] != "relay" {
		t.Fatalf("retry: %d %v", status, out)
	}
	if saved.DeviceID != "phone" || saved.APNSToken != "token" || saved.RelayGrant != "grant" {
		t.Fatalf("saved registration: %+v", saved)
	}
}

func TestLiveActivityRegistrationSaveFailure(t *testing.T) {
	h := newTestHub(t)
	mux := h.routes()
	failure := errors.New("database write failed")
	var saved db.LiveActivity
	h.store = &registrationErrorStore{activity: func(ctx context.Context, v db.LiveActivity) error {
		deadline, ok := ctx.Deadline()
		if !ok || time.Until(deadline) > deviceStoreTimeout {
			t.Error("activity write has no bounded deadline")
		}
		if failure == nil {
			saved = v
		}
		return failure
	}}
	const body = `{"activity_id":"activity","push_token":"token","push_to_start_token":"start-token"}`
	for _, lang := range []i18n.Lang{i18n.En, i18n.ZhHans, i18n.Ja} {
		req := httptest.NewRequest("POST", "/api/v1/devices/phone/live-activity", strings.NewReader(body))
		req.RemoteAddr = "127.0.0.1:12345"
		req.Header.Set("Authorization", "Bearer "+h.cfg.Token)
		req.Header.Set("Accept-Language", string(lang))
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)
		var out map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
			t.Fatal(err)
		}
		if w.Code != 503 || out["code"] != "internal" || out["ok"] != nil || out["error"] != i18n.T(lang, "http-status.unavailable") {
			t.Fatalf("%s: %d %v", lang, w.Code, out)
		}
	}
	failure = nil
	status, out := call(t, mux, "POST", "/api/v1/devices/phone/live-activity", h.cfg.Token, body)
	if status != 200 || out["ok"] != true {
		t.Fatalf("retry: %d %v", status, out)
	}
	if saved.DeviceID != "phone" || saved.ActivityID != "activity" || saved.PushToken != "token" || saved.PushToStartToken != "start-token" {
		t.Fatalf("saved activity: %+v", saved)
	}
}

// A device whose push registration the hub retired (its credential revoked, or inactive) is told to register again:
// 409 push_registration_required in its language, not a 500 or 503 it retries forever.
func TestRetiredRegistrationAsksTheAppToRegisterAgain(t *testing.T) {
	h := newTestHub(t)
	mux := h.routes()
	store := &registrationErrorStore{
		prefs:    func(context.Context, string, string, json.RawMessage) error { return db.ErrPushRegistrationConflict },
		activity: func(context.Context, db.LiveActivity) error { return db.ErrPushRegistrationConflict },
	}
	h.store, h.push.store = store, store
	for _, route := range []struct{ method, path, body string }{
		{"PUT", "/api/v1/devices/phone/prefs", `{"input":true}`},
		{"POST", "/api/v1/devices/phone/live-activity", `{"push_to_start_token":"start-token"}`},
	} {
		for _, lang := range []i18n.Lang{i18n.En, i18n.ZhHans, i18n.Ja} {
			req := httptest.NewRequest(route.method, route.path, strings.NewReader(route.body))
			req.RemoteAddr = "127.0.0.1:12345"
			req.Header.Set("Authorization", "Bearer "+h.cfg.Token)
			req.Header.Set("Accept-Language", string(lang))
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, req)
			var out map[string]any
			if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
				t.Fatal(err)
			}
			if w.Code != http.StatusConflict || out["code"] != pushRegistrationRequired || out["error"] != i18n.T(lang, "push.registration_required") {
				t.Fatalf("%s %s %s: %d %v", route.method, route.path, lang, w.Code, out)
			}
		}
	}
}

func TestLiveActivityRegistrationWriteContext(t *testing.T) {
	for _, mode := range []string{"cancel", "request deadline", "store timeout"} {
		t.Run(mode, func(t *testing.T) {
			h := newTestHub(t)
			h.live.storeTimeout = 20 * time.Millisecond
			mux := http.NewServeMux()
			h.live.routes(mux)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if mode == "request deadline" {
				var stop context.CancelFunc
				ctx, stop = context.WithTimeout(ctx, 10*time.Millisecond)
				defer stop()
			}
			h.store = &registrationErrorStore{activity: func(writeCtx context.Context, _ db.LiveActivity) error {
				if mode == "request deadline" {
					want, _ := ctx.Deadline()
					got, ok := writeCtx.Deadline()
					if !ok || got.After(want) {
						t.Errorf("write deadline %v exceeds request deadline %v", got, want)
					}
				}
				if mode == "cancel" {
					cancel()
				}
				select {
				case <-writeCtx.Done():
					if mode == "cancel" && !errors.Is(writeCtx.Err(), context.Canceled) {
						t.Errorf("cancellation: %v", writeCtx.Err())
					}
					if mode != "cancel" && !errors.Is(writeCtx.Err(), context.DeadlineExceeded) {
						t.Errorf("deadline: %v", writeCtx.Err())
					}
					return writeCtx.Err()
				case <-time.After(time.Second):
					t.Error("write ignored cancellation/timeout")
					return errors.New("unbounded write")
				}
			}}
			req := httptest.NewRequest("POST", "/api/v1/devices/phone/live-activity", strings.NewReader(`{"push_token":"token"}`)).WithContext(ctx)
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, req)
			if w.Code != 503 {
				t.Fatalf("status = %d: %s", w.Code, w.Body.String())
			}
		})
	}
}

type registrationTransport func(*http.Request) (*http.Response, error)

func (f registrationTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestRegistrationBackgroundWritesKeepRunning(t *testing.T) {
	h := newTestHub(t)
	var logs bytes.Buffer
	h.logger = log.New(&logs, "", 0)
	h.push.logf = h.logf
	failure := errors.New("database write failed")
	s := &registrationErrorStore{
		device: func(context.Context, db.Device) error { return failure },
		activity: func(ctx context.Context, _ db.LiveActivity) error {
			if _, ok := ctx.Deadline(); !ok {
				t.Error("background activity write has no deadline")
			}
			return failure
		},
	}
	h.push.store, h.store = s, s
	d := Device{DeviceID: "phone", APNSToken: "token", Topic: "app.example", RelayURL: "https://relay.example.com", RelayGrant: "old"}
	if err := h.push.register(d); !errors.Is(err, failure) {
		t.Fatalf("register error: %v", err)
	}
	h.push.relay = &http.Client{Transport: registrationTransport(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"grant":"renewed"}`))}, nil
	})}
	if _, ok := h.push.device(d.DeviceID); ok {
		t.Fatal("failed enrollment became sendable")
	}
	// Renewal belongs to an already committed registration, not the failed one.
	h.push.mu.Lock()
	h.push.devices[d.DeviceID] = d
	h.push.mu.Unlock()
	renewed, err := h.push.renewGrant(d)
	if !errors.Is(err, failure) || renewed.RelayGrant != "old" {
		t.Fatalf("renewal did not report persistence failure: %+v, %v", renewed, err)
	}
	h.live.update("phone", func(v *db.LiveActivity) { v.PushToStartToken = "new-token" })
	if h.live.rows["phone"].PushToStartToken != "new-token" {
		t.Fatal("background token update stopped")
	}
	select {
	case <-h.live.signal:
	default:
		t.Fatal("background update did not signal")
	}
	if !strings.Contains(logs.String(), "save device: database write failed") || !strings.Contains(logs.String(), "save live activity: database write failed") {
		t.Fatalf("save failures were silent: %s", logs.String())
	}
}
