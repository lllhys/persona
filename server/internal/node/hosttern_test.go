package node

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/host"
	"github.com/nwverse/persona/server/internal/proto"
	"github.com/nwverse/persona/server/internal/terncli"
)

// fakeTern answers the Tern host's tern commands from canned layouts (by window) and processes (by block), and
// records every call.
type fakeTern struct {
	mu      sync.Mutex
	inspect map[string]string
	procs   map[string]string
	capture string
	errs    map[string]error // by the command's first word
	calls   [][]string
}

func (f *fakeTern) client() *terncli.Client {
	return &terncli.Client{Bin: func() string { return "tern" }, Run: f.run}
}

func (f *fakeTern) run(_ context.Context, _ string, args ...string) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, args)
	if args[0] == "--version" {
		return []byte("tern 0.9.13 (8c6dfbd)\n"), nil
	}
	if err := f.errs[args[0]]; err != nil {
		return nil, err
	}
	window, rest := args[2], args[3:]
	switch args[0] {
	case "inspect":
		return []byte(f.inspect[window]), nil
	case "process":
		if p, ok := f.procs[rest[0]]; ok {
			return []byte(p), nil
		}
		return nil, fmt.Errorf("exit status 1: tern process: no block is called `%s`", rest[0])
	case "capture":
		return []byte(f.capture), nil
	case "new":
		return []byte(`{"session":7,"tab":8,"block":9}`), nil
	}
	return nil, nil
}

// called are the calls whose first word is cmd.
func (f *fakeTern) called(cmd string) [][]string {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out [][]string
	for _, c := range f.calls {
		if c[0] == cmd {
			out = append(out, c)
		}
	}
	return out
}

// ternLayout: the first window's session with a claude, a browser block, a shell, a codex behind node, classic
// herdr's client and a dead block; a second window's own session, and the first one again.
var ternLayout = map[string]string{
	"": `{"sessions":[{"id":4,"name":"Default","shown":true,"tabs":[
		{"id":10,"name":null,"number":1,"blocks":[
			{"id":11,"program":"/bin/zsh","cwd":"/home/me/app","title":"✳ Fix the build","live":true,"browser":null},
			{"id":12,"program":"","live":false,"browser":"https://example.com"}]},
		{"id":20,"name":"api","number":2,"blocks":[
			{"id":21,"program":"/bin/zsh","cwd":"/home/me/api","title":"zsh","live":true,"browser":null},
			{"id":22,"program":"/bin/zsh","cwd":"/home/me/api","title":"codex","live":true,"browser":null},
			{"id":23,"program":"/bin/zsh","cwd":"/home/me","title":"herdr","live":true,"browser":null},
			{"id":24,"program":"/bin/zsh","cwd":"/home/me","live":false,"browser":null}]}]}],
		"clients":[{"kind":"window","window":""},{"kind":"window","window":"w2"},{"kind":"cli","window":""}]}`,
	"w2": `{"sessions":[{"id":5,"name":"notes","tabs":[{"id":30,"number":1,"blocks":[
			{"id":31,"program":"/bin/zsh","cwd":"/home/me/notes","live":true,"browser":null}]}]},
		{"id":4,"name":"Default","tabs":[]}],"clients":[]}`,
}

var ternProcs = map[string]string{
	"11": `{"pane":11,"child":{"pid":100,"name":"zsh","argv":["/bin/zsh","-l"]},
		"foreground":{"pid":101,"name":"2.1.316","argv":["claude","--model","x"],"cwd":"/home/me/app"}}`,
	"21": `{"pane":21,"child":{"pid":200,"name":"zsh","argv":["/bin/zsh","-l"]},"foreground":{"pid":200,"name":"zsh","argv":["/bin/zsh","-l"]}}`,
	"22": `{"pane":22,"foreground":{"pid":301,"name":"node","argv":["node","/usr/lib/node_modules/@openai/codex/bin/codex.js"]}}`,
	"23": `{"pane":23,"foreground":{"pid":401,"name":"herdr","argv":["herdr"]}}`,
	"31": `{"pane":31,"foreground":{"pid":501,"name":"codex","argv":["codex"]}}`,
}

func TestTernSnapshot(t *testing.T) {
	f := &fakeTern{inspect: ternLayout, procs: ternProcs}
	h := newTernHost(f.client())
	l, err := h.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var spaces, slots, panes []string
	for _, s := range l.Spaces {
		spaces = append(spaces, fmt.Sprintf("%s:%s:%d", s.ID, s.Label, s.Number))
	}
	for _, s := range l.Slots {
		slots = append(slots, fmt.Sprintf("%s/%s:%s:%d", s.SpaceID, s.ID, s.Label, s.Number))
	}
	for _, p := range l.Panes {
		panes = append(panes, p.PaneID+"="+p.Agent)
	}
	// Session 4 counts once (the second window shows it too); the browser and the dead block are no panes; a shell
	// and herdr's client run no agent of Tern's.
	if !slices.Equal(spaces, []string{"4:Default:1", "5:notes:2"}) ||
		!slices.Equal(slots, []string{"4/10:1:1", "4/20:api:2", "5/30:1:1"}) ||
		!slices.Equal(panes, []string{"tern:11=claude", "tern:21=", "tern:22=codex", "tern:23=", "tern:31=codex"}) {
		t.Fatalf("spaces %q\nslots %q\npanes %q", spaces, slots, panes)
	}
	if len(l.Agents) != 3 {
		t.Fatalf("agents %+v", l.Agents)
	}
	a := l.Agents[0]
	if a.Title != "Fix the build" || a.RawTitle != "✳ Fix the build" || a.WorkspaceID != "4" || a.TabID != "10" ||
		a.Cwd != "/home/me/app" || a.ForegroundCwd != "/home/me/app" || a.AgentStatus != proto.StatusUnknown {
		t.Errorf("claude pane %+v", a)
	}
	// A block of the second window is addressed in it.
	if err := h.SendKeys(context.Background(), host.PaneRef{Kind: host.Tern, ID: "31"}, "enter"); err != nil {
		t.Fatal(err)
	}
	if got := f.called("send"); len(got) != 1 || !slices.Equal(got[0], []string{"send", "--window", "w2", "31", "keys", "enter"}) {
		t.Errorf("send %q", got)
	}
	if pids, err := h.PaneProcesses(context.Background(), host.PaneRef{Kind: host.Tern, ID: "11"}); err != nil || !slices.Equal(pids, []int{101, 100}) {
		t.Errorf("processes %v %v", pids, err)
	}
}

func TestTernNotRunning(t *testing.T) {
	f := &fakeTern{errs: map[string]error{"inspect": fmt.Errorf("exit status 1: tern inspect: no Tern is running (no session daemon)")}}
	if _, err := newTernHost(f.client()).Snapshot(context.Background()); !errors.Is(err, host.ErrNotRunning) {
		t.Errorf("snapshot: %v", err)
	}
}

func TestTernRead(t *testing.T) {
	f := &fakeTern{capture: "one\n\x1b[31mred\x1b[0m\n\n  \n\x1b[0m\n"}
	h := newTernHost(f.client())
	pane := host.PaneRef{Kind: host.Tern, ID: "11"}
	// Colors: rows end in \r\n; the blank rows at the bottom are dropped.
	got, err := h.Read(context.Background(), pane, host.ReadOpts{Source: "visible", ANSI: true})
	if err != nil || got != "one\r\n\x1b[31mred\x1b[0m\r\n" {
		t.Errorf("ansi read %q %v", got, err)
	}
	got, _ = h.Read(context.Background(), pane, host.ReadOpts{Source: "recent", Lines: 1})
	if got != "\x1b[31mred\x1b[0m\n" {
		t.Errorf("last line %q", got)
	}
	c := f.called("capture")
	if len(c) != 2 || !slices.Contains(c[0], "--ansi") || slices.Contains(c[0], "--scrollback") ||
		slices.Contains(c[1], "--ansi") || !slices.Contains(c[1], "--scrollback") {
		t.Errorf("captures %q", c)
	}
}

func TestTernPrompt(t *testing.T) {
	f := &fakeTern{inspect: ternLayout, procs: ternProcs}
	h := newTernHost(f.client())
	ctx := context.Background()
	// A shell in front: the text would run as a command, so nothing is typed.
	if err := h.Prompt(ctx, host.PaneRef{Kind: host.Tern, ID: "21"}, "fix it"); !errors.Is(err, host.ErrNotDelivered) {
		t.Fatalf("prompt to a shell: %v", err)
	}
	if got := f.called("send"); len(got) != 0 {
		t.Fatalf("typed into a shell: %q", got)
	}
	if err := h.Prompt(ctx, host.PaneRef{Kind: host.Tern, ID: "11"}, "fix it\x1b[201~"); err != nil {
		t.Fatal(err)
	}
	got := f.called("send")
	if len(got) != 2 || got[0][4] != "paste" || strings.Contains(got[0][5], "\x1b") || !strings.HasPrefix(got[0][5], "fix it") ||
		!slices.Equal(got[1], []string{"send", "--window", "", "11", "keys", "enter"}) {
		t.Errorf("sends %q", got)
	}
}

func TestTernStartRenameClose(t *testing.T) {
	f := &fakeTern{inspect: ternLayout, procs: ternProcs}
	h := newTernHost(f.client())
	ctx := context.Background()
	cr, err := h.CreateSpace(ctx, "/home/me/app", "")
	if err != nil || cr.SpaceID != "7" || cr.SlotID != "8" || cr.Pane != (host.PaneRef{Kind: host.Tern, ID: "9"}) {
		t.Fatalf("space %+v %v", cr, err)
	}
	if _, err := h.CreateSlot(ctx, "4", "/home/me", "build"); err != nil {
		t.Fatal(err)
	}
	if err := h.RenameSlot(ctx, "20", "backend"); err != nil { // through the tab's first block
		t.Fatal(err)
	}
	if err := h.RenameSpace(ctx, "4", "work"); err != nil {
		t.Fatal(err)
	}
	if got := f.called("new"); len(got) != 2 ||
		!slices.Equal(got[0], []string{"new", "--window", "", "session", "app", "--cwd", "/home/me/app", "--json"}) ||
		!slices.Equal(got[1], []string{"new", "--window", "", "tab", "4", "--cwd", "/home/me", "--json"}) {
		t.Errorf("new %q", got)
	}
	if got := f.called("rename"); len(got) != 3 || !slices.Equal(got[0][3:], []string{"9", "build"}) ||
		!slices.Equal(got[1][3:], []string{"21", "backend"}) || !slices.Equal(got[2][3:], []string{"4", "work"}) {
		t.Errorf("renames %q", got)
	}
	// Closing the last block of a session no window shows ends the session; one a window shows stays.
	for _, shown := range []bool{false, true} {
		f.inspect = map[string]string{"": fmt.Sprintf(`{"sessions":[{"id":7,"name":"app","shown":%v,"tabs":[]}],"clients":[]}`, shown)}
		f.calls = nil
		if err := h.Close(ctx, host.PaneRef{Kind: host.Tern, ID: "9"}, "7", "8"); err != nil {
			t.Fatal(err)
		}
		kills := f.called("kill")
		if shown && len(kills) != 0 || !shown && (len(kills) != 1 || !slices.Equal(kills[0], []string{"kill", "--window", "", "session", "7"})) {
			t.Errorf("shown=%v: kills %q", shown, kills)
		}
	}
}

// node.info: Tern is available when its binary is there and its daemon answers; the version is the binary's.
func TestTernEngine(t *testing.T) {
	ctx := context.Background()
	if e := ternEngine(ctx, &terncli.Client{Bin: func() string { return "" }}); e != (proto.Engine{ID: "tern", Name: "Tern"}) {
		t.Errorf("not installed: %+v", e)
	}
	running := &fakeTern{inspect: map[string]string{"": `{"sessions":[]}`}}
	if e := ternEngine(ctx, running.client()); !e.Available || e.Version != "0.9.13" {
		t.Errorf("running: %+v", e)
	}
	stopped := &fakeTern{errs: map[string]error{"inspect": fmt.Errorf("exit status 1: no Tern is running")}}
	if e := ternEngine(ctx, stopped.client()); e.Available || e.Version != "0.9.13" {
		t.Errorf("stopped: %+v", e)
	}
}

// renameHost lists spaces and records the ones it renames.
type renameHost struct {
	bareHost
	spaces  []string
	renamed []string
}

func (h *renameHost) Spaces(context.Context) ([]host.Space, error) {
	var out []host.Space
	for _, id := range h.spaces {
		out = append(out, host.Space{ID: id})
	}
	return out, nil
}
func (h *renameHost) RenameSpace(_ context.Context, space, name string) error {
	h.renamed = append(h.renamed, space+"="+name)
	return nil
}
func (h *renameHost) RenameSlot(context.Context, string, string) error          { return nil }
func (h *renameHost) Close(context.Context, host.PaneRef, string, string) error { return nil }

// Beside herdr (the default host), a Tern session is renamed in Tern, and a herdr workspace still in herdr.
func TestRenameSpaceInTheHostListingIt(t *testing.T) {
	n, _ := testNode(t)
	hh := &renameHost{bareHost: bareHost{kind: host.Herdr}, spaces: []string{"w1"}}
	f := &fakeTern{inspect: ternLayout, procs: ternProcs}
	n.hosts = host.NewRegistry(hh, newTernHost(f.client()))
	c := proto.EngineChoice{Hosts: []string{"herdr", "tern"}, Default: "herdr"}
	n.eng.name, n.eng.set = proto.LegacyEngine(c), &c
	for _, id := range []string{"4", "w1"} {
		if _, err := request(t, n, "space.rename", map[string]any{"workspace_id": id, "name": "work"}); err != nil {
			t.Fatalf("%s: %v", id, err)
		}
	}
	if got := f.called("rename"); len(got) != 1 || !slices.Equal(got[0], []string{"rename", "--window", "", "4", "work"}) {
		t.Errorf("tern renames %q", got)
	}
	if !slices.Equal(hh.renamed, []string{"w1=work"}) {
		t.Errorf("herdr renames %q", hh.renamed)
	}
}

// A node driving Tern lists the claude in block 11 as its session (by the pid's sessions file), in Tern's session and
// tab, and sends to it with a paste and an enter after a colored look at its screen.
func TestTernSessionListedAndSent(t *testing.T) {
	withSubmitGrace(t, 50*time.Millisecond)
	n, _, home := herdrNode(t)
	f := &fakeTern{inspect: ternLayout, procs: ternProcs, capture: screenFixture(t, "claude-2.1.312-empty-162.ansi")}
	n.hosts = host.NewRegistry(newTernHost(f.client()))
	n.eng.name = proto.EngineTern
	writeTranscript(t, home, "/home/me/app", sidA, time.Now().Add(-time.Minute), `{"type":"custom-title","customTitle":"x"}`)
	writeClaudePID(t, home, 101, sidA)
	s := byID(listSessions(t, n))[sidA]
	if s.PaneID != "tern:11" || s.Host != "tern" || s.SpaceKind != "tern" || s.SpaceID != "4" || s.SlotID != "10" {
		t.Fatalf("listed %+v", s)
	}
	if _, err := request(t, n, "session.send", map[string]any{"session_id": sidA, "text": "hello"}); err != nil {
		t.Fatal(err)
	}
	sends := f.called("send")
	if len(sends) < 2 || sends[0][3] != "11" || sends[0][4] != "paste" || sends[0][5] != "hello" ||
		!slices.Equal(sends[len(sends)-1][3:], []string{"11", "keys", "enter"}) {
		t.Errorf("sends %q", sends)
	}
}

// A claude in a Tern block names its block in TERN_PANE: while Tern lists a claude in that pane it is Tern's
// (claudebg.go claudeInPane), not also an outside session of an ordinary terminal.
func TestClaudeInATernBlockIsTerns(t *testing.T) {
	n, _, _ := herdrNode(t)
	old := paneEnvOfPID
	paneEnvOfPID = func(int) map[string]string { return map[string]string{ternPaneEnv: "42"} }
	t.Cleanup(func() { paneEnvOfPID = old })
	if got := n.enginePaneRefs(paneEnvOfPID(7)); !slices.Equal(got, []string{"tern:42"}) {
		t.Fatalf("pane refs %v", got)
	}
	n.mu.Lock()
	n.hostAgents = map[string][]host.Agent{host.Tern: {{Agent: "claude", PaneID: "tern:42"}}}
	n.mu.Unlock()
	if !n.claudeInPane(claudeProc{pid: 7, Entrypoint: "cli"}) {
		t.Error("a claude in a block Tern lists is not Tern's")
	}
}
