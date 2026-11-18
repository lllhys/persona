//go:build unix

package hubedge

import (
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/nwverse/persona/server/internal/procs"
)

// proc is one slot: a hub process the edge started.
type proc struct {
	name, binary string
	cmd          *exec.Cmd
	out          *lineWriter
	msgs         chan Msg // closed once the slot's pipe ends
	exited       chan struct{}
	exitCode     int
	hello        Msg
	sockets      map[string]string      // port -> path
	found        map[string]os.FileInfo // path -> the file already there when the slot started (not its own)
	started      time.Time
	released     atomic.Bool // the edge asked it to stop (release)
}

func (p *proc) send(m Msg) { _ = p.out.send(m) }

func (p *proc) alive() bool {
	select {
	case <-p.exited:
		return false
	default:
		return true
	}
}

// spawn starts binary as the next slot: standby waits for activate, otherwise it serves at once. It takes the name the
// slot that served last is not using, so a standby never shares the serving slot's sockets (nor removes them as it
// exits), however many standbys failed before it.
func (e *edge) spawn(binary string, standby bool) (*proc, error) {
	name := "a"
	e.mu.Lock()
	if e.served == "a" {
		name = "b"
	}
	e.mu.Unlock()
	p := &proc{name: name, binary: binary, msgs: make(chan Msg, 8), exited: make(chan struct{}),
		sockets: map[string]string{}, found: map[string]os.FileInfo{}}
	var socks string
	for _, ln := range e.o.Listeners {
		_, port, _ := net.SplitHostPort(ln.Addr().String())
		path := filepath.Join(e.o.RunDir, "slot-"+name+"-"+port+".sock")
		p.sockets[port] = path
		if fi, err := os.Lstat(path); err == nil {
			p.found[path] = fi
		}
		if socks != "" {
			socks += ";"
		}
		socks += port + "=" + path
	}
	toSlotR, toSlotW, err := os.Pipe()
	if err != nil {
		return nil, err
	}
	fromSlotR, fromSlotW, err := os.Pipe()
	if err != nil {
		_ = toSlotR.Close()
		_ = toSlotW.Close()
		return nil, err
	}
	role := "active"
	if standby {
		role = "standby"
	}
	env := procs.ChildEnv(append(os.Environ(), EnvSlot+"="+name, EnvRole+"="+role, EnvSockets+"="+socks))
	cmd := exec.Command(binary, e.o.Args...)
	cmd.Env = env
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	cmd.ExtraFiles = []*os.File{toSlotR, fromSlotW}       // fds 3 and 4 (slot.go)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true} // its own group: what it started goes with it (kill)
	err = cmd.Start()
	_ = toSlotR.Close()
	_ = fromSlotW.Close()
	if err != nil {
		_ = toSlotW.Close()
		_ = fromSlotR.Close()
		return nil, err
	}
	p.cmd, p.out, p.started = cmd, &lineWriter{w: toSlotW}, time.Now()
	go readMsgs(fromSlotR, p.msgs)
	go func() {
		_ = cmd.Wait()
		p.exitCode = cmd.ProcessState.ExitCode()
		_ = toSlotW.Close()
		_ = fromSlotR.Close()
		p.removeSockets()
		close(p.exited)
	}()
	e.logf("hub edge: slot %s started (%s, %s, pid %d)", name, binary, role, cmd.Process.Pid)
	return p, nil
}

// removeSockets removes the socket files p left behind, and only those: a file that was already there when p started
// and is still the same one (same inode and modification time: a file system may hand a freed inode out again) is not
// p's, since p never listened, so it stays.
func (p *proc) removeSockets() {
	for _, path := range p.sockets {
		now, err := os.Lstat(path)
		if err != nil {
			continue
		}
		if was, ok := p.found[path]; ok && os.SameFile(was, now) && was.ModTime().Equal(now.ModTime()) {
			continue
		}
		_ = os.Remove(path)
	}
}

// await reads p's messages until want: hello is kept on the way. A failed message, the slot exiting or the time
// running out is the error.
func (e *edge) await(p *proc, want string, within time.Duration) error {
	t := time.NewTimer(within)
	defer t.Stop()
	for {
		select {
		case m, ok := <-p.msgs:
			if !ok {
				<-p.exited
				return fmt.Errorf("it exited (code %d)", p.exitCode)
			}
			switch m.T {
			case MsgHello:
				p.hello = m
			case MsgFailed:
				return errors.New(m.Reason)
			}
			if m.T == want {
				return nil
			}
		case <-t.C:
			return fmt.Errorf("no %s within %s", want, within)
		case <-e.ctx.Done():
			return e.ctx.Err()
		}
	}
}

// kill ends p and what it started, at once.
func (e *edge) kill(p *proc) {
	if p == nil || !p.alive() {
		return
	}
	_ = syscall.Kill(-p.cmd.Process.Pid, syscall.SIGKILL)
	<-p.exited
}

// release asks p to stop the way a planned stop does and waits for it; past ReleaseWait it is killed.
func (e *edge) release(p *proc) {
	if p == nil || !p.alive() {
		return
	}
	p.released.Store(true)
	p.send(Msg{T: MsgRelease})
	select {
	case <-p.exited:
	case <-time.After(e.o.ReleaseWait):
		e.logf("hub edge: slot %s did not stop within %s: killed", p.name, e.o.ReleaseWait)
		e.kill(p)
	}
}

// startActive runs binary as a slot that serves at once and waits until it does.
func (e *edge) startActive(binary string) (*proc, error) {
	p, err := e.spawn(binary, false)
	if err != nil {
		return nil, err
	}
	if err := e.await(p, MsgReady, e.o.ActivateWait); err != nil {
		e.kill(p)
		return nil, err
	}
	e.logf("hub edge: slot %s serving (%s, build %d)", p.name, p.hello.Version, p.hello.Build)
	return p, nil
}
