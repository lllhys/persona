package hub

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"

	"golang.org/x/term"

	"github.com/nwverse/persona/server/internal/apierr"
	"github.com/nwverse/persona/server/internal/clitext"
	"github.com/nwverse/persona/server/internal/db"
	"github.com/nwverse/persona/server/internal/i18n"
	"github.com/nwverse/persona/server/internal/managerconf"
)

// `personad hub admin …` on the hub's machine (docs/operations/admin.md): widen or set the admin UI's scope, reset its password.

const adminUsage = `usage:
  personad hub admin access                        show who may open the admin UI
  personad hub admin access --get                  print hub.json's admin.access (empty when not chosen yet)
  personad hub admin access <loopback|lan|cidrs|any> [--cidrs a,b]
                                                   set it (the only way to widen it; the web page only narrows)
  personad hub admin reset-password [--clear]      set a new administrator password (or clear it: first-run
                                                   setup again); every signed-in browser is signed out
  personad hub admin setup-code                    a one-time code for first-run setup from this machine (asks the
                                                   running hub; valid 10 minutes)`

// accessChoices explains each scope (the catalog key of its meaning); admin_access_help in scripts/deploy/install.sh
// prints the same.
var accessChoices = []struct{ name, meaning string }{
	{accessLoopback, "hub-cli.access_loopback"},
	{accessLAN, "hub-cli.access_lan"},
	{accessCIDRs, "hub-cli.access_cidrs"},
	{accessAny, "hub-cli.access_any"},
}

// AdminCommand runs `personad hub admin <args>`. Help asked for prints the usage on stdout (exit 0); a usage mistake
// is a clitext.UsageError (exit 2), found before hub.json is read.
func AdminCommand(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		return clitext.UsageError(clitext.Named(adminUsage))
	}
	if adminHelpAsked(args) {
		_, err := fmt.Fprintln(stdout, clitext.Named(adminUsage))
		return err
	}
	switch args[0] {
	case "access":
		return adminAccessCommand(ctx, dataDir(), args[1:], stdout, stderr)
	case "reset-password":
		return adminResetPassword(ctx, dataDir(), args[1:], stdin, stdout, stderr)
	case "setup-code":
		if len(args) != 1 {
			return clitext.UsageError("admin setup-code takes no arguments\n" + clitext.Named(adminUsage))
		}
		return adminSetupCode(ctx, dataDir(), stdout)
	}
	return clitext.UsageError(fmt.Sprintf("unknown admin command %q\n%s", args[0], clitext.Named(adminUsage)))
}

// adminHelpAsked: help, -h or --help as the command, or after setup-code (which takes no arguments). access and
// reset-password answer -h with their flags (flag.ErrHelp, exit 0).
func adminHelpAsked(args []string) bool {
	if args[0] == "help" || clitext.IsHelpArg(args[0]) {
		return true
	}
	return args[0] == "setup-code" && slices.ContainsFunc(args[1:], clitext.IsHelpArg)
}

func adminAccessCommand(ctx context.Context, dir string, args []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("hub admin access", flag.ContinueOnError)
	flags.SetOutput(stderr)
	cidrsFlag := flags.String("cidrs", "", "networks for access cidrs, comma separated (192.0.2.0/24,2001:db8::/48)")
	get := flags.Bool("get", false, "print hub.json's admin.access only")
	// Flags may come after the scope: `access cidrs --cidrs …`.
	var positional []string
	rest := args
	for {
		if err := flags.Parse(rest); err != nil {
			return err
		}
		if flags.NArg() == 0 {
			break
		}
		positional = append(positional, flags.Arg(0))
		rest = flags.Args()[1:]
	}
	o, err := readHubJSON(dir)
	if err != nil {
		return err
	}
	var current AdminConfig
	if raw, ok := o.Get("admin"); ok {
		_ = json.Unmarshal(raw, &current)
	}
	if *get {
		fmt.Fprintln(stdout, current.Access)
		return nil
	}
	l := i18n.Terminal()
	if len(positional) == 0 {
		shown := current.Access
		if shown == "" {
			shown = i18n.T(l, "hub-cli.access_default", i18n.Args{"access": accessLAN})
		}
		fmt.Fprintln(stdout, i18n.T(l, "hub-cli.access_current", i18n.Args{"access": shown}))
		if len(current.CIDRs) > 0 {
			fmt.Fprintln(stdout, i18n.T(l, "hub-cli.access_cidrs_current", i18n.Args{"list": strings.Join(current.CIDRs, ", ")}))
		}
		if env := adminEnv(); env != nil {
			fmt.Fprintln(stdout, i18n.T(l, "hub-cli.access_env", i18n.Args{"access": env.Access}))
		}
		fmt.Fprintln(stdout, "\n"+i18n.T(l, "hub-cli.access_choices"))
		for _, c := range accessChoices {
			fmt.Fprintf(stdout, "  %-8s %s\n", c.name, i18n.T(l, c.meaning))
		}
		return nil
	}
	if len(positional) > 1 {
		return errors.New(adminUsage)
	}
	access := positional[0]
	if !validAccess(access) || access == "" {
		return errors.New(i18n.T(l, "hub-cli.access_invalid", i18n.Args{"access": strconv.Quote(access)}))
	}
	cidrs := splitList(*cidrsFlag)
	if access == accessCIDRs {
		prefixes, err := parseCIDRs(cidrs)
		if err != nil {
			return err
		}
		if len(prefixes) == 0 {
			return errors.New(i18n.T(l, "hub-cli.access_cidrs_missing"))
		}
		cidrs = cidrs[:0]
		for _, p := range prefixes {
			cidrs = append(cidrs, p.String())
		}
	} else if len(cidrs) > 0 {
		return errors.New(i18n.T(l, "hub-cli.access_cidrs_only"))
	}
	err = editHubJSON(dir, func(o *jsonObject) error {
		admin, err := o.Object("admin")
		if err != nil {
			return err
		}
		if err := admin.Set("access", access); err != nil {
			return err
		}
		if access == accessCIDRs {
			if err := admin.Set("cidrs", cidrs); err != nil {
				return err
			}
		}
		o.SetObject("admin", admin)
		return nil
	})
	if err != nil {
		return err
	}
	for _, c := range accessChoices {
		if c.name == access {
			fmt.Fprintln(stdout, i18n.T(l, "hub-cli.access_set", i18n.Args{"access": access, "meaning": i18n.T(l, c.meaning)}))
		}
	}
	if env := adminEnv(); env != nil {
		fmt.Fprintln(stderr, i18n.T(l, "hub-cli.access_env_wins", i18n.Args{"access": env.Access}))
	}
	cfg := &Config{Listen: ":18660", dir: dir}
	if b, err := os.ReadFile(dir + "/hub.json"); err == nil {
		_ = json.Unmarshal(b, cfg)
	}
	cfg.applyEnv()
	if access == accessAny && cfg.Public.TLSListen == "" {
		fmt.Fprintln(stderr, i18n.T(l, "hub-cli.access_any_without_tls"))
	}
	switch err := notifyAdminReload(ctx, cfg); {
	case err == nil:
		fmt.Fprintln(stdout, i18n.T(l, "hub-cli.access_applied"))
	case errors.Is(err, errHubNotRunning):
		fmt.Fprintln(stdout, i18n.T(l, "hub-cli.access_next_start"))
	default:
		fmt.Fprintln(stderr, i18n.T(l, "hub-cli.access_notify_failed", i18n.Args{"error": err}))
	}
	return nil
}

var errHubNotRunning = errors.New("hub not running")

// adminSetupCode prints a fresh first-run setup code from the running hub.
func adminSetupCode(ctx context.Context, dir string, stdout io.Writer) error {
	l := i18n.Terminal()
	cfg := &Config{Listen: ":18660", dir: dir}
	b, err := os.ReadFile(dir + "/hub.json")
	if err != nil {
		return &cliError{i18n.T(l, "hub-cli.no_hub_config", i18n.Args{"dir": dir, "error": err}), err}
	}
	_ = json.Unmarshal(b, cfg)
	cfg.applyEnv()
	resp, err := adminLoopbackPost(ctx, cfg, "/api/v1/admin/setup-code")
	if errors.Is(err, errHubNotRunning) {
		return errors.New(i18n.T(l, "hub-cli.setup_code_hub_down"))
	}
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	var out struct {
		Code      string    `json:"code"`
		ExpiresAt time.Time `json:"expires_at"`
	}
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	_ = json.Unmarshal(raw, &out)
	switch {
	case resp.StatusCode == http.StatusConflict:
		return errors.New(i18n.T(l, "hub-cli.setup_code_password_set", i18n.Args{"command": clitext.Named("personad hub admin reset-password")}))
	case resp.StatusCode != http.StatusOK || out.Code == "":
		// with the cause in detail (#5728)
		return errors.New(i18n.T(l, "hub-cli.setup_code_failed", i18n.Args{"status": resp.StatusCode, "error": apierr.Describe(raw)}))
	}
	fmt.Fprintln(stdout, i18n.T(l, "hub-cli.setup_code", i18n.Args{"code": out.Code, "expires": out.ExpiresAt.UTC().Format(time.RFC3339)}))
	return nil
}

// notifyAdminReload asks the hub on this machine to read admin.access again (POST /api/v1/admin/reload over
// loopback with the master token, like `hub pair`).
func notifyAdminReload(ctx context.Context, cfg *Config) error {
	resp, err := adminLoopbackPost(ctx, cfg, "/api/v1/admin/reload")
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d (an older hub without the admin UI?)", resp.StatusCode)
	}
	return nil
}

// adminLoopbackPost posts {} to the hub on this machine from loopback (managerconf.CLIHubURL, CLIDial) with the master
// token (errHubNotRunning when nothing answers).
func adminLoopbackPost(ctx context.Context, cfg *Config, path string) (*http.Response, error) {
	if cfg.Token == "" {
		return nil, errHubNotRunning
	}
	_, port, err := net.SplitHostPort(cfg.Listen)
	if n, perr := strconv.Atoi(port); err != nil || perr != nil || n <= 0 || n > 65535 {
		return nil, errors.New("hub.json listen has no valid port")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, managerconf.CLIHubURL(cfg.Listen)+path, strings.NewReader("{}"))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+cfg.Token)
	req.Header.Set("Content-Type", "application/json")
	transport := &http.Transport{Proxy: nil, DialContext: managerconf.CLIDial}
	defer transport.CloseIdleConnections()
	client := &http.Client{Timeout: 5 * time.Second, Transport: transport,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := client.Do(req)
	if err != nil {
		var ne *net.OpError
		if errors.As(err, &ne) {
			return nil, errHubNotRunning
		}
		return nil, err
	}
	return resp, nil
}

func adminResetPassword(ctx context.Context, dir string, args []string, stdin io.Reader, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("hub admin reset-password", flag.ContinueOnError)
	flags.SetOutput(stderr)
	clear := flags.Bool("clear", false, "clear the password: the next visit sets a new one (first-run setup)")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New(adminUsage)
	}
	l := i18n.Terminal()
	b, err := os.ReadFile(dir + "/hub.json")
	if err != nil {
		return &cliError{i18n.T(l, "hub-cli.no_hub_config", i18n.Args{"dir": dir, "error": err}), err}
	}
	cfg := &Config{}
	if err := json.Unmarshal(b, cfg); err != nil {
		return fmt.Errorf("hub.json: %w", err)
	}
	cfg.applyEnv()
	var password string
	if !*clear {
		if password, err = readNewPassword(l, stdin, stderr); err != nil {
			return err
		}
	}
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	database, err := db.Open(ctx, cfg.DatabaseURL, db.HubSet)
	if err != nil {
		return err
	}
	defer database.Close()
	if *clear {
		if err := database.ClearAdminPassword(ctx); err != nil {
			return err
		}
		fmt.Fprintln(stdout, i18n.T(l, "hub-cli.password_cleared"))
		return nil
	}
	hash, err := hashAdminPassword(password)
	if err != nil {
		return err
	}
	if err := database.SetAdminPasswordSignOut(ctx, hash, ""); err != nil {
		return err
	}
	fmt.Fprintln(stdout, i18n.T(l, "hub-cli.password_updated"))
	return nil
}

// readNewPassword asks twice without echo on a terminal; otherwise it reads one line from stdin (a script). A refused
// password says why in l.
func readNewPassword(l i18n.Lang, stdin io.Reader, stderr io.Writer) (string, error) {
	refused := func(problem error) error { return &cliError{i18n.Localize(problem, l), problem} }
	if f, ok := stdin.(*os.File); ok && term.IsTerminal(int(f.Fd())) {
		fmt.Fprint(stderr, i18n.T(l, "hub-cli.password_prompt")+" ")
		first, err := term.ReadPassword(int(f.Fd()))
		fmt.Fprintln(stderr)
		if err != nil {
			return "", err
		}
		fmt.Fprint(stderr, i18n.T(l, "hub-cli.password_again")+" ")
		second, err := term.ReadPassword(int(f.Fd()))
		fmt.Fprintln(stderr)
		if err != nil {
			return "", err
		}
		if string(first) != string(second) {
			return "", errors.New(i18n.T(l, "hub-cli.password_mismatch"))
		}
		if problem := adminPasswordProblem(string(first)); problem != nil {
			return "", refused(problem)
		}
		return string(first), nil
	}
	line, err := bufio.NewReader(io.LimitReader(stdin, 4096)).ReadString('\n')
	if err != nil && err != io.EOF {
		return "", err
	}
	password := strings.TrimRight(line, "\r\n")
	if problem := adminPasswordProblem(password); problem != nil {
		return "", refused(problem)
	}
	return password, nil
}
