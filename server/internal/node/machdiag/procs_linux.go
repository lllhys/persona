//go:build linux

package machdiag

import (
	"context"
	"os"
	"strconv"
	"syscall"
	"time"
)

// cpuWindow is how long CPU use is measured over: two readings of every process's ticks this far apart (one tick is
// 2 % of a core at this width). A lifetime average (ticks over the process's age) would show an idle Agent that once
// compiled for an hour as busy.
const cpuWindow = 500 * time.Millisecond

// readProcesses reads /proc/<pid>/stat of every process twice, cpuWindow apart. No process is started, and only stat
// is read: never cmdline or environ.
func readProcesses(ctx context.Context) ([]process, error) {
	first, err := readProcStats()
	if err != nil {
		return nil, err
	}
	start := time.Now()
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-time.After(cpuWindow):
	}
	second, err := readProcStats()
	if err != nil {
		return nil, err
	}
	return sampleCPU(first, second, time.Since(start), os.Getpagesize()), nil
}

func readProcStats() (map[int]procStat, error) {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return nil, err
	}
	out := make(map[int]procStat, len(entries))
	for _, e := range entries {
		pid, err := strconv.Atoi(e.Name())
		if err != nil {
			continue
		}
		b, err := os.ReadFile("/proc/" + e.Name() + "/stat")
		if err != nil {
			continue // it exited
		}
		if st, ok := parseProcStat(b); ok {
			out[pid] = st
		}
	}
	return out, nil
}

// readMemTotal is the machine's memory, as sysinfo(2) reports it.
func readMemTotal() int64 {
	var si syscall.Sysinfo_t
	if syscall.Sysinfo(&si) != nil {
		return 0
	}
	return int64(si.Totalram) * int64(si.Unit)
}
