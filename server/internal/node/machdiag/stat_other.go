//go:build !linux && !darwin

package machdiag

import "io/fs"

// statOf: the disk section is not described on this system (diskSupported), so nothing is walked.
func statOf(fs.FileInfo) (fileStat, bool) { return fileStat{}, false }

func availableBytes(string) (int64, error) { return 0, errUnsupported }
