package node

import (
	"encoding/json"
	"testing"
)

func TestLeaderToolWatchRechecksLatePaneIdentity(t *testing.T) {
	for _, tc := range []struct{ name, previous, current, want string }{
		{"still unknown", "", "", "revision"},
		{"late leader", "", "leader", ""},
		{"same leader", "leader", "leader", "revision"},
		{"pane reused", "leader", "worker", ""},
		{"identity lost", "leader", "", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw, _ := json.Marshal(map[string]string{"revision": "revision", "session": tc.previous})
			got := mcpToolsParams(raw, tc.current, false, true)
			revision, _ := got["revision"].(string)
			if revision != tc.want || got["session_id"] != tc.current || got["assistant"] != false || got["guest"] != true {
				t.Fatalf("watch changed caller identity or reused a stale cursor: %v", got)
			}
		})
	}
}
