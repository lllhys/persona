package assistant

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/db"
	"github.com/nwverse/persona/server/internal/prompts"
)

// scopeFixture is a hub whose main conversation is "main", with leader "lead" and background supervisor "bg":
// conversation "other" handed out 14 open tasks (newer than anything else), the user has a personal to-do, and
// conversation "side" handed out one task that ended two hours ago and one still open (the oldest of all), and
// supervises a task "other" handed out. Three tasks wait for the user to verify: one of side's, two of other's.
func scopeFixture(t *testing.T) (*Service, *fakeHub) {
	t.Helper()
	f := newFakeHub()
	f.settings = &Settings{Main: "main", Leaders: []string{"lead"}, BackgroundSupervisor: "bg"}
	s := newStorelessService(t, f)
	now := time.Now()
	ended := now.Add(-2 * time.Hour)
	for i := range 14 {
		at := now.Add(-time.Duration(i) * time.Minute)
		putTodo(s, Todo{ID: fmt.Sprintf("todo_other_%02d", i), Title: fmt.Sprintf("other task %02d", i), Origin: "other",
			CreatedAt: at, UpdatedAt: at, Supervision: &Supervision{Supervisor: "other"}})
	}
	old := now.Add(-48 * time.Hour)
	putTodo(s, Todo{ID: "todo_personal", Title: "personal errand", CreatedAt: old, UpdatedAt: old})
	putTodo(s, Todo{ID: "todo_side_done", Title: "side finished task", Origin: "side", CreatedAt: old, UpdatedAt: ended,
		DoneAt: &ended, DoneBy: db.DoneByAuto, Supervision: &Supervision{Supervisor: "side"}})
	putTodo(s, Todo{ID: "todo_side_open", Title: "side open task", Origin: "side", CreatedAt: old.Add(-time.Hour),
		UpdatedAt: old, Supervision: &Supervision{Supervisor: "side"}})
	putTodo(s, Todo{ID: "todo_side_watch", Title: "side supervises this", Origin: "other", CreatedAt: old, UpdatedAt: old,
		Supervision: &Supervision{Supervisor: "side"}})
	putTodo(s, Todo{ID: "todo_lead_own", Title: "leader hands out", Origin: "lead", CreatedAt: old, UpdatedAt: old,
		Supervision: &Supervision{Supervisor: "lead"}})
	putTodo(s, Todo{ID: "todo_lead_watch", Title: "leader supervises", Origin: "main", CreatedAt: old, UpdatedAt: old,
		Supervision: &Supervision{Supervisor: "lead"}})
	b := &s.accept
	b.mu.Lock()
	if !s.loadAcceptLocked(context.Background()) {
		b.mu.Unlock()
		t.Fatal("acceptance book not loaded")
	}
	for i, id := range []string{"todo_other_00", "todo_side_open", "todo_other_01"} {
		b.asks[id] = &acceptAsk{Changed: "changed " + id, AskedAt: now.Add(time.Duration(i-10) * time.Hour)}
	}
	b.mu.Unlock()
	return s, f
}

func scopedContext(t *testing.T, s *Service, id string) string {
	t.Helper()
	return s.Context(context.Background(), ContextParams{SessionID: id}).Text
}

// A side conversation's snapshot lists only the tasks it handed out or supervises, and what of them waits for the
// user to verify: the user asked it how to verify "this", and the model took one of another conversation's tasks
// from the snapshot for it (#8666).
func TestSnapshotSideConversationSeesOnlyItsOwnTasks(t *testing.T) {
	s, _ := scopeFixture(t)
	got := scopedContext(t, s, "side")
	for _, want := range []string{"- 这个对话派出或督办的任务（", "todo_side_done", "todo_side_open", "todo_side_watch",
		"- 这个对话的任务里等用户验收的（", "changed todo_side_open"} {
		if !strings.Contains(got, want) {
			t.Errorf("side conversation's snapshot lacks %q:\n%s", want, got)
		}
	}
	for _, leak := range []string{"todo_other_", "todo_personal", "todo_lead_", "changed todo_other", "优先显示的开放任务详情", "开放待办索引"} {
		if strings.Contains(got, leak) {
			t.Errorf("side conversation's snapshot shows %q:\n%s", leak, got)
		}
	}
	if !strings.Contains(got, "- 开放待办：共 19 件（完整列表用 todo_list）") {
		t.Errorf("the count of every open to-do and its todo_list entrance are gone:\n%s", got)
	}
}

// The first rows are taken after the filter: a side conversation's oldest open task and its task that ended hours ago
// stay in its list however many newer tasks other conversations have (#8666).
func TestSnapshotOwnTaskIsNotCrowdedOutByOtherConversations(t *testing.T) {
	s, _ := scopeFixture(t)
	snap := scopeSnapshot(prompts.Snapshot{Tasks: s.snapshotTasks(context.Background(), "")}, "")
	for _, task := range snap.Tasks {
		if strings.HasPrefix(task.ID, "todo_side_") {
			t.Fatalf("the fixture does not crowd side's tasks out of the whole list: %s is in it", task.ID)
		}
	}
	scoped := scopeSnapshot(prompts.Snapshot{Tasks: s.snapshotTasks(context.Background(), "")}, "side")
	var ids []string
	for _, task := range scoped.Tasks {
		ids = append(ids, task.ID)
	}
	if got := strings.Join(ids, ","); got != "todo_side_watch,todo_side_open,todo_side_done" {
		t.Fatalf("side conversation's tasks = %s, want its two open ones (newest first) then the one that ended", got)
	}
}

// The main conversation still gets every task, in the words it always had (the golden snapshots pin them).
func TestSnapshotMainConversationStillSeesEveryTask(t *testing.T) {
	s, _ := scopeFixture(t)
	got := scopedContext(t, s, "main")
	for _, want := range []string{"- 优先显示的开放任务详情（", "- 开放待办索引（", "todo_personal", "todo_side_open",
		"- 等用户验收的（", "changed todo_other_00", "changed todo_side_open", "changed todo_other_01"} {
		if !strings.Contains(got, want) {
			t.Errorf("main conversation's snapshot lacks %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "这个对话") {
		t.Errorf("main conversation's snapshot got a side conversation's words:\n%s", got)
	}
	snap := scopeSnapshot(prompts.Snapshot{Tasks: s.snapshotTasks(context.Background(), "")}, "")
	if len(snap.Tasks) != snapshotTaskLimit {
		t.Fatalf("main conversation's task details = %d, want %d", len(snap.Tasks), snapshotTaskLimit)
	}
}

// A leader sees the tasks it handed out and the ones it supervises (the main conversation handed them to it), and
// none of the others.
func TestSnapshotLeaderSeesWhatItHandsOutAndSupervises(t *testing.T) {
	s, _ := scopeFixture(t)
	got := scopedContext(t, s, "lead")
	for _, want := range []string{"todo_lead_own", "todo_lead_watch", "- 这个对话派出或督办的任务（"} {
		if !strings.Contains(got, want) {
			t.Errorf("leader's snapshot lacks %q:\n%s", want, got)
		}
	}
	for _, leak := range []string{"todo_other_", "todo_side_", "todo_personal", "等用户验收的"} {
		if strings.Contains(got, leak) {
			t.Errorf("leader's snapshot shows %q:\n%s", leak, got)
		}
	}
}

// A new conversation that handed out nothing says so in its own task line, instead of carrying the others' tasks.
func TestSnapshotNewConversationHasNoTasks(t *testing.T) {
	s, _ := scopeFixture(t)
	got := scopedContext(t, s, "fresh")
	if !strings.Contains(got, "要它新查、新做或改方向才用 agent_send）：没有\n") {
		t.Errorf("no empty task line for a new conversation:\n%s", got)
	}
	if strings.Contains(got, "todo_other") || strings.Contains(got, "todo_side") || strings.Contains(got, "todo_personal") ||
		strings.Contains(got, "等用户验收的") {
		t.Errorf("a new conversation's snapshot lists tasks:\n%s", got)
	}
}

// The background supervisor follows the main conversation's tasks, and a scheduled task's own conversation may brief
// on all of them: both keep the whole list, as does any conversation while there is no main one.
func TestSnapshotScopeKeepsTheWholeListForTheMainConversationsStandIns(t *testing.T) {
	s, _ := scopeFixture(t)
	ctx := context.Background()
	sc, err := s.lockedSchedule(ctx)
	if err != nil {
		t.Fatal(err)
	}
	sc.tasks = append(sc.tasks, Task{ID: "digest", RunSessionID: "routine"})
	sc.mu.Unlock()
	for id, want := range map[string]string{"main": "", "bg": "", "routine": "", "": "", "side": "side", "lead": "lead"} {
		if got := s.snapshotScope(ctx, id); got != want {
			t.Errorf("scope of %q = %q, want %q", id, got, want)
		}
	}
	f := newFakeHub()
	f.settings = &Settings{Leaders: []string{"lead"}}
	if got := newStorelessService(t, f).snapshotScope(ctx, "side"); got != "" {
		t.Errorf("with no main conversation the scope of side is %q, want every task", got)
	}
}

// Scoping leaves the cached lists as they were (other messages render them at the same time), filters the tasks
// waiting for the user's nod too, and lists in a scoped index only the open tasks its details do not show.
func TestScopeSnapshotFiltersEveryTaskListAndCopies(t *testing.T) {
	mine, theirs := prompts.SnapshotOwner{Origin: "side"}, prompts.SnapshotOwner{Origin: "other"}
	snap := prompts.Snapshot{
		Tasks:          []prompts.SnapshotTask{{ID: "a", Owner: theirs}, {ID: "b", Owner: mine}},
		OpenTodos:      []prompts.SnapshotOpenTodo{{ID: "a", Owner: theirs}, {ID: "b", Owner: mine}, {ID: "c", Owner: mine}, {ID: "p"}},
		OpenTodosTotal: 4,
		Approvals:      []prompts.SnapshotApproval{{ID: "a", Owner: theirs}, {ID: "b", Owner: mine}},
		Unaccepted:     []prompts.SnapshotAccept{{Title: "a", Owner: theirs}, {Title: "b", Owner: mine}}, UnacceptedTotal: 2,
	}
	got := scopeSnapshot(snap, "side")
	if !got.Scoped || len(got.Tasks) != 1 || got.Tasks[0].ID != "b" || len(got.Approvals) != 1 || got.Approvals[0].ID != "b" ||
		len(got.Unaccepted) != 1 || got.UnacceptedTotal != 1 || len(got.OpenTodos) != 1 || got.OpenTodos[0].ID != "c" ||
		got.OpenTodosTotal != 4 {
		t.Fatalf("scoped snapshot = %+v", got)
	}
	if snap.Tasks[0].ID != "a" || snap.OpenTodos[0].ID != "a" || snap.Approvals[0].ID != "a" || snap.Unaccepted[0].Title != "a" {
		t.Fatalf("scoping changed the cached lists: %+v", snap)
	}
	if all := scopeSnapshot(snap, ""); all.Scoped || len(all.Tasks) != 2 || len(all.OpenTodos) != 4 || all.UnacceptedTotal != 2 {
		t.Fatalf("unscoped snapshot = %+v", all)
	}
}

// The side conversation's task lines, pinned: its own heading, the task rows as the main conversation has them, the
// open ones the details do not show, and the verify and approve lines marked as its tasks'; and the empty line.
func TestGoldenSnapshotScoped(t *testing.T) {
	now := time.Date(2026, 11, 16, 0, 52, 0, 0, time.UTC)
	golden(t, "assistant_snapshot.scoped", prompts.RenderSnapshot(prompts.Snapshot{Now: now, Zone: "UTC", Scoped: true,
		OpenTodosLoaded: true, OpenTodosTotal: 19,
		Tasks: []prompts.SnapshotTask{{ID: "todo_1", Title: "示例任务", State: "已完成", Health: "已验收", DoneWhen: 3,
			Machine: "mac", Session: "abcd1234", Last: "做完了"}},
		OpenTodos:  []prompts.SnapshotOpenTodo{{ID: "todo_2", Title: "另一件"}},
		Approvals:  []prompts.SnapshotApproval{{ID: "todo_3", Title: "等批准的", Stage: "停在 PR", Gate: "等用户批准合并"}},
		Unaccepted: []prompts.SnapshotAccept{{Title: "示例任务", Verdict: "做完了", Since: now}}, UnacceptedTotal: 1,
	}))
	golden(t, "assistant_snapshot.scoped_none", prompts.RenderSnapshot(prompts.Snapshot{Now: now, Zone: "UTC", Scoped: true,
		OpenTodosLoaded: true, OpenTodosTotal: 19}))
}

// A personal to-do has no origin and no supervisor, so a scoped conversation never matches it: with only personal
// to-dos open, a side conversation's every task list and its compact index are empty, its rendered snapshot says it
// has no tasks and shows neither the personal row nor the index heading, and the main conversation still lists it.
func TestScopeSnapshotLeavesOutPersonalTodos(t *testing.T) {
	index := prompts.Snapshot{OpenTodos: []prompts.SnapshotOpenTodo{{ID: "todo_personal"}}, OpenTodosTotal: 1, OpenTodosLoaded: true}
	if got := scopeSnapshot(index, "side"); len(got.OpenTodos) != 0 {
		t.Fatalf("a side conversation's index keeps a personal to-do: %+v", got.OpenTodos)
	} else if text := prompts.RenderSnapshot(got); strings.Contains(text, "todo_personal") || strings.Contains(text, "其他开放任务") {
		t.Fatalf("a side conversation's rendered index shows a personal to-do:\n%s", text)
	}
	personal := prompts.Snapshot{
		Tasks:          []prompts.SnapshotTask{{ID: "todo_personal"}},
		OpenTodos:      []prompts.SnapshotOpenTodo{{ID: "todo_personal"}},
		OpenTodosTotal: 1,
		Approvals:      []prompts.SnapshotApproval{{ID: "todo_personal"}},
		Unaccepted:     []prompts.SnapshotAccept{{Title: "todo_personal"}}, UnacceptedTotal: 1,
	}
	got := scopeSnapshot(personal, "side")
	if len(got.Tasks) != 0 || len(got.OpenTodos) != 0 || len(got.Approvals) != 0 || len(got.Unaccepted) != 0 ||
		got.UnacceptedTotal != 0 || got.OpenTodosTotal != 1 {
		t.Fatalf("a side conversation's snapshot keeps a personal to-do: %+v", got)
	}

	f := newFakeHub()
	f.settings = &Settings{Main: "main"}
	s := newStorelessService(t, f)
	old := time.Now().Add(-48 * time.Hour)
	putTodo(s, Todo{ID: "todo_personal", Title: "personal errand", CreatedAt: old, UpdatedAt: old})
	side := scopedContext(t, s, "side")
	if !strings.Contains(side, "要它新查、新做或改方向才用 agent_send）：没有\n") {
		t.Errorf("no empty task line for a side conversation with only a personal to-do open:\n%s", side)
	}
	for _, leak := range []string{"todo_personal", "personal errand", "其他开放任务", "开放待办索引"} {
		if strings.Contains(side, leak) {
			t.Errorf("side conversation's snapshot shows %q:\n%s", leak, side)
		}
	}
	if !strings.Contains(side, "- 开放待办：共 1 件（完整列表用 todo_list）") {
		t.Errorf("the count of every open to-do and its todo_list entrance are gone:\n%s", side)
	}
	if main := scopedContext(t, s, "main"); !strings.Contains(main, "todo_personal") {
		t.Errorf("main conversation's snapshot lost the personal to-do:\n%s", main)
	}
}
