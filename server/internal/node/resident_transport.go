package node

import (
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"time"

	"github.com/nwverse/persona/server/internal/link"
	"github.com/nwverse/persona/server/internal/osproc"
)

// ResidentTransport owns the attach listener and its run lock independently of a connection's Node.
// Keeping duplicates through exec leaves the same socket and accept backlog available to reconnecting relays.
type ResidentTransport struct {
	Listener net.Listener
	lock     *os.File
}

func OpenResidentTransport() (*ResidentTransport, error) {
	path := AttachSocketPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	if transport, inherited, err := inheritedResidentTransport(path); inherited || err != nil {
		return transport, err
	}
	lock, err := link.LockFileHandle(path+".run", false)
	if errors.Is(err, link.ErrLocked) {
		return nil, fmt.Errorf("a resident node is already running (%s.run is locked)", path)
	}
	if err != nil {
		return nil, fmt.Errorf("resident node lock: %w", err)
	}
	fail := func(err error) (*ResidentTransport, error) { _ = lock.Close(); return nil, err }
	if c, err := net.DialTimeout("unix", path, time.Second); err == nil {
		_ = c.Close()
		return fail(fmt.Errorf("a resident node is already listening on %s", path))
	}
	_ = os.Remove(path)
	ln, err := osproc.Listen("unix", path)
	if err != nil {
		return fail(err)
	}
	_ = os.Chmod(path, 0o600)
	ln.(*osproc.UnixListener).SetUnlinkOnClose(false)
	return &ResidentTransport{Listener: ln, lock: lock}, nil
}

func (t *ResidentTransport) Close() {
	_ = t.Listener.Close()
	_ = t.lock.Close()
}
