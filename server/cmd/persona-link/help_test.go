package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestSubcommandHelp(t *testing.T) {
	for sub, want := range map[string]string{"install": "--no-node", "node": "personad-node", "mode": "两种模式的配对", "status": "退出码 0，否则 1",
		"log": "persona-link logs ", "address": "address remove"} {
		got := subcommandHelp(sub)
		if !strings.Contains(got, want) || got == usage() || strings.Contains(got, "persona-link pair ") {
			t.Errorf("%s: %q", sub, got)
		}
	}
	if got := subcommandHelp("supervise"); got != usage() {
		t.Errorf("supervise has no entry, wants the whole usage: %q", got)
	}
}

// TestSubcommandHelpExitsZero runs main in a child process (this test binary, re-executed): `<sub> -h` prints that
// subcommand's help on stdout and exits 0; an unknown subcommand and a wrong flag still exit 2.
func TestSubcommandHelpExitsZero(t *testing.T) {
	if args := os.Getenv("PERSONA_LINK_TEST_HELP_ARGS"); args != "" {
		os.Args = append([]string{"persona-link"}, strings.Split(args, "\x1f")...)
		main()
		os.Exit(0)
	}
	home := t.TempDir()
	runAs := func(name string, args ...string) (string, string, int) {
		cmd := exec.Command(os.Args[0], "-test.run=^TestSubcommandHelpExitsZero$")
		cmd.Env = append(os.Environ(), "PERSONA_LINK_TEST_HELP_ARGS="+strings.Join(args, "\x1f"), "HOME="+home,
			"PERSONA_HOME="+filepath.Join(home, ".persona"), "PERSONA_LINK_NAME="+name)
		var stdout, stderr strings.Builder
		cmd.Stdout, cmd.Stderr = &stdout, &stderr
		err := cmd.Run()
		code := 0
		if ee, ok := err.(*exec.ExitError); ok {
			code = ee.ExitCode()
		} else if err != nil {
			t.Fatal(err)
		}
		return stdout.String(), stderr.String(), code
	}
	run := func(args ...string) (string, string, int) { return runAs("", args...) }
	for _, args := range [][]string{{"install", "-h"}, {"node", "-h"}, {"node", "install", "--help"}, {"mode", "-h"}, {"status", "-h"}, {"status", "--json", "-help"}} {
		out, _, code := run(args...)
		if code != 0 || !strings.Contains(out, "persona-link "+args[0]) || strings.Contains(out, "persona-link discover") {
			t.Errorf("%q: exit %d, stdout %q", args, code, out)
		}
	}
	if out, _, code := runAs("persona-link", "install", "-h"); code != 0 || !strings.Contains(out, "persona-link install") {
		t.Errorf("under the new name: exit %d, stdout %q", code, out)
	}
	for _, args := range [][]string{{"bogus", "-h"}, {"status", "--bogus"}} {
		if _, errOut, code := run(args...); code != 2 || !strings.Contains(errOut, "用法") {
			t.Errorf("%q: exit %d, stderr %q", args, code, errOut)
		}
	}
}
