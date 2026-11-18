package claudeplugin

import (
	"os"

	"golang.org/x/sys/windows"
)

// lockFile holds an exclusive lock on path (created when missing) until the returned function is called: one check
// at a time, whether the resident node or a `claude-plugin` command runs it.
func lockFile(path string) (func(), error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err := windows.LockFileEx(windows.Handle(f.Fd()), windows.LOCKFILE_EXCLUSIVE_LOCK, 0, 1, 0, &windows.Overlapped{}); err != nil {
		f.Close()
		return nil, err
	}
	return func() {
		_ = windows.UnlockFileEx(windows.Handle(f.Fd()), 0, 1, 0, &windows.Overlapped{})
		f.Close()
	}, nil
}
