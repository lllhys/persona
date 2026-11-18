package hub

import (
	"context"
	"fmt"
	"io"
	"log"
	"slices"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/nwverse/persona/server/internal/assistantwire"
	"github.com/nwverse/persona/server/internal/db"
	"github.com/nwverse/persona/server/internal/proto"
)

// shutdownOrderStore records the production cleanup order. Closing saved also releases the finite node worker.
type shutdownOrderStore struct {
	*helloStore
	hub                 *Hub
	mu                  sync.Mutex
	events              []string
	helloStoppedAtClose bool
	saved               chan struct{}
	saveOnce            sync.Once
}

func newShutdownOrderStore() *shutdownOrderStore {
	base := newHelloStore()
	base.release()
	return &shutdownOrderStore{helloStore: base, saved: make(chan struct{})}
}

func (s *shutdownOrderStore) PutSetting(ctx context.Context, key string, value any) error {
	err := s.helloStore.PutSetting(ctx, key, value)
	if key == clientAppsSetting && err == nil {
		s.record("hello saved")
		s.releaseNode()
	}
	return err
}

func (s *shutdownOrderStore) CloseContext(context.Context) error {
	s.hub.helloWrites.mu.Lock()
	stopped := s.hub.helloWrites.stopped
	s.hub.helloWrites.mu.Unlock()
	s.mu.Lock()
	s.helloStoppedAtClose = stopped
	s.mu.Unlock()
	s.record("database closed")
	return nil
}

func (s *shutdownOrderStore) releaseNode() { s.saveOnce.Do(func() { close(s.saved) }) }

func (s *shutdownOrderStore) record(event string) {
	s.mu.Lock()
	s.events = append(s.events, event)
	s.mu.Unlock()
}

func (s *shutdownOrderStore) snapshot() ([]string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.events...), s.helloStoppedAtClose
}

// Drive runHub's named cleanup itself. The cancel seam represents a connection that is still open after deploy
// cancellation; the real sequence must save its report before the finite-worker join and before closing the Store.
func TestHubCleanupDrainsHelloBeforeNodeJoinAndDatabaseClose(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		deployCtx, cancelDeploy := context.WithCancel(context.Background())
		t.Cleanup(cancelDeploy)
		h := newTestHubWithDeployCtx(t, deployCtx)
		h.assistantOf(assistantwire.AssistantID).updating.Store(true)
		budget := newShutdownBudget(h.cfg)
		h.shutdown = budget
		t.Cleanup(budget.close)
		store := newShutdownOrderStore()
		store.hub = h
		h.store = store
		t.Cleanup(func() {
			store.releaseNode()
			h.nodeWorkers.Wait()
		})
		h.nodeWorkers.Add(1)
		go func() {
			defer h.nodeWorkers.Done()
			<-store.saved
			store.record("node joined")
		}()

		cancel := func() {
			cancelDeploy()
			c := &client{hub: h, device: "dev_cleanup", role: roleFull, out: make(chan []byte, 16)}
			h.onClientMessage(context.Background(), c, []byte(`{"type":"client_hello","platform":"ios","version":"1.0","locale":"en"}`))
		}
		h.closeDatabaseAfterWorkers(cancel, store.CloseContext)
		got, stoppedAtClose := store.snapshot()
		if want := []string{"hello saved", "node joined", "database closed"}; !slices.Equal(got, want) {
			t.Errorf("hub cleanup order = %v, want %v", got, want)
		}
		if !stoppedAtClose {
			t.Error("the database closed before the hello-writer shutdown step")
		}
	})
}

type firstPhoneWriteGate struct {
	*helloStore
	started     chan struct{}
	released    chan struct{}
	startOnce   sync.Once
	releaseOnce sync.Once
}

func newFirstPhoneWriteGate() *firstPhoneWriteGate {
	base := newHelloStore()
	base.release()
	return &firstPhoneWriteGate{helloStore: base, started: make(chan struct{}), released: make(chan struct{})}
}

func (s *firstPhoneWriteGate) ReplacePhoneTools(ctx context.Context, registration db.PhoneToolRegistration) error {
	first := false
	s.startOnce.Do(func() {
		first = true
		close(s.started)
	})
	if first {
		<-s.released
	}
	return s.helloStore.ReplacePhoneTools(ctx, registration)
}

func (s *firstPhoneWriteGate) release() { s.releaseOnce.Do(func() { close(s.released) }) }

// Both producers initially wait for the same new device. Releasing one slot at a time identifies the later
// replacement deterministically; its redundant admission token must not survive the final drain.
func TestPhoneRacingProducersReturnRedundantSlot(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		h := newTestHub(t)
		store := newFirstPhoneWriteGate()
		h.store = store
		t.Cleanup(store.release)
		h.setPhoneTools(db.PhoneToolRegistration{DeviceID: "seed", Tools: []proto.Tool{{Name: "phone_device_info"}}})
		select {
		case <-store.started:
		case <-time.After(readLoopLimit):
			t.Fatal("the seed phone write did not start")
		}
		for i := range helloPendingLimit {
			h.setPhoneTools(db.PhoneToolRegistration{DeviceID: fmt.Sprintf("flood-%d", i), Tools: []proto.Tool{{Name: "phone_contacts"}}})
		}
		h.phoneToolUpdates.mu.Lock()
		pending, order := len(h.phoneToolUpdates.pending), len(h.phoneToolUpdates.order)
		h.phoneToolUpdates.mu.Unlock()
		if pending != helloPendingLimit || order != helloPendingLimit || len(h.helloWrites.toolsSlot) != helloPendingLimit {
			t.Fatalf("full phone queue = %d/%d entries and %d slots, want %d", pending, order, len(h.helloWrites.toolsSlot), helloPendingLimit)
		}

		done := make(chan string, 2)
		tools := map[string]string{"a": "phone_contacts", "b": "phone_reminders_list"}
		for _, producer := range []string{"a", "b"} {
			go func() {
				h.setPhoneTools(db.PhoneToolRegistration{DeviceID: "shared", CredentialID: "dev_" + producer, Tools: []proto.Tool{{Name: tools[producer]}}})
				done <- producer
			}()
		}
		synctest.Wait()
		select {
		case producer := <-done:
			t.Fatalf("producer %s finished before room appeared", producer)
		default:
		}

		// Detach one real pending entry and return its token. The Store is still held, so no other capacity appears.
		detachOne := func() {
			h.phoneToolUpdates.mu.Lock()
			id := h.phoneToolUpdates.order[0]
			h.phoneToolUpdates.order = h.phoneToolUpdates.order[1:]
			delete(h.phoneToolUpdates.pending, id)
			h.phoneToolUpdates.mu.Unlock()
			releaseHelloReports(h.helloWrites.toolsSlot, 1)
		}
		detachOne()
		var first string
		select {
		case first = <-done:
		case <-time.After(readLoopLimit):
			t.Fatal("neither same-device producer returned after room appeared")
		}
		synctest.Wait()
		select {
		case second := <-done:
			t.Fatalf("producer %s returned without a second free slot", second)
		default:
		}
		h.phoneToolUpdates.mu.Lock()
		queued := h.phoneToolUpdates.pending["shared"]
		sharedOrder := 0
		for _, id := range h.phoneToolUpdates.order {
			if id == "shared" {
				sharedOrder++
			}
		}
		pending, order = len(h.phoneToolUpdates.pending), len(h.phoneToolUpdates.order)
		h.phoneToolUpdates.mu.Unlock()
		if len(queued.Tools) != 1 || queued.Tools[0].Name != tools[first] || sharedOrder != 1 || pending != helloPendingLimit || order != helloPendingLimit {
			t.Fatalf("after the first producer, shared registration = %+v with %d queue positions and %d/%d pending", queued, sharedOrder, pending, order)
		}

		detachOne()
		var second string
		select {
		case second = <-done:
		case <-time.After(readLoopLimit):
			t.Fatal("the second same-device producer did not return")
		}
		if second == first {
			t.Fatalf("producer %q returned twice", second)
		}
		h.phoneToolUpdates.mu.Lock()
		queued = h.phoneToolUpdates.pending["shared"]
		sharedOrder = 0
		for _, id := range h.phoneToolUpdates.order {
			if id == "shared" {
				sharedOrder++
			}
		}
		pending, order = len(h.phoneToolUpdates.pending), len(h.phoneToolUpdates.order)
		h.phoneToolUpdates.mu.Unlock()
		if len(queued.Tools) != 1 || queued.Tools[0].Name != tools[second] || sharedOrder != 1 || pending != helloPendingLimit-1 || order != helloPendingLimit-1 {
			t.Fatalf("the later producer did not leave its only newest registration: %+v, positions=%d, pending=%d/%d", queued, sharedOrder, pending, order)
		}

		store.release()
		waitHelloWrites(t, h)
		if got := len(h.helloWrites.toolsSlot); got != 0 {
			t.Fatalf("phone slots in use after the drain = %d, want zero", got)
		}
		var saved []db.PhoneToolRegistration
		for _, registration := range store.savedRegistrations() {
			if registration.DeviceID == "shared" {
				saved = append(saved, registration)
			}
		}
		if len(saved) != 1 || len(saved[0].Tools) != 1 || saved[0].Tools[0].Name != tools[second] {
			t.Fatalf("saved shared registrations = %+v, want only producer %q's newest tools", saved, second)
		}
	})
}

// A raw Hub lets stop race the first start rather than newTestHub's eager start. Either order is valid: an accepted
// report drains with the workers, while a report after stop is not written and will be sent again on reconnect.
func TestHelloStopRacingFirstReport(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		for range 32 {
			store := newHelloStore()
			store.release()
			h := &Hub{store: store, logger: log.New(io.Discard, "", 0)}
			start := make(chan struct{})
			var race sync.WaitGroup
			race.Add(2)
			go func() {
				defer race.Done()
				<-start
				h.updateClientApp("dev_race", func(assistantwire.ClientApp, bool) assistantwire.ClientApp {
					return assistantwire.ClientApp{Platform: "ios", Version: "1.0", SeenAt: time.Now().UTC()}
				})
			}()
			go func() {
				defer race.Done()
				<-start
				h.stopHelloWriters()
			}()
			close(start)
			race.Wait()
			h.stopHelloWriters()
		}

		store := newHelloStore()
		store.release()
		h := &Hub{store: store, logger: log.New(io.Discard, "", 0)}
		h.stopHelloWriters()
		h.updateClientApp("dev_after_stop", func(assistantwire.ClientApp, bool) assistantwire.ClientApp {
			return assistantwire.ClientApp{Platform: "ios", Version: "1.0", SeenAt: time.Now().UTC()}
		})
		if got := store.putAttempts(clientAppsSetting); got != 0 {
			t.Fatalf("a report after stop caused %d client-app writes, want none", got)
		}
	})
}
