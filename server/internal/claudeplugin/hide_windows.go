package claudeplugin

import (
	"os/exec"
	"syscall"

	"golang.org/x/sys/windows"
)

// hideWindow keeps the CLI from opening a console window when the node runs without one (the scheduled task's
// supervisor starts it windowless, docs/operations/windows.md).
func hideWindow(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: windows.CREATE_NO_WINDOW}
}
