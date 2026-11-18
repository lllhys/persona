package herdr

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestSnapshotInventoryCompleteness(t *testing.T) {
	for _, tc := range []struct {
		name, reply string
		incomplete  bool
	}{
		{"missing snapshot", `{}`, true},
		{"null snapshot", `{"snapshot":null}`, true},
		{"missing panes", `{"snapshot":{"agents":[]}}`, true},
		{"missing agents", `{"snapshot":{"panes":[]}}`, true},
		{"empty arrays", `{"snapshot":{"agents":[],"panes":[]}}`, false},
		{"null inventories", `{"snapshot":{"agents":null,"panes":null}}`, false},
		{"partial display data", `{"snapshot":{"agents":[{"agent":"claude","pane_id":"w1:p1"}]}}`, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sock := fakeServer(t, func(string, map[string]any) string { return `"result":` + tc.reply })
			snap, err := (&Client{Socket: sock}).Snapshot(context.Background())
			if err != nil || snap == nil {
				t.Fatalf("display decode changed: %+v, %v", snap, err)
			}
			if snap.InventoryIncomplete != tc.incomplete {
				t.Fatalf("incomplete=%v, want %v", snap.InventoryIncomplete, tc.incomplete)
			}
			if tc.name == "partial display data" && (len(snap.Agents) != 1 || snap.Agents[0].PaneID != "w1:p1") {
				t.Fatalf("partial display data lost: %+v", snap)
			}
			encoded, err := json.Marshal(snap)
			if err != nil || strings.Contains(string(encoded), "InventoryIncomplete") || strings.Contains(string(encoded), "inventory_incomplete") {
				t.Fatalf("local metadata changed wire JSON: %s, %v", encoded, err)
			}
		})
	}
}

func TestRebuiltSnapshotInventoryCompleteness(t *testing.T) {
	for _, scenario := range []string{"complete", "missing workspaces", "missing panes", "empty workspaces", "null workspaces", "null panes"} {
		t.Run(scenario, func(t *testing.T) {
			whole := snapshotFixture() // Only the whole reply exceeds the fake socket's limit.
			sock := fakeServerLimited(t, 8192, func(method string, params map[string]any) string {
				if method == "workspace.list" {
					switch scenario {
					case "missing workspaces":
						return `"result":{}`
					case "empty workspaces":
						return `"result":{"workspaces":[]}`
					case "null workspaces":
						return `"result":{"workspaces":null}`
					}
				}
				if method == "pane.list" && params["workspace_id"] == "w3" {
					// An empty workspace still needs an explicit pane inventory.
					switch scenario {
					case "missing panes":
						return `"result":{}`
					case "null panes":
						return `"result":{"panes":null}`
					}
				}
				return snapshotReply(whole, method, params)
			})
			snap, err := (&Client{Socket: sock}).Snapshot(context.Background())
			if err != nil || snap == nil {
				t.Fatalf("rebuild failed: %+v, %v", snap, err)
			}
			wantIncomplete := strings.HasPrefix(scenario, "missing")
			if snap.InventoryIncomplete != wantIncomplete {
				t.Fatalf("incomplete=%v, want %v", snap.InventoryIncomplete, wantIncomplete)
			}
		})
	}
}
