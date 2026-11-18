package hub

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/devicewire"
	"github.com/nwverse/persona/server/internal/proto"
)

// The hub's side of a node reconnect, in the order the manager's messages reach it: the machine's state, the node's
// events as they come, then devices.connected (machineConnected), whose node.configure hands the node the Codex
// inventory the hub keeps. A new connection's node starts with an empty registry; an inventory it reports before
// devices.connected (a node from before it waited for node.configure, node codex_registry.go rememberCodex) must not
// replace what the hub kept: the threads the user joined, the other homes, the app-servers.
func TestCodexInventoryKeptAcrossReconnect(t *testing.T) {
	thread := "c0de-11111111222222223333333344444444"
	kept := proto.CodexConfig{LegacyHome: "/home/me/.codex", Homes: []string{"/home/me/.codex", "/home/me/.codex-work"},
		Servers: []proto.CodexServer{{ID: "one", Home: "/home/me/.codex-work", Endpoint: "ws://127.0.0.1:4500"}},
		Joined:  map[string]string{thread: "one"}}
	bare := proto.CodexConfig{LegacyHome: "/home/me/.codex", Homes: []string{"/home/me/.codex"}}
	for _, tc := range []struct {
		name string
		// between is what the manager reports of the machine between the two connections, before the new node's events.
		between []devicewire.State
		// restart: the hub (and the manager with it) restarted in between; the new hub reports the machine connected.
		restart bool
	}{
		{name: "offline first", between: []devicewire.State{{}, {Connected: true}}},
		{name: "new connection at once", between: []devicewire.State{{Connected: true}}},
		{name: "hub restarted", restart: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newTestHub(t)
			h.store = &settingsStore{vals: map[string][]byte{}}
			at := time.Now().UTC()
			report := func(s devicewire.State) {
				s.Machine.ID, s.Machine.Name, s.Machine.Kind, s.Machine.Online = "box", "Box", "ssh", s.Connected
				if s.Connected {
					s.ConnectedAt = at
				}
				h.applyDeviceStates(devicewire.States{Machines: []devicewire.State{s}})
			}
			event := func(m *Machine, c proto.CodexConfig) {
				raw, _ := json.Marshal(c)
				m.onEvent("codex.inventory", raw)
			}
			handed := func(m *Machine) proto.CodexConfig {
				var c proto.CodexConfig
				reply := h.machineConnected(context.Background(), m, at)
				if err := json.Unmarshal(reply.Configure["codex"], &c); err != nil {
					t.Fatalf("node.configure's codex: %v", err)
				}
				return c
			}
			intact := func(what string, c proto.CodexConfig) {
				t.Helper()
				if c.Joined[thread] != "one" || len(c.Homes) != 2 || len(c.Servers) != 1 {
					t.Fatalf("%s: the inventory the hub kept is gone: homes=%v servers=%v joined=%v", what, c.Homes, c.Servers, c.Joined)
				}
			}

			// The first connection: the hub hands it what it has, then keeps what the node reports (the user joined a
			// thread on app-server "one").
			report(devicewire.State{Connected: true})
			m := h.machineByID("box")
			handed(m)
			event(m, kept)
			intact("while connected", m.codexConfig(context.Background()))

			// The node reconnects (a laptop waking, a hub upgrade): a new Node's first report, then devices.connected.
			at = at.Add(time.Minute)
			if tc.restart { // a new hub has done its part for no connection yet
				h2 := newTestHub(t)
				h2.store = h.store
				h = h2
				report(devicewire.State{Connected: true})
				m = h.machineByID("box")
			}
			for _, s := range tc.between {
				report(s)
			}
			event(m, bare)
			intact("node.configure's codex after the reconnect", handed(m))

			// After node.configure the node reports the hub's inventory with what it found: kept again.
			found := kept
			found.Homes = append(append([]string{}, kept.Homes...), "/home/me/.codex-new")
			event(m, found)
			if got := m.codexConfig(context.Background()); len(got.Homes) != 3 || got.Joined[thread] != "one" {
				t.Fatalf("the report after node.configure was not kept: homes=%v joined=%v", got.Homes, got.Joined)
			}
		})
	}
}
