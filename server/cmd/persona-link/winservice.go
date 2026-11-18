package main

import (
	"encoding/binary"
	"encoding/csv"
	"errors"
	"fmt"
	"html"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf16"

	"github.com/nwverse/persona/server/internal/i18n"
	"github.com/nwverse/persona/server/internal/link"
)

// On Windows persona-link runs from a scheduled task of the user's own (docs/operations/windows.md "Install and start at logon"): `install`
// writes the task, registers it with schtasks and starts it; `uninstall` takes it away. The task starts at sign-in
// in the user's session and needs no administrator: Task Scheduler refuses a standard user a task that runs without
// a sign-in (S4U), and the node has to see the user's Orca and its agents anyway. What it runs is the supervisor
// (`persona-link supervise`, supervise_windows.go) from a console-less copy of persona-link, which keeps persona-link and
// the resident node running: the one task stands for both systemd units of Linux.

const (
	taskName        = "Persona Link"
	serviceExe      = "persona-link-service.exe"
	taskRepeat      = "PT5M" // the task is started again this often: a no-op while the supervisor runs
	nodeLogMax      = 10 << 20
	taskStopTimeout = 10 * time.Second
)

func (e *env) taskFile() string {
	return existingPath(filepath.Join(e.personaDir(), "persona-link-task.xml"), filepath.Join(e.personaDir(), legacyTaskFile))
}
func (e *env) serviceExePath() string {
	dir := filepath.Join(e.personaDir(), "bin")
	return existingPath(filepath.Join(dir, serviceExe), filepath.Join(dir, legacyServiceExe))
}
func (e *env) serviceLog() string { return filepath.Join(e.personaDir(), "link-service.log") }

// exeName is a program's file name on this computer.
func (e *env) exeName(name string) string {
	if e.goos == "windows" {
		return name + ".exe"
	}
	return name
}

// taskXML is the scheduled task. Started at the user's sign-in, and every taskRepeat from the install on (a time
// trigger: a logon trigger's repetition would only begin at the next sign-in) with IgnoreNew: a supervisor that ended
// for any reason is back within minutes, and a running one is left alone. Task Scheduler's own restart on failure
// does not help: it is for a task that could not be started, not for a program that exited with an error. No time
// limit (the default ends a task after 72 hours), not stopped on battery or when idle ends, and normal priority (the
// default 7 is below normal, for CPU and disk alike).
func taskXML(user, command, args string, installed time.Time) string {
	x := html.EscapeString
	return `<?xml version="1.0" encoding="UTF-16"?>
<Task version="1.2" xmlns="http://schemas.microsoft.com/windows/2004/02/mit/task">
  <RegistrationInfo>
    <Description>Persona link: keeps this computer reachable from the Persona hub and runs the resident node. Written by persona-link install; persona-link uninstall removes it.</Description>
  </RegistrationInfo>
  <Triggers>
    <LogonTrigger>
      <UserId>` + x(user) + `</UserId>
    </LogonTrigger>
    <TimeTrigger>
      <Repetition>
        <Interval>` + taskRepeat + `</Interval>
      </Repetition>
      <StartBoundary>` + installed.Format("2006-01-02T15:04:05") + `</StartBoundary>
    </TimeTrigger>
  </Triggers>
  <Principals>
    <Principal id="Author">
      <UserId>` + x(user) + `</UserId>
      <LogonType>InteractiveToken</LogonType>
      <RunLevel>LeastPrivilege</RunLevel>
    </Principal>
  </Principals>
  <Settings>
    <MultipleInstancesPolicy>IgnoreNew</MultipleInstancesPolicy>
    <DisallowStartIfOnBatteries>false</DisallowStartIfOnBatteries>
    <StopIfGoingOnBatteries>false</StopIfGoingOnBatteries>
    <StartWhenAvailable>true</StartWhenAvailable>
    <IdleSettings>
      <StopOnIdleEnd>false</StopOnIdleEnd>
      <RestartOnIdle>false</RestartOnIdle>
    </IdleSettings>
    <ExecutionTimeLimit>PT0S</ExecutionTimeLimit>
    <Priority>5</Priority>
  </Settings>
  <Actions Context="Author">
    <Exec>
      <Command>` + x(command) + `</Command>
      <Arguments>` + x(args) + `</Arguments>
    </Exec>
  </Actions>
</Task>
`
}

// writeUTF16 writes s as UTF-16LE with a byte order mark: what schtasks /XML reads whatever the user's code page.
func writeUTF16(path, s string) error {
	u := utf16.Encode([]rune(s))
	b := make([]byte, 2+2*len(u))
	b[0], b[1] = 0xff, 0xfe
	for i, c := range u {
		binary.LittleEndian.PutUint16(b[2+2*i:], c)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o600)
}

func readUTF16(path string) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	if len(b) < 2 || b[0] != 0xff || b[1] != 0xfe || len(b)%2 != 0 {
		return "", fmt.Errorf("%s: not UTF-16LE", path)
	}
	u := make([]uint16, (len(b)-2)/2)
	for i := range u {
		u[i] = binary.LittleEndian.Uint16(b[2+2*i:])
	}
	return string(utf16.Decode(u)), nil
}

// taskRunsNode says whether the installed task's supervisor starts the resident node too.
func (e *env) taskRunsNode() bool {
	s, err := readUTF16(e.taskFile())
	return err == nil && strings.Contains(s, "<Arguments>supervise</Arguments>")
}

func (e *env) schtasks(args ...string) (string, error) { return e.command("schtasks", args...) }

// taskStatus is the task's state as schtasks words it in the user's language (Ready, Running…); ok is false when
// the task is not registered.
func (e *env) taskStatus() (status string, ok bool) {
	out, err := e.schtasks("/Query", "/TN", taskName, "/FO", "CSV", "/NH")
	if err != nil {
		return "", false
	}
	rec, err := csv.NewReader(strings.NewReader(firstLine(out))).Read()
	if err != nil || len(rec) < 3 {
		return "", true
	}
	return rec[2], true
}

// stopTask ends the running task, which ends the supervisor and, with it, persona-link and the node (their job dies
// with the supervisor), and waits for the link's run lock to come free.
func (e *env) stopTask() {
	if _, ok := e.taskStatus(); !ok {
		return
	}
	_, _ = e.schtasks("/End", "/TN", taskName)
	for deadline := e.now().Add(taskStopTimeout); link.Running(e.configPath()) && e.now().Before(deadline); {
		e.sleep(200 * time.Millisecond)
	}
}

// installNodeBinary puts the resident node at ~/.persona/bin/personad.exe. Unlike on Linux a given --personad replaces
// one already there: Windows has no node update over the link yet (docs/operations/node-update.md), so installing again is how
// a Windows node is upgraded.
func (e *env) installNodeBinary(from string) error {
	bin := e.nodeBin()
	if from == "" {
		if !fileExists(bin) {
			return link.NewTextError("link-cli.windows_node_binary_required", nil, i18n.Args{"path": bin})
		}
		return nil
	}
	if a, err := os.Stat(from); err != nil {
		return err
	} else if b, err := os.Stat(bin); err == nil && os.SameFile(a, b) {
		return nil
	}
	if err := copyExecutable(from, bin); err != nil {
		return link.NewTextError("link-cli.binary_install_failed", err, i18n.Args{"path": bin})
	}
	fmt.Fprintln(e.stdout, terminalText("link-cli.binary_installed", i18n.Args{"path": bin}))
	return nil
}

// installTask is install's Windows part, after the pairing: stop the task if it runs (the programs it runs cannot be
// replaced while they run), put the programs in place, register the task and start it, then wait for the link.
func installTask(e *env, withNode bool, from string, invite *link.PairInvite) error {
	if e.user == "" {
		return link.NewTextError("link-cli.windows_user_unknown", nil)
	}
	var oldPID int
	if st, err := link.ReadStatus(e.configPath()); err == nil && link.Running(e.configPath()) {
		oldPID = st.PID
	}
	e.stopTask()
	if link.Running(e.configPath()) {
		fmt.Fprintln(e.stdout, terminalText("link-cli.windows_other_process"))
	}
	bin, copied, err := e.installBinary()
	if err != nil {
		return link.NewTextError("link-cli.binary_install_failed", err, i18n.Args{"path": e.binPath()})
	}
	if copied {
		fmt.Fprintln(e.stdout, terminalText("link-cli.binary_installed", i18n.Args{"path": bin}))
	}
	args := "supervise"
	if withNode {
		if err := e.installNodeBinary(from); err != nil {
			fmt.Fprintln(e.stdout, terminalText("link-cli.windows_node_install_failed", i18n.Args{"error": link.ErrorText(i18n.Terminal(), err)}))
			withNode = false
		}
	}
	if !withNode {
		args += " --no-node"
	}
	if err := guiCopy(bin, e.serviceExePath()); err != nil {
		return link.NewTextError("link-cli.binary_install_failed", err, i18n.Args{"path": e.serviceExePath()})
	}
	file := e.taskFile()
	if err := writeUTF16(file, taskXML(e.user, e.serviceExePath(), args, e.now())); err != nil {
		return err
	}
	if out, err := e.schtasks("/Create", "/TN", taskName, "/XML", file, "/F"); err != nil {
		return link.NewTextError("link-cli.schtasks_create_failed", nil, i18n.Args{"error": orErr(out, err)})
	}
	started := e.now()
	if out, err := e.schtasks("/Run", "/TN", taskName); err != nil {
		return link.NewTextError("link-cli.schtasks_run_failed", nil, i18n.Args{"error": orErr(out, err)})
	}
	what := "persona-link"
	if withNode {
		what = terminalText("link-cli.windows_task_programs")
	}
	fmt.Fprintln(e.stdout, terminalText("link-cli.windows_task_installed", i18n.Args{"task": taskName, "path": file, "programs": what, "log": e.personaDir()}))
	return waitStarted(e, started, oldPID, invite)
}

// uninstallTask is uninstall's Windows part: end and delete the task, remove its file and the console-less copy.
func uninstallTask(e *env) {
	_, registered := e.taskStatus()
	if !registered && !fileExists(e.taskFile()) {
		fmt.Fprintln(e.stdout, terminalText("link-cli.windows_task_missing", i18n.Args{"task": taskName}))
		return
	}
	if registered {
		e.stopTask()
		if out, err := e.schtasks("/Delete", "/TN", taskName, "/F"); err != nil {
			fmt.Fprintln(e.stdout, terminalText("link-cli.windows_task_delete_error", i18n.Args{"error": orErr(out, err)}))
		}
	}
	_ = os.Remove(e.taskFile())
	// The supervisor may take a moment to go after the task ends; its file cannot be removed while it runs.
	for deadline := e.now().Add(taskStopTimeout); ; e.sleep(200 * time.Millisecond) {
		if err := os.Remove(e.serviceExePath()); err == nil || errors.Is(err, os.ErrNotExist) || !e.now().Before(deadline) {
			break
		}
	}
	fmt.Fprintln(e.stdout, terminalText("link-cli.windows_task_removed", i18n.Args{"task": taskName}))
}

// taskState is status's line about the scheduled task.
func (e *env) taskState() *serviceState {
	s := &serviceState{Manager: "schtasks", Unit: taskName, File: e.taskFile()}
	s.Installed = fileExists(s.File)
	if st, ok := e.taskStatus(); ok {
		s.Active = st
		if st == "" {
			s.Active = "registered"
		}
	}
	return s
}

// windowsTaskActiveText localizes Link's own fallback, never the status words returned by schtasks.
func windowsTaskActiveText(active string) string {
	if active == "registered" {
		return terminalText("link-cli.windows_task_registered")
	}
	return active
}

// taskText is the report's line about the scheduled task.
func taskText(s *serviceState, line func(string, ...any)) {
	switch {
	case s.Active == "" && !s.Installed:
		line("%s", terminalText("link-cli.service_not_installed"))
	case s.Active == "":
		line("%s", terminalText("link-cli.windows_service_unregistered", i18n.Args{"task": s.Unit, "path": s.File}))
	default:
		line("%s", terminalText("link-cli.windows_service_installed", i18n.Args{"task": s.Unit, "active": windowsTaskActiveText(s.Active)}))
	}
}
