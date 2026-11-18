//go:build !darwin

package osproc

// newAcceptor is the listener itself: elsewhere Go accepts a connection close-on-exec in one call (accept4 with
// SOCK_CLOEXEC), or on Windows as a socket no process inherits.
func newAcceptor(ln socketListener) (acceptor, error) { return ln, nil }
