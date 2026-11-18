package assistant

import (
	"context"
	"encoding/json"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/db"
	"github.com/nwverse/persona/server/internal/proto"
)

func completeReviewChecks(todo Todo) []ReviewCheck {
	if todo.Supervision == nil {
		return nil
	}
	checks := make([]ReviewCheck, 0, len(todo.Supervision.DoneWhen))
	for _, criterion := range todo.Supervision.DoneWhen {
		checks = append(checks, ReviewCheck{Criterion: criterion, Met: true, Evidence: "verified: " + criterion})
	}
	return checks
}

func TestCriteriaLegacyAndReordering(t *testing.T) {
	v := supervisionFromRow(db.TodoSupervisionRow{DoneWhen: []string{"build", "test"}, Reviews: json.RawMessage("[]")})
	if len(v.Criteria) != 2 || v.Criteria[0].State != "pending" || v.Criteria[0].At != nil || v.Criteria[0].By != "" {
		t.Fatalf("legacy criteria: %+v", v.Criteria)
	}
	at := time.Now().UTC()
	v.applyChecks([]ReviewCheck{{Criterion: "test", Met: true, Evidence: "go test: PASS"}}, at, "supervisor:main")
	v.DoneWhen = []string{"test", "deploy", "build"}
	v = v.clone()
	if v.Criteria[0].Criterion != "test" || v.Criteria[0].State != "met" || v.Criteria[0].Evidence != "go test: PASS" ||
		!v.Criteria[0].At.Equal(at) || v.Criteria[1].State != "pending" || v.Criteria[2].State != "pending" {
		t.Fatalf("reordered criteria: %+v", v.Criteria)
	}
	copy := v.clone()
	copy.Criteria[0].Evidence = "changed clone"
	*copy.Criteria[0].At = at.Add(time.Hour)
	if v.Criteria[0].Evidence != "go test: PASS" || !v.Criteria[0].At.Equal(at) {
		t.Fatal("clone aliases the ledger")
	}
}

func TestCriteriaReportRejectsChangedTask(t *testing.T) {
	s, f, todo := newSupervisedService(t)
	agentTurn(s, f, proto.StatusWorking, "", 0)
	ctx := context.Background()
	s.superviseTodo(ctx, todo.ID, "conv1", []string{"new first criterion", "new second criterion"}, 0)
	err := s.recordCriterionReports(ctx, todo, "box", "s1", []criterionReport{{Number: 1, State: "met", Evidence: "old first criterion"}}, time.Now())
	if err == nil || !strings.Contains(err.Error(), "changed") {
		t.Fatalf("stale report: %v", err)
	}
	for _, c := range todoNow(s, todo.ID).Supervision.Criteria {
		if c.State != "pending" || c.At != nil {
			t.Fatalf("stale evidence applied to a new criterion: %+v", c)
		}
	}
}

// A decision review keeps both the current criteria and the decision; when the
// owner hands it back, the digest carries the evidence beside that decision.
func TestCriteriaReviewAndDecisionDigest(t *testing.T) {
	s, f, todo := newSupervisedService(t)
	waitsForReview(t, s, f, todo.ID, "两种走法")
	args := rolloutDecision()
	args["checks"] = []ReviewCheck{{Criterion: todo.Supervision.DoneWhen[0], Met: true, Evidence: "PR merged before rollout"}}
	if _, err := reviewErr(s, todo.ID, args); err != nil {
		t.Fatal(err)
	}
	v := todoNow(s, todo.ID).Supervision
	r, c := v.Reviews[len(v.Reviews)-1], v.Criteria[0]
	if v.State != reviewNeedsUser || r.WhyOwner != "priority" || r.Reason != args["reason"] ||
		r.Recommend != args["recommend"] || len(r.Options) != 2 || r.CostOfWaiting != args["cost_of_waiting"] ||
		c.State != "met" || c.Evidence != "PR merged before rollout" || c.By != "supervisor:conv1" || c.At == nil || !c.At.Equal(r.At) {
		t.Fatalf("decision or criterion lost: review %+v, criterion %+v", r, c)
	}
	if rec := answerAsk(s, todo.ID, map[string]string{"response": respondDecideYourself}); rec.Code != http.StatusOK {
		t.Fatalf("owner response: %d %s", rec.Code, rec.Body.String())
	}
	waitFor(t, "the decision and criterion in the owner response digest", func() bool {
		for _, n := range f.notices() {
			if strings.Contains(n, "用户回答不了你发的决定") && strings.Contains(n, r.Reason) &&
				strings.Contains(n, r.Options[1].LeadsTo) && strings.Contains(n, "criterion 1 [met]") &&
				strings.Contains(n, c.Evidence) && strings.Contains(n, c.By) && strings.Contains(n, c.At.UTC().Format(time.RFC3339)) {
				return true
			}
		}
		return false
	})
}

func TestCriteriaReportAndReview(t *testing.T) {
	for _, stored := range []bool{false, true} {
		name := "memory"
		if stored {
			name = "postgres"
		}
		t.Run(name, func(t *testing.T) {
			var st Store
			if stored {
				st = todoTestDB(t)
			}
			s, f, todo := newSupervisedServiceOn(t, st)
			agentTurn(s, f, proto.StatusWorking, "", 0)
			ctx := context.Background()
			now := time.Now().UTC()
			report := func(criteria string) error {
				_, err := s.reportToSupervisor(ctx, "box", "s1", json.RawMessage(
					`{"kind":"progress","text":"逐条核对","criteria":`+criteria+`}`), now)
				return err
			}
			if err := report(`[{"number":2,"state":"met","evidence":"two screenshots attached"}]`); err != nil {
				t.Fatal(err)
			}
			cur := todoNow(s, todo.ID)
			if cur.DoneAt != nil || cur.Supervision.State != "" || len(cur.Supervision.Reviews) != 0 {
				t.Fatal("an agent report accepted the task")
			}
			c := cur.Supervision.Criteria
			if c[0].State != "pending" || c[1].State != "met" || c[1].By != "agent:box/s1" || !c[1].At.Equal(now) {
				t.Fatalf("reported criteria: %+v", c)
			}
			if len(f.notices()) != 0 {
				t.Fatal("a progress report woke the supervisor")
			}
			if _, err := s.reportToSupervisor(ctx, "box", "s1",
				json.RawMessage(`{"kind":"blocked","text":"Need a deployment decision"}`), now); err != nil {
				t.Fatal(err)
			}
			waitFor(t, "criteria evidence in supervision message", func() bool {
				return strings.Contains(strings.Join(f.notices(), "\n"), "two screenshots attached")
			})
			// A review changes only what it checked, including a later correction to an agent's report.
			_, err := runJSON(t, s, "conv1", "task_review", map[string]any{
				"todo_id": todo.ID, "verdict": "rework",
				"checks": []ReviewCheck{{Criterion: todo.Supervision.DoneWhen[1], Evidence: "dark screenshot missing"}},
			})
			if err != nil {
				t.Fatal(err)
			}
			c = todoNow(s, todo.ID).Supervision.Criteria
			if c[0].State != "pending" || c[1].State != "not_met" || c[1].Evidence != "dark screenshot missing" ||
				c[1].By != "supervisor:conv1" || c[1].At == nil {
				t.Fatalf("review criteria: %+v", c)
			}
			if err := report(`[{"number":1,"state":"met","evidence":"PR merged"},{"number":2,"state":"pending"}]`); err != nil {
				t.Fatal(err)
			}
			c = todoNow(s, todo.ID).Supervision.Criteria
			if c[0].State != "met" || c[1].State != "pending" || c[1].Evidence != "" {
				t.Fatalf("reset: %+v", c)
			}
			if got, err := runJSON(t, s, "conv1", "todo_get", map[string]any{"id": todo.ID}); err != nil ||
				!strings.Contains(got, "1. [met]") || !strings.Contains(got, "agent:box/s1") || !strings.Contains(got, "PR merged") {
				t.Fatalf("assistant read: %s, %v", got, err)
			}
			// All updates in a malformed batch are refused, without a partial write.
			for _, bad := range []string{
				`[{"number":0,"state":"met","evidence":"x"}]`,
				`[{"number":3,"state":"met","evidence":"x"}]`,
				`[{"number":1,"state":"met","evidence":" "}]`,
				`[{"number":1,"state":"wrong","evidence":"x"}]`,
				`[{"number":1,"state":"pending"},{"number":1,"state":"met","evidence":"x"}]`,
				`[{"number":1,"state":"pending"},{"number":2,"state":"met"}]`,
			} {
				if err := report(bad); err == nil {
					t.Fatalf("accepted %s", bad)
				}
				if got := todoNow(s, todo.ID).Supervision.Criteria; !reflect.DeepEqual(got, c) {
					t.Fatalf("invalid report changed criteria: %+v", got)
				}
			}
			if stored {
				reloadTodos(t, s)
				if got := todoNow(s, todo.ID).Supervision.Criteria; !reflect.DeepEqual(got, c) {
					t.Fatalf("restart lost criteria: %+v", got)
				}
			}
			e := &todoEnv{t: t, s: s, f: f}
			if got := e.get(todo.ID); got == nil || !reflect.DeepEqual(got.Supervision.Criteria, c) {
				t.Fatalf("todo API lost criteria: %+v", got)
			}
			// A final review confirms every current entry without relying on an earlier agent report.
			_, err = runJSON(t, s, "conv1", "task_review", map[string]any{
				"todo_id": todo.ID, "verdict": "accepted",
				"checks": []ReviewCheck{
					{Criterion: todo.Supervision.DoneWhen[0], Met: true, Evidence: "PR merged"},
					{Criterion: todo.Supervision.DoneWhen[1], Met: true, Evidence: "both screenshots verified"},
				},
			})
			if err != nil {
				t.Fatal(err)
			}
			got := e.get(todo.ID)
			if got.DoneAt == nil || got.Supervision.Criteria[0].Evidence != "PR merged" || got.Supervision.Criteria[1].State != "met" ||
				got.Supervision.Criteria[1].By != "supervisor:conv1" {
				t.Fatalf("final review: %+v", got)
			}
			payload, _ := json.Marshal(map[string]any{"id": got.ID, "supervision": got.Supervision})
			t.Logf("todo response fields: %s", payload)
		})
	}
}

// An ordinary accepted/done review proves every current criterion in this call. Invalid checks write no review,
// criterion state, brief result or completion; cannot, cancelled and a task without criteria keep their old behavior.
func TestAcceptedReviewRequiresCompleteCurrentCriteria(t *testing.T) {
	for _, stored := range []bool{false, true} {
		name := "memory"
		if stored {
			name = "postgres"
		}
		t.Run(name, func(t *testing.T) {
			var st Store
			if stored {
				st = todoTestDB(t)
			}
			s, _, todo := newSupervisedServiceOn(t, st)
			before := todoNow(s, todo.ID)
			valid := completeReviewChecks(before)
			for _, tc := range []struct {
				name      string
				checks    []ReviewCheck
				want      string
				userCheck bool
			}{
				{name: "no checks", want: "has no check", userCheck: true},
				{name: "missing current criterion", checks: valid[:1], want: "has no check"},
				{name: "duplicate check", checks: []ReviewCheck{valid[0], valid[0], valid[1]}, want: "duplicate"},
				{name: "unknown criterion", checks: append(append([]ReviewCheck{}, valid...), ReviewCheck{Criterion: "old criterion", Met: true, Evidence: "old"}), want: "unknown current done_when"},
				{name: "blank criterion", checks: append(append([]ReviewCheck{}, valid...), ReviewCheck{Criterion: " ", Met: true, Evidence: "blank"}), want: "unknown current done_when"},
				{name: "unmet current criterion", checks: []ReviewCheck{valid[0], {Criterion: before.Supervision.DoneWhen[1], Evidence: "still missing"}}, want: "not met"},
				{name: "blank evidence", checks: []ReviewCheck{valid[0], {Criterion: before.Supervision.DoneWhen[1], Met: true, Evidence: " \n\t"}}, want: "non-empty evidence"},
			} {
				t.Run(tc.name, func(t *testing.T) {
					res, err := reviewErr(s, todo.ID, map[string]any{"verdict": "accepted", "checks": tc.checks, "user_check": tc.userCheck})
					if err == nil || !strings.Contains(err.Error(), tc.want) || !strings.Contains(err.Error(), "nothing was written") {
						t.Fatalf("accepted invalid checks: %s, %v", res, err)
					}
					after := todoNow(s, todo.ID)
					if after.DoneAt != nil || !reflect.DeepEqual(after.Supervision, before.Supervision) {
						t.Fatalf("a refused review wrote the task or ledger: before %+v, after %+v", before.Supervision, after.Supervision)
					}
					if brief, ok := s.briefOfTodo(todo.ID); ok && brief.Result != nil {
						t.Fatalf("a refused review wrote the brief result: %+v", brief.Result)
					}
				})
			}
			if res, err := reviewErr(s, todo.ID, map[string]any{"verdict": "accepted", "checks": valid}); err != nil ||
				!strings.Contains(res, "accepted and closed") || todoNow(s, todo.ID).DoneAt == nil {
				t.Fatalf("complete acceptance: %s, %v", res, err)
			}

			ctx := context.Background()
			newTask := func(title string, criteria []string) Todo {
				t.Helper()
				created, err := s.createTaskTodo(ctx, title, "", "conv1", "", "")
				if err != nil {
					t.Fatal(err)
				}
				s.superviseTodo(ctx, created.ID, "conv1", criteria, 0)
				return todoNow(s, created.ID)
			}
			cannot := newTask("cannot be done", []string{"first", "second"})
			if res, err := reviewErr(s, cannot.ID, map[string]any{"verdict": "accepted", "outcome": "cannot",
				"checks": []ReviewCheck{{Criterion: "first", Evidence: "not possible"}}}); err != nil ||
				!strings.Contains(res, "accepted and closed") || todoNow(s, cannot.ID).DoneAt == nil {
				t.Fatalf("outcome cannot: %s, %v", res, err)
			}
			cancelled := newTask("cancelled", []string{"first", "second"})
			if res, err := reviewErr(s, cancelled.ID, map[string]any{"verdict": "cancelled"}); err != nil ||
				!strings.Contains(res, "cancelled and closed") || !todoNow(s, cancelled.ID).Cancelled() {
				t.Fatalf("cancelled: %s, %v", res, err)
			}
			plain := newTask("no criteria", nil)
			if res, err := reviewErr(s, plain.ID, map[string]any{"verdict": "accepted"}); err != nil ||
				!strings.Contains(res, "accepted and closed") || todoNow(s, plain.ID).DoneAt == nil {
				t.Fatalf("no done_when: %s, %v", res, err)
			}
		})
	}
}

// Checks are matched to the criteria left by a scope change, not the brief's or an earlier ledger's wording.
func TestAcceptedReviewUsesCurrentCriteriaAfterScopeChange(t *testing.T) {
	s, _, todo := newSupervisedService(t)
	ctx := context.Background()
	if _, err := s.applyScope(ctx, todo, scopeChange{By: "conv1", Change: "use the current verification steps", Why: "the scope changed",
		DoneWhen: []string{"current build passes", "current screenshots verified"}}); err != nil {
		t.Fatal(err)
	}
	if res, err := reviewErr(s, todo.ID, map[string]any{"verdict": "accepted", "checks": completeReviewChecks(todo)}); err == nil ||
		!strings.Contains(err.Error(), "unknown current done_when") {
		t.Fatalf("accepted the superseded criteria: %s, %v", res, err)
	}
	current := todoNow(s, todo.ID)
	if current.DoneAt != nil || len(current.Supervision.Reviews) != 0 {
		t.Fatalf("the stale acceptance wrote the task: %+v %+v", current, current.Supervision)
	}
	if res, err := reviewErr(s, todo.ID, map[string]any{"verdict": "accepted", "checks": completeReviewChecks(current)}); err != nil ||
		!strings.Contains(res, "accepted and closed") {
		t.Fatalf("accepted the current criteria: %s, %v", res, err)
	}
}
