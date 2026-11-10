package node

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/nwverse/persona/server/internal/proto"
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
