//go:build windows

package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"golang.org/x/sys/windows"

	"github.com/nwverse/persona/server/internal/link"
	"github.com/nwverse/persona/server/internal/linktray"
)

// `persona-link tray`: the Windows tray (internal/linktray, docs/operations/windows.md "Tray"). It runs from the console-less
// persona-link-service.exe, started at sign-in by the Run entry `install` writes, and by Windows for pairing links.
// What its buttons do is done here, the same way the commands do it.

func trayCmd(e *env, args []string) error {
	var open string
	for i := 0; i < len(args); i++ {
		v, next, ok, err := flagValue(args, i, "--open")
		switch {
		case err != nil:
			return err
		case !ok:
			return errUsage
		}
		open, i = v, next
	}
	hideConsoles(e)
	return linktray.Run(linktray.Options{Host: trayHost{e}, Exe: e.trayExe(), Open: strings.TrimSpace(open)})
}

// hideConsoles: the tray has no console, so every program it runs (schtasks) would open a window of its own.
func hideConsoles(e *env) {
	e.command = func(name string, args ...string) (string, error) {
		cmd := exec.Command(name, args...)
		cmd.Env = e.commandEnv()
		cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: windows.CREATE_NO_WINDOW}
		out, err := cmd.CombinedOutput()
		return strings.TrimSpace(string(out)), err
	}
}

// trayExe is what starts the tray: the installed console-less copy, else this program.
func (e *env) trayExe() string {
	if fileExists(e.serviceExePath()) {
		return e.serviceExePath()
	}
	self, _ := e.exe()
	return self
}

type trayHost struct{ e *env }

func (h trayHost) DataDir() string { return h.e.personaDir() }
func (h trayHost) Version() string { return version }

func (h trayHost) Snapshot() linktray.Snapshot {
	path := h.e.configPath()
	s := linktray.Snapshot{Now: h.e.now()}
	if c, err := link.Read(path); err == nil {
		s.Config = c
	}
	if st, err := link.ReadStatus(path); err == nil {
		s.Status = st
		s.Running = st.State != "stopped" && linkAlive(st.PID)
	}
	return s
}

// linkAlive says whether pid is a running persona-link: alive, and that program (a pid is reused once it exits).
// Read from outside, without its run lock: link.Running takes the lock for an instant, which a persona-link starting
// at that instant would trip on, and the tray asks every few seconds.
func linkAlive(pid int) bool {
	if pid <= 0 || pid == os.Getpid() {
		return false
	}
	p, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return false
	}
	defer windows.CloseHandle(p)
	var code uint32
	if windows.GetExitCodeProcess(p, &code) != nil || code != 259 { // STILL_ACTIVE
		return false
	}
	buf := make([]uint16, windows.MAX_PATH)
	n := uint32(len(buf))
	if windows.QueryFullProcessImageName(p, 0, &buf[0], &n) != nil {
		return false
	}
	name := strings.ToLower(filepath.Base(windows.UTF16ToString(buf[:n])))
	return strings.HasPrefix(name, "persona-link") || strings.HasPrefix(name, legacyLinkBinary)
}

func (h trayHost) TaskState() linktray.Task {
	st, ok := h.e.taskStatus()
	if !ok {
		return linktray.Task{}
	}
	return linktray.Task{Installed: true, State: st}
}

func (h trayHost) Reconnect() error {
	return link.Poke(h.e.configPath(), link.PokeRedial)
}

// RestartService ends the scheduled task (the supervisor, persona-link and the node with it) and starts it again.
func (h trayHost) RestartService() error {
	e := h.e
	if _, ok := e.taskStatus(); !ok {
		return linktray.ErrNoService
	}
	e.stopTask()
	if out, err := e.schtasks("/Run", "/TN", taskName); err != nil {
		return fmt.Errorf("schtasks /Run: %s", orErr(out, err))
	}
	return nil
}

// InstallService runs `persona-link install`, as a person would: it quits this tray to replace the program, and
// starts the new one when the task is in.
func (h trayHost) InstallService() error {
	e := h.e
	prog := e.binPath()
	if !fileExists(prog) {
		self, err := e.exe()
		if err != nil {
			return err
		}
		if strings.EqualFold(filepath.Base(self), serviceExe) {
			return linktray.ErrInstallInTerminal
		}
		prog = self
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, prog, "install")
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: windows.CREATE_NO_WINDOW}
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("persona-link install: %s", orErr(strings.TrimSpace(string(lastLines(out, 3))), err))
	}
	return nil
}

func (h trayHost) Pair(raw, machine string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	c, err := link.Pair(ctx, strings.TrimSpace(raw), strings.TrimSpace(machine), "", h.e.configPath())
	if err != nil {
		return "", pairError(err)
	}
	_ = link.Poke(h.e.configPath(), link.PokeCheck) // a running persona-link moves to the new credential within seconds anyway, connected or not
	return c.Machine, nil
}

func (h trayHost) PairListen(listen string) (linktray.Invitation, error) {
	inv, err := link.PairListen(h.e.configPath(), strings.TrimSpace(listen), nil, h.e.now())
	if err != nil {
		return linktray.Invitation{}, err
	}
	return linktray.Invitation{Link: inv.Link, Listen: inv.Listen, Addrs: inv.Addrs}, nil
}

func (h trayHost) SetMode(mode string) error {
	_, err := link.SetMode(h.e.configPath(), mode, "")
	return err
}

func (h trayHost) AddAddress(raw string) error {
	_, err := link.AddAddress(h.e.configPath(), raw)
	return err
}

func (h trayHost) RemoveAddress(url string) error {
	return link.RemoveAddress(h.e.configPath(), url)
}

func (h trayHost) Rename(name string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), nameWait)
	defer cancel()
	return link.RequestRename(ctx, h.e.configPath(), name)
}

// systemTray is the tray of this computer: its entries in the user's registry and the running tray.
func systemTray() trayControl {
	return trayControl{setAutostart: linktray.SetAutostart, autostart: linktray.Autostart,
		registerLinks: linktray.RegisterLinks, unregisterLinks: linktray.UnregisterLinks,
		start: startTrayProcess, quit: linktray.Quit, running: linktray.Running, removeData: linktray.RemoveData}
}

// startTrayProcess runs the tray from exe, not in a job of whoever ran install (a terminal's, the old tray's): the
// tray outlives it. A job that allows no breakaway refuses the flag; then it is started inside that job.
func startTrayProcess(exe string) error {
	cmd := exec.Command(exe, "tray")
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: windows.CREATE_BREAKAWAY_FROM_JOB | windows.CREATE_NEW_PROCESS_GROUP}
	err := cmd.Start()
	if err != nil {
		cmd = exec.Command(exe, "tray")
		cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: windows.CREATE_NEW_PROCESS_GROUP}
		err = cmd.Start()
	}
	if err != nil {
		return err
	}
	return cmd.Process.Release()
}
