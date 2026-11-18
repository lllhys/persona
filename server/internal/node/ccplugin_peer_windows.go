//go:build windows

package node

import (
	"fmt"
	"net"
	"syscall"
	"unsafe"
)

// sioAFUnixGetPeerPID is SIO_AF_UNIX_GETPEERPID (_WSAIOR(IOC_VENDOR, 256)): the process id at the other end of an
// AF_UNIX socket, on Windows 10 1803 and later.
const sioAFUnixGetPeerPID = 0x58000100

// ccPluginPeerPID is the process at the other end of a node socket connection. Only the plugin's requests ask for it
// here: peerPID stays unknown on Windows for its other callers (peer_other.go), whose fallbacks expect that.
func ccPluginPeerPID(c net.Conn) (int, error) {
	uc, ok := c.(*net.UnixConn)
	if !ok {
		return 0, fmt.Errorf("not a unix socket")
	}
	raw, err := uc.SyscallConn()
	if err != nil {
		return 0, err
	}
	var pid uint32
	var serr error
	if err := raw.Control(func(fd uintptr) {
		var n uint32
		serr = syscall.WSAIoctl(syscall.Handle(fd), sioAFUnixGetPeerPID, nil, 0, (*byte)(unsafe.Pointer(&pid)), 4, &n, nil, 0)
	}); err != nil {
		return 0, err
	}
	return int(pid), serr
}
