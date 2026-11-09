package node

import "os"

// fileInode is 0: os.Lstat does not read a file index on Windows; the mode and mtime tell a replaced socket
// (codexEndpointPrint).
func fileInode(os.FileInfo) uint64 { return 0 }
