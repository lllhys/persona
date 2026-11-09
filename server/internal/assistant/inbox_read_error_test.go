package assistant

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/assistantwire"
	"github.com/nwverse/persona/server/internal/connector"
	"github.com/nwverse/persona/server/internal/i18n"
)

// A source the round could not read keeps what it listed, and the inbox status says which one is behind and why, so
// the owner does not take the list for complete; the next round that reads it clears that (#4801).
func TestInboxSaysWhichSourceCouldNotBeRead(t *testing.T) {
	h := newInboxHub(t)
	ctx := context.Background()
	names := map[string]string{"example": "Example"}
	ok := assistantwire.InboxRound{Round: roundPoll, Names: names,
		Sources: []assistantwire.InboxSource{{ID: "example", Name: "Example", Things: []connector.InboxThing{thing("a", time.Now())}}}}
	if err := h.InboxCollected(ctx, ok); err != nil {
		t.Fatal(err)
	}
	before := len(openItems(h))
	if before != 1 {
		t.Fatalf("%d items after the first round", before)
	}
	failed := assistantwire.InboxRound{Round: roundPoll, Names: names,
		Sources: []assistantwire.InboxSource{{ID: "example", Name: "Example", Things: []connector.InboxThing{}, ReadError: assistantwire.ReadFailed}}}
	if err := h.InboxCollected(ctx, failed); err != nil {
		t.Fatal(err)
	}
	if n := len(openItems(h)); n != before {
		t.Fatalf("a source that could not be read lost its items: %d, had %d", n, before)
	}
	tr := h.inboxTriage()
	if len(tr.Sources) != 1 || tr.Sources[0].Source != "example" || tr.Sources[0].ReadError != assistantwire.ReadFailed ||
		tr.Sources[0].ReadReason == "" || tr.Sources[0].Name != "Example" || tr.Waiting != 0 || tr.State != "ok" {
		t.Fatalf("triage after a failed read: %+v", tr)
	}
	if err := h.InboxCollected(ctx, ok); err != nil {
		t.Fatal(err)
	}
	if tr := h.inboxTriage(); len(tr.Sources) != 0 {
		t.Fatalf("a source read again still says it failed: %+v", tr)
	}
}

// Review of #4825: a held round over a failed read carries the earlier things together with read_error (mergeRounds).
// Those things are applied, so new items and Done markers are not lost when the hub then clears what it held; the
// failure still shows. An unknown code reads as failed, so no raw i18n key reaches the apps.
func TestInboxAppliesThingsAFailedSourceCarries(t *testing.T) {
	h := newInboxHub(t)
	ctx := context.Background()
	names := map[string]string{"example": "Example"}
	r := assistantwire.InboxRound{Round: roundPoll, Names: names, Sources: []assistantwire.InboxSource{{ID: "example", Name: "Example",
		Things: []connector.InboxThing{thing("a", time.Now())}, ReadError: assistantwire.ReadFailed}}}
	if err := h.InboxCollected(ctx, r); err != nil {
		t.Fatal(err)
	}
	if n := len(openItems(h)); n != 1 {
		t.Fatalf("the things a failed source carried were dropped: %d items", n)
	}
	if tr := h.inboxTriage(); len(tr.Sources) != 1 || tr.Sources[0].ReadError != assistantwire.ReadFailed {
		t.Fatalf("the failure does not show: %+v", tr)
	}
	r.Sources[0].Things, r.Sources[0].ReadError = []connector.InboxThing{}, "a_code_from_a_newer_hub"
	if err := h.InboxCollected(ctx, r); err != nil {
		t.Fatal(err)
	}
	if tr := h.inboxTriage(); len(tr.Sources) != 1 || tr.Sources[0].ReadError != assistantwire.ReadFailed ||
		tr.Sources[0].ReadReason == "" || tr.Sources[0].ReadReason == "assistant-inbox_admit.read_a_code_from_a_newer_hub" {
		t.Fatalf("an unknown code: %+v", tr.Sources)
	}
}

// failingCollectHub is the test hub, except that collecting the sources now (inbox.collect) fails with err while it
// is set.
type failingCollectHub struct {
	Hub
	err error
}

func (h *failingCollectHub) AppCall(ctx context.Context, p AppCall) (json.RawMessage, error) {
	if h.err != nil && p.AppID == AppInbox && p.Method == assistantwire.InboxCollect {
		return nil, h.err
	}
	return h.Hub.AppCall(ctx, p)
}

// A pull to refresh whose collect call fails says so (#1849): 502 inbox_refresh_failed with a sentence in the
// request's language and the collect error as the detail. It used to answer 200 with the list as it was, so the apps
// took it for a refresh that worked. One that collects lists what it read.
func TestInboxRefreshSaysWhenTheCollectFailed(t *testing.T) {
	h := newInboxHub(t)
	h.addConnector(&inboxConnector{things: []connector.InboxThing{thing("a", time.Now())}}, false)
	bridge := &failingCollectHub{Hub: h.Service.hub, err: errors.New("hand the round to the manager: context deadline exceeded")}
	h.Service.hub = bridge
	said := map[any]bool{}
	for _, l := range []i18n.Lang{i18n.En, i18n.Ja} {
		status, body := serveJSON(t, h.Service, "POST", "/api/v1/inbox/refresh?sources=all", string(l), `{}`)
		if status != http.StatusBadGateway || body["code"] != "inbox_refresh_failed" || body["error"] != i18n.T(l, "assistant-inbox.refresh_failed") ||
			body["detail"] != bridge.err.Error() || body["retryable"] != true || body["items"] != nil {
			t.Fatalf("%s, the collect failed: %d %v", l, status, body)
		}
		said[body["error"]] = true
	}
	if len(said) != 2 || said["assistant-inbox.refresh_failed"] {
		t.Fatalf("the failure is not said in each language: %v", said)
	}
	if n := len(openItems(h)); n != 0 {
		t.Fatalf("a failed refresh collected %d items", n)
	}
	bridge.err = nil
	status, body := serveJSON(t, h.Service, "POST", "/api/v1/inbox/refresh?sources=all", "en", `{}`)
	if items, _ := body["items"].([]any); status != http.StatusOK || len(items) != 1 || body["triage"] == nil {
		t.Fatalf("the collect worked: %d %v", status, body)
	}
}
