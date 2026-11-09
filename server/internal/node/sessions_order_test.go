package node

import (
	"context"
	"fmt"
	"slices"
	"testing"

	"github.com/nwverse/persona/server/internal/host"
	"github.com/nwverse/persona/server/internal/proto"
)

// Live agents whose rows tie on UpdatedAt (none has a transcript yet, so all are zero) come out in id order on every
// list: walked in the agents map's order, the stable sort kept a new shuffle each second, and the hub got the whole
// list again for it. Eight tied rows came out in the same order twice with probability 1/8! before.
func TestSessionListTiedRowsKeepIDOrder(t *testing.T) {
	n, _ := testNode(t)
	var want []string
	for i := 8; i >= 1; i-- {
		id := fmt.Sprintf("0d000000-0000-4000-8000-%012d", i)
		n.agents[id] = host.Agent{Agent: "claude", AgentStatus: proto.StatusWorking, Cwd: "/work/repo"}
		want = append(want, id)
	}
	slices.Sort(want)
	for pass := range 2 {
		var got []string
		for _, s := range n.sessionList(context.Background(), false) {
			if !s.UpdatedAt.IsZero() {
				t.Fatalf("%s has an UpdatedAt, the rows should tie", s.SessionID)
			}
			got = append(got, s.SessionID)
		}
		if !slices.Equal(got, want) {
			t.Fatalf("list %d: %v, want the ids in order %v", pass+1, got, want)
		}
	}
}
