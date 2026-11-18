//go:build !unix

package hubedge

import (
	"context"
	"errors"
	"net"
	"time"
)

// Options is how Run runs the edge (Linux and macOS only).
type Options struct {
	Listeners     []net.Listener
	Binary        string
	Args          []string
	RunDir        string
	Control       string
	Version       string
	Logf          func(format string, args ...any)
	RestoreUpdate func(build int64, cause error) (binary string, ok bool, err error)

	HoldMax, StandbyWait, ReleaseWait, ActivateWait, ConfirmWait time.Duration
}

// Run is not available here: the hub serves by itself.
func Run(context.Context, Options) error {
	return errors.New("hubedge: the edge runs on Linux and macOS only")
}
