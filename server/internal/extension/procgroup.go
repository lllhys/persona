package extension

import (
	"errors"
	"os/exec"
	"runtime"
	"time"

	"github.com/nwverse/persona/server/internal/osproc"
)

// A market git or a tool check runs as the leader of a process group: at its deadline the whole group is stopped, so a
// helper it started that shares its output (git-remote-https or ssh share git's) neither keeps the call waiting
// until it exits, which on a stalled remote can be hours, nor outlives the call. The group is a session of its own,
// without a controlling terminal, so nothing in it can stop on reading one (osproc.SessionGroupAttr).

// outputWait is how long a call still reads the output once its program has exited or been stopped.
const outputWait = time.Second

// inOwnGroup makes cmd, from exec.CommandContext, stop its whole group when the context ends, and stop waiting for
// its output outputWait after its program has exited or been stopped.
func inOwnGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = osproc.SessionGroupAttr()
	cmd.Cancel = func() error { return osproc.StopGroup(cmd.Process.Pid, true) }
	cmd.WaitDelay = outputWait
}

// endGroup stops what is left of cmd's group once cmd has run, and answers cmd's err, with exec.ErrWaitDelay as
// success: the program exited 0 and only a helper it left kept the output open.
func endGroup(cmd *exec.Cmd, err error) error {
	if cmd.Process != nil && runtime.GOOS != "windows" {
		// The group outlives its leader while a member is left; already empty is ESRCH. Not on Windows: there the
		// stop is taskkill /T by the leader's pid, which is gone (and may be reused) once it has exited.
		_ = osproc.StopGroup(cmd.Process.Pid, true)
	}
	if errors.Is(err, exec.ErrWaitDelay) {
		return nil
	}
	return err
}

// headBuffer keeps the first max bytes written to it and drops the rest. A write never fails, so a program that
// prints more than that is not stopped by a short write.
type headBuffer struct {
	buf []byte
	max int
}

func (h *headBuffer) Write(p []byte) (int, error) {
	if room := h.max - len(h.buf); room > 0 {
		h.buf = append(h.buf, p[:min(room, len(p))]...)
	}
	return len(p), nil
}
