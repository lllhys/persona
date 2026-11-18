package node

import (
	"os"
	"time"

	"golang.org/x/sys/unix"
)

// fileBirth is when the file at path was made, when the kernel and the file system report it (statx's btime: ext4,
// btrfs, xfs on recent kernels); false where they do not.
func fileBirth(path string, _ os.FileInfo) (time.Time, bool) {
	var st unix.Statx_t
	if err := unix.Statx(unix.AT_FDCWD, path, 0, unix.STATX_BTIME, &st); err != nil || st.Mask&unix.STATX_BTIME == 0 || st.Btime.Sec <= 0 {
		return time.Time{}, false
	}
	return time.Unix(st.Btime.Sec, int64(st.Btime.Nsec)).UTC(), true
}
