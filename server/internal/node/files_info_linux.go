//go:build linux

package node

import (
	"os"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

// fileInfoSys: the owner and status change time from stat; the creation time from statx where the file system keeps it
// (ext4, btrfs, xfs with a 4.11+ kernel; never on tmpfs or older kernels).
func fileInfoSys(info os.FileInfo, path string) fileOwner {
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return fileOwner{}
	}
	o := fileOwner{uid: int(st.Uid), gid: int(st.Gid), ok: true, ctime: time.Unix(st.Ctim.Unix())}
	var sx unix.Statx_t
	if err := unix.Statx(unix.AT_FDCWD, path, unix.AT_SYMLINK_NOFOLLOW, unix.STATX_BTIME, &sx); err == nil && sx.Mask&unix.STATX_BTIME != 0 {
		o.birth, o.birthKnown = time.Unix(sx.Btime.Sec, int64(sx.Btime.Nsec)), true
	}
	return o
}
