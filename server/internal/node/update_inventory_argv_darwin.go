package node

import "golang.org/x/sys/unix"

func updateProcessArgv(pid int) ([]string, error) {
	data, err := unix.SysctlRaw("kern.procargs2", pid)
	if err != nil {
		return nil, err
	}
	return updateDarwinArgv(data)
}

// processExecPath is the executable the kernel ran for pid: KERN_PROCARGS2's executable path, which a process that
// renames itself (Node.js's process.title) leaves alone.
func processExecPath(pid int) (string, error) {
	data, err := unix.SysctlRaw("kern.procargs2", pid)
	if err != nil {
		return "", err
	}
	return darwinExecPath(data)
}
