package node

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

func updateProcessArgv(pid int) ([]string, error) {
	data, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/cmdline")
	if err != nil {
		return nil, err
	}
	if len(data) == 0 || data[len(data)-1] != 0 {
		return nil, fmt.Errorf("incomplete process arguments")
	}
	return strings.Split(string(data[:len(data)-1]), "\x00"), nil
}

// processExecPath is the executable the kernel ran for pid: /proc/<pid>/exe (a file replaced on disk since keeps
// its path, without the kernel's " (deleted)").
func processExecPath(pid int) (string, error) {
	p, err := os.Readlink("/proc/" + strconv.Itoa(pid) + "/exe")
	if err != nil {
		return "", err
	}
	return strings.TrimSuffix(p, " (deleted)"), nil
}
