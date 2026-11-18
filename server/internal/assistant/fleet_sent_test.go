package assistant

import (
	"bytes"
	"context"
	"encoding/json"
	"slices"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/assistantwire"
	"github.com/nwverse/persona/server/internal/proto"
	"github.com/nwverse/persona/server/internal/rpc"
)

// toldLists are the fleet.sessions values the hub was told, in order.
func toldLists(h *lineHub) []rpc.Marshaled {
	h.mu.Lock()
	defer h.mu.Unlock()
	var out []rpc.Marshaled
	for i, e := range h.events {
		if e == assistantwire.EventFleetSessions {
			out = append(out, h.values[i].(rpc.Marshaled))
		}
	}
	return out
}

// The lists told with the digests are the ones told when each machine's last bytes were kept (the old way, modelled
// here): a list is told when its json.Marshal bytes differ from the machine's last told ones, as those bytes, and
// a hub reconnect (FleetUp) tells every list again. What was told stays as it was, though the buffer is reused.
func TestSentDigestsTellWhatTheBytesDid(t *testing.T) {
	s, d, h, ids, lists := sentFleet(t, 3, 40)
	kept := map[string][]byte{} // the old way: each machine's last bytes
	var want [][]byte
	publish := func(id string) {
		list := s.publishList(id)
		b, err := json.Marshal(assistantwire.FleetSessions{MachineID: id, Sessions: list, Muted: s.mutedSessions(id, list)})
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(b, kept[id]) {
			kept[id] = b
			want = append(want, b)
		}
	}
	steps := []struct {
		id   string
		list int // -1: the hub reconnects
	}{
		{"box00", 0}, {"box01", 0}, {"box02", 0}, // first lists
		{"box00", 0}, {"box01", 0}, // the same again: not told
		{"box00", 1}, {"box00", 1}, // a turn ended, told once
		{"box01", 2}, {"box00", 0}, // a session went; box00 back to its first list: told, it differs from its last
		{"", -1},                   // the hub reconnects: every list told again
		{"box02", 0}, {"box01", 2}, // unchanged since
		{"box02", 1}, {"box02", 2}, {"box02", 1}, // each differs from the one before
	}
	for i, st := range steps {
		if st.list < 0 {
			s.FleetUp(context.Background())
			clear(kept) // the old FleetUp dropped every machine's bytes
			for _, id := range ids {
				publish(id) // the old way's tells; here FleetUp told them, and these publishes tell nothing more
			}
		} else {
			d.setSessions(st.id, slices.Clone(lists[st.id][st.list]))
			publish(st.id)
		}
		got := toldLists(h)
		if len(got) != len(want) {
			t.Fatalf("step %d (%s): told %d lists, the old way %d", i, st.id, len(got), len(want))
		}
		for k := range got {
			if !bytes.Equal(got[k], want[k]) {
				t.Fatalf("step %d: list %d told\n%s\nthe old way\n%s", i, k, got[k], want[k])
			}
		}
	}
}

// A hub reconnect tells every machine's list, even unchanged, and each once.
func TestFleetUpTellsEveryListAgain(t *testing.T) {
	s, _, h, ids, _ := sentFleet(t, 20, 10)
	for _, id := range ids {
		s.publishSessions(id)
	}
	if n := h.toldOf(assistantwire.EventFleetSessions); n != len(ids) {
		t.Fatalf("told %d lists of %d machines", n, len(ids))
	}
	for _, id := range ids {
		s.publishSessions(id)
	}
	if n := h.toldOf(assistantwire.EventFleetSessions); n != len(ids) {
		t.Fatalf("unchanged lists told again: %d", n)
	}
	s.FleetUp(context.Background())
	got := toldLists(h)
	if len(got) != 2*len(ids) {
		t.Fatalf("after a reconnect told %d lists, want %d", len(got), 2*len(ids))
	}
	for i, id := range ids {
		if !bytes.Equal(got[len(ids)+i], got[i]) {
			t.Fatalf("%s told again as\n%s\nnot\n%s", id, got[len(ids)+i], got[i])
		}
	}
}

// A list that does not encode (a time past year 9999) is told as null once after a list that did; then the next list
// that encodes is told. A machine never told before whose list does not encode is told nothing: as json.Marshal's nil
// was handled.
func TestSentListThatDoesNotEncode(t *testing.T) {
	s, d, h, _, lists := sentFleet(t, 2, 3)
	bad := slices.Clone(lists["box00"][0])
	bad[1].UpdatedAt = time.Date(10000, 1, 1, 0, 0, 0, 0, time.UTC)
	if _, err := json.Marshal(bad); err == nil {
		t.Fatal("the list encodes")
	}
	steps := []struct {
		id   string
		list []proto.Session
		told string // "": nothing told; "null"; "list": the list
	}{
		{"box01", bad, ""},
		{"box00", lists["box00"][0], "list"},
		{"box00", bad, "null"},
		{"box00", bad, ""},
		{"box00", lists["box00"][0], "list"},
		{"box00", lists["box00"][0], ""},
	}
	for i, st := range steps {
		d.setSessions(st.id, slices.Clone(st.list))
		before := len(toldLists(h))
		s.publishSessions(st.id)
		got := toldLists(h)
		switch {
		case st.told == "" && len(got) != before:
			t.Fatalf("step %d: told %q", i, got[len(got)-1])
		case st.told != "" && len(got) != before+1:
			t.Fatalf("step %d: told %d lists, want one", i, len(got)-before)
		case st.told == "null" && got[before] != nil:
			t.Fatalf("step %d: told %q, want null", i, got[before])
		case st.told == "list" && !bytes.HasPrefix(got[before], []byte(`{"machine_id":"box00"`)):
			t.Fatalf("step %d: told %q", i, got[before])
		}
	}
}

// What is kept per machine is its sentList, and the one buffer holds at most the longest list told: the bytes of
// each machine's list are not kept (#3197).
func TestSentKeepsNoList(t *testing.T) {
	s, _, _, ids, _ := sentFleet(t, 20, 50)
	longest := 0
	for _, id := range ids {
		list := s.publishList(id)
		b, _ := json.Marshal(assistantwire.FleetSessions{MachineID: id, Sessions: list, Muted: s.mutedSessions(id, list)})
		longest = max(longest, len(b))
	}
	f := &s.fleet
	f.sentMu.Lock()
	defer f.sentMu.Unlock()
	if len(f.sent) != len(ids) {
		t.Fatalf("%d machines kept", len(f.sent))
	}
	for id, l := range f.sent {
		if l.n == 0 {
			t.Fatalf("%s kept nothing of its list", id)
		}
	}
	if c := f.enc.Cap(); c > 2*longest {
		t.Fatalf("the buffer holds %d bytes for lists of at most %d", c, longest)
	}
}
