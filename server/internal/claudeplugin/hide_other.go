//go:build !windows

package claudeplugin

import "os/exec"

func hideWindow(*exec.Cmd) {}
