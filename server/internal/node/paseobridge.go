package node

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/nwverse/persona/server/internal/assistantwire"
	"github.com/nwverse/persona/server/internal/compat"
	"github.com/nwverse/persona/server/internal/i18n"
	"github.com/nwverse/persona/server/internal/lazyre"
	"github.com/nwverse/persona/server/internal/osproc"
	"github.com/nwverse/persona/server/internal/proto"
	"github.com/nwverse/persona/server/internal/runtimepkg"
)

// The Paseo engine (docs/integrations/paseo.md). Paseo is a daemon running coding agents through their SDKs, with no terminal:
// the node reaches it through the Paseo bridge (integrations/paseo), a runtime (runtimes.go) that speaks Paseo's
// WebSocket protocol on one side and the runtime protocol on the other. The node carries the bridge (runtimepkg) and
// runs it itself while the machine's engine is Paseo — nothing to install: it writes the bridge out beside the
// runtime socket and runs it with a JavaScript runtime found on the machine (Paseo's own when it can), again when it
// exits, until the engine changes. The bridge dials the runtime socket like any runtime.

// paseoPackage is the bridge's package (runtimepkg.Packages).
const paseoPackage = "persona-paseo-runtime"

// paseoRuntimeID is the runtime id the bridge says hello with (the engine's sessions' space kind).
const paseoRuntimeID = proto.EnginePaseo

// paseoBridgeRestart bounds the wait before the bridge is run again after it exited: from paseoBridgeRestartMin,
// doubling, back to the minimum after it ran for paseoBridgeSteady. Tests shorten them.
var (
	paseoBridgeRestartMin = time.Second
	paseoBridgeRestartMax = time.Minute
	paseoBridgeSteady     = 5 * time.Minute
	paseoBridgeStopWait   = 3 * time.Second
)

// paseoHome is the Paseo home: PASEO_HOME, else ~/.paseo (as Paseo's own CLI).
func paseoHome() string {
	if h := strings.TrimSpace(os.Getenv("PASEO_HOME")); h != "" {
		return proto.ExpandHome(h, userHome())
	}
	return filepath.Join(userHome(), ".paseo")
}

func userHome() string {
	h, _ := os.UserHomeDir()
	return h
}

// paseoPidFile is what a running Paseo daemon writes in its home: its process and the address it listens on.
type paseoPidFile struct {
	PID    int    `json:"pid"`
	Listen string `json:"listen"`
}

// readPaseoPid reads home's paseo.pid (false when Paseo is not running there).
func readPaseoPid(home string) (paseoPidFile, bool) {
	var p paseoPidFile
	b, err := os.ReadFile(filepath.Join(home, "paseo.pid"))
	if err != nil || json.Unmarshal(b, &p) != nil {
		return paseoPidFile{}, false
	}
	return p, true
}

// paseoReachable: the Paseo daemon of home answers at the address its paseo.pid gives (a TCP or unix connection
// opens). Only there, as Paseo's own CLI: a default port may be another home's daemon.
func paseoReachable(ctx context.Context, home string) bool {
	p, ok := readPaseoPid(home)
	if !ok || p.Listen == "" {
		return false
	}
	listen := p.Listen
	network, addr := "tcp", listen
	if rest, ok := strings.CutPrefix(listen, "unix:"); ok {
		network, addr = "unix", strings.TrimPrefix(rest, "//")
	} else if host, port, err := net.SplitHostPort(listen); err == nil {
		if host == "0.0.0.0" || host == "" {
			host = "127.0.0.1"
		} else if host == "::" {
			host = "::1"
		}
		addr = net.JoinHostPort(host, port)
	}
	d := net.Dialer{Timeout: time.Second}
	c, err := d.DialContext(ctx, network, addr)
	if err != nil {
		return false
	}
	_ = c.Close()
	return true
}

// jsRuntime is a JavaScript runtime to run the bridge with: node, or an Electron app's binary run as node.
type jsRuntime struct {
	Path     string
	Electron bool   // ELECTRON_RUN_AS_NODE=1: Paseo's desktop app runs its daemon this way
	Version  string // the Node.js version it said it is (pickJSRuntime)
	From     string // where it was found, for the log
}

// minNodeMajor is the oldest Node.js the bridge runs on.
const minNodeMajor = 22

// findPaseoJSRuntime finds what runs the Paseo bridge: PERSONA_NODEJS when set; the runtime the Paseo daemon of home
// itself runs on (paseoDaemonRuntime: the CLI's node, or the desktop app's binary); else a node on PATH or where
// installers put it (a service's PATH is short); Paseo's desktop app last (paseoAppPaths): a Mac with only the app has
// a Node.js there, also before Paseo runs. Each is asked its version; too old ones are skipped.
func findPaseoJSRuntime(ctx context.Context, home string) (jsRuntime, error) {
	var cands []jsRuntime
	var tried []string // why the Paseo daemon's runtime was passed over, if it was
	if p := strings.TrimSpace(os.Getenv("PERSONA_NODEJS")); p != "" {
		cands = append(cands, jsRuntime{Path: p, Electron: !isNodeBinary(p), From: "PERSONA_NODEJS"})
	}
	if js, why := paseoDaemonRuntime(ctx, home); js.Path != "" {
		cands = append(cands, js)
	} else if why != "" {
		tried = append(tried, why)
	}
	if p, err := exec.LookPath("node"); err == nil {
		cands = append(cands, jsRuntime{Path: p, From: "PATH"})
	}
	for _, p := range commonNodePaths() {
		cands = append(cands, jsRuntime{Path: p, From: "installed"})
	}
	for _, p := range paseoAppPaths(runtime.GOOS, userHome()) {
		cands = append(cands, jsRuntime{Path: p, Electron: true, From: "Paseo's app"})
	}
	return pickJSRuntime(ctx, cands, tried)
}

// pickJSRuntime is the first of cands there that says it is Node.js minNodeMajor or newer, with that version; the
// error says what was tried.
func pickJSRuntime(ctx context.Context, cands []jsRuntime, tried []string) (jsRuntime, error) {
	seen := map[string]bool{}
	for _, c := range cands {
		if seen[c.Path] {
			continue
		}
		seen[c.Path] = true
		if st, err := os.Stat(c.Path); err != nil || st.IsDir() {
			continue
		}
		v, err := jsVersion(ctx, c)
		if err != nil {
			tried = append(tried, c.Path+": "+err.Error())
			continue
		}
		if major, _ := strconv.Atoi(strings.SplitN(strings.TrimPrefix(v, "v"), ".", 2)[0]); major < minNodeMajor {
			tried = append(tried, fmt.Sprintf("%s: Node.js %s is older than %d", c.Path, v, minNodeMajor))
			continue
		}
		c.Version = v
		return c, nil
	}
	if len(tried) > 0 {
		return jsRuntime{}, fmt.Errorf("no usable Node.js (%s)", strings.Join(tried, "; "))
	}
	return jsRuntime{}, errors.New("no Node.js found (set PERSONA_NODEJS)")
}

// paseoDaemonRuntime is the runtime the Paseo daemon of home runs on: the executable of the process its paseo.pid
// names, as the kernel says (processExecutable). Only while that daemon answers there (paseoReachable), and only a
// Node.js or an Electron app: a paseo.pid left behind by a Paseo that crashed, or before a reboot, names a process
// that may now be any program of this user, and asking it its version runs it with -e … (and, were it taken for an
// app, ELECTRON_RUN_AS_NODE). why says why a running daemon's executable was passed over ("" otherwise).
func paseoDaemonRuntime(ctx context.Context, home string) (js jsRuntime, why string) {
	p, ok := readPaseoPid(home)
	if !ok || p.PID <= 0 || !paseoReachable(ctx, home) {
		return jsRuntime{}, ""
	}
	exe := processExecutable(p.PID)
	switch {
	case exe == "":
		return jsRuntime{}, ""
	case isNodeBinary(exe):
		return jsRuntime{Path: exe, From: "the Paseo daemon's"}, ""
	case isElectronApp(exe):
		return jsRuntime{Path: exe, Electron: true, From: "the Paseo daemon's"}, ""
	}
	return jsRuntime{}, exe + ": the Paseo daemon runs on it, and it is neither a Node.js nor an Electron app"
}

// isNodeBinary: the file is a node (by name), not an app binary that runs as node only with ELECTRON_RUN_AS_NODE.
func isNodeBinary(path string) bool {
	b := strings.ToLower(filepath.Base(path))
	return b == "node" || b == "node.exe" || strings.HasPrefix(b, "node-") || strings.HasPrefix(b, "nodejs")
}

// isElectronApp: the file is an Electron app's binary, which runs as Node.js with ELECTRON_RUN_AS_NODE, told by what
// every Electron app ships: on a Mac, Electron's framework in the app bundle the file is in (the app's own binary or
// one of its helpers inside it), elsewhere the app's resources/app.asar (or resources/app) beside it.
func isElectronApp(path string) bool {
	dir := filepath.Dir(path)
	if st, err := os.Stat(filepath.Join(dir, "resources", "app.asar")); err == nil && !st.IsDir() {
		return true
	}
	if dirExists(filepath.Join(dir, "resources", "app")) {
		return true
	}
	for d := dir; ; d = filepath.Dir(d) {
		if filepath.Base(d) == "Contents" && strings.HasSuffix(filepath.Dir(d), ".app") &&
			dirExists(filepath.Join(d, "Frameworks", "Electron Framework.framework")) {
			return true
		}
		if filepath.Dir(d) == d {
			return false
		}
	}
}

// paseoAppPaths are where Paseo's desktop app keeps its binary on a Mac (the system's Applications folder, then the
// user's); nothing elsewhere. The app runs its daemon on that binary as Node.js.
func paseoAppPaths(goos, home string) []string {
	if goos != "darwin" {
		return nil
	}
	const bin = "Paseo.app/Contents/MacOS/Paseo"
	return []string{filepath.Join("/Applications", bin), filepath.Join(home, "Applications", bin)}
}

// commonNodePaths are where Node.js installers put node, newest nvm version first.
func commonNodePaths() []string {
	home := userHome()
	var out []string
	for _, dir := range agentSystemDirs { // Homebrew, /usr/local
		out = append(out, filepath.Join(dir, "node"))
	}
	out = append(out, filepath.Join(distroNodeDir, "node"), filepath.Join(home, ".volta", "bin", "node"),
		filepath.Join(home, ".local", "bin", "node"))
	nvm, _ := filepath.Glob(filepath.Join(home, ".nvm", "versions", "node", "*", "bin", "node"))
	sort.Sort(sort.Reverse(sort.StringSlice(nvm)))
	return append(out, nvm...)
}

// jsVersion is the Node.js version a runtime reports.
func jsVersion(ctx context.Context, js jsRuntime) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, js.Path, "-e", "process.stdout.write(process.versions.node)")
	cmd.Env = js.env(os.Environ())
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	v := strings.TrimSpace(string(out))
	if v == "" {
		return "", errors.New("no version")
	}
	return v, nil
}

// env is base to run js with, in a new slice: ELECTRON_RUN_AS_NODE=1 for an app binary, and never for a node. A node
// started from an Electron host (an editor's extension host starts its children with it) inherits the variable; it
// is dropped, or everything the bridge starts would get it too, and an Electron app started from there would run as
// a bare Node.js.
func (js jsRuntime) env(base []string) []string {
	out := make([]string, 0, len(base)+1)
	for _, kv := range base {
		if !strings.HasPrefix(kv, "ELECTRON_RUN_AS_NODE=") {
			out = append(out, kv)
		}
	}
	if js.Electron {
		out = append(out, "ELECTRON_RUN_AS_NODE=1")
	}
	return out
}

// processExecutable is the executable a process of this user runs, as the kernel says ("" when it cannot be told):
// /proc/<pid>/exe on Linux, KERN_PROCARGS2's executable path on macOS (processExecPath). Never the process's name,
// which it may change: Paseo's supervisor calls itself "Paseo Supervisor". Tests replace it.
var processExecutable = func(pid int) string {
	p, err := processExecPath(pid)
	if err != nil || !filepath.IsAbs(p) {
		return ""
	}
	return p
}

// writePackage writes a package's files out under dir/<hash> once (a version written before is reused) and returns
// that directory. Written beside and renamed in, so a half-written version is never run.
func writePackage(dir string, pkg *runtimepkg.Package) (string, error) {
	final := filepath.Join(dir, pkg.Hash)
	if st, err := os.Stat(final); err == nil && st.IsDir() {
		return final, nil
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	tmp, err := os.MkdirTemp(dir, ".write-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(tmp)
	for _, f := range pkg.Files {
		if !runtimepkg.ValidPath(f.Path) {
			return "", fmt.Errorf("bad path %q in package %s", f.Path, pkg.Name)
		}
		p := filepath.Join(tmp, filepath.FromSlash(f.Path))
		if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
			return "", err
		}
		if err := os.WriteFile(p, f.Data, 0o600); err != nil {
			return "", err
		}
	}
	if err := os.Rename(tmp, final); err != nil && !dirExists(final) {
		return "", err
	}
	// Older versions go; the one written now stays.
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if e.IsDir() && e.Name() != pkg.Hash && !strings.HasPrefix(e.Name(), ".") {
			_ = os.RemoveAll(filepath.Join(dir, e.Name()))
		}
	}
	return final, nil
}

func dirExists(p string) bool {
	st, err := os.Stat(p)
	return err == nil && st.IsDir()
}

// paseoBridge runs the bridge for a runtime host while the engine wants it.
type paseoBridge struct {
	mu     sync.Mutex
	cancel context.CancelFunc // ends the running loop; nil while it does not run
	done   chan struct{}
	// status is why the last run could not start or ended, for node.info: what the bridge printed about it when it
	// did (paseoExited), else the error. "" until a run fails; a run that starts leaves it, so a bridge that exits
	// again and again keeps saying why while the next run starts.
	status string
	since  time.Time // when the bridge that runs now started (zero while none runs)
	tail   []string  // the last paseoBridgeTail lines that bridge printed, oldest first
}

// paseoBridgeTail is how many of the bridge's last lines the node keeps for its notes.
const paseoBridgeTail = 50

// paseoBridgeConnectWait is how long a bridge may run without connecting before node.info says so: it connects in a
// second or two, once it has read Paseo's state. Tests shorten it.
var paseoBridgeConnectWait = 30 * time.Second

// setPaseoBridge runs the bridge (on) or stops it (off) for this runtime host. Called whenever a hub connection says
// which engine the machine uses.
func (rh *RuntimeHost) setPaseoBridge(on bool) {
	b := &rh.paseo
	b.mu.Lock()
	defer b.mu.Unlock()
	if on == (b.cancel != nil) {
		return
	}
	if !on {
		b.cancel()
		b.cancel = nil
		done := b.done
		b.mu.Unlock()
		<-done
		b.mu.Lock()
		rh.logger.Printf("paseo bridge: stopped (the engine is not Paseo any more)")
		return
	}
	base := rh.ctx
	if base == nil {
		base = context.Background()
	}
	ctx, cancel := context.WithCancel(base)
	b.cancel, b.done, b.status, b.since, b.tail = cancel, make(chan struct{}), "", time.Time{}, nil
	go rh.paseoBridgeLoop(ctx, b.done)
}

// paseoBridgeNote is node.info's note on a Paseo that answers while its bridge is not connected, "" while the bridge
// is not wanted or still has time to connect. Once it has run paseoBridgeConnectWait without connecting, the last
// thing it printed (Paseo refusing it, say); else why the last run could not start or ended. In the canonical Chinese
// of the node-engine catalog, which the hub words in each reader's language (i18n.EngineText).
func (rh *RuntimeHost) paseoBridgeNote() string {
	b := &rh.paseo
	b.mu.Lock()
	defer b.mu.Unlock()
	switch {
	case b.cancel == nil:
		return ""
	case !b.since.IsZero() && time.Since(b.since) >= paseoBridgeConnectWait:
		if line := lastBridgeLine(b.tail); line != "" {
			return i18n.T(i18n.Default, "node-engine.paseo_not_connected_why", i18n.Args{"reason": line})
		}
		return i18n.T(i18n.Default, "node-engine.paseo_not_connected")
	case b.status != "":
		return i18n.T(i18n.Default, "node-engine.paseo_unreachable", i18n.Args{"reason": b.status})
	}
	return ""
}

// paseoBridgeRunning marks a bridge run that started (on) or ended (off): its lines are kept from the start.
func (rh *RuntimeHost) paseoBridgeRunning(on bool) {
	rh.paseo.mu.Lock()
	defer rh.paseo.mu.Unlock()
	if on {
		rh.paseo.since, rh.paseo.tail = time.Now(), nil
	} else {
		rh.paseo.since = time.Time{}
	}
}

// paseoBridgeLine keeps a line the bridge printed.
func (rh *RuntimeHost) paseoBridgeLine(line string) {
	rh.paseo.mu.Lock()
	defer rh.paseo.mu.Unlock()
	if len(rh.paseo.tail) == paseoBridgeTail {
		rh.paseo.tail = append(rh.paseo.tail[:0], rh.paseo.tail[1:]...)
	}
	rh.paseo.tail = append(rh.paseo.tail, line)
}

func (rh *RuntimeHost) setPaseoStatus(s string) {
	rh.paseo.mu.Lock()
	rh.paseo.status = s
	rh.paseo.mu.Unlock()
}

// paseoBridgeLoop runs the bridge until ctx ends, again after it exits (backing off).
func (rh *RuntimeHost) paseoBridgeLoop(ctx context.Context, done chan struct{}) {
	defer close(done)
	wait := paseoBridgeRestartMin
	for ctx.Err() == nil {
		started := time.Now()
		err := rh.runPaseoBridge(ctx)
		if ctx.Err() != nil {
			return
		}
		if time.Since(started) > paseoBridgeSteady {
			wait = paseoBridgeRestartMin
		}
		if err != nil {
			rh.setPaseoStatus(paseoFailure(err))
			rh.logger.Printf("paseo bridge: %v (again in %s)", err, wait)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}
		wait = min(wait*2, paseoBridgeRestartMax)
	}
}

// paseoBridgeCommand is how the bridge is run (tests replace it with a fake that need not be JavaScript).
var paseoBridgeCommand = func(ctx context.Context, rh *RuntimeHost) (*exec.Cmd, error) {
	pkg, err := runtimepkg.Load(paseoPackage)
	if err != nil {
		return nil, err
	}
	if pkg == nil {
		return nil, errors.New("this personad carries no Paseo bridge (built without go generate ./internal/runtimepkg)")
	}
	dir, err := writePackage(filepath.Join(filepath.Dir(rh.path), "runtime-packages", paseoPackage), pkg)
	if err != nil {
		return nil, fmt.Errorf("write the bridge: %w", err)
	}
	home := paseoHome()
	js, err := findPaseoJSRuntime(ctx, home)
	if err != nil {
		return nil, err
	}
	rh.logger.Printf("paseo bridge: Node.js %s, %s: %s", js.Version, js.From, js.Path)
	cmd := exec.Command(js.Path, paseoBridgeArgs(dir, rh.path, home)...)
	cmd.Env = js.env(append(os.Environ(), "PERSONA_RUNTIME_SOCKET="+rh.path))
	cmd.Dir = dir
	return cmd, nil
}

// paseoBridgeArgs is the bridge's command line after the JavaScript runtime. The node's mcp.json (beside the runtime
// socket) is what the bridge gives each new Agent as its Persona tools, and of those the Agent's own run without a card
// (--allow-tools: agentAllowedTools without the office rule, which the bridge's Agents do not get).
func paseoBridgeArgs(dir, socket, home string) []string {
	return []string{filepath.Join(dir, "paseo", "bridge.mjs"), "--socket", socket, "--paseo-home", home,
		"--mcp-config", filepath.Join(filepath.Dir(socket), "mcp.json"),
		"--allow-tools", strings.Join(compat.MCPAllowedToolNames(assistantwire.AgentOwnTools, true), ",")}
}

// runPaseoBridge runs the bridge once, until it exits or ctx ends (then it is stopped: SIGTERM to its group, SIGKILL
// after paseoBridgeStopWait). Its output goes to the log, line by line, and its last lines are kept for node.info.
func (rh *RuntimeHost) runPaseoBridge(ctx context.Context) error {
	cmd, err := paseoBridgeCommand(ctx, rh)
	if err != nil {
		return err
	}
	cmd.SysProcAttr = osproc.GroupAttr()
	out, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	cmd.Stderr = cmd.Stdout
	// Held open while the bridge runs: it exits when this ends, so a node that dies (killed, no time to stop its
	// group) leaves no bridge behind holding the runtime id against the next node's.
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	defer stdin.Close()
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start: %w", err)
	}
	rh.paseoBridgeRunning(true)
	defer rh.paseoBridgeRunning(false)
	rh.logger.Printf("paseo bridge: running (pid %d)", cmd.Process.Pid)
	logged := make(chan struct{})
	go func() {
		defer close(logged)
		logLines(out, func(line string) {
			rh.logger.Printf("paseo bridge: %s", line)
			rh.paseoBridgeLine(line)
		})
	}()
	exited := make(chan error, 1)
	go func() {
		<-logged
		exited <- cmd.Wait()
	}()
	select {
	case err := <-exited:
		rh.paseo.mu.Lock()
		said := bridgeErrorLines(rh.paseo.tail)
		rh.paseo.mu.Unlock()
		return &paseoExited{err: err, said: said}
	case <-ctx.Done():
		_ = osproc.StopGroup(cmd.Process.Pid, false)
		select {
		case <-exited:
		case <-time.After(paseoBridgeStopWait):
			_ = osproc.StopGroup(cmd.Process.Pid, true)
			<-exited
		}
		return ctx.Err()
	}
}

// paseoExited is a bridge run that ended by itself: how it exited (nil: status 0), for the log, and what the bridge
// printed about it (bridgeErrorLines), for node.info.
type paseoExited struct {
	err  error
	said string
}

func (e *paseoExited) Error() string {
	if e.err != nil {
		return "exited: " + e.err.Error()
	}
	return "exited"
}

func (e *paseoExited) Unwrap() error { return e.err }

// paseoFailure is why a bridge run failed, for node.info: the error lines of a bridge that exited when it printed
// any ("exited: exit status 1" says nothing), else the error itself (no Node.js, say).
func paseoFailure(err error) string {
	var exited *paseoExited
	if errors.As(err, &exited) && exited.said != "" {
		return exited.said
	}
	return err.Error()
}

// nodeErrorHead is the first line of an error as Node.js prints one it was not given to handle ("SyntaxError: …",
// "Error [ERR_MODULE_NOT_FOUND]: …", "DOMException [AbortError]: …"), and as the bridge prints one ("uncaught: Error:
// …", bridge.mjs).
var nodeErrorHead = lazyre.New(`^(uncaught: )?[A-Z]?\w*(Error|Exception)( \[\w+\])?: `)

// bridgeNoise: a line of a bridge's output that says nothing of why it stopped: blank, a stack frame, the source
// line and caret Node.js shows above an error, the properties it shows after one, and its version and hint below.
func bridgeNoise(line string) bool {
	t := strings.TrimSpace(line)
	return t == "" || strings.HasPrefix(t, "at ") || strings.Trim(t, "^ ") == "" || t == "{" || t == "}" ||
		strings.HasPrefix(t, "Node.js v") || strings.HasPrefix(t, "(Use `node --trace") ||
		strings.HasPrefix(line, "  ") && strings.Contains(t, ": ") && !nodeErrorHead.MatchString(t)
}

// bridgeErrorLines is what a bridge that exited said about it: the last error it printed (nodeErrorHead) with the
// rest of that error's message (up to three lines in all, the stack left out), else its last line that is not noise;
// "" when it printed nothing else. Clipped for a note.
func bridgeErrorLines(lines []string) string {
	for i := len(lines) - 1; i >= 0; i-- {
		if !nodeErrorHead.MatchString(lines[i]) {
			continue
		}
		said := []string{strings.TrimSpace(lines[i])}
		for _, l := range lines[i+1:] {
			if len(said) == 3 || bridgeNoise(l) {
				break
			}
			said = append(said, strings.TrimSpace(l))
		}
		return clipNote(strings.Join(said, " "))
	}
	for i := len(lines) - 1; i >= 0; i-- {
		if !bridgeNoise(lines[i]) {
			return clipNote(strings.TrimSpace(lines[i]))
		}
	}
	return ""
}

// lastBridgeLine is the last line a running bridge printed, clipped for a note ("" before it printed any).
func lastBridgeLine(lines []string) string {
	for i := len(lines) - 1; i >= 0; i-- {
		if t := strings.TrimSpace(lines[i]); t != "" {
			return clipNote(t)
		}
	}
	return ""
}

// clipNote is s cut to what a note under an engine shows (the apps show one line, the web the rest on hover).
func clipNote(s string) string {
	const most = 300
	if r := []rune(s); len(r) > most {
		return string(r[:most]) + "…"
	}
	return s
}

// logLines hands each line of r to f (long lines cut), until r ends. A line longer than the buffer is cut too and
// its rest dropped, and the lines after it still come: nothing else reads r, and a program whose log nobody reads
// is stuck on its next write once the pipe is full (#3959).
func logLines(r io.Reader, f func(string)) {
	br := bufio.NewReaderSize(r, 64<<10)
	for {
		b, err := br.ReadSlice('\n')
		line := strings.TrimRight(string(b), "\r\n") // copied: the next read reuses b
		long := errors.Is(err, bufio.ErrBufferFull)
		for errors.Is(err, bufio.ErrBufferFull) {
			_, err = br.ReadSlice('\n')
		}
		if len(b) > 0 {
			if long || len(line) > 2000 {
				line = line[:min(len(line), 2000)] + "…"
			}
			f(line)
		}
		if err != nil {
			return
		}
	}
}

// paseoMinVersion is the oldest Paseo Persona is tested with: older ones have known problems (0.14 cancels a turn when
// a message comes while it starts). One still runs, with a note asking to update it.
var paseoMinVersion = [3]int{0, 15, 0}

// paseoVersionNote is node.info's note on a Paseo of version v: one asking to update it when v is older than
// paseoMinVersion, "" for a newer one or a version it cannot read.
func paseoVersionNote(v string) string {
	if !versionAtLeast(v, [3]int{}) || versionAtLeast(v, paseoMinVersion) {
		return ""
	}
	least := fmt.Sprintf("%d.%d.%d", paseoMinVersion[0], paseoMinVersion[1], paseoMinVersion[2])
	return i18n.T(i18n.Default, "node-engine.paseo_outdated", i18n.Args{"version": v, "minimum": least})
}

// paseoEngine is the Paseo engine as found on this machine for node.info: available when the bridge is connected or
// the Paseo daemon answers; its version is Paseo's (what the bridge said in its hello). note asks to update a Paseo
// older than paseoMinVersion, or says why the bridge is not connected while Paseo answers (paseoBridgeNote).
func (n *Node) paseoEngine(ctx context.Context) proto.Engine {
	e := proto.Engine{ID: proto.EnginePaseo, Name: proto.EngineNames[proto.EnginePaseo]}
	var h *runtimeConn
	if n.runtimes != nil {
		n.runtimes.mu.Lock()
		h = n.runtimes.byRuntime(paseoRuntimeID)
		n.runtimes.mu.Unlock()
	}
	if h != nil {
		h.mu.Lock()
		e.Version = h.hello.Version
		h.mu.Unlock()
		e.Available = true
		e.Note = paseoVersionNote(e.Version)
		return e
	}
	e.Available = paseoReachable(ctx, paseoHome())
	if n.runtimeHost != nil && e.Available {
		e.Note = n.runtimeHost.paseoBridgeNote()
	}
	return e
}
