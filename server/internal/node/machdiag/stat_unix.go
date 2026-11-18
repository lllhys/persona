//go:build linux || darwin

package machdiag

import (
	"io/fs"
	"syscall"
)

// statOf reads the device, inode, link count and allocated blocks (st_blocks is in 512-byte units on both systems).
func statOf(info fs.FileInfo) (fileStat, bool) {
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return fileStat{}, false
	}
	dev := uint64(st.Dev) // int32 on darwin, uint64 on linux
	return fileStat{
		id:    fileID{dev: dev, ino: st.Ino},
		dev:   dev,
		links: uint64(st.Nlink), // uint16 on darwin
		dir:   info.IsDir(),
		bytes: st.Blocks * 512,
	}, true
}

// availableBytes is what an ordinary user can still write on the filesystem holding path, like df.
func availableBytes(path string) (int64, error) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return 0, err
	}
	return int64(st.Bavail) * int64(st.Bsize), nil // the field types differ per system
}
