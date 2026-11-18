//go:build unix

package devices

import (
	"errors"
	"syscall"
)

func proxyProcessAlive(pid int) bool {
	return !errors.Is(syscall.Kill(pid, 0), syscall.ESRCH)
}

func killProxyProcess(pid int) {
	_ = syscall.Kill(pid, syscall.SIGKILL)
}
