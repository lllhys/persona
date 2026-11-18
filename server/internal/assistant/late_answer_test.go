package assistant

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/db"
	"github.com/nwverse/persona/server/internal/proto"
)

// The card of an ask_user keeps what its arguments said of the decision (deadline, reversible, each question's
// recommendation), and its history tells who answered: the user on the card, the user on the to-do after it expired,
// or nobody, the recommendation taken at the deadline (late_answer.go).

const lateAskQuestion = "Merge the release PR now?"

func askRecord(t *testing.T, database *db.DB, id string) *db.InteractionRecord {
	t.Helper()
	rec, err := database.InteractionRecord(context.Background(), id)
	if err != nil || rec == nil {
		t.Fatalf("history of %s: %v %v", id, rec, err)
	}
	return rec
}

func askArgs(deadline, reversible string) string {
	return strings.Replace(strings.Replace(askDeadlineArgsJSON, "%s", deadline, 1), "%s", reversible, 1)
}

// expireRecorded is askTodoEnv.expireWith with the card's history written as the fleet writes it in a manager
// (fleet_ix_own.go: the card when it goes up, "expired" before the tool hears of it); this env runs without the fleet.
func expireRecorded(e *askTodoEnv, database *db.DB, args string) (proto.Interaction, string) {
	e.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	done := make(chan string, 1)
	go func() {
		text, err := e.s.toolAskUser(ctx, "server", "c1x1x1", json.RawMessage(args))
		if err != nil {
			text = "error: " + err.Error()
		}
		done <- text
	}()
	var ix proto.Interaction
	waitFor(e.t, "the question card", func() bool {
		if qs := e.s.pendingQuestions(); len(qs) == 1 {
			ix = qs[0]
			return true
		}
		return false
	})
	expired := proto.Resolution{Dismiss: true, Reason: proto.DismissedExpired}
	if err := database.InsertInteraction(ctx, ix); err != nil {
		e.t.Fatal(err)
	}
	if err := database.FinishInteraction(ctx, ix.ID, "expired", &expired); err != nil {
		e.t.Fatal(err)
	}
	answer, _ := json.Marshal(expired)
	if _, err := e.s.Action(ctx, Action{Op: "answer", InteractionID: ix.ID, Answer: answer}); err != nil {
		e.t.Fatal(err)
	}
	select {
	case text := <-done:
		return ix, text
	case <-ctx.Done():
		e.t.Fatal("ask_user did not return")
		return ix, ""
	}
}

func TestAskUserCardKeepsTheDecisionFields(t *testing.T) {
	database := todoTestDB(t)
	e := newAskTodoEnv(t, database)
	ix, _ := expireRecorded(e, database, askArgs("6h", "true"))
	if ix.Deadline == nil || ix.Reversible == nil || !*ix.Reversible || ix.Questions[0].Recommend != "Merge" {
		t.Fatalf("the card: deadline %v reversible %v recommend %q", ix.Deadline, ix.Reversible, ix.Questions[0].Recommend)
	}
	rec := askRecord(t, database, ix.ID)
	got := rec.Interaction
	if got.Deadline == nil || !got.Deadline.Equal(*ix.Deadline) || got.Reversible == nil || !*got.Reversible || got.Questions[0].Recommend != "Merge" {
		t.Fatalf("the history: deadline %v reversible %v recommend %q", got.Deadline, got.Reversible, got.Questions[0].Recommend)
	}
	if rec.Status != "expired" || rec.Resolution == nil || rec.Resolution.Late || len(rec.Resolution.Answers) != 0 {
		t.Fatalf("before the deadline: %s %+v", rec.Status, rec.Resolution)
	}

	// Past the deadline the recommendation is taken by default: the history says so, the card still expired.
	todo := e.onlyAskTodo()
	e.s.sweepDecisionDeadlines(time.Now().Add(7 * time.Hour))
	waitFor(t, "the default recorded", func() bool {
		r := askRecord(t, database, ix.ID).Resolution
		return r != nil && r.Late
	})
	waitFor(t, "the to-do closed", func() bool { cur := e.get(todo.ID); return cur != nil && cur.DoneAt != nil })
	rec = askRecord(t, database, ix.ID)
	r := rec.Resolution
	if rec.Status != "expired" || r.AnsweredBy != proto.AnsweredByDefault || r.Answers[lateAskQuestion] != "Merge" || r.Reason == "" || r.Dismiss {
		t.Fatalf("after the deadline: %s %+v", rec.Status, r)
	}
}

// A card that waits until its deadline answers itself at once: the history records that default too.
func TestAskUserInlineDefaultIsInTheHistory(t *testing.T) {
	old := askInlineSlack
	askInlineSlack = 2 * time.Minute
	t.Cleanup(func() { askInlineSlack = old })
	database := todoTestDB(t)
	e := newAskTodoEnv(t, database)
	ix, text := expireRecorded(e, database, askArgs("1m", "true"))
	if !strings.Contains(text, "taken by default") {
		t.Fatalf("tool result %q", text)
	}
	waitFor(t, "the default recorded", func() bool {
		r := askRecord(t, database, ix.ID).Resolution
		return r != nil && r.Late
	})
	if r := askRecord(t, database, ix.ID).Resolution; r.AnsweredBy != proto.AnsweredByDefault || r.Answers[lateAskQuestion] != "Merge" {
		t.Fatalf("history %+v", r)
	}
}

// A question that cannot be undone is never taken by default: past its deadline the history still has no answer.
func TestIrreversibleAskUserHistoryStaysUnanswered(t *testing.T) {
	database := todoTestDB(t)
	e := newAskTodoEnv(t, database)
	ix, _ := expireRecorded(e, database, askArgs("6h", "false"))
	e.s.sweepDecisionDeadlines(time.Now().Add(7 * time.Hour)) // a question that cannot be undone is handled within the sweep
	rec := askRecord(t, database, ix.ID)
	if rec.Interaction.Reversible == nil || *rec.Interaction.Reversible || rec.Resolution == nil || rec.Resolution.Late || len(rec.Resolution.Answers) != 0 {
		t.Fatalf("history %+v %+v", rec.Interaction.Reversible, rec.Resolution)
	}
}

// Without a deadline the card has none of the fields (nothing is guessed); the user's answer on its to-do afterwards is
// the user's own in the history.
func TestAskUserAnsweredOnItsTodoIsTheUsers(t *testing.T) {
	database := todoTestDB(t)
	e := newAskTodoEnv(t, database)
	ix, _ := expireRecorded(e, database, askTodoArgs)
	rec := askRecord(t, database, ix.ID)
	if rec.Interaction.Deadline != nil || rec.Interaction.Reversible != nil || rec.Interaction.Questions[0].Recommend != "" {
		t.Fatalf("fields without a deadline: %+v", rec.Interaction)
	}
	raw, _ := json.Marshal(rec.Interaction)
	for _, key := range []string{`"deadline"`, `"reversible"`, `"recommend"`} {
		if strings.Contains(string(raw), key) {
			t.Fatalf("%s written without a value: %s", key, raw)
		}
	}
	todo := e.onlyAskTodo()
	e.must("POST", "/api/v1/todos/"+todo.ID+"/decisions", `{"question":"`+lateAskQuestion+`","choice":"Wait"}`)
	waitFor(t, "the answer recorded", func() bool {
		r := askRecord(t, database, ix.ID).Resolution
		return r != nil && r.Late
	})
	rec = askRecord(t, database, ix.ID)
	if r := rec.Resolution; rec.Status != "expired" || r.AnsweredBy != "" || r.Answers[lateAskQuestion] != "Wait" || r.Dismiss {
		t.Fatalf("history %s %+v", rec.Status, r)
	}
}

// LateAnswer only ever writes over a card that expired: one the user answered on the card keeps its answer.
func TestLateAnswerLeavesAnsweredCards(t *testing.T) {
	database := todoTestDB(t)
	ctx := context.Background()
	ix := proto.Interaction{ID: "ix_answered", MachineID: "server", SessionID: "c1", Kind: proto.IxQuestion, CreatedAt: time.Now().UTC()}
	if err := database.InsertInteraction(ctx, ix); err != nil {
		t.Fatal(err)
	}
	if err := database.FinishInteraction(ctx, ix.ID, "resolved", &proto.Resolution{Answers: map[string]string{"Q": "A"}}); err != nil {
		t.Fatal(err)
	}
	if ok, err := database.LateAnswer(ctx, ix.ID, map[string]string{"Q": "B"}, proto.AnsweredByDefault, "late"); err != nil || ok {
		t.Fatalf("late answer over an answered card: %v %v", ok, err)
	}
	if r := askRecord(t, database, ix.ID).Resolution; r.Late || r.AnsweredBy != "" || r.Answers["Q"] != "A" {
		t.Fatalf("history %+v", r)
	}
}

// A recommend is kept when it names one of the question's options, with or without a deadline; reversible as the
// asker said it.
func TestAskRecommendAndReversible(t *testing.T) {
	opts := []proto.QuestionOption{{Label: "A"}, {Label: "B"}}
	var a askDeadlineArgs
	_ = json.Unmarshal([]byte(`{"reversible":false,"questions":[{"recommend":" B "},{"recommend":"C"},{}]}`), &a)
	if got := []string{askRecommend(a, 0, opts), askRecommend(a, 1, opts), askRecommend(a, 2, opts), askRecommend(a, 3, opts)}; strings.Join(got, ",") != "B,,," {
		t.Fatalf("recommends %q", got)
	}
	var ix proto.Interaction
	keepAskDecision(&ix, a, askDeadline{})
	if ix.Deadline != nil || ix.Reversible == nil || *ix.Reversible {
		t.Fatalf("no deadline, reversible false: %+v %+v", ix.Deadline, ix.Reversible)
	}
	ix = proto.Interaction{}
	keepAskDecision(&ix, askDeadlineArgs{}, askDeadline{})
	if ix.Deadline != nil || ix.Reversible != nil {
		t.Fatalf("nothing said: %+v %+v", ix.Deadline, ix.Reversible)
	}
}

// A task's needs_user decision is no card: its review on the to-do keeps the recommendation, the deadline and whether it
// can be undone, so they can still be read once it is decided.
func TestNeedsUserReviewKeepsTheDecisionFields(t *testing.T) {
	for _, c := range []struct {
		args       map[string]any
		deadline   bool
		reversible *bool
	}{
		{map[string]any{"deadline": "2h", "reversible": true}, true, new(true)},
		{map[string]any{"deadline": "2h", "reversible": false}, true, new(false)},
		{map[string]any{}, false, nil},
	} {
		s, f, todo, _, _ := conclusionService(t, false)
		waitsForReview(t, s, f, todo.ID, "两种走法：A 先灰度，B 直接全量。")
		args := map[string]any{"verdict": "needs_user", "reason": "先灰度还是直接全量", "recommend": "先灰度 10%"}
		for k, v := range c.args {
			args[k] = v
		}
		reviewTask(t, s, todo.ID, args)
		reviews := todoNow(s, todo.ID).Supervision.Reviews
		r := reviews[len(reviews)-1]
		if r.Verdict != reviewNeedsUser || r.Recommend != "先灰度 10%" || (r.Deadline != nil) != c.deadline {
			t.Fatalf("%v: review %+v", c.args, r)
		}
		if (r.Reversible == nil) != (c.reversible == nil) || r.Reversible != nil && *r.Reversible != *c.reversible {
			t.Fatalf("%v: reversible %v", c.args, r.Reversible)
		}
	}
}
