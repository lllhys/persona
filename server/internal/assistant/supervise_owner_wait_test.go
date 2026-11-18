package assistant

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/proto"
)

// ownerDecision is a needs_user review a decision the owner can answer (#3804).
func ownerDecision(todoID string) json.RawMessage {
	raw, _ := json.Marshal(map[string]any{"todo_id": todoID, "verdict": "needs_user", "why_owner": "only_owner_knows",
		"reason": "两版公告选哪一版", "recommend": "A 版", "cost_of_waiting": "公告晚一天发",
		"options": []map[string]string{{"label": "A 版", "leads_to": "今天发 A 版"}, {"label": "B 版", "leads_to": "今天发 B 版"}}})
	return raw
}

// stopOnOwner makes the task's agent stop with drafts for the owner to pick from, and the judge say so (needs_user).
func stopOnOwner(t *testing.T, s *Service, f *fakeHub, todoID string) {
	t.Helper()
	f.judge = `{"verdict":"needs_user","reason":"两版草稿等用户挑一版再发"}`
	agentTurn(s, f, proto.StatusWorking, "", 0)
	agentTurn(s, f, proto.StatusDone, "两版公告草稿都写好了，等你挑一版我再发。", 0)
	waitFor(t, "the owner wait on the ledger", func() bool { return todoNow(s, todoID).Supervision.Waits != nil })
}

// The way #1277 saw it fail: a task whose work stopped on a decision only the owner can make (two drafts for them to
// pick from) was accepted as done, and the decision was lost with it. The judge's needs_user is kept on the ledger, and
// accepting is refused while it holds; a decision for the owner is what the review has to be.
func TestAcceptedIsRefusedWhileTheWorkWaitsOnTheOwner(t *testing.T) {
	s, f, todo := newSupervisedService(t)
	ctx := context.Background()
	stopOnOwner(t, s, f, todo.ID)
	w := todoNow(s, todo.ID).Supervision.Waits
	if w.Reason != "两版草稿等用户挑一版再发" || w.At.IsZero() || w.Work.IsZero() {
		t.Fatalf("waits = %+v", w)
	}
	if health, _ := taskHealth(todoNow(s, todo.ID), nil, time.Now()); health != "停下等用户拍板，别直接验收" {
		t.Fatalf("snapshot health = %q", health)
	}

	raw, _ := json.Marshal(map[string]any{"todo_id": todo.ID, "verdict": "accepted", "note": "草稿写好了"})
	_, err := s.runTool(ctx, "server", "conv1", "task_review", raw)
	if err == nil || !strings.Contains(err.Error(), "decision only the owner can make") || !strings.Contains(err.Error(), "needs_user") ||
		!strings.Contains(err.Error(), "两版草稿等用户挑一版再发") {
		t.Fatalf("accepted while the work waits on the owner: %v", err)
	}
	now := todoNow(s, todo.ID)
	if now.DoneAt != nil || len(now.Supervision.Reviews) != 0 || now.Supervision.Waits == nil {
		t.Fatalf("a refused review changed the task: done %v, %+v", now.DoneAt, now.Supervision)
	}
	// user_check does not get round it either.
	raw, _ = json.Marshal(map[string]any{"todo_id": todo.ID, "verdict": "accepted", "user_check": true})
	if _, err := s.runTool(ctx, "server", "conv1", "task_review", raw); err == nil {
		t.Fatal("accepted with user_check while the work waits on the owner")
	}

	// The decision goes to the owner: allowed, and the wait is answered.
	if res, err := s.runTool(ctx, "server", "conv1", "task_review", ownerDecision(todo.ID)); err != nil || !strings.Contains(res, "waits for the user") {
		t.Fatalf("needs_user: %s %v", res, err)
	}
	if v := todoNow(s, todo.ID).Supervision; v.State != reviewNeedsUser || v.Waits != nil {
		t.Fatalf("after the decision went to the owner: %+v", v)
	}
}

// The owner's answer reaches the agent and it goes on: the wait is over, and once the judge finds the work done the task
// is accepted as usual.
func TestOwnerWaitEndsWhenTheAgentGoesOn(t *testing.T) {
	s, f, todo := newSupervisedService(t)
	ctx := context.Background()
	stopOnOwner(t, s, f, todo.ID)

	f.judge = `{"verdict":"done","reason":"按用户选的 A 版发了"}`
	agentTurn(s, f, proto.StatusWorking, "", 0)
	waitFor(t, "the wait cleared by work", func() bool { return todoNow(s, todo.ID).Supervision.Waits == nil })
	agentTurn(s, f, proto.StatusDone, "按你选的 A 版发出去了，PR https://github.com/o/r/pull/7 已合并", 0)
	waitFor(t, "waits for review", func() bool { return todoNow(s, todo.ID).Supervision.State == reviewPending })

	raw, _ := json.Marshal(map[string]any{"todo_id": todo.ID, "verdict": "accepted", "note": "A 版已发", "checks": completeReviewChecks(todoNow(s, todo.ID))})
	if res, err := s.runTool(ctx, "server", "conv1", "task_review", raw); err != nil || !strings.Contains(res, "accepted and closed") {
		t.Fatalf("accepted after the agent went on: %s %v", res, err)
	}
}

// A task the owner no longer wants can still be cancelled while its work waits on them.
func TestCancelledWhileTheWorkWaitsOnTheOwner(t *testing.T) {
	s, f, todo := newSupervisedService(t)
	stopOnOwner(t, s, f, todo.ID)
	raw, _ := json.Marshal(map[string]any{"todo_id": todo.ID, "verdict": "cancelled", "note": "用户说不发公告了"})
	if _, err := s.runTool(context.Background(), "server", "conv1", "task_review", raw); err != nil {
		t.Fatalf("cancelled: %v", err)
	}
	if now := todoNow(s, todo.ID); now.DoneAt == nil || now.Supervision.Waits != nil {
		t.Fatalf("after cancelling: done %v, %+v", now.DoneAt, now.Supervision)
	}
}

// The wait is kept in the database with the ledger, so a manager restart does not let the task be accepted.
func TestOwnerWaitSurvivesTheLedgerRow(t *testing.T) {
	at := time.Date(2026, 11, 11, 9, 0, 0, 0, time.UTC)
	v := &Supervision{Supervisor: "conv1", DoneWhen: []string{}, Waits: &OwnerWait{Work: at.Add(-time.Hour), At: at, Reason: "等用户挑一版"}}
	back := supervisionFromRow(v.row("todo_x"))
	if back.Waits == nil || !back.Waits.At.Equal(at) || !back.Waits.Work.Equal(at.Add(-time.Hour)) || back.Waits.Reason != "等用户挑一版" {
		t.Fatalf("waits back from its row = %+v", back.Waits)
	}
	c := v.clone()
	c.Waits.Reason = "changed"
	if v.Waits.Reason != "等用户挑一版" {
		t.Fatal("clone shares the wait")
	}
	if none := (&Supervision{}).row("todo_y"); none.Waits != nil {
		t.Fatalf("no wait is written as %s", none.Waits)
	}
}
