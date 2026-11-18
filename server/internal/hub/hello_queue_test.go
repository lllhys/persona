package hub

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/nwverse/persona/server/internal/assistantwire"
	"github.com/nwverse/persona/server/internal/db"
	"github.com/nwverse/persona/server/internal/i18n"
	"github.com/nwverse/persona/server/internal/proto"
)

// Deploy cancellation begins closing the app connections; it is not the end of their accepted reports' lifetime.
func TestHelloReportsAfterDeployCancelDrainOnStop(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		deployCtx, cancelDeploy := context.WithCancel(context.Background())
		t.Cleanup(cancelDeploy)
		h := newTestHubWithDeployCtx(t, deployCtx)
		h.assistantOf(assistantwire.AssistantID).updating.Store(true)
		store := newHelloStore()
		store.release()
		h.store = store

		cancelDeploy()
		// Let consumers derived from deployCtx finish exiting. Independent consumers remain ready for the connection
		// that serveHTTP has not closed yet.
		synctest.Wait()
		c := &client{hub: h, device: "dev_late", role: roleFull, out: make(chan []byte, 16)}
		h.onClientMessage(context.Background(), c, []byte(`{"type":"client_hello","platform":"ios","version":"1.0","locale":"en"}`))
		h.onClientMessage(context.Background(), c, []byte(`{"type":"register_phone","device_id":"late","tools":[{"name":"phone_contacts"}]}`))
		waitClientMessage(t, c, "client_hello_ack")
		h.stopHelloWriters()

		for _, key := range []string{clientAppsSetting, deviceLocalesSetting, userLocaleSetting} {
			if got := store.putCount(key); got != 1 {
				t.Errorf("the late report saved %s %d times, want once", key, got)
			}
		}
		var apps map[string]assistantwire.ClientApp
		store.savedSetting(t, clientAppsSetting, &apps)
		if apps["dev_late"].Version != "1.0" {
			t.Errorf("the late client hello was not saved: %+v", apps)
		}
		registrations := store.savedRegistrations()
		if len(registrations) != 1 || registrations[0].DeviceID != "late" {
			t.Errorf("the late phone registration was not saved: %+v", registrations)
		}
	})
}

// A stalled Store and one reporting connection cannot grow any pending set without limit. Earlier honest work,
// queue order, replacement coalescing and producers waiting for detached room all retain their ordinary behavior.
func TestHelloPendingQueuesBoundAndPreserveEarlierReports(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		h := newTestHub(t)
		h.assistantOf(assistantwire.AssistantID).updating.Store(true)
		var logMu sync.Mutex
		var logs bytes.Buffer
		h.logger = log.New(lockedWriter{mu: &logMu, w: &logs}, "", 0)
		store := newHelloStore()
		store.holdLoads = true
		h.store = store
		t.Cleanup(store.release)

		honest := &client{hub: h, device: "dev_honest", role: roleFull, out: make(chan []byte, 32)}
		h.onClientMessage(context.Background(), honest, []byte(`{"type":"client_hello","platform":"ios","version":"1.0","locale":"en"}`))
		h.onClientMessage(context.Background(), honest, []byte(`{"type":"register_phone","device_id":"honest","tools":[{"name":"phone_device_info"}]}`))
		seenLoads := map[string]bool{}
		for len(seenLoads) != 2 {
			select {
			case key := <-store.loadStarted:
				if key == clientAppsSetting || key == deviceLocalesSetting {
					seenLoads[key] = true
				}
			case <-time.After(readLoopLimit):
				t.Fatal("the initial setting loads did not start")
			}
		}
		select {
		case <-store.toolsStarted:
		case <-time.After(readLoopLimit):
			t.Fatal("the honest phone-tool write did not start")
		}

		flood := &client{hub: h, device: "dev_flood", role: roleFull, out: make(chan []byte, 4*(helloPendingLimit+16))}
		floodPhone := func(id string) {
			h.onClientMessage(context.Background(), flood, []byte(fmt.Sprintf(`{"type":"register_phone","device_id":%q,"tools":[{"name":"phone_contacts"}]}`, id)))
		}
		for i := range helloPendingLimit - 1 {
			h.onClientMessage(context.Background(), flood, []byte(`{"type":"client_hello","platform":"ios","version":"2.0","locale":"ja"}`))
			floodPhone(fmt.Sprintf("flood-%d", i))
		}
		floodPhone(fmt.Sprintf("flood-%d", helloPendingLimit-1))

		pendingSizes := func() (int, int, int, int) {
			h.apps.pendingMu.Lock()
			apps := len(h.apps.pending)
			h.apps.pendingMu.Unlock()
			h.langs.pendingMu.Lock()
			langs := len(h.langs.pending)
			h.langs.pendingMu.Unlock()
			h.phoneToolUpdates.mu.Lock()
			tools, order := len(h.phoneToolUpdates.pending), len(h.phoneToolUpdates.order)
			h.phoneToolUpdates.mu.Unlock()
			return apps, langs, tools, order
		}
		if apps, langs, tools, order := pendingSizes(); apps != helloPendingLimit || langs != helloPendingLimit || tools != helloPendingLimit || order != helloPendingLimit {
			t.Fatalf("filled pending sizes = apps %d, locales %d, phones %d/%d, want exactly %d each", apps, langs, tools, order, helloPendingLimit)
		}

		// Replacing an already pending device takes no capacity and moves it behind every other pending device.
		replacementStarted := time.Now()
		h.setPhoneTools(db.PhoneToolRegistration{DeviceID: "flood-0", CredentialID: "dev_flood", Tools: []proto.Tool{{Name: "phone_reminders_list"}}})
		if elapsed := time.Since(replacementStarted); elapsed != 0 {
			t.Fatalf("a pending phone replacement waited for capacity for %s", elapsed)
		}
		h.phoneToolUpdates.mu.Lock()
		last := h.phoneToolUpdates.order[len(h.phoneToolUpdates.order)-1]
		replacement := h.phoneToolUpdates.pending["flood-0"]
		h.phoneToolUpdates.mu.Unlock()
		if last != "flood-0" || len(replacement.Tools) != 1 || replacement.Tools[0].Name != "phone_reminders_list" {
			t.Fatalf("the full-queue replacement did not keep its newest position and tools: last %q, registration %+v", last, replacement)
		}

		// The root goroutine's timed-out admissions advance synctest's fake clock while every Store gate stays held.
		for i := range 3 {
			h.onClientMessage(context.Background(), flood, []byte(`{"type":"client_hello","platform":"ios","version":"2.0","locale":"ja"}`))
			floodPhone(fmt.Sprintf("dropped-%d", i))
		}
		if apps, langs, tools, order := pendingSizes(); apps != helloPendingLimit || langs != helloPendingLimit || tools != helloPendingLimit || order != helloPendingLimit {
			t.Fatalf("overflow pending sizes = apps %d, locales %d, phones %d/%d, want exactly %d each", apps, langs, tools, order, helloPendingLimit)
		}
		logMu.Lock()
		logText := logs.String()
		logMu.Unlock()
		for _, queue := range []string{clientAppsSetting, deviceLocalesSetting, "phone tools"} {
			if got := strings.Count(logText, queue+" pending queue full; dropping reports"); got != 1 {
				t.Errorf("overflow logged %q %d times, want once; log %q", queue, got, logText)
			}
		}

		// This producer is genuinely blocked at the bound. Detaching the held batches returns room and must wake it at
		// the same fake instant, not after its Store-timeout timer fires.
		waiting := &client{hub: h, device: "dev_waiting", role: roleFull, out: make(chan []byte, 16)}
		waitStarted := time.Now()
		waitingDone := make(chan struct{})
		go func() {
			defer close(waitingDone)
			h.onClientMessage(context.Background(), waiting, []byte(`{"type":"client_hello","platform":"ios","version":"3.0","locale":"en"}`))
			h.onClientMessage(context.Background(), waiting, []byte(`{"type":"register_phone","device_id":"waiting","tools":[{"name":"phone_contacts"}]}`))
		}()
		synctest.Wait()
		select {
		case <-waitingDone:
			t.Fatal("a producer finished while every pending set was still full")
		default:
		}
		store.release()
		waitHelloLoop(t, waitingDone, "the producer admitted after batch detachment")
		if elapsed := time.Since(waitStarted); elapsed != 0 {
			t.Fatalf("the producer waited for its timeout instead of detached room: %s", elapsed)
		}
		waitHelloWrites(t, h)

		var apps map[string]assistantwire.ClientApp
		store.savedSetting(t, clientAppsSetting, &apps)
		if len(apps) != 3 || apps["dev_honest"].Version != "1.0" || apps["dev_flood"].Version != "2.0" || apps["dev_waiting"].Version != "3.0" {
			t.Errorf("saved apps lost an earlier or admitted report: %+v", apps)
		}
		var locales map[string]deviceLocale
		store.savedSetting(t, deviceLocalesSetting, &locales)
		if len(locales) != 3 || locales["dev_honest"].Locale != i18n.En || locales["dev_flood"].Locale != i18n.Ja || locales["dev_waiting"].Locale != i18n.En {
			t.Errorf("saved locales lost an earlier or admitted report: %+v", locales)
		}
		if got := store.putCount(clientAppsSetting); got != 2 {
			t.Errorf("client-app writes = %d, want the detached bound batch and the admitted report", got)
		}
		if got := store.putCount(deviceLocalesSetting); got != 2 {
			t.Errorf("device-locale writes = %d, want the detached bound batch and the admitted report", got)
		}
		if got := store.putCount(userLocaleSetting); got != 1 {
			t.Errorf("user-locale writes = %d, want one", got)
		}

		registrations := store.savedRegistrations()
		if len(registrations) != helloPendingLimit+2 {
			t.Fatalf("saved %d registrations, want honest + %d flood + waiting: %+v", len(registrations), helloPendingLimit, registrations)
		}
		if registrations[0].DeviceID != "honest" {
			t.Errorf("the honest registration did not stay first: %+v", registrations[0])
		}
		for i := 1; i < helloPendingLimit; i++ {
			if want := fmt.Sprintf("flood-%d", i); registrations[i].DeviceID != want {
				t.Fatalf("registration %d = %q, want %q", i, registrations[i].DeviceID, want)
			}
		}
		if got := registrations[helloPendingLimit]; got.DeviceID != "flood-0" || len(got.Tools) != 1 || got.Tools[0].Name != "phone_reminders_list" {
			t.Errorf("the replacement did not persist last with its newest tools: %+v", got)
		}
		if got := registrations[helloPendingLimit+1].DeviceID; got != "waiting" {
			t.Errorf("the producer admitted after detachment saved %q, want waiting", got)
		}
		for _, registration := range registrations {
			if strings.HasPrefix(registration.DeviceID, "dropped-") {
				t.Errorf("a dropped registration was written: %+v", registration)
			}
		}
		if apps, langs, tools, order := pendingSizes(); apps != 0 || langs != 0 || tools != 0 || order != 0 {
			t.Errorf("after the drain pending sizes = apps %d, locales %d, phones %d/%d, want zero", apps, langs, tools, order)
		}
		if len(h.helloWrites.appsSlot) != 0 || len(h.helloWrites.langsSlot) != 0 || len(h.helloWrites.toolsSlot) != 0 {
			t.Errorf("semaphore tokens remained after the drain: apps %d, locales %d, phones %d", len(h.helloWrites.appsSlot), len(h.helloWrites.langsSlot), len(h.helloWrites.toolsSlot))
		}
		logMu.Lock()
		defer logMu.Unlock()
		if got := strings.Count(logs.String(), "pending queue full; dropping reports"); got != 3 {
			t.Errorf("the whole flood emitted %d overflow lines, want one per queue; log %q", got, logs.String())
		}
	})
}

// An unreadable existing user locale is not an absent one. Neither device history nor user language may be saved.
func TestHelloUserLocaleLoadFailureAbortsAllLocaleWrites(t *testing.T) {
	h := newTestHub(t)
	h.assistantOf(assistantwire.AssistantID).updating.Store(true)
	store := newHelloStore()
	store.release()
	store.settingsStore.vals[userLocaleSetting] = []byte(`"ja"`)
	store.failUserLoad.Store(true)
	h.store = store
	helloFrom(h, "dev_mac", `{"locale":"en"}`)
	for _, key := range []string{deviceLocalesSetting, userLocaleSetting} {
		if got := store.putAttempts(key); got != 0 {
			t.Errorf("an unreadable user locale caused %d writes to %s", got, key)
		}
	}
	store.settingsStore.mu.Lock()
	stored := string(store.settingsStore.vals[userLocaleSetting])
	store.settingsStore.mu.Unlock()
	if stored != `"ja"` || h.userLang() != i18n.Default {
		t.Fatalf("an unreadable user locale was treated as absent: stored %s, published %s", stored, h.userLang())
	}
}

type helloKeyGateStore struct {
	*helloStore
	key         string
	started     chan struct{}
	released    chan struct{}
	startOnce   sync.Once
	releaseOnce sync.Once
}

func (s *helloKeyGateStore) Setting(ctx context.Context, key string, out any) (bool, error) {
	if key == s.key {
		s.startOnce.Do(func() { close(s.started) })
		<-s.released
	}
	return s.helloStore.Setting(ctx, key, out)
}

func (s *helloKeyGateStore) release() { s.releaseOnce.Do(func() { close(s.released) }) }

// The batch detached behind a keyless report can contain a keyed one that the initial preparation did not cover.
func TestHelloKeyedLocaleBehindKeylessBatchLoadsDeviceHistory(t *testing.T) {
	h := newTestHub(t)
	h.assistantOf(assistantwire.AssistantID).updating.Store(true)
	base := newHelloStore()
	base.release()
	store := &helloKeyGateStore{helloStore: base, key: userLocaleSetting, started: make(chan struct{}), released: make(chan struct{})}
	t.Cleanup(store.release)
	h.store = store
	h.noteUserLang("", i18n.En)
	select {
	case <-store.started:
	case <-time.After(readLoopLimit):
		t.Fatal("the user-locale load did not start")
	}
	h.noteUserLang("dev_a", i18n.Ja)
	store.release()
	waitHelloWrites(t, h)
	var locales map[string]deviceLocale
	base.savedSetting(t, deviceLocalesSetting, &locales)
	if len(locales) != 1 || locales["dev_a"].Locale != i18n.Ja || h.userLang() != i18n.En {
		t.Fatalf("the keyed report behind the keyless batch was not initialized and applied: locales %+v, user %s", locales, h.userLang())
	}
}

// Persistence can happen much later than reception; every device locale carries the synchronous hello's time.
func TestHelloDeviceLocaleUsesReceiptTime(t *testing.T) {
	h := newTestHub(t)
	h.assistantOf(assistantwire.AssistantID).updating.Store(true)
	store := newHelloStore()
	store.holdLoads = true
	h.store = store
	t.Cleanup(store.release)
	c := &client{hub: h, device: "dev_receipt", role: roleFull, out: make(chan []byte, 16)}
	before := time.Now().UTC().Truncate(time.Second)
	h.onClientHello(c, []byte(`{"platform":"ios","version":"1.0","locale":"en"}`))
	h.onClientHello(c, []byte(`{"platform":"ios","version":"1.0","locale":"ja"}`))
	after := time.Now().UTC().Truncate(time.Second)
	store.release()
	waitHelloWrites(t, h)
	var locales map[string]deviceLocale
	store.savedSetting(t, deviceLocalesSetting, &locales)
	receipt := locales["dev_receipt"].At
	if locales["dev_receipt"].Locale != i18n.Ja || receipt.Before(before) || receipt.After(after) {
		t.Fatalf("saved locale %+v has receipt time outside [%s, %s]", locales["dev_receipt"], before, after)
	}
	if got := store.putCount(deviceLocalesSetting); got != 1 {
		t.Fatalf("the held burst saved device locales %d times, want one", got)
	}
}

// A failed write must not mark the in-memory SeenAt values as persisted; an identical report retries the snapshot.
func TestClientAppFailedSaveRetriesIdenticalSnapshot(t *testing.T) {
	h := newTestHub(t)
	store := newHelloStore()
	store.release()
	existing := map[string]assistantwire.ClientApp{
		"dev_existing": {Platform: "macos", Version: "4.0", SeenAt: time.Now().UTC().Truncate(time.Second)},
	}
	if err := store.settingsStore.PutSetting(context.Background(), clientAppsSetting, existing); err != nil {
		t.Fatal(err)
	}
	store.failAppsPut.Store(true)
	h.store = store
	c := &client{hub: h, device: "dev_phone", role: roleAnswer, out: make(chan []byte, 16)}
	hello := []byte(`{"platform":"ios","version":"1.0"}`)
	h.onClientHello(c, hello)
	waitHelloWrites(t, h)
	if got := store.putAttempts(clientAppsSetting); got != 1 {
		t.Fatalf("the first save made %d attempts, want one", got)
	}
	if got := store.putCount(clientAppsSetting); got != 0 {
		t.Fatalf("the failed save was counted as successful %d times", got)
	}
	h.onClientHello(c, hello)
	waitHelloWrites(t, h)
	if attempts, saved := store.putAttempts(clientAppsSetting), store.putCount(clientAppsSetting); attempts != 2 || saved != 1 {
		t.Fatalf("after the identical retry attempts=%d successful=%d, want 2 and 1", attempts, saved)
	}
	var apps map[string]assistantwire.ClientApp
	store.savedSetting(t, clientAppsSetting, &apps)
	if len(apps) != 2 || apps["dev_existing"].Version != "4.0" || apps["dev_phone"].Version != "1.0" {
		t.Fatalf("the retried snapshot is incomplete: %+v", apps)
	}
}

// A hub and its workers created outside a bubble can accept and finish a report synchronously waited inside one.
func TestHelloOutsideHubCanBeWaitedInsideSynctest(t *testing.T) {
	h := newTestHub(t)
	h.assistantOf(assistantwire.AssistantID).updating.Store(true)
	store := newHelloStore()
	store.release()
	h.store = store
	synctest.Test(t, func(t *testing.T) {
		c := &client{hub: h, device: "dev_bubble", role: roleFull, out: make(chan []byte, 16)}
		h.onClientMessage(context.Background(), c, []byte(`{"type":"client_hello","platform":"ios","version":"1.0","locale":"en"}`))
		h.onClientMessage(context.Background(), c, []byte(`{"type":"register_phone","device_id":"bubble","tools":[{"name":"phone_contacts"}]}`))
		h.waitHelloWrites()
		// The outside consumer uses the real clock for retention while this hello has synctest's receipt time, so this
		// case asserts completion rather than applying that cross-clock fixture's client-app retention result.
		if attempts, saved := store.putAttempts(clientAppsSetting), store.putCount(clientAppsSetting); attempts != 1 || saved != 1 {
			t.Fatalf("the outside hub did not finish the bubble's client-app write: attempts=%d saved=%d", attempts, saved)
		}
		var locales map[string]deviceLocale
		store.savedSetting(t, deviceLocalesSetting, &locales)
		if locales["dev_bubble"].Locale != i18n.En {
			t.Fatalf("the outside hub did not save the bubble's locale: %+v", locales)
		}
		registrations := store.savedRegistrations()
		if len(registrations) != 1 || registrations[0].DeviceID != "bubble" {
			t.Fatalf("the outside hub did not save the bubble's registration: %+v", registrations)
		}
	})
}
