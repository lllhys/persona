// Package manager is `personad manager …`: a manager made in the app (docs/assistant/managers.md, "A second manager") joins the hub
// with its one-time install code, runs, and is installed as a service of its own (systemd user unit, launchd agent,
// or a container). The manager itself is the assistant's program (assistant.RunManager) with a directory of its own.
package manager

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/nwverse/persona/server/internal/clitext"
	"github.com/nwverse/persona/server/internal/i18n"
	"github.com/nwverse/persona/server/internal/identity"
	"github.com/nwverse/persona/server/internal/managerupdate"
	"github.com/nwverse/persona/server/internal/terminalmaintenance"
	"github.com/nwverse/persona/server/internal/textfmt"
	"github.com/nwverse/persona/server/internal/update"
)

// Usage is the help of `personad manager` in l.
func Usage(l i18n.Lang) string {
	return i18n.T(l.Or(i18n.En), "manager-cli.usage")
}

// errUsage: the arguments are wrong; the caller prints Usage.
var errUsage = errors.New("arguments")

// ErrUsage is errUsage for the command line (personad prints Usage and exits 2).
var ErrUsage = errUsage

// Main runs `personad manager <args>`.
func Main(ctx context.Context, build update.Build, args []string, stdout io.Writer) error {
	if len(args) == 0 {
		return errUsage
	}
	e := newEnv(stdout)
	switch args[0] {
	case "maintenance":
		return terminalmaintenance.Main(ctx, args[1:], stdout)
	case "update":
		return managerupdate.Main(ctx, build, args[1:], stdout)
	case "capabilities":
		return cmdCapabilities(stdout, args[1:])
	case "join":
		return cmdJoin(ctx, e, args[1:])
	case "run":
		return cmdRun(ctx, e, build, args[1:])
	case "install":
		if isBuiltin(args[1:]) {
			return cmdInstallBuiltin(ctx, e, args[1:])
		}
		return cmdInstall(ctx, e, args[1:])
	case "uninstall":
		if isBuiltin(args[1:]) {
			return cmdUninstallBuiltin(ctx, e, args[1:])
		}
		return cmdUninstall(ctx, e, args[1:])
	case "status":
		return cmdStatus(ctx, e, args[1:])
	case "search-reindex":
		return cmdSearchReindex(ctx, e, args[1:])
	case "-h", "--help", "help":
		fmt.Fprint(stdout, clitext.Named(Usage(e.l()))) // as the program was run (persona-manager, personad)
		return nil
	}
	return errUsage
}

// --- helpers ------------------------------------------------------------------------------------------------------

// parseInterleaved parses flags that may come before, between or after the positional arguments. What the flag package
// prints waits in a buffer: -h writes the flags' help to the flag set's output (stdout) and returns errHelp; a flag
// mistake writes nothing there and returns a usageMistake with the flag's message, which the caller prints with Usage on
// stderr.
func parseInterleaved(fs *flag.FlagSet, args []string) ([]string, error) {
	out := fs.Output()
	var printed bytes.Buffer
	fs.SetOutput(&printed)
	defer fs.SetOutput(out)
	var pos []string
	for {
		if err := fs.Parse(args); err != nil {
			if errors.Is(err, flag.ErrHelp) {
				_, _ = io.Copy(out, &printed)
				return nil, errHelp
			}
			return nil, usageMistake{err.Error()}
		}
		args = fs.Args()
		if len(args) == 0 {
			return pos, nil
		}
		pos = append(pos, args[0])
		args = args[1:]
	}
}

// usageMistake is errUsage that says what was wrong (the flag package's "flag provided but not defined: -json").
type usageMistake struct{ msg string }

func (e usageMistake) Error() string { return e.msg }
func (e usageMistake) Unwrap() error { return errUsage }

// ConfigError is a mistake in the manager's configuration (no manager.json, no database to use): personad exits 2,
// which its systemd service does not restart on (RestartPreventExitStatus=2; a container's restart policy does, so
// manager run --join waits instead).
type ConfigError struct{ Err error }

func (e ConfigError) Error() string { return e.Err.Error() }
func (e ConfigError) Unwrap() error { return e.Err }

// terminalError keeps a localized message without discarding the underlying cause.
type terminalError struct {
	text  string
	cause error
}

func (e terminalError) Error() string { return e.text }
func (e terminalError) Unwrap() error { return e.cause }

func withTerminalCause(text string, cause error) error {
	return terminalError{text: text, cause: cause}
}

// errHelp: -h was asked for; the flags' help is printed already.
var errHelp = errors.New("help")

// ErrHelp is errHelp for the command line (exit 0).
var ErrHelp = errHelp

func fileExists(p string) bool {
	st, err := os.Stat(p)
	return err == nil && !st.IsDir()
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	return textfmt.Cut(s, 200)
}

// shellQuote makes s one word for sh.
func shellQuote(s string) string {
	if s != "" && strings.Trim(s, "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789/._-+=:@") == "" {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// dataDir is this machine's data directory (~/.persona), for the binary the service runs.
func dataDir() string { return identity.HomeDir() }
