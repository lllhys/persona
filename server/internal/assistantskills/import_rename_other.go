//go:build !linux && !darwin && !windows

package assistantskills

import "errors"

func importRename(from, to string) error {
	return errors.New("exclusive directory rename is unavailable")
}
