package subscriptions

import (
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/nwverse/persona/sdk/go/wire"
	"github.com/nwverse/persona/server/internal/assistantwire"
	"github.com/nwverse/persona/server/internal/connector"
	"github.com/nwverse/persona/server/internal/rpc"
)

// How each state says a subscription is behind (#6189), as a built-in's failed read does.
func TestReadErrorOfEachState(t *testing.T) {
	for state, want := range map[string]string{StateOK: "", "": "", StateOff: "", StateNotConnected: "",
		StateFailing: assistantwire.ReadFailed, StateNeedsRecipe: assistantwire.ReadFailed, StateRateLimited: assistantwire.ReadRateLimited} {
		if got := readErrorOf(state); got != want {
			t.Errorf("%q: %q, want %q", state, got, want)
		}
	}
}

// A subscription whose run failed is behind in the Inbox on the next round, not a day later, and still hands the
// rows it holds; a run that reads again clears it; with its Inbox off it says nothing (#6189).
func TestAFailingSubscriptionIsBehindInTheInboxAtOnce(t *testing.T) {
	e := newEnv(t)
	sub := e.turnOn(e.create(itemsRecipe()))
	e.svc.RunDue(e.ctx)
	if got := e.inbox(sub.ID).ReadError; got != "" {
		t.Fatalf("a subscription that reads: %q", got)
	}
	e.conn.setAnswer(func(connector.CatalogQuery) (string, error) { return "", errors.New("upstream said no") })
	e.clock.advance(20 * time.Minute)
	e.svc.RunDue(e.ctx)
	if s := e.get(sub.ID); s.State != StateFailing {
		t.Fatalf("after a failed run: %+v", s)
	}
	if got := e.inbox(sub.ID).ReadError; got != assistantwire.ReadFailed || len(e.rows(sub.ID)) == 0 {
		t.Fatalf("after a failed run: %q, rows %d", got, len(e.rows(sub.ID)))
	}
	e.conn.setAnswer(func(connector.CatalogQuery) (string, error) { return notices(1, 2, 3, 4), nil })
	e.clock.advance(time.Hour)
	e.svc.RunDue(e.ctx)
	if got := e.inbox(sub.ID).ReadError; got != "" {
		t.Fatalf("after it reads again: %q (%+v)", got, e.get(sub.ID))
	}
	e.conn.setAnswer(func(connector.CatalogQuery) (string, error) {
		return "", &rpc.Error{Code: wire.ConnectorErrorCodeRateLimited, Message: "slow down", Data: json.RawMessage(`{"retry_after":600}`)}
	})
	e.clock.advance(time.Hour)
	e.svc.RunDue(e.ctx)
	if got := e.inbox(sub.ID).ReadError; got != assistantwire.ReadRateLimited {
		t.Fatalf("asked to wait: %q (%+v)", got, e.get(sub.ID))
	}
	e.patch(sub.ID, Patch{ToInbox: ptr(false), ToKB: ptr(true)})
	if got := e.inbox(sub.ID).ReadError; got != "" {
		t.Fatalf("with its Inbox off: %q", got)
	}
}

// A subscription says what it reads, and while its knowledge base is on, each row's chat there (#6222): the one chat
// of a subscription grouped by source.
func TestInboxSourcesSayWhatTheyReadAndTheirRowsChats(t *testing.T) {
	e := newEnv(t)
	sub := e.turnOn(e.create(itemsRecipe()))
	e.svc.RunDue(e.ctx)
	if src := e.inbox(sub.ID); src.Connector != "example" || src.SourceName != "Notices" || src.Chats != nil {
		t.Fatalf("with its knowledge base off: %+v", src)
	}
	e.patch(sub.ID, Patch{ToKB: ptr(true)})
	src, rows := e.inbox(sub.ID), e.rows(sub.ID)
	if len(rows) == 0 || len(src.Chats) != len(rows) || src.Chats[rows[0].Thread] != rowAll {
		t.Fatalf("with its knowledge base on: chats %v, rows %d", src.Chats, len(rows))
	}
}
