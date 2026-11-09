package node

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/nwverse/persona/server/internal/proto"
	"github.com/nwverse/persona/server/internal/rpc"
)

// The Cindy engine (docs/integrations/engines.md). Cindy runs coding agents (Claude Code, Codex, Pi) in its desktop
// app and loopback host service (`cindy-host`), storing sessions and messages in its profile database and Claude
// transcripts on disk. While the machine's engine is Cindy, the node runs an in-process runtime bridge (runtimes.go)
// that talks to Cindy's loopback host API (`host-endpoint.json` / `CINDY_HOST_API`) or reads its profile database
// (`cindy-<owner>.db`) when only the database is on disk.

// cindyRuntimeID is the runtime id the Cindy bridge registers with (and its sessions' space kind and host).
const cindyRuntimeID = proto.EngineCindy

var (
	// cindyBridgePoll is how often the bridge refreshes Cindy's sessions and pending interactions.
	cindyBridgePoll = 1500 * time.Millisecond
	// cindyBridgeRestart is the wait before the bridge reconnects after its loop ends.
	cindyBridgeRestart = 2 * time.Second
	// cindyAskRetry is how long a card the node could not take (no hub connected yet) waits before retrying.
	cindyAskRetry = 5 * time.Second
)

// cindyBin finds the installed Cindy executable; tests replace it or set CINDY_HOME to isolate from the host machine.
var cindyBin = findCindyBin

// cindyDataDirs returns the Cindy profile directories to inspect on this machine. When CINDY_HOME, CINDY_HOST_DATA_DIR
// or XDT_USER_DATA_DIR is set, only that directory is used (so tests setting CINDY_HOME never touch the user's profile).
func cindyDataDirs() []string {
	for _, env := range []string{"CINDY_HOME", "CINDY_HOST_DATA_DIR", "XDT_USER_DATA_DIR"} {
		if v := strings.TrimSpace(os.Getenv(env)); v != "" {
			return []string{proto.ExpandHome(v, userHome())}
		}
	}
	base := cindyConfigBase()
	if base == "" {
		return nil
	}
	return []string{
		filepath.Join(base, "CindyGlobal"),
		filepath.Join(base, "Cindy"),
	}
}

func cindyConfigBase() string {
	home := userHome()
	switch runtime.GOOS {
	case "darwin":
		if home != "" {
			return filepath.Join(home, "Library", "Application Support")
		}
	case "windows":
		if v := strings.TrimSpace(os.Getenv("APPDATA")); v != "" {
			return v
		}
		if home != "" {
			return filepath.Join(home, "AppData", "Roaming")
		}
	default:
		if v := strings.TrimSpace(os.Getenv("XDG_CONFIG_HOME")); v != "" {
			return v
		}
		if home != "" {
			return filepath.Join(home, ".config")
		}
	}
	return ""
}

func cindyExplicitHome() bool {
	for _, env := range []string{"CINDY_HOME", "CINDY_HOST_DATA_DIR", "XDT_USER_DATA_DIR"} {
		if strings.TrimSpace(os.Getenv(env)) != "" {
			return true
		}
	}
	return false
}

func findCindyBin() string {
	if p := strings.TrimSpace(os.Getenv("CINDY_BIN")); p != "" {
		p = proto.ExpandHome(p, userHome())
		if isExecutableFile(p) {
			return p
		}
		return ""
	}
	if cindyExplicitHome() {
		return ""
	}
	if p, err := exec.LookPath("cindy"); err == nil && p != "" {
		return p
	}
	home := userHome()
	var cands []string
	if home != "" {
		cands = append(cands,
			filepath.Join(home, ".local", "bin", "cindy"),
			filepath.Join(home, ".local", "opt", "cindy", "current", "Cindy"),
			filepath.Join(home, "Applications", "Cindy.app", "Contents", "MacOS", "Cindy"),
		)
	}
	cands = append(cands,
		"/Applications/Cindy.app/Contents/MacOS/Cindy",
		"/opt/Cindy/Cindy",
		"/usr/local/bin/cindy",
		"/usr/bin/cindy",
	)
	for _, c := range cands {
		if isExecutableFile(c) {
			return c
		}
	}
	return ""
}

func isExecutableFile(p string) bool {
	st, err := os.Stat(p)
	return err == nil && !st.IsDir()
}

// cindyInstalledVersion reads Cindy's version from its installation metadata or run markers without launching the GUI.
func cindyInstalledVersion(bin string, dirs []string) string {
	for _, dir := range dirs {
		if v := cindyMarkerVersion(dir); v != "" {
			return v
		}
	}
	if bin == "" {
		return ""
	}
	for _, root := range cindyInstallRoots(bin) {
		// Linux user/package install: resources/linux-build-info has line 1 "cindy-linux-v1", line 2 "<version>".
		if b, err := os.ReadFile(filepath.Join(root, "resources", "linux-build-info")); err == nil {
			lines := strings.Split(strings.TrimSpace(string(b)), "\n")
			if len(lines) >= 2 && strings.HasPrefix(strings.TrimSpace(lines[0]), "cindy-") {
				if v := strings.TrimSpace(lines[1]); v != "" {
					return v
				}
			}
		}
		// User-installed release directory: releases/<version>-<sha256>
		if filepath.Base(filepath.Dir(root)) == "releases" {
			base := filepath.Base(root)
			if i := strings.IndexByte(base, '-'); i > 0 {
				if v := base[:i]; v != "" && v[0] >= '0' && v[0] <= '9' {
					return v
				}
			}
		}
		// macOS app bundle: Contents/Info.plist
		if v := plistVersion(filepath.Join(root, "..", "Info.plist")); v != "" {
			return v
		}
	}
	return ""
}

func cindyInstallRoots(bin string) []string {
	var roots []string
	add := func(p string) {
		if p != "" && !slices.Contains(roots, p) {
			roots = append(roots, p)
		}
	}
	if real, err := filepath.EvalSymlinks(bin); err == nil {
		bin = real
	}
	dir := filepath.Dir(bin)
	add(dir)
	// ~/.local/opt/cindy/launch -> ~/.local/opt/cindy/current
	if cur, err := filepath.EvalSymlinks(filepath.Join(dir, "current")); err == nil {
		add(cur)
	}
	add(filepath.Join(dir, "current"))
	if home := userHome(); home != "" && !cindyExplicitHome() {
		if cur, err := filepath.EvalSymlinks(filepath.Join(home, ".local", "opt", "cindy", "current")); err == nil {
			add(cur)
		}
	}
	return roots
}

func plistVersion(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	s := string(b)
	_, rest, ok := strings.Cut(s, "CFBundleShortVersionString")
	if !ok {
		return ""
	}
	_, rest, ok = strings.Cut(rest, "<string>")
	if !ok {
		return ""
	}
	v, _, ok := strings.Cut(rest, "</string>")
	if !ok {
		return ""
	}
	return strings.TrimSpace(v)
}

func cindyMarkerVersion(dir string) string {
	matches, _ := filepath.Glob(filepath.Join(dir, "diagnostics", "run-markers", "run-*.json"))
	sort.Sort(sort.Reverse(sort.StringSlice(matches)))
	for _, m := range matches {
		b, err := os.ReadFile(m)
		if err != nil {
			continue
		}
		var rm struct {
			Version string `json:"version"`
		}
		if json.Unmarshal(b, &rm) == nil && strings.TrimSpace(rm.Version) != "" {
			return strings.TrimSpace(rm.Version)
		}
	}
	return ""
}

func cindyProcessAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	p, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	if runtime.GOOS == "windows" {
		return true
	}
	err = p.Signal(syscall.Signal(0))
	return err == nil || errors.Is(err, syscall.EPERM)
}

func cindyDesktopRunning(dir string) (bool, string) {
	// Check diagnostics/run-markers/run-*.json
	matches, _ := filepath.Glob(filepath.Join(dir, "diagnostics", "run-markers", "run-*.json"))
	for _, m := range matches {
		b, err := os.ReadFile(m)
		if err != nil {
			continue
		}
		var rm struct {
			PID     int    `json:"pid"`
			Version string `json:"version"`
		}
		if json.Unmarshal(b, &rm) == nil && cindyProcessAlive(rm.PID) {
			return true, strings.TrimSpace(rm.Version)
		}
	}
	// Check .dev-instances/*.json
	devs, _ := filepath.Glob(filepath.Join(dir, ".dev-instances", "*.json"))
	for _, d := range devs {
		b, err := os.ReadFile(d)
		if err != nil {
			continue
		}
		var rec struct {
			PID int `json:"pid"`
		}
		if json.Unmarshal(b, &rec) == nil && cindyProcessAlive(rec.PID) {
			return true, ""
		}
	}
	// Check SingletonLock symlink ("<host>-<pid>")
	if target, err := os.Readlink(filepath.Join(dir, "SingletonLock")); err == nil {
		if i := strings.LastIndexByte(target, '-'); i >= 0 {
			if pid, err := strconv.Atoi(target[i+1:]); err == nil && cindyProcessAlive(pid) {
				return true, ""
			}
		}
	}
	return false, ""
}

// cindyEndpoint describes a discovered Cindy host API or database in a profile directory.
type cindyEndpoint struct {
	DataDir string
	BaseURL string
	Token   string
	DBPath  string
	Version string
}

// discoverCindyEndpoint finds a running cindy-host loopback server, Local Agent discovery endpoint, or profile database.
func discoverCindyEndpoint(ctx context.Context, dirs []string) (*cindyEndpoint, bool) {
	if raw := strings.TrimSpace(os.Getenv("CINDY_HOST_API")); raw != "" {
		base := strings.TrimRight(raw, "/")
		var dir, token string
		if len(dirs) > 0 {
			dir = dirs[0]
			token = readCindyToken(dir)
		}
		if ok, ver := probeCindyHostHTTP(ctx, base, token); ok {
			return &cindyEndpoint{DataDir: dir, BaseURL: base, Token: token, Version: ver}, true
		}
	}
	for _, dir := range dirs {
		// 1. Check host-endpoint.json (cindy-host)
		if b, err := os.ReadFile(filepath.Join(dir, "host-endpoint.json")); err == nil {
			var ep struct {
				PID     int    `json:"pid"`
				BaseURL string `json:"baseUrl"`
			}
			if json.Unmarshal(b, &ep) == nil && ep.BaseURL != "" {
				base := strings.TrimRight(ep.BaseURL, "/")
				token := readCindyToken(dir)
				if ok, ver := probeCindyHostHTTP(ctx, base, token); ok {
					return &cindyEndpoint{DataDir: dir, BaseURL: base, Token: token, Version: ver}, true
				}
			}
		}
		// 2. Check local-agent/discovery.json (Cindy Desktop Local Agent API v1)
		if b, err := os.ReadFile(filepath.Join(dir, "local-agent", "discovery.json")); err == nil {
			var disc struct {
				Protocol int    `json:"protocol"`
				Port     int    `json:"port"`
				Instance string `json:"instance"`
			}
			if json.Unmarshal(b, &disc) == nil && disc.Port > 0 {
				if ok, ver := probeCindyLocalAgent(ctx, disc.Port, disc.Instance); ok {
					db := findCindyOwnerDB(dir)
					return &cindyEndpoint{DataDir: dir, DBPath: db, Version: ver}, true
				}
			}
		}
		// 3. Check running desktop process or owner database cindy-<owner>.db
		running, ver := cindyDesktopRunning(dir)
		db := findCindyOwnerDB(dir)
		if running || db != "" {
			return &cindyEndpoint{DataDir: dir, DBPath: db, Version: ver}, true
		}
	}
	return nil, false
}

func readCindyToken(dir string) string {
	b, err := os.ReadFile(filepath.Join(dir, "host-api-token"))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

func findCindyOwnerDB(dir string) string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}
	var best string
	var bestMod time.Time
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasPrefix(name, "cindy-") || !strings.HasSuffix(name, ".db") || strings.Contains(name, ".lock") {
			continue
		}
		p := filepath.Join(dir, name)
		st, err := e.Info()
		if err != nil {
			continue
		}
		if best == "" || st.ModTime().After(bestMod) {
			best, bestMod = p, st.ModTime()
		}
	}
	return best
}

func probeCindyHostHTTP(ctx context.Context, baseURL, token string) (bool, string) {
	cctx, cancel := context.WithTimeout(ctx, 1500*time.Millisecond)
	defer cancel()
	req, err := http.NewRequestWithContext(cctx, http.MethodGet, baseURL+"/health", nil)
	if err != nil {
		return false, ""
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return false, ""
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return false, ""
	}
	var health struct {
		Status  string `json:"status"`
		Service string `json:"service"`
		Version string `json:"version"`
	}
	if json.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(&health) != nil || health.Status != "ok" {
		return false, ""
	}
	return true, strings.TrimSpace(health.Version)
}

func probeCindyLocalAgent(ctx context.Context, port int, instance string) (bool, string) {
	cctx, cancel := context.WithTimeout(ctx, 1500*time.Millisecond)
	defer cancel()
	payload, _ := json.Marshal(map[string]any{
		"version":  1,
		"instance": instance,
		"id":       "probe0001:1",
		"method":   "capabilities",
		"params":   map[string]any{},
	})
	url := fmt.Sprintf("http://127.0.0.1:%d/rpc", port)
	req, err := http.NewRequestWithContext(cctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return false, ""
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return false, ""
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return false, ""
	}
	var out struct {
		Result struct {
			Protocol int    `json:"protocol"`
			Version  string `json:"version"`
		} `json:"result"`
	}
	if json.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(&out) != nil || out.Result.Protocol == 0 {
		return false, ""
	}
	return true, strings.TrimSpace(out.Result.Version)
}

// cindyEngine is the Cindy engine as found on this machine for node.info: available when its runtime bridge is
// connected, its host / desktop endpoint or database is present, or the `cindy` application is installed.
func (n *Node) cindyEngine(ctx context.Context) proto.Engine {
	e := proto.Engine{ID: proto.EngineCindy, Name: proto.EngineNames[proto.EngineCindy]}
	var h *runtimeConn
	if n.runtimes != nil {
		n.runtimes.mu.Lock()
		h = n.runtimes.byRuntime(cindyRuntimeID)
		n.runtimes.mu.Unlock()
	}
	dirs := cindyDataDirs()
	bin := cindyBin()
	if h != nil {
		h.mu.Lock()
		e.Version = h.hello.Version
		h.mu.Unlock()
		if e.Version == "" {
			e.Version = cindyInstalledVersion(bin, dirs)
		}
		e.Available = true
		return e
	}
	if ep, ok := discoverCindyEndpoint(ctx, dirs); ok {
		e.Available = true
		e.Version = ep.Version
	}
	if bin != "" {
		e.Available = true
	}
	if e.Version == "" && (e.Available || bin != "") {
		e.Version = cindyInstalledVersion(bin, dirs)
	}
	if n.runtimeHost != nil && e.Available {
		if s := n.runtimeHost.cindyBridgeStatus(); s != "" {
			e.Note = "Cindy 在运行，但 Persona 连不上它：" + s
		}
	}
	return e
}

// cindyBridgeState holds the in-process Cindy bridge lifecycle on RuntimeHost.
type cindyBridgeState struct {
	mu     sync.Mutex
	cancel context.CancelFunc
	done   chan struct{}
	status string
}

// setCindyBridge starts or stops the Cindy runtime bridge on rh.
func (rh *RuntimeHost) setCindyBridge(on bool) {
	b := &rh.cindy
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
		rh.logger.Printf("cindy bridge: stopped (the engine is not Cindy any more)")
		return
	}
	base := rh.ctx
	if base == nil {
		base = context.Background()
	}
	ctx, cancel := context.WithCancel(base)
	b.cancel, b.done, b.status = cancel, make(chan struct{}), ""
	go rh.cindyBridgeLoop(ctx, b.done)
}

func (rh *RuntimeHost) cindyBridgeStatus() string {
	rh.cindy.mu.Lock()
	defer rh.cindy.mu.Unlock()
	if rh.cindy.cancel == nil {
		return ""
	}
	return rh.cindy.status
}

func (rh *RuntimeHost) setCindyStatus(s string) {
	rh.cindy.mu.Lock()
	rh.cindy.status = s
	rh.cindy.mu.Unlock()
}

func (rh *RuntimeHost) cindyBridgeLoop(ctx context.Context, done chan struct{}) {
	defer close(done)
	for ctx.Err() == nil {
		ep, ok := discoverCindyEndpoint(ctx, cindyDataDirs())
		if ok && (ep.BaseURL != "" || ep.DBPath != "") {
			err := rh.runCindyBridge(ctx, ep)
			if ctx.Err() != nil {
				return
			}
			if err != nil {
				rh.setCindyStatus(err.Error())
				rh.logger.Printf("cindy bridge: %v (again in %s)", err, cindyBridgeRestart)
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(cindyBridgeRestart):
		}
	}
}

func (rh *RuntimeHost) runCindyBridge(ctx context.Context, ep *cindyEndpoint) error {
	// If an external runtime connection already owns "cindy", yield until it leaves.
	if rh.reg != nil {
		rh.reg.mu.Lock()
		existing := rh.reg.byRuntime(cindyRuntimeID)
		rh.reg.mu.Unlock()
		if existing != nil {
			return nil
		}
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	hostEnd, bridgeEnd := net.Pipe()
	go func() {
		rh.serveConn(ctx, hostEnd, "")
		cancel()
	}()
	ver := ep.Version
	if ver == "" {
		ver = cindyInstalledVersion(cindyBin(), cindyDataDirs())
	}
	if ver == "" {
		ver = rh.version
	}
	b := newCindyBridge(ep, bridgeEnd, rh.logger.Printf)
	rh.setCindyStatus("")
	return b.run(ctx, ver)
}

// cindyBridge bridges a Cindy host API or SQLite profile database to the node's runtime protocol.
type cindyBridge struct {
	ep   *cindyEndpoint
	conn io.ReadWriteCloser
	peer *rpc.Peer
	logf func(string, ...any)
	http *http.Client

	mu       sync.Mutex
	cindyOf  map[string]string     // Persona session_id -> Cindy session id
	rowByID  map[string]cindyDBRow // Cindy session id -> latest row
	asks     map[string]*cindyAsk  // prompt key -> active phone interaction
	pushed   string                // last marshalled sessions list
	launches string                // last marshalled launchers list
	watched  map[string]bool       // Persona session_ids being watched
}

type cindyAsk struct {
	cindyID string
	reqID   string
	cancel  context.CancelFunc
	retryAt time.Time
}

type cindyDBRow struct {
	ID              string          `json:"id"`
	Title           string          `json:"title"`
	WorkingDir      string          `json:"workingDir"`
	ProjectDir      string          `json:"projectDir"`
	WorkspaceKind   string          `json:"workspaceKind"`
	WorktreePath    string          `json:"worktreePath"`
	Model           string          `json:"model"`
	Effort          string          `json:"effort"`
	PermissionMode  string          `json:"permissionMode"`
	PlanModeEnabled bool            `json:"planModeEnabled"`
	Status          string          `json:"status"`
	SDKSessionID    string          `json:"sdkSessionId"`
	AgentKind       string          `json:"agentKind"`
	ProviderID      string          `json:"providerId"`
	LastActivityAt  int64           `json:"lastActivityAt"`
	UserSendAt      int64           `json:"userSendAt"`
	UpdatedAt       json.RawMessage `json:"updatedAt"`
	ListPreview     string          `json:"listPreview"`
	ListPreviewRole string          `json:"listPreviewRole"`
}

func newCindyBridge(ep *cindyEndpoint, conn io.ReadWriteCloser, logf func(string, ...any)) *cindyBridge {
	return &cindyBridge{
		ep:      ep,
		conn:    conn,
		logf:    logf,
		http:    &http.Client{Timeout: 10 * time.Second},
		cindyOf: map[string]string{},
		rowByID: map[string]cindyDBRow{},
		asks:    map[string]*cindyAsk{},
		watched: map[string]bool{},
	}
}

func (b *cindyBridge) writable() bool {
	return b.ep != nil && b.ep.BaseURL != "" && b.ep.Token != ""
}

func (b *cindyBridge) run(ctx context.Context, version string) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	defer b.conn.Close()
	b.peer = rpc.New("c", b.conn)
	b.peer.OnRequest = b.request
	served := make(chan error, 1)
	go func() { served <- b.peer.Serve(ctx, b.conn) }()

	w := b.writable()
	launchers := b.fetchLaunchers(ctx)
	hello := map[string]any{
		"runtime":   cindyRuntimeID,
		"name":      proto.EngineNames[proto.EngineCindy],
		"agent":     "claude",
		"version":   version,
		"instance":  newUUID(),
		"caps":      map[string]any{"send": w, "interrupt": w, "start": w, "resume": w, "close": w, "modes": w},
		"launchers": launchers,
	}
	if err := b.peer.Call(ctx, "runtime.hello", hello, nil); err != nil {
		return fmt.Errorf("hello: %w", err)
	}
	if raw, err := json.Marshal(launchers); err == nil {
		b.launches = string(raw)
	}

	wake := make(chan struct{}, 1)
	if w {
		go b.watchSSE(ctx, wake)
	}
	t := time.NewTicker(cindyBridgePoll)
	defer t.Stop()
	for {
		if err := b.refresh(ctx); err != nil {
			return err
		}
		select {
		case err := <-served:
			if err == nil {
				err = errors.New("the runtime host closed the connection")
			}
			return err
		case <-ctx.Done():
			return ctx.Err()
		case <-wake:
		case <-t.C:
		}
	}
}

func (b *cindyBridge) watchSSE(ctx context.Context, wake chan<- struct{}) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, b.ep.BaseURL+"/events", nil)
	if err != nil {
		return
	}
	req.Header.Set("Authorization", "Bearer "+b.ep.Token)
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return
	}
	sc := bufio.NewScanner(resp.Body)
	sc.Buffer(make([]byte, 64<<10), 2<<20)
	for sc.Scan() {
		line := sc.Text()
		data, ok := strings.CutPrefix(line, "data:")
		if !ok {
			continue
		}
		data = strings.TrimSpace(data)
		var ev struct {
			Type      string `json:"type"`
			SessionID string `json:"sessionId"`
			Event     struct {
				Type string `json:"type"`
				Data struct {
					Text    string `json:"text"`
					IsFinal bool   `json:"isFinal"`
				} `json:"data"`
			} `json:"event"`
		}
		if json.Unmarshal([]byte(data), &ev) != nil {
			continue
		}
		if ev.Type == "agent_event" && ev.Event.Type == "text" && !ev.Event.Data.IsFinal && ev.Event.Data.Text != "" {
			if sid := b.personaIDOf(ev.SessionID); sid != "" && b.isWatched(sid) {
				_ = b.peer.Notify("session.delta", map[string]any{"session_id": sid, "text": ev.Event.Data.Text})
			}
			continue
		}
		select {
		case wake <- struct{}{}:
		default:
		}
	}
}

func (b *cindyBridge) personaIDOf(cindyID string) string {
	b.mu.Lock()
	defer b.mu.Unlock()
	for pid, cid := range b.cindyOf {
		if cid == cindyID {
			return pid
		}
	}
	return ""
}

func (b *cindyBridge) isWatched(sid string) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.watched[sid]
}

func (b *cindyBridge) postJSON(ctx context.Context, path string, in any, out any) error {
	body, err := json.Marshal(in)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, b.ep.BaseURL+path, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if b.ep.Token != "" {
		req.Header.Set("Authorization", "Bearer "+b.ep.Token)
	}
	resp, err := b.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode >= 400 {
		var errBody struct {
			Error   string `json:"error"`
			Message string `json:"message"`
		}
		_ = json.Unmarshal(raw, &errBody)
		msg := firstOf(errBody.Message, errBody.Error, strings.TrimSpace(string(raw)))
		return fmt.Errorf("cindy %s (%d): %s", path, resp.StatusCode, msg)
	}
	if out != nil && len(raw) > 0 {
		return json.Unmarshal(raw, out)
	}
	return nil
}

func (b *cindyBridge) fetchLaunchers(ctx context.Context) []map[string]any {
	if !b.writable() {
		return nil
	}
	var catalog struct {
		Providers []struct {
			ID        string   `json:"id"`
			Name      string   `json:"name"`
			Connected bool     `json:"connected"`
			Agents    []string `json:"agents"`
			Models    map[string][]struct {
				ID            string   `json:"id"`
				Name          string   `json:"name"`
				Efforts       []string `json:"efforts"`
				DefaultEffort string   `json:"defaultEffort"`
			} `json:"models"`
		} `json:"providers"`
	}
	if err := b.postJSON(ctx, "/api/providers", map[string]any{}, &catalog); err != nil {
		return nil
	}
	var out []map[string]any
	for _, p := range catalog.Providers {
		if !p.Connected {
			continue
		}
		for _, ag := range []struct{ cindyKey, personaAgent, suffix string }{
			{"claude-code", "claude", "claude"},
			{"codex", "codex", "codex"},
			{"pi", "pi", "pi"},
		} {
			ms := p.Models[ag.cindyKey]
			if len(ms) == 0 && !slices.Contains(p.Agents, ag.cindyKey) {
				continue
			}
			name := "cindy-" + ag.suffix
			if p.ID != "" && p.ID != "xd" && nameRe.MatchString("cindy-"+p.ID+"-"+ag.suffix) {
				name = "cindy-" + p.ID + "-" + ag.suffix
			}
			var models []map[string]any
			defModel := ""
			for i, m := range ms {
				if m.ID == "" {
					continue
				}
				if i == 0 {
					defModel = firstOf(m.Name, m.ID)
				}
				entry := map[string]any{"id": m.ID, "name": firstOf(m.Name, m.ID), "default": i == 0}
				if len(m.Efforts) > 0 {
					entry["efforts"] = m.Efforts
				}
				if m.DefaultEffort != "" {
					entry["default_effort"] = m.DefaultEffort
				}
				models = append(models, entry)
			}
			l := map[string]any{"name": name, "agent": ag.personaAgent}
			if defModel != "" {
				l["model"] = defModel
			}
			if len(models) > 0 {
				l["models"] = models
			}
			out = append(out, l)
		}
	}
	return out
}

func (b *cindyBridge) refresh(ctx context.Context) error {
	rows, running, aliases, err := b.loadSessions(ctx)
	if err != nil {
		return err
	}
	if b.writable() {
		if ls := b.fetchLaunchers(ctx); ls != nil {
			if raw, err := json.Marshal(ls); err == nil && string(raw) != b.launches {
				if b.peer.Notify("runtime.launchers", map[string]any{"launchers": ls}) == nil {
					b.launches = string(raw)
				}
			}
		}
	}
	cindyOf := map[string]string{}
	rowByID := map[string]cindyDBRow{}
	waiting := map[string]string{}
	out := make([]map[string]any, 0, len(rows))
	for _, r := range rows {
		if r.ID == "" || r.Status == "archived" || r.Status == "deleted" {
			continue
		}
		var pending []cindyPendingInteraction
		if b.writable() {
			_ = b.postJSON(ctx, "/api/interactions/pending", map[string]any{"sessionId": r.ID}, &pending)
		}
		mapped, sid := mapCindySessionRow(r, running[r.ID], len(pending) > 0, aliases)
		if sid == "" {
			continue
		}
		out = append(out, mapped)
		cindyOf[sid] = r.ID
		rowByID[r.ID] = r
		if len(pending) > 0 {
			waiting[r.ID] = sid
			b.syncPrompts(ctx, r.ID, sid, pending)
		}
	}
	b.mu.Lock()
	b.cindyOf = cindyOf
	b.rowByID = rowByID
	b.mu.Unlock()
	if raw, err := json.Marshal(out); err == nil && string(raw) != b.pushed {
		if b.peer.Notify("runtime.sessions", map[string]any{"sessions": out}) == nil {
			b.pushed = string(raw)
		}
	}
	b.prunePrompts(ctx, waiting)
	return nil
}

func (b *cindyBridge) loadSessions(ctx context.Context) ([]cindyDBRow, map[string]bool, map[string]string, error) {
	running := map[string]bool{}
	aliases := map[string]string{}
	if b.writable() {
		var rows []cindyDBRow
		if err := b.postJSON(ctx, "/api/sessions/list", map[string]any{"status": "active", "limit": 300}, &rows); err != nil {
			return nil, nil, nil, err
		}
		var rt []struct {
			SessionID     string `json:"sessionId"`
			IsTurnRunning bool   `json:"isTurnRunning"`
		}
		if b.postJSON(ctx, "/api/runtime/sessions", map[string]any{}, &rt) == nil {
			for _, s := range rt {
				if s.IsTurnRunning {
					running[s.SessionID] = true
				}
			}
		}
		var pa []struct {
			ProjectKey string `json:"projectKey"`
			Alias      string `json:"alias"`
		}
		if b.postJSON(ctx, "/api/project-aliases/list", map[string]any{}, &pa) == nil {
			for _, a := range pa {
				if a.ProjectKey != "" && a.Alias != "" {
					aliases[a.ProjectKey] = a.Alias
				}
			}
		}
		return rows, running, aliases, nil
	}
	if b.ep != nil && b.ep.DBPath != "" {
		rows, err := readCindySQLiteSessions(ctx, b.ep.DBPath)
		return rows, running, aliases, err
	}
	return nil, nil, nil, errors.New("no Cindy endpoint or database")
}

// cindyModes maps Cindy's permission_mode values to Persona's (Claude Code's).
var cindyToPersonaMode = map[string]string{
	"ask":               "default",
	"default":           "default",
	"acceptEdits":       "acceptEdits",
	"plan":              "plan",
	"auto":              "auto",
	"bypassPermissions": proto.ModeBypass,
}

var personaToCindyMode = map[string]string{
	"default":        "ask",
	"acceptEdits":    "acceptEdits",
	"plan":           "plan",
	"auto":           "auto",
	proto.ModeBypass: "bypassPermissions",
}

func cindyAgentName(kind string) string {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "codex":
		return "codex"
	case "pi":
		return "pi"
	default:
		return "claude"
	}
}

func cindyPersonaSessionID(r cindyDBRow) (sid, transcript string) {
	agent := cindyAgentName(r.AgentKind)
	sdk := strings.TrimSpace(r.SDKSessionID)
	if agent == "claude" && validID(sdk) {
		return sdk, sdk
	}
	id := strings.TrimSpace(r.ID)
	if id == "" {
		return "", ""
	}
	if strings.HasPrefix(id, "cindy-") && runtimeIDRe.MatchString(id) {
		return id, ""
	}
	cand := "cindy-" + id
	if runtimeIDRe.MatchString(cand) {
		return cand, ""
	}
	return "", ""
}

func parseCindyTime(raw json.RawMessage, fallbackMs int64) (time.Time, int64) {
	if len(raw) > 0 {
		var s string
		if json.Unmarshal(raw, &s) == nil && s != "" {
			if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
				return t.UTC(), t.UnixMilli()
			}
		}
		var ms int64
		if json.Unmarshal(raw, &ms) == nil && ms > 0 {
			return time.UnixMilli(ms).UTC(), ms
		}
	}
	if fallbackMs > 0 {
		return time.UnixMilli(fallbackMs).UTC(), fallbackMs
	}
	now := time.Now().UTC()
	return now, now.UnixMilli()
}

func mapCindySessionRow(r cindyDBRow, isRunning, isBlocked bool, aliases map[string]string) (map[string]any, string) {
	sid, transcript := cindyPersonaSessionID(r)
	if sid == "" {
		return nil, ""
	}
	agent := cindyAgentName(r.AgentKind)
	cwd := firstOf(strings.TrimSpace(r.WorktreePath), strings.TrimSpace(r.WorkingDir), strings.TrimSpace(r.ProjectDir))
	spaceDir := firstOf(strings.TrimSpace(r.ProjectDir), strings.TrimSpace(r.WorkingDir), strings.TrimSpace(r.WorktreePath))
	title := strings.TrimSpace(r.Title)
	if title == "" || title == "New Maker" {
		if cwd != "" {
			title = filepath.Base(cwd)
		} else {
			title = "Cindy"
		}
	}
	status := proto.StatusIdle
	switch {
	case isBlocked:
		status = proto.StatusBlocked
	case isRunning:
		status = proto.StatusWorking
	case r.LastActivityAt > 0 || r.UserSendAt > 0 || strings.TrimSpace(r.ListPreview) != "":
		status = proto.StatusDone
	}
	updated, rev := parseCindyTime(r.UpdatedAt, max(r.LastActivityAt, r.UserSendAt))
	row := map[string]any{
		"session_id": sid,
		"title":      title,
		"cwd":        cwd,
		"status":     status,
		"updated_at": updated,
		"rev":        rev,
		"agent":      agent,
	}
	if transcript != "" {
		row["transcript"] = transcript
	}
	if r.WorkspaceKind != "dialogue" && spaceDir != "" {
		row["space_id"] = spaceDir
		row["space_name"] = firstOf(aliases[spaceDir], filepath.Base(spaceDir))
	}
	if wt, proj := strings.TrimSpace(r.WorktreePath), strings.TrimSpace(r.ProjectDir); wt != "" && proj != "" && wt != proj {
		row["repo_root"] = proj
	}
	if m := strings.TrimSpace(r.Model); m != "" {
		row["model"] = m
	}
	if e := strings.TrimSpace(r.Effort); e != "" && validModes("", e) == nil {
		row["effort"] = e
	}
	mode := cindyToPersonaMode[strings.TrimSpace(r.PermissionMode)]
	if r.PlanModeEnabled {
		mode = "plan"
	}
	if mode != "" {
		row["permission_mode"] = mode
	}
	if prev := strings.TrimSpace(r.ListPreview); prev != "" {
		row["last_text"] = snippet(prev)
	}
	return row, sid
}

type cindyPendingInteraction struct {
	RequestID string          `json:"requestId"`
	Request   json.RawMessage `json:"request"`
}

func (b *cindyBridge) syncPrompts(ctx context.Context, cindyID, personaID string, pending []cindyPendingInteraction) {
	for _, p := range pending {
		if p.RequestID == "" {
			continue
		}
		key := cindyID + "/" + p.RequestID
		b.mu.Lock()
		a := b.asks[key]
		due := a == nil || (!a.retryAt.IsZero() && time.Now().After(a.retryAt))
		if due {
			b.asks[key] = &cindyAsk{cindyID: cindyID, reqID: p.RequestID}
		}
		b.mu.Unlock()
		if due {
			b.askInteraction(ctx, key, cindyID, personaID, p)
		}
	}
}

func (b *cindyBridge) prunePrompts(ctx context.Context, waiting map[string]string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for key, a := range b.asks {
		if _, ok := waiting[a.cindyID]; ok {
			continue
		}
		if a.cancel != nil {
			a.cancel()
		}
		delete(b.asks, key)
	}
}

func (b *cindyBridge) askInteraction(ctx context.Context, key, cindyID, personaID string, p cindyPendingInteraction) {
	ix := mapCindyInteraction(personaID, p)
	if ix == nil {
		return
	}
	sum := sha256.Sum256([]byte(key))
	ix.ID = "cindy_" + hex.EncodeToString(sum[:8])
	askCtx, cancel := context.WithCancel(ctx)
	b.mu.Lock()
	if a := b.asks[key]; a != nil {
		a.cancel, a.retryAt = cancel, time.Time{}
	}
	b.mu.Unlock()
	go func() {
		defer cancel()
		var res proto.Resolution
		err := b.peer.Call(askCtx, "interaction.create", map[string]any{"interaction": ix}, &res)
		if askCtx.Err() != nil {
			if ctx.Err() == nil {
				_ = b.peer.Notify("interaction.cancel", map[string]any{"id": ix.ID})
			}
			return
		}
		if err != nil {
			b.mu.Lock()
			if a := b.asks[key]; a != nil {
				a.retryAt = time.Now().Add(cindyAskRetry)
			}
			b.mu.Unlock()
			return
		}
		if res.Dismiss {
			return
		}
		decision := mapCindyDecision(ix.Kind, res)
		_ = b.postJSON(ctx, "/api/interactions/respond", map[string]any{
			"sessionId": cindyID,
			"requestId": p.RequestID,
			"decision":  decision,
		}, nil)
	}()
}

func mapCindyInteraction(personaID string, p cindyPendingInteraction) *proto.Interaction {
	var raw struct {
		Kind        string          `json:"kind"`
		ToolName    string          `json:"toolName"`
		ToolInput   json.RawMessage `json:"toolInput"`
		Description string          `json:"description"`
		Reason      string          `json:"reason"`
		Plan        string          `json:"plan"`
		Questions   []struct {
			Question    string `json:"question"`
			Header      string `json:"header"`
			MultiSelect bool   `json:"multiSelect"`
			Options     []struct {
				Label       string `json:"label"`
				Description string `json:"description"`
			} `json:"options"`
		} `json:"questions"`
	}
	if json.Unmarshal(p.Request, &raw) != nil {
		return nil
	}
	ix := &proto.Interaction{SessionID: personaID}
	switch raw.Kind {
	case "plan":
		ix.Kind = proto.IxPlan
		ix.Plan = firstOf(raw.Plan, raw.Description)
		return ix
	case "question":
		for _, q := range raw.Questions {
			pq := proto.Question{Question: q.Question, Header: q.Header, MultiSelect: q.MultiSelect}
			for _, o := range q.Options {
				pq.Options = append(pq.Options, proto.QuestionOption{Label: o.Label, Description: o.Description})
			}
			ix.Questions = append(ix.Questions, pq)
		}
		if len(ix.Questions) == 0 {
			return nil
		}
		ix.Kind = proto.IxQuestion
		return ix
	default:
		ix.Kind = proto.IxPermission
		ix.ToolName = firstOf(raw.ToolName, "Tool")
		if len(raw.ToolInput) > 0 && string(raw.ToolInput) != "null" {
			ix.ToolInput = raw.ToolInput
			ix.Summary = toolSummary(ix.ToolName, raw.ToolInput)
		}
		if ix.Summary == "" {
			ix.Summary = firstOf(raw.Description, ix.ToolName)
		}
		ix.Reason = raw.Reason
		return ix
	}
}

func mapCindyDecision(kind string, res proto.Resolution) map[string]any {
	switch kind {
	case proto.IxQuestion:
		return map[string]any{"kind": "question", "answers": res.Answers}
	case proto.IxPlan:
		allow := res.Approve != nil && *res.Approve
		if !allow {
			return map[string]any{"kind": "permission", "behavior": "deny", "reason": firstOf(res.Feedback, res.Message, "denied")}
		}
		return map[string]any{"kind": "permission", "behavior": "allow"}
	default:
		allow := res.Allow != nil && *res.Allow
		if !allow {
			return map[string]any{"kind": "permission", "behavior": "deny", "reason": firstOf(res.Message, res.Feedback, "denied")}
		}
		return map[string]any{"kind": "permission", "behavior": "allow", "always": res.Always}
	}
}

func (b *cindyBridge) request(ctx context.Context, method string, params json.RawMessage) (any, error) {
	var p struct {
		SessionID      string        `json:"session_id"`
		Limit          int           `json:"limit"`
		BeforeSeq      *int          `json:"before_seq"`
		Text           string        `json:"text"`
		Images         []proto.Image `json:"images"`
		ClientMsgID    string        `json:"client_msg_id"`
		PermissionMode string        `json:"permission_mode"`
		Effort         string        `json:"effort"`
		Cwd            string        `json:"cwd"`
		Prompt         string        `json:"prompt"`
		Launcher       string        `json:"launcher"`
		Model          string        `json:"model"`
		WorkspaceID    string        `json:"workspace_id"`
		Resume         string        `json:"resume"`
		OpenElsewhere  bool          `json:"open_elsewhere"`
	}
	_ = json.Unmarshal(params, &p)
	ok := map[string]any{"ok": true}

	switch method {
	case "session.watch":
		b.mu.Lock()
		b.watched[p.SessionID] = true
		b.mu.Unlock()
		return ok, nil
	case "session.unwatch":
		b.mu.Lock()
		delete(b.watched, p.SessionID)
		b.mu.Unlock()
		return ok, nil
	case "session.start":
		return b.startSession(ctx, p.Cwd, p.Prompt, p.Launcher, p.Model, p.PermissionMode, p.Effort, p.WorkspaceID, p.Resume, p.OpenElsewhere)
	}

	b.mu.Lock()
	cindyID := b.cindyOf[p.SessionID]
	b.mu.Unlock()
	if cindyID == "" {
		return nil, &rpc.Error{Code: "session_gone", Message: "这个会话已经不在 Cindy 里了"}
	}

	if method == "session.items" {
		return b.sessionItems(ctx, cindyID, p.Limit, p.BeforeSeq)
	}
	if !b.writable() {
		return nil, &rpc.Error{Code: "runtime_away", Message: "Cindy 服务没有在运行"}
	}

	switch method {
	case "session.send":
		cid := p.ClientMsgID
		if cid == "" {
			cid = newUUID()
		}
		var steerRes struct {
			OK bool `json:"ok"`
		}
		if b.postJSON(ctx, "/api/input/steer", map[string]any{"sessionId": cindyID, "item": map[string]any{"text": p.Text}}, &steerRes) == nil && steerRes.OK {
			return ok, nil
		}
		err := b.postJSON(ctx, "/api/input/enqueue", map[string]any{
			"sessionId": cindyID,
			"item": map[string]any{
				"clientId":         cid,
				"text":             p.Text,
				"persistedContent": p.Text,
			},
		}, nil)
		if err != nil {
			return nil, err
		}
		return ok, nil
	case "session.interrupt":
		if err := b.postJSON(ctx, "/api/input/stop", map[string]any{"sessionId": cindyID}, nil); err != nil {
			return nil, err
		}
		return ok, nil
	case "session.close":
		if err := b.postJSON(ctx, "/api/sessions/update", map[string]any{
			"sessionId": cindyID,
			"patch":     map[string]any{"status": "archived"},
		}, nil); err != nil {
			return nil, err
		}
		return ok, nil
	case "session.modes":
		patch := map[string]any{}
		if p.PermissionMode != "" {
			cm := personaToCindyMode[p.PermissionMode]
			if cm == "" {
				return nil, &rpc.Error{Code: "unsupported", Message: "unsupported: Cindy 没有这个权限模式（" + p.PermissionMode + "）"}
			}
			patch["permissionMode"] = cm
			patch["planModeEnabled"] = p.PermissionMode == "plan"
		}
		if p.Effort != "" {
			patch["effort"] = p.Effort
		}
		if len(patch) > 0 {
			if err := b.postJSON(ctx, "/api/sessions/update", map[string]any{"sessionId": cindyID, "patch": patch}, nil); err != nil {
				return nil, err
			}
		}
		return ok, nil
	default:
		return nil, &rpc.Error{Code: "unsupported", Message: "unsupported: " + method}
	}
}

func (b *cindyBridge) startSession(ctx context.Context, cwd, prompt, launcher, model, mode, effort, workspaceID, resume string, openElsewhere bool) (any, error) {
	if !b.writable() {
		return nil, &rpc.Error{Code: "runtime_away", Message: "Cindy 服务没有在运行"}
	}
	if resume != "" {
		b.mu.Lock()
		cindyID := b.cindyOf[resume]
		row := b.rowByID[cindyID]
		b.mu.Unlock()
		if openElsewhere && cindyID == "" {
			return nil, &rpc.Error{Code: openElsewhereCode, Message: "这个会话正在别处运行"}
		}
		if cindyID == "" {
			cindyID = resume
			createReq := map[string]any{
				"id":         cindyID,
				"workingDir": firstOf(cwd, workspaceID),
				"agentKind":  "claude-code",
			}
			if model != "" {
				createReq["model"] = model
			}
			if effort != "" {
				createReq["effort"] = effort
			}
			if cm := personaToCindyMode[mode]; cm != "" {
				createReq["permissionMode"] = cm
				createReq["planModeEnabled"] = mode == "plan"
			}
			_ = b.postJSON(ctx, "/api/sessions/create", createReq, &row)
		} else {
			_ = b.postJSON(ctx, "/api/sessions/update", map[string]any{
				"sessionId": cindyID,
				"patch":     map[string]any{"status": "active"},
			}, &row)
		}
		if strings.TrimSpace(prompt) != "" {
			_ = b.postJSON(ctx, "/api/input/enqueue", map[string]any{
				"sessionId": cindyID,
				"item": map[string]any{
					"clientId":         newUUID(),
					"text":             prompt,
					"persistedContent": prompt,
				},
			}, nil)
		}
		b.mu.Lock()
		b.cindyOf[resume] = cindyID
		b.mu.Unlock()
		return map[string]any{
			"session": map[string]any{
				"session_id": resume,
				"title":      firstOf(row.Title, filepath.Base(cwd), "Cindy"),
				"cwd":        firstOf(row.WorkingDir, cwd),
				"status":     proto.StatusIdle,
				"agent":      "claude",
				"transcript": resume,
			},
		}, nil
	}

	agentKind := "claude-code"
	switch {
	case strings.HasSuffix(launcher, "-codex") || launcher == "codex":
		agentKind = "codex"
	case strings.HasSuffix(launcher, "-pi") || launcher == "pi":
		agentKind = "pi"
	}
	dir := firstOf(cwd, workspaceID)
	createReq := map[string]any{
		"workingDir": dir,
		"agentKind":  agentKind,
	}
	if model != "" {
		createReq["model"] = model
	}
	if effort != "" {
		createReq["effort"] = effort
	}
	if cm := personaToCindyMode[mode]; cm != "" {
		createReq["permissionMode"] = cm
		createReq["planModeEnabled"] = mode == "plan"
	}
	var created cindyDBRow
	if err := b.postJSON(ctx, "/api/sessions/create", createReq, &created); err != nil {
		return nil, err
	}
	if created.ID == "" {
		return nil, errors.New("Cindy did not return a session id")
	}
	if strings.TrimSpace(prompt) != "" {
		if err := b.postJSON(ctx, "/api/input/enqueue", map[string]any{
			"sessionId": created.ID,
			"item": map[string]any{
				"clientId":         newUUID(),
				"text":             prompt,
				"persistedContent": prompt,
			},
		}, nil); err != nil {
			return nil, err
		}
	}
	mapped, sid := mapCindySessionRow(created, strings.TrimSpace(prompt) != "", false, nil)
	if sid == "" {
		sid = "cindy-" + created.ID
		mapped = map[string]any{
			"session_id": sid,
			"title":      firstOf(created.Title, filepath.Base(dir), "Cindy"),
			"cwd":        dir,
			"status":     proto.StatusIdle,
			"agent":      cindyAgentName(agentKind),
		}
	}
	b.mu.Lock()
	b.cindyOf[sid] = created.ID
	b.rowByID[created.ID] = created
	b.mu.Unlock()
	return map[string]any{"session": mapped}, nil
}

const openElsewhereCode = "open_elsewhere"

type cindyMessageRow struct {
	ID        string          `json:"id"`
	Role      string          `json:"role"`
	Content   string          `json:"content"`
	ToolUseID string          `json:"toolUseId"`
	CreatedAt json.RawMessage `json:"createdAt"`
}

func (b *cindyBridge) sessionItems(ctx context.Context, cindyID string, limit int, beforeSeq *int) (any, error) {
	if limit <= 0 || limit > 200 {
		limit = 200
	}
	var msgs []cindyMessageRow
	if b.writable() {
		var wrapped struct {
			Messages []cindyMessageRow `json:"messages"`
		}
		if err := b.postJSON(ctx, "/api/sessions/messages", map[string]any{"sessionId": cindyID, "limit": 500}, &wrapped); err == nil {
			msgs = wrapped.Messages
		} else {
			_ = b.postJSON(ctx, "/api/messages/list", map[string]any{"sessionId": cindyID, "limit": 500}, &msgs)
		}
	} else if b.ep != nil && b.ep.DBPath != "" {
		msgs, _ = readCindySQLiteMessages(ctx, b.ep.DBPath, cindyID)
	}
	items := make([]proto.Item, 0, len(msgs))
	for i, m := range msgs {
		ts, _ := parseCindyTime(m.CreatedAt, 0)
		seq := i + 1
		it := proto.Item{ID: m.ID, Seq: seq, TS: ts}
		text := unwrapCindyMessageContent(m.Content)
		switch m.Role {
		case "user":
			it.Role, it.Kind, it.Text = "user", proto.KindText, text
		case "assistant":
			it.Role, it.Kind, it.Text = "assistant", proto.KindText, text
		case "tool_use":
			it.Role, it.Kind, it.ToolName, it.ToolUseID, it.Text = "assistant", proto.KindToolUse, "Tool", m.ToolUseID, text
		case "tool_result":
			it.Role, it.Kind, it.ToolUseID, it.Text = "user", proto.KindToolResult, m.ToolUseID, text
		case "error":
			it.Role, it.Kind, it.Text, it.IsError = "assistant", proto.KindText, text, true
		default:
			continue
		}
		items = append(items, it)
	}
	toSeq := len(items)
	if beforeSeq != nil {
		var filtered []proto.Item
		for _, it := range items {
			if it.Seq < *beforeSeq {
				filtered = append(filtered, it)
			}
		}
		items = filtered
	}
	hasMore := false
	if len(items) > limit {
		items = items[len(items)-limit:]
		hasMore = true
	}
	return map[string]any{
		"items":    items,
		"to_seq":   toSeq,
		"has_more": hasMore,
		"history":  "cindy:" + cindyID,
	}, nil
}

func unwrapCindyMessageContent(raw string) string {
	s := strings.TrimSpace(raw)
	if !strings.HasPrefix(s, "{") && !strings.HasPrefix(s, "[") {
		return raw
	}
	var obj struct {
		Text    string `json:"text"`
		Message string `json:"message"`
		Content string `json:"content"`
	}
	if json.Unmarshal([]byte(s), &obj) == nil {
		if v := firstOf(obj.Text, obj.Message, obj.Content); v != "" {
			return v
		}
	}
	var parts []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if json.Unmarshal([]byte(s), &parts) == nil && len(parts) > 0 {
		var texts []string
		for _, p := range parts {
			if p.Text != "" {
				texts = append(texts, p.Text)
			}
		}
		if len(texts) > 0 {
			return strings.Join(texts, "\n")
		}
	}
	return raw
}

func readCindySQLiteSessions(ctx context.Context, dbPath string) ([]cindyDBRow, error) {
	sqlite, err := exec.LookPath("sqlite3")
	if err != nil {
		return nil, err
	}
	cctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	q := `SELECT id, title, COALESCE(working_dir, '') AS workingDir, COALESCE(project_dir, '') AS projectDir, ` +
		`COALESCE(workspace_kind, 'project') AS workspaceKind, COALESCE(worktree_path, '') AS worktreePath, ` +
		`COALESCE(model, '') AS model, COALESCE(effort, '') AS effort, COALESCE(permission_mode, '') AS permissionMode, ` +
		`COALESCE(status, 'active') AS status, COALESCE(sdk_session_id, '') AS sdkSessionId, ` +
		`COALESCE(agent_kind, 'cc') AS agentKind, COALESCE(provider_id, '') AS providerId, ` +
		`COALESCE(last_activity_at, 0) AS lastActivityAt, COALESCE(user_send_at, 0) AS userSendAt, ` +
		`COALESCE(updated_at, 0) AS updatedAt, COALESCE(list_preview, '') AS listPreview ` +
		`FROM sessions WHERE status = 'active' AND hidden_at IS NULL ` +
		`ORDER BY COALESCE(last_activity_at, user_send_at, updated_at) DESC LIMIT 300`
	out, err := exec.CommandContext(cctx, sqlite, "-readonly", "-bail", "-json", "-cmd", ".timeout 2000", dbPath, q).Output()
	if err != nil {
		return nil, err
	}
	if len(bytes.TrimSpace(out)) == 0 {
		return nil, nil
	}
	var rows []cindyDBRow
	if err := json.Unmarshal(out, &rows); err != nil {
		return nil, err
	}
	return rows, nil
}

func readCindySQLiteMessages(ctx context.Context, dbPath, sessionID string) ([]cindyMessageRow, error) {
	sqlite, err := exec.LookPath("sqlite3")
	if err != nil || strings.ContainsAny(sessionID, "'\"\x00") {
		return nil, err
	}
	cctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	q := fmt.Sprintf(`SELECT id, role, COALESCE(content, '') AS content, COALESCE(tool_use_id, '') AS toolUseId, `+
		`COALESCE(created_at, 0) AS createdAt FROM messages `+
		`WHERE session_id = '%s' AND rewind_at IS NULL ORDER BY created_at ASC, rowid ASC LIMIT 500`, sessionID)
	out, err := exec.CommandContext(cctx, sqlite, "-readonly", "-bail", "-json", "-cmd", ".timeout 2000", dbPath, q).Output()
	if err != nil || len(bytes.TrimSpace(out)) == 0 {
		return nil, err
	}
	var rows []cindyMessageRow
	_ = json.Unmarshal(out, &rows)
	return rows, nil
}
