package node

import (
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/nwverse/persona/server/internal/compat"
	"github.com/nwverse/persona/server/internal/i18n"
	"github.com/nwverse/persona/server/internal/lazyre"
)

// The resident node on a Mac is a launchd agent (scripts/deploy/node-launchd.sh, persona-link's nodesvc.go). A plist
// without ProcessType gets launchd's default limits ("spawn type = daemon" in `launchctl print`): the node and the
// Claude Code it starts are throttled like a background job. One such node got 15.45 CPU seconds in 139 s (scheduling
// priority 4 instead of 31), and Claude did not start in time. ProcessType=Interactive lifts the limits; it grants no
// privacy (TCC) permission. New installs write it. An existing plist is not rewritten behind the user's back:
// `node doctor` reports it, and `node doctor --fix` writes it and loads the agent again (#5944, docs/server/server.md
// "Persistent node").

// NodeLaunchdLabel is the resident node's launchd agent, as both installers name it (and write it on a new install).
const NodeLaunchdLabel = "com.persona.node"

// legacyNodeLaunchdLabel is the name the installers gave the agent before the rename (#277). Both leave an agent that
// is already installed as it is, so a Mac set up before then keeps running its node under this label.
const legacyNodeLaunchdLabel = compat.NodeLabel

// nodeLaunchdLabels are the labels the resident node's agent can have, today's first.
var nodeLaunchdLabels = []string{NodeLaunchdLabel, legacyNodeLaunchdLabel}

// isNodeLaunchdLabel says whether name is one of the resident node's labels. launchd sets XPC_SERVICE_NAME to the
// label of the job it started, and the node's children inherit it.
func isNodeLaunchdLabel(name string) bool { return slices.Contains(nodeLaunchdLabels, name) }

const (
	launchdCheck = "launchd-process-type"
	// launchdReloaded is the name --fix reports (DoctorReport.Fixed) when it loaded the agent again; launchdCheck there
	// means it rewrote the plist.
	launchdReloaded    = "launchd-reload"
	processInteractive = "Interactive"
)

// NodeLaunchAgentPath is where the resident node's launchd agent called label is described.
func NodeLaunchAgentPath(home, label string) string {
	return filepath.Join(home, "Library", "LaunchAgents", label+".plist")
}

// launchdProgram is the program of the launchd agent called label (a process's XPC_SERVICE_NAME, which what the agent
// starts inherits), as its plist in home's LaunchAgents names it: Program, else the first of ProgramArguments. TCC
// holds what an agent starts to the agent's program for its privacy permissions, so the node of the hub's computer,
// which the built-in manager's agent starts, reads with that program's (persona-manager on a new install). "" when
// there is no such plist or it names no program.
func launchdProgram(home, label string) string {
	if home == "" || label == "" || strings.ContainsAny(label, "/\\") {
		return ""
	}
	b, err := os.ReadFile(NodeLaunchAgentPath(home, label))
	if err != nil {
		return ""
	}
	entries, _, err := plistTop(b)
	if err != nil {
		return ""
	}
	var p string
	if e := entries["Program"]; e.elem == "string" {
		p = strings.TrimSpace(e.text)
	}
	if e := entries["ProgramArguments"]; p == "" && e.elem == "array" {
		p = firstPlistString(b[e.start:e.end])
	}
	if rest, ok := strings.CutPrefix(p, "~/"); ok {
		p = filepath.Join(home, rest)
	}
	return p
}

// firstPlistString is the text of the first <string> in b, a plist <array> element; "" when it has none.
func firstPlistString(b []byte) string {
	d := xml.NewDecoder(bytes.NewReader(b))
	for {
		tok, err := d.Token()
		if err != nil {
			return ""
		}
		if t, ok := tok.(xml.StartElement); ok && t.Name.Local == "string" {
			var s string
			if d.DecodeElement(&s, &t) != nil {
				return ""
			}
			return strings.TrimSpace(s)
		}
	}
}

// launchdDomain is this user's login session as launchctl names it; an agent there is domain + "/" + label.
func launchdDomain() string { return "gui/" + strconv.Itoa(os.Getuid()) }

// pickNodeLaunchdLabel says which of the resident node's agents the doctor looks at, and --fix rewrites and reloads:
// the one whose plist is in home's LaunchAgents; today's label when there is none.
//
// With both there (an agent from before the rename next to one installed again under today's label), it is the one
// launchd has loaded. That is the node that is running: the check is about how launchd runs it (spawn type), and it
// is the one --fix has to reload. The other plist is a file launchd is not running now; loading it would start a
// second node next to the running one. self is this process's XPC_SERVICE_NAME: under one of the agents, that one is
// loaded, without asking launchctl. Both loaded (two nodes), neither loaded, or nobody to ask (launchctl nil):
// today's, which is what a new install writes.
func pickNodeLaunchdLabel(ctx context.Context, home, domain, self string,
	launchctl func(context.Context, ...string) (string, error)) string {
	var present []string
	for _, label := range nodeLaunchdLabels {
		// Missing means what it means to inspectLaunchAgent; a plist it cannot look at is there, and reported so.
		if _, err := os.Stat(NodeLaunchAgentPath(home, label)); !errors.Is(err, fs.ErrNotExist) {
			present = append(present, label)
		}
	}
	switch {
	case len(present) == 1:
		return present[0]
	case len(present) == 0:
		return NodeLaunchdLabel
	case slices.Contains(present, self):
		return self
	}
	if launchctl != nil {
		var loaded []string
		for _, label := range present {
			if _, err := launchctl(ctx, "print", domain+"/"+label); err == nil {
				loaded = append(loaded, label)
			}
		}
		if len(loaded) == 1 {
			return loaded[0]
		}
	}
	return NodeLaunchdLabel
}

// runLaunchctl is DoctorOptions.Launchctl on a Mac.
func runLaunchctl(ctx context.Context, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "/bin/launchctl", args...).CombinedOutput()
	return string(out), err
}

// launchAgent is what the doctor found about the agent.
type launchAgent struct {
	exists      bool
	readErr     error
	plist       []byte
	parseErr    error
	processType string // "" when the plist has none
	logPath     string // the plist's StandardErrorPath: where a node that does not come back says why
	loaded      bool   // launchctl print knows it
	spawnType   string // launchctl print's "spawn type" (daemon, interactive, background, adaptive); "" when not asked
}

var spawnTypeLine = lazyre.New(`(?m)^\s*spawn type = ([A-Za-z]+)`)

func inspectLaunchAgent(ctx context.Context, opts DoctorOptions) launchAgent {
	var a launchAgent
	b, err := os.ReadFile(opts.LaunchAgent)
	if errors.Is(err, fs.ErrNotExist) {
		return a
	}
	a.exists = true
	if err != nil {
		a.readErr = err
		return a
	}
	a.plist = b
	if entries, _, err := plistTop(b); err != nil {
		a.parseErr = err
	} else {
		if e, ok := entries["ProcessType"]; ok {
			a.processType = strings.TrimSpace(e.text)
		}
		if e := entries["StandardErrorPath"]; e.elem == "string" {
			a.logPath = strings.TrimSpace(e.text)
		}
	}
	if opts.Launchctl != nil && opts.LaunchdTarget != "" {
		if out, err := opts.Launchctl(ctx, "print", opts.LaunchdTarget); err == nil {
			a.loaded = true
			if m := spawnTypeLine.FindStringSubmatch(out); m != nil {
				a.spawnType = m[1]
			}
		}
	}
	return a
}

// staleLoad: launchd runs the agent from a plist read before ProcessType=Interactive was written into it.
func (a launchAgent) staleLoad() bool {
	return a.loaded && a.spawnType != "" && a.spawnType != "interactive"
}

const (
	launchdFixHintKey       = "node-doctor.launchd_fix_hint"
	launchdFixHintInsideKey = "node-doctor.launchd_fix_hint_inside"

	// Legacy test and display callers compare these default strings. Keep them as constants: translating them here
	// would load every zh-Hans catalog during package initialization, before nodecmd knows whether it needs a report.
	launchdFixHint = "在这台 Mac 上运行 personad node doctor --fix：写上 ProcessType=Interactive 并重新加载（常驻 node 会重启，进行中的助手对话会中断）"
	// launchdFixHintInside is the hint inside the node (the web page's self-check, a claude the node started): the
	// same command, from a terminal of the Mac's own.
	launchdFixHintInside = "在这台 Mac 的终端里运行 personad node doctor --fix（在常驻 node 启动的进程里运行不行：重新加载会把 node 自己停掉）：" +
		"写上 ProcessType=Interactive 并重新加载（常驻 node 会重启，进行中的助手对话会中断）"
)

// checkLaunchAgent says what a is. Inside the node --fix leaves the agent alone (RunDoctor), so the hint sends the
// user to a terminal, and a --fix there says it did nothing to it.
func checkLaunchAgent(a launchAgent, opts DoctorOptions) DoctorCheck {
	c := checkLaunchAgentFile(a, opts.LaunchAgent, opts.Lang)
	if opts.InsideNode && c.Hint == i18n.T(opts.Lang, launchdFixHintKey) {
		c.Hint = i18n.T(opts.Lang, launchdFixHintInsideKey)
		if opts.Fix {
			c.Detail = i18n.T(opts.Lang, "node-doctor.launchd_inside_fix_detail", i18n.Args{"detail": c.Detail})
		}
	}
	return c
}

func checkLaunchAgentFile(a launchAgent, path string, lang i18n.Lang) DoctorCheck {
	c := DoctorCheck{Name: launchdCheck, Items: []string{}}
	switch {
	case !a.exists:
		c.Status, c.Detail = DoctorOK, i18n.T(lang, "node-doctor.launchd_missing")
		return c
	case a.readErr != nil:
		c.Status = DoctorWarn
		c.Detail = i18n.T(lang, "node-doctor.launchd_unreadable", i18n.Args{"error": shorten(a.readErr.Error(), 80)})
	case errors.Is(a.parseErr, errBinaryPlist):
		c.Status, c.Detail = DoctorWarn, i18n.T(lang, "node-doctor.launchd_binary")
		c.Hint = i18n.T(lang, "node-doctor.launchd_binary_hint", i18n.Args{"path": shellQuote(path)})
	case a.parseErr != nil:
		c.Status = DoctorWarn
		c.Detail = i18n.T(lang, "node-doctor.launchd_invalid", i18n.Args{"error": shorten(a.parseErr.Error(), 80)})
		c.Hint = i18n.T(lang, "node-doctor.launchd_invalid_hint")
	case a.processType == "":
		c.Status = DoctorWarn
		c.Detail = i18n.T(lang, "node-doctor.launchd_no_process_type")
		c.Hint = i18n.T(lang, launchdFixHintKey)
	case a.processType != processInteractive:
		c.Status = DoctorWarn
		c.Detail = i18n.T(lang, "node-doctor.launchd_other_process_type", i18n.Args{"process_type": a.processType})
		c.Hint = i18n.T(lang, launchdFixHintKey)
	case a.staleLoad():
		c.Status = DoctorWarn
		c.Detail = i18n.T(lang, "node-doctor.launchd_stale_load", i18n.Args{"spawn": a.spawnType})
		c.Hint = i18n.T(lang, launchdFixHintKey)
	default:
		c.Status, c.Detail = DoctorOK, "ProcessType=Interactive"
		if a.loaded && a.spawnType != "" {
			c.Detail = i18n.T(lang, "node-doctor.launchd_interactive_running", i18n.Args{"spawn": a.spawnType})
		}
		return c
	}
	c.Items = []string{path}
	return c
}

// launchdRetries and launchdRetryEvery: right after bootout, launchd may still be taking the old job down and refuse
// the bootstrap (5: Input/output error) for a moment.
const (
	launchdRetries    = 10
	launchdRetryEvery = 300 * time.Millisecond
)

type doctorWrappedError struct {
	text  string
	cause error
}

func (e *doctorWrappedError) Error() string { return e.text }
func (e *doctorWrappedError) Unwrap() error { return e.cause }

func wrapDoctorError(lang i18n.Lang, key string, args i18n.Args, cause error) error {
	args["error"] = cause.Error()
	return &doctorWrappedError{text: i18n.T(lang, key, args), cause: cause}
}

// fixLaunchAgent writes ProcessType=Interactive into the agent's plist (nothing else in it changes) and, when launchd
// has the agent loaded, loads it again so that the running node gets it. An agent that is not loaded stays unloaded:
// launchd reads the new plist at the next login. wrote and reloaded say what it did; a reload that failed is err
// (wrote stays true when the plist was rewritten before it).
func fixLaunchAgent(ctx context.Context, opts DoctorOptions, a launchAgent) (wrote, reloaded bool, err error) {
	if !a.exists || a.readErr != nil || a.parseErr != nil {
		return false, false, nil
	}
	rewrite := a.processType != processInteractive
	reload := a.loaded && (rewrite || a.staleLoad())
	if rewrite {
		b, err := withProcessType(a.plist)
		if err != nil {
			return false, false, wrapDoctorError(opts.Lang, "node-doctor.launchd_rewrite_failed", i18n.Args{"path": opts.LaunchAgent}, err)
		}
		if err := replaceFile(opts.LaunchAgent, b); err != nil {
			return false, false, wrapDoctorError(opts.Lang, "node-doctor.launchd_rewrite_failed", i18n.Args{"path": opts.LaunchAgent}, err)
		}
		wrote = true
	}
	if reload && opts.Launchctl != nil {
		if err := reloadLaunchAgent(ctx, opts); err != nil {
			return wrote, false, err
		}
		reloaded = true
	}
	return wrote, reloaded, nil
}

// nodeRestartWait is how long --fix waits, after loading the agent again, for the restarted node to answer: launchd
// returns from bootstrap at once, a node publishes its sockets about a second after it starts, and launchd starts one
// that exited at once again after its ThrottleInterval (10 s in both installers).
const nodeRestartWait = 15 * time.Second

// restartSockets are the checks of what the resident node serves, which a reload takes away until the new node is up.
var restartSockets = map[string]func(context.Context, DoctorOptions) DoctorCheck{
	"node.sock": func(ctx context.Context, o DoctorOptions) DoctorCheck {
		return checkNodeSocketIn(ctx, o.NodeSock, o.Lang)
	},
	"runtime.sock": func(ctx context.Context, o DoctorOptions) DoctorCheck {
		return checkRuntimeSocketIn(ctx, o.RuntimeSock, o.Lang)
	},
}

// waitForRestart waits, at most opts.RestartWait (nodeRestartWait when 0), for the sockets that answered before the
// reload (in before) to answer again, so that the report after --fix is about the restarted node rather than the gap
// before it. It returns the ones that still do not.
func waitForRestart(ctx context.Context, opts DoctorOptions, before DoctorReport) []string {
	var want []string
	for _, c := range before.Checks {
		if restartSockets[c.Name] != nil && c.Status == DoctorOK {
			want = append(want, c.Name)
		}
	}
	wait := opts.RestartWait
	if wait <= 0 {
		wait = nodeRestartWait
	}
	ctx, cancel := context.WithTimeout(ctx, wait)
	defer cancel()
	for {
		want = slices.DeleteFunc(want, func(name string) bool { return restartSockets[name](ctx, opts).Status == DoctorOK })
		if len(want) == 0 {
			return nil
		}
		select {
		case <-ctx.Done():
			return want
		case <-time.After(200 * time.Millisecond):
		}
	}
}

// notBackYet marks the checks of the sockets the reloaded node did not bring back in time (waitForRestart): still
// failing, but said to be right after a restart, with where the node says why.
func notBackYet(rep *DoctorReport, names []string, wait time.Duration, logPath string, lang i18n.Lang) {
	if wait <= 0 {
		wait = nodeRestartWait
	}
	for i := range rep.Checks {
		c := &rep.Checks[i]
		if c.Status == DoctorOK || !slices.Contains(names, c.Name) {
			continue
		}
		c.Detail = i18n.T(lang, "node-doctor.launchd_not_back_detail", i18n.Args{"detail": c.Detail, "wait": wait})
		if logPath == "" {
			c.Hint = i18n.T(lang, "node-doctor.launchd_not_back_hint")
		} else {
			c.Hint = i18n.T(lang, "node-doctor.launchd_not_back_log_hint", i18n.Args{"path": logPath})
		}
	}
}

// reloadLaunchAgent takes the agent out of launchd and loads it again from its plist: kickstart would restart it
// from the definition launchd already has.
func reloadLaunchAgent(ctx context.Context, opts DoctorOptions) error {
	target := opts.LaunchdTarget
	domain := target[:max(strings.LastIndex(target, "/"), 0)]
	bootOut, bootErr := opts.Launchctl(ctx, "bootout", target)
	var last string
	for i := 1; ; i++ {
		out, err := opts.Launchctl(ctx, "bootstrap", domain, opts.LaunchAgent)
		if err == nil {
			return nil
		}
		last = firstLine(out)
		if last == "" {
			last = err.Error()
		}
		if i == launchdRetries || ctx.Err() != nil {
			break
		}
		select {
		case <-ctx.Done():
		case <-time.After(launchdRetryEvery):
		}
	}
	if bootErr != nil {
		if s := firstLine(bootOut); s != "" {
			last = i18n.T(opts.Lang, "node-doctor.launchd_bootout_detail", i18n.Args{"detail": last, "output": s})
		}
	}
	return errors.New(i18n.T(opts.Lang, "node-doctor.launchd_bootstrap_failed", i18n.Args{
		"detail": last, "domain": domain, "path": shellQuote(opts.LaunchAgent),
	}))
}

// replaceFile writes b over path in one step, keeping its permissions; through a symlink, the file it points to.
func replaceFile(path string, b []byte) error {
	if real, err := filepath.EvalSymlinks(path); err == nil {
		path = real
	}
	mode := fs.FileMode(0o644)
	if st, err := os.Stat(path); err == nil {
		mode = st.Mode().Perm()
	}
	f, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	_, err = f.Write(b)
	if err == nil {
		err = f.Chmod(mode)
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Rename(tmp, path)
	}
	if err != nil {
		os.Remove(tmp)
	}
	return err
}

var errBinaryPlist = errors.New("binary property list")

// plistEntry is one key of an XML property list's top-level dictionary: the element of its value ("string", "true",
// "dict"...), the text of a simple value, and where the value is in the file.
type plistEntry struct {
	elem, text string
	start, end int64
}

// plistTop reads an XML property list's top-level dictionary, and where it closes (the offset of its </dict>).
func plistTop(b []byte) (map[string]plistEntry, int64, error) {
	if bytes.HasPrefix(b, []byte("bplist")) {
		return nil, 0, errBinaryPlist
	}
	d := xml.NewDecoder(bytes.NewReader(b))
	for _, name := range []string{"plist", "dict"} {
		if err := plistEnter(d, name); err != nil {
			return nil, 0, err
		}
	}
	entries := map[string]plistEntry{}
	key, haveKey := "", false
	for {
		at := d.InputOffset()
		tok, err := d.Token()
		if err != nil {
			return nil, 0, err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if t.Name.Local == "key" {
				if err := d.DecodeElement(&key, &t); err != nil {
					return nil, 0, err
				}
				haveKey = true
				continue
			}
			if !haveKey {
				return nil, 0, fmt.Errorf("<%s> without a key", t.Name.Local)
			}
			e := plistEntry{elem: t.Name.Local, start: at}
			switch t.Name.Local {
			case "dict", "array":
				err = d.Skip()
			default:
				err = d.DecodeElement(&e.text, &t)
			}
			if err != nil {
				return nil, 0, err
			}
			e.end = d.InputOffset()
			entries[key] = e
			key, haveKey = "", false
		case xml.EndElement:
			if t.Name.Local != "dict" || at == d.InputOffset() { // <dict/> closes where it opens: no room for a key
				return nil, 0, errors.New("empty top-level dictionary")
			}
			return entries, at, nil
		}
	}
}

// plistEnter reads up to and including the start of element name, the next element.
func plistEnter(d *xml.Decoder, name string) error {
	for {
		tok, err := d.Token()
		if err != nil {
			return err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if t.Name.Local != name {
				return fmt.Errorf("<%s> where <%s> should be", t.Name.Local, name)
			}
			return nil
		case xml.EndElement:
			return fmt.Errorf("</%s> where <%s> should be", t.Name.Local, name)
		}
	}
}

// withProcessType is the plist b with ProcessType set to Interactive: its value replaced when there is one, a line
// added at the end of the dictionary otherwise. Everything else stays byte for byte.
func withProcessType(b []byte) ([]byte, error) {
	entries, closeAt, err := plistTop(b)
	if err != nil {
		return nil, err
	}
	const value = "<string>" + processInteractive + "</string>"
	var out []byte
	if e, ok := entries["ProcessType"]; ok {
		out = append(append(append(out, b[:e.start]...), value...), b[e.end:]...)
	} else {
		line := "  <key>ProcessType</key>" + value + "\n"
		if closeAt > 0 && b[closeAt-1] != '\n' {
			line = "\n" + line
		}
		out = append(append(append(out, b[:closeAt]...), line...), b[closeAt:]...)
	}
	// What was written reads back as the same dictionary but for ProcessType.
	got, _, err := plistTop(out)
	if err != nil {
		return nil, err
	}
	if got["ProcessType"].elem != "string" || got["ProcessType"].text != processInteractive {
		return nil, errors.New("ProcessType did not take")
	}
	for k, e := range entries {
		if g, ok := got[k]; k != "ProcessType" && (!ok || g.elem != e.elem || g.text != e.text) {
			return nil, fmt.Errorf("%s changed", k)
		}
	}
	if len(got) != len(entries) && !(len(got) == len(entries)+1 && entries["ProcessType"].elem == "") {
		return nil, errors.New("keys changed")
	}
	return out, nil
}
