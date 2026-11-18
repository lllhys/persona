//go:build !windows

package claudeplugin

import (
	"os"
	"syscall"
)

// lockFile holds an exclusive lock on path (created when missing) until the returned function is called: one check
// at a time, whether the resident node or a `claude-plugin` command runs it.
func lockFile(path string) (func(), error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		f.Close()
		return nil, err
	}
	return func() {
		_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		f.Close()
	}, nil
}
