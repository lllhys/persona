package subscriptions

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/nwverse/persona/server/internal/connector"
)

// replyConn is the env's connector with an inbox that takes replies.
type replyConn struct {
	*fakeConn
	rmu     sync.Mutex
	replies []connector.InboxReply
}

func (r *replyConn) Manifest() connector.Manifest {
	return connector.Manifest{Capabilities: []connector.Capability{connector.CapCatalog, connector.CapInbox}}
}
func (r *replyConn) Collect(context.Context) ([]connector.InboxThing, error) { return nil, nil }
func (r *replyConn) Reply(_ context.Context, rp connector.InboxReply) error {
	r.rmu.Lock()
	defer r.rmu.Unlock()
	r.replies = append(r.replies, rp)
	return nil
}

// replyEnv is an env whose connector takes replies, with a recipe that maps reply_to and threads, and one run held.
func replyEnv(t *testing.T, ids bool) (*env, *replyConn, Subscription) {
	e := newEnv(t)
	rc := &replyConn{fakeConn: e.conn}
	e.conns.infos["example"].Conn = rc
	var items []map[string]any
	for i := 1; i <= 2; i++ {
		it := map[string]any{"title": fmt.Sprint("Comment ", i), "thread": "example-org/example-repo#12",
			"at": fmt.Sprintf("2026-11-10T0%d:00:00Z", i), "to": "Comment on example-org/example-repo#12 (anyone who can see the repo)"}
		if ids {
			it["id"] = fmt.Sprint("c", i)
		}
		items = append(items, it)
	}
	b, _ := json.Marshal(map[string]any{"items": items})
	e.answer(string(b))
	r := itemsRecipe()
	r.GroupBy = "thread"
	r.Fields = &connector.SubscriptionFields{Title: "$.title", Thread: "$.thread", Time: "$.at", ReplyTo: "$.to"}
	if ids {
		r.Fields.ID = "$.id"
	}
	sub := e.turnOn(e.create(r))
	e.svc.RunDue(e.ctx)
	return e, rc, sub
}

// A row whose newest item says where a reply goes can be replied to through the subscription's connector (#6198):
// the reply finds the item and the thread the connector knows it by.
func TestARowWithReplyToCanBeRepliedToThroughItsConnector(t *testing.T) {
	e, rc, sub := replyEnv(t, true)
	rows := e.rows(sub.ID)
	if len(rows) != 1 || !rows[0].CanReply || rows[0].ReplyTo != "Comment on example-org/example-repo#12 (anyone who can see the repo)" || rows[0].Last != "c2" {
		t.Fatalf("rows: %+v", rows)
	}
	in, thread, err := e.svc.ReplyTarget(e.ctx, sub.ID, rows[0].Thread, rows[0].Last)
	if err != nil || thread != "example-org/example-repo#12" {
		t.Fatalf("target: %v %q", err, thread)
	}
	if err := in.Reply(e.ctx, connector.InboxReply{Thread: thread, Last: rows[0].Last, Text: "Thanks"}); err != nil || len(rc.replies) != 1 {
		t.Fatalf("reply: %v %+v", err, rc.replies)
	}
	// A row with no item to answer, or whose item is gone, cannot be answered (#9194): refused as such a row.
	for _, last := range []string{"", "gone", "h:abc"} {
		if _, _, err := e.svc.ReplyTarget(e.ctx, sub.ID, "", last); !errors.Is(err, connector.ErrCannotReply) {
			t.Errorf("for %q: %v, want a row that cannot be answered", last, err)
		}
	}
	// A subscription that is gone is a source that is not here, whatever the row.
	if _, _, err := e.svc.ReplyTarget(e.ctx, "sub_gone", "", "c2"); err == nil || errors.Is(err, connector.ErrCannotReply) {
		t.Errorf("a target of a subscription that is gone: %v", err)
	}
	// The connector can't be called now: a reply is not routed; the row stops offering one once a run found it so.
	e.conns.infos["example"].Status = connector.StateNotSignedIn
	if _, _, err := e.svc.ReplyTarget(e.ctx, sub.ID, "", "c2"); err == nil || errors.Is(err, connector.ErrCannotReply) {
		t.Errorf("routed a reply while the connector can't be called, or blamed the row: %v", err)
	}
	cur, _ := e.store.GetSubscription(e.ctx, sub.ID)
	cut := *cur
	cut.RunState = StateNotConnected
	if ok, _ := e.store.UpdateSubscription(e.ctx, cut, cur.Revision); !ok {
		t.Fatal("update")
	}
	if rows := e.rows(sub.ID); rows[0].CanReply {
		t.Error("offered a reply while the subscription can't reach its connector")
	}
	// Switched off: no reply offered.
	e.conns.infos["example"].Status, e.conns.infos["example"].Enabled = connector.StateOK, false
	cut.RunState = StateOK
	_, _ = e.store.UpdateSubscription(e.ctx, cut, cur.Revision)
	if rows := e.rows(sub.ID); rows[0].CanReply {
		t.Error("offered a reply through a connector that is switched off")
	}
}

// No reply without an id from the source (a weak hash id names nothing there), nor through a connector without an inbox.
func TestNoReplyWithoutAnIDOrAnInbox(t *testing.T) {
	e, _, sub := replyEnv(t, false)
	if rows := e.rows(sub.ID); len(rows) != 1 || rows[0].CanReply || rows[0].ReplyTo != "" {
		t.Fatalf("a row of weak ids: %+v", rows)
	}
	e2, _, sub2 := replyEnv(t, true)
	e2.conns.infos["example"].Conn = e2.conn // the plain connector: a catalog, no inbox
	if rows := e2.rows(sub2.ID); rows[0].CanReply {
		t.Fatal("offered a reply through a connector without an inbox")
	}
}
