package assistant

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/proto"
)

// flakySeenMem is the fleet's store whose first SeenTimes fails, as a database that blipped while the manager
// started.
type flakySeenMem struct {
	*fleetMem
	mu    sync.Mutex
	fails int // SeenTimes calls still to fail
	calls int
}

func (m *flakySeenMem) SeenTimes(ctx context.Context) (map[[2]string]time.Time, error) {
	m.mu.Lock()
	m.calls++
	fail := m.fails > 0
	if fail {
		m.fails--
	}
	m.mu.Unlock()
	if fail {
		return nil, errors.New("connection refused")
	}
	return m.fleetMem.SeenTimes(ctx)
}

func (m *flakySeenMem) seenCalls() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.calls
}

// newFlakySeenTest is newFleetTest on a store whose first SeenTimes fails, with a finished session the user looked
// at after it finished (seen in the table at seen).
func newFlakySeenTest(t *testing.T) (*fleetTest, *flakySeenMem, time.Time) {
	t.Helper()
	ft := newFleetTest(t)
	seen := time.Now().UTC().Add(-time.Minute).Truncate(time.Second)
	ft.st.seen[[2]string{"mac", "s1"}] = seen
	st := &flakySeenMem{fleetMem: ft.st, fails: 1}
	ft.s = New(Options{Hub: ft.f, Store: func() Store { return st }, Logf: t.Logf, Devices: ft.d})
	ft.d.setSessions("mac", []proto.Session{{SessionID: "s1", Status: proto.StatusDone, UpdatedAt: seen.Add(-time.Minute)}})
	return ft, st, seen
}

func (ft *fleetTest) stateOf(machine, session string) string {
	ft.t.Helper()
	for _, s := range ft.lastList(machine) {
		if s.SessionID == session {
			return s.State
		}
	}
	ft.t.Fatalf("%s/%s not in the hub's list", machine, session)
	return ""
}

// Seen times that could not be read when the manager started are read on the hub's next connection: the session
// the user had looked at is idle again, not unread until opened once more.
func TestSeenTimesReadAgainOnFleetUp(t *testing.T) {
	ft, st, seen := newFlakySeenTest(t)
	ft.s.FleetUp(context.Background())
	if got := ft.s.seenAt("mac", "s1"); !got.IsZero() {
		t.Fatalf("seenAt after the failed read = %v", got)
	}
	if got := ft.stateOf("mac", "s1"); got != proto.StateUnread {
		t.Fatalf("state after the failed read = %q", got)
	}

	ft.s.FleetUp(context.Background())
	if got := ft.s.seenAt("mac", "s1"); !got.Equal(seen) {
		t.Fatalf("seenAt = %v, want %v", got, seen)
	}
	if got := ft.stateOf("mac", "s1"); got != proto.StateIdle {
		t.Fatalf("state = %q, want idle", got)
	}

	ft.s.FleetUp(context.Background()) // read once they could be
	if n := st.seenCalls(); n != 2 {
		t.Fatalf("SeenTimes called %d times, want 2", n)
	}
}

// The expire loop's retry (retrySeen) reads them without a new connection to the hub, keeps a time marked
// meanwhile, and tells the hub the lists with the states it now knows.
func TestSeenTimesRetryKeepsMarkedMeanwhile(t *testing.T) {
	ft, _, seen := newFlakySeenTest(t)
	ft.s.FleetUp(context.Background())
	newer := time.Now().UTC()
	ft.s.fleet.mu.Lock()
	if ft.s.fleet.seen == nil {
		ft.s.fleet.seen = map[[2]string]time.Time{}
	}
	ft.s.fleet.seen[[2]string{"mac", "s2"}] = newer // marked since the start; the table has none for it
	ft.s.fleet.seen[[2]string{"mac", "s1"}] = newer // and one the table has an older time for
	ft.s.fleet.mu.Unlock()

	ft.s.retrySeen(context.Background())
	if got := ft.s.seenAt("mac", "s1"); !got.Equal(newer) || got.Equal(seen) {
		t.Fatalf("s1 seenAt = %v, want the newer %v", got, newer)
	}
	if got := ft.s.seenAt("mac", "s2"); !got.Equal(newer) {
		t.Fatalf("s2 seenAt = %v, want %v", got, newer)
	}
	if got := ft.stateOf("mac", "s1"); got != proto.StateIdle {
		t.Fatalf("state = %q, want idle", got)
	}
}

// A store that does not keep seen times (errNoTable) is not asked again.
func TestSeenTimesNoTableNotRetried(t *testing.T) {
	ft := newFleetTest(t)
	st := &noSeenMem{fleetMem: ft.st}
	ft.s = New(Options{Hub: ft.f, Store: func() Store { return st }, Logf: t.Logf, Devices: ft.d})
	ft.s.FleetUp(context.Background())
	ft.s.FleetUp(context.Background())
	if st.calls != 1 {
		t.Fatalf("SeenTimes called %d times, want 1", st.calls)
	}
}

type noSeenMem struct {
	*fleetMem
	calls int
}

func (m *noSeenMem) SeenTimes(context.Context) (map[[2]string]time.Time, error) {
	m.calls++
	return nil, errNoTable
}

// A runtime's session the user saw is told to its node (#6299), so a runtime keeping an unread mark of its own (Paseo)
// clears it: only while its node lists it as unread (done), and only a runtime's.
func TestSeenToldToTheRuntimesNode(t *testing.T) {
	ft := newFleetTest(t)
	ft.d.setSessions("mac", []proto.Session{
		{SessionID: "paseo-1", Status: proto.StatusDone, SourceKind: proto.SourceRuntime},
		{SessionID: "paseo-2", Status: proto.StatusIdle, SourceKind: proto.SourceRuntime},
		{SessionID: "11111111-2222-4333-8444-555555555555", Status: proto.StatusDone},
	})
	for _, id := range []string{"paseo-2", "11111111-2222-4333-8444-555555555555", "paseo-1"} {
		ft.s.markSeen(context.Background(), "mac", id)
	}
	waitFor(t, "the node told", func() bool { return len(ft.d.callsTo("mac", deviceSessionSeen)) == 1 })
	if got := string(ft.d.callsTo("mac", deviceSessionSeen)[0]); got != `{"session_id":"paseo-1"}` {
		t.Fatalf("told %s", got)
	}
}
