//go:build unix

package node

import (
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"syscall"

	"golang.org/x/sys/unix"

	"github.com/nwverse/persona/server/internal/osproc"
)

// This descriptor ticket is valid only in the same PID after exec, and is consumed before starting any child.
const residentHandoverEnv = "PERSONA_NODE_HANDOVER"

const ResidentHandoverSupported = true

type ResidentFiles struct{ listener, lock *os.File }

func inheritedResidentTransport(path string) (*ResidentTransport, bool, error) {
	ticket := os.Getenv(residentHandoverEnv)
	_ = os.Unsetenv(residentHandoverEnv)
	if ticket == "" {
		return nil, false, nil
	}
	parts := strings.Split(ticket, ":")
	invalid := func() (*ResidentTransport, bool, error) {
		return nil, true, fmt.Errorf("invalid resident listener handover")
	}
	if len(parts) != 3 {
		return invalid()
	}
	pid, e1 := strconv.Atoi(parts[0])
	listenerFD, e2 := strconv.Atoi(parts[1])
	lockFD, e3 := strconv.Atoi(parts[2])
	if e1 != nil || e2 != nil || e3 != nil || pid != os.Getpid() || listenerFD <= 2 || lockFD <= 2 || listenerFD == lockFD {
		return invalid()
	}
	listener := os.NewFile(uintptr(listenerFD), "resident-listener")
	lock := os.NewFile(uintptr(lockFD), "resident-lock")
	if listener == nil || lock == nil {
		return invalid()
	}
	defer listener.Close()
	ln, err := osproc.FileListener(listener)
	if err != nil {
		_ = lock.Close()
		return invalid()
	}
	file, statErr := lock.Stat()
	at, pathErr := os.Stat(path + ".run")
	address, ok := ln.Addr().(*net.UnixAddr)
	if !ok || address.Name != path || statErr != nil || pathErr != nil || !os.SameFile(file, at) || !file.Mode().IsRegular() {
		_ = ln.Close()
		_ = lock.Close()
		return invalid()
	}
	if err := unix.Flock(lockFD, unix.LOCK_EX|unix.LOCK_NB); err != nil {
		_ = ln.Close()
		_ = lock.Close()
		return invalid()
	}
	// SealInherited normally did this already. Keep it explicit for direct callers and tests.
	syscall.CloseOnExec(lockFD)
	ln.(*osproc.UnixListener).SetUnlinkOnClose(false)
	return &ResidentTransport{Listener: ln, lock: lock}, true, nil
}

func (t *ResidentTransport) PrepareExec() (*ResidentFiles, error) {
	listener, err := t.Listener.(*osproc.UnixListener).File()
	if err != nil {
		return nil, err
	}
	fd, err := unix.FcntlInt(t.lock.Fd(), unix.F_DUPFD_CLOEXEC, 3)
	if err != nil {
		_ = listener.Close()
		return nil, err
	}
	return &ResidentFiles{listener: listener, lock: os.NewFile(uintptr(fd), "resident-lock")}, nil
}

func (f *ResidentFiles) Close() {
	_ = f.listener.Close()
	_ = f.lock.Close()
}

func (f *ResidentFiles) Exec(path string, args, env []string) error {
	clean := make([]string, 0, len(env)+1)
	for _, entry := range env {
		if !strings.HasPrefix(entry, residentHandoverEnv+"=") {
			clean = append(clean, entry)
		}
	}
	clean = append(clean, fmt.Sprintf("%s=%d:%d:%d", residentHandoverEnv, os.Getpid(), f.listener.Fd(), f.lock.Fd()))
	// No other child may inherit the listener or lock between clearing close-on-exec and exec.
	syscall.ForkLock.RLock()
	defer syscall.ForkLock.RUnlock()
	files := []*os.File{f.listener, f.lock}
	for _, file := range files {
		if _, err := unix.FcntlInt(file.Fd(), unix.F_SETFD, 0); err != nil {
			for _, seal := range files {
				syscall.CloseOnExec(int(seal.Fd()))
			}
			return err
		}
	}
	err := syscall.Exec(path, args, clean)
	for _, file := range files {
		syscall.CloseOnExec(int(file.Fd()))
	}
	return err
}
