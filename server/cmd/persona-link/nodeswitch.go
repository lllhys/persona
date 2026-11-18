package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"log"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/nwverse/persona/server/internal/identity"
	"github.com/nwverse/persona/server/internal/link"
)

// The device's program (docs/operations/link.md "The device's program"): a computer that is a device runs persona-node as its
// resident node, at <data dir>/bin/persona-node, beside personad (the hub's and the manager's program) when it has
// that too. A new node service runs persona-node when the computer has it (nodeBin); one that runs personad already
// keeps running it until `persona-link node switch` moves it over, whole or not at all.

const (
	programPersonad    = "personad"
	programPersonaNode = "persona-node"
)

func (e *env) personadBin() string {
	dir := filepath.Join(e.personaDir(), "bin")
	return existingPath(filepath.Join(dir, e.exeName(programPersonad)), filepath.Join(dir, e.exeName(legacyNodeBinary)))
}
func (e *env) personaNodeBin() string {
	return filepath.Join(e.personaDir(), "bin", e.exeName(programPersonaNode))
}

// nodeBin is the program a new node service runs: the device's own persona-node where this computer has it, else
// personad. A computer that runs a hub or a manager too keeps its node on the personad it has (hostsHubOrManager).
func (e *env) nodeBin() string {
	if fileExists(e.personadBin()) && e.hostsHubOrManager() {
		return e.personadBin()
	}
	if p := e.personaNodeBin(); e.programOf(p) == programPersonaNode {
		return p
	}
	return e.personadBin()
}

// programOf asks the file at path which program it is (node-update-check prints it; a build that does not say is
// personad); "" when there is no such file or it does not run here.
func (e *env) programOf(path string) string {
	if !fileExists(path) {
		return ""
	}
	out, err := e.command(path, "node-update-check")
	if err != nil {
		return ""
	}
	var check struct {
		Program string `json:"program"`
	}
	if json.Unmarshal([]byte(strings.TrimSpace(out)), &check) != nil {
		return ""
	}
	return nodeProgramName(check.Program)
}

// Older nodes omit their program or report the exact legacy daemon identifier.
// Unknown identifiers stay unknown; a filename is never a program assertion.
func nodeProgramName(program string) string {
	if program == "" || program == legacyNodeBinary {
		return programPersonad
	}
	return program
}

// hostsHubOrManager: this computer runs a hub or a manager too (their files are in its data directory). Their services
// run personad, which updates with the node while the node service runs it too: such a computer's node stays on
// personad.
func (e *env) hostsHubOrManager() bool {
	if fileExists(filepath.Join(e.personaDir(), "hub.json")) {
		return true
	}
	found, _ := filepath.Glob(filepath.Join(e.personaDir(), "managers", "*", "manager.json"))
	return len(found) > 0
}

// switchBackup is where node switch keeps the service file it replaced until the new node answers; one left there
// means a switch that did not finish (recoverNodeSwitch).
func (e *env) switchBackup() string { return filepath.Join(e.personaDir(), "node-switch.backup") }

// serviceProgram is the program a node service file runs (its ExecStart's or ProgramArguments' first word).
func (e *env) serviceProgram(content string) string {
	switch e.goos {
	case "linux":
		for _, line := range strings.Split(content, "\n") {
			if rest, ok := strings.CutPrefix(strings.TrimSpace(line), "ExecStart="); ok {
				rest = strings.TrimSpace(rest)
				if !strings.HasPrefix(rest, `"`) {
					word, _, _ := strings.Cut(rest, " ")
					return word
				}
				// systemdQuote's word: backslash escapes inside the quotes, %% and $$ for % and $.
				var b strings.Builder
				for i := 1; i < len(rest); i++ {
					switch c := rest[i]; {
					case c == '\\' && i+1 < len(rest):
						i++
						b.WriteByte(rest[i])
					case c == '"':
						return strings.NewReplacer("%%", "%", "$$", "$").Replace(b.String())
					default:
						b.WriteByte(c)
					}
				}
				return ""
			}
		}
	case "darwin":
		_, rest, ok := strings.Cut(content, "<key>ProgramArguments</key>")
		if !ok {
			return ""
		}
		_, rest, ok = strings.Cut(rest, "<string>")
		if !ok {
			return ""
		}
		word, _, _ := strings.Cut(rest, "</string>")
		return html.UnescapeString(word)
	}
	return ""
}

// retarget is content with the program it runs, from, replaced by to (the service's other settings stay as they
// are, whoever wrote them); ok false when from is not there in the form this file names it.
func (e *env) retarget(content, from, to string) (string, bool) {
	var old, repl string
	switch e.goos {
	case "linux":
		old, repl = "ExecStart="+systemdQuote(from)+" ", "ExecStart="+systemdQuote(to)+" "
		if !strings.Contains(content, old) {
			old, repl = "ExecStart="+from+" ", "ExecStart="+systemdQuote(to)+" "
		}
	case "darwin":
		old, repl = "<string>"+html.EscapeString(from)+"</string><string>node</string>", "<string>"+html.EscapeString(to)+"</string><string>node</string>"
	default:
		return "", false
	}
	if !strings.Contains(content, old) {
		return "", false
	}
	return strings.Replace(content, old, repl, 1), true
}

// bootstrapLaunchd loads a service file after its old job was booted out. Bootstrap right after bootout often fails
// for a moment while the old job is still going away, so try five times, one second apart.
func (e *env) bootstrapLaunchd(domain, file string) (out string, err error) {
	for try := 0; try < 5; try++ {
		if out, err = e.command("launchctl", "bootstrap", domain, file); err == nil {
			return out, nil
		}
		e.sleep(time.Second)
	}
	return out, err
}

// restartNodeService starts the node service again from its file as it is now.
func (e *env) restartNodeService(file string) error {
	switch e.goos {
	case "linux":
		for _, step := range [][]string{{"daemon-reload"}, {"restart", e.residentUnitName()}} {
			if out, err := e.systemctl(step...); err != nil {
				return fmt.Errorf("systemctl --user %s: %s", strings.Join(step, " "), orErr(out, err))
			}
		}
	case "darwin":
		domain := "gui/" + strconv.Itoa(e.uid)
		_, _ = e.command("launchctl", "bootout", domain+"/"+e.residentLabel())
		out, err := e.bootstrapLaunchd(domain, file)
		if err != nil {
			return fmt.Errorf("launchctl bootstrap: %s", orErr(out, err))
		}
	}
	return nil
}

// nodeAnswers waits up to wait for this computer's resident node to answer a ping on node.sock as program (either
// program when program is ""), and says what answered last; it gives up when ctx ends. The ping changes nothing and
// does not touch the hub's connection (it is not the attach socket).
var nodeAnswers = func(ctx context.Context, e *env, program string, wait time.Duration) (bool, string) {
	sock := filepath.Join(identity.SocketDir(e.home), "node.sock")
	deadline := e.now().Add(wait)
	last := "no answer on " + sock
	for {
		if got, err := pingNode(sock); err != nil {
			last = err.Error()
		} else {
			last = "it answers as " + got
			if got == program || program == "" {
				return true, last
			}
		}
		if !e.now().Before(deadline) || ctx.Err() != nil {
			return false, last
		}
		e.sleep(time.Second)
	}
}

// pingNode asks the node on sock which program answers.
func pingNode(sock string) (string, error) {
	c, err := net.DialTimeout("unix", sock, 2*time.Second)
	if err != nil {
		return "", err
	}
	defer c.Close()
	_ = c.SetDeadline(time.Now().Add(3 * time.Second))
	if _, err := c.Write([]byte(`{"kind":"ping"}` + "\n")); err != nil {
		return "", err
	}
	line, err := bufio.NewReader(c).ReadBytes('\n')
	if err != nil {
		return "", err
	}
	var pong struct {
		OK      bool   `json:"ok"`
		Program string `json:"program"`
	}
	if json.Unmarshal(line, &pong) != nil || !pong.OK {
		return "", fmt.Errorf("not a node's answer: %q", strings.TrimSpace(string(line)))
	}
	return nodeProgramName(pong.Program), nil
}

// switchWait is how long node switch waits for the node to answer after a restart (launchd and systemd restart a node
// that exits at once after 10 s).
var switchWait = 60 * time.Second

// restartGrace is how long a rollback whose restart was refused looks for a node that runs all the same.
var restartGrace = 5 * time.Second

// writeDurable puts content at path whole or not at all, on disk before it returns (file and directory synced): a
// backup that a power cut left empty would be worse than none.
func writeDurable(path, content string) error {
	tmp := path + ".tmp"
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	_, err = f.WriteString(content)
	if err == nil {
		err = f.Sync()
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Rename(tmp, path)
	}
	if err != nil {
		os.Remove(tmp)
		return err
	}
	if d, err := os.Open(filepath.Dir(path)); err == nil {
		_ = d.Sync()
		d.Close()
	}
	return nil
}

// switchLock is held for the whole of a node switch: recovery leaves a backup alone while a switch runs, and a second
// switch waits for none.
func (e *env) switchLock() string { return filepath.Join(e.personaDir(), "node-switch.lock") }

// errSwitchRunning: another persona-link is switching the node service now.
var errSwitchRunning = errors.New("another persona-link is switching the node service now: try again when it is done")

// nodeSwitch is `persona-link node switch [--to personad]`: the node service runs the other program from now on
// (persona-node, the device's own, by default). The old service file is kept beside it until the new node answers;
// when it does not answer within switchWait, or the switch is stopped, the old file comes back and the old program runs
// again: the computer always ends with a node.
func nodeSwitch(parent context.Context, e *env, to string) error {
	if e.goos != "linux" && e.goos != "darwin" {
		return fmt.Errorf("node switch works on Linux and macOS (on %s the node is part of persona-link's own service)", e.goos)
	}
	file := e.nodeServiceFile()
	if !fileExists(file) {
		return errors.New("there is no node service to switch: persona-link node install")
	}
	release, err := link.LockFile(e.switchLock(), false)
	if errors.Is(err, link.ErrLocked) {
		return errSwitchRunning
	} else if err != nil {
		return err
	}
	defer release()
	if _, err := recoverLocked(e); err != nil {
		return err
	}
	var target string
	switch to {
	case programPersonaNode:
		if e.hostsHubOrManager() {
			return fmt.Errorf("this computer runs a hub or a manager too (%s): their personad updates with the node, so the node service stays on personad", e.personaDir())
		}
		target = e.personaNodeBin()
		if e.programOf(target) != programPersonaNode {
			if err := e.placeBundledNode(); err != nil {
				return err
			}
		}
	case programPersonad:
		target = e.personadBin()
		if e.programOf(target) != programPersonad {
			return fmt.Errorf("no personad that runs here at %s: install it first (the install command with --hub, or the manager's install command)", target)
		}
	default:
		return errUsage
	}
	b, err := os.ReadFile(file)
	if err != nil {
		return err
	}
	old := string(b)
	from := e.serviceProgram(old)
	if from == target {
		fmt.Fprintf(e.stdout, "The node service already runs %s.\n", target)
		return nil
	}
	next, ok := e.retarget(old, from, target)
	if from == "" || !ok {
		return fmt.Errorf("could not tell which program %s runs: switch it by hand", file)
	}
	if err := e.macOutsideDesktop(); err != nil {
		return err
	}
	if err := writeDurable(e.switchBackup(), old); err != nil {
		return fmt.Errorf("could not keep the service as it is: %w", err)
	}
	// Stopped half way (Ctrl-C, the terminal closing, a TERM): the old service comes back first, here.
	ctx, stop := signal.NotifyContext(parent, os.Interrupt, syscall.SIGTERM, syscall.SIGHUP)
	defer stop()
	if err := writeDurable(file, next); err != nil {
		_ = os.Remove(e.switchBackup())
		return fmt.Errorf("could not write %s: %w", file, err)
	}
	fmt.Fprintf(e.stdout, "Switching the node service to %s (it restarts)…\n", target)
	why := ""
	if err := e.restartNodeService(file); err != nil {
		why = err.Error()
	} else if ok, last := nodeAnswers(ctx, e, to, switchWait); !ok {
		why = filepath.Base(target) + " did not answer in " + switchWait.String() + " (" + last + ")"
	}
	if ctx.Err() != nil {
		why = "stopped"
	}
	if why == "" {
		_ = os.Remove(e.switchBackup())
		fmt.Fprintf(e.stdout, "The node service runs %s now; %s stays for anything else that runs it.\n", target, from)
		return nil
	}
	_, err = rollbackNodeSwitch(e, old, why)
	return err
}

// macOutsideDesktop refuses a restart of the node's launchd agent from outside the Mac's desktop session (over ssh):
// launchd there may take the agent away (bootout) and then refuse to load one again (bootstrap into gui/<uid>), the old
// one as much as the new, which would leave no node. An answer that says nothing (or none) does not refuse.
func (e *env) macOutsideDesktop() error {
	if e.goos != "darwin" {
		return nil
	}
	if out, err := e.command("launchctl", "managername"); err == nil && out != "" && out != "Aqua" {
		return fmt.Errorf("this is not a session of the Mac's desktop (launchd's manager here: %s), where restarting the node service could stop it and not start it again: run this in Terminal on the Mac", out)
	}
	return nil
}

// placeBundledNode puts the persona-node that came with this persona-link (beside its own executable, as in the Persona
// Link Mac app) at bin/persona-node, when there is one.
func (e *env) placeBundledNode() error {
	target := e.personaNodeBin()
	exe, err := e.exe()
	if err == nil {
		if p, rerr := filepath.EvalSymlinks(exe); rerr == nil {
			exe = p
		}
		bundled := filepath.Join(filepath.Dir(exe), e.exeName(programPersonaNode))
		if bundled != target && e.programOf(bundled) == programPersonaNode {
			if err := copyExecutable(bundled, target); err != nil {
				return fmt.Errorf("could not put persona-node at %s: %w", target, err)
			}
			fmt.Fprintf(e.stdout, "Put the persona-node that came with Persona Link at %s.\n", target)
			return nil
		}
	}
	return fmt.Errorf("no persona-node that runs here at %s: install it first (the install command with --node)", target)
}

// rollbackNodeSwitch puts the service file from before the switch back and starts it again. restored: the node
// answers again; err says why the switch did not happen (and what is still wrong, if anything).
func rollbackNodeSwitch(e *env, old, why string) (restored bool, err error) {
	file := e.nodeServiceFile()
	if err := writeDurable(file, old); err != nil {
		return false, fmt.Errorf("%s, and the old service file could not be put back (%v): it is in %s", why, err, e.switchBackup())
	}
	// The backup stays until the service manager has started the old service: cut off before that, or with the start
	// refused (launchd can refuse a bootstrap for a while after a bootout), the next recovery (persona-link run checks
	// every half minute) does this again. Some node answering all the same means one runs: nothing to do again.
	if err := e.restartNodeService(file); err != nil {
		if ok, _ := nodeAnswers(context.Background(), e, "", restartGrace); ok {
			_ = os.Remove(e.switchBackup())
			return true, fmt.Errorf("%s: the old service file is back and a node answers (starting it said: %v)", why, err)
		}
		return false, fmt.Errorf("%s; the old service file is back, but it did not start again (%v): persona-link tries again later", why, err)
	}
	_ = os.Remove(e.switchBackup())
	if ok, last := nodeAnswers(context.Background(), e, "", switchWait); !ok {
		return false, fmt.Errorf("%s; the old service file is back and started, but the node does not answer yet (%s)", why, last)
	}
	return true, fmt.Errorf("%s: the node service runs %s again", why, e.serviceProgram(old))
}

// switchPending says a node switch did not finish: its backup is there while no switch holds the lock.
func (e *env) switchPending() bool {
	if !fileExists(e.switchBackup()) {
		return false
	}
	release, err := link.LockFile(e.switchLock(), false)
	if err != nil {
		return false // one is running, or the lock cannot be had: not ours to say
	}
	release()
	return true
}

// recoverNodeSwitch finishes a switch that stopped half way (its backup is still there and no switch is running): the
// old service file comes back and starts again. persona-link run does it when it starts and every half minute, and node
// install and switch before their own work; node uninstall drops the backup instead, and node status only says so.
func recoverNodeSwitch(e *env) error {
	if e.goos != "linux" && e.goos != "darwin" || !fileExists(e.switchBackup()) {
		return nil
	}
	release, err := link.LockFile(e.switchLock(), false)
	if errors.Is(err, link.ErrLocked) {
		return nil // a switch is running: it finishes or rolls back itself
	} else if err != nil {
		return err
	}
	defer release()
	_, err = recoverLocked(e)
	return err
}

// recoverLocked is recoverNodeSwitch with the switch lock held. A backup that is not a usable service file (empty, or
// naming a program that is not there) is dropped and the service left as it is: putting it back would leave no node.
func recoverLocked(e *env) (bool, error) {
	b, err := os.ReadFile(e.switchBackup())
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	} else if err != nil {
		return false, err
	}
	old := string(b)
	if prog := e.serviceProgram(old); prog == "" || !fileExists(prog) {
		_ = os.Remove(e.switchBackup())
		fmt.Fprintf(e.stdout, "Dropped %s, a node switch's backup that names no program here; the node service stays as it is.\n", e.switchBackup())
		return false, nil
	}
	if err := e.macOutsideDesktop(); err != nil { // the service and the backup stay as they are, for a session that can
		return false, fmt.Errorf("a node switch did not finish (%s), and %w", e.switchBackup(), err)
	}
	restored, err := rollbackNodeSwitch(e, old, "a node switch did not finish")
	if restored {
		fmt.Fprintln(e.stdout, err) // what happened; back to how it was
		return true, nil
	}
	return false, err
}

// lockedNodeCommand runs fn with the switch lock held (node install and uninstall, persona-link install's node step):
// none of them works on a service a switch is changing. An install (recover) first finishes a switch that did not; an
// uninstall, which takes the service away, drops that switch's backup once it is done instead (putting the old service
// back and starting it would undo the uninstall, and one that cannot be started again would block it).
func lockedNodeCommand(e *env, recover bool, fn func() error) error {
	if e.goos != "linux" && e.goos != "darwin" {
		return fn()
	}
	if err := os.MkdirAll(e.personaDir(), 0o700); err != nil {
		return err
	}
	release, err := link.LockFile(e.switchLock(), false)
	if errors.Is(err, link.ErrLocked) {
		return errSwitchRunning
	} else if err != nil {
		return err
	}
	defer release()
	if !recover {
		if err := fn(); err != nil {
			return err
		}
		if err := os.Remove(e.switchBackup()); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		return nil
	}
	if _, err := recoverLocked(e); err != nil {
		return err
	}
	return fn()
}

// watchSwitch is what persona-link run starts once it holds its lock: watchNodeSwitch for this computer, its messages
// and errors in the log. A variable for tests.
var watchSwitch = func(ctx context.Context) {
	e := newEnv()
	e.stdout = logLines{}
	watchNodeSwitch(ctx, e, 30*time.Second)
}

// logLines writes what is written to it to the log, a line at a time.
type logLines struct{}

func (logLines) Write(p []byte) (int, error) {
	for _, line := range strings.Split(strings.TrimRight(string(p), "\n"), "\n") {
		log.Print(line)
	}
	return len(p), nil
}

// watchNodeSwitch undoes a switch whose persona-link died half way (kill -9, a panic) while persona-link run keeps
// running: at start and then every while, recoverNodeSwitch (which leaves a running switch alone). An error goes to
// the log when it is new (one that stays is not written again every time).
func watchNodeSwitch(ctx context.Context, e *env, every time.Duration) {
	last := ""
	for {
		err := recoverNodeSwitch(e)
		if msg := fmt.Sprint(err); err != nil && msg != last {
			log.Printf("node switch: %v", err)
			last = msg
		} else if err == nil {
			last = ""
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(every):
		}
	}
}
