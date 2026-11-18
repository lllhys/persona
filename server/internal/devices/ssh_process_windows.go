package devices

import (
	"errors"
	"fmt"
	"unsafe"

	"github.com/nwverse/persona/server/internal/osproc"
	"golang.org/x/sys/windows"
)

// stopSSHProcessTree also works after ssh exited, when taskkill /T can no longer find its descendants from the root.
// Parent ids remain in the Windows process snapshot, so kill ssh first and then walk what it left behind. Repeating
// the snapshot catches a descendant forked during the first pass.
func stopSSHProcessTree(pid int) error {
	if err := osproc.Kill(pid); err != nil {
		return err
	}
	for round := 0; ; round++ {
		descendants, err := sshDescendants(pid)
		if err != nil {
			return err
		}
		if len(descendants) == 0 {
			return nil
		}
		if round == 5 {
			return fmt.Errorf("ssh descendants of process %d are still running", pid)
		}
		for _, child := range descendants {
			if err := osproc.Kill(child); err != nil {
				return err
			}
		}
	}
}

// sweepSSHProcessTree checks once more after Wait, including any child created while cancellation was in progress.
func sweepSSHProcessTree(pid int) {
	_ = stopSSHProcessTree(pid)
}

func sshDescendants(root int) ([]int, error) {
	snap, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return nil, err
	}
	defer windows.CloseHandle(snap)
	children := map[int][]int{}
	e := windows.ProcessEntry32{Size: uint32(unsafe.Sizeof(windows.ProcessEntry32{}))}
	for err = windows.Process32First(snap, &e); err == nil; err = windows.Process32Next(snap, &e) {
		pid, parent := int(e.ProcessID), int(e.ParentProcessID)
		if pid != root {
			children[parent] = append(children[parent], pid)
		}
	}
	if !errors.Is(err, windows.ERROR_NO_MORE_FILES) {
		return nil, err
	}
	seen := map[int]bool{root: true}
	var descendants []int
	var visit func(int)
	visit = func(parent int) {
		for _, pid := range children[parent] {
			if seen[pid] {
				continue
			}
			seen[pid] = true
			visit(pid)
			descendants = append(descendants, pid) // descendants before their parent
		}
	}
	visit(root)
	return descendants, nil
}
