//go:build !darwin && !linux && !windows

package node

import "os"

// fileInfoSys: nothing beyond FileInfo on other systems.
func fileInfoSys(os.FileInfo, string) fileOwner { return fileOwner{} }
