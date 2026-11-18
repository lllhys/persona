//go:build !windows

package main

import "github.com/nwverse/persona/server/internal/link"

// supervise is Windows' (supervise_windows.go): elsewhere systemd or launchd restart what exits.
func supervise(*env, []string) error {
	return link.NewTextError("link-cli.supervise_not_windows", nil)
}
