package hub

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/nwverse/persona/server/internal/db"
)

// installInputs are what newInstallConfig needs to know about this machine.
type installInputs struct {
	lanIP   string // this machine's LAN IPv4 ("" when none was found)
	listen  string // ":18660"
	tls     string // ":18662", or with an address: used as given
	outside bool   // TLS on every address
	agents  bool   // herdr or claude is here: keep this machine as a local machine
}

// newInstallConfig is the hub.json a new install gets (docs/assistant/onboarding.md 4.1). These defaults are written into new
// files only, never added to defaultConfig(): LoadConfig decodes every existing hub.json over defaultConfig(), so a
// default there would change every hub whose file does not name the field (Public is a value: a TLSListen default
// would turn TLS on for every old hub.json without "public").
func newInstallConfig(in installInputs) *Config {
	cfg := defaultConfig()
	cfg.Listen = in.listen
	if in.lanIP != "" {
		cfg.PublicURL = "http://" + net.JoinHostPort(in.lanIP, portOf(in.listen))
	}
	host, port, _ := net.SplitHostPort(in.tls)
	switch {
	case host != "":
		cfg.Public.TLSListen = in.tls
	case in.outside:
		cfg.Public.TLSListen = ":" + port
	case in.lanIP != "":
		// Only the LAN IPv4 address: Persona Link and mDNS need TLS, the internet over IPv6 does not get the port.
		cfg.Public.TLSListen = net.JoinHostPort(in.lanIP, port)
	}
	cfg.Admin.FirstSetup = firstSetupCode
	if !in.agents {
		cfg.Machines = []MachineConfig{}
	}
	return cfg
}

// setupConfig is what the later steps need from hub.json (environment overrides applied, like the hub does).
type setupConfig struct {
	*Config
	fresh bool
	// noLocal: a hub.json a hub has run with lists no machine, and claude or herdr is here (localMachine).
	noLocal bool
}

func (e *setupEnv) hubJSON() string { return filepath.Join(e.root, "hub.json") }

// config writes hub.json. A hub.json no hub has read yet (none, or one without a token: db.sh and `hub admin access`
// write such files before the first run) gets every new-install default it does not have; one a hub has run with
// keeps every value, and only gets what it cannot run without.
func (e *setupEnv) config() (*setupConfig, error) {
	raw, err := readHubJSON(e.root)
	if err != nil {
		return nil, e.fail("config", e.hubJSON()+" 读不了或不是 JSON 对象："+err.Error(), "修好或挪走这个文件，再运行 personad hub setup")
	}
	_, hasToken := raw.Get("token")
	fresh := !hasToken
	e.restartHub, e.restartWhy, e.hubRestart, e.switched, e.prevMode = false, nil, nil, false, ""
	var notes []string
	services := e.canManagerService(context.Background())
	if e.o.managerService && !services {
		return nil, e.fail("config", "这里装不了内置管理者的常驻服务（"+e.noManagerServiceWhy(context.Background())+"）", "去掉 --manager-service 重跑")
	}
	if fresh {
		in := e.installInputs(&notes)
		gen, err := marshalNoEscape(newInstallConfig(in))
		if err != nil {
			return nil, err
		}
		genObj, err := parseJSONObject(gen)
		if err != nil {
			return nil, err
		}
		if err := editHubJSON(e.root, func(o *jsonObject) error {
			if err := fillMissing(o, genObj); err != nil {
				return err
			}
			return freshManagerMode(o, services, e.o.managerService)
		}); err != nil {
			return nil, e.fail("config", "写不了 "+e.hubJSON()+"："+err.Error(), "检查这个目录的权限")
		}
	} else {
		if e.o.managerService {
			changed, err := e.toManagerService(context.Background())
			if err != nil {
				return nil, err
			}
			if changed {
				e.restartHub = true // a running hub reads assistant.mode only when it starts
				e.restartWhy = append(e.restartWhy, "内置管理者改成了自己的常驻服务")
				notes = append(notes, "assistant.mode 改成 service：hub 不再拉起内置管理者，它是自己的常驻服务")
			}
		}
		if note := e.keptListenNote(raw); note != "" {
			notes = append(notes, note)
		}
		if e.o.outside {
			if err := editHubJSON(e.root, outsideTLS); err != nil {
				return nil, e.fail("config", "写不了 "+e.hubJSON()+"："+err.Error(), "检查这个目录的权限")
			}
		}
	}
	b, err := os.ReadFile(e.hubJSON())
	if err != nil {
		return nil, e.fail("config", "读不了 "+e.hubJSON()+"："+err.Error(), "检查这个目录的权限")
	}
	cfg := &Config{Listen: ":18660", dir: e.root}
	if err := json.Unmarshal(b, cfg); err != nil {
		return nil, e.fail("config", e.hubJSON()+" 格式有误："+err.Error(), "修正 JSON 后重跑")
	}
	cfg.applyEnv()
	if _, err := doctorPort(cfg.Listen); err != nil {
		return nil, e.fail("config", "hub.json 的 listen（"+cfg.Listen+"）没有有效端口", "改成 \":18660\" 这样的写法后重跑")
	}
	detail := "已写好 " + e.hubJSON()
	if !fresh {
		detail = "用已有的 " + e.hubJSON() + "，已有的值不改"
	}
	if fresh {
		detail += "（明文端口 " + portOf(cfg.Listen)
		if cfg.Public.TLSListen != "" {
			detail += "，TLS " + cfg.Public.TLSListen
		} else {
			detail += "，没找到局域网地址，先不开 TLS"
		}
		detail += "）"
	}
	if len(notes) > 0 {
		detail += "；" + strings.Join(notes, "；")
	}
	e.report("config", true, detail, "")
	return &setupConfig{Config: cfg, fresh: fresh, noLocal: !fresh && noMachines(raw) && e.hasAgents()}, nil
}

// localMachine adds this machine to a hub.json a hub has run with that lists none, once claude or herdr is here
// (installed before them, #4083), and has service restart the hub, which reads machines only when it starts. Not while
// a manager keeps any machine (machines_kept.go): their entries left hub.json for its own list, and one added here
// would be handed to a manager on every rerun, over the entry it keeps. It reads that from the database, so it runs
// after database (which may start it).
func (e *setupEnv) localMachine(ctx context.Context, cfg *setupConfig) error {
	if !cfg.noLocal {
		return nil
	}
	kept, err := e.keptMachines(ctx, cfg.Config)
	if err != nil {
		e.report("config", false, "hub.json 里没有电脑，但读不了哪些电脑归管理者管（"+err.Error()+"），这次没补本机",
			"看管理界面的「电脑」里有没有这台电脑，没有就加上；修好后重跑 personad hub setup")
		return nil
	}
	if len(kept) > 0 {
		return nil
	}
	if err := editHubJSON(e.root, func(o *jsonObject) error {
		if !noMachines(o) {
			return nil
		}
		return o.Set("machines", []MachineConfig{defaultLocalMachine()})
	}); err != nil {
		return e.fail("config", "写不了 "+e.hubJSON()+"："+err.Error(), "检查这个目录的权限")
	}
	e.restartHub = true
	e.restartWhy = append(e.restartWhy, "hub.json 补了本机")
	e.report("config", true, "补上本机作为电脑（kind: local）", "")
	return nil
}

// keptMachines is machine → the manager that keeps it, as the hub reads it (loadMachinesKept). It migrates nothing:
// the hub running may be an older build than this program.
func keptMachines(ctx context.Context, cfg *Config) (map[string]string, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	database, err := db.Attach(ctx, cfg.DatabaseURL, db.HubSet)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", machinesKeptKey, err)
	}
	defer database.Close()
	h := &Hub{cfg: cfg, store: database, logger: log.New(io.Discard, "", 0)}
	if err := h.loadMachinesKept(ctx); err != nil {
		var pe *pgconn.PgError
		if errors.As(err, &pe) && pe.Code == "42P01" {
			return nil, nil // no hub_settings: a hub from before managers kept machines
		}
		return nil, fmt.Errorf("read %s: %w", machinesKeptKey, err)
	}
	return h.devices.kept, nil
}

// keptListenNote is what a rerun says about --listen / --tls on a hub.json a hub has run with: those flags only shape
// a new install, so a value given explicitly that differs from the file's is named with both sides; the same value
// again (or a flag not given) says nothing.
func (e *setupEnv) keptListenNote(raw *jsonObject) string {
	var file struct {
		Listen string
		Public struct {
			TLSListen string `json:"tls_listen"`
		}
	}
	file.Listen = ":18660" // Config's default when hub.json has none
	if b, ok := raw.Get("listen"); ok {
		_ = json.Unmarshal(b, &file.Listen)
	}
	if b, ok := raw.Get("public"); ok {
		_ = json.Unmarshal(b, &file.Public)
	}
	var diffs []string
	if e.o.listenSet && !sameListen(e.o.listen, file.Listen, "") {
		diffs = append(diffs, "listen 仍是 "+file.Listen+"（这次给的 --listen "+e.o.listen+"）")
	}
	// A new install binds a TLS address given without a host to the LAN IPv4 (newInstallConfig): that is the same.
	if e.o.tlsSet && !sameListen(e.o.tls, file.Public.TLSListen, e.lanIPv4()) {
		have := file.Public.TLSListen
		if have == "" {
			have = "空（没开 TLS）"
		}
		diffs = append(diffs, "public.tls_listen 仍是 "+have+"（这次给的 --tls "+e.o.tls+"）")
	}
	if len(diffs) == 0 {
		return ""
	}
	return strings.Join(diffs, "，") + "：--listen、--tls 只用于新装，已有的不改"
}

// sameListen: the address a flag asked for is the one the file has; a flag without a host also matches the file's
// port on lan (what a new install writes for it), and "localhost" / "0.0.0.0" read like their usual spellings.
func sameListen(flag, file, lan string) bool {
	if flag == file {
		return true
	}
	fh, fp, err1 := net.SplitHostPort(flag)
	hh, hp, err2 := net.SplitHostPort(file)
	if err1 != nil || err2 != nil || fp != hp {
		return false
	}
	norm := func(h string) string {
		switch h {
		case "0.0.0.0", "::", "*":
			return ""
		case "localhost":
			return "127.0.0.1"
		}
		return h
	}
	fh, hh = norm(fh), norm(hh)
	return fh == hh || (fh == "" && lan != "" && hh == lan)
}

// installInputs looks at this machine for a new hub.json: the LAN address, free ports (another hub on the default
// ones moves this one along by 100), whether there are agents to drive here.
func (e *setupEnv) installInputs(notes *[]string) installInputs {
	in := installInputs{lanIP: e.lanIPv4(), listen: e.o.listen, tls: e.o.tls, outside: e.o.outside}
	lp, _ := strconv.Atoi(portOf(in.listen))
	tp, _ := strconv.Atoi(portOf(in.tls))
	if !e.portFree(lp) || !e.portFree(tp) {
		for k := 1; k <= 20; k++ {
			l, t := lp+100*k, tp+100*k
			if t > 65535 {
				break
			}
			if e.portFree(l) && e.portFree(t) {
				*notes = append(*notes, fmt.Sprintf("端口 %d / %d 被别的程序占着，可能是另一个 Persona，改用 %d / %d", lp, tp, l, t))
				lh, _, _ := net.SplitHostPort(in.listen)
				th, _, _ := net.SplitHostPort(in.tls)
				in.listen, in.tls = net.JoinHostPort(lh, strconv.Itoa(l)), net.JoinHostPort(th, strconv.Itoa(t))
				break
			}
		}
	}
	in.agents = e.hasAgents()
	return in
}

// noMachines: hub.json says "machines": []. Without the key the hub starts with this machine already (defaultConfig).
func noMachines(o *jsonObject) bool {
	raw, ok := o.Get("machines")
	var list []json.RawMessage
	return ok && json.Unmarshal(raw, &list) == nil && len(list) == 0
}

func (e *setupEnv) isInsideContainer() bool {
	if e.isContainer != nil {
		return e.isContainer()
	}
	return inContainer()
}

func (e *setupEnv) hasAgents() bool {
	if e.getenv("PERSONA_LOCAL_NODE") == "0" || e.isInsideContainer() {
		return false
	}
	return e.hasProgram("herdr") || e.hasProgram("claude")
}

// hasProgram: on PATH, or where the node looks besides (install.sh's has_herdr).
func (e *setupEnv) hasProgram(name string) bool {
	if _, err := e.lookPath(name); err == nil {
		return true
	}
	for _, d := range []string{filepath.Join(e.home, ".local", "bin"), filepath.Join(e.home, "bin"), "/opt/homebrew/bin", "/usr/local/bin"} {
		if fi, err := os.Stat(filepath.Join(d, name)); err == nil && !fi.IsDir() && fi.Mode()&0o111 != 0 {
			return true
		}
	}
	return false
}

// fillMissing adds what src has and dst does not, into nested objects too; nothing dst has is changed.
func fillMissing(dst, src *jsonObject) error {
	for _, k := range src.Keys() {
		want, _ := src.Get(k)
		have, ok := dst.Get(k)
		if !ok {
			dst.SetRaw(k, want)
			continue
		}
		if !isJSONObject(want) || !isJSONObject(have) {
			continue
		}
		srcNested, srcErr := parseJSONObject(want)
		dstNested, dstErr := parseJSONObject(have)
		if srcErr != nil || dstErr != nil {
			continue
		}
		if err := fillMissing(dstNested, srcNested); err != nil {
			return err
		}
		dst.SetRaw(k, dstNested.Marshal())
	}
	return nil
}

func isJSONObject(raw json.RawMessage) bool {
	t := bytes.TrimSpace(raw)
	return len(t) > 0 && t[0] == '{'
}

// outsideTLS is --outside on an existing hub.json: TLS on every address (a LAN-only "<address>:18662" becomes
// ":18662"; none becomes ":18662"). The person asked for it, so this one existing value does change.
func outsideTLS(o *jsonObject) error {
	pub, err := o.Object("public")
	if err != nil {
		return err
	}
	var cur string
	if raw, ok := pub.Get("tls_listen"); ok {
		_ = json.Unmarshal(raw, &cur)
	}
	want := ":18662"
	if cur != "" {
		want = ":" + portOf(cur)
	}
	if cur == want {
		return nil
	}
	if err := pub.Set("tls_listen", want); err != nil {
		return err
	}
	o.SetObject("public", pub)
	return nil
}
