//go:build unix

package hubedge

import (
	"bufio"
	"encoding/json"
	"errors"
	"net"
	"os"
	"time"

	"github.com/nwverse/persona/server/internal/acceptloop"
	"github.com/nwverse/persona/server/internal/osproc"
)

// listenControl serves `personad hub switch` on the control socket: owner only (0600 in a 0700 directory), the same
// trust as the hub's own files. A socket file nobody answers on is a dead edge's and is replaced.
func (e *edge) listenControl() (net.Listener, error) {
	if c, err := net.DialTimeout("unix", e.o.Control, time.Second); err == nil {
		_ = c.Close()
		return nil, errors.New("another edge answers on " + e.o.Control)
	}
	_ = os.Remove(e.o.Control)
	ln, err := osproc.Listen("unix", e.o.Control)
	if err != nil {
		return nil, err
	}
	if err := os.Chmod(e.o.Control, 0o600); err != nil {
		_ = ln.Close()
		return nil, err
	}
	go acceptloop.Run(e.ctx, ln, e.acceptLogf, func(c net.Conn) { go e.serveControl(c) })
	return ln, nil
}

func (e *edge) serveControl(c net.Conn) {
	defer c.Close()
	_ = c.SetReadDeadline(time.Now().Add(10 * time.Second))
	var m Msg
	line, err := bufio.NewReader(c).ReadBytes('\n')
	if err != nil || json.Unmarshal(line, &m) != nil {
		return
	}
	_ = c.SetReadDeadline(time.Time{})
	w := &lineWriter{w: c}
	switch m.T {
	case MsgStatus:
		st := Msg{T: MsgStatus, Proto: Proto, Edge: e.o.Version}
		if p := e.current(); p != nil {
			st.Version, st.Build, st.PID, st.Slot, st.Binary = p.hello.Version, p.hello.Build, p.cmd.Process.Pid, p.name, p.binary
		}
		_ = w.send(st)
	case MsgSwitch:
		if m.Kind == "" {
			m.Kind = KindDeploy
		}
		err := validBinary(m.Binary)
		if err == nil && m.Prev != "" {
			err = validBinary(m.Prev)
		}
		if err == nil {
			r := switchReq{m: m, done: make(chan error, 1), progress: func(phase string) { _ = w.send(Msg{T: MsgProgress, Phase: phase}) }}
			select {
			case e.reqs <- r:
				err = <-r.done
			case <-e.ctx.Done():
				err = errors.New("the edge is stopping")
			}
		}
		done := Msg{T: MsgDone, OK: err == nil}
		if err != nil {
			done.Reason = err.Error()
		}
		if p := e.current(); p != nil {
			done.Version, done.Build = p.hello.Version, p.hello.Build
		}
		_ = w.send(done)
	}
}
