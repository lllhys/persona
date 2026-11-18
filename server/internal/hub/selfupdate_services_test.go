package hub

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/proto"
	"github.com/nwverse/persona/server/internal/update"
)

const oldProgram = "#!/bin/sh\necho old\n"

// serviceCase sets up what runs the programs beside target: the files it writes under the services' root (name ->
// content, "%s" standing for target's directory): user/ and etc-user/ are the user's systemd units (in that order of
// precedence), system/ the machine's, agents/ launchd property lists. unit is the user unit the hub runs in, self the
// program it runs as, state what systemd says of each unit ("on" or "off"; a unit not in it cannot be asked about).
type serviceCase struct {
	name     string
	programs []string // the release's
	files    map[string]string
	unit     string
	self     string // beside target; "" is personad
	state    map[string]string
	missing  string // the program a service runs that the release lacks; "" when it installs
}

type serviceHub struct {
	*Hub
	machine *Machine
	target  string
	dir     string // the services' root
	execs   func() []string
}

func (c serviceCase) hub(t *testing.T) serviceHub {
	t.Helper()
	h, m, target, execs := updatingHub(t, splitRelease(t, c.programs...), "release")
	h.build.Commit = gitA
	bin := filepath.Dir(target)
	for _, name := range update.SplitPrograms {
		if err := os.WriteFile(filepath.Join(bin, name), []byte(oldProgram), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	dir := t.TempDir()
	for name, content := range c.files {
		writeService(t, dir, name, strings.ReplaceAll(content, "%s", bin))
	}
	self := target
	if c.self != "" {
		self = filepath.Join(bin, c.self)
	}
	h.upd.self = func() (string, error) { return self, nil }
	f := serviceFinder{
		scopes: []unitScope{
			{user: true, dirs: []string{filepath.Join(dir, "user"), filepath.Join(dir, "etc-user")}},
			{dirs: []string{filepath.Join(dir, "system")}},
		},
		plists: []string{filepath.Join(dir, "agents")},
		home:   t.TempDir(),
		unit:   func() (string, bool) { return c.unit, true },
	}
	if c.state != nil {
		f.state = func(user bool, name string) (bool, bool) { s, ok := c.state[name]; return s == "on", ok }
	}
	h.upd.services = f
	return serviceHub{Hub: h, machine: m, target: target, dir: dir, execs: execs}
}

func writeService(t *testing.T, dir, name, content string) {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

const agentRunning = `<?xml version="1.0" encoding="UTF-8"?>
<plist version="1.0"><dict>
  <key>Label</key><string>com.persona.manager.assistant</string>
  <key>ProgramArguments</key><array><string>%s/persona-manager</string><string>assistant</string></array>
</dict></plist>
`

const unitRunningHub = "[Unit]\nDescription=hub\n\n[Service]\nExecStart=\"%s/persona-hub\" run\n"

func TestUpdateServiceProbeDoesNotBlockStatusOrReserveStaleCandidate(t *testing.T) {
	h := serviceCase{programs: []string{"persona-manager"}, files: map[string]string{"user/old.service": unitRunningHub}}.hub(t)
	u := h.upd
	if _, err := u.check(context.Background()); err != nil {
		t.Fatal(err)
	}
	probing, release := make(chan struct{}), make(chan struct{})
	u.services.state = func(bool, string) (bool, bool) { close(probing); <-release; return false, true }
	done := make(chan error, 1)
	go func() { done <- u.begin(context.Background(), false) }()
	<-probing
	viewed := make(chan struct{})
	go func() {
		u.view()
		u.mu.Lock()
		u.release = nil // A settings/source change invalidates the probed object.
		u.mu.Unlock()
		close(viewed)
	}()
	select {
	case <-viewed:
	case <-time.After(time.Second):
		close(release)
		<-done
		t.Fatal("service probe held the update mutex and blocked status")
	}
	close(release)
	if err := <-done; err != errUpdateBusy {
		t.Fatalf("stale candidate accepted: %v", err)
	}
	if u.view().State == "downloading" || len(h.execs()) != 0 {
		t.Fatal("stale candidate started installing")
	}
}

// A release without persona-hub or persona-manager for this platform is not installed while a service runs that
// program: it would go on starting the old build beside the new personad. The app shows why, the history says it
// once, and installing it by hand is refused for the same reason; nothing is replaced.
func TestUpdateRefusedWhileAServiceRunsAProgramItLacks(t *testing.T) {
	for _, c := range []serviceCase{
		{name: "the hub's unit runs persona-hub", programs: []string{"persona-manager"}, missing: "persona-hub",
			files: map[string]string{"user/personad.service": unitRunningHub}},
		{name: "a drop-in makes the unit run persona-hub", programs: []string{"persona-manager"}, missing: "persona-hub",
			files: map[string]string{
				"user/personad.service":                          "[Service]\nExecStart=%s/personad hub\n",
				"user/personad.service.d/90-persona-compat.conf": "[Service]\nExecStart=\nExecStart=%s/persona-hub run\n",
			}},
		{name: "a drop-in from another directory of the scope", programs: []string{"persona-manager"}, missing: "persona-hub",
			files: map[string]string{
				"user/personad.service":                 "[Service]\nExecStart=%s/personad hub\n",
				"etc-user/personad.service.d/50-x.conf": "[Service]\nExecStart=\nExecStart=%s/persona-hub run\n",
			}},
		{name: "spaces around the key's =", programs: []string{"persona-manager"}, missing: "persona-hub",
			files: map[string]string{"user/personad.service": "[Service]\nExecStart = '%s/persona-hub' run\n"}},
		{name: "a machine's unit", programs: []string{"persona-manager"}, missing: "persona-hub",
			files: map[string]string{"system/persona.service": unitRunningHub}},
		{name: "a unit systemd cannot be asked about", programs: []string{"persona-manager"}, missing: "persona-hub",
			files: map[string]string{"user/personad.service": unitRunningHub}, state: map[string]string{"other.service": "off"}},
		{name: "an enabled unit", programs: []string{"persona-manager"}, missing: "persona-hub",
			files: map[string]string{"user/personad.service": unitRunningHub}, state: map[string]string{"personad.service": "on"}},
		{name: "an agent runs persona-manager", programs: []string{"persona-hub"}, missing: "persona-manager",
			files: map[string]string{"agents/com.persona.manager.assistant.plist": agentRunning}},
		{name: "an agent's Program is persona-manager", programs: []string{"persona-hub"}, missing: "persona-manager",
			files: map[string]string{"agents/a.plist": strings.Replace(agentRunning, "<key>ProgramArguments</key><array><string>%s/persona-manager",
				"<key>Program</key><string>%s/persona-manager</string><key>ProgramArguments</key><array><string>%s/personad", 1)}},
		// switched to persona-hub by hand (hub switch --binary) inside a unit that runs personad
		{name: "the hub runs as persona-hub in its unit", programs: []string{"persona-manager"}, missing: "persona-hub",
			self: "persona-hub", unit: "personad.service", files: map[string]string{"user/personad.service": "[Service]\nExecStart=\"%s/personad\" hub\n"},
			state: map[string]string{"personad.service": "on"}},
	} {
		t.Run(c.name, func(t *testing.T) {
			h := c.hub(t)
			ctx := context.Background()
			if _, err := h.upd.check(ctx); err != nil {
				t.Fatal(err)
			}
			if v := h.upd.view(); v.Latest == nil || !strings.Contains(v.Latest.Held, c.missing) {
				t.Fatalf("latest %+v", v.Latest)
			}
			if h.upd.wantsAuto() {
				t.Fatal("would install it by itself")
			}
			h.upd.noteHeld(ctx)
			h.upd.noteHeld(ctx)
			if hist := h.upd.settings.History; len(hist) != 1 || hist[0].Result != "held" || !strings.Contains(hist[0].Reason, c.missing) {
				t.Fatalf("history %+v", hist)
			}
			if err := h.upd.begin(ctx, true); err == nil || !strings.Contains(err.Error(), c.missing) {
				t.Fatalf("installing it by hand: %v", err)
			}
			h.upd.install(ctx, true) // the hourly check's, had the services changed since it read the release
			if hist := h.upd.settings.History; len(hist) != 2 || hist[1].Result != "failed" || !strings.Contains(hist[1].Reason, c.missing) {
				t.Fatalf("history %+v", hist)
			}
			nothingReplaced(t, h)
		})
	}
}

// nothingReplaced checks that personad and the programs beside it are the old build and nothing restarted.
func nothingReplaced(t *testing.T, h serviceHub) {
	t.Helper()
	if b, _ := os.ReadFile(h.target); string(b) != oldProgram || len(h.execs()) != 0 {
		t.Fatalf("personad replaced or restarted (%v)", h.execs())
	}
	for _, name := range update.SplitPrograms {
		if b, _ := os.ReadFile(filepath.Join(filepath.Dir(h.target), name)); string(b) != oldProgram {
			t.Fatalf("%s replaced", name)
		}
	}
}

// When no service runs the program the release lacks, it installs as before: personad and the programs it has are
// replaced, the one it lacks keeps its old file, and the hub goes on as personad.
func TestUpdateLeavesAProgramNoServiceRuns(t *testing.T) {
	for _, c := range []serviceCase{
		{name: "the hub's unit runs personad", programs: []string{"persona-manager"}, unit: "personad.service",
			files: map[string]string{
				"user/personad.service": "[Service]\nExecStart=\"%s/personad\" hub\n",
				"user/other.service":    "[Service]\nExecStart=/bin/true\n",
				// persona-hub named outside [Service], and by a unit a drop-in took back to personad
				"user/notes.service":                    "[Unit]\nExecStart=%s/persona-hub run\n",
				"user/back.service":                     "[Service]\nExecStart=%s/persona-hub run\n",
				"user/back.service.d/override.conf":     "[Service]\nExecStart=\nExecStart=%s/personad hub\n",
				"agents/com.persona.hub.plist":          strings.ReplaceAll(agentRunning, "persona-manager", "personad"),
				"user/persona-hub.service.d/notes.conf": "[Service]\nExecStart=%s/persona-hub run\n",
			}},
		{name: "persona-hub started by hand", programs: []string{"persona-manager"}, self: "persona-hub"},
		{name: "persona-hub started by hand in another service", programs: []string{"persona-manager"}, self: "persona-hub",
			unit: "tmux.service", files: map[string]string{"user/tmux.service": "[Service]\nExecStart=/usr/bin/tmux new-session -d\n"}},
		{name: "persona-hub started by systemd-run", programs: []string{"persona-manager"}, self: "persona-hub", unit: "run-u12.service"},
		{name: "the machine's unit is shadowed by the user's own", programs: []string{"persona-manager"},
			files: map[string]string{
				"user/personad.service":     "[Service]\nExecStart=\"%s/personad\" hub\n",
				"etc-user/personad.service": unitRunningHub,
			}},
		{name: "a unit systemd does not run", programs: []string{"persona-manager"},
			files: map[string]string{"user/old-hub.service": unitRunningHub}, state: map[string]string{"old-hub.service": "off"}},
		{name: "a disabled agent", programs: []string{"persona-manager"},
			files: map[string]string{"agents/a.plist": strings.Replace(strings.ReplaceAll(agentRunning, "persona-manager<", "persona-hub<"),
				"</dict>", "<key>Disabled</key><true/></dict>", 1)}},
		{name: "an agent runs persona-manager, the release has it", programs: []string{"persona-manager"},
			files: map[string]string{"agents/com.persona.manager.assistant.plist": agentRunning}},
	} {
		t.Run(c.name, func(t *testing.T) {
			h := c.hub(t)
			ctx := context.Background()
			if _, err := h.upd.check(ctx); err != nil {
				t.Fatal(err)
			}
			if v := h.upd.view(); v.Latest == nil || v.Latest.Held != "" || !h.upd.wantsAuto() {
				t.Fatalf("latest %+v, auto %v", v.Latest, h.upd.wantsAuto())
			}
			go h.upd.install(ctx, true)
			waitFor(t, "the restart", func() bool { return len(h.execs()) == 1 })
			if c.self == "" && h.execs()[0] != h.target {
				t.Fatalf("restarted into %v, not personad", h.execs())
			}
			bin := filepath.Dir(h.target)
			if b, _ := os.ReadFile(filepath.Join(bin, "persona-hub")); string(b) != oldProgram {
				t.Fatal("persona-hub, which the release lacks, was replaced")
			}
			if b, _ := os.ReadFile(filepath.Join(bin, "persona-manager")); string(b) == oldProgram {
				t.Fatal("persona-manager was not replaced")
			}
		})
	}
}

// The update looks again just before it replaces anything: a unit pointed at persona-hub while it waited for the agents
// to finish holds it back too.
func TestUpdateLooksAgainBeforeTheSwap(t *testing.T) {
	h := serviceCase{programs: []string{"persona-manager"}}.hub(t)
	setSessions(h.machine, proto.Session{SessionID: "s1", Status: proto.StatusWorking})
	ctx := context.Background()
	if _, err := h.upd.check(ctx); err != nil {
		t.Fatal(err)
	}
	if err := h.upd.begin(ctx, false); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "waiting for idle", func() bool { return h.upd.view().State == "waiting_idle" })
	writeService(t, h.dir, "user/personad.service", strings.ReplaceAll(unitRunningHub, "%s", filepath.Dir(h.target)))
	setSessions(h.machine, proto.Session{SessionID: "s1", Status: proto.StatusIdle})
	// The failed state is published before its history is recorded. Wait for both, and read the history under its lock.
	var hist []updateRecord
	waitFor(t, "the failed update's history", func() bool {
		h.upd.mu.Lock()
		defer h.upd.mu.Unlock()
		if h.upd.state != "failed" || len(h.upd.settings.History) == 0 {
			return false
		}
		hist = slices.Clone(h.upd.settings.History)
		return true
	})
	if len(hist) != 1 || !strings.Contains(hist[0].Reason, "persona-hub") {
		t.Fatalf("history %+v", hist)
	}
	nothingReplaced(t, h)
}

// The program of a unit's ExecStart, read as systemd does. hub-systemd.sh reads the same table the same way
// (scripts/test/test_hub_switch_deploy.py).
func TestExecProgram(t *testing.T) {
	b, err := os.ReadFile("testdata/exec_start.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct{ Line, Program string }
	if err := json.Unmarshal(b, &cases); err != nil {
		t.Fatal(err)
	}
	const home = "/home/u"
	for _, c := range cases {
		unit := filepath.Join(t.TempDir(), "a.service")
		if err := os.WriteFile(unit, []byte("[Service]\n"+c.Line+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		got := ""
		if p := unitPrograms(unit, nil, home); len(p) > 0 {
			got = p[len(p)-1]
		}
		if want := strings.ReplaceAll(c.Program, "<home>", home); got != want {
			t.Errorf("%s: %q, want %q", c.Line, got, want)
		}
	}
}

// The service the hub runs in, from its cgroup: a hub started by hand (a login session, a terminal) runs in none.
func TestCgroupUnit(t *testing.T) {
	for cgroup, want := range map[string]struct {
		name string
		user bool
	}{
		"0::/user.slice/user-1000.slice/user@1000.service/app.slice/personad.service\n":         {"personad.service", true},
		"12:pids:/system.slice/persona.service\n1:name=systemd:/system.slice/persona.service\n": {"persona.service", false},
		"0::/user.slice/user-1000.slice/session-3.scope\n":                                      {},
		"0::/user.slice/user-1000.slice/user@1000.service/app.slice/vte-spawn-1.scope\n":        {},
		"0::/user.slice/user-1000.slice/user@1000.service\n":                                    {},
		"0::/\n": {},
	} {
		if name, user := cgroupUnit(cgroup); name != want.name || user != want.user {
			t.Errorf("%q: %q %v, want %+v", cgroup, name, user, want)
		}
	}
}

// What systemd says of a unit: it counts when it is enabled (or started without being asked for) or active.
func TestUnitOn(t *testing.T) {
	for show, want := range map[string][2]bool{
		"UnitFileState=enabled\nActiveState=inactive\n":  {true, true},
		"ActiveState=active\nUnitFileState=disabled\n":   {true, true},
		"UnitFileState=static\nActiveState=inactive\n":   {true, true},
		"UnitFileState=disabled\nActiveState=inactive\n": {false, true},
		"UnitFileState=masked\nActiveState=inactive\n":   {false, true},
		"UnitFileState=\nActiveState=inactive\n":         {false, true}, // not found
		"":                                               {false, false},
		"Failed to connect to bus: No medium found\n":    {false, false},
	} {
		if on, known := unitOn(show); on != want[0] || known != want[1] {
			t.Errorf("%q: on %v known %v, want %v", show, on, known, want)
		}
	}
}

// A binary property list is read through plutil, as a Mac has it (a stand-in here).
func TestReadBinaryPlist(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "a.plist")
	if err := os.WriteFile(file, []byte("bplist00\x00\x01"), 0o644); err != nil {
		t.Fatal(err)
	}
	xml := filepath.Join(dir, "a.xml")
	if err := os.WriteFile(xml, []byte(strings.ReplaceAll(agentRunning, "%s", "/x")), 0o644); err != nil {
		t.Fatal(err)
	}
	tools := filepath.Join(dir, "tools")
	writeService(t, tools, "plutil", "#!/bin/sh\n[ \"$*\" = \"-convert xml1 -o - "+file+"\" ] && exec cat "+xml+"\nexit 1\n")
	if err := os.Chmod(filepath.Join(tools, "plutil"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", tools+string(os.PathListSeparator)+os.Getenv("PATH"))
	job, ok := readLaunchdJob(file)
	if !ok || !slices.Equal(job.args, []string{"/x/persona-manager", "assistant"}) {
		t.Fatalf("%+v %v", job, ok)
	}
}
