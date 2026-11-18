package devices

import (
	"os/exec"

	"github.com/nwverse/persona/server/internal/osproc"
)

// stopSSHTreeOnCancel makes cancellation end ProxyCommand and ssh's other descendants too, not just ssh itself.
func stopSSHTreeOnCancel(cmd *exec.Cmd) {
	cmd.SysProcAttr = osproc.GroupAttr()
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return nil
		}
		return stopSSHProcessTree(cmd.Process.Pid)
	}
}
