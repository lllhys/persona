package assistant

import (
	"cmp"
	"context"
	"fmt"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/nwverse/persona/server/internal/assistantwire"
	"github.com/nwverse/persona/server/internal/connectorwire"
)

// Attempts are only a read budget. Recovery and deduplication depend on the saved park and source receipt,
// never this process-local map or an Inbox collection cursor.
type parkReplyBook struct {
	running sync.Mutex
	asked   map[string]time.Time
}

func parkReplyBinding(e *ParkEvidence) bool {
	if e == nil || e.Kind != "person_reply" || e.Contact == nil || e.Reply == nil {
		return false
	}
	w := e.Reply
	if w.Source != "larkcli" || w.Brand != "feishu" && w.Brand != "lark" || w.AccountOpenID == w.PersonOpenID {
		return false
	}
	for _, id := range []string{w.AppID, w.AccountOpenID, w.PersonOpenID, w.ChatID, w.ThreadID, w.RequestMessageID} {
		if !parkReplyID(id) {
			return false
		}
	}
	return parkReplyURL(e.Contact.URL, *w) != ""
}

func parkReplyID(id string) bool {
	return id != "" && len(id) <= 200 && strings.IndexFunc(id, func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '-')
	}) < 0
}

// Only the source's exact topic AppLink can bind a reader. Request identity is checked again against the actual
// source message; a display name, generic chat link or arbitrary URL never chooses a person/topic.
func parkReplyURL(raw string, w connectorwire.LarkReplyWatch) string {
	u, err := url.Parse(raw)
	host := "applink.feishu.cn"
	if w.Brand == "lark" {
		host = "applink.larksuite.com"
	}
	if err != nil || u.Scheme != "https" || u.Host != host || u.User != nil || u.Path != "/client/thread/open" || u.Fragment != "" {
		return ""
	}
	q, err := url.ParseQuery(u.RawQuery)
	if err != nil || len(q["open_chat_id"]) != 1 || q.Get("open_chat_id") != w.ChatID ||
		len(q["open_thread_id"]) != 1 || q.Get("open_thread_id") != w.ThreadID {
		return ""
	}
	if ids, present := q["message_id"]; present && (len(ids) != 1 || ids[0] != w.RequestMessageID) {
		return ""
	}
	u.RawQuery = q.Encode()
	return u.String()
}

// The existing minute source pass owns these reads; no user request/snapshot waits for them and no new
// subscription, feature switch, source write or automatic external message is introduced.
func (s *Service) sweepParkRepliesBounded(now time.Time) {
	b := &s.parkReplies
	if !b.running.TryLock() {
		return
	}
	defer b.running.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if s.ensureTodosLoaded(ctx) != nil {
		return
	}
	tasks := s.todosWhere(ctx, "*", func(t *Todo) bool {
		p := t.Supervision.parking()
		return t.DoneAt == nil && p != nil && p.WaitingOn == parkSomeoneElse && !p.At.IsZero() && parkReplyBinding(p.Evidence)
	})
	if b.asked == nil {
		b.asked = map[string]time.Time{}
	}
	active := make(map[string]bool, len(tasks))
	for _, t := range tasks {
		active[t.ID] = true
	}
	for id := range b.asked {
		if !active[id] {
			delete(b.asked, id)
		}
	}
	slices.SortFunc(tasks, func(a, c Todo) int {
		return cmp.Or(b.asked[a.ID].Compare(b.asked[c.ID]), strings.Compare(a.ID, c.ID))
	})
	for _, t := range tasks[:min(4, len(tasks))] {
		if ctx.Err() != nil {
			return
		}
		if at := b.asked[t.ID]; !at.IsZero() && now.Sub(at) < time.Minute {
			continue
		}
		b.asked[t.ID] = now
		p := t.Supervision.parking()
		cctx, stop := context.WithTimeout(ctx, 8*time.Second)
		var receipt connectorwire.LarkReplyObservation
		err := s.appCall(cctx, AppConnectors, assistantwire.ConnectorsLarkReply, *p.Evidence.Reply, &receipt)
		stop()
		if err == nil {
			err = s.applyParkReply(ctx, t, receipt)
		}
		if err != nil {
			s.logf("task %s: read bound person reply: %v", t.ID, err)
		}
	}
}

func parkReplyMatches(p *TaskReview, o connectorwire.LarkReplyObservation) bool {
	if p == nil || p.At.IsZero() || p.WaitingOn != parkSomeoneElse || !parkReplyBinding(p.Evidence) || p.Reply != nil {
		return false
	}
	e, w := p.Evidence, *p.Evidence.Reply
	q, r := o.Request, o.Reply
	if o.Watch != w || o.CheckedAt.Before(p.At) || o.CheckedAt.After(time.Now()) || q.ID != w.RequestMessageID ||
		!parkReplyID(r.ID) || r.ID == q.ID || e.Contact.Status != "sent" || e.Contact.SentAt.IsZero() ||
		r.SenderType != "user" || r.IDType != "open_id" || r.SenderID != w.PersonOpenID ||
		r.AppID != "" || q.ChatID != w.ChatID || r.ChatID != w.ChatID || q.ThreadID != w.ThreadID || r.ThreadID != w.ThreadID {
		return false
	}
	askedByUser := q.SenderType == "user" && q.IDType == "open_id" && q.SenderID == w.AccountOpenID && q.AppID == ""
	askedByBot := q.SenderType == "app" && q.SenderID != "" &&
		(q.IDType == "app_id" && q.SenderID == w.AppID || q.AppID == w.AppID)
	if !askedByUser && !askedByBot || parkReplyURL(q.URL, w) != parkReplyURL(e.Contact.URL, w) {
		return false
	}
	for _, m := range []connectorwire.LarkReplyMessage{q, r} {
		if m.At.IsZero() || !m.Before.After(m.At) || m.Before.Sub(m.At) > time.Minute || m.Before.After(o.CheckedAt) {
			return false
		}
	}
	// Source time is an interval, not a rounded instant. In particular a request/reply in the park's own
	// ambiguous minute cannot establish that it was already sent / arrived afterwards.
	return !e.Contact.SentAt.Before(q.At) && e.Contact.SentAt.Before(q.Before) && !q.Before.After(p.At) &&
		r.At.After(p.At) && !r.At.Before(q.Before)
}

func (s *Service) applyParkReply(ctx context.Context, t Todo, o connectorwire.LarkReplyObservation) error {
	held := t.Supervision.parking()
	if !parkReplyMatches(held, o) {
		return nil
	}
	// Completion/deletion and the ledger transition must not pass each other between the recheck and commit.
	// This is the same todo-write -> supervision-write order used by task updates.
	s.todos.writing.Lock()
	defer s.todos.writing.Unlock()
	v, changed, err := s.updateSupervision(ctx, t.ID, func(v *Supervision) bool {
		p := v.parking()
		cur, ok := s.todoNow(t.ID)
		if !ok || cur.DoneAt != nil || p == nil || !p.At.Equal(held.At) || !parkReplyMatches(p, o) {
			return false
		}
		receipt := o
		p.Reply = &receipt
		v.State = reviewPending
		return true
	})
	if err != nil {
		return err
	}
	if changed {
		s.signal(superviseSignal{Todo: t.ID, Kind: sigReview, Key: reviewKey(&v),
			Detail: fmt.Sprintf("The named person replied in the bound Lark topic: %s (message %s). This is a source reply, not an owner decision or Agent delivery.",
				held.Evidence.Contact.URL, o.Reply.ID)})
	}
	return nil
}
