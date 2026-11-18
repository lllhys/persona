//go:build !windows

package node

import (
	"context"

	"github.com/nwverse/persona/server/internal/host"
)

// freshPaneShells preserves the pre-existing Unix choice: the host's report, else herdrPaneShell. It performs no
// foreground request or config read, so Unix starts keep their exact inputs and typed bytes.
func (n *Node) freshPaneShells(_ context.Context, _ host.Launcher, created *host.Created) []string {
	if created.Shell != "" {
		return []string{created.Shell}
	}
	return []string{herdrPaneShell()}
}
