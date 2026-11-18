package assistantskills

import "golang.org/x/sys/unix"

func importRename(from, to string) error { return unix.RenamexNp(from, to, unix.RENAME_EXCL) }
