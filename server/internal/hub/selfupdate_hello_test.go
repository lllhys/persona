package hub

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/nwverse/persona/server/internal/assistantwire"
)

func TestNodePolicyManagerHelloWithoutUpdater(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		name := "disabled"
		if enabled {
			name = "enabled"
		}
		t.Run(name, func(t *testing.T) {
			h := newTestHub(t)
			h.cfg.AutoUpdateNodes = enabled
			h.upd = nil
			h.bus.idents = &memIdents{}
			mux := http.NewServeMux()
			h.busRoutes(mux)
			srv := httptest.NewServer(mux)
			t.Cleanup(srv.Close)
			const id = "m-test"
			remote := dialRawBus(t, srv, busToken(t, h, id, "Test manager"))
			params := busManagerHello{ID: id, Capabilities: []string{assistantwire.CapabilityConfigOwn}}
			var reply busHelloReply
			if err := remote.call("manager.hello", params, &reply); err != nil {
				t.Fatal(err)
			}
			if reply.Devices == nil || reply.Devices.AutoUpdateNodes {
				t.Fatalf("uninitialized policy must withhold node updates: %+v", reply.Devices)
			}
			if h.upd != nil {
				t.Fatal("manager hello initialized an updater as a side effect")
			}
			// Once initialized, the normal policy reader retains startup fallback.
			h.upd = newUpdater(h)
			if err := remote.call("manager.hello", params, &reply); err != nil {
				t.Fatal(err)
			}
			if reply.Devices == nil || reply.Devices.AutoUpdateNodes != enabled {
				t.Fatalf("initialized policy differs from startup choice: %+v", reply.Devices)
			}
		})
	}
}
