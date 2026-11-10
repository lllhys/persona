package assistant

import (
	"context"
	"errors"
	"slices"
	"time"

	"github.com/nwverse/persona/server/internal/kb"
	"github.com/nwverse/persona/server/internal/proto"
)

// The session a recorded meeting's batches go to (docs/assistant/meeting-minutes.md "Feeding the attending session"):
// the one the upload names, else the main chat as it is at each batch. Checked at every batch, since trust and
// sessions change mid-meeting: whether it may be given other people's words at all (attendee_not_allowed), whether it
// can take a message now (attendee_unavailable), and whether it is free for one (idle or done, nothing waiting on it).

// Problems the feeder notes in the live log for GET and POST …/live to report.
const (
	attendeeUnavailable = "attendee_unavailable"
	attendeeNotAllowed  = "attendee_not_allowed"
)

// attendeeBusy is attendeeVerdict's word for a session that may attend but not take a batch now (no problem: the
// phone shows it catching up).
const attendeeBusy = "busy"

// attendeeNeverOrigins are the origins of assistant conversations whose replies leave the apps: they never attend.
var attendeeNeverOrigins = []string{OriginLarkAgent, OriginChannel, OriginExtension, OriginInbox}

// attendeeTarget is the session a batch goes to: ref by its real ids (the main chat's too), persona when it is one of
// the assistant's conversations (assistant.send resumes one that stopped) rather than an Agent (session.send).
type attendeeTarget struct {
	ref     kb.SessionRef
	persona bool
}

// attendeeFacts are what attendeeVerdict needs beyond the session's row.
type attendeeFacts struct {
	ours    bool // its computer is one this manager carries
	work    bool // that computer's trust is work
	online  bool // and its node can be reached now
	found   bool // the session is listed there
	guest   bool // a guest conversation (guest.go)
	unknown bool // whether it is a guest, or where it comes from, cannot be told now
	// settling: its last turn ended moments ago and that turn's push and hooks are not decided yet; a batch now would
	// pass for that turn's end. running: a batch's turn is under way there.
	settling, running bool
}

// attendeeVerdict says what becomes of a batch for sess: "" it goes now, attendeeBusy it waits, or the problem.
func attendeeVerdict(sess proto.Session, f attendeeFacts) string {
	switch {
	case !f.ours || f.work:
		return attendeeNotAllowed
	case !f.online, !f.found:
		return attendeeUnavailable
	}
	if sess.Assistant {
		switch {
		case spaceOf(sess) == SpaceHealth, slices.Contains(attendeeNeverOrigins, sess.Origin), sess.OpenedBy != "", f.guest:
			return attendeeNotAllowed
		case f.unknown:
			return attendeeUnavailable
		}
	}
	if sess.PermissionMode == proto.ModeBypass { // other people's words reach it and nothing would ever ask first
		return attendeeNotAllowed
	}
	if !sess.Assistant {
		switch {
		case sess.IsSubagent, sess.SourceKind != "" && (sess.Capabilities == nil || !sess.Capabilities.Send):
			return attendeeUnavailable // it takes no messages
		case sess.Status == proto.StatusOffline, sess.Status == proto.StatusInterrupted:
			return attendeeUnavailable // closed: an Agent is not resumed for a meeting
		}
	}
	switch {
	case sess.Status == proto.StatusWorking, sess.Status == proto.StatusBlocked, sess.Status == proto.StatusUnknown, sess.Status == "",
		sess.Starting, sess.PendingInteractions > 0, sess.State == proto.StateWorking, sess.State == proto.StateNeedsYou,
		sess.BackgroundWorking, sess.TurnErrorResumes, f.settling, f.running:
		return attendeeBusy
	}
	return ""
}

// attendeeRef is the session recording r is attended by: the one its upload names, else the main chat as it is now;
// false when there is none (no main chat).
func (s *Service) attendeeRef(ctx context.Context, r kb.Recording) (kb.SessionRef, bool) {
	if a := r.AttendingSession; a != nil && a.MachineID != "" && a.SessionID != "" {
		return *a, true
	}
	cfg := s.settings(ctx)
	if cfg.Main == "" || cfg.MachineID == "" {
		return kb.SessionRef{}, false
	}
	return kb.SessionRef{MachineID: cfg.MachineID, SessionID: cfg.Main}, true
}

// attendeeCheck checks ref for a batch now: the target, and attendeeVerdict's word.
func (s *Service) attendeeCheck(ctx context.Context, ref kb.SessionRef, now time.Time) (attendeeTarget, string) {
	if s.devs != nil {
		ref.MachineID = s.devs.Canonical(ref.MachineID) // an id another one absorbed: that one
	}
	t := attendeeTarget{ref: ref}
	var f attendeeFacts
	machine := ref.MachineID
	f.ours, f.online = s.attendeeMachine(ctx, machine)
	f.work = s.isWorkMachine(machine)
	if !f.ours || f.work || !f.online {
		return t, attendeeVerdict(proto.Session{}, f)
	}
	sess, found, err := s.attendeeRow(ctx, machine, ref.SessionID)
	if err != nil {
		s.logf("meeting: sessions of %s: %v", machine, err)
		return t, attendeeUnavailable
	}
	f.found = found
	t.persona = sess.Assistant
	if found && sess.Assistant {
		_, guest, err := s.guestOf(ctx, machine, ref.SessionID)
		f.guest, f.unknown = guest, err != nil
		if origins, ok := s.conversationOrigins(); !ok {
			f.unknown = true
		} else if o, ok := origins[ref.SessionID]; ok {
			sess.Origin, sess.OpenedBy = o.origin, o.ext // the book is where the lists' fields come from
		}
	}
	if found {
		s.meetingTurnStopped(machine, ref.SessionID, sess, false) // stopped since its batch: whatever its end looked like
		running, err := s.meetingTurnRunning(ctx, machine, ref.SessionID)
		f.running = running || err != nil
		f.settling = s.turnSettling(machine, ref.SessionID, now)
	}
	return t, attendeeVerdict(sess, f)
}

// attendeeMachine says whether machine is one this manager carries, and whether its node can be reached now: the
// devices' own list when the manager has them (the fleet), else the hub's.
func (s *Service) attendeeMachine(ctx context.Context, machine string) (ours, online bool) {
	if s.devs == nil {
		d, ok := s.device(ctx, machine)
		return ok, d.Online && d.Reachable
	}
	for _, d := range s.devs.List() {
		if d.ID == machine {
			return true, d.Online && d.Reachable
		}
	}
	return false, false
}

// attendeeRow is machine's session as the apps get it (its state, cards and origin decorated), and whether it is
// listed there.
func (s *Service) attendeeRow(ctx context.Context, machine, id string) (proto.Session, bool, error) {
	if s.fleetOn() {
		if _, ok := s.fleetMachine(machine); !ok {
			return proto.Session{}, false, nil
		}
		sess, ok := s.decoratedSession(machine, id)
		return sess, ok, nil
	}
	list, err := s.sessions(ctx, machine)
	if errors.Is(err, ErrNoDevice) {
		return proto.Session{}, false, nil
	}
	if err != nil {
		return proto.Session{}, false, err
	}
	for _, c := range list {
		if c.SessionID == id {
			return c, true, nil
		}
	}
	return proto.Session{}, false, nil
}

// turnSettling: machine's session's last turn ended less than its push's delay ago (and a little more) and is still
// followed: its push and the hooks of its end are decided against the session as it is then, so a batch waits.
func (s *Service) turnSettling(machine, id string, now time.Time) bool {
	ended, ok := s.turnsBook().endedAt(machine + "/" + id)
	return ok && now.Sub(ended) < s.notifyTimings().doneDelay()+15*time.Second
}

// endedAt is when the session's followed turn ended; false when none is followed or it has not ended.
func (t *turnTracker) endedAt(key string) (time.Time, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if tr := t.turns[key]; tr != nil && !tr.endedAt.IsZero() {
		return tr.endedAt, true
	}
	return time.Time{}, false
}
