package assistant

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/assistantwire"
	"github.com/nwverse/persona/server/internal/db"
)

func TestExactDecisionOption(t *testing.T) {
	options := []DecisionOption{{Label: "Small rollout"}, {Label: "Full rollout"}}
	for _, tc := range []struct {
		name, text, want string
	}{
		{"first letter", "A", "Small rollout"},
		{"second letter", "B", "Full rollout"},
		{"lowercase letter", "b", "Full rollout"},
		{"letter punctuation", "\u300c B!\u300d", "Full rollout"},
		{"full label", "Full rollout", "Full rollout"},
		{"label spacing", " Full \trollout! ", "Full rollout"},
		{"label punctuation", "Full-rollout", "Full rollout"},
		{"incomplete label", "Full", ""},
		{"label case is exact", "full rollout", ""},
		{"prose", "I choose B", ""},
		{"two letters", "A/B", ""},
		{"negation", "Not B, A", ""},
		{"out of range", "C", ""},
		{"number", "2", ""},
		{"empty", " \t\n", ""},
		{"punctuation only", "!?...", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := exactDecisionOption(tc.text, options); got != tc.want {
				t.Fatalf("exactDecisionOption(%q) = %q, want %q", tc.text, got, tc.want)
			}
		})
	}
	t.Run("four options preserve displayed order", func(t *testing.T) {
		four := append(options, DecisionOption{Label: "Wait"}, DecisionOption{Label: "Cancel"})
		if got := exactDecisionOption("D.", four); got != "Cancel" {
			t.Fatalf("fourth letter chose %q", got)
		}
	})
	for _, tc := range []struct {
		name, text string
		options    []DecisionOption
	}{
		{"duplicate normalized labels", "Ship now", []DecisionOption{{Label: "Ship now"}, {Label: "Ship-now"}}},
		{"letter conflicts with label", "A", []DecisionOption{{Label: "B"}, {Label: "A"}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := exactDecisionOption(tc.text, tc.options); got != "" {
				t.Fatalf("ambiguous answer %q chose %q", tc.text, got)
			}
		})
	}
}

func agentChoiceMessage(t *testing.T, s *Service, r SessionRef, text string) {
	t.Helper()
	raw, err := json.Marshal(assistantwire.UserSpokeParams{SessionRef: r, Text: text})
	if err != nil {
		t.Fatal(err)
	}
	b := &BusHub{svc: s}
	if err := b.event(context.Background(), busEvent{name: assistantwire.EventUserSpoke, params: raw}); err != nil {
		t.Fatal(err)
	}
}

func TestAgentDecisionExactChoiceSettlesWithoutSupervisor(t *testing.T) {
	for _, letter := range []string{"A", "B"} {
		t.Run(letter, func(t *testing.T) {
			database := todoTestDB(t)
			s, f, todo, _, _ := conclusionServiceOn(t, database, false)
			waitsForReview(t, s, f, todo.ID, "The owner needs to choose a rollout.")
			reviewTask(t, s, todo.ID, rolloutDecision())
			a := askOf(s, todo.ID)
			sent := len(sentToAgent(f))
			ref := SessionRef{MachineID: a.MachineID, SessionID: a.SessionID}
			agentChoiceMessage(t, s, ref, letter)
			got := askOf(s, todo.ID)
			if got.AgentReply == nil || got.DueAt.IsZero() || got.AcceptedAt.IsZero() || got.ByDefault ||
				!got.AgentReply.ToldAt.IsZero() || todoNow(s, todo.ID).Supervision.State == reviewNeedsUser {
				t.Fatalf("the exact choice still needs a supervisor: %+v", got)
			}
			index := int(letter[0] - 'A')
			want := a.Options[index].Label
			rows, err := database.ListTodoDecisions(context.Background(), todo.ID, true)
			if err != nil || len(rows) != 1 || rows[0].Choice != want || rows[0].Reason != letter ||
				rows[0].Source != db.DecisionManual || rows[0].SourceMachine != ref.MachineID || rows[0].SourceSession != ref.SessionID {
				t.Fatalf("the owner's exact choice/source was not recorded: %+v, %v", rows, err)
			}
			agentChoiceMessage(t, s, ref, letter)
			s.sweepDecisionDeadlines(time.Now().Add(48 * time.Hour))
			if result, err := s.acceptTryAs(context.Background(), todo.ID, "", true); err != nil || result.taken {
				t.Fatalf("the default could still run: %+v %v", result, err)
			}
			rows, err = database.ListTodoDecisions(context.Background(), todo.ID, true)
			if err != nil || len(rows) != 1 || len(sentToAgent(f)) != sent {
				t.Fatalf("the answer was recorded or sent again: rows %+v, err %v, sends %q", rows, err, sentToAgent(f)[sent:])
			}
			var stored map[string]*acceptAsk
			if _, err := database.Setting(context.Background(), acceptanceKey, &stored); err != nil {
				t.Fatal(err)
			}
			persisted := stored[todo.ID]
			if persisted == nil || persisted.AgentReply == nil || persisted.AcceptedAt.IsZero() || persisted.DueAt.IsZero() || persisted.ByDefault {
				t.Fatalf("the settled choice was not persisted: %+v", persisted)
			}
			reloaded, nextHub := newQuietServiceOn(t, database)
			if err := reloaded.ensureTodosLoaded(context.Background()); err != nil {
				t.Fatal(err)
			}
			reloaded.sweepDecisionDeadlines(time.Now().Add(48 * time.Hour))
			again, ok := reloaded.askCopy(todo.ID)
			if !ok || again.AcceptedAt.IsZero() || again.ByDefault || len(sentToAgent(nextHub)) != 0 {
				t.Fatalf("the settled choice defaulted after reload: %+v", again)
			}
		})
	}
}

func TestAgentDecisionExactLabelSettlesAfterEarlierUnclearReply(t *testing.T) {
	for _, first := range []string{"", "I have a question about the options."} {
		t.Run(first, func(t *testing.T) {
			s, _, todo := decisionAgentReplyService(t)
			a := askOf(s, todo.ID)
			ref := SessionRef{MachineID: a.MachineID, SessionID: a.SessionID}
			agentChoiceMessage(t, s, ref, first)
			if got := askOf(s, todo.ID); got.AgentReply == nil || !got.AcceptedAt.IsZero() {
				t.Fatalf("the unclear reply was not left paused: %+v", got)
			}
			label := a.Options[1].Label
			agentChoiceMessage(t, s, ref, "\u300c "+label+"!\u300d")
			if got := askOf(s, todo.ID); got.AcceptedAt.IsZero() || got.Chosen != label || got.ByDefault {
				t.Fatalf("a later exact label did not settle the decision: %+v", got)
			}
			if todoNow(s, todo.ID).Supervision.State == reviewNeedsUser {
				t.Fatal("the later exact label still waits for a supervisor")
			}
		})
	}
}

func TestAgentDecisionUnclearTextDoesNotChoose(t *testing.T) {
	for _, text := range []string{"I choose B", "A/B", "Not A", "Please explain the full rollout."} {
		t.Run(text, func(t *testing.T) {
			s, _, todo := decisionAgentReplyService(t)
			a := askOf(s, todo.ID)
			agentChoiceMessage(t, s, SessionRef{MachineID: a.MachineID, SessionID: a.SessionID}, text)
			if got := askOf(s, todo.ID); got.AgentReply == nil || got.DueAt.IsZero() || !got.AcceptedAt.IsZero() ||
				got.Chosen != "" || got.ByDefault || todoNow(s, todo.ID).Supervision.State != reviewNeedsUser {
				t.Fatalf("unclear text was guessed or left the default enabled: %+v", got)
			}
		})
	}
}

type failingAgentDecisionStore struct{ *db.DB }

func (s failingAgentDecisionStore) InsertTodoDecision(context.Context, db.TodoDecisionRow) (bool, error) {
	return false, errors.New("decision write unavailable")
}

func TestAgentDecisionWriteFailureKeepsTheAskPaused(t *testing.T) {
	database := todoTestDB(t)
	s, f, todo, _, _ := conclusionServiceOn(t, failingAgentDecisionStore{database}, false)
	waitsForReview(t, s, f, todo.ID, "The owner needs to choose a rollout.")
	reviewTask(t, s, todo.ID, rolloutDecision())
	a := askOf(s, todo.ID)
	agentChoiceMessage(t, s, SessionRef{MachineID: a.MachineID, SessionID: a.SessionID}, "B")
	if got := askOf(s, todo.ID); got.AgentReply == nil || got.DueAt.IsZero() || !got.AcceptedAt.IsZero() || got.ByDefault ||
		todoNow(s, todo.ID).Supervision.State != reviewNeedsUser {
		t.Fatalf("a failed decision write ended the ask: %+v", got)
	}
	if rows, err := database.ListTodoDecisions(context.Background(), todo.ID, true); err != nil || len(rows) != 0 {
		t.Fatalf("unexpected stored decision: %+v %v", rows, err)
	}
	waitFor(t, "the recoverable supervisor handoff", func() bool {
		return strings.Contains(strings.Join(f.notices(), "\n"), "task_decided")
	})
}
