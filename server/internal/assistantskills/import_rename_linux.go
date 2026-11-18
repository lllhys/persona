package assistantskills

import "golang.org/x/sys/unix"

// No check-then-rename fallback: unsupported filesystems fail closed rather than overwrite an empty directory.
func importRename(from, to string) error {
	return unix.Renameat2(unix.AT_FDCWD, from, unix.AT_FDCWD, to, unix.RENAME_NOREPLACE)
}
