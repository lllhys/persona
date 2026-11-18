package assistant

import (
	"context"
	"errors"
	"time"

	"github.com/nwverse/persona/server/internal/assistantwire"
	"github.com/nwverse/persona/server/internal/db"
	"github.com/nwverse/persona/server/internal/i18n"
)

// What a call's NextAt is for.
const (
	stepRingTimeout = "ring_timeout" // ringing, and no phone said what came of it
	stepRetry       = "retry"        // missed once: ring again
	stepRemind      = "remind"       // missed twice (or declined): push the matter again
)

// urgentIdle is the longest the loop sleeps without looking (a second manager's or a restart's rows).
const urgentIdle = time.Minute

var (
	errCallNotFound = errors.New("no such call")
	errCallState    = errors.New("the call is past that")
)

// wakeUrgent makes the loop look at the calls now (a new one rang).
func (s *Service) wakeUrgent() {
	select {
	case s.urgentWake <- struct{}{}:
	default:
	}
}

// urgentToolsChanged tells the hub the assistant's tools changed (urgent_call comes with the switch): it asks for
// them again, and conversations the node starts from then on list them.
func (s *Service) urgentToolsChanged() {
	if err := s.hub.Tell(assistantwire.EventToolsChanged, map[string]any{}); err != nil {
		s.logf("assistant: %s: %v", assistantwire.EventToolsChanged, err)
	}
}

// urgentLoop acts on the calls when their next step is due: a ring nobody reported on, the retry after a missed one,
// the reminders after two. The rows say what is due, so a restart picks up where it was.
func (s *Service) urgentLoop(ctx context.Context) {
	for {
		wait := urgentIdle
		if next := s.urgentDue(ctx, time.Now()); !next.IsZero() {
			wait = min(wait, max(time.Until(next), 50*time.Millisecond))
		}
		t := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			t.Stop()
			return
		case <-s.urgentWake:
			t.Stop()
		case <-t.C:
		}
	}
}

// urgentDue runs the steps due by now and returns when the next one is (zero: none known).
func (s *Service) urgentDue(ctx context.Context, now time.Time) time.Time {
	st := s.urgentStore()
	if st == nil || s.hello(ctx).Assistant.Standby {
		return time.Time{}
	}
	due, next, err := st.UrgentCallsDue(ctx, now)
	if err != nil {
		s.logf("assistant: urgent calls: %v", err)
		return time.Time{}
	}
	for _, c := range due {
		s.urgentStep(ctx, st, c, now)
	}
	if len(due) > 0 {
		return now // what a step scheduled may be due already
	}
	return next
}

// urgentStep runs c's due step once: the row's version is the claim, so a second manager or a phone's report at the
// same moment does not act on it too.
func (s *Service) urgentStep(ctx context.Context, st urgentStore, c db.UrgentCall, now time.Time) {
	switch c.Step {
	case stepRingTimeout:
		if c.State == callRinging {
			s.urgentMissed(ctx, st, c, now)
			return
		}
	case stepRetry:
		if c.State == callMissed {
			s.urgentRetry(ctx, st, c, now)
			return
		}
	case stepRemind:
		s.urgentRemind(ctx, st, c, now)
		return
	}
	c.NextAt, c.Step = nil, "" // nothing to do for this state
	s.saveCall(ctx, st, c)
}

// saveCall writes c; false when it changed meanwhile (the other change stands).
func (s *Service) saveCall(ctx context.Context, st urgentStore, c db.UrgentCall) bool {
	ok, err := st.UpdateUrgentCall(ctx, c)
	if err != nil {
		s.logf("assistant: urgent call %s: %v", c.ID, err)
	}
	return ok && err == nil
}

// urgentMissed: nobody answered c's ring. The first ring of a matter rings again after the retry wait; the second
// falls back to a P0 push and reminders. A test call just ends.
func (s *Service) urgentMissed(ctx context.Context, st urgentStore, c db.UrgentCall, now time.Time) {
	c.State = callMissed
	switch {
	case c.Why == urgentWhyTest:
		c.NextAt, c.Step = nil, ""
		s.saveCall(ctx, st, c)
	case c.Attempt == 1:
		set, _ := s.urgentSettings(ctx)
		at := now.Add(time.Duration(max(set.RetryMinutes, 1)) * time.Minute)
		c.NextAt, c.Step = &at, stepRetry
		s.saveCall(ctx, st, c)
	default:
		at := now.Add(urgentRemindFirst)
		c.NextAt, c.Step = &at, stepRemind
		if s.saveCall(ctx, st, c) {
			title, body := missedText(s.lang(ctx), c)
			s.urgentPush(ctx, c, title, body)
		}
	}
	s.urgentBroadcast(ctx, c)
}

// urgentRetry rings a missed matter once more, unless it was settled meanwhile (its card answered) or the owner
// turned calls off; then the matter just goes as a push.
func (s *Service) urgentRetry(ctx context.Context, st urgentStore, c db.UrgentCall, now time.Time) {
	if c.InteractionID != "" {
		if _, pending := s.pendingIx(c.InteractionID); !pending {
			c.State, c.NextAt, c.Step = callCancelled, nil, ""
			s.saveCall(ctx, st, c)
			s.urgentBroadcast(ctx, c)
			return
		}
	}
	set, _ := s.urgentSettings(ctx)
	if !set.Enabled || set.Quiet.inQuiet(now.In(s.location(ctx))) {
		at := now.Add(urgentRemindFirst)
		c.NextAt, c.Step = &at, stepRemind
		if s.saveCall(ctx, st, c) {
			title, body := missedText(s.lang(ctx), c)
			s.urgentPush(ctx, c, title, body)
		}
		return
	}
	timeout := now.Add(urgentRingTimeout)
	c.State, c.Attempt, c.RangAt, c.NextAt, c.Step = callRinging, 2, &now, &timeout, stepRingTimeout
	if !s.saveCall(ctx, st, c) {
		return
	}
	c.Version++
	if res := s.ring(ctx, c); res.Devices == 0 {
		s.urgentMissed(ctx, st, c, now) // the second ring went nowhere: as missed, the push and reminders
	}
}

// urgentRemind pushes a missed matter again (after 10 and 30 minutes) until it is settled.
func (s *Service) urgentRemind(ctx context.Context, st urgentStore, c db.UrgentCall, now time.Time) {
	if c.InteractionID != "" {
		if _, pending := s.pendingIx(c.InteractionID); !pending {
			c.NextAt, c.Step = nil, ""
			s.saveCall(ctx, st, c)
			return
		}
	}
	c.Reminders++
	if c.Reminders >= urgentReminderLimit {
		c.NextAt, c.Step = nil, ""
	} else {
		at := now.Add(urgentRemindSecond)
		c.NextAt = &at
	}
	if s.saveCall(ctx, st, c) {
		title, body := missedText(s.lang(ctx), c)
		s.urgentPush(ctx, c, title, body)
	}
}

// callReport is what a phone said about a call.
type callReport struct {
	state, outcome, transcript string
	seconds                    int
}

// reportCall applies a phone's report to call id; errCallState when the call is past it (another phone answered, the
// manager counted it missed).
func (s *Service) reportCall(ctx context.Context, st urgentStore, id string, r callReport, now time.Time) (db.UrgentCall, error) {
	for range 3 { // a change that lost to another is read again
		c, ok, err := st.UrgentCall(ctx, id)
		if err != nil {
			return c, err
		}
		if !ok {
			return c, errCallNotFound
		}
		next, push, err := callAfter(c, r, now)
		if err != nil {
			return c, err
		}
		if next == nil { // nothing changes (the same report again)
			return c, nil
		}
		if r.state == callMissed {
			// as the loop's missed: the retry, or the push and reminders
			s.urgentMissed(ctx, st, c, now)
			c, _, err = st.UrgentCall(ctx, id)
			return c, err
		}
		if !s.saveCall(ctx, st, *next) {
			continue
		}
		next.Version++
		if push {
			l := s.lang(ctx)
			s.urgentPush(ctx, *next, i18n.T(l, "assistant-urgent_call.declined_title", i18n.Args{"title": next.Title}), next.Body)
		}
		if r.state == callEnded {
			s.workEvent(next.MachineID, next.Conversation, "urgent_call_ended", map[string]any{"call": next.ID, "seconds": next.Seconds})
		}
		s.urgentBroadcast(ctx, *next)
		return *next, nil
	}
	return db.UrgentCall{}, errCallState
}

// callAfter is c after report r (nil: no change), and whether the matter is pushed now (declined: the owner still has
// to settle it).
func callAfter(c db.UrgentCall, r callReport, now time.Time) (*db.UrgentCall, bool, error) {
	switch r.state {
	case callAnswered:
		if c.State == callAnswered {
			return nil, false, nil
		}
		if c.State != callRinging {
			return nil, false, errCallState
		}
		c.State, c.AnsweredAt, c.NextAt, c.Step = callAnswered, &now, nil, ""
		return &c, false, nil
	case callDeclined:
		if c.State == callDeclined {
			return nil, false, nil
		}
		if c.State != callRinging {
			return nil, false, errCallState
		}
		c.State, c.NextAt, c.Step = callDeclined, nil, ""
		return &c, c.Why != urgentWhyTest, nil
	case callMissed:
		if c.State != callRinging {
			return nil, false, nil // already counted missed, answered elsewhere, cancelled
		}
		return &c, false, nil
	case callEnded:
		if c.State == callEnded {
			return nil, false, nil
		}
		if c.State != callAnswered && c.State != callRinging {
			return nil, false, errCallState
		}
		if c.AnsweredAt == nil {
			c.AnsweredAt = &now
		}
		c.State, c.EndedAt, c.NextAt, c.Step = callEnded, &now, nil, ""
		c.Seconds, c.Outcome, c.Transcript = r.seconds, r.outcome, r.transcript
		return &c, false, nil
	case callSeen:
		if c.Step != stepRemind {
			return nil, false, nil
		}
		c.NextAt, c.Step = nil, ""
		return &c, false, nil
	}
	return nil, false, errCallState
}

// urgentBroadcast tells the apps where a call is: a phone still ringing for it stops when it was answered, declined,
// missed or cancelled elsewhere.
func (s *Service) urgentBroadcast(ctx context.Context, c db.UrgentCall) {
	s.event(ctx, map[string]any{"type": "urgent_call", "id": c.ID, "state": c.State, "conversation": c.Conversation})
}

// urgentCardDone: card id is over (answered anywhere, withdrawn, expired). A call about it that still rings or waits
// for its retry is cancelled, and its reminders stop.
func (s *Service) urgentCardDone(id string) {
	st := s.urgentStore()
	if st == nil || id == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	calls, err := st.UrgentCallsSince(ctx, time.Now().Add(-48*time.Hour))
	if err != nil {
		return
	}
	for _, c := range calls {
		if c.InteractionID != id || c.NextAt == nil {
			continue
		}
		if c.State == callRinging || c.State == callMissed && c.Step == stepRetry {
			c.State = callCancelled
		}
		c.NextAt, c.Step = nil, ""
		if s.saveCall(ctx, st, c) {
			s.urgentBroadcast(ctx, c)
		}
	}
}
