package assistant

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"sort"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/assistantwire"
	"github.com/nwverse/persona/server/internal/proto"
)

// overlayBefore is overlayAgentArchives as it was before the archive's rows were indexed (#6836): every call went
// through every record and appended its row to a list that grew as it went. Kept to prove the whole list unchanged.
func overlayBefore(machine string, list []proto.Session, records map[string]agentArchive, pending map[string]bool, mark uint64) []proto.Session {
	seen := make(map[string]bool, len(list))
	for i := range list {
		key := runtimeListID(machine, list[i].SessionID)
		seen[key] = true
		if rec, ok := records[key]; ok && !list[i].Assistant {
			active := rec.active(list[i], mark)
			list[i].Archived = rec.Archived && !pending[key] && !active
			list[i].ArchiveChangedAt = archiveChangedAt(rec.ChangedAt)
		}
	}
	keys := make([]string, 0, len(records))
	for key, rec := range records {
		if !seen[key] && rec.Session.MachineID == machine {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	for _, key := range keys {
		list = append(list, archiveRow(records[key]))
	}
	return list
}

// apartService is a manager whose machine "mac" lists twelve idle sessions, three of them with an archive record
// (two archived, one restored), and whose archive keeps six more rows the node no longer lists (four archived, two
// restored).
func apartService(t *testing.T) (*fleetTest, []proto.Session) {
	t.Helper()
	ft := newFleetTest(t)
	at := time.Now().Add(-48 * time.Hour).UTC().Truncate(time.Second)
	rows := make([]proto.Session, 12)
	for i := range rows {
		rows[i] = proto.Session{MachineID: "mac", SessionID: fmt.Sprintf("listed-%02d", i), Agent: "claude",
			Status: proto.StatusIdle, State: proto.StateIdle, Title: fmt.Sprintf("Task %d", i), UpdatedAt: at}
	}
	records := map[string]agentArchive{}
	put := func(s proto.Session, archived bool) {
		records[runtimeListID("mac", s.SessionID)] = agentArchive{Session: archiveSnapshot(s), Archived: archived, ChangedAt: at.Add(time.Hour)}
	}
	put(rows[1], true)
	put(rows[2], false)
	put(rows[3], true)
	for i := range 6 {
		put(proto.Session{MachineID: "mac", SessionID: fmt.Sprintf("old-%02d", i), Agent: "codex", Title: fmt.Sprintf("Old %d", i),
			UpdatedAt: at.Add(-time.Duration(i+3) * 24 * time.Hour)}, i%3 != 0)
	}
	if err := ft.st.PutSetting(context.Background(), agentArchivesKey, records); err != nil {
		t.Fatal(err)
	}
	ft.d.setSessions("mac", rows)
	return ft, rows
}

// wholeBefore is the machine's whole list the way it was made before #6836, folded as decorated folds it.
func wholeBefore(t *testing.T, s *Service, machine string) []proto.Session {
	t.Helper()
	mark := s.archiveMark()
	var records map[string]agentArchive
	if _, err := s.archiveStore().Setting(context.Background(), agentArchivesKey, &records); err != nil {
		t.Fatal(err)
	}
	list := overlayBefore(machine, s.decoratedUnarchived(machine), records, nil, mark)
	s.foldInventory(machine, list)
	return list
}

func sameRowsJSON(t *testing.T, what string, got, want any) {
	t.Helper()
	g, _ := json.Marshal(got)
	w, _ := json.Marshal(want)
	if string(g) != string(w) {
		t.Fatalf("%s:\n got %s\nwant %s", what, g, w)
	}
}

// The whole list made from the archive's index is the one made before, byte for byte, when the node's list changes
// in other rows (the index is reused), when the node lists a row only the archive had and stops listing it again, and
// when a record changes (the index is made again).
func TestArchiveIndexKeepsTheWholeList(t *testing.T) {
	ft, rows := apartService(t)
	check := func(what string) {
		t.Helper()
		want := wholeBefore(t, ft.s, "mac")
		sameRowsJSON(t, what, ft.s.decorated("mac"), want)
	}
	check("first")
	rows[5].Title = "Renamed"
	ft.d.setSessions("mac", rows)
	check("another row changed")
	with := append(append([]proto.Session{}, rows...), proto.Session{MachineID: "mac", SessionID: "old-01", Agent: "codex",
		Status: proto.StatusIdle, State: proto.StateIdle, Title: "Old 1"})
	ft.d.setSessions("mac", with)
	check("the node lists a row only the archive had")
	ft.d.setSessions("mac", rows)
	check("and stops listing it")
	if w := archiveRequest(ft.s, "mac", "listed-07", roleFull, true); w.Code != http.StatusOK {
		t.Fatalf("archive: %d %s", w.Code, w.Body)
	}
	check("a record written")
	if n := len(ft.s.agentArchives.tails); n != 1 {
		t.Fatalf("%d machines indexed", n)
	}
}

// fleetLists are the fleet.sessions and fleet.archived the hub was told since n, in order.
func fleetLists(t *testing.T, f *fakeHub, n int) (events []string, sessions []assistantwire.FleetSessions, archived []assistantwire.FleetArchived) {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, e := range f.told[n:] {
		switch e.event {
		case assistantwire.EventFleetSessions:
			var p assistantwire.FleetSessions
			if err := json.Unmarshal(e.params, &p); err != nil {
				t.Fatal(err)
			}
			sessions = append(sessions, p)
		case assistantwire.EventFleetArchived:
			var p assistantwire.FleetArchived
			if err := json.Unmarshal(e.params, &p); err != nil {
				t.Fatal(err)
			}
			archived = append(archived, p)
		default:
			continue
		}
		events = append(events, e.event)
	}
	return events, sessions, archived
}

func toldSince(f *fakeHub) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.told)
}

// hubShown is what the hub gives the apps of a whole list (Hub.shown without deletions): every row but the archived
// ones that are not the assistant's.
func hubShown(list []proto.Session) (shown, archived []proto.Session) {
	for _, s := range list {
		if s.Archived && !s.Assistant {
			archived = append(archived, s)
		} else {
			shown = append(shown, s)
		}
	}
	return shown, archived
}

// rowsByID is rows by session id, for comparing sets.
func rowsByID(rows []proto.Session) map[string]proto.Session {
	out := map[string]proto.Session{}
	for _, s := range rows {
		out[s.SessionID] = s
	}
	return out
}

// Told apart, the list is what the hub showed the apps of the whole list, in the same order, and fleet.archived is
// exactly the rows it left out; the archived rows come first, so a session just archived is never in neither.
func TestApartIsWhatTheHubShowed(t *testing.T) {
	ft, _ := apartService(t)
	n := toldSince(ft.f)
	ft.s.publishList("mac")
	_, whole, _ := fleetLists(t, ft.f, n)
	if len(whole) != 1 {
		t.Fatalf("%d lists told", len(whole))
	}
	wantShown, wantArchived := hubShown(whole[0].Sessions)
	if len(wantArchived) != 6 || len(wantShown) != 12 {
		t.Fatalf("the fixture: %d shown, %d archived", len(wantShown), len(wantArchived))
	}

	ft.f.mu.Lock()
	ft.f.hello.ArchivedApart = true
	ft.f.mu.Unlock()
	ft.s.fleet.sentMu.Lock()
	ft.s.fleet.sent, ft.s.fleet.archived = nil, nil // a new connection (FleetUp)
	ft.s.fleet.sentMu.Unlock()
	n = toldSince(ft.f)
	ft.s.publishList("mac")
	events, lists, archived := fleetLists(t, ft.f, n)
	if !reflect.DeepEqual(events, []string{assistantwire.EventFleetArchived, assistantwire.EventFleetSessions}) {
		t.Fatalf("told %v", events)
	}
	sameRowsJSON(t, "the list told apart", lists[0].Sessions, wantShown)
	sameRowsJSON(t, "the archived rows", rowsByID(archived[0].Sessions), rowsByID(wantArchived))
	if archived[0].MachineID != "mac" {
		t.Fatalf("machine %q", archived[0].MachineID)
	}
}

// fleet.archived is told when the archived rows change, and once more on a new connection; a list that changes in
// its visible rows does not tell it again.
func TestArchivedToldWhenTheyChange(t *testing.T) {
	ft, rows := apartService(t)
	ft.f.mu.Lock()
	ft.f.hello.ArchivedApart = true
	ft.f.mu.Unlock()
	step := func(what string, change func(), wantEvents []string) []assistantwire.FleetArchived {
		t.Helper()
		n := toldSince(ft.f)
		change()
		events, _, archived := fleetLists(t, ft.f, n)
		if !reflect.DeepEqual(events, wantEvents) {
			t.Fatalf("%s: told %v, want %v", what, events, wantEvents)
		}
		return archived
	}
	both := []string{assistantwire.EventFleetArchived, assistantwire.EventFleetSessions}
	list := []string{assistantwire.EventFleetSessions}
	step("first", func() { ft.s.publishList("mac") }, both)
	step("a visible row changed", func() {
		rows[5].Title = "Renamed"
		ft.d.setSessions("mac", rows)
		ft.s.publishList("mac")
	}, list)
	got := step("a listed session archived", func() {
		if w := archiveRequest(ft.s, "mac", "listed-07", roleFull, true); w.Code != http.StatusOK {
			t.Fatalf("archive: %d %s", w.Code, w.Body)
		}
	}, both)
	if _, ok := rowsByID(got[0].Sessions)["listed-07"]; !ok || len(got[0].Sessions) != 7 {
		t.Fatalf("archived rows after archiving: %d", len(got[0].Sessions))
	}
	got = step("restored", func() {
		if w := archiveRequest(ft.s, "mac", "listed-07", roleFull, false); w.Code != http.StatusOK {
			t.Fatalf("restore: %d %s", w.Code, w.Body)
		}
	}, both)
	if _, ok := rowsByID(got[0].Sessions)["listed-07"]; ok || len(got[0].Sessions) != 6 {
		t.Fatalf("archived rows after restoring: %d", len(got[0].Sessions))
	}
	step("nothing changed", func() { ft.s.publishList("mac") }, nil)
	step("a new connection", func() {
		ft.s.fleet.sentMu.Lock()
		ft.s.fleet.sent, ft.s.fleet.archived = nil, nil
		ft.s.fleet.sentMu.Unlock()
		ft.s.publishList("mac")
	}, both)
}
