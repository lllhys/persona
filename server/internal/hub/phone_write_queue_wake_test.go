package hub

import (
	"context"
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/nwverse/persona/server/internal/db"
	"github.com/nwverse/persona/server/internal/proto"
)

func TestPhoneWriteQueueReturnsWhileWakePushWaits(t *testing.T) {
	type receivedPush struct {
		payload  map[string]any
		pushType string
		priority string
	}
	received := make(chan receivedPush, 1)
	release := make(chan struct{})
	unblock := sync.OnceFunc(func() { close(release) })
	h := directHub(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || !strings.HasPrefix(r.URL.Path, "/3/device/") {
			http.NotFound(w, r)
			return
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		received <- receivedPush{payload: body, pushType: r.Header.Get("apns-push-type"), priority: r.Header.Get("apns-priority")}
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}))
	t.Cleanup(unblock) // Release the held request before directHub closes its server.
	h.store = setupTestDB(t)
	h.setPhoneTools(db.PhoneToolRegistration{DeviceID: "phone", Platform: "ios", Tools: []proto.Tool{{Name: "phone_reminders_create"}}})
	result := make(chan proto.ToolResult, 1)
	go func() {
		result <- h.callPhone(context.Background(), "phone_reminders_create", json.RawMessage(`{"title":"queued with a wake","queue_if_offline":true}`))
	}()
	select {
	case push := <-received:
		aps, _ := push.payload["aps"].(map[string]any)
		if push.pushType != "background" || push.priority != "5" || aps["content-available"] != float64(1) || push.payload["persona_wake"] != "tool" {
			t.Fatalf("wake push = %+v", push)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("queued write did not request a silent wake")
	}
	// APNs is still blocked here. The receipt must not wait for push acceptance or a phone connection.
	select {
	case got := <-result:
		var row db.PhoneWrite
		if got.IsError || json.Unmarshal([]byte(textOf(got)), &row) != nil || row.ID == "" || row.State != "queued" {
			t.Fatalf("enqueue while push waits = %+v", got)
		}
		phoneWriteState(t, h, row.ID, "queued")
	case <-time.After(time.Second):
		t.Fatal("enqueue waited for the wake push")
	}
	if !lockWithin(t, h, time.Second) {
		t.Fatal("wake push held the hub's global lock")
	}
	unblock()
}

// Only admission is faked here so synctest can settle every wake goroutine and advance the throttle clock.
type phoneWakeAdmissionStore struct {
	Store
	err error
}

func (s *phoneWakeAdmissionStore) EnqueuePhoneWrite(_ context.Context, id, tool string, args json.RawMessage) (*db.PhoneWrite, error) {
	if s.err != nil {
		return nil, s.err
	}
	return &db.PhoneWrite{ID: id, Tool: tool, Args: args, State: "queued"}, nil
}

func phoneWakeTestHub(t *testing.T, configured bool) (*Hub, *fleetPushSink, *phoneWakeAdmissionStore) {
	t.Helper()
	h := newTestHub(t)
	store := &phoneWakeAdmissionStore{Store: newMeshStore()}
	h.store = store
	sink := &fleetPushSink{}
	h.push.relay = &http.Client{Transport: sink}
	if configured {
		key, err := ecdh.P256().GenerateKey(rand.Reader)
		if err != nil {
			t.Fatal(err)
		}
		if err := h.push.register(Device{DeviceID: "phone", Name: "phone", Kind: "iphone", APNSToken: "phone-device",
			Topic: "app.example", PushKey: base64.StdEncoding.EncodeToString(key.PublicKey().Bytes()),
			RelayURL: "https://relay.example", RelayGrant: "grant-phone"}); err != nil {
			t.Fatal(err)
		}
	}
	h.setPhoneTools(db.PhoneToolRegistration{DeviceID: "phone", Platform: "ios", Tools: []proto.Tool{{Name: "phone_reminders_create"}}})
	return h, sink, store
}

func TestPhoneWriteQueueWakeUsesThirtySecondThrottle(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		h, pushes, _ := phoneWakeTestHub(t, true)
		enqueuePhoneWriteTest(t, h, "first")
		synctest.Wait()
		if got := pushes.count("phone", "background", ""); got != 1 {
			t.Fatalf("first queued write sent %d wake pushes", got)
		}
		enqueuePhoneWriteTest(t, h, "second")
		enqueuePhoneWriteTest(t, h, "third")
		synctest.Wait()
		if got := pushes.count("phone", "background", ""); got != 1 {
			t.Fatalf("queued writes bypassed the wake throttle: %d pushes", got)
		}
		<-time.After(30 * time.Second)
		enqueuePhoneWriteTest(t, h, "after throttle")
		synctest.Wait()
		if got := pushes.count("phone", "background", ""); got != 2 {
			t.Fatalf("wake throttle did not expire: %d pushes", got)
		}
	})
}

func TestPhoneWriteQueueWakeSkipsUnconfiguredPushAndFailedAdmission(t *testing.T) {
	for _, configured := range []bool{false, true} {
		t.Run(map[bool]string{false: "unconfigured push", true: "failed admission"}[configured], func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				h, pushes, store := phoneWakeTestHub(t, configured)
				if configured {
					store.err = errors.New("queue full")
					got := h.callPhone(context.Background(), "phone_reminders_create", json.RawMessage(`{"title":"refused","queue_if_offline":true}`))
					if !got.IsError {
						t.Fatal("failed queue admission returned success")
					}
				} else {
					enqueuePhoneWriteTest(t, h, "without pushes")
				}
				synctest.Wait()
				if got := pushes.count("phone", "background", ""); got != 0 || len(h.wokeAt) != 0 {
					t.Fatalf("unnecessary wake: pushes=%d, attempts=%v", got, h.wokeAt)
				}
			})
		})
	}
}
