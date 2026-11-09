package sandbox

import (
	"errors"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"
)

// agentDial connects to the unix socket at path: 0 when it answers the connection, 1 when it cannot.
func agentDial(path string) int {
	c, err := net.DialTimeout("unix", path, 2*time.Second)
	if err != nil {
		return 1
	}
	c.Close()
	return 0
}

// agentFixture is a fake home with ~/.persona and an explicit custom .state-link symlink, with a file of each
// kind the agent sandbox closes or leaves open. Nothing in it is a real secret.
type agentFixture struct {
	home, dir, alias string // dir: the real data directory; alias: an explicit custom .state-link
	ours, theirs     string // this start's launcher settings, another launcher's
}

func newAgentFixture(t *testing.T) agentFixture {
	t.Helper()
	// A short home under /tmp (/private/tmp, a link away): a unix socket's path is limited to 104 bytes on macOS.
	tmp, err := os.MkdirTemp("/tmp", "pas-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(tmp) })
	home := realPath(tmp)
	f := agentFixture{home: home, dir: filepath.Join(home, ".persona"), alias: filepath.Join(home, ".state-link")}
	f.ours = filepath.Join(f.dir, "launcher-settings", "claude-ours.json")
	f.theirs = filepath.Join(f.dir, "launcher-settings", "other-claude-settings.json")
	files := map[string]string{
		"secret.key": "k", "secret.key.bak": "k", "handles.key": "k", "launch.key": "k", "hub.json": "{}", "hub.json.bak": "{}",
		"link.json":       "{}",
		".hub.json.12345": "{}", "tls/hub.key": "k", "tls/.tls-1": "k", "link-tls/id.key": "k",
		"managers/m1/manager.json": "{}", "managers/m1/secret.key": "k", "extensions/data/notes/token": "t",
		"launcher-settings/claude-ours.json": "{}", "launcher-settings/other-claude-settings.json": "{}",
		"mcp.json": "{}", "claude-settings.json": "{}", "data/talk.key": "keynote", "data/proj/link.json": "{}",
		"relay-tunnel.token": "t", "docker/db-password": "p", "edge.token": "t", "edge.secret": "s", "edge.password": "p",
		"data/proj/edge.token": "t",
		"assistant/notes.md":   "n",
		AgentOnFile:            "",
	}
	for rel, body := range files {
		p := filepath.Join(f.dir, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	for _, d := range []string{"extensions/run/notes", "run", "bin"} {
		if err := os.MkdirAll(filepath.Join(f.dir, d), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(".persona", f.alias); err != nil {
		t.Fatal(err)
	}
	return f
}

func (f agentFixture) plan() AgentPlan {
	return AgentPlan{DataDirs: []string{f.alias, f.dir, f.alias}, Home: f.home, Cwd: filepath.Join(f.alias, "assistant"),
		LauncherSettings: []string{filepath.Join(f.alias, "launcher-settings", "claude-ours.json")}}
}

// The profile names each data directory once by its real path (the alias only as a name not to be replaced), escapes
// it in the regexes, closes the folders above it and above a closed folder in it to renames, and opens only this
// start's launcher settings.
func TestDarwinAgentProfile(t *testing.T) {
	f := newAgentFixture(t)
	got, err := DarwinAgent(f.plan())
	if err != nil {
		t.Fatal(err)
	}
	D := f.dir
	var lits []string
	for p := D; p != "/"; p = filepath.Dir(p) {
		lits = append(lits, `(literal "`+p+`")`)
	}
	lits = append(lits, `(literal "`+D+`/agent-sandbox.on")`, `(literal "`+D+`/extensions")`, `(literal "`+f.alias+`")`)
	re := reQuote(D)
	want := strings.Join([]string{
		`(version 1)`,
		`(allow default)`,
		`(deny file-read* file-write*`,
		`  (regex #"^` + re + `/\.?(hub\.json|link\.json|tailcat\.json|tailcat-hub\.json|daemons\.json|manager\.json|apns|relay\.json|relay\.secret|relay-tunnel\.token|devices\.json|mail_accounts\.json)[^/]*(/.*)?$")`,
		`  (regex #"^` + re + `/[^/]*\.(key|p8|pem|crt|token|secret|password)([.~-][^/]*)?$")`,
		`  (subpath "` + D + `/launcher-settings")`,
		`  (subpath "` + D + `/tls")`,
		`  (subpath "` + D + `/link-tls")`,
		`  (subpath "` + D + `/docker")`,
		`  (subpath "` + D + `/extensions/data")`,
		`  (subpath "` + D + `/extensions/staging")`,
		`  (subpath "` + D + `/plugins")`,
		`  (subpath "` + D + `/node-plugins")`,
		`  (subpath "` + D + `/managers")`,
		`  (subpath "` + D + `/extensions/run")`,
		`  (subpath "` + D + `/run"))`,
		`(deny network-outbound`,
		`  (remote unix-socket (subpath "` + D + `/extensions/run"))`,
		`  (remote unix-socket (subpath "` + D + `/plugins/run"))`,
		`  (remote unix-socket (subpath "` + D + `/run")))`,
		`(deny file-write*`,
		`  ` + strings.Join(lits, "\n  ") + `)`,
		`(allow file-read* (literal "` + D + `/launcher-settings/claude-ours.json"))`,
		`(allow file-read-metadata (literal "` + D + `/launcher-settings"))`,
	}, "\n")
	if got != want {
		t.Fatalf("profile:\n%s\nwant:\n%s", got, want)
	}
	if strings.Contains(got, `"`+f.alias+`/`) {
		t.Error("a rule names the data directory by its link, not its real path")
	}
	if !strings.Contains(re, `\.persona`) {
		t.Errorf("not escaped: %s", re)
	}
}

// A data directory that is the home (PERSONA_HOME=~) or holds the project the agent runs in is refused: its rules
// would reach the user's own files. Persona's own workspaces in it are fine.
func TestDarwinAgentRefuses(t *testing.T) {
	f := newAgentFixture(t)
	if _, err := DarwinAgent(AgentPlan{DataDirs: []string{f.home}, Home: f.home}); err == nil || !strings.Contains(err.Error(), "home") {
		t.Errorf("home: %v", err)
	}
	if _, err := DarwinAgent(AgentPlan{DataDirs: []string{filepath.Dir(f.home)}, Home: f.home}); err == nil {
		t.Error("above the home: no error")
	}
	proj := filepath.Join(f.dir, "code", "app")
	if err := os.MkdirAll(proj, 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := DarwinAgent(AgentPlan{DataDirs: []string{f.dir}, Home: f.home, Cwd: proj}); err == nil || !strings.Contains(err.Error(), "工作目录") {
		t.Errorf("project inside: %v", err)
	}
	for _, ws := range []string{"assistant", "health", "guest/x"} {
		cwd := filepath.Join(f.dir, ws)
		_ = os.MkdirAll(cwd, 0o700)
		if _, err := DarwinAgent(AgentPlan{DataDirs: []string{f.dir}, Home: f.home, Cwd: cwd}); err != nil {
			t.Errorf("%s: %v", ws, err)
		}
	}
	if _, err := DarwinAgent(AgentPlan{DataDirs: []string{filepath.Join(f.home, "missing")}, Home: f.home}); err == nil {
		t.Error("no data directory: no error")
	}
}

// WrapAgent wraps only when it can, and says why not otherwise; inside the sandbox it leaves a start alone.
func TestWrapAgentFallsBack(t *testing.T) {
	f := newAgentFixture(t)
	argv, env := []string{"/bin/echo", "hi"}, []string{"A=1"}
	ok := func() Support { return Support{OS: "darwin", SandboxExec: true} }
	plan := f.plan()
	plan.Probe, plan.Check = ok, func(string) error { return nil }
	a, e, st := WrapAgent(plan, argv, env)
	if st.State != AgentActive || a[0] != "/usr/bin/sandbox-exec" || a[1] != "-p" || !slices.Equal(a[3:], argv) || !slices.Contains(e, AgentEnv+"=1") {
		t.Fatalf("%+v %v %v", st, a, e)
	}
	// Already inside: nothing added.
	if a2, _, st := WrapAgent(plan, argv, e); st.State != AgentActive || !slices.Equal(a2, argv) {
		t.Errorf("nested: %+v %v", st, a2)
	}
	cases := map[string]struct {
		plan  AgentPlan
		state string
	}{
		"linux":       {AgentPlan{DataDirs: plan.DataDirs, Home: f.home, Probe: func() Support { return Support{OS: "linux", Bwrap: "/usr/bin/bwrap"} }}, AgentUnsupported},
		"no tool":     {AgentPlan{DataDirs: plan.DataDirs, Home: f.home, Probe: func() Support { return Support{OS: "darwin", Why: "机器上没有 sandbox-exec"} }}, AgentUnavailable},
		"inner":       {AgentPlan{DataDirs: plan.DataDirs, Home: f.home, Probe: ok, Inner: "Codex 自带沙箱"}, AgentSkipped},
		"home":        {AgentPlan{DataDirs: []string{f.home}, Home: f.home, Probe: ok}, AgentSkipped},
		"bad profile": {AgentPlan{DataDirs: plan.DataDirs, Home: f.home, Probe: ok, Check: func(string) error { return errors.New("syntax") }}, AgentUnavailable},
	}
	for name, c := range cases {
		a, e, st := WrapAgent(c.plan, argv, env)
		if st.State != c.state || st.Why == "" || !slices.Equal(a, argv) || !slices.Equal(e, env) {
			t.Errorf("%s: %+v %v %v", name, st, a, e)
		}
	}
}

// TestDarwinAgentSandboxForReal runs shell commands under the real profile in a fake data directory (~/.persona, also
// named by a custom .state-link): the secrets cannot be read (directly, through the link, through a link elsewhere, as backups,
// at any depth, in another case), the data directory cannot be moved, the extensions' sockets cannot be dialed; what
// an agent needs works (Persona's own files, its socket, this start's launcher settings, personad, git, the network).
// ps is a setuid program and does not start in a sandbox (a known side effect). The control runs the must-fail
// commands with no rules and needs them to succeed, so their failures come from the rules.
func TestDarwinAgentSandboxForReal(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("darwin only")
	}
	if sup := Probe(); !sup.SandboxExec {
		t.Skip("no sandbox here: " + sup.Why)
	}
	f := newAgentFixture(t)
	D, alias := f.dir, f.alias
	self, _ := os.Executable()
	self = realPath(self)
	bin := filepath.Join(D, "bin", "personad")
	if b, err := os.ReadFile(self); err != nil {
		t.Fatal(err)
	} else if err := os.WriteFile(bin, b, 0o700); err != nil {
		t.Fatal(err)
	}
	elsewhere := filepath.Join(f.home, "link-to-key")
	if err := os.Symlink(filepath.Join(D, "secret.key"), elsewhere); err != nil {
		t.Fatal(err)
	}
	sock := func(p string) {
		if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
			t.Fatal(err)
		}
		ln, err := net.Listen("unix", p)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { ln.Close() })
		go func() {
			for {
				c, err := ln.Accept()
				if err != nil {
					return
				}
				c.Close()
			}
		}()
	}
	nodeSock, proxySock, edgeSock := filepath.Join(D, "n.sock"), filepath.Join(D, "extensions", "run", "notes", "p.sock"), filepath.Join(D, "run", "e.sock")
	sock(nodeSock)
	sock(proxySock)
	sock(edgeSock)
	work := filepath.Join(f.home, "work")
	if err := os.MkdirAll(work, 0o700); err != nil {
		t.Fatal(err)
	}
	profile, err := DarwinAgent(f.plan())
	if err != nil {
		t.Fatal(err)
	}
	q := shellQuote
	run := func(profile, script string) (string, error) {
		cmd := exec.Command("/usr/bin/sandbox-exec", "-p", profile, "/bin/sh", "-c", script)
		cmd.Dir = work
		cmd.Env = append(os.Environ(), "HOME="+f.home)
		out, err := cmd.CombinedOutput()
		return strings.TrimSpace(string(out)), err
	}
	mustFail := map[string]string{
		"cat secret.key":                     "cat " + q(D+"/secret.key"),
		"cat secret.key through .state-link": "cat " + q(alias+"/secret.key"),
		"cat secret.key through a link":      "cat " + q(elsewhere),
		"cat secret.key.bak":                 "cat " + q(D+"/secret.key.bak"),
		"cat handles.key":                    "cat " + q(D+"/handles.key"),
		"cat launch.key":                     "cat " + q(D+"/launch.key"),
		"cat hub.json":                       "cat " + q(D+"/hub.json"),
		"cat link.json":                      "cat " + q(D+"/link.json"),
		"cat HUB.JSON":                       "cat " + q(D+"/HUB.JSON"),
		"cat hub.json.bak":                   "cat " + q(D+"/hub.json.bak"),
		"cat .hub.json.12345":                "cat " + q(D+"/.hub.json.12345"),
		"cat link-tls/id.key":                "cat " + q(D+"/link-tls/id.key"),
		"cat tls/hub.key":                    "cat " + q(D+"/tls/hub.key"),
		"cat tls/.tls-1":                     "cat " + q(D+"/tls/.tls-1"),
		"cat managers/m1/manager.json":       "cat " + q(D+"/managers/m1/manager.json"),
		"cat extensions/data":                "cat " + q(D+"/extensions/data/notes/token"),
		"cat relay-tunnel.token":             "cat " + q(D+"/relay-tunnel.token"),
		"cat docker/db-password":             "cat " + q(D+"/docker/db-password"),
		"cat edge.token":                     "cat " + q(D+"/edge.token"),
		"cat another launcher's settings":    "cat " + q(f.theirs),
		"cp secret.key":                      "cp " + q(D+"/secret.key") + " " + q(work+"/k"),
		"mv the data directory":              "mv " + q(D) + " " + q(D+"2") + " && mv " + q(D+"2") + " " + q(D),
		"mv the folder above it":             "mv " + q(f.home) + " " + q(f.home+"2") + " && mv " + q(f.home+"2") + " " + q(f.home),
		"replace .state-link":                "rm " + q(alias) + " && ln -s .persona " + q(alias),
		"rm the switch":                      "rm " + q(D+"/"+AgentOnFile) + " && touch " + q(D+"/"+AgentOnFile),
		"dial an extension's proxy":          q(self) + " agent-dial " + q(proxySock),
		"dial the hub's edge socket":         q(self) + " agent-dial " + q(edgeSock),
		"python reads secret.key":            "python3 -c " + q("open('"+D+"/secret.key').read()"),
		"mv extensions/ and read its data":   "mv " + q(D+"/extensions") + " " + q(D+"/ext2") + " && cat " + q(D+"/ext2/data/notes/token") + " && mv " + q(D+"/ext2") + " " + q(D+"/extensions"),
	}
	mustWork := map[string]string{
		"cat mcp.json":              "cat " + q(D+"/mcp.json"),
		"cat through .state-link":   "cat " + q(alias+"/claude-settings.json"),
		"cat our launcher settings": "cat " + q(f.ours),
		"cat data/talk.key":         "cat " + q(D+"/data/talk.key"),
		"cat data/proj/link.json":   "cat " + q(D+"/data/proj/link.json"),
		"cat data/proj/edge.token":  "cat " + q(D+"/data/proj/edge.token"),
		"write assistant/":          "echo x > " + q(D+"/assistant/x") + " && cat " + q(D+"/assistant/x"),
		"write in the home":         "echo x > " + q(f.home+"/x") + " && mkdir -p " + q(f.home+"/d") + " && mv " + q(f.home+"/x") + " " + q(f.home+"/y"),
		"dial node.sock":            q(self) + " agent-dial " + q(nodeSock),
		"personad --version":        q(self) + " --version",
		"<D>/bin/personad":          q(bin) + " --version",
		"git":                       "git init -q . && git -c user.name=t -c user.email=t@example.com commit -q --allow-empty -m x && git log --oneline",
		"pgrep":                     "sleep 5 & p=$!; pgrep -x sleep >/dev/null; r=$?; kill $p; exit $r",
	}
	for name, script := range mustFail {
		if out, err := run(profile, script); err == nil {
			t.Errorf("%s: worked in the sandbox: %s", name, out)
		}
	}
	for name, script := range mustWork {
		if out, err := run(profile, script); err != nil {
			t.Errorf("%s: failed in the sandbox: %v %s", name, err, out)
		}
	}
	if out, err := run(profile, "/bin/ps -p $$"); err == nil {
		t.Errorf("ps started in the sandbox (setuid programs are expected not to): %s", out)
	}
	curl := "curl -sS -o /dev/null -m 15 https://example.com"
	if _, err := exec.Command("/bin/sh", "-c", curl).CombinedOutput(); err != nil {
		t.Logf("skip curl: no network outside the sandbox either (%v)", err)
	} else if out, err := run(profile, curl); err != nil {
		t.Errorf("curl: failed in the sandbox: %v %s", err, out)
	}
	// Control: with no rules each must-fail command works, so the failures above are the rules'.
	for name, script := range mustFail {
		if name == "python reads secret.key" {
			if _, err := exec.LookPath("python3"); err != nil {
				continue
			}
		}
		if out, err := run("(version 1)(allow default)", script); err != nil {
			t.Errorf("control %s: failed with no rules: %v %s", name, err, out)
		}
	}
	if _, err := os.Stat(filepath.Join(work, "k")); err != nil {
		t.Errorf("control cp: %v", err)
	}
}

func shellQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }
