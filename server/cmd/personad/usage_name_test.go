package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestUsageMistakesNameTheProgram runs main in a child process (this test binary, re-executed) under each name: a
// missing or unknown subcommand prints the usage with the name it was run as, no log timestamp, and exits 2.
func TestUsageMistakesNameTheProgram(t *testing.T) {
	if args := os.Getenv("PERSONAD_TEST_USAGE_ARGS"); args != "" {
		os.Args = append([]string{os.Getenv("PERSONAD_TEST_USAGE_NAME")}, strings.Split(args, "\x1f")...)
		main()
		os.Exit(0)
	}
	home := t.TempDir()
	run := func(name string, args ...string) (string, int) {
		cmd := exec.Command(os.Args[0], "-test.run=^TestUsageMistakesNameTheProgram$")
		cmd.Env = append(os.Environ(), "PERSONAD_TEST_USAGE_ARGS="+strings.Join(args, "\x1f"), "PERSONAD_TEST_USAGE_NAME="+name,
			"HOME="+home, "PERSONA_HOME="+filepath.Join(home, ".persona"), "PERSONA_LANG=zh-Hans")
		out, err := cmd.CombinedOutput()
		code := 0
		if ee, ok := err.(*exec.ExitError); ok {
			code = ee.ExitCode()
		} else if err != nil {
			t.Fatal(err)
		}
		return string(out), code
	}
	for _, name := range []string{"personad", "/usr/local/bin/personad"} {
		want := "usage: personad "
		for _, args := range [][]string{{"release"}, {"release", "bogus"}, {"release", "check-next"}, {"backup"}, {"backup", "bogus"}, {"launch"}, {"update-watch"}} {
			out, code := run(name, args...)
			if code != 2 || !strings.HasPrefix(out, want) && !strings.Contains(out, "\n"+want) {
				t.Errorf("%s %q: exit %d, output %q", name, args, code, out)
			}
		}
		if out, _ := run(name, "release"); !strings.Contains(out, "release manifest") || !strings.Contains(out, "release check-next") {
			t.Errorf("%s release: the release usage is missing: %q", name, out)
		}
		if out, _ := run(name, "release", "bogus"); !strings.Contains(out, `unknown release command "bogus"`) {
			t.Errorf("%s release bogus: %q", name, out)
		}
		// The hints and usage texts beyond the usage: lines name the program as run too.
		base := filepath.Base(name)
		for _, c := range []struct {
			args []string
			want string
		}{
			{[]string{"node", "tailcat"}, "（" + base + " node -h 看用法）\n  " + base + " node --stdio"},
			{[]string{"node", "bogus"}, "（" + base + " node -h 看用法）"},
			{[]string{"sysmon", "x"}, "usage: " + base + " sysmon [n]"},
			{[]string{"sysmon", "99999"}, base + " sysmon: n is at most"},
			{[]string{"bogus"}, "  " + base + " hub [run]"},
		} {
			out, code := run(name, c.args...)
			if code != 2 || !strings.Contains(out, c.want) {
				t.Errorf("%s %q: exit %d, want %q in %q", name, c.args, code, c.want, out)
			}
		}
	}
}
