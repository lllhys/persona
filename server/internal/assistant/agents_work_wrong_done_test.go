package assistant

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/db"
)

// statsOf is GET /api/v1/agents/work/stats over the last hour, once the events written in the background are in.
func statsOf(t *testing.T, s *Service) workStats {
	t.Helper()
	s.waitWorkEvents()
	w := serve(s, "GET", "/api/v1/agents/work/stats?hours=1", "")
	if w.Code != 200 {
		t.Fatalf("stats %d %s", w.Code, w.Body)
	}
	var out workStats
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	return out
}

// storedEvents are the events st took; testEvents those a service without a database kept.
func storedEvents(st *workMemStore) []db.WorkEvent {
	st.wmu.Lock()
	defer st.wmu.Unlock()
	return append([]db.WorkEvent(nil), st.events...)
}

func testEvents(s *Service) []db.WorkEvent {
	s.work.mu.Lock()
	defer s.work.mu.Unlock()
	return append([]db.WorkEvent(nil), s.work.testEvents...)
}

// eventsOf are the details of events of kind, by to-do.
func eventsOf(events []db.WorkEvent, kind string) map[string][]map[string]any {
	out := map[string][]map[string]any{}
	for _, e := range events {
		if e.Kind == kind {
			id, _ := e.Detail["todo"].(string)
			out[id] = append(out[id], e.Detail)
		}
	}
	return out
}

// The counts: a reopen of a to-do the judge closed counts up to 24 hours after the close, the boundary included; one
// a person closed (by hand, or a supervisor's acceptance) never does; a rework counts only after the judge's done; each
// to-do once.
func TestSummarizeWrongDone(t *testing.T) {
	ev := func(kind string, d map[string]any) db.WorkEvent {
		return db.WorkEvent{At: turnEnd, Kind: kind, Detail: d}
	}
	reopen := func(todo, doneBy string, judge bool, after time.Duration) db.WorkEvent {
		return ev(todoReopenedEvent, map[string]any{"todo": todo, "done_by": doneBy, "judge": judge, "after": after.Seconds()})
	}
	rework := func(todo string, judged bool) db.WorkEvent {
		return ev(reviewReworkEvent, map[string]any{"todo": todo, "judged_done": judged})
	}
	s := summarizeWork(24, []db.WorkEvent{
		reopen("todo_hour", db.DoneByAuto, true, time.Hour),
		reopen("todo_hour", db.DoneByAuto, true, time.Minute), // closed and reopened again: one to-do
		reopen("todo_day", db.DoneByAuto, true, 24*time.Hour), // the boundary counts
		reopen("todo_judge", db.DoneByReview, true, time.Minute),
		reopen("todo_late", db.DoneByAuto, true, 24*time.Hour+time.Second),
		reopen("todo_user", "", false, time.Minute),
		reopen("todo_accepted", db.DoneByReview, false, time.Minute),
		rework("todo_judged", true),
		rework("todo_judged", true),
		rework("todo_working", false),
	})
	if s.WrongDone != (wrongDoneStats{Reopened: 3, Rework: 1}) {
		t.Fatalf("wrong_done %+v", s.WrongDone)
	}
	if len(s.Actions) != 0 || len(s.Triaged) != 0 {
		t.Fatalf("counted elsewhere too: actions %v, triaged %v", s.Actions, s.Triaged)
	}
	// Only added: with nothing to count it is there with zeros, next to the fields that were there.
	b, _ := json.Marshal(summarizeWork(24, nil))
	for _, want := range []string{`"wrong_done":{"reopened":0,"rework":0}`, `"triaged":{}`, `"needs_you":{`, `"waits":{}`} {
		if !strings.Contains(string(b), want) {
			t.Errorf("stats lack %s: %s", want, b)
		}
	}
}

// Every way a done to-do reopens records it, with who had closed it and how long ago: the hub's judge on its own, the
// judge for a supervised task's supervisor (judgeClose), a supervisor's acceptance, the user. The stats count the
// judge's closes reopened within a day.
func TestWorkStatsCountReopenedAutoDone(t *testing.T) {
	st := &workMemStore{}
	s, _ := newTaskServiceOn(t, st)
	ctx := context.Background()
	now := time.Now().UTC()
	hourAgo, dayAgo := now.Add(-time.Hour), now.Add(-wrongDoneWithin-time.Minute)
	accepted := func(by string) *Supervision {
		return &Supervision{Supervisor: "conv1", State: reviewAccepted, Reviews: []TaskReview{{At: hourAgo, Verdict: reviewAccepted, By: by}}}
	}
	task := func(id, doneBy string, doneAt time.Time, v *Supervision) {
		putTodo(s, Todo{ID: id, Title: id, Origin: "conv1", CreatedAt: doneAt.Add(-time.Hour), UpdatedAt: doneAt,
			DoneAt: &doneAt, DoneBy: doneBy, DoneReason: "做完了", Supervision: v})
	}
	task("todo_auto", db.DoneByAuto, hourAgo, nil)
	task("todo_late", db.DoneByAuto, dayAgo, nil)
	task("todo_judge", db.DoneByReview, hourAgo, accepted(reviewerJudge))
	task("todo_accepted", db.DoneByReview, hourAgo, accepted(""))
	putTodo(s, Todo{ID: "todo_user", Title: "买牛奶", CreatedAt: hourAgo, UpdatedAt: hourAgo})
	if w := serve(s, "PATCH", "/api/v1/todos/todo_user", `{"done":true}`); w.Code != 200 {
		t.Fatalf("done by hand: %d %s", w.Code, w.Body)
	}

	for _, id := range []string{"todo_auto", "todo_late", "todo_accepted", "todo_user"} {
		if w := serve(s, "PATCH", "/api/v1/todos/"+id, `{"done":false}`); w.Code != 200 {
			t.Fatalf("reopen %s: %d %s", id, w.Code, w.Body)
		}
	}
	// A change asked for on its "Please verify" card, or its agent set to work again, reopens it this way.
	if err := s.reopenTodo(ctx, "todo_judge"); err != nil {
		t.Fatal(err)
	}
	// An open to-do marked open again is no reopen.
	if w := serve(s, "PATCH", "/api/v1/todos/todo_auto", `{"done":false}`); w.Code != 200 {
		t.Fatalf("open again: %d %s", w.Code, w.Body)
	}

	s.waitWorkEvents() // a stored event is written on its own goroutine (workEvent)
	got := eventsOf(storedEvents(st), todoReopenedEvent)
	for id, want := range map[string]struct {
		doneBy string
		judge  bool
		after  time.Duration
	}{
		"todo_auto":     {db.DoneByAuto, true, time.Hour},
		"todo_late":     {db.DoneByAuto, true, wrongDoneWithin + time.Minute},
		"todo_judge":    {db.DoneByReview, true, time.Hour},
		"todo_accepted": {db.DoneByReview, false, time.Hour},
		"todo_user":     {"", false, 0},
	} {
		if len(got[id]) != 1 {
			t.Errorf("%s: %d events %v", id, len(got[id]), got[id])
			continue
		}
		d := got[id][0]
		after, _ := d["after"].(float64)
		if d["done_by"] != want.doneBy || d["judge"] != want.judge || after < want.after.Seconds() || after > (want.after+time.Minute).Seconds() {
			t.Errorf("%s: %v, want done_by %q judge %v after ~%v", id, d, want.doneBy, want.judge, want.after)
		}
	}
	if s := statsOf(t, s); s.WrongDone != (wrongDoneStats{Reopened: 2}) {
		t.Fatalf("wrong_done %+v (want todo_auto and todo_judge)", s.WrongDone)
	}
}

// task_review sending back a supervised task that waited for review because the judge found it done counts, past
// the last round too (that goes to the user as a decision; one refused for want of a decision records nothing); one
// sent back while it was not waiting for review (the agent was told more before it stopped again) does not, nor does
// an acceptance. The task counts once.
func TestWorkStatsCountReworkAfterJudgedDone(t *testing.T) {
	s, f, todo := newSupervisedService(t)
	ctx := context.Background()

	waitsForReview(t, s, f, todo.ID, "做完了，PR 已合并")
	reviewTask(t, s, todo.ID, map[string]any{"verdict": "rework", "note": "截图只有浅色，补深色"})
	reviewTask(t, s, todo.ID, map[string]any{"verdict": "rework", "note": "顺便补一个单测"}) // not waiting for review
	if _, _, err := s.updateSupervision(ctx, todo.ID, func(v *Supervision) bool { v.Round = maxReworkRounds; return true }); err != nil {
		t.Fatal(err)
	}
	waitsForReview(t, s, f, todo.ID, "深色截图和单测都补上了")
	// Past the last round, a rework without a decision for the owner is refused and records nothing; with one, it goes
	// to the user and counts.
	raw, _ := json.Marshal(map[string]any{"todo_id": todo.ID, "verdict": "rework", "note": "还是不对"})
	if _, err := s.runTool(ctx, "server", "conv1", "task_review", raw); err == nil || !strings.Contains(err.Error(), "why_owner") {
		t.Fatalf("a rework past the last round with no decision: %v", err)
	}
	if res := reviewTask(t, s, todo.ID, map[string]any{"verdict": "rework", "note": "还是不对", "why_owner": "priority",
		"reason": "返工了 2 轮还没做完，接下来怎么办", "recommend": "再返工一轮", "cost_of_waiting": "PR 一直开着",
		"options": []map[string]string{{"label": "再返工一轮", "leads_to": "再等大约半小时"}, {"label": "就这样收下", "leads_to": "缺深色截图，直接合并"}},
	}); !strings.Contains(res, "sent back 2 times") {
		t.Fatalf("past the last round: %s", res)
	}

	var judged []bool
	for _, d := range eventsOf(testEvents(s), reviewReworkEvent)[todo.ID] {
		b, _ := d["judged_done"].(bool)
		judged = append(judged, b)
	}
	if len(judged) != 3 || !judged[0] || judged[1] || !judged[2] {
		t.Fatalf("judged_done %v, want [true false true]", judged)
	}
	if got := summarizeWork(1, testEvents(s)).WrongDone; got != (wrongDoneStats{Rework: 1}) {
		t.Fatalf("wrong_done %+v", got)
	}

	// An acceptance after the judge's done is no rework.
	s2, f2, other := newSupervisedService(t)
	waitsForReview(t, s2, f2, other.ID, "做完了，PR 已合并")
	reviewTask(t, s2, other.ID, map[string]any{"verdict": "accepted", "note": "核对过了", "checks": completeReviewChecks(todoNow(s2, other.ID))})
	if n := len(eventsOf(testEvents(s2), reviewReworkEvent)); n != 0 {
		t.Fatalf("an acceptance recorded %d reworks", n)
	}
	if got := summarizeWork(1, testEvents(s2)).WrongDone; got != (wrongDoneStats{}) {
		t.Fatalf("an acceptance counted: %+v", got)
	}
}
