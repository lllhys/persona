package assistant

import (
	"cmp"
	"context"
	"strings"
	"sync"
	"time"

	"github.com/nwverse/persona/server/internal/prompts"
	"github.com/nwverse/persona/server/internal/proto"
)

// ---- signals ----

const (
	sigReview      = "review"      // the work looks finished: review it
	sigIdle        = "idle"        // its agents stopped, and the work is not finished
	sigUnjudged    = "unjudged"    // its agents stopped, and the judge could not say whether the work is finished
	sigNeedsYou    = "needs_you"   // an agent waits for an answer
	sigStalled     = "stalled"     // working, but nothing moved for long
	sigInterrupted = "interrupted" // the model service cut its turn off
	sigFailed      = "failed"      // its agent did not start
	sigStarted     = "started"     // its agent started after the supervisor's retry (start_remedy.go)
	sigReport      = "report"      // its agent reported to the supervisor (report_to_supervisor, agent_control.go)
	sigEscalated   = "escalated"   // its leader conversation judged it needs the user (supervise_leader.go): told the main one
	sigOrphaned    = "orphaned"    // nobody works on it: its agents are closed or offline and nothing brings one back (tasks_orphan.go)
	sigAsk         = "ask"         // its agent's card came to the supervisor before the user (ask_route.go)
	// sigLeaderStuck: a leader's task is stuck (leader_reports.go): the main conversation says so in one line, no push
	// (the owner's ruling after H1, #3609). Detail is the direction's name and where it is stuck.
	sigLeaderStuck = "leader_stuck"
)

var (
	// superviseGather is how long a first signal waits for others to go out with it.
	superviseGather = 15 * time.Second
	// superviseEvery is the least time between two supervision messages; superviseUrgentEvery when one waits on a decision.
	superviseEvery       = 10 * time.Minute
	superviseUrgentEvery = 2 * time.Minute
	// superviseNeedsAfter is how long an agent waits for an answer before the supervisor hears of it (the user was
	// pushed; a quiet task's at once: the user was not).
	superviseNeedsAfter = 3 * time.Minute
	// superviseStallMin is the least time without progress that counts as stuck (or 1.5× what the task should take).
	superviseStallMin = 20 * time.Minute
	// superviseTick is how often the tasks are looked at.
	superviseTick = 30 * time.Second
	// superviseHoldMax bounds how long the main conversation's turns stay unpushed after a supervision message went in.
	superviseHoldMax = 2 * time.Hour
)

// superviseSignal is one thing about one task the supervisor should know. Key names the stretch of state it is about:
// the same signal with the same key is told once.
type superviseSignal struct {
	Todo, Kind, Key  string
	At               time.Time
	Machine, Session string
	Detail, Ask      string
	Minutes          int
	// A report's (sigReport): its kind (progress, question, blocked, done), evidence and the step it is on.
	Report      string
	Evidence    []string
	Step, Steps int
	// Orphan: why nobody works on it (sigOrphaned): orphanError, orphanClosed or orphanOffline.
	Orphan string
	// To is the conversation it goes to when that is not digestTo's to say: a card's rung (sigAsk), the main
	// conversation for a leader's escalated cards.
	To string
	// Cards: an escalated signal's cards the user was given (ask_escalate).
	Cards []prompts.SuperviseAsk
}

const (
	supervisionInfo   = "info"
	supervisionTriage = "triage"
	supervisionUser   = "user"
)

func (g superviseSignal) level() string {
	if g.Kind == sigReport && g.Report == upReportProgress {
		return supervisionInfo
	}
	return supervisionTriage
}

// id names what a newer signal replaces while it waits: the task's signal of that kind (a report: of that kind of
// report, so a progress report does not push out a question).
func (g superviseSignal) id() string {
	if g.Kind == sigReport {
		return g.Todo + "|" + g.Kind + ":" + g.Report
	}
	if g.Kind == sigAsk { // its cards with each rung are an entry of their own
		return g.Todo + "|" + g.Kind + "@" + g.To
	}
	if g.Kind == sigEscalated && len(g.Cards) > 0 { // a leader's escalated cards, apart from its needs_user
		return g.Todo + "|" + g.Kind + ":cards"
	}
	return g.Todo + "|" + g.Kind
}

// urgent: a signal that waits on a decision goes out sooner than the rest.
func (g superviseSignal) urgent() bool {
	switch g.Kind {
	case sigNeedsYou, sigIdle, sigUnjudged, sigFailed, sigInterrupted, sigStarted, sigEscalated, sigAsk, sigOrphaned: // started: the user waits to hear it
		return true
	case sigReport:
		return g.Report != upReportProgress
	}
	return false
}

type superviseBook struct {
	mu      sync.Mutex
	writeMu sync.Mutex // one ledger write at a time (updateSupervision)
	dedupMu sync.Mutex // serializes digest reservations and delivery
	dedup   map[string]superviseFingerprint
	pending map[string]superviseSignal   // by todo|kind
	sent    map[string]string            // todo|kind → the key last told
	asks    map[string]string            // todo → what its agent last asked (InteractionRaised)
	last    time.Time                    // when the last supervision message went out
	queued  map[string][]superviseSignal // conversation → the signals of the message still waiting in its queue
	timer   *time.Timer
	timerAt time.Time
	hold    map[string]time.Time // conversation → when a supervision message went in: its turn's finish is not pushed
	visible map[string]bool      // an explicit user action makes the current triage turn visible
	// failSeen: tasks whose failed start a supervision message carried, and when (start_remedy.go remedyTurnEnded)
	failSeen map[string]time.Time
	// lines are the reports being paraphrased for the Progress row (report_line.go; the tests wait on them).
	lines sync.WaitGroup
}

func (b *superviseBook) init() {
	if b.pending == nil {
		b.pending, b.sent, b.asks, b.hold = map[string]superviseSignal{}, map[string]string{}, map[string]string{}, map[string]time.Time{}
		b.visible = map[string]bool{}
	}
	if b.failSeen == nil {
		b.failSeen = map[string]time.Time{}
	}
}

// superviseTimings are how supervision messages gather and space out (Service.superviseTimes; a zero field: the
// package's superviseGather, superviseEvery or superviseUrgentEvery).
type superviseTimings struct {
	gather, every, urgent time.Duration
}

// superviseTimings are s's own timings, the package's for each one it does not set. Tests set them on their Service,
// so that a digest timer left over from one test never reads what another test's cleanup writes.
func (s *Service) superviseTimings() superviseTimings {
	p := s.superviseTimes
	if p.gather <= 0 {
		p.gather = superviseGather
	}
	if p.every <= 0 {
		p.every = superviseEvery
	}
	if p.urgent <= 0 {
		p.urgent = superviseUrgentEvery
	}
	return p
}

// digestWait is how long until the pending signals are due (false: none): each waits p.gather after it came, and
// p.every (p.urgent for an urgent one) after the last message (last; zero: none yet).
func digestWait(now, last time.Time, pending []superviseSignal, p superviseTimings) (time.Duration, bool) {
	var due time.Time
	for _, g := range pending {
		at := g.At.Add(p.gather)
		if !last.IsZero() {
			gap := p.every
			if g.urgent() {
				gap = p.urgent
			}
			if next := last.Add(gap); next.After(at) {
				at = next
			}
		}
		if due.IsZero() || at.Before(due) {
			due = at
		}
	}
	if due.IsZero() {
		return 0, false
	}
	return max(0, due.Sub(now)), true
}

// signal records g for the supervisor, unless the same stretch of state was told already, and sees that the message
// goes out when it is due.
func (s *Service) signal(g superviseSignal) {
	if g.level() == supervisionInfo {
		return // Progress is already recorded on the task card; it never wakes the assistant.
	}
	if g.At.IsZero() {
		g.At = time.Now()
	}
	b := &s.supervise
	b.mu.Lock()
	b.init()
	if b.sent[g.id()] == g.Key {
		b.mu.Unlock()
		return
	}
	if cur, ok := b.pending[g.id()]; ok {
		g.At = cur.At // still waiting: it keeps its place
		if g.Ask == "" {
			g.Ask = cur.Ask
		}
		if g.Detail == "" && cur.Key == g.Key {
			g.Detail = cur.Detail // the watch's sight of what an event told already
		}
		if g.Kind == sigReport && cur.Key != g.Key {
			g = joinReports(cur, g) // two questions before the message went: both are asked
		}
	}
	b.pending[g.id()] = g
	s.scheduleDigestLocked(time.Now())
	b.mu.Unlock()
}

// scheduleDigestLocked sets the timer for when the pending signals are due; s.supervise.mu is held.
func (s *Service) scheduleDigestLocked(now time.Time) {
	b := &s.supervise
	list := make([]superviseSignal, 0, len(b.pending))
	for _, g := range b.pending {
		list = append(list, g)
	}
	wait, ok := digestWait(now, b.last, list, s.superviseTimings())
	if !ok {
		return
	}
	at := now.Add(wait)
	if b.timer != nil && !b.timerAt.After(at) {
		return // set already, for no later
	}
	if b.timer != nil {
		b.timer.Stop()
	}
	b.timerAt = at
	b.timer = time.AfterFunc(wait, func() { s.sendDigest(context.Background()) })
}

// retryDigestLocked tries the digest again in d, unless one is due sooner (b.mu held).
func (s *Service) retryDigestLocked(d time.Duration) {
	b := &s.supervise
	at := time.Now().Add(d)
	if b.timer != nil && !b.timerAt.After(at) {
		return
	}
	if b.timer != nil {
		b.timer.Stop()
	}
	b.timerAt = at
	b.timer = time.AfterFunc(d, func() { s.sendDigest(context.Background()) })
}

// watchSignals are what t and its agents show now that the supervisor should hear of, by their state alone: a task
// waiting for review (told once per review: again after a restart of the assistant, which forgot what it told), an
// agent waiting for an answer (at once for a quiet task), or working without progress for long. Nothing more for a
// task waiting for review or for the user, and nothing for one done or not supervised. A task nobody works on any more
// is told from what the minute pass found (orphanSignal, tasks_orphan.go), since that asks whether its computers answer.
func watchSignals(t Todo, agents []proto.Session, now time.Time) []superviseSignal {
	v := t.Supervision
	if v == nil || t.DoneAt != nil {
		return nil
	}
	switch v.state(t) {
	case reviewPending:
		return []superviseSignal{{Todo: t.ID, Kind: sigReview, Key: reviewKey(v)}}
	case reviewNeedsUser:
		return nil
	}
	var out []superviseSignal
	for _, c := range agents {
		base := superviseSignal{Todo: t.ID, Machine: c.MachineID, Session: c.SessionID}
		switch {
		case c.State == proto.StateNeedsYou || c.PendingInteractions > 0:
			since := cmp.Or(c.StateSince, c.UpdatedAt)
			if wait := now.Sub(since); t.Quiet || wait >= superviseNeedsAfter {
				g := base
				g.Kind, g.Key, g.Minutes = sigNeedsYou, c.SessionID+"@"+since.UTC().Format(time.RFC3339), int(wait.Minutes())
				out = append(out, g)
			}
		case c.State == proto.StateWorking:
			moved := c.UpdatedAt
			if c.ActivityAt != nil && c.ActivityAt.After(moved) {
				moved = *c.ActivityAt
			}
			if still := now.Sub(moved); !moved.IsZero() && still >= stallAfter(v.ExpectMinutes) {
				g := base
				g.Kind, g.Key, g.Minutes = sigStalled, c.SessionID+"@"+moved.UTC().Format(time.RFC3339), int(still.Minutes())
				g.Detail = strings.TrimSpace(c.Activity)
				out = append(out, g)
			}
		}
	}
	return out
}

// reviewKey names one review a task waits for: when it began to wait.
func reviewKey(v *Supervision) string { return "review@" + v.UpdatedAt.UTC().Format(time.RFC3339Nano) }

// stallAfter is how long without progress counts as stuck for a task that should take expect minutes.
func stallAfter(expect int) time.Duration {
	return max(superviseStallMin, time.Duration(expect)*time.Minute*3/2)
}

// superviseLoop looks at the supervised tasks every superviseTick, and lets the supervision messages go out.
func (s *Service) superviseLoop(ctx context.Context) {
	t := time.NewTicker(superviseTick)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		s.superviseWatch(ctx, time.Now())
	}
}

// superviseWatch records what the supervised tasks' agents show now (watchSignals), and lets go of a hold on the
// main conversation's pushes that outlived superviseHoldMax.
func (s *Service) superviseWatch(ctx context.Context, now time.Time) {
	s.autoReviewTick(ctx, now)
	s.askTick(now) // cards held past their time go up a rung (ask_route.go)
	routed := s.routedSessions()
	for _, t := range s.todosOf(ctx, "*") {
		if t.Supervision == nil || t.DoneAt != nil {
			continue
		}
		s.remedyWatch(ctx, t, now) // a failed start's kept tab expired (start_remedy.go)
		for _, g := range watchSignals(t, s.taskAgents(ctx, t), now) {
			if g.Kind == sigNeedsYou && routed[[2]string{g.Machine, g.Session}] {
				continue // its card is with the supervisor as an ask already
			}
			s.signal(g)
		}
		if g, ok := s.orphanSignal(t); ok { // nobody works on it, as the minute pass found (tasks_orphan.go)
			s.signal(g)
		}
	}
	b := &s.supervise
	b.mu.Lock()
	changed := false
	for id, at := range b.hold {
		if now.Sub(at) > superviseHoldMax {
			delete(b.hold, id)
			delete(b.visible, id)
			changed = true
		}
	}
	b.mu.Unlock()
	if changed {
		s.publish(ctx)
	}
}

// SetSuperviseTimings makes supervision messages gather for gather and come at most every every (urgent: urgent), and
// answers what puts them back: for the hub's end-to-end tests, which cannot reach the package's own.
func SetSuperviseTimings(gather, every, urgent time.Duration) (restore func()) {
	g, e, u := superviseGather, superviseEvery, superviseUrgentEvery
	superviseGather, superviseEvery, superviseUrgentEvery = gather, every, urgent
	return func() { superviseGather, superviseEvery, superviseUrgentEvery = g, e, u }
}
