package node

import (
	"errors"
	"fmt"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Port discovery on Windows (docs/operations/ports.md): no ps, no lsof, no /proc. The process tree comes from a Toolhelp
// snapshot (as listWinProcs), the listening sockets and their owners from iphlpapi's GetExtendedTcpTable; no child
// process is started.

var (
	iphlpapi                = windows.NewLazySystemDLL("iphlpapi.dll")
	procGetExtendedTcpTable = iphlpapi.NewProc("GetExtendedTcpTable")
)

const (
	tcpTableOwnerPIDListener = 3 // TCP_TABLE_OWNER_PID_LISTENER
	afInet                   = 2
	afInet6                  = 23
	maxTCPTable              = 16 << 20
)

// winProcs is the process table from a Toolhelp snapshot, with each process's creation time where this user may
// read it (winProcTable uses it to drop a parent link that a reused pid would fake).
func winProcs() (map[int]proc, error) {
	snap, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return nil, fmt.Errorf("process snapshot: %w", err)
	}
	defer windows.CloseHandle(snap)
	var entries []winEntry
	e := windows.ProcessEntry32{Size: uint32(unsafe.Sizeof(windows.ProcessEntry32{}))}
	for err = windows.Process32First(snap, &e); err == nil; err = windows.Process32Next(snap, &e) {
		entries = append(entries, winEntry{pid: int(e.ProcessID), ppid: int(e.ParentProcessID),
			name: windows.UTF16ToString(e.ExeFile[:]), start: processStart(e.ProcessID)})
	}
	if !errors.Is(err, windows.ERROR_NO_MORE_FILES) {
		return nil, fmt.Errorf("process snapshot: %w", err)
	}
	return winProcTable(entries), nil
}

// processStart is a process's creation time, zero when it cannot be opened (another user's, a protected one).
func processStart(pid uint32) time.Time {
	if pid == 0 {
		return time.Time{}
	}
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pid)
	if err != nil {
		return time.Time{}
	}
	defer windows.CloseHandle(h)
	var created, exited, kernel, user windows.Filetime
	if windows.GetProcessTimes(h, &created, &exited, &kernel, &user) != nil {
		return time.Time{}
	}
	return time.Unix(0, created.Nanoseconds())
}

// winListeners are the listening TCP sockets, IPv4 and IPv6, of the processes owners has and of those whose
// environment names one of roots' panes (paneListeners, which adds those to owners).
func winListeners(owners map[int]string, roots map[string][]int, panesOf func(pid int) []string) ([]listener, error) {
	var all []listener
	for _, t := range []struct {
		af    uint32
		parse func([]byte) []listener
	}{{afInet, parseWinTCP4}, {afInet6, parseWinTCP6}} {
		b, err := tcpTable(t.af)
		if err != nil {
			return nil, err
		}
		all = append(all, t.parse(b)...)
	}
	return paneListeners(all, owners, roots, panesOf), nil
}

// tcpTable is GetExtendedTcpTable's listener table for one address family, grown until it fits (a socket opened
// between the size query and the read makes it larger).
func tcpTable(af uint32) ([]byte, error) {
	size := uint32(16 << 10)
	for {
		b := make([]byte, size)
		r, _, _ := procGetExtendedTcpTable.Call(uintptr(unsafe.Pointer(&b[0])), uintptr(unsafe.Pointer(&size)), 0,
			uintptr(af), tcpTableOwnerPIDListener, 0)
		switch windows.Errno(r) {
		case 0:
			return b[:size], nil
		case windows.ERROR_INSUFFICIENT_BUFFER:
			if size > maxTCPTable {
				return nil, fmt.Errorf("GetExtendedTcpTable: table of %d bytes", size)
			}
			size += 4 << 10
		default:
			return nil, fmt.Errorf("GetExtendedTcpTable: %w", windows.Errno(r))
		}
	}
}
