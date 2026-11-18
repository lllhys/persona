package extmgr

import (
	"encoding/json"
	"fmt"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/devices"
	"github.com/nwverse/persona/server/internal/proto"
	"github.com/nwverse/persona/server/internal/streams"
)

// mirrorDevices are the devices as the extensions manager reads them (devices.Devices: Views, FindSession), each
// computer with its sessions as its node listed them.
type mirrorDevices struct {
	Devices // Has, CallNode: not called here
	mu      sync.Mutex
	ids     []string
	names   map[string]string
	lists   map[string][]proto.Session
}

func (d *mirrorDevices) Views() []devices.View {
	d.mu.Lock()
	defer d.mu.Unlock()
	out := make([]devices.View, 0, len(d.ids))
	for _, id := range d.ids {
		out = append(out, devices.View{Summary: devices.Summary{ID: id, Name: d.names[id], Kind: "ssh", SSHHost: id,
			Online: true}, Info: proto.NodeInfo{OS: "linux", Arch: "amd64"}})
	}
	return out
}

func (d *mirrorDevices) FindSession(id string, match func(*proto.Session) bool) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	if !slices.Contains(d.ids, id) {
		return false
	}
	list := d.lists[id]
	for i := range list {
		if match(&list[i]) {
			return true
		}
	}
	return false
}

// views are the devices' views as the mirror got them before #3197: with a copy of each computer's sessions.
func (d *mirrorDevices) oldViews() []oldView {
	d.mu.Lock()
	defer d.mu.Unlock()
	var out []oldView
	for _, id := range d.ids {
		out = append(out, oldView{id: id, sessions: slices.Clone(d.lists[id])})
	}
	return out
}

type oldView struct {
	id       string
	sessions []proto.Session
}

// The readers as they read the mirror's copies before #3197 (computer_lcu.go sessionHome, hubglue.go askerCwd,
// extension_autoupgrade.go extensionBusy).

func oldSessionHome(views []oldView, id string) string {
	if id == "" {
		return ""
	}
	for _, v := range views {
		for _, s := range v.sessions {
			if s.SessionID == id {
				return v.id
			}
		}
	}
	return ""
}

func oldAskerCwd(views []oldView, by asker) string {
	if by.session == "" || by.machine == "" {
		return ""
	}
	// h.byID: the last view with the id (machine ids are unique in Views).
	for i := len(views) - 1; i >= 0; i-- {
		if views[i].id != by.machine {
			continue
		}
		for _, s := range views[i].sessions {
			if s.SessionID == by.session {
				return s.Cwd
			}
		}
		return ""
	}
	return ""
}

func oldAssistantWorking(views []oldView) bool {
	for _, v := range views {
		for _, s := range v.sessions {
			if s.Assistant && (s.State == proto.StateWorking || s.Status == proto.StatusWorking) {
				return true
			}
		}
	}
	return false
}

// The mirror keeps no sessions: its three readers ask the devices and answer what they answered from the mirror's
// copies.
func TestMirrorReadersMatchTheOldPath(t *testing.T) {
	h, _ := extensionTestHub(t)
	d := &mirrorDevices{ids: []string{"box", "mac", "pi"}, names: map[string]string{"box": "Box", "mac": "Mac", "pi": "Pi"},
		lists: map[string][]proto.Session{
			"box": {{SessionID: "a", Cwd: "/code/a"}, {SessionID: "shared", Cwd: "/code/box"}},
			"mac": {{SessionID: "shared", Cwd: "/code/mac"}, {SessionID: "chat", Cwd: "/home/u/.persona/assistant", Assistant: true,
				State: proto.StateIdle, Status: proto.StatusDone}},
			"pi": {},
		}}
	h.opts.Devices = d
	busy := func() bool { return h.extensionBusy("none") != "" }
	check := func(step string) {
		t.Helper()
		views := d.oldViews()
		for _, id := range []string{"", "a", "shared", "chat", "gone"} {
			if got, want := h.sessionHome(id), oldSessionHome(views, id); got != want {
				t.Errorf("%s: sessionHome(%q) = %q, the old path %q", step, id, got, want)
			}
		}
		for _, by := range []asker{{}, {machine: "box", session: "a"}, {machine: "box", session: "shared"},
			{machine: "mac", session: "shared"}, {machine: "mac", session: "a"}, {machine: "nowhere", session: "a"},
			{machine: "box"}, {machine: "pi", session: "a"}} {
			if got, want := h.askerCwd(by), oldAskerCwd(views, by); got != want {
				t.Errorf("%s: askerCwd(%+v) = %q, the old path %q", step, by, got, want)
			}
		}
		if got, want := busy(), oldAssistantWorking(views); got != want {
			t.Errorf("%s: busy %v, the old path %v", step, got, want)
		}
	}
	check("listed")
	d.mu.Lock()
	d.lists["mac"][1].State = proto.StateWorking // the assistant works
	d.mu.Unlock()
	check("assistant working")
	d.mu.Lock()
	d.lists["mac"][1].State, d.lists["mac"][1].Status = proto.StateIdle, proto.StatusWorking
	d.ids = []string{"mac", "box", "pi"} // another order: the first listing a session is its home
	d.mu.Unlock()
	check("reordered")
	d.mu.Lock()
	d.lists["mac"][1].Status = proto.StatusDone
	d.ids = []string{"box", "pi"} // mac gone
	d.mu.Unlock()
	check("a computer gone")
	if m := h.machineByID("box"); m == nil || h.machineName("box") != "Box" || h.machineByName("box") != m {
		t.Fatalf("box: %v, named %q", m, h.machineName("box"))
	}
	d.mu.Lock()
	d.names["box"] = "Renamed"
	d.mu.Unlock()
	if h.machineName("box") != "Renamed" || h.machineByName("renamed") == nil {
		t.Fatalf("a rename is not seen: %q", h.machineName("box"))
	}
}

// Lookups while another refreshes the mirror (a node's events on its read loop, an app's requests naming the
// computer, a rename) read nothing the refresh writes: run with -race. The mirror's cfg used to be rewritten under
// the machine's lock on every lookup while the callers read it without.
func TestMirrorLookupsWhileItIsRefreshed(t *testing.T) {
	h := newTestManager(t)
	d := &mirrorDevices{ids: []string{"box"}, names: map[string]string{"box": "Box"},
		lists: map[string][]proto.Session{"box": {{SessionID: "conv1", Cwd: "/code/app"}}}}
	h.opts.Devices = d
	var wg sync.WaitGroup
	stop := time.Now().Add(200 * time.Millisecond)
	run := func(f func()) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for time.Now().Before(stop) {
				f()
			}
		}()
	}
	run(func() { h.NodeEvent("box", "plugin.node.state", json.RawMessage(`{}`)) })
	run(func() {
		if h.machineName("box") == "" {
			t.Error("no name")
		}
	})
	run(func() {
		if h.sessionHome("conv1") != "box" || h.askerCwd(asker{machine: "box", session: "conv1"}) != "/code/app" {
			t.Error("the session is not found")
		}
	})
	n := 0
	run(func() { // the computer is renamed, now and then
		n++
		d.mu.Lock()
		d.names["box"] = fmt.Sprintf("Box %d", n%3)
		d.mu.Unlock()
		if m := h.machineByName("box"); m == nil || m.cfg.ID != "box" {
			t.Error("box not found by its ssh alias")
		}
	})
	wg.Wait()
}

// A port stream's event reaches the manager's stream on that computer, with the mirror keeping no sessions.
func TestPortStreamEventFindsItsMachine(t *testing.T) {
	h := newTestManager(t)
	h.opts.Devices = &mirrorDevices{ids: []string{"box"}, names: map[string]string{"box": "Box"}}
	if !h.NodeEvent("box", streams.EventData, json.RawMessage(`{"id":"mx1","data":"aGVsbG8="}`)) {
		t.Fatal("the manager's stream event was not taken")
	}
	if h.NodeEvent("box", streams.EventData, json.RawMessage(`{"id":"p1","data":"aGVsbG8="}`)) {
		t.Fatal("the hub's stream event was taken")
	}
}

// sessionDevices keep each computer's sessions (devices.Devices.FindSession) for a test that puts its computers in
// h.machines itself, without devices.
type sessionDevices struct {
	Devices // Has, CallNode: not called here
	mu      sync.Mutex
	lists   map[string][]proto.Session
}

func (d *sessionDevices) FindSession(id string, match func(*proto.Session) bool) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	list := d.lists[id]
	for i := range list {
		if match(&list[i]) {
			return true
		}
	}
	return false
}

// keepSessions gives h devices that keep its computers' sessions (before anything of h reads them).
func keepSessions(t *testing.T, h *Manager) {
	t.Helper()
	if h.opts.Devices != nil {
		t.Fatal("the test has devices of its own")
	}
	h.opts.Devices = &sessionDevices{lists: map[string][]proto.Session{}}
}

// listSessions makes id's node list sessions, on the devices keepSessions gave h.
func listSessions(h *Manager, id string, list []proto.Session) {
	d := h.opts.Devices.(*sessionDevices)
	d.mu.Lock()
	defer d.mu.Unlock()
	d.lists[id] = list
}
