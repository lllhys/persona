package devices_test

import (
	"encoding/json"
	"testing"

	"github.com/nwverse/persona/server/internal/devices"
	"github.com/nwverse/persona/server/internal/extmgr"
	"github.com/nwverse/persona/server/internal/streams"
)

// BenchmarkExtensionsPortStreamEvent: one stream.data event of the extensions manager's own port streams on one of
// 20 computers of 200 sessions each, over real devices. The extensions manager looks the computer up for each one
// (NodeEvent -> machineByID -> machinesNow -> Devices.Views); before #3197 that copied every computer's sessions
// into its mirror. Here and not in extmgr: only this package can list sessions on real devices (export_test.go).
func BenchmarkExtensionsPortStreamEvent(b *testing.B) {
	d := devices.ListedMachinesWithSessions(b, 20, 200)
	h := extmgr.New(extmgr.Options{Dir: b.TempDir(), Config: &extmgr.Config{}, Devices: d})
	params := json.RawMessage(`{"id":"mx1","data":"aGVsbG8="}`)
	b.ReportAllocs()
	for b.Loop() {
		if !h.NodeEvent("soak08", streams.EventData, params) {
			b.Fatal("not taken")
		}
	}
}
