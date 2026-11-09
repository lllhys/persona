package hub

import (
	"context"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/db"
	"github.com/nwverse/persona/server/internal/proto"
)

// stalledToolsStore is a store whose phone tool write waits: for release, or for its context to end.
type stalledToolsStore struct {
	*meshStore
	started chan struct{}
	release chan struct{}
}

func (s *stalledToolsStore) ReplacePhoneTools(ctx context.Context, _ db.PhoneToolRegistration) error {
	close(s.started)
	select {
	case <-s.release:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func lockWithin(t *testing.T, h *Hub, d time.Duration) bool {
	t.Helper()
	got := make(chan struct{})
	go func() { h.mu.Lock(); h.mu.Unlock(); close(got) }()
	select {
	case <-got:
		return true
	case <-time.After(d):
		return false
	}
}

// A phone registering its tools while the database stalls holds up neither the hub's other work (broadcasts, client
// messages: all under h.mu) nor itself for good (#5358).
func TestPhoneToolsWriteStallsNothingElse(t *testing.T) {
	old := phoneToolsTimeout
	phoneToolsTimeout = 300 * time.Millisecond
	t.Cleanup(func() { phoneToolsTimeout = old })
	h := newTestHub(t)
	st := &stalledToolsStore{meshStore: newMeshStore(), started: make(chan struct{}), release: make(chan struct{})}
	h.store = st
	done := make(chan struct{})
	go func() {
		defer close(done)
		h.setPhoneTools(db.PhoneToolRegistration{DeviceID: "d1", Tools: []proto.Tool{{Name: "phone_device_info"}}})
	}()
	<-st.started
	if !lockWithin(t, h, time.Second) {
		close(st.release)
		<-done
		t.Fatal("h.mu is held while the phone tools are written")
	}
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		close(st.release)
		t.Fatal("the write did not give up at its timeout")
	}
	if got := h.phoneToolList(); len(got) != 2 || got[0].Name != "phone_write_queue_list" || got[1].Name != "phone_write_queue_cancel" {
		t.Fatalf("a registration that was not saved is listed: %+v", got)
	}
}

// A credential revoked while its phone's tools were being written does not get them listed afterwards.
func TestPhoneToolsRevokedDuringTheWrite(t *testing.T) {
	h := newTestHub(t)
	st := &stalledToolsStore{meshStore: newMeshStore(), started: make(chan struct{}), release: make(chan struct{})}
	h.store = st
	done := make(chan struct{})
	go func() {
		defer close(done)
		h.setPhoneTools(db.PhoneToolRegistration{DeviceID: "d1", CredentialID: "dev_1", Tools: []proto.Tool{{Name: "phone_device_info"}}})
	}()
	<-st.started
	revoked := make(chan struct{})
	go func() { h.revoked("dev_1"); close(revoked) }()
	select {
	case <-revoked:
	case <-time.After(2 * time.Second):
		close(st.release)
		t.Fatal("revoking waits for the phone tool write")
	}
	close(st.release)
	<-done
	if got := h.phoneToolList(); len(got) != 2 || got[0].Name != "phone_write_queue_list" || got[1].Name != "phone_write_queue_cancel" {
		t.Fatalf("a revoked device's tools are listed: %+v", got)
	}
}
