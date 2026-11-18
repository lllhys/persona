package node

import (
	"os"
	"syscall"
	"time"
)

// fileBirth is when the file of info was made, as the file system keeps it (APFS and HFS+ do).
func fileBirth(_ string, info os.FileInfo) (time.Time, bool) {
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok || st.Birthtimespec.Sec <= 0 {
		return time.Time{}, false
	}
	return time.Unix(st.Birthtimespec.Sec, st.Birthtimespec.Nsec).UTC(), true
}
