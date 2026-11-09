package node

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/proto"
)

// A codexSessions pass resolves each rollout directory's symlinks, stats each rollout and finds the assistant's project
// once (codexListPass), where every thread used to do it again. Through a Codex home whose sessions directory is a
// symlink, also configured under an aliased path, the rows are the ones looking everything up per thread gives, an
// archived rollout stays hidden, and the pass resolved each directory only once.
func TestCodexListPassMatchesPerThreadLookups(t *testing.T) {
	n := remoteTestNode(t)
	home := n.codexHome()
	store := filepath.Join(t.TempDir(), "store")
	day := time.Now()
	dayDir := filepath.Join(store, day.Format("2006"), day.Format("01"), day.Format("02"))
	archived := filepath.Join(home, "archived_sessions")
	for _, dir := range []string{dayDir, archived} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(store, filepath.Join(home, "sessions")); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(t.TempDir(), "codex")
	if err := os.Symlink(home, alias); err != nil {
		t.Fatal(err)
	}
	n.configureCodex(proto.CodexConfig{Homes: []string{alias}})

	const (
		cli      = "0c000000-0000-4000-8000-000000000001"
		sub      = "0c000000-0000-4000-8000-000000000002"
		desktop  = "0c000000-0000-4000-8000-000000000003"
		program  = "0c000000-0000-4000-8000-000000000004"
		held     = "0c000000-0000-4000-8000-000000000005"
		archive  = "0c000000-0000-4000-8000-000000000006"
		cliMeta  = `"originator":"codex-tui","source":"cli"`
		stampFmt = "2006-01-02T15-04-05"
	)
	write := func(dir, id, meta, cwd string) {
		t.Helper()
		p := filepath.Join(dir, "rollout-"+day.Format(stampFmt)+"-"+id+".jsonl")
		line := `{"timestamp":"2026-01-01T00:00:00Z","type":"session_meta","payload":{"id":"` + id + `","cwd":"` + cwd + `",` + meta + `}}` + "\n"
		if err := os.WriteFile(p, []byte(line+event("task_started")+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write(dayDir, cli, cliMeta, "/work/cli")
	write(dayDir, sub, `"originator":"codex-tui","source":{"subagent":{"thread_spawn":{"parent_thread_id":"`+cli+`","depth":1}}}`, "/work/cli")
	write(dayDir, desktop, `"originator":"Codex Desktop","source":"vscode"`, "/work/app")
	write(dayDir, program, `"originator":"codex_exec","source":"exec"`, "/work/eval")
	write(dayDir, held, cliMeta, "/work/held")
	write(archived, archive, cliMeta, "/work/old")
	heldID := n.registerCodex(home, held, "") // its public id: the raw one only where home is the legacy home
	n.codices.mu.Lock()
	n.codices.liveRollouts = map[string]string{heldID: codexHolderCLI}
	n.codices.mu.Unlock()

	want := n.codexSessionsIn(map[string]bool{}, nil)
	pass := newCodexListPass()
	got := n.codexSessionsIn(map[string]bool{}, pass)
	gotJSON, _ := json.Marshal(got)
	wantJSON, _ := json.Marshal(want)
	if string(gotJSON) != string(wantJSON) {
		t.Fatalf("rows differ with the pass\n got %s\nwant %s", gotJSON, wantJSON)
	}
	listed := map[string]bool{}
	for _, s := range got {
		listed[s.SourceThreadID] = true
	}
	if len(got) != 4 || !listed[cli] || !listed[sub] || !listed[desktop] || !listed[held] {
		t.Fatalf("listed %v, want the CLI thread, its subagent, the App thread and the held one", listed)
	}
	if listed[archive] || listed[program] {
		t.Fatalf("listed %v: an archived rollout and a program's run stay hidden", listed)
	}
	if rolloutPath(home, archive) == "" {
		t.Fatal("the archived rollout must stay readable by id")
	}
	// Six rollouts in two directories: each directory resolved once, each rollout stat'ed once.
	if len(pass.real) != 2 || len(pass.stats) != 6 {
		t.Fatalf("the pass resolved %d directories and stat'ed %d rollouts, want 2 and 6", len(pass.real), len(pass.stats))
	}
}
