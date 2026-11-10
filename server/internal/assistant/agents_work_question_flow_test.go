package assistant

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/db"
	"github.com/nwverse/persona/server/internal/proto"
)

type workQuestionFlowStore struct {
	*fleetMem
	work *failingDeferredWorkStore
}

func (s *workQuestionFlowStore) PutSessionWork(ctx context.Context, w db.SessionWork) error {
	return s.work.PutSessionWork(ctx, w)
}
func (s *workQuestionFlowStore) SessionWorks(ctx context.Context, since time.Time) ([]db.SessionWork, error) {
	return s.work.SessionWorks(ctx, since)
}
func (s *workQuestionFlowStore) PruneSessionWork(ctx context.Context, before time.Time) error {
	return s.work.PruneSessionWork(ctx, before)
}
func (s *workQuestionFlowStore) AddWorkEvent(ctx context.Context, e db.WorkEvent) error {
	return s.work.AddWorkEvent(ctx, e)
}
func (s *workQuestionFlowStore) WorkEvents(ctx context.Context, since time.Time) ([]db.WorkEvent, error) {
	return s.work.WorkEvents(ctx, since)
}

func waitWorkQuestionDelivered(t *testing.T, s *Service, id string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		s.fleet.askMu.Lock()
		q := s.fleet.asked[id]
		done := q != nil && !q.ended.IsZero()
		s.fleet.askMu.Unlock()
		if done {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("owned question was not delivered to its owner")
}

func TestWorkQuestionCancelAndPauseThroughPhoneLifecycle(t *testing.T) {
	for _, tc := range []struct {
		name       string
		resolution proto.Resolution
	}{
		{"cancel", proto.Resolution{Cancel: true}},
		{"defer", proto.Resolution{Answers: map[string]string{"Continue now?": "Later"}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			work := &failingDeferredWorkStore{}
			s, f, ix := deferredCardFixture(t, work)
			s.waitWorkEvents()
			store := &workQuestionFlowStore{fleetMem: newFleetMem(), work: work}
			s.store = func() Store { return store }
			if err := s.putTriageSwitches(context.Background(), TriageSwitches{Enabled: true, Decide: true}); err != nil {
				t.Fatal(err)
			}
			ft := &fleetTest{t: t, s: s, f: f}
			s.askOnPhone([]proto.Interaction{ix})
			if pending := ft.waitPending(1); pending[0].ID != ix.ID || !pending[0].CanCancel {
				t.Fatalf("wrong pending card: %+v", pending)
			}
			resolveWorkQuestionHTTP(t, s, ix.ID, tc.resolution)
			waitWorkQuestionDelivered(t, s, ix.ID)
			ft.waitPending(0)
			status, outcome, err := store.InteractionOutcome(context.Background(), ix.ID)
			if err != nil || outcome == nil || outcome.Cancel != tc.resolution.Cancel {
				t.Fatalf("history outcome: %s %+v %v", status, outcome, err)
			}
			if tc.resolution.Cancel && (status != "cancelled" || len(outcome.Answers) != 0) {
				t.Fatalf("cancellation wrote an answer: %s %+v", status, outcome)
			}
			if !tc.resolution.Cancel && (status != "resolved" || outcome.Answers["Continue now?"] != "Later") {
				t.Fatalf("pause answer not recorded: %s %+v", status, outcome)
			}
			if !entryOf(t, s, "s1").Work.Deferred || len(f.sentToAgents()) != 0 {
				t.Fatal("phone response did not finalize without replying")
			}
			s.askOnPhone(nil)
			// A stale snapshot must replay the stored outcome to Action, not recreate pending UI.
			s.askOnPhone([]proto.Interaction{ix})
			waitWorkQuestionDelivered(t, s, ix.ID)
			ft.waitPending(0)
			s.askOnPhone(nil)
			s.waitWorkEvents()
			s = newServiceOn(t, f, store)
			s.workTick(context.Background(), time.Now())
			if len(s.workCards()) != 0 {
				t.Fatal("restart restored finalized work card")
			}
			s.askOnPhone([]proto.Interaction{ix})
			waitWorkQuestionDelivered(t, s, ix.ID)
			if len(s.pendingInteractions()) != 0 {
				t.Fatal("restart replay reopened old question")
			}
			s.askOnPhone(nil)
			c := stopped("s1", "Continue now?")
			c.UpdatedAt = time.Now().UTC()
			f.setSessions("box", c)
			triage(t, s, "s1")
			cards := s.workCards()
			if len(cards) != 1 || cards[0].ID == ix.ID {
				t.Fatal("new turn's question was swallowed")
			}
			s.askOnPhone(cards)
			ft.s = s
			ft.waitPending(1)
			resolveWorkQuestionHTTP(t, s, cards[0].ID, proto.Resolution{Answers: map[string]string{"Continue now?": "Continue"}})
			waitWorkQuestionDelivered(t, s, cards[0].ID)
			if len(f.sentToAgents()) != 1 {
				t.Fatal("normal answer did not reach the agent exactly once")
			}
			s.askOnPhone(nil)
			s.waitWorkEvents()
		})
	}
}

func resolveWorkQuestionHTTP(t *testing.T, s *Service, id string, resolution proto.Resolution) {
	t.Helper()
	body, err := json.Marshal(resolution)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/api/v1/interactions/"+id+"/resolve", bytes.NewReader(body))
	response := httptest.NewRecorder()
	s.fleetHandler(http.NotFoundHandler()).ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("resolve HTTP %d: %s", response.Code, response.Body.String())
	}
}
