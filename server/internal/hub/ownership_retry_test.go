package hub

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/nwverse/persona/server/internal/assistantwire"
	"github.com/nwverse/persona/server/internal/proto"
)

// startupOwnershipStore fails each selected setting read a fixed number of times and records when it was tried.
type startupOwnershipStore struct {
	*ownersStore
	mu       sync.Mutex
	failures map[string]int
	reads    map[string][]time.Time
}

func (s *startupOwnershipStore) Setting(ctx context.Context, key string, out any) (bool, error) {
	s.mu.Lock()
	s.reads[key] = append(s.reads[key], time.Now())
	fail := s.failures[key] > 0
	if fail {
		s.failures[key]--
	}
	s.mu.Unlock()
	if fail {
		return false, errors.New("database restarting")
	}
	return s.ownersStore.Setting(ctx, key, out)
}

func (s *startupOwnershipStore) readTimes(key string) []time.Time {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]time.Time(nil), s.reads[key]...)
}

// gatedEntriesWriteStore holds the entries write until a recovery read has had the chance to run.
type gatedEntriesWriteStore struct {
	*entriesFailStore
	started     chan struct{}
	readStarted chan struct{}
	release     chan struct{}
}

func (s *gatedEntriesWriteStore) Setting(ctx context.Context, key string, out any) (bool, error) {
	if key == machineEntriesKey {
		select {
		case s.readStarted <- struct{}{}:
		default:
		}
	}
	return s.entriesFailStore.Setting(ctx, key, out)
}

func (s *gatedEntriesWriteStore) PutSetting(ctx context.Context, key string, v any) error {
	if key == machineEntriesKey {
		select {
		case s.started <- struct{}{}:
		default:
		}
		<-s.release
	}
	return s.entriesFailStore.PutSetting(ctx, key, v)
}

func TestEntriesRecoveryReadWaitsForWrite(t *testing.T) {
	ctx := context.Background()
	gated := &gatedEntriesWriteStore{entriesFailStore: &entriesFailStore{meshStore: newMeshStore()},
		started: make(chan struct{}, 1), readStarted: make(chan struct{}, 1), release: make(chan struct{})}
	h := newTestHub(t)
	h.store = gated
	h.devices.mu.Lock()
	h.devices.keptRead, h.devices.reportedRead = true, true
	h.devices.mu.Unlock()
	var once sync.Once
	release := func() { once.Do(func() { close(gated.release) }) }
	defer release()

	saved := make(chan struct{})
	go func() {
		defer close(saved)
		h.saveMachineEntries(ctx)
	}()
	select {
	case <-gated.started:
	case <-time.After(5 * time.Second):
		t.Fatal("the entries write did not begin")
	}
	loaded := make(chan error, 1)
	go func() { loaded <- h.loadMachineEntries(ctx) }()
	select {
	case <-gated.readStarted:
		t.Fatal("a recovery read ran during the write")
	case <-time.After(100 * time.Millisecond): // a blocked read has no event; release it after this deadlock guard
	}
	if got := gated.readCount(); got != 0 {
		t.Fatalf("a recovery read ran during the write: %d reads", got)
	}
	release()
	select {
	case <-saved:
	case <-time.After(5 * time.Second):
		t.Fatal("the entries write did not finish")
	}
	select {
	case err := <-loaded:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the recovery read did not finish after the write")
	}
	if got := gated.readCount(); got != 1 {
		t.Fatalf("recovery reads after the write = %d, want 1", got)
	}
}

func TestStartupOwnershipReadsRetryAndReconcile(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx := context.Background()
		h := newTestHub(t)
		h.cfg.Machines = []MachineConfig{
			{ID: "moved", Name: "Moved", Kind: "ssh"},
			{ID: "dedicated", Name: "Dedicated", Kind: "local", OwnerRequired: true},
		}
		st := &startupOwnershipStore{
			ownersStore: &ownersStore{kept: map[string]json.RawMessage{
				machineOwnersKey: json.RawMessage(`{"moved":"m-0b0b0b","dedicated":"m-0b0b0b"}`),
				machinesKeptKey:  json.RawMessage(`{"kept":"m-0b0b0b"}`),
			}},
			failures: map[string]int{machineOwnersKey: 2, machinesKeptKey: 2},
			reads:    map[string][]time.Time{},
		}
		h.bus.idents = st
		h.store = newMeshStore()
		if err := h.store.PutSetting(ctx, machineEntriesKey, reportedMachines{
			"m-0b0b0b": {
				"kept": {
					Cfg:  json.RawMessage(`{"id":"kept","name":"Kept computer","kind":"ssh"}`),
					Info: proto.NodeInfo{Hostname: "kept-host"},
				},
			},
		}); err != nil {
			t.Fatal(err)
		}

		h.startDevices(ctx)
		moved := h.localMachine("moved")
		if moved == nil {
			t.Fatal("the configured machine is missing after startup")
		}
		moved.mu.Lock()
		moved.online, moved.connected, moved.connectedAt = true, true, time.Now()
		moved.mu.Unlock()
		if got := moved.ownerID(); got != assistantwire.AssistantID {
			t.Fatalf("the unread moved machine initially belongs to %q, want the carrier", got)
		}
		if h.localMachine("kept") != nil {
			t.Fatal("a machine whose kept-list read failed is already listed")
		}

		loopCtx, cancel := context.WithCancel(ctx)
		done := make(chan struct{})
		go func() {
			defer close(done)
			h.machineTrustLoop(loopCtx)
		}()
		defer func() {
			cancel()
			<-done
		}()
		synctest.Wait()
		for _, key := range []string{machineOwnersKey, machinesKeptKey} {
			if got := len(st.readTimes(key)); got != 2 {
				t.Fatalf("%s was read %d times after the first retry, want 2", key, got)
			}
		}
		if h.entryOwner("moved") != assistantwire.AssistantID || h.localMachine("kept") != nil {
			t.Fatal("the second failed read unexpectedly reconciled the machines")
		}

		synctest.Sleep(trustRetryEvery)
		synctest.Wait()
		for _, key := range []string{machineOwnersKey, machinesKeptKey} {
			reads := st.readTimes(key)
			if len(reads) != 3 {
				t.Fatalf("%s was read %d times after recovery, want 3", key, len(reads))
			}
			if gap := reads[2].Sub(reads[1]); gap != trustRetryEvery {
				t.Fatalf("%s retry gap = %v, want %v", key, gap, trustRetryEvery)
			}
		}
		if got := h.entryOwner("moved"); got != "m-0b0b0b" {
			t.Fatalf("the recovered owner = %q, want m-0b0b0b", got)
		}
		if got := moved.ownerID(); got != "m-0b0b0b" || moved.snapshot().Online {
			t.Fatalf("handover left owner %q and online %v", got, moved.snapshot().Online)
		}
		if got := h.localMachine("dedicated").ownerID(); got != "m-0b0b0b" {
			t.Fatalf("the dedicated machine's recovered owner = %q", got)
		}
		kept, ok := listed(h, "kept")
		if !ok || kept.Manager != "m-0b0b0b" || kept.Name != "Kept computer" {
			t.Fatalf("the recovered kept machine = %+v, present %v", kept, ok)
		}
		// Finish the coalesced config and manager updates before leaving the fake-time bubble.
		synctest.Sleep(200 * time.Millisecond)
		synctest.Wait()
	})
}

func TestMachineListOmitsUnavailableOwner(t *testing.T) {
	h := newTestHub(t)
	h.cfg.Machines = []MachineConfig{{ID: "dedicated", Name: "Dedicated", Kind: "local", OwnerRequired: true}}
	st := &startupOwnershipStore{
		ownersStore: &ownersStore{kept: map[string]json.RawMessage{
			machineOwnersKey: json.RawMessage(`{"dedicated":"m-0b0b0b"}`),
		}},
		failures: map[string]int{machineOwnersKey: 1},
		reads:    map[string][]time.Time{},
	}
	h.bus.idents = st
	h.startDevices(context.Background())
	m, ok := listed(h, "dedicated")
	if !ok {
		t.Fatal("the dedicated machine is missing from the machine list")
	}
	if m.Manager != "" {
		t.Fatalf("manager = %q while ownership is unavailable, want empty", m.Manager)
	}
}

func TestKeptRecoveryRetriesEntriesRead(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx := context.Background()
		h := newTestHub(t)
		seen := time.Date(2026, 11, 14, 12, 0, 0, 0, time.UTC)
		keptStore := &startupOwnershipStore{
			ownersStore: &ownersStore{kept: map[string]json.RawMessage{
				machinesKeptKey: json.RawMessage(`{"kept":"m-0b0b0b"}`),
			}},
			failures: map[string]int{machinesKeptKey: 1},
			reads:    map[string][]time.Time{},
		}
		entries := &entriesFailStore{meshStore: newMeshStore()}
		if err := entries.PutSetting(ctx, machineEntriesKey, reportedMachines{
			"m-0b0b0b": {"kept": {
				Cfg:      json.RawMessage(`{"id":"kept","name":"Kept computer","kind":"ssh"}`),
				Info:     proto.NodeInfo{Hostname: "kept-host"},
				LastSeen: seen,
			}},
		}); err != nil {
			t.Fatal(err)
		}
		h.bus.idents = keptStore
		h.store = entries
		h.startDevices(ctx) // kept #1 fails; entries #1 succeeds but filters out the not-yet-kept machine.
		h.saveMachineEntries(ctx)
		if got := entries.readCount(); got != 1 {
			t.Fatalf("entries reads after a write with the kept list unread = %d, want 1", got)
		}
		if got := keptEntries(t, entries.meshStore)["m-0b0b0b"]["kept"]; got.Info.Hostname != "kept-host" || !got.LastSeen.Equal(seen) {
			t.Fatalf("a write with the kept list unread dropped the saved entry: %+v", got)
		}
		entries.mu.Lock()
		entries.fails = 1 // entries #2 fails after the kept-list recovery.
		entries.mu.Unlock()

		loopCtx, cancel := context.WithCancel(ctx)
		done := make(chan struct{})
		go func() {
			defer close(done)
			h.machineTrustLoop(loopCtx)
		}()
		defer func() {
			cancel()
			<-done
		}()
		synctest.Wait()
		if got := len(keptStore.readTimes(machinesKeptKey)); got != 2 {
			t.Fatalf("kept reads = %d, want 2", got)
		}
		if got := entries.readCount(); got != 2 {
			t.Fatalf("entries reads after the injected failure = %d, want 2", got)
		}
		h.devices.mu.Lock()
		degradedRead := h.devices.reportedRead
		h.devices.mu.Unlock()
		h.saveMachineEntries(ctx)
		if got := entries.readCount(); got != 2 {
			t.Fatalf("entries reads after a write during recovery = %d, want 2", got)
		}
		if got := keptEntries(t, entries.meshStore)["m-0b0b0b"]["kept"]; got.Info.Hostname != "kept-host" || !got.LastSeen.Equal(seen) {
			t.Fatalf("a write during recovery dropped the saved entry: %+v", got)
		}

		synctest.Sleep(trustRetryEvery)
		synctest.Wait()
		if got := entries.readCount(); got != 3 {
			t.Fatalf("entries reads after the trust retry = %d, want 3", got)
		}
		if got := len(keptStore.readTimes(machinesKeptKey)); got != 3 {
			t.Fatalf("kept reads after the trust retry = %d, want 3", got)
		}
		m, ok := listed(h, "kept")
		if !ok || m.Manager != "m-0b0b0b" || m.Name != "Kept computer" || m.Kind != "ssh" ||
			m.Hostname != "kept-host" || !m.LastSeen.Equal(seen) {
			t.Fatalf("recovered kept machine = %+v, present %v", m, ok)
		}
		if degradedRead {
			t.Fatal("the failed entries read left reportedRead true, allowing a degraded write-back")
		}
		h.devices.mu.Lock()
		recovered := h.devices.reportedRead && h.devices.keptRead
		h.devices.mu.Unlock()
		if !recovered {
			t.Fatal("recovery did not finish after the successful entries read")
		}
		synctest.Sleep(200 * time.Millisecond)
		synctest.Wait()
	})
}

func TestLateKeptListReadReconcilesMachineList(t *testing.T) {
	ctx := context.Background()
	h := newTestHub(t)
	st := &startupOwnershipStore{
		ownersStore: &ownersStore{kept: map[string]json.RawMessage{
			machinesKeptKey: json.RawMessage(`{"kept":"m-0b0b0b"}`),
		}},
		failures: map[string]int{machinesKeptKey: 1},
		reads:    map[string][]time.Time{},
	}
	h.bus.idents = st
	h.store = newMeshStore()
	if err := h.store.PutSetting(ctx, machineEntriesKey, reportedMachines{
		"m-0b0b0b": {
			"kept": {Cfg: json.RawMessage(`{"id":"kept","name":"Kept computer","kind":"ssh"}`)},
		},
	}); err != nil {
		t.Fatal(err)
	}
	h.startDevices(ctx)
	if h.localMachine("kept") != nil {
		t.Fatal("a machine whose kept-list read failed is already listed")
	}
	keeps, known := h.keepsAnyKnown(ctx, "m-0b0b0b")
	if !known || !keeps {
		t.Fatalf("the late kept-list read returned known %v and keeps %v", known, keeps)
	}
	m, ok := listed(h, "kept")
	if !ok || m.Manager != "m-0b0b0b" || m.Name != "Kept computer" {
		t.Fatalf("the late-read kept machine = %+v, present %v", m, ok)
	}
}
