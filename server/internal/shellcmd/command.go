package shellcmd

import (
	"strings"

	"github.com/nwverse/persona/server/internal/lazyre"
)

// NameRe is a name an alias or a launcher may have: the shell's rule for a function name, a dash allowed.
var NameRe = lazyre.New(`^[A-Za-z_][A-Za-z0-9_-]*$`)

// AssignRe is a word that sets a variable (NAME=value).
var AssignRe = lazyre.New(`^[A-Za-z_][A-Za-z0-9_]*=`)

// CommandIndex is where the command of one simple command starts, past the variables set for it (put in env) and
// the wrappers that run what follows (env, with its -u / -i, command, exec, noglob, nocorrect); -1: there is none.
func CommandIndex(cmd []string, env map[string]string) int {
	index, _ := CommandEnvironment(cmd, env)
	return index
}

// CommandEnvironment also reports whether env discarded all inherited variables.
func CommandEnvironment(cmd []string, env map[string]string) (index int, reset bool) {
	envOptions := false
	for i := 0; i < len(cmd); i++ {
		w := cmd[i]
		switch {
		case AssignRe.MatchString(w):
			k, v, _ := strings.Cut(w, "=")
			env[k] = v
			envOptions = false
		case w == "env":
			envOptions = true
		case w == "command" || w == "exec" || w == "noglob" || w == "nocorrect":
			envOptions = false
		case envOptions && (w == "-i" || w == "--ignore-environment" || w == "-"):
			clear(env)
			reset = true
		case envOptions && w == "--":
			envOptions = false
		case envOptions && w == "-u" && i+1 < len(cmd): // env -u NAME
			delete(env, cmd[i+1])
			i++
		case envOptions && strings.HasPrefix(w, "-"):
			return -1, reset
		default:
			return i, reset
		}
	}
	return -1, reset
}
