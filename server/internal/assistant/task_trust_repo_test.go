package assistant

import (
	"context"
	"encoding/json"
	"errors"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/nwverse/persona/server/internal/apierr"
	"github.com/nwverse/persona/server/internal/db"
	"github.com/nwverse/persona/server/internal/proto"
)

// repoMachine has the fake's machines answer worktree.status as a computer with two repositories: /code/app with its
// worktrees under /wt/app, and /code/other with its worktrees under /wt/other. Any other folder is in no repository.
// offline makes every machine unreachable while it is true.
func repoMachine(f *fakeHub, offline *bool) {
	f.answerNode("worktree.status", func(_ string, raw json.RawMessage) (any, error) {
		if offline != nil && *offline {
			return nil, apierr.ErrOffline
		}
		var in struct{ Cwd string }
		_ = json.Unmarshal(raw, &in)
		switch {
		case in.Cwd == "/code/app" || in.Cwd == "/code/other":
			return proto.WorktreeStatus{Path: in.Cwd, Branch: "main"}, nil
		case strings.HasPrefix(in.Cwd, "/code/app/"): // a folder inside the main checkout
			return proto.WorktreeStatus{Path: "/code/app", Branch: "main"}, nil
		case strings.HasPrefix(in.Cwd, "/wt/app/"):
			return proto.WorktreeStatus{Worktree: true, Path: in.Cwd, Root: "/code/app", Branch: "fix"}, nil
		case strings.HasPrefix(in.Cwd, "/wt/other/"):
			return proto.WorktreeStatus{Worktree: true, Path: in.Cwd, Root: "/code/other", Branch: "fix"}, nil
		}
		return proto.WorktreeStatus{}, nil
	})
}

// resolvedTask is a task on box in cwd, with its repository as box says.
func resolvedTask(s *Service, cwd string, readonly bool) PlanTask {
	t := PlanTask{Machine: "box", Cwd: cwd, Readonly: readonly}
	s.resolveTaskRepo(context.Background(), &t)
	return t
}

// Trusting a task in one worktree of a repository lets a task in any other worktree of it, and its main checkout, start
// at once; another repository, another computer and a read-only kind are other kinds (#8259).
func TestTaskTrustCoversEveryWorktreeOfTheRepository(t *testing.T) {
	ctx := context.Background()
	st := &memStore{}
	s, f := newTaskServiceOn(t, st)
	f.sessions["mac"] = nil
	repoMachine(f, nil)

	fixA := resolvedTask(s, "/wt/app/fix-a", false)
	if fixA.Repo != "/code/app" {
		t.Fatalf("repository of a linked worktree = %q, want its main checkout", fixA.Repo)
	}
	s.trustKinds(ctx, []PlanTask{fixA})
	if got := storedTrust(t, st); !slices.Equal(got, []string{"box|/code/app|edit"}) {
		t.Fatalf("tasks.trust = %v, want the repository's kind", got)
	}
	for _, cwd := range []string{"/wt/app/perf-b", "/code/app", "/code/app/server"} {
		if task := resolvedTask(s, cwd, false); !s.planStartsAtOnce(ctx, []PlanTask{task}) {
			t.Errorf("a task in %s (repository %q) still asks", cwd, task.Repo)
		}
	}
	for _, cwd := range []string{"/code/other", "/wt/other/fix-a", "/tmp/notes"} {
		if task := resolvedTask(s, cwd, false); s.planStartsAtOnce(ctx, []PlanTask{task}) {
			t.Errorf("a task in %s (repository %q) starts without asking", cwd, task.Repo)
		}
	}
	elsewhere := PlanTask{Machine: "mac", Cwd: "/wt/app/perf-b"}
	s.resolveTaskRepo(ctx, &elsewhere)
	if elsewhere.Repo != "/code/app" || s.planStartsAtOnce(ctx, []PlanTask{elsewhere}) {
		t.Errorf("the same repository on another computer starts without asking: %+v", elsewhere)
	}

	// Reading and editing stay apart: the trusted edit kind is not the read kind, and a trusted read kind does not
	// let an edit start.
	trusted := s.trustCover(ctx)
	if readB := resolvedTask(s, "/wt/app/perf-b", true); trustedTask(trusted, readB) {
		t.Errorf("the edit kind covers reading: %q", trustKey(readB))
	}
	readOther := resolvedTask(s, "/wt/other/fix-a", true)
	s.trustKinds(ctx, []PlanTask{readOther})
	if !slices.Contains(storedTrust(t, st), "box|/code/other|read") {
		t.Fatalf("tasks.trust = %v, want the other repository's read kind", storedTrust(t, st))
	}
	if editOther := resolvedTask(s, "/wt/other/perf-b", false); s.planStartsAtOnce(ctx, []PlanTask{editOther}) {
		t.Error("a trusted read kind lets an edit start without asking")
	}
	if !trustedTask(s.trustCover(ctx), resolvedTask(s, "/wt/other/perf-b", true)) {
		t.Error("the read kind does not cover the repository's other worktree")
	}
}

// A key remembered before #8259 names an exact folder: it still lets that folder start at once, and is not widened to
// the rest of the repository.
func TestTaskTrustExactFolderKeysStillMatch(t *testing.T) {
	ctx := context.Background()
	st := &memStore{}
	s, f := newTaskServiceOn(t, st)
	repoMachine(f, nil)
	if err := st.PutSetting(ctx, trustSetting, []string{"box|/wt/app/fix-a|edit", "box|~/code/other|edit"}); err != nil {
		t.Fatal(err)
	}
	if task := resolvedTask(s, "/wt/app/fix-a", false); task.Repo != "/code/app" || !s.planStartsAtOnce(ctx, []PlanTask{task}) {
		t.Fatalf("an old exact-folder key no longer matches its folder: %+v", task)
	}
	if task := resolvedTask(s, "/wt/app/perf-b", false); s.planStartsAtOnce(ctx, []PlanTask{task}) {
		t.Fatal("an old exact-folder key was widened to the repository's other worktrees")
	}
	// The folder as it was spelled when remembered, whatever repository the machine names for it now.
	other := PlanTask{Machine: "box", Cwd: "~/code/other", Repo: "/code/other"}
	if !s.planStartsAtOnce(ctx, []PlanTask{other}) {
		t.Fatal("an old key spelled with ~ no longer matches")
	}
}

// When the machine cannot say which repository a folder is in (offline, an older node, an error), the kind is the
// exact folder: what is remembered then covers only it, and a remembered repository does not reach a worktree whose
// repository is unknown. Never looser than before #8259.
func TestTaskTrustUnresolvedRepositoryFallsBackToTheFolder(t *testing.T) {
	ctx := context.Background()
	st := &memStore{}
	s, f := newTaskServiceOn(t, st)
	offline := true
	repoMachine(f, &offline)

	fixA := resolvedTask(s, "/wt/app/fix-a", false)
	if fixA.Repo != "" {
		t.Fatalf("an offline machine named repository %q", fixA.Repo)
	}
	s.trustKinds(ctx, []PlanTask{fixA})
	if got := storedTrust(t, st); !slices.Equal(got, []string{"box|/wt/app/fix-a|edit"}) {
		t.Fatalf("tasks.trust = %v, want the exact folder", got)
	}
	if !s.planStartsAtOnce(ctx, []PlanTask{resolvedTask(s, "/wt/app/fix-a", false)}) {
		t.Fatal("the folder remembered while offline does not start at once")
	}
	if s.planStartsAtOnce(ctx, []PlanTask{resolvedTask(s, "/wt/app/perf-b", false)}) {
		t.Fatal("a sibling worktree starts without asking on a folder kind")
	}
	offline = false
	if s.planStartsAtOnce(ctx, []PlanTask{resolvedTask(s, "/wt/app/perf-b", false)}) {
		t.Fatal("a folder kind remembered offline was widened to the repository once the machine came back")
	}

	// The repository trusted, the machine offline again: its worktrees ask, its main checkout (that exact folder) does not.
	s.trustKinds(ctx, []PlanTask{resolvedTask(s, "/wt/app/perf-b", false)})
	offline = true
	if s.planStartsAtOnce(ctx, []PlanTask{resolvedTask(s, "/wt/app/next", false)}) {
		t.Fatal("a worktree whose repository is unknown starts on the repository's kind")
	}
	if !s.planStartsAtOnce(ctx, []PlanTask{resolvedTask(s, "/code/app", false)}) {
		t.Fatal("the repository's main checkout asks while its repository is unknown")
	}

	// A node that fails or names no absolute folder: the folder again.
	f.answerNode("worktree.status", func(string, json.RawMessage) (any, error) { return nil, errors.New("unknown method") })
	if task := resolvedTask(s, "/wt/app/next", false); task.Repo != "" {
		t.Fatalf("an older node named repository %q", task.Repo)
	}
	f.answerNode("worktree.status", func(string, json.RawMessage) (any, error) {
		return proto.WorktreeStatus{Worktree: true, Path: "/wt/app/next", Root: "code/app"}, nil
	})
	if task := resolvedTask(s, "/wt/app/next", false); task.Repo != "" {
		t.Fatalf("a relative root became repository %q", task.Repo)
	}
}

var planIDPattern = regexp.MustCompile(`plan_[0-9a-f]+`)

// End to end through task_plan and the card's remember box: the plan resolves each task's repository itself (what the
// tool call says is ignored), Start keeps the repository's kind, and the next plan in another worktree of it starts at
// once while one in another repository waits for Start. Start re-resolves after the card moved the task.
func TestTaskPlanRememberCoversTheRepositoryAcrossWorktrees(t *testing.T) {
	ctx := context.Background()
	st := &profileTaskStore{memStore: &memStore{}, plans: map[string]db.TaskPlanRow{}}
	s, f := newTaskServiceOn(t, st)
	repoMachine(f, nil)
	plan := func(cwd string, extra map[string]any) string {
		t.Helper()
		task := map[string]any{"title": "修登录崩溃", "prompt": "修好并补测试", "machine": "box", "cwd": cwd,
			"permission": "bypassPermissions", "why": "这个仓库在 box 上"}
		for k, v := range extra {
			task[k] = v
		}
		raw, _ := json.Marshal(map[string]any{"tasks": []any{task}})
		out, err := s.toolTaskPlan(ctx, "side1", raw)
		if err != nil {
			t.Fatal(err)
		}
		return out
	}
	starts := func() int {
		f.mu.Lock()
		defer f.mu.Unlock()
		return len(f.starts)
	}

	out := plan("/wt/app/fix-a", map[string]any{"repo": "/code/other"})
	id := planIDPattern.FindString(out)
	if !strings.Contains(out, "nothing has started") || id == "" {
		t.Fatalf("an untrusted writable task did not wait for Start: %s", out)
	}
	p, err := s.loadPlan(ctx, id)
	if err != nil || p.Tasks[0].Repo != "/code/app" {
		t.Fatalf("plan's repository = %+v (%v), want the machine's answer, not the tool call's", p, err)
	}
	if _, err := s.startPlan(ctx, id, nil, true); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the first task's agent", func() bool { return starts() == 1 })
	if got := storedTrust(t, st.memStore); !slices.Equal(got, []string{"box|/code/app|edit"}) {
		t.Fatalf("tasks.trust = %v, want the repository's kind", got)
	}

	if out := plan("/wt/app/perf-b", nil); !strings.Contains(out, "without asking") {
		t.Fatalf("a task in another worktree of the trusted repository waited for Start: %s", out)
	}
	waitFor(t, "the second task's agent", func() bool { return starts() == 2 })

	out = plan("/wt/other/fix-a", nil)
	id = planIDPattern.FindString(out)
	if !strings.Contains(out, "nothing has started") || id == "" {
		t.Fatalf("a task in another repository started without asking: %s", out)
	}
	// The card moves it into the trusted repository: Start names that repository, not the one it was planned in.
	moved, err := s.startPlan(ctx, id, []PlanTask{{Cwd: "/wt/app/third"}}, false)
	if err != nil {
		t.Fatal(err)
	}
	if moved.Tasks[0].Repo != "/code/app" {
		t.Fatalf("Start kept repository %q after the card moved the task", moved.Tasks[0].Repo)
	}
	waitFor(t, "the third task's agent", func() bool { return starts() == 3 })
}

// repoPlan makes a one-task plan on box in cwd through task_plan (extra adds or overrides the task's fields), and
// returns its id and what the tool said.
func repoPlan(t *testing.T, s *Service, cwd string, extra map[string]any) (id, out string) {
	t.Helper()
	task := map[string]any{"title": "修登录崩溃", "prompt": "修好并补测试", "machine": "box", "cwd": cwd,
		"permission": "bypassPermissions", "why": "这个仓库在 box 上"}
	for k, v := range extra {
		task[k] = v
	}
	raw, _ := json.Marshal(map[string]any{"tasks": []any{task}})
	out, err := s.toolTaskPlan(context.Background(), "side1", raw)
	if err != nil {
		t.Fatal(err)
	}
	return planIDPattern.FindString(out), out
}

func fakeStarts(f *fakeHub) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.starts)
}

// A plan made in repository A whose card picks an existing workspace in repository B: Start with "start these
// directly" remembers B, the repository that runs, and saves it with the admitted cwd; A stays a kind that asks.
func TestTaskTrustRemembersTheRepositoryOfTheExistingWorkspaceThatRuns(t *testing.T) {
	ctx := context.Background()
	st := &profileTaskStore{memStore: &memStore{}, plans: map[string]db.TaskPlanRow{}}
	s, f := newTaskServiceOn(t, st)
	repoMachine(f, nil)

	id, out := repoPlan(t, s, "/wt/app/fix-a", nil)
	if id == "" || !strings.Contains(out, "nothing has started") {
		t.Fatalf("an untrusted writable task did not wait for Start: %s", out)
	}
	p, err := s.startPlan(ctx, id, []PlanTask{{Workspace: &TaskWorkspace{Mode: workspaceExisting, Existing: "/wt/other/fix-b"}}}, true)
	if err != nil {
		t.Fatal(err)
	}
	if p.Tasks[0].Repo != "/code/other" {
		t.Fatalf("Start named repository %q for a task that runs in /wt/other/fix-b", p.Tasks[0].Repo)
	}
	waitFor(t, "the task's agent", func() bool { return fakeStarts(f) == 1 })
	if got := storedTrust(t, st.memStore); !slices.Equal(got, []string{"box|/code/other|edit"}) {
		t.Fatalf("tasks.trust = %v, want only the repository that runs (/code/other)", got)
	}
	saved, err := s.loadPlan(ctx, id)
	if err != nil || saved.Tasks[0].Cwd != "/wt/other/fix-b" || saved.Tasks[0].Repo != "/code/other" {
		t.Fatalf("saved task = %+v (%v), want the admitted cwd with its repository", saved.Tasks[0], err)
	}

	if _, out := repoPlan(t, s, "/wt/app/next", nil); !strings.Contains(out, "nothing has started") {
		t.Fatalf("the repository the plan was made in, which did not run, starts without asking: %s", out)
	}
	if _, out := repoPlan(t, s, "/wt/other/next", nil); !strings.Contains(out, "without asking") {
		t.Fatalf("a task in the repository that ran still waits for Start: %s", out)
	}
	// The other way round: trusted B does not let a plan made in B that picks a workspace in A start at once.
	if _, out := repoPlan(t, s, "/wt/other/third", map[string]any{"workspace": map[string]any{"existing": "/wt/app/third"}}); !strings.Contains(out, "nothing has started") {
		t.Fatalf("a plan made in the trusted repository but running in another started without asking: %s", out)
	}
}

// A new checkout the engine makes somewhere else than the plan's repository: what is remembered is the repository the
// checkout is in, or, when the machine names none for it, that exact folder; never the repository the plan came from.
func TestTaskTrustRemembersTheRepositoryOfTheCheckoutMade(t *testing.T) {
	for _, tc := range []struct{ name, made, want string }{
		{"another repository", "/wt/other/made-x", "box|/code/other|edit"},
		{"no repository named", "/scratch/made-y", "box|/scratch/made-y|edit"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			st := &profileTaskStore{memStore: &memStore{}, plans: map[string]db.TaskPlanRow{}}
			s, f := newTaskServiceOn(t, st)
			repoMachine(f, nil)
			f.answerNode(DeviceWorktreeCreate, func(string, json.RawMessage) (any, error) {
				return proto.WorktreeCreateResult{WorkspaceID: "ws-made", Path: tc.made, Branch: "task/made"}, nil
			})

			id, out := repoPlan(t, s, "/wt/app/fix-a", map[string]any{"workspace": map[string]any{"new": map[string]any{"branch": "task/made"}}})
			if id == "" || !strings.Contains(out, "nothing has started") {
				t.Fatalf("an untrusted writable task did not wait for Start: %s", out)
			}
			if _, err := s.startPlan(ctx, id, nil, true); err != nil {
				t.Fatal(err)
			}
			waitFor(t, "the task's agent", func() bool { return fakeStarts(f) == 1 })
			if got := storedTrust(t, st.memStore); !slices.Equal(got, []string{tc.want}) {
				t.Fatalf("tasks.trust = %v, want only %s", got, tc.want)
			}
			saved, err := s.loadPlan(ctx, id)
			if err != nil || saved.Tasks[0].Cwd != tc.made || trustKey(saved.Tasks[0]) != tc.want {
				t.Fatalf("saved task = %+v (%v), want the checkout made with its kind %s", saved.Tasks[0], err, tc.want)
			}
			if _, out := repoPlan(t, s, "/wt/app/next", nil); !strings.Contains(out, "nothing has started") {
				t.Fatalf("the repository the plan was made in, which did not run, starts without asking: %s", out)
			}
		})
	}
}
