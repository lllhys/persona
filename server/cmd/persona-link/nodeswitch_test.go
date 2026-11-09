package main

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"log"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/compat"
	"github.com/nwverse/persona/server/internal/link"
	"github.com/nwverse/persona/server/internal/update/updatetest"
)

// deviceFiles puts the node programs a device can have in f's data dir (personad and persona-node each when asked)
// and has node-update-check answer for them as the real ones do.
func deviceFiles(t *testing.T, f *fakeEnv, personad, personaNode bool) {
	t.Helper()
	_ = os.MkdirAll(filepath.Join(f.personaDir(), "bin"), 0o755)
	if personad {
		_ = os.WriteFile(f.personadBin(), []byte("#!/bin/sh\n# personad\n"), 0o755)
	}
	if personaNode {
		_ = os.WriteFile(f.personaNodeBin(), []byte("#!/bin/sh\n# persona-node\n"), 0o755)
	}
	prev := f.onCommand
	f.onCommand = func(call string) (string, error) {
		switch call {
		case f.personaNodeBin() + " node-update-check":
			return `{"version":"v","os":"linux","arch":"amd64","program":"persona-node"}`, nil
		case f.personadBin() + " node-update-check":
			return `{"version":"v","os":"linux","arch":"amd64","program":"personad"}`, nil
		}
		if prev != nil {
			return prev(call)
		}
		return "", nil
	}
}

// answering makes nodeAnswers say program answered (or nothing did, for "") and records what it was asked to wait for.
func answering(t *testing.T, answers ...string) *[]string {
	t.Helper()
	old := nodeAnswers
	t.Cleanup(func() { nodeAnswers = old })
	var asked []string
	nodeAnswers = func(_ context.Context, _ *env, program string, _ time.Duration) (bool, string) {
		asked = append(asked, program)
		got := ""
		if len(answers) > 0 {
			got, answers = answers[0], answers[1:]
		}
		if got == "" {
			return false, "no answer"
		}
		return got == program || program == "", "it answers as " + got
	}
	return &asked
}

// A new node service runs the device's own program when the computer has it, else personad; a service that runs
// personad already stays as it is (and says how to switch).
func TestNodeInstallRunsPersonaNode(t *testing.T) {
	linuxPaths(t)
	f := newFakeEnv(t)
	deviceFiles(t, f, true, true)
	if err := nodeInstall(f.env, ""); err != nil {
		t.Fatal(err)
	}
	if unit := mustRead(t, f.nodeUnitPath()); !strings.Contains(unit, `ExecStart="`+f.personaNodeBin()+`" node --attach`) {
		t.Fatalf("unit:\n%s", unit)
	}
	if st := nodeStatus(f.env); st.Runs != f.personaNodeBin() || !st.HasBin {
		t.Fatalf("%+v", st)
	}

	// On a computer that runs a manager (or a hub) a new service runs the personad it has.
	m := newFakeEnv(t)
	deviceFiles(t, m, true, true)
	_ = os.MkdirAll(filepath.Join(m.personaDir(), "managers", "m-1"), 0o755)
	_ = os.WriteFile(filepath.Join(m.personaDir(), "managers", "m-1", "manager.json"), []byte("{}"), 0o600)
	if err := nodeInstall(m.env, ""); err != nil || !strings.Contains(mustRead(t, m.nodeUnitPath()), `"`+m.personadBin()+`"`) {
		t.Fatalf("manager computer: %v\n%s", err, mustRead(t, m.nodeUnitPath()))
	}

	// The Mac app hands its persona-node over: copied under its own name.
	g := newFakeEnv(t)
	from := filepath.Join(t.TempDir(), "helper")
	_ = os.WriteFile(from, []byte("#!/bin/sh\n# persona-node\n"), 0o755)
	g.onCommand = func(call string) (string, error) {
		if strings.HasSuffix(call, " node-update-check") && !strings.HasPrefix(call, g.personadBin()) {
			return `{"program":"persona-node"}`, nil
		}
		return "", nil
	}
	if err := nodeCmd(g.env, []string{"install", "--node-bin", from}); err != nil {
		t.Fatal(err)
	}
	if !fileExists(g.personaNodeBin()) || fileExists(g.personadBin()) ||
		!strings.Contains(mustRead(t, g.nodeUnitPath()), `"`+g.personaNodeBin()+`"`) {
		t.Fatalf("persona-node %v personad %v\n%s", fileExists(g.personaNodeBin()), fileExists(g.personadBin()), mustRead(t, g.nodeUnitPath()))
	}

	// A service that runs personad already: left alone, with the way over said.
	h := newFakeEnv(t)
	deviceFiles(t, h, true, false)
	if err := nodeInstall(h.env, ""); err != nil {
		t.Fatal(err)
	}
	deviceFiles(t, h, false, true)
	h.out.Reset()
	if err := nodeInstall(h.env, ""); err != nil || !strings.Contains(mustRead(t, h.nodeUnitPath()), `"`+h.personadBin()+`"`) ||
		!strings.Contains(h.out.String(), "persona-link node switch") {
		t.Fatalf("%v\n%s", err, h.out)
	}
}

// node switch moves a personad node service over to persona-node once persona-node answers, keeping the service's own
// settings; when it does not answer, the old service comes back and personad answers again.
func TestNodeSwitch(t *testing.T) {
	linuxPaths(t)
	for _, tc := range []struct {
		name    string
		answers []string
		ok      bool
	}{{"comes up", []string{"persona-node"}, true}, {"does not come up", []string{"", "personad"}, false}} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFakeEnv(t)
			deviceFiles(t, f, true, true)
			_ = os.MkdirAll(filepath.Dir(f.nodeUnitPath()), 0o755)
			old := strings.Replace(nodeUnitFile(f.personadBin(), f.nodeLog()), "RestartSec=10", "RestartSec=10\nNice=5", 1)
			_ = os.WriteFile(f.nodeUnitPath(), []byte(old), 0o644)
			asked := answering(t, tc.answers...)
			f.calls = nil
			err := nodeCmd(f.env, []string{"switch"})
			unit := mustRead(t, f.nodeUnitPath())
			if fileExists(f.switchBackup()) {
				t.Error("the backup stayed")
			}
			restarts := strings.Count(strings.Join(f.calls, "\n"), "systemctl --user restart personad-node.service")
			if tc.ok {
				if err != nil || !strings.Contains(unit, `ExecStart="`+f.personaNodeBin()+`" node --attach`) || !strings.Contains(unit, "Nice=5") ||
					restarts != 1 || strings.Join(*asked, ",") != "persona-node" {
					t.Fatalf("%v restarts %d asked %v\n%s", err, restarts, *asked, unit)
				}
				if !fileExists(f.personadBin()) {
					t.Fatal("personad went")
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), "runs "+f.personadBin()+" again") || unit != old || restarts != 2 ||
				strings.Join(*asked, ",") != "persona-node," {
				t.Fatalf("%v restarts %d asked %q\n%s", err, restarts, *asked, unit)
			}
		})
	}
}

// node switch refuses when there is nothing to switch to, when the computer runs a hub or a manager (their personad
// updates with the node), and says so when it runs persona-node already.
func TestNodeSwitchRefuses(t *testing.T) {
	linuxPaths(t)
	f := newFakeEnv(t)
	answering(t)
	if err := nodeCmd(f.env, []string{"switch"}); err == nil || !strings.Contains(err.Error(), "no node service") {
		t.Fatalf("no service: %v", err)
	}
	deviceFiles(t, f, true, false)
	if err := nodeInstall(f.env, ""); err != nil {
		t.Fatal(err)
	}
	if err := nodeCmd(f.env, []string{"switch"}); err == nil || !strings.Contains(err.Error(), "no persona-node") {
		t.Fatalf("no persona-node: %v", err)
	}
	deviceFiles(t, f, false, true)
	for _, host := range []string{"hub.json", filepath.Join("managers", "m-1", "manager.json")} {
		p := filepath.Join(f.personaDir(), host)
		_ = os.MkdirAll(filepath.Dir(p), 0o755)
		_ = os.WriteFile(p, []byte("{}"), 0o600)
		if err := nodeCmd(f.env, []string{"switch"}); err == nil || !strings.Contains(err.Error(), "hub or a manager") {
			t.Fatalf("%s: %v", host, err)
		}
		_ = os.Remove(p)
	}
	before := mustRead(t, f.nodeUnitPath())
	if !strings.Contains(before, `"`+f.personadBin()+`"`) {
		t.Fatalf("refusals changed the service:\n%s", before)
	}
	answering(t, "persona-node")
	if err := nodeCmd(f.env, []string{"switch"}); err != nil {
		t.Fatal(err)
	}
	f.out.Reset()
	if err := nodeCmd(f.env, []string{"switch"}); err != nil || !strings.Contains(f.out.String(), "already runs") {
		t.Fatalf("%v %s", err, f.out)
	}
}

// A switch that stopped half way (its backup still there, no switch holding the lock): node status only says so; the
// next node install (or switch, or persona-link run) puts the old service back and starts it. While a switch holds the
// lock, nothing touches its backup and a second switch is refused. A backup that is no usable service file is dropped,
// never written over the service.
func TestNodeSwitchRecovers(t *testing.T) {
	linuxPaths(t)
	f := newFakeEnv(t)
	deviceFiles(t, f, true, true)
	_ = os.MkdirAll(filepath.Dir(f.nodeUnitPath()), 0o755)
	old := nodeUnitFile(f.personadBin(), f.nodeLog())
	switched := nodeUnitFile(f.personaNodeBin(), f.nodeLog())
	_ = os.WriteFile(f.switchBackup(), []byte(old), 0o644)
	_ = os.WriteFile(f.nodeUnitPath(), []byte(switched), 0o644)
	answering(t, "personad")

	// A switch is running (it holds the lock): status, install and a second switch leave everything as it is.
	release, err := link.LockFile(f.switchLock(), false)
	if err != nil {
		t.Fatal(err)
	}
	if st := nodeStatus(f.env); st.SwitchPending {
		t.Error("pending while a switch runs")
	}
	for _, cmd := range [][]string{{"install"}, {"uninstall"}, {"switch"}} {
		if err := nodeCmd(f.env, cmd); err != errSwitchRunning {
			t.Fatalf("%v while a switch runs: %v", cmd, err)
		}
	}
	if mustRead(t, f.nodeUnitPath()) != switched || !fileExists(f.switchBackup()) {
		t.Fatal("a running switch's backup was touched")
	}
	release()

	// It died: status says so and changes nothing; install puts the old service back.
	f.calls = nil
	f.out.Reset()
	if err := nodeCmd(f.env, []string{"status", "--json"}); err != nil || !strings.Contains(f.out.String(), `"switch_pending": true`) ||
		strings.HasPrefix(strings.TrimSpace(f.out.String()), "A") || mustRead(t, f.nodeUnitPath()) != switched {
		t.Fatalf("status: %v %s", err, f.out)
	}
	if err := nodeCmd(f.env, []string{"install"}); err != nil {
		t.Fatal(err)
	}
	if mustRead(t, f.nodeUnitPath()) != old || fileExists(f.switchBackup()) ||
		!strings.Contains(strings.Join(f.calls, "\n"), "systemctl --user restart personad-node.service") ||
		!strings.Contains(f.out.String(), "did not finish") {
		t.Fatalf("calls %q\n%s", f.calls, f.out)
	}

	// A backup a power cut left empty: dropped, the service stays as it is (it runs persona-node, which works).
	_ = os.WriteFile(f.nodeUnitPath(), []byte(switched), 0o644)
	_ = os.WriteFile(f.switchBackup(), nil, 0o644)
	f.calls = nil
	if err := recoverNodeSwitch(f.env); err != nil || mustRead(t, f.nodeUnitPath()) != switched || fileExists(f.switchBackup()) ||
		strings.Contains(strings.Join(f.calls, "\n"), "restart") {
		t.Fatalf("empty backup: %v %q", err, f.calls)
	}
}

// A switch that is stopped (its context ends while it waits) or whose restart fails goes back to the old service; when
// even that does not start, the error says so and where things are.
func TestNodeSwitchRollsBack(t *testing.T) {
	linuxPaths(t)
	for _, tc := range []struct {
		name, fail, want string
		stop             bool
	}{
		{"stopped", "", "stopped: the node service runs", true},
		{"restart fails", "restart", "the node service runs", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFakeEnv(t)
			deviceFiles(t, f, true, true)
			_ = os.MkdirAll(filepath.Dir(f.nodeUnitPath()), 0o755)
			old := nodeUnitFile(f.personadBin(), f.nodeLog())
			_ = os.WriteFile(f.nodeUnitPath(), []byte(old), 0o644)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			prev := nodeAnswers
			t.Cleanup(func() { nodeAnswers = prev })
			nodeAnswers = func(_ context.Context, _ *env, program string, _ time.Duration) (bool, string) {
				if program == programPersonaNode && tc.stop {
					cancel()
					return false, "stopped while waiting"
				}
				return program == "", "it answers as personad"
			}
			restarts := 0
			onCmd := f.onCommand
			f.onCommand = func(call string) (string, error) {
				if strings.HasPrefix(call, "systemctl --user restart") {
					restarts++
					if tc.fail == "restart" && restarts == 1 {
						return "Failed to restart", errors.New("exit status 1")
					}
				}
				return onCmd(call)
			}
			err := nodeSwitch(ctx, f.env, programPersonaNode)
			if err == nil || !strings.Contains(err.Error(), tc.want) || mustRead(t, f.nodeUnitPath()) != old || fileExists(f.switchBackup()) {
				t.Fatalf("%v\n%s", err, mustRead(t, f.nodeUnitPath()))
			}
		})
	}
	// The old service does not come back either: said, with the service file back in place.
	f := newFakeEnv(t)
	deviceFiles(t, f, true, true)
	_ = os.MkdirAll(filepath.Dir(f.nodeUnitPath()), 0o755)
	old := nodeUnitFile(f.personadBin(), f.nodeLog())
	_ = os.WriteFile(f.nodeUnitPath(), []byte(old), 0o644)
	answering(t, "", "")
	if err := nodeSwitch(context.Background(), f.env, programPersonaNode); err == nil || !strings.Contains(err.Error(), "does not answer yet") ||
		mustRead(t, f.nodeUnitPath()) != old {
		t.Fatalf("%v", err)
	}
}

// --to personad moves a node service back to personad (a device that became a hub or a manager); the Mac app's
// persona-node, beside persona-link, is put in place when the computer has none.
func TestNodeSwitchBackAndBundled(t *testing.T) {
	linuxPaths(t)
	f := newFakeEnv(t)
	deviceFiles(t, f, true, true)
	_ = os.MkdirAll(filepath.Dir(f.nodeUnitPath()), 0o755)
	_ = os.WriteFile(f.nodeUnitPath(), []byte(nodeUnitFile(f.personaNodeBin(), f.nodeLog())), 0o644)
	_ = os.WriteFile(filepath.Join(f.personaDir(), "hub.json"), []byte("{}"), 0o600)
	answering(t, "personad")
	if err := nodeCmd(f.env, []string{"switch", "--to", "personad"}); err != nil ||
		!strings.Contains(mustRead(t, f.nodeUnitPath()), `"`+f.personadBin()+`"`) {
		t.Fatalf("%v\n%s", err, mustRead(t, f.nodeUnitPath()))
	}

	g := newFakeEnv(t)
	deviceFiles(t, g, true, false)
	exe, _ := g.exe()
	if p, err := filepath.EvalSymlinks(exe); err == nil { // where placeBundledNode looks: beside the resolved executable
		exe = p
	}
	bundled := filepath.Join(filepath.Dir(exe), "persona-node")
	_ = os.WriteFile(bundled, []byte("#!/bin/sh\n# bundled persona-node\n"), 0o755)
	prev := g.onCommand
	g.onCommand = func(call string) (string, error) {
		if call == bundled+" node-update-check" || (call == g.personaNodeBin()+" node-update-check" && fileExists(g.personaNodeBin())) {
			return `{"program":"persona-node"}`, nil
		}
		return prev(call)
	}
	_ = os.MkdirAll(filepath.Dir(g.nodeUnitPath()), 0o755)
	_ = os.WriteFile(g.nodeUnitPath(), []byte(nodeUnitFile(g.personadBin(), g.nodeLog())), 0o644)
	answering(t, "persona-node")
	if err := nodeCmd(g.env, []string{"switch"}); err != nil || !strings.Contains(mustRead(t, g.personaNodeBin()), "bundled") ||
		!strings.Contains(mustRead(t, g.nodeUnitPath()), `"`+g.personaNodeBin()+`"`) || !fileExists(g.personadBin()) {
		t.Fatalf("%v\n%s", err, g.out)
	}

	// node install --node-bin on a computer that has personad (no hub or manager): persona-node goes in beside it.
	h := newFakeEnv(t)
	deviceFiles(t, h, true, false)
	from := filepath.Join(t.TempDir(), "helper")
	_ = os.WriteFile(from, []byte("#!/bin/sh\n# persona-node\n"), 0o755)
	hp := h.onCommand
	h.onCommand = func(call string) (string, error) {
		if call == from+" node-update-check" || (call == h.personaNodeBin()+" node-update-check" && fileExists(h.personaNodeBin())) {
			return `{"program":"persona-node"}`, nil
		}
		return hp(call)
	}
	if err := nodeCmd(h.env, []string{"install", "--node-bin", from}); err != nil || !fileExists(h.personaNodeBin()) ||
		!strings.Contains(mustRead(t, h.nodeUnitPath()), `"`+h.personaNodeBin()+`"`) {
		t.Fatalf("%v\n%s", err, h.out)
	}
}

// On a Mac the launchd agent is switched the same way: its program argument, then bootout and bootstrap.
func TestNodeSwitchMac(t *testing.T) {
	f := newFakeEnv(t)
	f.goos, f.uid = "darwin", 501
	deviceFiles(t, f, true, true)
	_ = os.MkdirAll(filepath.Dir(f.nodePlistPath()), 0o755)
	_ = os.WriteFile(f.nodePlistPath(), []byte(nodePlist(f.personadBin(), f.nodeLog())), 0o644)
	answering(t, "persona-node")
	fprev := f.onCommand
	f.onCommand = func(call string) (string, error) { // in Terminal on the Mac
		if call == "launchctl managername" {
			return "Aqua", nil
		}
		return fprev(call)
	}
	f.calls = nil
	if err := nodeCmd(f.env, []string{"switch"}); err != nil {
		t.Fatalf("%v\n%s", err, f.out)
	}
	plist := mustRead(t, f.nodePlistPath())
	if !strings.Contains(plist, "<string>"+f.personaNodeBin()+"</string><string>node</string>") ||
		!strings.Contains(strings.Join(f.calls, "\n"), "launchctl bootstrap gui/501 "+f.nodePlistPath()) {
		t.Fatalf("calls %q\n%s", f.calls, plist)
	}

	// Over ssh (launchd's manager is not Aqua) the switch is refused before anything changes: launchd there may take the
	// agent away and not load one again.
	s := newFakeEnv(t)
	s.goos, s.uid = "darwin", 501
	deviceFiles(t, s, true, true)
	_ = os.MkdirAll(filepath.Dir(s.nodePlistPath()), 0o755)
	sold := nodePlist(s.personadBin(), s.nodeLog())
	_ = os.WriteFile(s.nodePlistPath(), []byte(sold), 0o644)
	sprev := s.onCommand
	s.onCommand = func(call string) (string, error) {
		if call == "launchctl managername" {
			return "Background", nil
		}
		return sprev(call)
	}
	s.calls = nil
	if err := nodeCmd(s.env, []string{"switch"}); err == nil || !strings.Contains(err.Error(), "Terminal on the Mac") ||
		mustRead(t, s.nodePlistPath()) != sold || fileExists(s.switchBackup()) || strings.Contains(strings.Join(s.calls, "\n"), "bootout") {
		t.Fatalf("%v calls %q", err, s.calls)
	}
	// Nor is a switch that did not finish undone from there: the service and its backup wait for a desktop session.
	moved := nodePlist(s.personaNodeBin(), s.nodeLog())
	_ = os.WriteFile(s.nodePlistPath(), []byte(moved), 0o644)
	_ = os.WriteFile(s.switchBackup(), []byte(sold), 0o644)
	s.calls = nil
	for _, args := range [][]string{{"switch"}, {"install"}} {
		if err := nodeCmd(s.env, args); err == nil || !strings.Contains(err.Error(), "Terminal on the Mac") ||
			mustRead(t, s.nodePlistPath()) != moved || !fileExists(s.switchBackup()) || strings.Contains(strings.Join(s.calls, "\n"), "bootout") {
			t.Fatalf("%v: %v calls %q", args, err, s.calls)
		}
	}

	// Back to personad, persona-node never answering: the plist comes back. bootstrap right after bootout fails at
	// first (launchd still taking the old job away): it is tried again.
	g := newFakeEnv(t)
	g.goos, g.uid = "darwin", 501
	deviceFiles(t, g, true, true)
	_ = os.MkdirAll(filepath.Dir(g.nodePlistPath()), 0o755)
	old := nodePlist(g.personadBin(), g.nodeLog())
	_ = os.WriteFile(g.nodePlistPath(), []byte(old), 0o644)
	answering(t, "", "personad")
	fails := 2
	prev := g.onCommand
	g.onCommand = func(call string) (string, error) {
		if strings.HasPrefix(call, "launchctl bootstrap") && fails > 0 {
			fails--
			return "Bootstrap failed: 5: Input/output error", errors.New("exit status 5")
		}
		return prev(call)
	}
	if err := nodeCmd(g.env, []string{"switch"}); err == nil || mustRead(t, g.nodePlistPath()) != old || fileExists(g.switchBackup()) ||
		strings.Count(strings.Join(g.calls, "\n"), "launchctl bootstrap") != 4 {
		t.Fatalf("%v calls %q", err, g.calls)
	}
}

// serviceProgram reads the program back from what systemdQuote and the plist write, odd characters and all.
func TestServiceProgram(t *testing.T) {
	f := newFakeEnv(t)
	for _, bin := range []string{"/home/u/.persona/bin/personad", `/home/a "b"/50%/$x/personad`} {
		if got := f.serviceProgram(nodeUnitFile(bin, "/tmp/l")); got != bin {
			t.Errorf("unit: %q, want %q", got, bin)
		}
	}
	f.goos = "darwin"
	if got := f.serviceProgram(nodePlist("/Users/a&b/<x>/personad", "/tmp/l")); got != "/Users/a&b/<x>/personad" {
		t.Errorf("plist: %q", got)
	}
}

// pingNode reads what a node answers on node.sock: its program, personad for one from before the program was said,
// an error for anything that is not a node's answer.
func TestPingNode(t *testing.T) {
	linuxPaths(t)
	dir, err := os.MkdirTemp("", "pl-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	sock := filepath.Join(dir, "node.sock")
	if _, err := pingNode(sock); err == nil {
		t.Fatal("no socket: want an error")
	}
	for answer, want := range map[string]string{
		`{"ok":true,"program":"persona-node","version":"v"}`: "persona-node",
		`{"ok":true}`: "personad",
		`{"ok":true,"standby":true,"program":"persona-node"}`: "persona-node",
		`{"output":null}`: "",
	} {
		ln, err := net.Listen("unix", sock)
		if err != nil {
			t.Fatal(err)
		}
		go func() {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			line, _ := bufio.NewReader(c).ReadString('\n')
			if strings.Contains(line, `"kind":"ping"`) {
				_, _ = c.Write([]byte(answer + "\n"))
			}
			c.Close()
		}()
		got, err := pingNode(sock)
		ln.Close()
		if got != want || (want == "") != (err != nil) {
			t.Errorf("%s: %q %v, want %q", answer, got, err, want)
		}
	}
}

// The real nodeAnswers waits until the node on node.sock answers as the program asked for, and gives up at its
// deadline or when its context ends.
func TestNodeAnswersWaitsForTheProgram(t *testing.T) {
	linuxPaths(t)
	home, err := os.MkdirTemp("", "pl-") // a unix socket path has about 100 bytes
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(home) })
	f := newFakeEnv(t)
	f.home = home
	_ = os.MkdirAll(f.personaDir(), 0o700)
	ln, err := net.Listen("unix", filepath.Join(f.personaDir(), "node.sock"))
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	answers := []string{"personad", "personad", "persona-node"} // the old node first, then the new one
	go func() {
		for i := 0; ; i++ {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			_, _ = bufio.NewReader(c).ReadString('\n')
			_, _ = c.Write([]byte(`{"ok":true,"program":"` + answers[min(i, len(answers)-1)] + `"}` + "\n"))
			c.Close()
		}
	}()
	if ok, last := nodeAnswers(context.Background(), f.env, programPersonaNode, time.Minute); !ok || !strings.Contains(last, "persona-node") {
		t.Fatalf("%v %s", ok, last)
	}
	if ok, _ := nodeAnswers(context.Background(), f.env, "nothing-answers-so", 3*time.Second); ok {
		t.Fatal("past the deadline")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if ok, _ := nodeAnswers(ctx, f.env, "nothing-answers-so", time.Hour); ok {
		t.Fatal("after its context ended")
	}
}

// persona-link run's watch undoes a switch whose persona-link died half way, and leaves a running one alone.
func TestWatchNodeSwitch(t *testing.T) {
	linuxPaths(t)
	f := newFakeEnv(t)
	deviceFiles(t, f, true, true)
	_ = os.MkdirAll(filepath.Dir(f.nodeUnitPath()), 0o755)
	old := nodeUnitFile(f.personadBin(), f.nodeLog())
	_ = os.WriteFile(f.switchBackup(), []byte(old), 0o644)
	_ = os.WriteFile(f.nodeUnitPath(), []byte(nodeUnitFile(f.personaNodeBin(), f.nodeLog())), 0o644)
	answering(t, "personad")
	release, _ := link.LockFile(f.switchLock(), false)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { watchNodeSwitch(ctx, f.env, 10*time.Millisecond); close(done) }()
	time.Sleep(50 * time.Millisecond)
	if mustRead(t, f.nodeUnitPath()) == old {
		t.Fatal("a running switch was undone")
	}
	release()
	deadline := time.Now().Add(5 * time.Second)
	for mustRead(t, f.nodeUnitPath()) != old && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	<-done
	if mustRead(t, f.nodeUnitPath()) != old || fileExists(f.switchBackup()) {
		t.Fatal("the dead switch was not undone")
	}
}

// node install --node-bin with persona-node on a computer that runs a manager but has no personad: refused, never
// persona-node under personad's name.
func TestNodeInstallRefusesPersonaNodeForAManager(t *testing.T) {
	linuxPaths(t)
	f := newFakeEnv(t)
	_ = os.MkdirAll(filepath.Join(f.personaDir(), "managers", "m-1"), 0o755)
	_ = os.WriteFile(filepath.Join(f.personaDir(), "managers", "m-1", "manager.json"), []byte("{}"), 0o600)
	from := filepath.Join(t.TempDir(), "helper")
	_ = os.WriteFile(from, []byte("#!/bin/sh\n"), 0o755)
	f.onCommand = func(call string) (string, error) {
		if call == from+" node-update-check" {
			return `{"program":"persona-node"}`, nil
		}
		return "", nil
	}
	if err := nodeCmd(f.env, []string{"install", "--node-bin", from}); err == nil || !strings.Contains(err.Error(), "install personad") ||
		fileExists(f.personadBin()) || fileExists(f.nodeUnitPath()) {
		t.Fatalf("%v", err)
	}
}

// No test reaches this computer's own node service through persona-link run's watcher (newEnv is the real home); the
// tests that look at it put their own in.
func TestMain(m *testing.M) {
	realWatchSwitch = watchSwitch
	watchSwitch = func(context.Context) {}
	os.Exit(m.Run())
}

var realWatchSwitch func(context.Context)

// A rollback whose restart launchd refuses (bootstrap failing past its retries) keeps the backup, so the next recovery
// starts the old service again; a refused restart with a node answering all the same is done.
func TestNodeSwitchKeepsTheBackupWhenTheOldServiceDoesNotStart(t *testing.T) {
	f := newFakeEnv(t)
	f.goos, f.uid = "darwin", 501
	deviceFiles(t, f, true, true)
	_ = os.MkdirAll(filepath.Dir(f.nodePlistPath()), 0o755)
	old := nodePlist(f.personadBin(), f.nodeLog())
	_ = os.WriteFile(f.nodePlistPath(), []byte(old), 0o644)
	bootstraps, refuse := 0, true
	prev := f.onCommand
	f.onCommand = func(call string) (string, error) {
		if strings.HasPrefix(call, "launchctl bootstrap") {
			bootstraps++
			if bootstraps > 1 && refuse { // the switch's own start goes; every start of the old service after it is refused
				return "Bootstrap failed: 5: Input/output error", errors.New("exit status 5")
			}
		}
		return prev(call)
	}
	answering(t) // nothing answers: not persona-node, and not the old node after the refused start
	err := nodeCmd(f.env, []string{"switch"})
	if err == nil || !strings.Contains(err.Error(), "tries again later") || mustRead(t, f.nodePlistPath()) != old || !fileExists(f.switchBackup()) {
		t.Fatalf("%v backup %v\n%s", err, fileExists(f.switchBackup()), mustRead(t, f.nodePlistPath()))
	}
	if !f.switchPending() {
		t.Fatal("status does not say the switch did not finish")
	}
	// The next look (persona-link run's watcher) starts it again, and launchd takes it now.
	refuse = false
	answering(t, "personad")
	if err := recoverNodeSwitch(f.env); err != nil || fileExists(f.switchBackup()) || mustRead(t, f.nodePlistPath()) != old {
		t.Fatalf("%v backup %v", err, fileExists(f.switchBackup()))
	}
	// launchd says no, but the old node runs and answers: nothing to do again.
	_ = os.WriteFile(f.switchBackup(), []byte(old), 0o644)
	_ = os.WriteFile(f.nodePlistPath(), []byte(nodePlist(f.personaNodeBin(), f.nodeLog())), 0o644)
	refuse = true
	answering(t, "personad")
	if err := recoverNodeSwitch(f.env); err != nil || fileExists(f.switchBackup()) || mustRead(t, f.nodePlistPath()) != old {
		t.Fatalf("%v backup %v", err, fileExists(f.switchBackup()))
	}
}

// The watcher keeps trying a recovery that fails, and logs that failure once, not at every look.
func TestWatchNodeSwitchLogsAFailureOnce(t *testing.T) {
	linuxPaths(t)
	f := newFakeEnv(t)
	deviceFiles(t, f, true, true)
	_ = os.MkdirAll(filepath.Dir(f.nodeUnitPath()), 0o755)
	old := nodeUnitFile(f.personadBin(), f.nodeLog())
	_ = os.WriteFile(f.switchBackup(), []byte(old), 0o644)
	_ = os.WriteFile(f.nodeUnitPath(), []byte(nodeUnitFile(f.personaNodeBin(), f.nodeLog())), 0o644)
	answering(t)
	var restarts atomic.Int32
	prev := f.onCommand
	f.onCommand = func(call string) (string, error) {
		if strings.HasPrefix(call, "systemctl --user restart") {
			restarts.Add(1)
			return "Failed to restart", errors.New("exit status 1")
		}
		return prev(call)
	}
	var logged syncBuffer
	log.SetOutput(&logged)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { watchNodeSwitch(ctx, f.env, time.Millisecond); close(done) }()
	deadline := time.Now().Add(5 * time.Second)
	for restarts.Load() < 3 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	cancel()
	<-done
	if n := strings.Count(logged.String(), "node switch:"); restarts.Load() < 3 || n != 1 || !fileExists(f.switchBackup()) {
		t.Fatalf("%d restarts, %d log lines, backup %v:\n%s", restarts.Load(), n, fileExists(f.switchBackup()), logged.String())
	}
}

// persona-link run watches for a node switch that did not finish once it holds its lock, not before (a second run
// that does not get the lock never starts one), and what the watcher says goes to the log, not to stdout (the menu bar
// app's JSON lines).
func TestKeepLinkWatchesNodeSwitches(t *testing.T) {
	path := filepath.Join(t.TempDir(), "link.json")
	started := make(chan bool, 2)
	prev := watchSwitch
	t.Cleanup(func() { watchSwitch = prev })
	watchSwitch = func(ctx context.Context) {
		started <- link.Running(path)
		<-ctx.Done()
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- keepLink(ctx, path, "", &bytes.Buffer{}, nil) }()
	select {
	case locked := <-started:
		if !locked {
			t.Fatal("the watcher started before persona-link run held its lock")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("persona-link run does not watch for a node switch that did not finish")
	}
	if err := keepLink(context.Background(), path, "", &bytes.Buffer{}, nil); !errors.Is(err, link.ErrRunning) {
		t.Fatalf("a second one: %v", err)
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	select {
	case <-started:
		t.Fatal("a persona-link run without the lock watched too")
	default:
	}

	// The real watcher, in a home of its own: a backup that names no program here is dropped, said in the log.
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("PERSONA_HOME", "")
	_, legacyHome := compat.EnvNames("PERSONA_HOME")
	t.Setenv(legacyHome, "")
	e := newEnv()
	_ = os.MkdirAll(e.personaDir(), 0o700)
	_ = os.WriteFile(e.switchBackup(), []byte("[Service]\nExecStart=\""+filepath.Join(home, "gone")+"\" node --attach\n"), 0o600)
	var logged syncBuffer
	log.SetOutput(&logged)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })
	wctx, wcancel := context.WithCancel(context.Background())
	wdone := make(chan struct{})
	go func() { realWatchSwitch(wctx); close(wdone) }()
	deadline := time.Now().Add(5 * time.Second)
	for fileExists(e.switchBackup()) && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	wcancel()
	<-wdone
	if fileExists(e.switchBackup()) || !strings.Contains(logged.String(), "Dropped "+e.switchBackup()) {
		t.Fatalf("backup %v, log:\n%s", fileExists(e.switchBackup()), logged.String())
	}
}

// node uninstall with a switch's backup left takes the service away and the backup with it; nothing is started again.
func TestNodeUninstallDropsAnUnfinishedSwitch(t *testing.T) {
	linuxPaths(t)
	f := newFakeEnv(t)
	deviceFiles(t, f, true, true)
	_ = os.MkdirAll(filepath.Dir(f.nodeUnitPath()), 0o755)
	_ = os.WriteFile(f.switchBackup(), []byte(nodeUnitFile(f.personadBin(), f.nodeLog())), 0o644)
	_ = os.WriteFile(f.nodeUnitPath(), []byte(nodeUnitFile(f.personaNodeBin(), f.nodeLog())), 0o644)
	answering(t)
	f.calls = nil
	if err := nodeCmd(f.env, []string{"uninstall"}); err != nil || fileExists(f.nodeUnitPath()) || fileExists(f.switchBackup()) ||
		strings.Contains(strings.Join(f.calls, "\n"), "restart") {
		t.Fatalf("%v unit %v backup %v calls %q", err, fileExists(f.nodeUnitPath()), fileExists(f.switchBackup()), f.calls)
	}
}

// --node-bin puts a program in under the name it says it is: one that does not say is refused, and so is persona-node
// on Windows (whose node runs personad.exe); persona-node given to node install as --personad is refused; persona-link
// install --node-bin puts persona-node in like node install.
func TestNodeBinIsWhatItSays(t *testing.T) {
	linuxPaths(t)
	f := newFakeEnv(t)
	from := filepath.Join(t.TempDir(), "something")
	_ = os.WriteFile(from, []byte("#!/bin/sh\nexit 1\n"), 0o755)
	f.onCommand = func(call string) (string, error) {
		if call == from+" node-update-check" {
			return "", errors.New("exit status 1")
		}
		return "", nil
	}
	if err := nodeCmd(f.env, []string{"install", "--node-bin", from}); err == nil || !strings.Contains(err.Error(), "does not say which program") ||
		fileExists(f.personadBin()) || fileExists(f.personaNodeBin()) || fileExists(f.nodeUnitPath()) {
		t.Fatalf("%v", err)
	}

	node := filepath.Join(t.TempDir(), "persona-node")
	_ = os.WriteFile(node, []byte("#!/bin/sh\n# persona-node\n"), 0o755)
	answers := func(call string) (string, error) { // the file given and its copy in bin/ say they are persona-node
		if call == node+" node-update-check" || strings.HasSuffix(call, string(filepath.Separator)+"persona-node node-update-check") {
			return `{"program":"persona-node"}`, nil
		}
		return "", nil
	}
	p := newFakeEnv(t)
	built := updatetest.Program(t, "persona-node", "", "") // --personad reads the build info, running nothing
	if err := nodeCmd(p.env, []string{"install", "--personad", built}); err == nil || !strings.Contains(err.Error(), "give it with --node-bin") ||
		fileExists(p.personadBin()) || fileExists(p.nodeUnitPath()) {
		t.Fatalf("--personad persona-node: %v", err)
	}

	w := newFakeEnv(t)
	w.goos = "windows"
	w.onCommand = answers
	if err := install(w.env, []string{"--node-bin", node}); err == nil || !strings.Contains(err.Error(), "is not personad") ||
		fileExists(filepath.Join(w.personaDir(), "bin", "personad.exe")) {
		t.Fatalf("windows: %v", err)
	}

	g := newFakeEnv(t)
	g.pairHere(t)
	g.linger = true
	g.onCommand = func(call string) (string, error) {
		if call == "systemctl --user restart persona-link.service" {
			g.saveStatus(t, link.Status{State: "connected", Mode: link.ModeActive, Kind: "lan", Address: "https://192.0.2.10:18662", Node: "online"}, 9)
		}
		return answers(call)
	}
	if err := install(g.env, []string{"--node-bin", node}); err != nil {
		t.Fatalf("%v\n%s", err, g.out)
	}
	if !fileExists(g.personaNodeBin()) || fileExists(g.personadBin()) || !strings.Contains(mustRead(t, g.nodeUnitPath()), `"`+g.personaNodeBin()+`"`) {
		t.Fatalf("persona-node %v personad %v\n%s\n%s", fileExists(g.personaNodeBin()), fileExists(g.personadBin()), mustRead(t, g.nodeUnitPath()), g.out)
	}
}
