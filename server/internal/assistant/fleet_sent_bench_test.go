package assistant

import (
	"fmt"
	"io"
	"runtime"
	"slices"
	"testing"

	"github.com/nwverse/persona/server/internal/assistantwire"
	"github.com/nwverse/persona/server/internal/proto"
)

// sentFleet is a manager with n machines of rows sessions each on a hub that keeps what it is told, and per machine
// three lists: a first one, the same with a turn ended, and the same with a session gone.
func sentFleet(tb testing.TB, n, rows int) (*Service, *fleetDevs, *lineHub, []string, map[string][3][]proto.Session) {
	tb.Helper()
	h := newLineHub(io.Discard, true)
	d := newFleetDevs()
	ids := make([]string, n)
	lists := map[string][3][]proto.Session{}
	for m := range n {
		id := fmt.Sprintf("box%02d", m)
		ids[m] = id
		d.add(id, "Computer "+id)
		first := make([]proto.Session, rows)
		for i := range first {
			first[i] = proto.Session{SessionID: fmt.Sprintf("s%03d", i), Cwd: "/code/app", Agent: "claude",
				Status: proto.StatusDone, PaneID: fmt.Sprintf("p%d", i), SpaceName: "app", Title: "a <b>session</b> & more",
				LastText: "the last words of the turn on " + id}
		}
		ended := slices.Clone(first)
		ended[rows/2].Status, ended[rows/2].LastText = proto.StatusWorking, "working on it"
		gone := slices.Delete(slices.Clone(first), 0, 1)
		lists[id] = [3][]proto.Session{first, ended, gone}
		d.setSessions(id, slices.Clone(first))
	}
	s := New(Options{Hub: h, Logf: func(string, ...any) {}, Devices: d})
	return s, d, h, ids, lists
}

// BenchmarkFleetSessionsKept: 20 computers of 200 sessions, each list told the hub once (publishList); then one
// computer's list changes (event=changed: a turn ends, then it starts again) or comes again unchanged (event=same) and
// is told, or not, each round. kept-B is what the lists told keep on the heap after a GC (each computer's last bytes,
// before #3197); the rest is per event.
func BenchmarkFleetSessionsKept(b *testing.B) {
	for _, event := range []string{"changed", "same"} {
		b.Run("event="+event, func(b *testing.B) {
			s, d, h, ids, lists := sentFleet(b, 20, 200)
			h.keep = false // what stays is the manager's
			before := heapNow()
			for _, id := range ids {
				s.publishSessions(id)
			}
			kept := heapNow() - before
			if got := h.toldOf(assistantwire.EventFleetSessions); got != len(ids) {
				b.Fatalf("told %d lists of %d", got, len(ids))
			}
			const id = "box07"
			n := 0
			b.ReportAllocs()
			for b.Loop() {
				if event == "changed" {
					d.setSessions(id, slices.Clone(lists[id][1-n&1]))
				}
				s.publishSessions(id)
				n++
			}
			want := len(ids) // the first lists
			if event == "changed" {
				want += n
			}
			if got := h.toldOf(assistantwire.EventFleetSessions); got != want {
				b.Fatalf("told %d lists in %d rounds, want %d", got, n, want)
			}
			b.ReportMetric(float64(kept), "kept-B")
			runtime.KeepAlive(s)
		})
	}
}

// heapNow is the live heap after a collection.
func heapNow() int64 {
	runtime.GC()
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	return int64(m.HeapAlloc)
}
