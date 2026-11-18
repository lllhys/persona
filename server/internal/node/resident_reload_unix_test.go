//go:build unix

package node

import (
	"context"
	"fmt"
	"net"
	"os"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/link"
	"github.com/nwverse/persona/server/internal/osproc"
	"golang.org/x/sys/unix"
)

func TestResidentListenerHandoverRetainsBacklogAndLock(t *testing.T) {
	shortHome(t)
	transport, err := OpenResidentTransport()
	if err != nil {
		t.Fatal(err)
	}
	defer transport.Close()
	before, err := os.Stat(AttachSocketPath())
	if err != nil {
		t.Fatal(err)
	}
	files, err := transport.PrepareExec()
	if err != nil {
		t.Fatal(err)
	}
	defer files.Close()
	_ = transport.Listener.Close()
	// The original accept loop has ended, but the retained descriptor still accepts connections into its backlog.
	client, err := net.DialTimeout("unix", AttachSocketPath(), time.Second)
	if err != nil {
		t.Fatalf("listener disappeared during handover: %v", err)
	}
	defer client.Close()
	listenerFD, err := unix.FcntlInt(files.listener.Fd(), unix.F_DUPFD_CLOEXEC, 3)
	if err != nil {
		t.Fatal(err)
	}
	lockFD, err := unix.FcntlInt(files.lock.Fd(), unix.F_DUPFD_CLOEXEC, 3)
	if err != nil {
		_ = unix.Close(listenerFD)
		t.Fatal(err)
	}
	// Duplicate the ticket descriptors as exec would: old Go os.File objects must not own the newly imported fds.
	t.Setenv(residentHandoverEnv, fmt.Sprintf("%d:%d:%d", os.Getpid(), listenerFD, lockFD))
	next, err := OpenResidentTransport()
	if err != nil {
		t.Fatal(err)
	}
	defer next.Close()
	if os.Getenv(residentHandoverEnv) != "" {
		t.Fatal("handover ticket was inherited by later children")
	}
	after, err := os.Stat(AttachSocketPath())
	if err != nil || !os.SameFile(before, after) {
		t.Fatal("socket was replaced instead of handed over")
	}
	next.Listener.(*osproc.UnixListener).SetDeadline(time.Now().Add(time.Second))
	accepted, err := next.Listener.Accept()
	if err != nil {
		t.Fatalf("queued connection was not inherited: %v", err)
	}
	_ = accepted.Close()
	if release, err := link.LockFile(AttachSocketPath()+".run", false); err == nil {
		release()
		t.Fatal("another resident could steal the listener during exec")
	}
}

func TestResidentReloadReapsIdleProcessesBeforeTransportEnds(t *testing.T) {
	shortHome(t) // the attach socket and its lock under a home of its own: not those of a node running on this computer
	shortenKeeperStop(t, 200*time.Millisecond, time.Second)
	n := assistantNode(t)
	stubborn, polite, spare := connectionEndProcs(t, n)
	for _, proc := range []*assistantProc{stubborn, polite, spare} {
		proc.mu.Lock()
		proc.busy, proc.compacting, proc.tasks = false, false, nil
		proc.mu.Unlock()
		liveAssistants.Store(proc, struct{}{})
		t.Cleanup(func() { liveAssistants.Delete(proc) })
	}
	transport, err := OpenResidentTransport()
	if err != nil {
		t.Fatal(err)
	}
	defer transport.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := (&ResidentReload{}).Prepare(ctx); err != nil {
		t.Fatal(err)
	}
	allExited(t, "reload prepared", stubborn, polite, spare)
	client, err := net.DialTimeout("unix", AttachSocketPath(), time.Second)
	if err != nil {
		t.Fatalf("Attach closed before idle processes were drained: %v", err)
	}
	_ = client.Close()
}

func TestResidentReloadRejectsNewWorkAfterDrainingAdmittedWork(t *testing.T) {
	r := &ResidentReload{}
	finish, err := r.enter()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	prepared := make(chan error, 1)
	go func() { prepared <- r.Prepare(ctx) }()
	select {
	case <-prepared:
		t.Fatal("reload did not wait for admitted work")
	default:
	}
	finish()
	if err := <-prepared; err != nil {
		t.Fatal(err)
	}
	if finish, err := r.enter(); err == nil {
		finish()
		t.Fatal("new work started in the drained old node")
	}
	r.Resume()
	finish, err = r.enter()
	if err != nil {
		t.Fatal("a canceled handover left admission paused")
	}
	finish()
}
