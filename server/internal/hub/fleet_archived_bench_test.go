package hub

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/assistantwire"
	"github.com/nwverse/persona/server/internal/proto"
)

// BenchmarkTakeFleetSessionsWithArchive is the hub's whole hop of a manager's fleet.sessions for a machine with 200
// live sessions, one of them changed, to two older apps and five delta apps, while its archive keeps 0, 2,000 or
// 8,000 archived rows (#6836): inside the list (an older manager) and told apart (fleet.archived, once).
func BenchmarkTakeFleetSessionsWithArchive(b *testing.B) {
	for _, records := range []int{0, 2000, 8000} {
		for _, apart := range []bool{false, true} {
			b.Run(fmt.Sprintf("records=%d/apart=%v", records, apart), func(b *testing.B) {
				h := newTestHub(&testing.T{})
				m := newMachine(h, MachineConfig{ID: "box", Name: "Box", Kind: "local"})
				h.machines, h.byID["box"] = append(h.machines, m), m
				for i := range 7 {
					c := &client{out: make(chan []byte, 4), subs: map[[2]string]bool{}, role: roleFull}
					if i >= 2 {
						c.delta, c.have = true, map[string]map[string][]byte{}
					}
					addTestClient(h, c)
				}
				const live = 200
				rows := make([][2][]byte, live)
				for i := range rows {
					at := time.Unix(1700000000+int64(i), 0)
					s := proto.Session{MachineID: "box", SessionID: fmt.Sprintf("s%03d", i), Title: "Run the tests", Cwd: "/Users/me/projects/app",
						Status: proto.StatusIdle, State: proto.StateIdle, Agent: "claude", UpdatedAt: at, ActivityAt: &at}
					for v := range rows[i] {
						s.LastText = fmt.Sprintf("%600d", v)
						rows[i][v], _ = json.Marshal(s)
					}
				}
				archived := make([]proto.Session, records)
				var tail []byte
				for i := range archived {
					at := time.Unix(1600000000+int64(i), 0)
					archived[i] = proto.Session{MachineID: "box", SessionID: fmt.Sprintf("a%05d", i), Title: "An archived task",
						Cwd: "/Users/me/projects/app", Status: proto.StatusOffline, State: proto.StateOffline, Agent: "claude", UpdatedAt: at,
						Archived: true, ArchiveChangedAt: &at, LastText: fmt.Sprintf("%180d", i), Capabilities: &proto.SessionCapabilities{Read: true}}
					raw, _ := json.Marshal(archived[i])
					tail = append(append(tail, ','), raw...)
				}
				if apart {
					raw, _ := json.Marshal(assistantwire.FleetArchived{MachineID: "box", Sessions: archived})
					busEvents[assistantwire.EventFleetArchived](h, nil, raw)
					tail = nil
				}
				take := busEvents[assistantwire.EventFleetSessions]
				var params []byte
				drain := func() {
					for c := range h.clients {
						for len(c.out) > 0 {
							<-c.out
						}
					}
				}
				send := func(n int) {
					params = append(params[:0], `{"machine_id":"box","sessions":[`...)
					for i := range rows {
						if i > 0 {
							params = append(params, ',')
						}
						v := n / live
						if i < n%live {
							v++
						}
						params = append(params, rows[i][v%2]...)
					}
					params = append(params, tail...)
					take(h, nil, append(params, `]}`...))
					drain()
				}
				send(0)
				for c := range h.clients {
					if c.delta {
						h.snapshotSessions(c, m)
					}
				}
				drain()
				b.ReportAllocs()
				n := 0
				for b.Loop() {
					n++
					send(n)
				}
				b.ReportMetric(float64(len(params)+2), "msg-B/op")
			})
		}
	}
}
