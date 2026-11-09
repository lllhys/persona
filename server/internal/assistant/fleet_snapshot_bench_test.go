package assistant

import (
	"fmt"
	"runtime"
	"slices"
	"testing"

	"github.com/nwverse/persona/server/internal/proto"
)

// BenchmarkFleetSnapshot: the live fleet the assistant computes after every session change (fleet_live.go), for one
// machine listing 300 sessions.
func BenchmarkFleetSnapshot(b *testing.B) {
	s := fleetOf300(b.Logf)
	b.ReportAllocs()
	for b.Loop() {
		s.fleetSnapshot()
	}
}

// fleetOf300 is a manager whose one machine lists 300 finished sessions.
func fleetOf300(logf func(string, ...any)) *Service {
	f := newFakeHub()
	d := newFleetDevs()
	d.add("mac", "Mac")
	s := New(Options{Hub: f, Logf: logf, Devices: d})
	list := make([]proto.Session, 300)
	for i := range list {
		list[i] = proto.Session{SessionID: fmt.Sprintf("s%03d", i), Cwd: "/code/app", Agent: "claude",
			Status: proto.StatusDone, PaneID: fmt.Sprintf("p%d", i), SpaceName: "app", Title: "a session", LastText: "done"}
	}
	d.setSessions("mac", list)
	return s
}

// The fleet of 300 sessions reads them where the machine's decorated list has them and writes out only the three
// items it shows: 56 allocations and 502 KB measured (3,066 and 3.06 MB in BenchmarkFleetSnapshot when each agent was
// a copy of its session and every one got its item). Most of what is left is the decorated copy of the list itself.
func TestFleetSnapshotAllocations(t *testing.T) {
	if raceEnabled {
		t.Skip("race instrumentation allocates")
	}
	if !isolated(t) {
		return
	}
	s := fleetOf300(func(string, ...any) {})
	if st := s.fleetSnapshot(); st.Unread != 300 || len(st.Items) != fleetMaxItems {
		t.Fatalf("fleet %+v", st)
	}
	if n := testing.AllocsPerRun(20, func() { s.fleetSnapshot() }); n > 57 {
		t.Errorf("a fleet of 300 sessions allocated %v times, want at most 57", n)
	}
	const runs = 20
	runtime.GC()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	for range runs {
		s.fleetSnapshot()
	}
	runtime.ReadMemStats(&after)
	if per := (after.TotalAlloc - before.TotalAlloc) / runs; per > 517000 {
		t.Errorf("a fleet of 300 sessions allocated %d bytes, want at most 517000", per)
	}
}

// The live fleet leaves out an agent whose space is muted, keeps one turned back on in it and one elsewhere: the
// session's own space decides, as in the node's row.
func TestFleetSnapshotMutedSpace(t *testing.T) {
	ft := newFleetTest(t)
	ft.mutes(Pins{Spaces: []string{"mac/app"}, Sessions: []string{}, Allowed: []string{"mac/s2"}})
	ft.d.setSessions("mac", []proto.Session{
		{SessionID: "s1", Cwd: "/code/app", Agent: "claude", Status: proto.StatusWorking, PaneID: "p1", SpaceName: "app"},
		{SessionID: "s2", Cwd: "/code/app", Agent: "claude", Status: proto.StatusWorking, PaneID: "p2", SpaceName: "app"},
		{SessionID: "s3", Cwd: "/code/web", Agent: "claude", Status: proto.StatusWorking, PaneID: "p3", SpaceName: "web"},
	})
	st := ft.s.fleetSnapshot()
	var ids []string
	for _, it := range st.Items {
		ids = append(ids, it.ID)
	}
	slices.Sort(ids)
	if !slices.Equal(ids, []string{"mac/s2", "mac/s3"}) {
		t.Fatalf("items %v", ids)
	}
}
