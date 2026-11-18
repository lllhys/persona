package node

import (
	"os"
	"syscall"
	"time"
)

// fileBirth is when the file of info was made (NTFS keeps it).
func fileBirth(_ string, info os.FileInfo) (time.Time, bool) {
	d, ok := info.Sys().(*syscall.Win32FileAttributeData)
	if !ok || d.CreationTime.Nanoseconds() <= 0 {
		return time.Time{}, false
	}
	return time.Unix(0, d.CreationTime.Nanoseconds()).UTC(), true
}
