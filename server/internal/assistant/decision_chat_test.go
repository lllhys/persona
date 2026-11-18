package assistant

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/db"
	"github.com/nwverse/persona/server/internal/i18n"
)

func TestDecisionChatAnswerSettlesOnlyItsOriginalRound(t *testing.T) {
	database := todoTestDB(t)
	s, f, todo := newDecisionAnswerServiceOn(t, database)
	waitsForReview(t, s, f, todo.ID, "Choose a rollout plan.")
	if _, err := reviewErr(s, todo.ID, rolloutDecision()); err != nil {
		t.Fatal(err)
	}
	review := currentDecisionReview(todoNow(s, todo.ID))
	if _, err := s.changeDecisionDismissal(context.Background(), todo.ID, review.ReviewAt, "", false); err != nil {
		t.Fatal(err)
	}
	before := s.reviewDismissals.byTodo[todo.ID]
	asked := askOf(s, todo.ID).AskedAt
	// A cached Web tab can answer immediately after the service restarts, before it lists to-dos again.
	s = newServiceOn(t, f, database)
	shortSupervise(t, s)
	s.superviseTimes = superviseTimings{gather: time.Hour, every: time.Hour, urgent: time.Hour}
	reply := func(round string) *httptest.ResponseRecorder {
		raw, _ := json.Marshal(map[string]string{"todo_id": todo.ID, "text": "Use the alternative with a smaller limit.", "asked_at": round})
		w := httptest.NewRecorder()
		s.ServeHTTP(w, httptest.NewRequest("POST", "/api/v1/assistant/conversations/conv1/route", strings.NewReader(string(raw))))
		return w
	}
	sent := len(sentToAgent(f))
	if _, err := s.routeDecisionAnswer(context.Background(), "other-conversation", todo.ID, asked.Format(timeKeyLayout), "Wrong task."); err == nil {
		t.Fatal("a different conversation answered this task's decision")
	}
	if w := reply(asked.Add(-time.Second).Format(timeKeyLayout)); w.Code != http.StatusConflict {
		t.Fatalf("stale round: %d %s", w.Code, w.Body.String())
	}
	if len(sentToAgent(f)) != sent || listedAsk(t, s, todo.ID) == nil {
		t.Fatal("a stale round sent an answer or removed the card")
	}
	// Load the dismissal book as a restarted service would, even if the stale answer never needed it.
	if _, err := s.decisionDismissed(context.Background(), todoNow(s, todo.ID)); err != nil {
		t.Fatal(err)
	}
	assertAnswerDismissal(t, s, database, todo.ID, before)
	if w := reply(asked.Format(timeKeyLayout)); w.Code != http.StatusOK {
		t.Fatalf("chat answer: %d %s", w.Code, w.Body.String())
	}
	rows, err := database.ListTodoDecisions(context.Background(), todo.ID, false)
	if err != nil || len(rows) != 1 || rows[0].Source != db.DecisionManual ||
		rows[0].Question != rolloutDecision()["reason"] ||
		rows[0].Choice != "Use the alternative with a smaller limit." || rows[0].SourceMessage == "" {
		t.Fatalf("task decision: %+v, %v", rows, err)
	}
	if listedAsk(t, s, todo.ID) != nil || todoNow(s, todo.ID).Supervision.State == reviewNeedsUser {
		t.Fatal("the answered decision is still waiting for the owner")
	}
	if len(sentToAgent(f)) != sent+1 {
		t.Fatal("the answer was not delivered exactly once")
	}
	before.Undone = true
	assertAnswerDismissal(t, s, database, todo.ID, before)
	if w := reply(asked.Format(timeKeyLayout)); w.Code != http.StatusConflict || len(sentToAgent(f)) != sent+1 {
		t.Fatalf("a replay answered twice: %d", w.Code)
	}
}

func TestOrdinaryTaskChatDoesNotSettleADecision(t *testing.T) {
	database := todoTestDB(t)
	s, f, todo := newDecisionAnswerServiceOn(t, database)
	waitsForReview(t, s, f, todo.ID, "Choose a rollout plan.")
	if _, err := reviewErr(s, todo.ID, rolloutDecision()); err != nil {
		t.Fatal(err)
	}
	if _, err := s.routeToTask(context.Background(), "conv1", todo.ID, "Please explain the trade-offs first."); err != nil {
		t.Fatal(err)
	}
	rows, err := database.ListTodoDecisions(context.Background(), todo.ID, false)
	if err != nil || len(rows) != 0 || listedAsk(t, s, todo.ID) == nil {
		t.Fatalf("ordinary discussion was treated as a decision: %+v, %v", rows, err)
	}
}

func TestDecisionChatFailedDeliveryKeepsTheCard(t *testing.T) {
	database := todoTestDB(t)
	s, f, todo := newSupervisedServiceOn(t, database)
	waitsForReview(t, s, f, todo.ID, "Choose a rollout plan.")
	if _, err := reviewErr(s, todo.ID, rolloutDecision()); err != nil {
		t.Fatal(err)
	}
	review := currentDecisionReview(todoNow(s, todo.ID))
	if _, err := s.changeDecisionDismissal(context.Background(), todo.ID, review.ReviewAt, "", false); err != nil {
		t.Fatal(err)
	}
	before := s.reviewDismissals.byTodo[todo.ID]
	f.mu.Lock()
	f.failSend = errors.New("test delivery refused")
	f.mu.Unlock()
	_, err := s.routeDecisionAnswer(context.Background(), "conv1", todo.ID, askOf(s, todo.ID).AskedAt.Format(timeKeyLayout), "Use the alternative.")
	if err == nil {
		t.Fatal("failed delivery was reported as answered")
	}
	rows, err := database.ListTodoDecisions(context.Background(), todo.ID, false)
	if err != nil || len(rows) != 0 || listedAsk(t, s, todo.ID) == nil ||
		todoNow(s, todo.ID).Supervision.State != reviewNeedsUser {
		t.Fatalf("failed delivery settled the decision: %+v, %v", rows, err)
	}
	assertAnswerDismissal(t, s, database, todo.ID, before)
}

func TestDecisionChatEndedAskKeepsDismissal(t *testing.T) {
	for _, kind := range []string{"accepted", "reopened", "closed", "ended review"} {
		t.Run(kind, func(t *testing.T) {
			database := todoTestDB(t)
			s, f, todo := newDecisionAnswerServiceOn(t, database)
			waitsForReview(t, s, f, todo.ID, "Choose a rollout plan.")
			if _, err := reviewErr(s, todo.ID, rolloutDecision()); err != nil {
				t.Fatal(err)
			}
			review := currentDecisionReview(todoNow(s, todo.ID))
			if _, err := s.changeDecisionDismissal(context.Background(), todo.ID, review.ReviewAt, "", false); err != nil {
				t.Fatal(err)
			}
			before := s.reviewDismissals.byTodo[todo.ID]
			asked := askOf(s, todo.ID).AskedAt
			if kind == "accepted" {
				s.accept.mu.Lock()
				s.accept.asks[todo.ID].AcceptedAt = time.Now()
				s.accept.mu.Unlock()
			} else {
				s.todos.mu.Lock()
				switch kind {
				case "reopened":
					at := asked.Add(time.Second)
					s.todos.todos[todo.ID].ReopenedAt = &at
				case "closed":
					at := time.Now()
					s.todos.todos[todo.ID].DoneAt = &at
				case "ended review":
					s.todos.todos[todo.ID].Supervision.State = reviewRework
				}
				s.todos.mu.Unlock()
			}
			sent := len(sentToAgent(f))
			_, err := s.routeDecisionAnswer(context.Background(), "conv1", todo.ID, asked.Format(timeKeyLayout), "Use the alternative.")
			if err == nil || err.Error() != i18n.Err("assistant-feishu_cards.already_handled").Error() {
				t.Fatalf("ended ask: %v", err)
			}
			assertAnswerDismissal(t, s, database, todo.ID, before)
			if len(sentToAgent(f)) != sent {
				t.Fatal("ended ask delivered an answer")
			}
		})
	}
}

func TestDecisionChatCheckedRoutingKeepsDismissal(t *testing.T) {
	database := todoTestDB(t)
	s, f, todo := newDecisionAnswerServiceOn(t, database)
	waitsForReview(t, s, f, todo.ID, "Choose a rollout plan.")
	if _, err := reviewErr(s, todo.ID, rolloutDecision()); err != nil {
		t.Fatal(err)
	}
	review := currentDecisionReview(todoNow(s, todo.ID))
	if _, err := s.changeDecisionDismissal(context.Background(), todo.ID, review.ReviewAt, "", false); err != nil {
		t.Fatal(err)
	}
	before := s.reviewDismissals.byTodo[todo.ID]
	asked := askOf(s, todo.ID).AskedAt
	h := &conclusionLookupHub{fakeHub: f, entered: make(chan struct{}), release: make(chan struct{})}
	setHub(s, h)
	ctx, cancel := context.WithTimeout(context.WithValue(context.Background(), conclusionLookupKey{}, true), 5*time.Second)
	defer cancel()
	done := make(chan error, 1)
	sent := len(sentToAgent(f))
	go func() {
		_, err := s.routeDecisionAnswer(ctx, "conv1", todo.ID, asked.Format(timeKeyLayout), "Use the alternative.")
		done <- err
	}()
	select {
	case <-h.entered:
	case <-ctx.Done():
		t.Fatal("answer did not reach session lookup")
	}
	s.todos.mu.Lock()
	at := asked.Add(time.Second)
	s.todos.todos[todo.ID].ReopenedAt = &at
	s.todos.mu.Unlock()
	close(h.release)
	select {
	case err := <-done:
		if !errors.Is(err, db.ErrTodoCompleted) {
			t.Fatalf("checked routing: %v", err)
		}
	case <-ctx.Done():
		t.Fatal("answer did not return after session lookup")
	}
	assertAnswerDismissal(t, s, database, todo.ID, before)
	if len(sentToAgent(f)) != sent {
		t.Fatal("checked routing sent a stale answer")
	}
}
