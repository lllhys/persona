//go:build windows

package node

import (
	"os"
	"syscall"
	"time"
)

// fileInfoSys: Windows has no owner or mode bits here (the client shows no permissions for a Windows computer), only a
// creation time.
func fileInfoSys(info os.FileInfo, _ string) fileOwner {
	d, ok := info.Sys().(*syscall.Win32FileAttributeData)
	if !ok {
		return fileOwner{}
	}
	return fileOwner{birth: time.Unix(0, d.CreationTime.Nanoseconds()), birthKnown: true}
}
