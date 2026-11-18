package assistant

import (
	"context"
	"strings"
)

// An ask_user whose card expired unanswered becomes a to-do (ask_todo.go): the user may answer it there later, or, past
// a reversible deadline, its recommended options are taken by default (decision_deadline.go). Either way the card's
// history says "expired" and, from here, what was answered after it (proto.Resolution.Late), so a reader of the history
// tells the user's own answer from one taken by default (docs/protocol/protocol.md "Who answered a card").

// recordLateAnswer records on question to-do a's card the answers given after it expired (by question as asked): by
// the user (by "") or by default (proto.AnsweredByDefault, with reason). Best effort, within historyTimeout.
func (s *Service) recordLateAnswer(a askTodo, choices []askChoice, by, reason string) {
	st, ok := s.fleetStore().(lateAnswerer)
	if !ok || a.Interaction == "" || len(choices) == 0 {
		return
	}
	answers := map[string]string{}
	for _, c := range choices {
		q := askedQuestion(a, c.Question)
		if q == "" || strings.TrimSpace(c.Choice) == "" {
			continue
		}
		answers[q] = c.Choice
	}
	if len(answers) == 0 {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), historyTimeout)
	defer cancel()
	if _, err := st.LateAnswer(ctx, a.Interaction, answers, by, reason); err != nil {
		s.logf("ask_user %s: record the answer after expiry: %v", a.Interaction, err)
	}
}

// askedQuestion is the question of a that q names: as asked, or as a decision clipped it (decisionText); "" for none.
func askedQuestion(a askTodo, q string) string {
	q = strings.TrimSpace(q)
	for _, asked := range a.Questions {
		if asked == q || decisionText(asked) == q {
			return asked
		}
	}
	return ""
}
