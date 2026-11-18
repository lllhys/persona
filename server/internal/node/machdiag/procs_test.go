package machdiag

import (
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/proto"
)

func TestParseProcStat(t *testing.T) {
	// A name may hold spaces and parentheses: it ends at the last ')'.
	b := []byte("4242 (tmux: server) S 1 4242 4242 0 -1 4194560 3000 0 0 0 150 50 0 0 20 0 1 0 900 20971520 2048 18446744073709551615 1 1 0 0 0 0 0 4096 134433283 0 0 0 17 3 0 0 0 0 0\n")
	st, ok := parseProcStat(b)
	if !ok || st.name != "tmux: server" || st.ticks != 200 || st.rssPages != 2048 {
		t.Fatalf("stat = %+v %v", st, ok)
	}
	st, ok = parseProcStat([]byte("7 (a) b)) R 1 2 3 4 5 6 7 8 9 10 11 12 13 14 15 16 17 18 19 20 21 22\n"))
	if !ok || st.name != "a) b)" {
		t.Fatalf("parenthesised name = %+v %v", st, ok)
	}
	for _, bad := range []string{"", "1 no parens", "1 (short) S 1 2", "1 (x) S 1 2 3 4 5 6 7 8 9 10 u s 13 14 15 16 17 18 19 20 21 22"} {
		if _, ok := parseProcStat([]byte(bad)); ok {
			t.Errorf("%q parsed", bad)
		}
	}
}

func TestParsePS(t *testing.T) {
	out := []byte(`    1  12345   0.3 /sbin/launchd
  501 204800  12.5 /Users/someone/.local/bin/claude
  502 102400   1,5 /Applications/Google Chrome.app/Contents/Frameworks/Google Chrome Helper (Renderer).app/Contents/MacOS/Google Chrome Helper (Renderer)
  503   2048   0.0 -zsh
  bad line
  504 x 1.0 thing
`)
	got := parsePS(out)
	want := []struct {
		name string
		rss  int64
		cpu  float64
	}{
		{"launchd", 12345 << 10, 0.3},
		{"claude", 204800 << 10, 12.5},
		{"Google Chrome Helper (Renderer)", 102400 << 10, 1.5},
		{"zsh", 2048 << 10, 0},
	}
	if len(got) != len(want) {
		t.Fatalf("got %d processes: %+v", len(got), got)
	}
	for i, w := range want {
		if got[i].name != w.name || got[i].rss != w.rss || got[i].cpu == nil || *got[i].cpu != w.cpu {
			t.Errorf("%d: %+v (cpu %v), want %+v", i, got[i], *got[i].cpu, w)
		}
	}
}

func cpu(v float64) *float64 { return &v }

func TestReportGroupsByNameAndKind(t *testing.T) {
	procs := []process{
		{name: "claude", rss: 100, cpu: cpu(10)},
		{name: "claude", rss: 200, cpu: cpu(2.5)},
		{name: "herdr", rss: 50},
		{name: "personad", rss: 40, cpu: cpu(0)},
		{name: "node", rss: 300, cpu: cpu(1)},
		{name: "Orca", rss: 10},
		{name: "kworker/0:1", rss: 0}, // a kernel thread: counted, not grouped
	}
	r := report(procs)
	if r.Count != len(procs) {
		t.Fatalf("count = %d", r.Count)
	}
	names := []string{}
	for _, g := range r.Groups {
		names = append(names, g.Name)
	}
	if !slices.Equal(names, []string{"claude", "node", "herdr", "personad", "Orca"}) {
		t.Fatalf("groups by RSS = %v", names)
	}
	claude := r.Groups[0]
	if claude.Count != 2 || claude.RSS != 300 || claude.Kind != proto.ProcessKindAgent || claude.CPU == nil || *claude.CPU != 12.5 {
		t.Fatalf("claude = %+v (cpu %v)", claude, *claude.CPU)
	}
	kinds := map[string]string{}
	for _, g := range r.Groups {
		kinds[g.Name] = g.Kind
	}
	// node cannot be attributed by name; Orca matches its engine case-insensitively.
	if kinds["node"] != proto.ProcessKindOther || kinds["herdr"] != proto.ProcessKindEngine || kinds["personad"] != proto.ProcessKindPersona || kinds["Orca"] != proto.ProcessKindEngine {
		t.Fatalf("kinds = %v", kinds)
	}
	if r.Groups[2].CPU != nil {
		t.Fatal("a group with no CPU reading has a CPU")
	}
}

// The largest 15 by memory, plus any of the busiest 5 by CPU that are not among them, sorted by memory.
func TestReportKeepsTopByMemoryAndByCPU(t *testing.T) {
	var procs []process
	for i := range 30 {
		procs = append(procs, process{name: fmt.Sprintf("p%02d", i), rss: int64(1000 - i), cpu: cpu(0)})
	}
	// Two small but busy groups, beyond the memory cut.
	procs = append(procs, process{name: "busy-a", rss: 1, cpu: cpu(90)}, process{name: "busy-b", rss: 2, cpu: cpu(80)})
	r := report(procs)
	if len(r.Groups) != topByRSS+2 {
		t.Fatalf("%d groups", len(r.Groups))
	}
	if r.Groups[0].Name != "p00" || r.Groups[topByRSS-1].Name != "p14" {
		t.Fatalf("memory order: %s … %s", r.Groups[0].Name, r.Groups[topByRSS-1].Name)
	}
	if r.Groups[topByRSS].Name != "busy-b" || r.Groups[topByRSS+1].Name != "busy-a" {
		t.Fatalf("busy groups: %+v", r.Groups[topByRSS:])
	}
	// Idle groups are never added for CPU.
	idle := report(procs[:20])
	if len(idle.Groups) != topByRSS {
		t.Fatalf("idle: %d groups", len(idle.Groups))
	}
	if empty := report(nil); empty.Groups == nil || len(empty.Groups) != 0 {
		t.Fatalf("no processes: %+v", empty)
	}
}

func TestSampleCPU(t *testing.T) {
	first := map[int]procStat{1: {name: "go", ticks: 100, rssPages: 1}, 2: {name: "old", ticks: 5, rssPages: 1}}
	second := map[int]procStat{
		1: {name: "go", ticks: 150, rssPages: 2}, // 50 ticks in half a second: one core fully
		2: {name: "new", ticks: 9, rssPages: 1},  // the pid was reused: no reading
		3: {name: "born", ticks: 1, rssPages: 1}, // started in between
	}
	got := map[string]process{}
	for _, p := range sampleCPU(first, second, 500*time.Millisecond, 4096) {
		got[p.name] = p
	}
	if p := got["go"]; p.rss != 8192 || p.cpu == nil || *p.cpu != 100 {
		t.Fatalf("go = %+v", p)
	}
	if got["new"].cpu != nil || got["born"].cpu != nil {
		t.Fatalf("unmeasured processes have CPU: %+v %+v", got["new"], got["born"])
	}
}
