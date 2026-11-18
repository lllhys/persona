//go:build unix

package devices

import "github.com/nwverse/persona/server/internal/osproc"

func stopSSHProcessTree(pid int) error {
	return osproc.StopGroup(pid, true)
}

// sweepSSHProcessTree catches a child forked while the group was being signalled. It runs after Wait reaped ssh;
// otherwise ssh's own zombie would keep the group present until the sweep timed out.
func sweepSSHProcessTree(pid int) {
	osproc.SweepGroup(pid, shutdownStepTimeout)
}
