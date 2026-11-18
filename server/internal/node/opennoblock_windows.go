//go:build windows

package node

import "os"

// openNoBlock opens path for reading (Windows has no FIFOs in the file system to wait on).
func openNoBlock(path string) (*os.File, error) { return os.Open(path) }

func openKnowledgeFile(root *os.Root, path string) (*os.File, error) {
	return root.OpenFile(path, os.O_RDONLY, 0)
}
