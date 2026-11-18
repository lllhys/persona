package assistant

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
)

// ---- the brief of a task_plan task (#3822) ----

// briefRequired refuses a plan's task that does not say what done is: a writable task needs done_when (one criterion
// that can be checked, at least) and expect_minutes, a read-only one a goal and expect_minutes. A task that continues
// another (continues) takes that task's brief instead and says why it takes over. The refusal names what is missing.
func briefRequired(t *PlanTask) error {
	if t.Continues != "" {
		if t.ContinuesWhy == "" {
			return errors.New("continues needs continues_why: one line on why the earlier task stopped and this one takes over")
		}
		return nil
	}
	var missing []string
	if t.Readonly {
		if t.Goal == "" {
			missing = append(missing, "goal (what the owner gets from it, in one sentence)")
		}
	} else if len(t.DoneWhen) == 0 {
		missing = append(missing, "done_when (at least one result that can be checked)")
	}
	if t.ExpectMinutes <= 0 {
		missing = append(missing, "expect_minutes (about how long it should take)")
	}
	if len(missing) == 0 {
		return nil
	}
	kind := "a writable task"
	if t.Readonly {
		kind = "a read-only task"
	}
	return fmt.Errorf("%s needs %s; nothing was planned. Add them and call task_plan again (a task that takes over from another "+
		"gives continues and continues_why instead and keeps that task's brief)", kind, strings.Join(missing, " and "))
}

// openSuccessor is an open to-do that already carries on to-do id (continues, directly or down a chain of to-dos that
// did): a second continuation of id would be a second to-do for the same work (#6427). False when there is none.
func (s *Service) openSuccessor(id string) (Todo, bool) {
	seen := map[string]bool{id: true}
	next := []string{id}
	for len(next) > 0 {
		b := &s.tbriefs
		b.mu.Lock()
		b.init()
		var found []string
		for todo, l := range b.links {
			if l.Continues != "" && slices.Contains(next, l.Continues) && !seen[todo] {
				found = append(found, todo)
			}
		}
		b.mu.Unlock()
		slices.Sort(found)
		next = nil
		for _, todo := range found {
			seen[todo] = true
			if t, ok := s.todoNow(todo); ok && t.DoneAt == nil && !t.Cancelled() {
				return t, true
			}
			next = append(next, todo)
		}
	}
	return Todo{}, false
}

// errContinued refuses a task that continues prev when to-do succ already does.
func errContinued(prev string, succ Todo) error {
	return fmt.Errorf("continues: %s is already carried on by to-do %s (%q), which is still open. Do not open a second "+
		"to-do for the same work: plan the task with todo_id %s (it runs on that to-do), or send its agent what is new "+
		"(agent_send)", prev, succ.ID, succ.Title, succ.ID)
}

// takeOver binds a continuation to its original to-do, without stopping Agents or reopening it before Start.
// An unsafe or unauthorized handoff is refused, never replaced with a second to-do.
func (s *Service) takeOver(ctx context.Context, conversation string, t *PlanTask) error {
	if t.Continues == "" {
		return nil
	}
	if t.ReuseTodo != "" && t.ReuseTodo != t.Continues {
		return fmt.Errorf("continues and todo_id must name the same to-do")
	}
	if succ, ok := s.openSuccessor(t.Continues); ok {
		return errContinued(t.Continues, succ)
	}
	if _, err := s.taskTodoForConversation(conversation, t.Continues); err != nil {
		return err
	}
	t.ReuseTodo = t.Continues
	return nil
}

// planBriefs gives each task of a plan from conversation (on machine) its brief as it will be kept, for the card to
// show before "Start": who asked is read off the conversation once (briefOrigin); a task that continues another takes
// that task's brief, with its criteria and expected time as they stand.
func (s *Service) planBriefs(ctx context.Context, machine, conversation string, tasks []PlanTask) error {
	var origin *briefRecord
	for i := range tasks {
		t := &tasks[i]
		if t.Continues != "" {
			prev, ok := s.todoNow(t.Continues)
			if !ok {
				return fmt.Errorf("task %d: continues: no task %q", i+1, t.Continues)
			}
			if prev.Origin == "" {
				return fmt.Errorf("task %d: continues: %s is the user's own to-do, not a task that was handed out", i+1, prev.ID)
			}
			brief, err := s.ensureTaskBrief(ctx, prev)
			if err != nil {
				return fmt.Errorf("task %d: continues: %v", i+1, err)
			}
			if t.ScopeRevision != "" && t.ScopeRevision != brief.ClockAt.Format(time.RFC3339Nano) {
				return fmt.Errorf("task %d: the scope of %s changed; read its brief and plan the continuation again", i+1, prev.ID)
			}
			// Its own prompt stays its own; what done means is the brief's, as its scope changes left it.
			t.DoneWhen, t.ExpectMinutes = brief.Criteria, brief.ExpectNow
			t.Brief = &brief
			continue
		}
		if origin == nil {
			o := s.briefOrigin(ctx, machine, conversation)
			origin = &o
		}
		rec := *origin
		rec.ID, rec.CreatedAt = "brief_"+randomHex(8), time.Now().UTC()
		rec.Goal, rec.DoneWhen, rec.ExpectMinutes, rec.OutOfBounds = firstNonEmpty(t.Goal, t.Title), t.DoneWhen, t.ExpectMinutes, t.OutOfBounds
		if rec.StartedBy != briefByOwner {
			rec.Reason = t.StartedWhy
		}
		v := (&taskBrief{rec: rec}).view()
		t.Brief = &v
	}
	return nil
}

// checkContinuationScope rejects a plan that was prepared against a different
// revision of the original brief. ClockAt advances for every scope entry,
// including changes that keep the effective criteria and expected time.
func (s *Service) checkContinuationScope(t *PlanTask) error {
	if t.Continues == "" {
		return nil
	}
	current, ok := s.briefOfTodo(t.Continues)
	if !ok || t.Brief == nil || current.ID != t.Brief.ID || !current.ClockAt.Equal(t.Brief.ClockAt) {
		return fmt.Errorf("the scope of %s changed or is unavailable; read its brief and plan the continuation again", t.Continues)
	}
	return nil
}

// briefStarted keeps the brief of plan p's task t now that it is to-do todo: the one its card showed, dated from the
// start, or the brief of the task it continues. A plan saved before briefs has none on its tasks: it gets one from
// what the task says, with nobody named as having asked.
func (s *Service) briefStarted(ctx context.Context, p *TaskPlan, t *PlanTask, todo Todo) {
	if _, ok := s.briefOfTodo(todo.ID); ok {
		return // a to-do the task runs on (reuse_todo) that has its brief already
	}
	if t.Continues != "" {
		prev, ok := s.todoNow(t.Continues)
		if ok {
			if brief, err := s.ensureTaskBrief(ctx, prev); err == nil {
				if err := s.linkTaskBrief(ctx, brief.ID, todo.ID, prev.ID, t.ContinuesWhy); err != nil {
					s.logf("task %s: brief of %s: %v", todo.ID, prev.ID, err)
				}
				return
			}
		}
		s.logf("task %s: the task it continues (%s) is gone: it gets a brief of its own", todo.ID, t.Continues)
	}
	rec := briefRecord{StartedBy: briefByUnknown, Conversation: p.Conversation, Goal: firstNonEmpty(t.Goal, t.Title),
		DoneWhen: t.DoneWhen, ExpectMinutes: t.ExpectMinutes, OutOfBounds: t.OutOfBounds}
	if t.Brief != nil && !t.Brief.Reconstructed && t.Continues == "" {
		rec = t.Brief.briefRecord
	}
	rec.CreatedAt = time.Now().UTC() // the clock starts when the work is handed out, not when it was proposed
	if _, err := s.putTaskBrief(ctx, rec, todo.ID); err != nil {
		s.logf("task %s: brief: %v", todo.ID, err)
	}
}
