package hub

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/user"
	"runtime"
	"strconv"
	"time"

	"github.com/nwverse/persona/server/internal/identity"
)

// `personad hub setup` (docs/assistant/onboarding.md 4.1, 5.5): one command that leaves a working hub on this machine. It writes
// hub.json (a whole new file only when there is none yet), makes sure there is a database (the same Postgres container
// scripts/deploy/db.sh makes, or the one hub.json names), installs the service that keeps the hub running (the same
// systemd units as scripts/deploy/hub-systemd.sh; a launchd agent on a Mac), waits for the hub, has the built-in manager
// bound and installed as a service of its own (setup_manager.go), checks it, and prints what to do next: a QR code for
// the phone and the admin URL with the first-run setup code in its fragment.
//
// Every step prints one line; a step that fails says what to do and stops. Running it again is safe: values already
// in hub.json are never changed, and a service that is already right is left running.

// ErrSetupStopped: a step failed and said why; the command exits non-zero without printing anything more.
var ErrSetupStopped = errors.New("hub setup stopped")

type setupOptions struct {
	yes       bool
	listen    string
	tls       string
	listenSet bool
	tlsSet    bool
	outside   bool
	noService bool
	json      bool
	// managerService: an existing hub's built-in manager becomes a service of its own (a new install's is anyway).
	managerService bool
}

// The database `personad hub setup` creates: scripts/deploy/db.sh's container, so deploy.sh keeps working with it.
const (
	setupDBContainer = "persona-postgres"
	setupDBVolume    = "persona-pgdata"
	setupDBPort      = 18661
	hubLaunchdLabel  = "com.persona.hub" // like com.persona.node (persona-link's nodesvc.go)
)

// SetupCommand is `personad hub setup [--yes] [--listen :18660] [--tls :18662] [--outside] [--no-service]
// [--manager-service] [--json]`.
func SetupCommand(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("hub setup", flag.ContinueOnError)
	flags.SetOutput(stderr)
	var o setupOptions
	flags.BoolVar(&o.yes, "yes", false, "不问问题，都按默认做（包括用 sudo 打开 linger）")
	flags.StringVar(&o.listen, "listen", ":18660", "新装时 hub 的明文端口")
	flags.StringVar(&o.tls, "tls", ":18662", "新装时 hub 的 TLS 端口（不写地址时只绑局域网 IPv4）")
	flags.BoolVar(&o.outside, "outside", false, "出门也要用：TLS 端口绑所有地址，自检也查公网")
	flags.BoolVar(&o.noService, "no-service", false, "不装常驻服务（自己运行 personad hub run）；hub 自己拉起内置管理者")
	flags.BoolVar(&o.managerService, "manager-service", false, "已有的 hub：把内置管理者改成自己的常驻服务（新装的本来就是）")
	flags.BoolVar(&o.json, "json", false, "一行一个 JSON：每步 {step, ok, detail, next}，最后 {done, …}")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("hub setup 不接受位置参数")
	}
	if o.noService && o.managerService {
		return fmt.Errorf("--manager-service 要装内置管理者的常驻服务，不能和 --no-service 一起用")
	}
	flags.Visit(func(f *flag.Flag) {
		o.listenSet = o.listenSet || f.Name == "listen"
		o.tlsSet = o.tlsSet || f.Name == "tls"
	})
	for _, a := range []string{o.listen, o.tls} {
		if _, err := doctorPort(a); err != nil {
			return fmt.Errorf("%q 不是有效的监听地址：写成 :端口 或 地址:端口", a)
		}
	}
	e, err := newSetupEnv(stdout, o)
	if err != nil {
		return err
	}
	return e.setup(ctx)
}

// setupEnv is everything setup touches outside its own files, so that tests swap them out.
type setupEnv struct {
	home, root string
	goos       string
	uid        int
	user       string
	out        io.Writer
	o          setupOptions

	run      func(ctx context.Context, env []string, name string, args ...string) ([]byte, error)
	runTTY   func(ctx context.Context, name string, args ...string) error // sudo: the password prompt needs the terminal
	lookPath func(string) (string, error)
	ask      func(question string) (yes, asked bool)
	lanIPv4  func() string
	portFree func(port int) bool
	exe      func() (string, error)
	pingDB   func(ctx context.Context, url string) error
	doctor   func(ctx context.Context, dir string, outside bool) []string
	getenv   func(string) string
	sleep    func(time.Duration)

	isContainer func() bool
	// restartHub: config changed what a running hub reads only at start, so service restarts it; restartWhy says what.
	restartHub bool
	restartWhy []string
	// hubRestart restarts the hub's service again (set by service when the hub runs as it): undoing a switch to the
	// built-in manager's own service that could not be finished. switched: this run set assistant.mode service on a
	// hub.json a hub had run with (--manager-service); prevMode is what it was ("" for none).
	hubRestart func(ctx context.Context) error
	switched   bool
	prevMode   string
	// builtinCode makes a one-time code that binds the built-in manager's own service (ManagerCode's); voidCode voids
	// it when the manager's program did not trade it.
	builtinCode func(ctx context.Context, cfg *Config) (string, error)
	voidCode    func(ctx context.Context, cfg *Config, code string) error
	// keptMachines: machine → the manager that keeps it, as the hub's database has it (keptMachines).
	keptMachines func(ctx context.Context, cfg *Config) (map[string]string, error)

	healthWait  time.Duration
	dbWait      time.Duration
	managerWait time.Duration // for the built-in manager's service to come online
	// The database setup creates; another name only for tests and trial runs (_PERSONA_SETUP_DB_CONTAINER…).
	dbContainer, dbVolume string
	dbPort                int
}

func newSetupEnv(out io.Writer, o setupOptions) (*setupEnv, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	root, err := identity.PrepareHome(home)
	if err != nil {
		return nil, err
	}
	name := os.Getenv("USER")
	if u, err := user.Current(); err == nil {
		name = u.Username
	}
	e := &setupEnv{
		home: home, root: root, goos: runtime.GOOS, uid: os.Getuid(), user: name, out: out, o: o,
		run:         runCommand,
		runTTY:      runOnTerminal,
		lookPath:    exec.LookPath,
		ask:         askTerminal,
		lanIPv4:     defaultLANIPv4,
		portFree:    tcpPortFree,
		exe:         ServerBinary, // persona-hub setup installs the personad beside it (the service: splitHub)
		pingDB:      pingPostgres,
		doctor:      setupDoctor,
		getenv:      identity.Getenv,
		sleep:       time.Sleep,
		isContainer: inContainer,
		builtinCode: func(ctx context.Context, cfg *Config) (string, error) {
			code, _, err := builtinCode(ctx, cfg)
			return code, err
		},
		voidCode:     voidBuiltinCode,
		keptMachines: keptMachines,

		healthWait:  90 * time.Second,
		dbWait:      30 * time.Second,
		managerWait: 90 * time.Second,
		dbContainer: setupDBContainer, dbVolume: setupDBVolume, dbPort: setupDBPort,
	}
	if o.json {
		e.ask = func(string) (bool, bool) { return false, false } // nobody to ask: the caller is a program
	}
	// Hidden, for trying setup out next to a real hub on the same machine (never in docs for users).
	if v := os.Getenv("_PERSONA_SETUP_DB_CONTAINER"); v != "" {
		e.dbContainer, e.dbVolume = v, v+"-data"
	}
	if v, err := strconv.Atoi(os.Getenv("_PERSONA_SETUP_DB_PORT")); err == nil && v > 0 && v < 65536 {
		e.dbPort = v
	}
	return e, nil
}

// --- output ---------------------------------------------------------------------------------------------------

var setupStepNames = map[string]string{
	"config": "配置", "database": "数据库", "service": "常驻", "health": "启动", "manager": "管理者", "doctor": "自检",
}

func (e *setupEnv) report(step string, ok bool, detail, next string) {
	if e.o.json {
		b, _ := marshalNoEscape(map[string]any{"step": step, "ok": ok, "detail": detail, "next": next})
		_, _ = e.out.Write(append(b, '\n'))
		return
	}
	mark := "完成"
	if !ok {
		mark = "有问题"
	}
	fmt.Fprintf(e.out, "%s｜%s：%s\n", mark, setupStepNames[step], detail)
	if next != "" {
		fmt.Fprintf(e.out, "  下一步：%s\n", next)
	}
}

// fail reports a step that stops setup.
func (e *setupEnv) fail(step, detail, next string) error {
	e.report(step, false, detail, next)
	return ErrSetupStopped
}

// --- the steps ------------------------------------------------------------------------------------------------

func (e *setupEnv) setup(ctx context.Context) error {
	if !e.o.json {
		fmt.Fprintln(e.out, "安装 hub（personad hub setup）")
	}
	cfg, err := e.config()
	if err != nil {
		return err
	}
	if err := e.database(ctx, cfg); err != nil {
		return err
	}
	if err := e.localMachine(ctx, cfg); err != nil {
		return err
	}
	if err := e.service(ctx, cfg); err != nil {
		return err
	}
	if err := e.health(ctx, cfg); err != nil {
		return err
	}
	if err := e.builtinManager(ctx, cfg); err != nil {
		return err
	}
	problems := e.doctor(ctx, e.root, e.o.outside)
	for _, p := range problems {
		e.report("doctor", false, p, "")
	}
	if len(problems) == 0 {
		e.report("doctor", true, "没发现问题", "")
	}
	return e.finish(ctx, cfg)
}
