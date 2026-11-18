package assistant

import (
	"cmp"
	"context"
	"errors"
	"slices"
	"sync"
	"time"

	"github.com/nwverse/persona/server/internal/channel"
)

// Where notify_user's copies to the chat channels went, for its answer (#8268). The two senders, the Feishu bot's
// (feishuPushTold) and the extension channels' (channelPushTold), each say once which channels they send to, then how
// each send went; the tool waits up to notifyCopyWait for that. Without it, a notice no device took was answered
// "the user was not alerted" although its Feishu copy had landed, and the conversation sent it again.

// notifyCopyWait is how long notify_user waits for its copies before it answers. The first Feishu card or a channel's
// text normally lands well within it; a copy that has not by then is answered as not confirmed and goes on in the
// background (each sender has its own 30 seconds).
var notifyCopyWait = 5 * time.Second

// How one copy went.
const (
	copySent    = "sent"    // the first Feishu card landed (or a notifications companion took it); a channel took the text
	copyFailed  = "failed"  // nothing of it went out
	copyPending = "pending" // not known yet when the tool answered
)

// errNoCopy: the Feishu push returned without a first card that landed or failed (nothing to send).
var errNoCopy = errors.New("nothing was sent")

// pushCopy is one chat channel a notify_user copy goes to.
type pushCopy struct {
	channel string // channel.Feishu, or an extension channel's id
	name    string // an extension channel's name; the Feishu bot is named in the answer's language
	state   string // copySent, copyFailed or copyPending
}

// copyReport collects one notify_user call's copies. Its methods do nothing on a nil report: a push nobody waits for.
type copyReport struct {
	mu      sync.Mutex
	senders int // the senders that have not said yet where they send
	copies  []pushCopy
	changed chan struct{} // closed on every change, then replaced
}

func newCopyReport(senders int) *copyReport {
	return &copyReport{senders: senders, changed: make(chan struct{})}
}

// going: one sender sends to these channels (none: it sends nothing). Each sender calls it once, before it sends.
func (r *copyReport) going(to ...pushCopy) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, c := range to {
		c.state = copyPending
		r.copies = append(r.copies, c)
	}
	r.senders--
	r.changedLocked()
}

// went: the copy to channel id went out (err nil) or did not. Only the first word on a copy counts.
func (r *copyReport) went(id string, err error) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for i := range r.copies {
		if r.copies[i].channel != id || r.copies[i].state != copyPending {
			continue
		}
		r.copies[i].state = copySent
		if err != nil {
			r.copies[i].state = copyFailed
		}
	}
	r.changedLocked()
}

func (r *copyReport) changedLocked() {
	close(r.changed)
	r.changed = make(chan struct{})
}

// wait returns the copies once every sender has said where it sends and how each went, or as they stand after d:
// the Feishu bot first, then the extension channels by name.
func (r *copyReport) wait(d time.Duration) []pushCopy {
	if r == nil {
		return nil
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	for {
		r.mu.Lock()
		done := r.senders <= 0 && !slices.ContainsFunc(r.copies, func(c pushCopy) bool { return c.state == copyPending })
		out, changed := slices.Clone(r.copies), r.changed
		r.mu.Unlock()
		if !done {
			select {
			case <-changed:
				continue
			case <-timer.C:
				r.mu.Lock()
				out = slices.Clone(r.copies)
				r.mu.Unlock()
			}
		}
		slices.SortStableFunc(out, func(a, b pushCopy) int {
			if (a.channel == channel.Feishu) != (b.channel == channel.Feishu) {
				if a.channel == channel.Feishu {
					return -1
				}
				return 1
			}
			return cmp.Compare(a.name, b.name)
		})
		return out
	}
}

// channelsCopied is channelsPushed for notify_user, which says in its answer where the copies went: the sends start
// now; wait on the report for how they went.
func (s *Service) channelsCopied(machine, session, title, body string) *copyReport {
	r := newCopyReport(2)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second) // as feishuPush
		defer cancel()
		_ = s.feishuPushTold(ctx, machine, session, title, body, "", r)
	}()
	go func() { _, _ = s.channelPushTold(context.Background(), machine, session, title, body, "", nil, r) }()
	return r
}
