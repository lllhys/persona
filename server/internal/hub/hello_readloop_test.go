package hub

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/nwverse/persona/server/internal/assistantwire"
	"github.com/nwverse/persona/server/internal/db"
	"github.com/nwverse/persona/server/internal/i18n"
	"github.com/nwverse/persona/server/internal/proto"
)

// helloStore can hold the loads and writes behind client_hello and register_phone. Its gates stay closed until the
// test releases them, even when a store operation's context expires: the operation deadline must never race the
// response assertion. Test cleanup releases any handler still blocked after a failure.
type helloStore struct {
	*settingsStore
	loadStarted    chan string
	putStarted     chan string
	toolsStarted   chan db.PhoneToolRegistration
	loadRelease    chan struct{}
	putRelease     chan struct{}
	toolsRelease   chan struct{}
	releaseOnce    sync.Once
	holdLoads      bool
	failAppsLoad   atomic.Bool
	failDeviceLoad atomic.Bool
	failUserLoad   atomic.Bool
	failAppsPut    atomic.Bool

	stateMu       sync.Mutex
	toolCalls     int
	registrations []db.PhoneToolRegistration
	putsByKey     map[string]int
	attemptsByKey map[string]int
}

func newHelloStore() *helloStore {
	return &helloStore{
		settingsStore: &settingsStore{vals: map[string][]byte{}},
		loadStarted:   make(chan string, 32),
		putStarted:    make(chan string, 32),
		toolsStarted:  make(chan db.PhoneToolRegistration, 32),
		loadRelease:   make(chan struct{}),
		putRelease:    make(chan struct{}),
		toolsRelease:  make(chan struct{}),
		putsByKey:     map[string]int{},
		attemptsByKey: map[string]int{},
	}
}

func (s *helloStore) release() {
	s.releaseOnce.Do(func() {
		close(s.loadRelease)
		close(s.putRelease)
		close(s.toolsRelease)
	})
}

func (s *helloStore) Setting(ctx context.Context, key string, out any) (bool, error) {
	if s.holdLoads && (key == clientAppsSetting || key == deviceLocalesSetting) {
		select {
		case s.loadStarted <- key:
		default:
		}
		// Do not honor the operation deadline here: it must not expire at the same instant as the response assertion.
		<-s.loadRelease
	}
	if key == clientAppsSetting && s.failAppsLoad.CompareAndSwap(true, false) {
		return false, errors.New("client apps load failed")
	}
	if key == deviceLocalesSetting && s.failDeviceLoad.CompareAndSwap(true, false) {
		return false, errors.New("device locales load failed")
	}
	if key == userLocaleSetting && s.failUserLoad.Load() {
		return false, errors.New("user locale load failed")
	}
	return s.settingsStore.Setting(ctx, key, out)
}

func (s *helloStore) PutSetting(ctx context.Context, key string, value any) error {
	s.stateMu.Lock()
	s.attemptsByKey[key]++
	s.stateMu.Unlock()
	select {
	case s.putStarted <- key:
	default:
	}
	<-s.putRelease
	if key == clientAppsSetting && s.failAppsPut.CompareAndSwap(true, false) {
		return errors.New("client apps save failed")
	}
	if err := s.settingsStore.PutSetting(ctx, key, value); err != nil {
		return err
	}
	s.stateMu.Lock()
	s.putsByKey[key]++
	s.stateMu.Unlock()
	return nil
}

func (s *helloStore) ReplacePhoneTools(ctx context.Context, registration db.PhoneToolRegistration) error {
	s.stateMu.Lock()
	s.toolCalls++
	call := s.toolCalls
	s.stateMu.Unlock()
	select {
	case s.toolsStarted <- registration:
	default:
	}
	// Only the older write waits. If a broken implementation runs the newer write concurrently, it can finish and
	// the older one then gets its chance to overwrite it: the final-state assertions catch that ordering bug.
	if call == 1 {
		<-s.toolsRelease
	}
	s.stateMu.Lock()
	s.registrations = append(s.registrations, registration)
	s.stateMu.Unlock()
	return nil
}

func (s *helloStore) savedRegistrations() []db.PhoneToolRegistration {
	s.stateMu.Lock()
	defer s.stateMu.Unlock()
	return append([]db.PhoneToolRegistration(nil), s.registrations...)
}

func (s *helloStore) putCount(key string) int {
	s.stateMu.Lock()
	defer s.stateMu.Unlock()
	return s.putsByKey[key]
}

func (s *helloStore) putAttempts(key string) int {
	s.stateMu.Lock()
	defer s.stateMu.Unlock()
	return s.attemptsByKey[key]
}

func (s *helloStore) savedSetting(t *testing.T, key string, out any) {
	t.Helper()
	s.settingsStore.mu.Lock()
	raw := append([]byte(nil), s.settingsStore.vals[key]...)
	s.settingsStore.mu.Unlock()
	if err := json.Unmarshal(raw, out); err != nil {
		t.Fatalf("decode saved %s %q: %v", key, raw, err)
	}
}

func waitHelloWrites(t *testing.T, h *Hub) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		h.waitHelloWrites()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("hello and phone-registration consumers did not become idle")
	}
}

func waitClientMessage(t *testing.T, c *client, messageType string) map[string]any {
	t.Helper()
	timer := time.NewTimer(readLoopLimit)
	defer timer.Stop()
	for {
		select {
		case raw := <-c.out:
			var msg map[string]any
			if json.Unmarshal(raw, &msg) == nil && msg["type"] == messageType {
				return msg
			}
		case <-timer.C:
			t.Fatalf("%s was not answered within %s", messageType, readLoopLimit)
			return nil
		}
	}
}

func waitHelloLoop(t *testing.T, done <-chan struct{}, what string) {
	t.Helper()
	select {
	case <-done:
	case <-time.After(readLoopLimit):
		t.Fatalf("%s remained blocked", what)
	}
}

// The connection reads these messages in order. Neither its ping nor another app's hello may wait for either store
// write: nothing in this sequence has a reply whose contents depend on the writes finishing (#6889).
func TestHelloAndRegisterPhoneLeaveReadLoopWhileStoreWritesAreHeld(t *testing.T) {
	h := newTestHub(t)
	store := newHelloStore()
	h.store = store
	t.Cleanup(store.release)

	// Keep ReplacePhoneTools occupied too, so the ping behind the registration would wait for both writes in the old
	// code rather than merely slipping past a write that had not started yet.
	seed := &client{hub: h, device: "dev_seed", role: roleFull, out: make(chan []byte, 16)}
	seedDone := make(chan struct{})
	go func() {
		defer close(seedDone)
		h.onClientMessage(context.Background(), seed, []byte(`{"type":"register_phone","device_id":"seed","tools":[{"name":"phone_device_info"}]}`))
	}()
	select {
	case <-store.toolsStarted:
	case <-time.After(readLoopLimit):
		t.Fatal("the first phone-tool write did not start")
	}

	phone := &client{hub: h, device: "dev_phone", role: roleFull, out: make(chan []byte, 16)}
	phoneDone := make(chan struct{})
	go func() {
		defer close(phoneDone)
		for _, raw := range []string{
			`{"type":"client_hello","platform":"ios","version":"1.0","locale":"en"}`,
			`{"type":"register_phone","device_id":"phone","tools":[{"name":"phone_contacts"}]}`,
			`{"type":"ping","id":"behind-writes"}`,
		} {
			h.onClientMessage(context.Background(), phone, []byte(raw))
		}
	}()
	select {
	case <-store.putStarted:
	case <-time.After(readLoopLimit):
		t.Fatal("the client-apps write did not start")
	}

	waitClientMessage(t, phone, "pong")
	other := &client{hub: h, device: "dev_other", role: roleFull, out: make(chan []byte, 16)}
	otherDone := make(chan struct{})
	go func() {
		defer close(otherDone)
		h.onClientMessage(context.Background(), other, []byte(`{"type":"client_hello","platform":"android","version":"2.0","locale":"ja"}`))
	}()
	waitClientMessage(t, other, "client_hello_ack")

	store.release()
	for _, done := range []chan struct{}{seedDone, phoneDone, otherDone} {
		waitHelloLoop(t, done, "a connection")
	}
	waitHelloWrites(t, h)
}

// A load held at the start gives the burst a definite batch boundary. Every report is applied in order, including the
// simulator registration between hellos, but each setting is saved once and the phone registry is saved once.
func TestHelloBurstCoalescesAndPersistsAfterRelease(t *testing.T) {
	h := newTestHub(t)
	store := newHelloStore()
	store.holdLoads = true
	h.store = store
	t.Cleanup(store.release)

	phone := &client{hub: h, device: "dev_phone", role: roleFull, out: make(chan []byte, 16)}
	mac := &client{hub: h, device: "dev_mac", role: roleFull, out: make(chan []byte, 16)}
	hellos := []struct {
		c     *client
		hello string
	}{
		{phone, `{"type":"client_hello","platform":"ios","version":"1.0","locale":"en"}`},
		{mac, `{"type":"client_hello","platform":"macos","version":"2.0","locale":"ja"}`},
		{phone, `{"type":"client_hello","platform":"ios","version":"1.1","locale":"en"}`},
		{mac, `{"type":"client_hello","platform":"macos","version":"2.1","locale":"ja"}`},
		{phone, `{"type":"client_hello","platform":"ios","version":"1.2","locale":"en"}`},
		{mac, `{"type":"client_hello","platform":"macos","version":"2.2","locale":"ja"}`},
	}
	helloDone := make(chan struct{})
	go func() {
		defer close(helloDone)
		for _, report := range hellos {
			h.onClientMessage(context.Background(), report.c, []byte(report.hello))
		}
	}()
	for range 3 {
		waitClientMessage(t, phone, "client_hello_ack")
		waitClientMessage(t, mac, "client_hello_ack")
	}
	h.onClientMessage(context.Background(), phone, []byte(`{"type":"register_phone","device_id":"phone","simulator":true,"tools":[{"name":"phone_contacts"}]}`))

	store.release()
	waitHelloLoop(t, helloDone, "the hello burst")
	waitHelloWrites(t, h)

	for _, key := range []string{clientAppsSetting, deviceLocalesSetting, userLocaleSetting} {
		if got := store.putCount(key); got != 1 {
			t.Fatalf("%s was written %d times for one held burst, want one coalesced snapshot", key, got)
		}
	}
	var apps map[string]assistantwire.ClientApp
	store.savedSetting(t, clientAppsSetting, &apps)
	if got := apps["dev_phone"]; got.Version != "1.2" || !got.Simulator {
		t.Fatalf("the final phone app lost its newer version or simulator marker: %+v", got)
	}
	if got := apps["dev_mac"]; got.Version != "2.2" {
		t.Fatalf("the final Mac app version is %+v", got)
	}
	var locales map[string]deviceLocale
	store.savedSetting(t, deviceLocalesSetting, &locales)
	if locales["dev_phone"].Locale != i18n.En || locales["dev_mac"].Locale != i18n.Ja {
		t.Fatalf("saved device locales: %+v", locales)
	}
	var savedUser string
	store.savedSetting(t, userLocaleSetting, &savedUser)
	if savedUser != string(i18n.En) || h.userLang() != i18n.En {
		t.Fatalf("a second device's first report replaced the first device's language: %q / %s", savedUser, h.userLang())
	}
	registrations := store.savedRegistrations()
	if len(registrations) != 1 || len(registrations[0].Tools) != 1 || registrations[0].Tools[0].Name != "phone_contacts" {
		t.Fatalf("saved phone registrations: %+v", registrations)
	}
	h.mu.Lock()
	registered := h.phoneTools["phone"]
	h.mu.Unlock()
	if len(registered.Tools) != 1 || registered.Tools[0].Name != "phone_contacts" || !hasTool(h.phoneToolList(), "phone_contacts") {
		t.Fatalf("registered phone tools: %+v", registered.Tools)
	}
}

// A failed cold load is not an empty setting. Its report is skipped without a save; the next report retries the load
// and preserves both the pre-existing device and the new one.
func TestClientAppsLoadFailureSkipsSaveAndRetries(t *testing.T) {
	h := newTestHub(t)
	store := newHelloStore()
	store.release()
	existing := map[string]assistantwire.ClientApp{"dev_keep": {Platform: "ios", Version: "0.9", SeenAt: time.Now().UTC()}}
	raw, _ := json.Marshal(existing)
	store.vals[clientAppsSetting] = raw
	store.failAppsLoad.Store(true)
	h.store = store

	first := &client{hub: h, device: "dev_dropped", role: roleAnswer, out: make(chan []byte, 8)}
	h.onClientHello(first, []byte(`{"platform":"ios","version":"1.0"}`))
	waitHelloWrites(t, h)
	if got := store.putCount(clientAppsSetting); got != 0 {
		t.Fatalf("a failed load caused %d client-apps saves", got)
	}
	var afterFailure map[string]assistantwire.ClientApp
	store.savedSetting(t, clientAppsSetting, &afterFailure)
	if len(afterFailure) != 1 || afterFailure["dev_keep"].Version != "0.9" {
		t.Fatalf("the failed load changed the saved map: %+v", afterFailure)
	}

	next := &client{hub: h, device: "dev_new", role: roleAnswer, out: make(chan []byte, 8)}
	h.onClientHello(next, []byte(`{"platform":"ios","version":"2.0"}`))
	waitHelloWrites(t, h)
	if got := store.putCount(clientAppsSetting); got != 1 {
		t.Fatalf("the successful retry made %d saves, want one", got)
	}
	var saved map[string]assistantwire.ClientApp
	store.savedSetting(t, clientAppsSetting, &saved)
	if len(saved) != 2 || saved["dev_keep"].Version != "0.9" || saved["dev_new"].Version != "2.0" {
		t.Fatalf("the retry did not preserve old and new devices: %+v", saved)
	}
	if _, droppedReportRetried := saved["dev_dropped"]; droppedReportRetried {
		t.Fatalf("the report whose load failed was not skipped: %+v", saved)
	}
}

// A missing user_locale is not permission to save one when the device history needed to decide it could not be read.
// The next hello retries, keeps the stored device history, and only then makes and saves the first-language decision.
func TestDeviceLocaleLoadFailureSkipsUserSave(t *testing.T) {
	h := newTestHub(t)
	store := newHelloStore()
	store.release()
	existing := map[string]deviceLocale{"dev_keep": {Locale: i18n.Ja, At: time.Now().UTC()}}
	raw, _ := json.Marshal(existing)
	store.vals[deviceLocalesSetting] = raw
	store.failDeviceLoad.Store(true)
	h.store = store

	first := &client{hub: h, device: "dev_dropped", role: roleFull, out: make(chan []byte, 8)}
	h.onClientHello(first, []byte(`{"locale":"ja"}`))
	waitHelloWrites(t, h)
	if got := store.putCount(deviceLocalesSetting) + store.putCount(userLocaleSetting); got != 0 {
		t.Fatalf("a failed device-locale load caused %d locale saves", got)
	}
	if h.userLang() != i18n.ZhHans {
		t.Fatalf("the failed batch published a user language: %s", h.userLang())
	}

	next := &client{hub: h, device: "dev_new", role: roleFull, out: make(chan []byte, 8)}
	h.onClientHello(next, []byte(`{"locale":"en"}`))
	waitHelloWrites(t, h)
	if got := store.putCount(deviceLocalesSetting); got != 1 {
		t.Fatalf("the device-locale retry made %d saves, want one", got)
	}
	if got := store.putCount(userLocaleSetting); got != 1 {
		t.Fatalf("the user-locale retry made %d saves, want one", got)
	}
	var saved map[string]deviceLocale
	store.savedSetting(t, deviceLocalesSetting, &saved)
	if len(saved) != 2 || saved["dev_keep"].Locale != i18n.Ja || saved["dev_new"].Locale != i18n.En {
		t.Fatalf("the retry did not preserve old and new device locales: %+v", saved)
	}
	if _, retried := saved["dev_dropped"]; retried {
		t.Fatalf("the report whose load failed was not skipped: %+v", saved)
	}
	if h.userLang() != i18n.En {
		t.Fatalf("the successful first report did not set the user language: %s", h.userLang())
	}
}

// Latest-per-device is not a valid locale coalescing rule. A changes to zh-Hans, B then changes to zh-Hans, and A
// changes back to en. Applying all three in order finishes at en; dropping A's intermediate change would finish at zh.
func TestLocaleBurstPreservesCrossDeviceOrder(t *testing.T) {
	h := newTestHub(t)
	store := newHelloStore()
	store.holdLoads = true
	h.store = store
	t.Cleanup(store.release)
	at := time.Now().UTC()
	storedLocales := map[string]deviceLocale{"dev_a": {Locale: i18n.En, At: at}, "dev_b": {Locale: i18n.Ja, At: at}}
	raw, _ := json.Marshal(storedLocales)
	store.vals[deviceLocalesSetting] = raw
	store.vals[userLocaleSetting] = []byte(`"en"`)
	h.loadUserLang(context.Background())

	a := &client{hub: h, device: "dev_a", role: roleFull, out: make(chan []byte, 8)}
	b := &client{hub: h, device: "dev_b", role: roleFull, out: make(chan []byte, 8)}
	done := make(chan struct{})
	go func() {
		defer close(done)
		for _, report := range []struct {
			c      *client
			locale string
		}{{a, "zh-Hans"}, {b, "zh-Hans"}, {a, "en"}} {
			h.onClientHello(report.c, []byte(`{"locale":"`+report.locale+`"}`))
		}
	}()
	waitClientMessage(t, a, "client_hello_ack")
	waitClientMessage(t, b, "client_hello_ack")
	waitClientMessage(t, a, "client_hello_ack")
	store.release()
	waitHelloLoop(t, done, "the cross-device locale burst")
	waitHelloWrites(t, h)
	if h.userLang() != i18n.En {
		t.Fatalf("the coalesced locale batch reordered or dropped intermediate changes: %s", h.userLang())
	}
	for _, key := range []string{deviceLocalesSetting, userLocaleSetting} {
		if got := store.putCount(key); got != 1 {
			t.Fatalf("%s was written %d times, want one final snapshot", key, got)
		}
	}
	var saved map[string]deviceLocale
	store.savedSetting(t, deviceLocalesSetting, &saved)
	if saved["dev_a"].Locale != i18n.En || saved["dev_b"].Locale != i18n.ZhHans {
		t.Fatalf("the final device locales are wrong: %+v", saved)
	}
	var savedUser string
	store.savedSetting(t, userLocaleSetting, &savedUser)
	if savedUser != string(i18n.En) {
		t.Fatalf("the persisted user locale is %q, want en", savedUser)
	}
}

// Delayed persistence must keep the receipt time, not stamp every report with the consumer's time and change which
// device history is pruned. The older changed device is the one that falls out of this full snapshot.
func TestLocaleBatchUsesReceiptTimesForPruning(t *testing.T) {
	h := newTestHub(t)
	store := newHelloStore()
	store.release()
	h.store = store
	base := time.Date(2026, time.January, 2, 3, 4, 5, 0, time.UTC)
	stored := make(map[string]deviceLocale, deviceLocalesMax)
	stored["dev_a"] = deviceLocale{Locale: i18n.En, At: base}
	for i := 1; i < deviceLocalesMax; i++ {
		stored[fmt.Sprintf("dev_%02d", i)] = deviceLocale{Locale: i18n.En, At: base.Add(time.Duration(i+1) * time.Second)}
	}
	raw, _ := json.Marshal(stored)
	store.vals[deviceLocalesSetting] = raw
	store.vals[userLocaleSetting] = []byte(`"en"`)
	h.writeUserLangs([]userLangUpdate{
		{key: "dev_a", lang: i18n.Ja, at: base.Add(time.Second)},
		{key: "dev_b", lang: i18n.Ja, at: base.Add(100 * time.Second)},
	})
	var saved map[string]deviceLocale
	store.savedSetting(t, deviceLocalesSetting, &saved)
	if len(saved) != deviceLocalesMax {
		t.Fatalf("saved %d device locales, want %d", len(saved), deviceLocalesMax)
	}
	if _, kept := saved["dev_a"]; kept {
		t.Fatal("consumer-side timestamps kept the oldest report instead of pruning it")
	}
	if saved["dev_b"].At != base.Add(100*time.Second) || saved["dev_01"].Locale != i18n.En {
		t.Fatalf("the batch did not preserve receipt times and newer history: %+v", saved)
	}
}

// Two replacements arrive behind an older in-flight write. Only the newest pending set is written afterwards, and
// neither the store nor the committed registry can let the older set finish last.
func TestRegisterPhoneNewestSnapshotWins(t *testing.T) {
	h := newTestHub(t)
	store := newHelloStore()
	h.store = store
	t.Cleanup(store.release)
	phone := &client{hub: h, device: "dev_phone", role: roleFull, out: make(chan []byte, 16)}
	addTestClient(h, phone)
	oldDone := make(chan struct{})
	go func() {
		defer close(oldDone)
		h.onClientMessage(context.Background(), phone, []byte(`{"type":"register_phone","device_id":"phone","tools":[{"name":"phone_device_info"}]}`))
	}()
	select {
	case first := <-store.toolsStarted:
		if len(first.Tools) != 1 || first.Tools[0].Name != "phone_device_info" {
			t.Fatalf("the held write is not the older registration: %+v", first)
		}
	case <-time.After(readLoopLimit):
		t.Fatal("the older phone-tool write did not start")
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		for _, raw := range []string{
			`{"type":"register_phone","device_id":"phone","tools":[{"name":"phone_reminders_list"}]}`,
			`{"type":"register_phone","device_id":"phone","tools":[{"name":"phone_contacts"}]}`,
			`{"type":"ping","id":"after-newest-registration"}`,
		} {
			h.onClientMessage(context.Background(), phone, []byte(raw))
		}
	}()
	// The pong proves all three registrations were read while the oldest store call is still held. Online routing has
	// already changed too; only the durable registry is allowed to lag.
	waitClientMessage(t, phone, "pong")
	h.mu.Lock()
	onlineNewest := phone.phoneToolNames["phone_contacts"] && !phone.phoneToolNames["phone_device_info"]
	h.mu.Unlock()
	if !onlineNewest {
		t.Fatal("the connected phone did not expose its newest tools before persistence")
	}
	if got := h.phoneClient("phone_contacts"); got != phone {
		t.Fatalf("the newest tool did not route to its connected phone before persistence: %p", got)
	}
	if got := h.phoneClient("phone_device_info"); got != nil {
		t.Fatal("the superseded tool still routes before persistence")
	}

	store.release()
	waitHelloLoop(t, oldDone, "the older registration")
	waitHelloLoop(t, done, "the registration sequence")
	waitHelloWrites(t, h)
	registrations := store.savedRegistrations()
	if len(registrations) != 2 {
		t.Fatalf("got %d phone writes, want the held old write and one coalesced newest write: %+v", len(registrations), registrations)
	}
	for _, saved := range registrations[len(registrations)-1:] {
		if len(saved.Tools) != 1 || saved.Tools[0].Name != "phone_contacts" {
			t.Fatalf("the store ended with an older tool set: %+v", saved.Tools)
		}
	}
	h.mu.Lock()
	registered := h.phoneTools["phone"]
	h.mu.Unlock()
	if len(registered.Tools) != 1 || registered.Tools[0].Name != "phone_contacts" {
		t.Fatalf("the registry ended with an older tool set: %+v", registered.Tools)
	}
	if hasTool(h.phoneToolList(), "phone_device_info") || hasTool(h.phoneToolList(), "phone_reminders_list") || !hasTool(h.phoneToolList(), "phone_contacts") {
		t.Fatalf("the published tool list does not match the newest registration: %+v", h.phoneToolList())
	}
}

// Replacing A's pending registration must move it behind B: the surviving messages are B1, A2, not A2, B1.
func TestRegisterPhoneCoalescingKeepsLatestMessageOrder(t *testing.T) {
	h := newTestHub(t)
	store := newHelloStore()
	h.store = store
	t.Cleanup(store.release)
	seed := &client{hub: h, device: "dev_seed", role: roleFull, out: make(chan []byte, 16)}
	h.onClientMessage(context.Background(), seed, []byte(`{"type":"register_phone","device_id":"seed","tools":[{"name":"phone_device_info"}]}`))
	select {
	case <-store.toolsStarted:
	case <-time.After(readLoopLimit):
		t.Fatal("the seed phone-tool write did not start")
	}
	a := &client{hub: h, device: "dev_a", role: roleFull, out: make(chan []byte, 16)}
	b := &client{hub: h, device: "dev_b", role: roleFull, out: make(chan []byte, 16)}
	for _, report := range []struct {
		c   *client
		raw string
	}{
		{a, `{"type":"register_phone","device_id":"a","tools":[{"name":"phone_contacts"}]}`},
		{b, `{"type":"register_phone","device_id":"b","tools":[{"name":"phone_contacts"}]}`},
		{a, `{"type":"register_phone","device_id":"a","tools":[{"name":"phone_contacts"},{"name":"phone_reminders_list"}]}`},
	} {
		h.onClientMessage(context.Background(), report.c, []byte(report.raw))
	}
	store.release()
	waitHelloWrites(t, h)
	registrations := store.savedRegistrations()
	if len(registrations) != 3 {
		t.Fatalf("got %d registrations, want seed plus the two surviving snapshots: %+v", len(registrations), registrations)
	}
	for i, want := range []string{"seed", "b", "a"} {
		if registrations[i].DeviceID != want {
			t.Fatalf("registration order = %+v, want seed, b, a", registrations)
		}
	}
	if got := registrations[2].Tools; len(got) != 2 || got[1].Name != "phone_reminders_list" {
		t.Fatalf("the last registration is not A's newest snapshot: %+v", got)
	}
}

func TestHelloSettingsLifecycle(t *testing.T) {
	t.Run("connection language precedes persistence", func(t *testing.T) {
		h := newTestHub(t)
		m := newMachine(h, MachineConfig{ID: "example", Name: "Example", Kind: kindLink})
		m.snap = &proto.Machine{ID: m.cfg.ID, Name: m.cfg.Name, DeployError: i18n.T(i18n.Default, "devices-deploy.hub_changed")}
		h.machines = []*Machine{m}
		h.byID = map[string]*Machine{m.cfg.ID: m}
		store := newHelloStore()
		h.store = store
		t.Cleanup(store.release)
		c := &client{hub: h, device: "dev_phone", role: roleFull, out: make(chan []byte, 16)}
		done := make(chan struct{})
		go func() {
			defer close(done)
			h.onClientHello(c, []byte(`{"locale":"en"}`))
		}()
		select {
		case <-store.putStarted:
		case <-time.After(readLoopLimit):
			t.Fatal("the locale save did not start")
		}
		machines := waitClientMessage(t, c, "machines")
		list := machines["machines"].([]any)
		deployError := list[0].(map[string]any)["deploy_error"]
		if deployError != i18n.T(i18n.En, "devices-deploy.hub_changed") {
			t.Fatalf("the connection's reply used its old language while its save was held: %v", machines)
		}
		waitClientMessage(t, c, "client_hello_ack")
		store.release()
		waitHelloLoop(t, done, "the locale-changing hello")
		waitHelloWrites(t, h)
	})

	t.Run("shutdown drains the last saves", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			deployCtx, cancelDeploy := context.WithCancel(context.Background())
			t.Cleanup(cancelDeploy)
			h := newTestHubWithDeployCtx(t, deployCtx)
			// The language update's unrelated assistant debounce has no bus connection in this fixture; do not leave its
			// timer inside the synctest bubble after the worker assertions finish.
			h.assistantOf(assistantwire.AssistantID).updating.Store(true)
			store := newHelloStore()
			h.store = store
			t.Cleanup(store.release)
			phone := &client{hub: h, device: "dev_phone", role: roleFull, out: make(chan []byte, 16)}
			h.onClientHello(phone, []byte(`{"platform":"ios","version":"1.0","locale":"en"}`))
			h.onClientMessage(context.Background(), phone, []byte(`{"type":"register_phone","device_id":"phone","tools":[{"name":"phone_contacts"}]}`))
			for {
				select {
				case key := <-store.putStarted:
					if key == clientAppsSetting {
						goto appWriteHeld
					}
				case <-time.After(readLoopLimit):
					t.Fatal("the first client-app save did not start")
				}
			}
		appWriteHeld:
			select {
			case <-store.toolsStarted:
			case <-time.After(readLoopLimit):
				t.Fatal("the first phone-tool save did not start")
			}

			// These reports are still queued, not merely part of an in-flight Store call, when shutdown starts.
			h.onClientHello(phone, []byte(`{"platform":"ios","version":"2.0","locale":"en"}`))
			other := &client{hub: h, device: "dev_other", role: roleFull, out: make(chan []byte, 16)}
			h.onClientMessage(context.Background(), other, []byte(`{"type":"register_phone","device_id":"other","tools":[{"name":"phone_contacts"}]}`))

			// Production cancels deployCtx, then explicitly stops the hello consumers after socket handlers finish.
			// Settle every goroutine instead of using a wall-clock negative assertion.
			cancelDeploy()
			joined := make(chan struct{})
			go func() {
				h.stopHelloWriters()
				h.nodeWorkers.Wait()
				close(joined)
			}()
			synctest.Wait()
			select {
			case <-joined:
				t.Fatal("the worker join returned while the last saves were still held")
			default:
			}
			store.release()
			waitHelloLoop(t, joined, "the worker join after release")
			if got := store.putCount(clientAppsSetting); got != 2 {
				t.Fatalf("shutdown saved %s %d times, want the held and queued snapshots", clientAppsSetting, got)
			}
			for _, key := range []string{deviceLocalesSetting, userLocaleSetting} {
				if got := store.putCount(key); got != 1 {
					t.Fatalf("shutdown saved %s %d times, want its last snapshot once", key, got)
				}
			}
			var apps map[string]assistantwire.ClientApp
			store.savedSetting(t, clientAppsSetting, &apps)
			if apps["dev_phone"].Version != "2.0" {
				t.Fatalf("shutdown lost the hello queued behind the held save: %+v", apps)
			}
			registrations := store.savedRegistrations()
			if len(registrations) != 2 || registrations[0].DeviceID != "phone" || registrations[1].DeviceID != "other" {
				t.Fatalf("shutdown lost or reordered the queued phone-tool registration: %+v", registrations)
			}
		})
	})
}
