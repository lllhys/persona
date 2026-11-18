package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// The managers' command line, run as persona-manager (this test binary again): the version, the usage, the
// mistakes and the manager commands at the top level and after "manager", each with its exit code.
func TestPersonaManagerCommandLine(t *testing.T) {
	if args := os.Getenv("PERSONA_MANAGER_TEST_ARGS"); args != "" {
		os.Args = append([]string{"persona-manager"}, strings.Split(args, "\x1f")...)
		main()
		os.Exit(0)
	}
	home := t.TempDir()
	run := func(args ...string) (string, int) {
		cmd := exec.Command(os.Args[0], "-test.run=^TestPersonaManagerCommandLine$")
		cmd.Env = append(os.Environ(), "PERSONA_MANAGER_TEST_ARGS="+strings.Join(args, "\x1f"), "HOME="+home,
			"PERSONA_HOME="+filepath.Join(home, ".persona"))
		out, err := cmd.CombinedOutput()
		code := 0
		if ee, ok := err.(*exec.ExitError); ok {
			code = ee.ExitCode()
		} else if err != nil {
			t.Fatal(err)
		}
		return string(out), code
	}
	for _, c := range []struct {
		args []string
		code int
		want string
	}{
		{[]string{"version", "-v"}, 0, "channel "},
		{[]string{"--help"}, 0, "persona-manager assistant"},
		{[]string{"help"}, 0, "persona-manager assistant"},
		{[]string{"tailcat-key", "x"}, 2, "persona-manager tailcat-key takes no arguments"},
		{[]string{"bogus"}, 2, "persona-manager manager join"},            // manager.Main's usage, as typed
		{[]string{"manager", "bogus"}, 2, "persona-manager manager join"}, // personad's form
		{[]string{"manager"}, 2, "persona-manager manager join"},          // as personad manager: the usage, exit 2
		{[]string{"node", "bogus"}, 2, "persona-manager node -h"},         // the node's commands (the local node it starts)

		// A flag mistake: the flag's message and the usage, exit 2 (capabilities' was 1, with a log timestamp).
		{[]string{"capabilities", "--bogus"}, 2, "flag provided but not defined: -bogus"},
		{[]string{"status", "--json"}, 2, "flag provided but not defined: -json"},
	} {
		out, code := run(c.args...)
		if code != c.code || !strings.Contains(out, c.want) {
			t.Errorf("%q: exit %d, want %d and %q in %q", c.args, code, c.code, c.want, out)
		}
	}
}

func TestPersonaManagerUsageSpeaksTheTerminalLanguage(t *testing.T) {
	home := t.TempDir()
	for _, tc := range []struct {
		name, setting, want string
	}{
		{name: "en", setting: "en", want: "persona-manager manager join <hub address> <install code>"},
		{name: "zh-Hans", setting: "zh-Hans", want: "persona-manager manager join <hub 地址> <安装码>"},
		{name: "ja", setting: "ja", want: "persona-manager manager join <ハブアドレス> <インストールコード>"},
		{name: "unknown", setting: "ko", want: "persona-manager manager join <hub address> <install code>"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, args := range [][]string{{"manager", "help"}, {"manager", "bogus"}} {
				cmd := exec.Command(os.Args[0], "-test.run=^TestPersonaManagerCommandLine$")
				for _, kv := range os.Environ() {
					if !strings.HasPrefix(kv, "PERSONA_LANG=") && !strings.HasPrefix(kv, "LC_ALL=") &&
						!strings.HasPrefix(kv, "LC_MESSAGES=") && !strings.HasPrefix(kv, "LANG=") {
						cmd.Env = append(cmd.Env, kv)
					}
				}
				cmd.Env = append(cmd.Env, "PERSONA_MANAGER_TEST_ARGS="+strings.Join(args, "\x1f"), "HOME="+home,
					"PERSONA_HOME="+filepath.Join(home, ".persona"), "PERSONA_LANG="+tc.setting)
				out, err := cmd.CombinedOutput()
				code := 0
				if ee, ok := err.(*exec.ExitError); ok {
					code = ee.ExitCode()
				} else if err != nil {
					t.Fatal(err)
				}
				wantCode := 0
				if args[1] == "bogus" {
					wantCode = 2
				}
				if code != wantCode || !strings.Contains(string(out), tc.want) {
					t.Fatalf("%q: exit %d, want %d and %q in %q", args, code, wantCode, tc.want, out)
				}
			}
		})
	}
}
