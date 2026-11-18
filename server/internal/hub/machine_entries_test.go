package hub

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/devicewire"
	"github.com/nwverse/persona/server/internal/proto"
)

// The owners of the tests below: lap is m-0a0a0a's, every other machine the carrier's.
func lapMovedOwners() *ownersStore {
	return &ownersStore{kept: map[string]json.RawMessage{machineOwnersKey: json.RawMessage(`{"lap":"m-0a0a0a"}`)}}
}

func keptEntries(t *testing.T, st Store) reportedMachines {
	t.Helper()
	var r reportedMachines
	if _, err := st.Setting(context.Background(), machineEntriesKey, &r); err != nil {
		t.Fatal(err)
	}
	return r
}

// The hub keeps what each manager reported of its own machines: from its report's entries and states (a manager from
// before sends no entry: the state's, else the one the hub handed it), an absorbed entry too, without secrets.
// Another manager's machine it names is refused and logged once. A machine connected as it is written is seen then; a
// report without the computer's description keeps the last one. A hub that restarts without the warm hand-over (it
// crashed, or was stopped for long) shows every manager's machines as last reported: offline, what they are, when
// last seen; what it reads is without the machines and managers that are gone. A move hands the entry over; a removed
// manager's go to the carrier; a removed machine is dropped; a removed manager's late report is not kept.
func TestTheHubKeepsWhatEachManagerReported(t *testing.T) {
	ctx := context.Background()
	st := newMeshStore()
	machines := []MachineConfig{{ID: "box", Name: "Box", Kind: "ssh", SSHHost: "box.example"}, {ID: "lap", Name: "Lap", Kind: kindLink},
		{ID: "alias", Name: "Alias", Kind: "ssh", SSHHost: "box.example"}, {ID: "old", Name: "Old", Kind: "ssh"}}
	var mu sync.Mutex
	var logs bytes.Buffer
	newHub := func() *Hub {
		h := newTestHub(t)
		h.store = st
		h.bus.idents = lapMovedOwners()
		h.cfg.Machines = machines
		h.logger = log.New(lockedWriter{&mu, &logs}, "", 0)
		return h
	}
	refusals := func() int {
		mu.Lock()
		defer mu.Unlock()
		return strings.Count(logs.String(), "not that manager's machine now")
	}

	h := newHub()
	h.startDevices(ctx)
	began := time.Now().UTC().Add(-time.Second)
	seenOld := time.Now().Add(-3 * time.Hour).UTC().Truncate(time.Second)
	boxCfg := `{"id":"box","name":"Box","kind":"ssh","ssh_host":"box.example","tailcat_addr":"tc-s3cret",` +
		`"proxy":"socks5://me:s3cret@jump.example:1080"}`
	oldState := devicewire.State{Machine: proto.Machine{ID: "old", Name: "Old", Kind: "ssh", LastSeen: seenOld},
		Info: proto.NodeInfo{Hostname: "old-host", OS: "linux", Version: "v0"}}
	carrier := devicewire.States{
		Machines: []devicewire.State{{Machine: proto.Machine{ID: "box", Name: "Box", Kind: "ssh", Online: true},
			Config: json.RawMessage(boxCfg), Info: proto.NodeInfo{Hostname: "box-host", OS: "linux", Version: "v1"},
			Connected: true, ConnectedAt: time.Now()}, oldState},
		Absorbed: map[string]string{"alias": "box"},
		Entries: []devicewire.RecMachine{{ID: "box", Kind: "ssh"}, {ID: "old", Kind: "ssh"},
			{ID: "alias", Kind: "ssh", Cfg: json.RawMessage(`{"id":"alias","name":"Alias","kind":"ssh","ssh_host":"box.example"}`)},
			{ID: "lap", Kind: kindLink, Cfg: json.RawMessage(`{"id":"lap","name":"Not the carrier's"}`)}},
	}
	h.applyDeviceStatesFrom(devicesOwner, carrier)
	other := devicewire.States{
		Machines: []devicewire.State{
			{Machine: proto.Machine{ID: "lap", Name: "Lap", Kind: kindLink, Online: true},
				Info: proto.NodeInfo{Hostname: "lap-host", OS: "darwin", Version: "v1"}, Connected: true, ConnectedAt: time.Now()},
			{Machine: proto.Machine{ID: "box", Name: "Box", Kind: "ssh", Online: true}, Info: proto.NodeInfo{Hostname: "elsewhere"}}},
		Entries: []devicewire.RecMachine{{ID: "lap", Kind: kindLink}},
	}
	h.applyDeviceStatesFrom("m-0a0a0a", other)
	h.applyDeviceStatesFrom("m-0a0a0a", other)
	// old's node is not described this time (the manager restarted): what it last was stays.
	carrier.Machines[1].Info = proto.NodeInfo{}
	h.applyDeviceStatesFrom(devicesOwner, carrier)
	waitFor(t, "the entries written", func() bool {
		r := keptEntries(t, st)
		return len(r["m-0a0a0a"]) == 1 && len(r[devicesOwner]) == 3
	})
	waitFor(t, "the writes done", func() bool { return entriesSettled(h) })
	r := keptEntries(t, st)
	if _, ok := r[devicesOwner]["lap"]; ok {
		t.Fatalf("the carrier's report of m-0a0a0a's machine was kept: %+v", r[devicesOwner])
	}
	if _, ok := r["m-0a0a0a"]["box"]; ok {
		t.Fatalf("m-0a0a0a's report of the carrier's machine was kept: %+v", r["m-0a0a0a"])
	}
	var boxKept MachineConfig
	if box := r[devicesOwner]["box"]; json.Unmarshal(box.Cfg, &boxKept) != nil || boxKept.SSHHost != "box.example" || boxKept.TailcatAddr != "" ||
		boxKept.Proxy != "socks5://me@jump.example:1080" || strings.Contains(string(box.Cfg), "s3cret") ||
		box.Info.Hostname != "box-host" || box.LastSeen.Before(began) {
		t.Fatalf("box as kept (its state's entry without its secrets, connected: seen now): %s %+v %v", box.Cfg, box.Info, box.LastSeen)
	}
	if old := r[devicesOwner]["old"]; old.Info.Hostname != "old-host" || !old.LastSeen.Equal(seenOld) {
		t.Fatalf("old as kept (as last described and seen): %+v %v", old.Info, old.LastSeen)
	}
	if alias := r[devicesOwner]["alias"]; !strings.Contains(string(alias.Cfg), `"Alias"`) || alias.Info.Hostname != "" {
		t.Fatalf("the absorbed entry as kept: %s %+v", alias.Cfg, alias.Info)
	}
	var lapCfg MachineConfig
	if lap := r["m-0a0a0a"]["lap"]; json.Unmarshal(lap.Cfg, &lapCfg) != nil || lapCfg.Name != "Lap" || lapCfg.Kind != kindLink ||
		lap.Info.Hostname != "lap-host" {
		t.Fatalf("lap as kept (the entry the hub handed over): %s %+v", lap.Cfg, lap.Info)
	}
	if n := refusals(); n != 2 {
		t.Fatalf("refusals logged %d times, want each once:\n%s", n, logs.String())
	}

	// m-0a0a0a goes away (lap seen until then); the hub stops without handing over (a crash). Meanwhile a manager
	// left the roster and a machine left hub.json: the next hub reads neither.
	gone := time.Now().UTC().Add(-time.Second)
	h.devicesGone("m-0a0a0a")
	waitFor(t, "the first hub's writes done", func() bool { return entriesSettled(h) })
	r = keptEntries(t, st)
	r["x-left"] = map[string]reportedEntry{"box": {Cfg: json.RawMessage(`{"id":"box"}`)}}
	r[devicesOwner]["ghost"] = reportedEntry{Cfg: json.RawMessage(`{"id":"ghost"}`)}
	_ = st.PutSetting(ctx, machineEntriesKey, r)
	next := newHub()
	next.startDevices(ctx)
	for id, want := range map[string]struct {
		host, owner string
		seen        func(time.Time) bool
	}{
		"box": {"box-host", devicesOwner, func(at time.Time) bool { return !at.Before(began) }},
		"lap": {"lap-host", "m-0a0a0a", func(at time.Time) bool { return !at.Before(gone) }},
		"old": {"old-host", devicesOwner, func(at time.Time) bool { return at.Equal(seenOld) }},
	} {
		m := next.machineByID(id)
		if m == nil {
			t.Fatalf("%s not shown", id)
		}
		if s := m.snapshot(); s.Online || s.Hostname != want.host || !want.seen(s.LastSeen) || m.ownerOrCarrier() != want.owner {
			t.Fatalf("%s after the restart: online %v host %q seen %v owner %s", id, s.Online, s.Hostname, s.LastSeen, m.ownerOrCarrier())
		}
	}
	next.devices.mu.Lock()
	_, left := next.devices.reported["x-left"]
	_, ghost := next.devices.reported[devicesOwner]["ghost"]
	next.devices.mu.Unlock()
	if left || ghost {
		t.Fatal("read with a manager the roster does not have, or a machine hub.json does not have")
	}

	// old moves to m-0b0b0b, which has not reported it; m-0a0a0a is removed (lap back to the carrier); box is removed.
	if _, err := next.moveMachine(ctx, "old", "m-0b0b0b"); err != nil {
		t.Fatal(err)
	}
	next.releaseMachines(ctx, "m-0a0a0a")
	next.forgetManagerState("m-0a0a0a")
	next.applyDeviceStatesFrom("m-0a0a0a", other) // a report it sent before it was hung up
	next.removeMachineLive("box")
	waitFor(t, "the entries handed over and dropped", func() bool {
		if !entriesSettled(next) {
			return false
		}
		r := keptEntries(t, st)
		_, box := r[devicesOwner]["box"]
		_, removed := r["m-0a0a0a"]
		_, oldLeft := r[devicesOwner]["old"]
		return !box && !removed && !oldLeft && r["m-0b0b0b"]["old"].Info.Hostname == "old-host" &&
			r[devicesOwner]["lap"].Info.Hostname == "lap-host" && r[devicesOwner]["alias"].Cfg != nil
	})
}

// entriesSettled: no write of what the managers reported is due or under way.
func entriesSettled(h *Hub) bool {
	d := &h.devices
	d.reportedSave.Lock()
	defer d.reportedSave.Unlock()
	d.mu.Lock()
	defer d.mu.Unlock()
	return !d.reportedDue
}

// entriesFailStore fails the first reads of what the managers reported.
type entriesFailStore struct {
	*meshStore
	mu    sync.Mutex
	fails int
	reads int
}

func (s *entriesFailStore) Setting(ctx context.Context, key string, out any) (bool, error) {
	if key == machineEntriesKey {
		s.mu.Lock()
		s.reads++
		fail := s.fails > 0
		if fail {
			s.fails--
		}
		s.mu.Unlock()
		if fail {
			return false, errors.New("database restarting")
		}
	}
	return s.meshStore.Setting(ctx, key, out)
}

func (s *entriesFailStore) readCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.reads
}

// A hub that could not read what the managers reported as it started reads it before it writes: the managers that
// did not report since stay. Still unreadable, nothing is written.
func TestMachineEntriesReadLateKeepTheOthers(t *testing.T) {
	ctx := context.Background()
	st := &entriesFailStore{meshStore: newMeshStore(), fails: 2}
	before := reportedMachines{
		"m-0b0b0b":   {"far": {Cfg: json.RawMessage(`{"id":"far"}`), Info: proto.NodeInfo{Hostname: "far-host"}}},
		devicesOwner: {"box": {Cfg: json.RawMessage(`{"id":"box"}`), Info: proto.NodeInfo{Hostname: "old-host"}}},
	}
	_ = st.meshStore.PutSetting(ctx, machineEntriesKey, before)
	h := newTestHub(t)
	h.store = st
	h.bus.idents = &ownersStore{kept: map[string]json.RawMessage{machineOwnersKey: json.RawMessage(`{"far":"m-0b0b0b"}`)}}
	h.cfg.Machines = []MachineConfig{{ID: "box", Name: "Box", Kind: "ssh"}, {ID: "far", Name: "Far", Kind: "ssh"}}
	h.startDevices(ctx) // the read fails
	h.applyDeviceStatesFrom(devicesOwner, devicewire.States{
		Machines: []devicewire.State{{Machine: proto.Machine{ID: "box", Name: "Box", Kind: "ssh", Online: true},
			Info: proto.NodeInfo{Hostname: "new-host"}, Connected: true, ConnectedAt: time.Now()}},
		Entries: []devicewire.RecMachine{{ID: "box", Kind: "ssh", Cfg: json.RawMessage(`{"id":"box","name":"Box","kind":"ssh"}`)}},
	})
	waitFor(t, "the write the report asked for", func() bool { return entriesSettled(h) }) // its read fails: nothing written
	if r := keptEntries(t, st.meshStore); r[devicesOwner]["box"].Info.Hostname != "old-host" {
		t.Fatalf("written over without reading: %+v", r)
	}
	// box moves to m-0b0b0b, which has not reported since: its far stays when the setting is read late.
	if _, err := h.moveMachine(ctx, "box", "m-0b0b0b"); err != nil {
		t.Fatal(err)
	}
	h.saveMachineEntries(ctx)
	r := keptEntries(t, st.meshStore)
	if r["m-0b0b0b"]["far"].Info.Hostname != "far-host" || r["m-0b0b0b"]["box"].Info.Hostname != "new-host" {
		t.Fatalf("after the late read: %+v", r)
	}
}

// The warm start ends for every manager's machines: one manager back does not keep another's shown as before.
func TestWarmStartEndsForEveryManager(t *testing.T) {
	ctx := context.Background()
	h := newTestHub(t)
	st := newMeshStore()
	h.store = st
	h.bus.idents = lapMovedOwners()
	h.cfg.Machines = []MachineConfig{{ID: "box", Name: "Box", Kind: "ssh"}, {ID: "lap", Name: "Lap", Kind: "ssh"}}
	_ = st.PutSetting(ctx, devicesWarmKey, map[string]any{"saved_at": time.Now(), "machines": map[string]any{
		"box": map[string]any{"info": proto.NodeInfo{Hostname: "box"}, "sessions": []proto.Session{{SessionID: "s1", Status: proto.StatusIdle}}},
		"lap": map[string]any{"info": proto.NodeInfo{Hostname: "lap"}, "sessions": []proto.Session{{SessionID: "s2", Status: proto.StatusIdle}}},
	}})
	h.startDevices(ctx)
	lap := h.machineByID("lap")
	if !lap.snapshot().Online || lap.ownerOrCarrier() != "m-0a0a0a" {
		t.Fatalf("lap before the managers: %+v", lap.snapshot())
	}
	h.applyDeviceStatesFrom(devicesOwner, devicewire.States{Machines: []devicewire.State{{
		Machine: proto.Machine{ID: "box", Name: "Box", Kind: "ssh", Online: true}, Connected: true, ConnectedAt: time.Now()}}})
	h.devicesWarmEnded()
	if lap.snapshot().Online {
		t.Fatal("m-0a0a0a's machine still shown as before the restart")
	}
	if ss := lap.sessionList(); len(ss) != 1 || ss[0].Status != proto.StatusUnknown {
		t.Fatalf("its sessions: %+v", ss)
	}
	if !h.machineByID("box").snapshot().Online {
		t.Fatal("the carrier's machine went offline with it")
	}
}

// A mesh record says how each computer's own manager reaches it: a machine moved to another manager is reachable
// through that one, whatever the carrier says of it.
func TestRecordReachIsTheOwnersReport(t *testing.T) {
	h := newTestHub(t)
	h.bus.idents = lapMovedOwners()
	h.cfg.Machines = []MachineConfig{{ID: "box", Name: "Box", Kind: "ssh"}, {ID: "lap", Name: "Lap", Kind: "ssh"}}
	h.startDevices(context.Background())
	h.applyDeviceStatesFrom(devicesOwner, devicewire.States{
		Machines: []devicewire.State{{Machine: proto.Machine{ID: "box", Name: "Box", Kind: "ssh", Online: true}, Connected: true}},
		Entries:  []devicewire.RecMachine{{ID: "box", Kind: "ssh", Avail: true, RTTMS: 3}, {ID: "lap", Kind: "ssh", Avail: true, RTTMS: 99}},
	})
	h.applyDeviceStatesFrom("m-0a0a0a", devicewire.States{
		Machines: []devicewire.State{{Machine: proto.Machine{ID: "lap", Name: "Lap", Kind: "ssh", Online: true}, Connected: true}},
		Entries:  []devicewire.RecMachine{{ID: "lap", Kind: "ssh", Avail: true, RTTMS: 7}},
	})
	got := map[string]recMachine{}
	for _, r := range h.recordEntries() {
		got[r.ID] = r
	}
	if !got["box"].Avail || got["box"].RTTMS != 3 || !got["lap"].Avail || got["lap"].RTTMS != 7 {
		t.Fatalf("the record: %+v", got)
	}
	// m-0a0a0a goes away: the hub cannot reach lap through it now.
	h.devicesGone("m-0a0a0a")
	got = map[string]recMachine{}
	for _, r := range h.recordEntries() {
		got[r.ID] = r
	}
	if !got["box"].Avail || got["lap"].Avail {
		t.Fatalf("the record with m-0a0a0a away: %+v", got)
	}
}
