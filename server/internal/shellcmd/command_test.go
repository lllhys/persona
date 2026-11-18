package shellcmd

import (
	"maps"
	"testing"
)

func TestCommandIndex(t *testing.T) {
	for _, c := range []struct {
		cmd  []string
		want int
		env  map[string]string
	}{
		{[]string{"A=1", "B=x=y", "claude", "-c"}, 2, map[string]string{"A": "1", "B": "x=y"}},
		{[]string{"env", "-i", "A=1", "exec", "noglob", "codex"}, 5, map[string]string{"A": "1"}},
		{[]string{"A=1", "env", "-u", "A", "claude"}, 4, map[string]string{}},
		{[]string{"command", "nocorrect", "claude"}, 2, map[string]string{}},
		{[]string{"A=1"}, -1, map[string]string{"A": "1"}},
		{nil, -1, map[string]string{}},
	} {
		env := map[string]string{}
		if got := CommandIndex(c.cmd, env); got != c.want || !maps.Equal(env, c.env) {
			t.Errorf("%q: got %d %v, want %d %v", c.cmd, got, env, c.want, c.env)
		}
	}
}

func TestCommandEnvironmentReset(t *testing.T) {
	for _, flag := range []string{"-i", "--ignore-environment", "-"} {
		env := map[string]string{"INHERITED": "1", "UNSET": "1"}
		words := []string{"BEFORE=1", "env", "-u", "UNSET", flag, "AFTER=2", "claude"}
		index, reset := CommandEnvironment(words, env)
		if index != 6 || !reset || !maps.Equal(env, map[string]string{"AFTER": "2"}) {
			t.Errorf("%q: index=%d reset=%t env=%v", flag, index, reset, env)
		}
	}
}
