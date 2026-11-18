//go:build !darwin && !linux && !windows

package node

import (
	"os"
	"time"
)

// fileBirth: this system's file times say nothing of when a file was made.
func fileBirth(string, os.FileInfo) (time.Time, bool) { return time.Time{}, false }
