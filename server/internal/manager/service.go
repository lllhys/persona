package manager

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"html"
	"io"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/nwverse/persona/server/internal/assistant"
	"github.com/nwverse/persona/server/internal/i18n"
	"github.com/nwverse/persona/server/internal/managerconf"
	"github.com/nwverse/persona/server/internal/pgdocker"
)

// A manager as a service of its own (docs/assistant/managers.md, "Commands"): on Linux the systemd user unit
// persona-manager-<id>.service, on a Mac the launchd agent com.persona.manager.<id>; each runs
// `<binary> manager run --dir <its directory>` and is started again when it exits. In a container the container is
// the service (docker run --restart unless-stopped). Named like the hub's personad and com.persona.hub.

// env is what the service commands touch, so tests can stand in for systemctl / launchctl.
type env struct {
	stdout io.Writer
	lang   i18n.Lang
	goos   string
	home   string
	uid    int
	user   string
	exe    func() (string, error)
	run    func(ctx context.Context, name string, args ...string) (string, error)
	look   func(name string) (string, error)
	getenv func(string) string
	// version is what a program says to `version -v` (update.BuildOf when nil).
	version func(ctx context.Context, path string) (string, error)

	// The manager's own Postgres (database.go): docker, a free port on 127.0.0.1, a connection check.
	docker   pgdocker.Runner
	freePort func() (int, error)
	ping     func(ctx context.Context, url string) error
	dbWait   time.Duration
}

func newEnv(stdout io.Writer) *env {
	home, _ := os.UserHomeDir()
	e := &env{stdout: stdout, lang: i18n.Terminal(), goos: runtime.GOOS, home: home, uid: os.Getuid(), exe: os.Executable,
		look: exec.LookPath, getenv: os.Getenv,
		run: func(ctx context.Context, name string, args ...string) (string, error) {
			out, err := exec.CommandContext(ctx, name, args...).CombinedOutput()
			return string(out), err
		},
		docker: pgdocker.RunCommand, freePort: pgdocker.FreeLoopbackPort, ping: pingPostgres, dbWait: 30 * time.Second}
	if u, err := user.Current(); err == nil {
		e.user = u.Username
	}
	return e
}

// self is how the user runs this program, for the lines it prints.
func (e *env) self() string { return "personad" }

func (e *env) l() i18n.Lang {
	if e.lang != "" {
		return e.lang.Or(i18n.En)
	}
	return i18n.Terminal()
}

func unitName(id string) string     { return "persona-manager-" + id + ".service" }
func launchdLabel(id string) string { return "com.persona.manager." + id }

func (e *env) unitPath(id string) string {
	base := e.getenv("XDG_CONFIG_HOME")
	if base == "" {
		base = filepath.Join(e.home, ".config")
	}
	return filepath.Join(base, "systemd", "user", e.compatUnit(id))
}

func (e *env) plistPath(id string) string {
	return filepath.Join(e.home, "Library", "LaunchAgents", e.compatLabel(id)+".plist")
}

// writeServiceFile writes a unit or plist in one step (managerconf.WriteFileAtomic: temporary file, fsync, rename), so
// a crash or a full disk mid-write never leaves systemd or launchd a file cut short. Through a link it replaces the
// file the link names: a former name linked to it (checkCompatibleService) stays the same file.
func writeServiceFile(path, text string) error {
	if file, err := filepath.EvalSymlinks(path); err == nil {
		path = file
	}
	return managerconf.WriteFileAtomic(path, []byte(text), 0o644)
}

// binary is what the service runs: the hub machine's <data dir>/bin/personad when there is one (install.sh puts it
// there and self-update keeps it current), else this executable.
func (e *env) binary() (string, error) {
	bin := filepath.Join(dataDir(), "bin", "personad")
	if fileExists(bin) {
		return bin, nil
	}
	exe, err := e.exe()
	if err != nil {
		return "", err
	}
	return filepath.Abs(exe)
}

func systemdQuote(s string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`, `%`, `%%`, `$`, `$$`).Replace(s) + `"`
}

func systemdEnvQuote(s string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`, `%`, `%%`).Replace(s) + `"`
}

// serviceUnit is a manager's systemd user unit. Exit status 2 is a usage or configuration mistake (no manager.json,
// a bad one): restarting cannot fix it.
func serviceUnit(id, bin, dir string) string {
	return `# Written by personad manager install; personad manager uninstall removes it (docs/assistant/managers.md).
[Unit]
Description=Persona manager ` + id + `
After=network-online.target

[Service]
ExecStart=` + systemdQuote(bin) + ` manager run --dir ` + systemdQuote(dir) + `
Environment=` + systemdEnvQuote("PERSONA_ASSISTANT_DEPLOY=service") + `
Environment=PATH=%h/.local/bin:%h/bin:/usr/local/bin:/usr/bin:/bin
Restart=always
RestartSec=5
RestartPreventExitStatus=2

[Install]
WantedBy=default.target
`
}

// servicePlist is a manager's launchd agent; its log is manager.log in its directory.
func servicePlist(id, bin, dir, home string) string {
	x := html.EscapeString
	path := strings.Join([]string{filepath.Join(home, ".local", "bin"), filepath.Join(home, "bin"), "/opt/homebrew/bin",
		"/usr/local/bin", "/usr/bin", "/bin", "/usr/sbin", "/sbin"}, ":")
	log := filepath.Join(dir, "manager.log")
	return `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
  <key>Label</key><string>` + launchdLabel(id) + `</string>
  <key>ProgramArguments</key><array><string>` + x(bin) + `</string><string>manager</string><string>run</string><string>--dir</string><string>` + x(dir) + `</string></array>
  <key>EnvironmentVariables</key><dict>
    <key>PERSONA_ASSISTANT_DEPLOY</key><string>service</string>
    <key>PATH</key><string>` + x(path) + `</string>
  </dict>
  <key>RunAtLoad</key><true/>
  <key>KeepAlive</key><true/>
  <key>ThrottleInterval</key><integer>5</integer>
  <key>StandardOutPath</key><string>` + x(log) + `</string>
  <key>StandardErrorPath</key><string>` + x(log) + `</string>
</dict></plist>
`
}

// dirFlag parses [--dir D] and finds the manager: D, or the one manager of this machine.
func dirFlag(e *env, name string, args []string) (string, *assistant.ManagerConfig, error) {
	fs := flag.NewFlagSet("manager "+name, flag.ContinueOnError)
	fs.SetOutput(e.stdout)
	dir := fs.String("dir", "", i18n.T(e.l(), "manager-cli.flag_dir"))
	pos, err := parseInterleaved(fs, args)
	if err != nil {
		return "", nil, err
	}
	if len(pos) != 0 {
		return "", nil, errUsage
	}
	d := *dir
	if d == "" {
		if d, err = onlyManager(e.l()); err != nil {
			return "", nil, err
		}
	}
	if d, err = filepath.Abs(d); err != nil {
		return "", nil, err
	}
	c, err := assistant.ReadManagerConfig(d)
	if err != nil {
		return "", nil, withTerminalCause(i18n.T(e.l(), "manager-cli.dir_invalid", i18n.Args{"directory": d, "error": err}), err)
	}
	return d, c, nil
}

func (e *env) systemctl(ctx context.Context, args ...string) (string, error) {
	return e.run(ctx, "systemctl", append([]string{"--user"}, args...)...)
}

// noSystemd says why there is no systemd user instance; "" when there is one.
func (e *env) noSystemd(ctx context.Context) string {
	if _, err := e.look("systemctl"); err != nil {
		return i18n.T(e.l(), "manager-cli.systemd_missing")
	}
	if out, err := e.systemctl(ctx, "show-environment"); err != nil {
		if line := firstLine(out); line != "" {
			return i18n.T(e.l(), "manager-cli.systemd_user_unreachable_detail", i18n.Args{"error": line})
		}
		return i18n.T(e.l(), "manager-cli.systemd_user_unreachable")
	}
	return ""
}

func cmdInstall(ctx context.Context, e *env, args []string) error {
	dir, c, err := dirFlag(e, "install", args)
	if err != nil {
		return err
	}
	if err := e.checkCompatibleService(c.ID); err != nil {
		return err
	}
	made, err := e.ownDatabase(ctx, dir, c)
	if err != nil {
		return err
	}
	fmt.Fprint(e.stdout, i18n.T(e.l(), "manager-cli.database_label", i18n.Args{"database": databaseLine(e.l(), c, made)}))
	bin, err := e.binary()
	if err != nil {
		return err
	}
	manual := i18n.T(e.l(), "manager-cli.service_manual", i18n.Args{"binary": shellQuote(bin), "directory": shellQuote(dir)})
	switch e.goos {
	case "linux":
		if why := e.noSystemd(ctx); why != "" {
			return errors.New(i18n.T(e.l(), "manager-cli.service_install_unavailable", i18n.Args{"reason": why, "manual": manual}))
		}
		path := e.unitPath(c.ID)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return err
		}
		if err := writeServiceFile(path, e.compatUnitText(c.ID, serviceUnit(c.ID, bin, dir))); err != nil {
			return err
		}
		for _, step := range [][]string{{"daemon-reload"}, {"enable", e.compatUnit(c.ID)}, {"restart", e.compatUnit(c.ID)}} {
			if out, err := e.systemctl(ctx, step...); err != nil {
				return errors.New(i18n.T(e.l(), "manager-cli.systemctl_failed", i18n.Args{"arguments": strings.Join(step, " "), "error": firstLine(out + " " + err.Error())}))
			}
		}
		fmt.Fprint(e.stdout, i18n.T(e.l(), "manager-cli.installed_systemd", i18n.Args{"name": c.Name, "id": c.ID, "unit": e.compatUnit(c.ID)}))
		if e.user != "" && !fileExists("/var/lib/systemd/linger/"+e.user) {
			fmt.Fprint(e.stdout, i18n.T(e.l(), "manager-cli.linger_disabled", i18n.Args{"user": e.user}))
		}
		return nil
	case "darwin":
		path := e.plistPath(c.ID)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return err
		}
		if err := writeServiceFile(path, e.compatLabelText(c.ID, servicePlist(c.ID, bin, dir, e.home))); err != nil {
			return err
		}
		domain := "gui/" + strconv.Itoa(e.uid)
		_, _ = e.run(ctx, "launchctl", "bootout", domain+"/"+e.compatLabel(c.ID))
		if out, err := bootstrapAgent(ctx, e, domain, path); err != nil {
			return errors.New(i18n.T(e.l(), "manager-cli.launchctl_bootstrap_failed", i18n.Args{"error": firstLine(out + " " + err.Error())}))
		}
		fmt.Fprint(e.stdout, i18n.T(e.l(), "manager-cli.installed_launchd", i18n.Args{"name": c.Name, "id": c.ID, "label": e.compatLabel(c.ID), "log": filepath.Join(dir, "manager.log")}))
		return nil
	}
	return errors.New(i18n.T(e.l(), "manager-cli.service_unsupported", i18n.Args{"os": e.goos, "manual": manual}))
}

func cmdUninstall(ctx context.Context, e *env, args []string) error {
	purge := false
	rest := args[:0:0]
	for _, a := range args {
		if a == "--purge" {
			purge = true
		} else {
			rest = append(rest, a)
		}
	}
	dir, c, err := dirFlag(e, "uninstall", rest)
	if err != nil {
		return err
	}
	if err := e.removeService(ctx, c); err != nil {
		return err
	}
	fmt.Fprintf(e.stdout, "%s\n", e.uninstallDatabase(ctx, dir, c, purge))
	fmt.Fprint(e.stdout, i18n.T(e.l(), "manager-cli.uninstall_directory_kept", i18n.Args{"directory": dir}))
	return nil
}

// uninstallDatabase stops the manager's own database container, keeping its volume, or with purge removes both; a
// database it was given is never touched. It says what it did.
func (e *env) uninstallDatabase(ctx context.Context, dir string, c *assistant.ManagerConfig, purge bool) string {
	if c.DatabaseContainer == "" {
		if c.DatabaseURL != "" {
			return i18n.T(e.l(), "manager-cli.uninstall_database_given")
		}
		return i18n.T(e.l(), "manager-cli.uninstall_database_none")
	}
	pg := e.pg(c.ID)
	if !purge {
		if err := pg.Stop(ctx); err != nil {
			return i18n.T(e.l(), "manager-cli.uninstall_database_stop_failed", i18n.Args{"container": pg.Name, "error": err, "volume": pg.Volume})
		}
		return i18n.T(e.l(), "manager-cli.uninstall_database_stopped", i18n.Args{"container": pg.Name, "volume": pg.Volume})
	}
	if err := pg.Remove(ctx); err != nil {
		return i18n.T(e.l(), "manager-cli.uninstall_database_remove_failed", i18n.Args{"container": pg.Name, "volume": pg.Volume, "error": err})
	}
	c.DatabaseURL, c.DatabaseContainer = "", ""
	if err := assistant.WriteManagerConfig(dir, *c); err != nil {
		return i18n.T(e.l(), "manager-cli.uninstall_database_config_failed", i18n.Args{"container": pg.Name, "volume": pg.Volume, "error": err})
	}
	return i18n.T(e.l(), "manager-cli.uninstall_database_removed", i18n.Args{"container": pg.Name, "volume": pg.Volume})
}

// removeService stops and removes the manager's service; one that was never installed is fine.
func (e *env) removeService(ctx context.Context, c *assistant.ManagerConfig) error {
	if err := e.checkCompatibleService(c.ID); err != nil {
		return err
	}
	switch e.goos {
	case "linux":
		path := e.unitPath(c.ID)
		if !fileExists(path) {
			fmt.Fprint(e.stdout, i18n.T(e.l(), "manager-cli.service_not_installed", i18n.Args{"id": c.ID, "path": path}))
			return nil
		}
		_, _ = e.systemctl(ctx, "disable", "--now", e.compatUnit(c.ID))
		if err := os.Remove(path); err != nil {
			return err
		}
		_, _ = e.systemctl(ctx, "daemon-reload")
	case "darwin":
		path := e.plistPath(c.ID)
		if !fileExists(path) {
			fmt.Fprint(e.stdout, i18n.T(e.l(), "manager-cli.service_not_installed", i18n.Args{"id": c.ID, "path": path}))
			return nil
		}
		_, _ = e.run(ctx, "launchctl", "bootout", "gui/"+strconv.Itoa(e.uid)+"/"+e.compatLabel(c.ID))
		if err := os.Remove(path); err != nil {
			return err
		}
	default:
		fmt.Fprint(e.stdout, i18n.T(e.l(), "manager-cli.service_none_on_os", i18n.Args{"os": e.goos}))
		return nil
	}
	fmt.Fprint(e.stdout, i18n.T(e.l(), "manager-cli.service_removed", i18n.Args{"id": c.ID}))
	return nil
}

func cmdStatus(ctx context.Context, e *env, args []string) error {
	if isBuiltin(args) {
		if len(args) != 1 {
			return errUsage
		}
		e.builtinStatus(ctx, true)
		return nil
	}
	var dirs []string
	if len(args) > 0 {
		d, _, err := dirFlag(e, "status", args)
		if err != nil {
			return err
		}
		dirs = []string{d}
	} else {
		dirs = localManagers()
	}
	if len(args) == 0 {
		defer e.builtinStatus(ctx, false) // the built-in manager's own service too, when it has one or a binding
	}
	if len(dirs) == 0 {
		fmt.Fprint(e.stdout, i18n.T(e.l(), "manager-cli.status_none_under", i18n.Args{"directory": managersRoot()}))
		return nil
	}
	for _, d := range dirs {
		c, err := assistant.ReadManagerConfig(d)
		if err != nil {
			fmt.Fprint(e.stdout, i18n.T(e.l(), "manager-cli.status_config_error", i18n.Args{"directory": d, "error": err}))
			continue
		}
		database := databaseLine(e.l(), c, false)
		if c.DatabaseContainer != "" {
			database = i18n.T(e.l(), "manager-cli.status_database_container", i18n.Args{"database": database, "state": e.containerState(ctx, c.DatabaseContainer)})
		}
		fmt.Fprint(e.stdout, i18n.T(e.l(), "manager-cli.status_manager", i18n.Args{"name": c.Name, "id": c.ID, "directory": d, "hub": c.Hub, "database": database, "service": e.serviceState(ctx, c.ID)}))
	}
	return nil
}

// containerState says whether the database container runs: docker not there or not answering is said as such, not
// taken for a container that is gone.
func (e *env) containerState(ctx context.Context, name string) string {
	switch err := pgdocker.CheckDocker(ctx, e.docker, e.look); {
	case errors.Is(err, pgdocker.ErrNoDocker):
		return i18n.T(e.l(), "manager-cli.container_docker_missing")
	case errors.Is(err, pgdocker.ErrDockerDenied):
		return i18n.T(e.l(), "manager-cli.container_docker_denied")
	case err != nil:
		return i18n.T(e.l(), "manager-cli.container_docker_down")
	}
	out, err := e.docker(ctx, nil, "docker", "inspect", "-f", "{{.State.Status}}", name)
	switch {
	case err != nil:
		return i18n.T(e.l(), "manager-cli.container_missing")
	case strings.TrimSpace(string(out)) == "running":
		return i18n.T(e.l(), "manager-cli.container_running")
	}
	return i18n.T(e.l(), "manager-cli.container_not_running", i18n.Args{"state": strings.TrimSpace(string(out))})
}

// serviceState is a line on the manager's service.
func (e *env) serviceState(ctx context.Context, id string) string {
	switch e.goos {
	case "linux":
		if !fileExists(e.unitPath(id)) {
			return i18n.T(e.l(), "manager-cli.service_not_installed_hint", i18n.Args{"command": "personad manager install"})
		}
		out, _ := e.systemctl(ctx, "is-active", e.compatUnit(id))
		return e.compatUnit(id) + " " + strings.TrimSpace(out)
	case "darwin":
		if !fileExists(e.plistPath(id)) {
			return i18n.T(e.l(), "manager-cli.service_not_installed_hint", i18n.Args{"command": "personad manager install"})
		}
		if _, err := e.run(ctx, "launchctl", "print", "gui/"+strconv.Itoa(e.uid)+"/"+e.compatLabel(id)); err != nil {
			return i18n.T(e.l(), "manager-cli.service_launchd_not_running", i18n.Args{"label": e.compatLabel(id)})
		}
		return i18n.T(e.l(), "manager-cli.service_launchd_running", i18n.Args{"label": e.compatLabel(id)})
	}
	return i18n.T(e.l(), "manager-cli.service_unsupported_os")
}
