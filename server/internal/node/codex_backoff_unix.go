//go:build !windows

package node

import (
	"os"
	"syscall"
)

// fileInode is the file's inode number (codexEndpointPrint).
func fileInode(st os.FileInfo) uint64 {
	if sys, ok := st.Sys().(*syscall.Stat_t); ok {
		return uint64(sys.Ino)
	}
	return 0
}
