package nodecmd

import (
	"testing"

	"github.com/nwverse/persona/server/internal/node"
)

// The node answers launch.env for a Codex provider only after waiting up to node.CodexCatalogWait for its gateway's
// model catalog: launch waits well longer, so that wait never ends a start with "the local node did not answer".
func TestLaunchWaitsLongerThanTheCatalogLookup(t *testing.T) {
	if w := node.CodexCatalogWait(); w <= 0 || w > LaunchNodeWait/4 {
		t.Fatalf("node waits up to %s for a Codex catalog, launch %s for the node", w, LaunchNodeWait)
	}
}
