package node

import (
	"bytes"
	"encoding/json"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/proto"
	"github.com/nwverse/persona/server/internal/rpc"
)

// An older record whose persistence is blocked must not reach the durable store after a newer launcher switch. Every
// writer releases state.mu around the callback so reads can proceed, while the persistence sequence keeps the switch
// from committing and writing ahead of the older callback.
func TestStatePersistenceSerializesOlderWritesBeforeSwitches(t *testing.T) {
	const id = "00000000-0000-4000-8000-000000000867"
	started := time.Now().UTC()
	older := sessionState{
		Launcher:      "older-launcher",
		Model:         "older-model",
		Cwd:           "/older",
		Pane:          "older-pane",
		StartedAt:     started,
		ModelRecovery: &proto.ModelRecovery{From: "configured-model", To: "recovery-model"},
	}
	newer := sessionState{
		Launcher:  "newer-launcher",
		Model:     "newer-model",
		Cwd:       "/newer",
		Pane:      "newer-pane",
		StartedAt: started.Add(time.Minute),
	}
	tests := []struct {
		name       string
		writeOlder func(*stateFile)
	}{
		{name: "put", writeOlder: func(s *stateFile) { s.put(id, older) }},
		{name: "update", writeOlder: func(s *stateFile) {
			s.update(id, func(v *sessionState) bool {
				v.AppMode = "older-mode"
				return true
			})
		}},
		{name: "adoptSwitch", writeOlder: func(s *stateFile) { s.adoptSwitch(id, older) }},
		{name: "restoreLaunch", writeOlder: func(s *stateFile) { s.restoreLaunch(id, older, true, "") }},
		{name: "setUltracode", writeOlder: func(s *stateFile) {
			s.setUltracode(id, ultracodeRecord{Since: started, Value: ultraBool(true), At: started})
		}},
		{name: "setPane", writeOlder: func(s *stateFile) { s.setPane(id, older.Pane) }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := newState()
			initial := older
			initial.Launcher, initial.Model, initial.Cwd, initial.Pane = "initial-launcher", "initial-model", "/initial", "initial-pane"
			s.Sessions[id] = initial

			oldStarted := make(chan struct{})
			releaseOld := make(chan struct{})
			var releaseOnce sync.Once
			unblock := func() { releaseOnce.Do(func() { close(releaseOld) }) }
			defer unblock()
			var mu sync.Mutex
			callbacks := 0
			var durable []sessionState
			s.persist = func(_ string, v sessionState) {
				mu.Lock()
				first := callbacks == 0
				callbacks++
				mu.Unlock()
				if first {
					close(oldStarted)
					<-releaseOld
				}
				mu.Lock()
				durable = append(durable, v)
				mu.Unlock()
			}

			oldDone := make(chan struct{})
			go func() {
				tc.writeOlder(s)
				close(oldDone)
			}()
			select {
			case <-oldStarted:
			case <-time.After(3 * time.Second):
				t.Fatal("the older write did not reach persistence")
			}

			secondCalling := make(chan struct{})
			secondDone := make(chan struct{})
			go func() {
				close(secondCalling)
				s.adoptSwitch(id, newer)
				close(secondDone)
			}()
			<-secondCalling
			select {
			case <-secondDone: // the unordered implementation commits and persists the switch here
			case <-time.After(200 * time.Millisecond): // a serialized switch waits for the older callback
			}

			readDone := make(chan sessionState, 1)
			go func() {
				v, _ := s.get(id)
				readDone <- v
			}()
			select {
			case <-readDone:
			case <-time.After(3 * time.Second):
				t.Fatal("a state read blocked behind the older persistence callback")
			}

			unblock()
			for name, done := range map[string]chan struct{}{"older": oldDone, "switch": secondDone} {
				select {
				case <-done:
				case <-time.After(3 * time.Second):
					t.Fatalf("the %s write did not finish", name)
				}
			}

			memory, ok := s.get(id)
			if !ok {
				t.Fatal("the session record disappeared")
			}
			requireLaunchState(t, "memory", memory, newer)
			if memory.ModelRecovery == nil || memory.ModelRecovery.To != "recovery-model" {
				t.Fatalf("the switch lost model recovery: %+v", memory.ModelRecovery)
			}
			mu.Lock()
			writes := append([]sessionState(nil), durable...)
			mu.Unlock()
			if len(writes) != 2 {
				t.Fatalf("persistence callback count = %d, want 2", len(writes))
			}
			last := writes[len(writes)-1]
			requireLaunchState(t, "durable", last, memory)
			reloaded := newState()
			reloaded.merge(map[string]sessionState{id: last})
			restored, ok := reloaded.get(id)
			if !ok {
				t.Fatal("the durable record did not reload")
			}
			requireLaunchState(t, "reloaded", restored, memory)
		})
	}
}

func requireLaunchState(t *testing.T, where string, got, want sessionState) {
	t.Helper()
	if got.Launcher != want.Launcher || got.Model != want.Model || got.Cwd != want.Cwd || got.Pane != want.Pane {
		t.Fatalf("%s launch state = {%q %q %q %q}, want {%q %q %q %q}", where,
			got.Launcher, got.Model, got.Cwd, got.Pane, want.Launcher, want.Model, want.Cwd, want.Pane)
	}
}

type stateRPCBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (b *stateRPCBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.b.Write(p)
}

func (b *stateRPCBuffer) Bytes() []byte {
	b.mu.Lock()
	defer b.mu.Unlock()
	return bytes.Clone(b.b.Bytes())
}

// This uses the production Node.persistState and rpc.Peer.Notify path. The old record is still being JSON-encoded
// when the smaller switch record reaches the writer semaphore; the semaphore alone does not restore mutation order.
func TestStatePersistenceOrdersActualRPCNotifications(t *testing.T) {
	wire := new(stateRPCBuffer)
	n := &Node{state: newState()}
	n.peer = rpc.New("n", wire)
	n.state.persist = n.persistState
	const id = "00000000-0000-4000-8000-000000000001"
	n.state.Sessions[id] = sessionState{Launcher: "initial", Cwd: "/tmp", StartedAt: time.Now()}
	old := sessionState{
		Launcher:       "claude",
		Model:          "claude-sonnet-5-5",
		Cwd:            "/tmp",
		StartedAt:      time.Now(),
		PendingHandoff: &pendingHandoff{Text: strings.Repeat("handoff", 4<<20)},
	}
	oldDone := make(chan struct{})
	go func() {
		n.state.put(id, old)
		close(oldDone)
	}()
	deadline := time.Now().Add(10 * time.Second)
	for {
		current, _ := n.state.get(id)
		if current.Launcher == "claude" {
			break // put committed its old snapshot and released state.mu
		}
		if time.Now().After(deadline) {
			t.Fatal("the older put did not commit its in-memory snapshot")
		}
		runtime.Gosched()
	}
	n.state.adoptSwitch(id, sessionState{
		Launcher:  "codex",
		Model:     "gpt-5.3-codex",
		Cwd:       "/private/tmp",
		StartedAt: old.StartedAt.Add(time.Minute),
	})
	<-oldDone
	current, _ := n.state.get(id)
	if current.Launcher != "codex" {
		t.Fatalf("in-memory launcher = %q, want codex", current.Launcher)
	}
	var launchers []string
	for _, line := range bytes.Split(bytes.TrimSpace(wire.Bytes()), []byte("\n")) {
		var event struct {
			Event  string `json:"event"`
			Params struct {
				State sessionState `json:"state"`
			} `json:"params"`
		}
		if err := json.Unmarshal(line, &event); err != nil {
			t.Fatalf("decode RPC line: %v", err)
		}
		if event.Event == "state.put" {
			launchers = append(launchers, event.Params.State.Launcher)
		}
	}
	if len(launchers) != 2 {
		t.Fatalf("state.put count = %d, want 2", len(launchers))
	}
	if launchers[1] != current.Launcher {
		t.Fatalf("wire order %v leaves durable launcher %q; memory has %q", launchers, launchers[1], current.Launcher)
	}
}
