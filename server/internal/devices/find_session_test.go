package devices

import (
	"slices"
	"testing"

	"github.com/nwverse/persona/server/internal/proto"
)

// FindSession reads the sessions of the machine Views lists with the id, as Views' copies of them were read before
// #3197: the same answer for each listed session and for none, without copying a session.
func TestFindSessionReadsTheListedMachine(t *testing.T) {
	d := ListedMachinesWithSessions(t, 4, 30)
	for _, v := range d.Views() {
		m := d.listed(v.ID)
		old := m.sessionList() // what the view carried
		for _, sid := range []string{old[0].SessionID, old[29].SessionID, "soak01-s000", "nowhere"} {
			want := slices.ContainsFunc(old, func(s proto.Session) bool { return s.SessionID == sid })
			if got := d.FindSession(v.ID, func(s *proto.Session) bool { return s.SessionID == sid }); got != want {
				t.Fatalf("%s: found %s %v, the view's copy says %v", v.ID, sid, got, want)
			}
		}
		seen := 0
		d.FindSession(v.ID, func(s *proto.Session) bool { seen++; return s.SessionID == old[4].SessionID })
		if seen != 5 {
			t.Fatalf("%s: read %d sessions to find the fifth", v.ID, seen)
		}
	}
	if d.FindSession("nowhere", func(*proto.Session) bool { return true }) {
		t.Fatal("a machine that is not listed has sessions")
	}
	if n := testing.AllocsPerRun(20, func() { d.FindSession("soak02", func(*proto.Session) bool { return false }) }); n != 0 {
		t.Fatalf("a lookup allocates %v times", n)
	}
}
