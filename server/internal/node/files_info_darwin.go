//go:build darwin

package node

import (
	"os"
	"syscall"
	"time"
)

// fileInfoSys: the owner, the status change time and the creation time macOS keeps for every file.
func fileInfoSys(info os.FileInfo, _ string) fileOwner {
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return fileOwner{}
	}
	return fileOwner{uid: int(st.Uid), gid: int(st.Gid), ok: true,
		ctime: time.Unix(st.Ctimespec.Unix()), birth: time.Unix(st.Birthtimespec.Unix()), birthKnown: true}
}
