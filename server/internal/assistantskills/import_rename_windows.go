package assistantskills

import "golang.org/x/sys/windows"

func importRename(from, to string) error {
	a, err := windows.UTF16PtrFromString(from)
	if err != nil {
		return err
	}
	b, err := windows.UTF16PtrFromString(to)
	if err != nil {
		return err
	}
	return windows.MoveFileEx(a, b, 0) // no MOVEFILE_REPLACE_EXISTING or cross-volume copy
}
