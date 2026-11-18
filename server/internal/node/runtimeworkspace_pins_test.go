package node

import (
	"context"
	"testing"

	"github.com/nwverse/persona/server/internal/proto"
)

func TestRuntimeWorkspaceNativePinsReachCatalogAndClear(t *testing.T) {
	n, _ := runtimeNode(t)
	if err := n.useEngine(context.Background(), proto.EnginePaseo); err != nil {
		t.Fatal(err)
	}
	rt := dialRuntime(t, n, paseoWorkspacesHello())
	publish := func(pin string) {
		t.Helper()
		if err := rt.peer.Notify("runtime.workspaces", map[string]any{"workspaces": []any{
			map[string]any{"workspace_id": "pinned", "cwd": "/w/app", "kind": "checkout", "space_id": "app", "pinned_at": pin},
			map[string]any{"workspace_id": "sibling", "cwd": "/w/app", "kind": "checkout", "space_id": "app"},
		}}); err != nil {
			t.Fatal(err)
		}
	}
	publish("2026-11-16T14:00:00+01:00")
	waitFor(t, "native pin in the catalog", func() bool { return catalog(t, n)["pinned"].PinnedAt == "2026-11-16T13:00:00Z" })
	if got := catalog(t, n); len(got) != 2 || got["sibling"].PinnedAt != "" {
		t.Fatalf("same-project and same-folder sibling changed: %+v", got)
	}
	publish("")
	waitFor(t, "the unpin replaces the prior snapshot", func() bool { return catalog(t, n)["pinned"].PinnedAt == "" })
}

func TestRuntimeWorkspaceNativePinValidation(t *testing.T) {
	for _, pin := range []string{"", "invalid", "2026-11-16", "2026-02-30T00:00:00Z", "0001-01-01T00:00:00Z"} {
		rows := cleanWorkspaces([]runtimeWorkspace{{ID: "sample", Cwd: "/w/app", PinnedAt: pin}})
		if len(rows) != 1 || rows[0].PinnedAt != "" {
			t.Fatalf("invalid pin %q removed the workspace or survived: %+v", pin, rows)
		}
	}
}
