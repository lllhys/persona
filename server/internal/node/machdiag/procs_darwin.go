//go:build darwin

package machdiag

import (
	"context"
	"os/exec"
	"time"

	"golang.org/x/sys/unix"
)

// psTimeout bounds the one ps run; it takes a few tens of milliseconds.
const psTimeout = 5 * time.Second

// readProcesses runs ps once for each process's pid, resident memory, CPU and executable (comm: the executable's
// path, never its arguments).
func readProcesses(ctx context.Context) ([]process, error) {
	ctx, cancel := context.WithTimeout(ctx, psTimeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, "/bin/ps", "-axo", "pid=,rss=,pcpu=,comm=").Output()
	if err != nil {
		return nil, err
	}
	return parsePS(out), nil
}

// readMemTotal is hw.memsize.
func readMemTotal() int64 {
	v, err := unix.SysctlUint64("hw.memsize")
	if err != nil {
		return 0
	}
	return int64(v)
}
