//go:build !linux && !darwin

package machdiag

import (
	"context"
	"errors"
)

// errUnsupported: this system's processes and disk are not described (Windows); its tools still are.
var errUnsupported = errors.New("not available on this system")

func readProcesses(context.Context) ([]process, error) { return nil, errUnsupported }

func readMemTotal() int64 { return 0 }
