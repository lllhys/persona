package assistant

import (
	"encoding/json"
	"fmt"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/proto"
)

// BenchmarkSessionsChangedEvent: a node's sessions.changed for 300 sessions as the assistant takes it (the devices
// have the list already, as in production).
func BenchmarkSessionsChangedEvent(b *testing.B) {
	s, params := sessionsChangedOf()
	b.ReportAllocs()
	for b.Loop() {
		s.NodeEvent("mac", "sessions.changed", params)
	}
}

func sessionsChangedOf() (*Service, []byte) {
	f := newFakeHub()
	d := newFleetDevs()
	d.add("mac", "Mac")
	s := New(Options{Hub: f, Logf: func(string, ...any) {}, Devices: d})
	list := make([]proto.Session, 300)
	for i := range list {
		list[i] = proto.Session{SessionID: fmt.Sprintf("s%03d", i), Cwd: "/code/app", Agent: "claude",
			Status: proto.StatusDone, PaneID: fmt.Sprintf("p%d", i), SpaceName: "app", Title: "a session",
			LastText: strings.Repeat("the last words of the turn ", 8)}
	}
	params, _ := json.Marshal(map[string]any{"sessions": list})
	d.setSessions("mac", list)
	return s, params
}

// The manager keeps of each list only what it reads again (#2472): about 995 KB an event measured, where copies of the
// previous list, the reply reminders' rows and the herdr marks' list made it 1.92 MB.
func TestSessionsChangedEventKeepsLittle(t *testing.T) {
	if raceEnabled {
		t.Skip("race instrumentation allocates")
	}
	if !isolated(t) {
		return
	}
	s, params := sessionsChangedOf()
	s.NodeEvent("mac", "sessions.changed", params)
	const runs = 200
	runtime.GC()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	for range runs {
		s.NodeEvent("mac", "sessions.changed", params)
	}
	runtime.ReadMemStats(&after)
	per := (after.TotalAlloc - before.TotalAlloc) / runs
	if per > 1025000 {
		t.Fatalf("one sessions.changed allocated %d bytes, want at most 1025000", per)
	}
}

// A sessions.changed for a machine the devices do not have is decoded from the event, as before; one they have is
// their list.
func TestSessionsChangedEventSource(t *testing.T) {
	ft := newFleetTestNoStore(t)
	handed := make(chan DeviceSessions, 2)
	ft.s.sessionsSeen = func(d DeviceSessions) { handed <- d }
	next := func() DeviceSessions {
		t.Helper()
		select {
		case d := <-handed:
			return d
		case <-time.After(5 * time.Second):
			t.Fatal("SessionsChanged was not handed the list")
			return DeviceSessions{}
		}
	}
	params, _ := json.Marshal(map[string]any{"sessions": []proto.Session{{SessionID: "x1"}, {SessionID: "x2"}}})
	ft.s.NodeEvent("elsewhere", "sessions.changed", params)
	if d := next(); d.MachineID != "elsewhere" || len(d.Sessions) != 2 || d.Sessions[1].SessionID != "x2" ||
		d.Sessions[0].MachineID != "elsewhere" || d.Sessions[1].MachineID != "elsewhere" {
		t.Fatalf("decoded: %+v", d)
	}
	if got := ft.s.fleetPrevious("elsewhere"); len(got) != 2 || !hasKeys(got, "x1", "x2") {
		t.Fatalf("decoded, kept: %+v", got)
	}
	ft.d.setSessions("mac", []proto.Session{{SessionID: "s1", Title: "kept"}})
	ft.s.NodeEvent("mac", "sessions.changed", params)
	if d := next(); len(d.Sessions) != 1 || d.Sessions[0].SessionID != "s1" || d.Sessions[0].Title != "kept" ||
		d.Sessions[0].MachineID != "mac" {
		t.Fatalf("the devices' list: %+v", d)
	}
	if got := ft.s.fleetPrevious("mac"); len(got) != 1 || !hasKeys(got, "s1") {
		t.Fatalf("the devices' list, kept: %+v", got)
	}
}

func hasKeys[V any](m map[string]V, keys ...string) bool {
	for _, k := range keys {
		if _, ok := m[k]; !ok {
			return false
		}
	}
	return true
}

// Of the decorated list told last, the manager keeps only the rows the reply reminders read: none when nobody waits
// for a reply, though the machine stays told (shownList does not decorate it again).
func TestShownKeepsOnlyRowsAwaitingReply(t *testing.T) {
	ft := newFleetTestNoStore(t)
	ft.sessions("mac", proto.Session{SessionID: "s1", Status: proto.StatusDone, LastText: "All tests pass."})
	ft.s.fleet.shownMu.Lock()
	kept, told := ft.s.fleet.shown["mac"]
	ft.s.fleet.shownMu.Unlock()
	if !told || kept == nil || len(kept) != 0 {
		t.Fatalf("kept %+v (told %v), want an empty list", kept, told)
	}
}

// decoratedOf is a manager whose machine "mac" lists 50 sessions, as a finished turn's push reads one of them.
func decoratedOf(tb testing.TB) (*Service, string) {
	tb.Helper()
	d := newFleetDevs()
	d.add("mac", "Mac")
	s := New(Options{Hub: newFakeHub(), Logf: func(string, ...any) {}, Devices: d})
	list := make([]proto.Session, 50)
	for i := range list {
		list[i] = proto.Session{SessionID: fmt.Sprintf("s%03d", i), Cwd: "/code/app", Agent: "claude", Status: proto.StatusDone,
			PaneID: fmt.Sprintf("p%d", i), Title: "a session", LastText: strings.Repeat("the last words of the turn ", 8)}
	}
	d.setSessions("mac", list)
	if got, ok := s.decoratedSession("mac", "s025"); !ok || got.SessionID != "s025" || got.State == "" {
		tb.Fatalf("decorated %+v (found %v)", got, ok)
	}
	return s, "s025"
}

func BenchmarkDecoratedSession(b *testing.B) {
	s, sid := decoratedOf(b)
	b.ReportAllocs()
	for b.Loop() {
		s.decoratedSession("mac", sid)
	}
}

// A finished turn's push decorates only its own row, not its machine's whole list (#2472): about 680 bytes a lookup
// measured, where decorating the 50 sessions was 85.7 KB.
func TestDecoratedSessionDecoratesOneRow(t *testing.T) {
	if raceEnabled {
		t.Skip("race instrumentation allocates")
	}
	if !isolated(t) {
		return
	}
	s, sid := decoratedOf(t)
	const runs = 2000
	runtime.GC()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	for range runs {
		s.decoratedSession("mac", sid)
	}
	runtime.ReadMemStats(&after)
	if per := (after.TotalAlloc - before.TotalAlloc) / runs; per > 700 {
		t.Fatalf("one decorated session allocated %d bytes, want at most 700", per)
	}
}

// raceEnabled is set by race_test.go: the race detector's instrumentation allocates, so allocation budgets skip.
var raceEnabled bool
