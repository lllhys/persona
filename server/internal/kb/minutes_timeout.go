package kb

import (
	"context"
	"errors"
	"time"
)

// A slow knowledge-base model (#3170). A minutes answer is several times longer than a summary, and a model that needs
// more than a call's time for it is slow, not wrong. So a try that runs out of its time (one call, or the whole job) is
// not an unusable answer: it is counted apart, the recording waits before its next try (the other recordings go first
// meanwhile), and each try gets one minutesCallTimeout more than the one before. minutesTimeoutTries of them for one
// revision make the minutes failed, so a recording no model ever finishes still can't hold the queue for ever.

const (
	minutesTimeoutTries = 4               // tries of one revision that ran out of time before the minutes are failed
	minutesRetry        = 2 * time.Minute // the wait after the first of them, doubled after each further one
)

// minutesCallTime is how long one model call may take after timeouts tries that ran out of time: 6, 12, 18, 24 minutes.
func minutesCallTime(timeouts int) time.Duration {
	return minutesCallTimeout * time.Duration(1+max(0, timeouts))
}

// minutesTimeouts is how many tries of r's current revision ran out of time.
func (r *Recording) minutesTimeouts() int {
	if r.MinutesFailRev != r.Revision {
		return 0
	}
	return r.MinutesTimeouts
}

// minutesWaiting: a try of r's current revision ran out of time and the next one is not due yet.
func (r *Recording) minutesWaiting(now time.Time) bool {
	return r.MinutesFailRev == r.Revision && now.Before(r.MinutesRetryAt)
}

// minutesFailuresFor starts r's counts over when they were of another revision than rev.
func (r *Recording) minutesFailuresFor(rev int64) {
	if r.MinutesFailRev != rev {
		r.MinutesFailRev, r.MinutesFails, r.MinutesTimeouts, r.MinutesRetryAt = rev, 0, 0, time.Time{}
	}
}

// clearMinutesFailures forgets the unusable answers and timeouts: minutes were written, or asked for again.
func (r *Recording) clearMinutesFailures() {
	r.MinutesFails, r.MinutesFailRev, r.MinutesTimeouts, r.MinutesRetryAt = 0, 0, 0, time.Time{}
}

// timedOut: err is a model call, or the whole job, running out of its own time while the round (ctx) goes on.
func timedOut(ctx context.Context, err error) bool {
	return errors.Is(err, context.DeadlineExceeded) && ctx.Err() == nil
}

// minutesTimedOut counts a try of revision rev of recording id that ran out of time, and puts the next one off.
// Minutes that end failed so from a final transcript take the hub's audio with them.
func (e *Engine) minutesTimedOut(id string, rev int64, now time.Time) {
	st := e.Store
	st.recMu.Lock()
	drop := false
	defer func() { e.dropAudioAfterMinutes(id, drop) }() // deferred calls run last first: after recMu is let go
	defer st.recMu.Unlock()
	r, ok := st.Recording(id)
	if !ok || r.Revision != rev {
		return
	}
	r.minutesFailuresFor(rev)
	r.MinutesTimeouts++
	wait := minutesRetry << min(r.MinutesTimeouts-1, 8)
	r.MinutesRetryAt = now.Add(wait)
	r.planMinutes()
	drop = r.MinutesState == MinutesFailed && r.transcriptionDone()
	if st.putEntity(KindRecording, r.ID, r, &r.Tagging, false) == nil && r.MinutesState == MinutesPending {
		e.logf("recording %s: minutes: try %d of %d ran out of time; the next, with %s per call, in %s", id,
			r.MinutesTimeouts, minutesTimeoutTries, minutesCallTime(r.MinutesTimeouts), wait)
	}
}
