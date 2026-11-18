package devices

import (
	"fmt"
	"testing"

	"github.com/nwverse/persona/server/internal/proto"
)

// ListedMachinesWithSessions is listedMachines with rows sessions listed on each, for the benchmarks outside the
// package (devices_test) that drive the devices as another package does.
func ListedMachinesWithSessions(tb testing.TB, n, rows int) *Devices {
	tb.Helper()
	d := listedMachines(tb, n)
	for _, m := range d.machinesNow() {
		m.setSessions(sessionRows(m.cfg.ID, rows))
	}
	return d
}

// sessionRows are rows sessions as a node lists them.
func sessionRows(machine string, rows int) []proto.Session {
	list := make([]proto.Session, rows)
	for i := range list {
		list[i] = proto.Session{MachineID: machine, SessionID: fmt.Sprintf("%s-s%03d", machine, i), Cwd: "/code/app",
			Agent: "claude", Status: proto.StatusDone, Title: "a session", LastText: "the last words of the turn"}
	}
	return list
}
