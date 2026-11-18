package subscriptions

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/nwverse/persona/sdk/go/wire"
	"github.com/nwverse/persona/server/internal/connector"
	"github.com/nwverse/persona/server/internal/rpc"
)

func pagingEnv(t *testing.T) *env {
	t.Helper()
	e := newEnv(t)
	src := noticesSource()
	src.Tools[0].InputSchema = json.RawMessage(`{"type":"object","properties":{"since":{"type":"string"},"limit":{"type":"integer","maximum":50},"cursor":{"type":"string"},"page":{"type":"integer","minimum":1}},"required":["since"],"additionalProperties":false}`)
	e.conn.setSources([]connector.CatalogSource{src})
	return e
}

func cursorRecipe() Recipe {
	r := itemsRecipe()
	r.CursorArg, r.NextCursorPath = "cursor", "$.next_cursor"
	return r
}

func numberedPageRecipe() Recipe {
	r := itemsRecipe()
	r.PageArg, r.NextPagePath = "page", "$.next_page"
	return r
}

func noticesPage(nextCursor, nextPage any, ids ...int) string {
	var doc map[string]any
	_ = json.Unmarshal([]byte(notices(ids...)), &doc)
	doc["next_cursor"], doc["next_page"] = nextCursor, nextPage
	raw, _ := json.Marshal(doc)
	return string(raw)
}

func queryArgs(t *testing.T, q connector.CatalogQuery) map[string]any {
	t.Helper()
	var args map[string]any
	if err := json.Unmarshal(q.Args, &args); err != nil {
		t.Fatal(err)
	}
	return args
}

func itemUIDs(items []Item) []string {
	out := make([]string, len(items))
	for i, it := range items {
		out[i] = it.UID
	}
	slices.Sort(out)
	return out
}

func TestCursorPaginationResumesTheOriginalWindow(t *testing.T) {
	e := pagingEnv(t)
	e.conn.setAnswer(func(q connector.CatalogQuery) (string, error) {
		switch cursor, _ := queryArgs(t, q)["cursor"].(string); cursor {
		case "":
			return noticesPage("second", nil, 3, 2, 2), nil // a duplicate within the page
		case "second":
			return noticesPage(nil, nil, 2, 1), nil // and one repeated from the preceding page
		default:
			return "", fmt.Errorf("unexpected cursor")
		}
	})
	sub := e.turnOn(e.create(cursorRecipe()))
	before := e.conn.callCount()
	e.svc.RunDue(e.ctx)
	if got := e.conn.callCount() - before; got != 1 {
		t.Fatalf("first leased run made %d source calls, want one page", got)
	}
	first := e.get(sub.ID)
	if first.Continuation == nil || first.Continuation.Cursor != "second" || first.Continuation.Pages != 1 {
		t.Fatalf("first continuation: %+v", first.Continuation)
	}
	if first.SinceAt != nil || first.InboxCatchup != nil {
		t.Fatalf("an intermediate page advanced the window or ended catch-up: %+v", first)
	}
	if first.NextRunAt == nil || first.LastRunAt == nil || !first.NextRunAt.Equal(*first.LastRunAt) {
		t.Fatalf("continuation was not due immediately: last %v next %v", first.LastRunAt, first.NextRunAt)
	}
	if got := itemUIDs(e.store.Items()); !slices.Equal(got, []string{"n2", "n3"}) {
		t.Fatalf("first page items: %v", got)
	}
	windowSince := first.Continuation.WindowSince
	firstCall := e.conn.calls[len(e.conn.calls)-1]
	if got := queryArgs(t, firstCall)["since"]; got != windowSince.Format(time.RFC3339) {
		t.Fatalf("first page since %v, window %s", got, windowSince)
	}

	e.svc.RunDue(e.ctx)
	if got := e.conn.callCount() - before; got != 2 {
		t.Fatalf("second leased run made %d total source calls, want two", got)
	}
	secondCall := e.conn.calls[len(e.conn.calls)-1]
	args := queryArgs(t, secondCall)
	if args["cursor"] != "second" || args["since"] != windowSince.Format(time.RFC3339) {
		t.Fatalf("continuation did not preserve cursor and original window: %v", args)
	}
	got := e.get(sub.ID)
	if got.Continuation != nil || got.SinceAt == nil || got.SinceAt.Format(time.RFC3339) != "2026-11-10T03:00:00Z" {
		t.Fatalf("terminal page state: %+v", got)
	}
	if got.InboxCatchup == nil || got.LastRunAt == nil || !got.InboxCatchup.Equal(*got.LastRunAt) {
		t.Fatalf("terminal page did not finish catch-up: %v %v", got.InboxCatchup, got.LastRunAt)
	}
	if got.NextRunAt == nil || !got.NextRunAt.After(e.clock.Now().Add(14*time.Minute)) {
		t.Fatalf("terminal page returned to its ordinary schedule: %v", got.NextRunAt)
	}
	if got := itemUIDs(e.store.Items()); !slices.Equal(got, []string{"n1", "n2", "n3"}) {
		t.Fatalf("all pages with duplicates: %v", got)
	}
	if e.store.SeenCount(sub.ID) != 3 {
		t.Fatalf("dedup keys after duplicate pages: %d", e.store.SeenCount(sub.ID))
	}
}

func TestPaginationRestartsAfterEveryCommittedPage(t *testing.T) {
	e := pagingEnv(t)
	e.conn.setAnswer(func(q connector.CatalogQuery) (string, error) {
		switch cursor, _ := queryArgs(t, q)["cursor"].(string); cursor {
		case "":
			return noticesPage("two", nil, 3), nil
		case "two":
			return noticesPage("three", nil, 2), nil
		case "three":
			return noticesPage(nil, nil, 1), nil
		default:
			return "", fmt.Errorf("unexpected cursor")
		}
	})
	sub := e.turnOn(e.create(cursorRecipe()))
	for page := 1; page <= 3; page++ {
		e.svc.RunDue(e.ctx)
		got := e.get(sub.ID)
		if page < 3 && (got.Continuation == nil || got.SinceAt != nil || got.InboxCatchup != nil) {
			t.Fatalf("page %d did not persist a resumable position: %+v", page, got)
		}
		// A crash after CommitRun loses every in-memory gate and wake-up. The stored immediate next run is enough.
		e.svc = e.service()
	}
	if got := itemUIDs(e.store.Items()); !slices.Equal(got, []string{"n1", "n2", "n3"}) {
		t.Fatalf("items after restarting between pages: %v", got)
	}
	if got := e.get(sub.ID); got.Continuation != nil || got.SinceAt == nil || got.InboxCatchup == nil {
		t.Fatalf("terminal state after restarts: %+v", got)
	}
}

func TestRunNowWhileOffFinishesItsContinuationAfterRestart(t *testing.T) {
	e := pagingEnv(t)
	e.conn.setAnswer(func(q connector.CatalogQuery) (string, error) {
		if cursor, _ := queryArgs(t, q)["cursor"].(string); cursor == "second" {
			return noticesPage(nil, nil, 2, 1), nil
		}
		return noticesPage("second", nil, 3), nil
	})
	sub := e.create(cursorRecipe())
	if _, err := e.svc.RunNow(e.ctx, sub.ID, Actor{}); err != nil {
		t.Fatal(err)
	}
	e.svc.RunDue(e.ctx)
	got := e.get(sub.ID)
	if got.State != StateOff || got.Continuation == nil || !got.Continuation.Manual || got.NextRunAt == nil {
		t.Fatalf("manual continuation while off: %+v", got)
	}
	e.svc = e.service() // no Run Now again: the persisted manual continuation is enough
	e.svc.RunDue(e.ctx)
	got = e.get(sub.ID)
	if got.State != StateOff || got.Continuation != nil || got.NextRunAt != nil || len(e.store.Items()) != 3 {
		t.Fatalf("finished manual continuation: %+v items %+v", got, e.store.Items())
	}
}

func TestPaginationCommitFailureReplaysOnlyTheCurrentPage(t *testing.T) {
	e := pagingEnv(t)
	e.conn.setAnswer(func(q connector.CatalogQuery) (string, error) {
		if cursor, _ := queryArgs(t, q)["cursor"].(string); cursor == "second" {
			return noticesPage(nil, nil, 2, 1), nil
		}
		return noticesPage("second", nil, 3), nil
	})
	sub := e.turnOn(e.create(cursorRecipe()))
	e.svc.RunDue(e.ctx)
	e.store.FailCommit = errors.New("manager stopped during commit")
	e.svc.RunDue(e.ctx)
	if got := itemUIDs(e.store.Items()); !slices.Equal(got, []string{"n3"}) {
		t.Fatalf("an uncommitted page was kept: %v", got)
	}
	if got := e.get(sub.ID); got.Continuation == nil || got.Continuation.Cursor != "second" || got.SinceAt != nil {
		t.Fatalf("failed commit moved the continuation: %+v", got)
	}
	e.store.FailCommit = nil
	e.svc = e.service()
	e.clock.advance(leaseLength + time.Second)
	e.svc.RunDue(e.ctx)
	if got := itemUIDs(e.store.Items()); !slices.Equal(got, []string{"n1", "n2", "n3"}) {
		t.Fatalf("replayed pages: %v", got)
	}
	if e.store.SeenCount(sub.ID) != 3 {
		t.Fatalf("seen keys after replay: %d", e.store.SeenCount(sub.ID))
	}
}

func TestPagePaginationUsesNumericArguments(t *testing.T) {
	e := pagingEnv(t)
	e.conn.setAnswer(func(q connector.CatalogQuery) (string, error) {
		switch page := queryArgs(t, q)["page"]; page {
		case float64(1):
			return noticesPage(nil, 2, 3, 2), nil
		case float64(2):
			return noticesPage(nil, nil, 1), nil
		default:
			return "", fmt.Errorf("unexpected page %v", page)
		}
	})
	sub := e.turnOn(e.create(numberedPageRecipe()))
	e.svc.RunDue(e.ctx)
	if got := e.get(sub.ID); got.Continuation == nil || got.Continuation.Page != 2 || got.SinceAt != nil {
		t.Fatalf("first numbered page: %+v", got)
	}
	e.svc.RunDue(e.ctx)
	if got := itemUIDs(e.store.Items()); !slices.Equal(got, []string{"n1", "n2", "n3"}) {
		t.Fatalf("numbered pages: %v", got)
	}
	last := queryArgs(t, e.conn.calls[len(e.conn.calls)-1])
	if last["page"] != float64(2) {
		t.Fatalf("page argument was not numeric: %v", last)
	}
}

func TestPagePaginationCanContinueOnAFullRequestedPage(t *testing.T) {
	e := pagingEnv(t)
	r := numberedPageRecipe()
	r.NextPagePath = ""
	r.Args = json.RawMessage(`{"since":"{{since}}","limit":2}`)
	e.conn.setAnswer(func(q connector.CatalogQuery) (string, error) {
		if queryArgs(t, q)["page"] == float64(2) {
			return noticesPage(nil, nil, 1), nil
		}
		return noticesPage(nil, nil, 3, 2), nil
	})
	sub := e.turnOn(e.create(r))
	e.svc.RunDue(e.ctx)
	if got := e.get(sub.ID); got.Continuation == nil || got.Continuation.Page != 2 || got.SinceAt != nil {
		t.Fatalf("full page did not continue: %+v", got)
	}
	e.svc.RunDue(e.ctx)
	if got := e.get(sub.ID); got.Continuation != nil || got.SinceAt == nil || len(e.store.Items()) != 3 {
		t.Fatalf("short terminal page: %+v items %+v", got, e.store.Items())
	}
}

func TestBaselineIsClearedOnlyAtTheTerminalPage(t *testing.T) {
	e := pagingEnv(t)
	e.conn.setAnswer(func(q connector.CatalogQuery) (string, error) {
		if cursor, _ := queryArgs(t, q)["cursor"].(string); cursor == "second" {
			return noticesPage(nil, nil, 2, 1), nil
		}
		return noticesPage("second", nil, 3, 2), nil
	})
	sub := e.create(cursorRecipe())
	changed := cursorRecipe()
	changed.Args = json.RawMessage(`{"since":"{{since}}","limit":20}`)
	toInbox := true
	updated, err := e.svc.Update(e.ctx, sub.ID, Patch{Revision: sub.Revision, Recipe: &changed, Existing: ExistingBaseline, ToInbox: &toInbox}, Actor{})
	if err != nil {
		t.Fatal(err)
	}
	e.svc.RunDue(e.ctx)
	got := e.get(sub.ID)
	if got.Baseline == nil || got.Continuation == nil || got.InboxCatchup != nil || len(e.store.Items()) != 0 || e.store.SeenCount(sub.ID) != 2 {
		t.Fatalf("intermediate baseline: %+v items %+v", got, e.store.Items())
	}
	e.svc.RunDue(e.ctx)
	got = e.get(sub.ID)
	if got.Baseline != nil || got.Continuation != nil || got.InboxCatchup == nil || len(e.store.Items()) != 0 || e.store.SeenCount(sub.ID) != 3 {
		t.Fatalf("terminal baseline: %+v items %+v", got, e.store.Items())
	}
	if updated.Revision != got.Revision {
		t.Fatalf("baseline runs changed the recipe revision: %d -> %d", updated.Revision, got.Revision)
	}
}

func TestInvalidAndRepeatedCursorsFailClosed(t *testing.T) {
	for _, tc := range []struct {
		name string
		next any
	}{
		{"repeated", "second"},
		{"wrong type", map[string]any{"token": "third"}},
		{"oversized", strings.Repeat("x", continuationCursorMax+1)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := pagingEnv(t)
			calls := 0
			e.conn.setAnswer(func(q connector.CatalogQuery) (string, error) {
				cursor, _ := queryArgs(t, q)["cursor"].(string)
				if cursor == "second" {
					calls++
					return noticesPage(tc.next, nil, 1), nil
				}
				return noticesPage("second", nil, 3), nil
			})
			sub := e.turnOn(e.create(cursorRecipe()))
			e.svc.RunDue(e.ctx)
			e.svc.RunDue(e.ctx)
			got := e.get(sub.ID)
			if got.State != StateNeedsRecipe || got.Error == nil || got.Error.Code != CodePagination {
				t.Fatalf("invalid cursor state: %+v", got)
			}
			if got.SinceAt != nil || got.InboxCatchup != nil || got.Continuation == nil || got.NextRunAt != nil {
				t.Fatalf("invalid cursor advanced or scheduled the pull: %+v", got)
			}
			if calls != 1 || len(e.store.Items()) != 1 || e.store.Items()[0].UID != "n3" {
				t.Fatalf("invalid page was partially committed: calls %d items %+v", calls, e.store.Items())
			}
		})
	}
}

func TestRepeatedPageAndPageCapFailClosed(t *testing.T) {
	e := pagingEnv(t)
	e.conn.setAnswer(func(q connector.CatalogQuery) (string, error) {
		if queryArgs(t, q)["page"] == float64(2) {
			return noticesPage(nil, 2, 1), nil
		}
		return noticesPage(nil, 2, 3), nil
	})
	sub := e.turnOn(e.create(numberedPageRecipe()))
	e.svc.RunDue(e.ctx)
	e.svc.RunDue(e.ctx)
	if got := e.get(sub.ID); got.State != StateNeedsRecipe || got.Error.Code != CodePagination || len(e.store.Items()) != 1 {
		t.Fatalf("repeated page: %+v items %+v", got, e.store.Items())
	}

	// A persisted position at the cap is refused before another source call; it cannot spin or advance the window.
	current := e.get(sub.ID)
	before := e.conn.callCount()
	current.Continuation.Pages = maxContinuationPages
	current.RunState, current.NextRunAt = StateOK, ptr(e.clock.Now())
	if ok, err := e.store.UpdateSubscription(e.ctx, current, current.Revision); err != nil || !ok {
		t.Fatalf("seed capped continuation: %v %v", ok, err)
	}
	e.svc = e.service()
	e.svc.RunDue(e.ctx)
	if e.conn.callCount() != before {
		t.Fatal("a continuation at the page cap called the source")
	}
	if got := e.get(sub.ID); got.Error == nil || got.Error.Code != CodePagination || got.SinceAt != nil {
		t.Fatalf("page cap: %+v", got)
	}
}

func TestRateLimitKeepsTheCommittedContinuation(t *testing.T) {
	e := pagingEnv(t)
	limited := false
	e.conn.setAnswer(func(q connector.CatalogQuery) (string, error) {
		cursor, _ := queryArgs(t, q)["cursor"].(string)
		if cursor == "second" {
			if limited {
				return "", &rpc.Error{Code: wire.ConnectorErrorCodeRateLimited, Data: json.RawMessage(`{"retry_after":600}`)}
			}
			return noticesPage(nil, nil, 2, 1), nil
		}
		return noticesPage("second", nil, 3), nil
	})
	sub := e.turnOn(e.create(cursorRecipe()))
	e.svc.RunDue(e.ctx)
	limited = true
	e.svc.RunDue(e.ctx)
	got := e.get(sub.ID)
	if got.State != StateRateLimited || got.Continuation == nil || got.Continuation.Cursor != "second" || got.SinceAt != nil || got.InboxCatchup != nil {
		t.Fatalf("rate limit lost its position: %+v", got)
	}
	if got := itemUIDs(e.store.Items()); !slices.Equal(got, []string{"n3"}) {
		t.Fatalf("items at rate limit: %v", got)
	}
	limited = false
	e.clock.advance(11 * time.Minute)
	e.svc.RunDue(e.ctx)
	if got := itemUIDs(e.store.Items()); !slices.Equal(got, []string{"n1", "n2", "n3"}) {
		t.Fatalf("items after rate limit: %v", got)
	}
	if got := e.get(sub.ID); got.State != StateOK || got.Continuation != nil || got.SinceAt == nil || got.InboxCatchup == nil {
		t.Fatalf("terminal state after rate limit: %+v", got)
	}
}

func TestContinuationRoundTripAndRecipeRace(t *testing.T) {
	since := time.Date(2026, 11, 9, 9, 0, 0, 0, time.UTC)
	until := since.Add(24 * time.Hour)
	newest := until.Add(-time.Hour)
	r := cursorRecipe()
	original := &Continuation{WindowSince: since, WindowEnd: until, Cursor: "next", Pages: 3,
		Cursors: []string{"one", "two", "next"}, NewestAt: &newest, SawItems: true, SawTimed: true}
	sub := Subscription{ID: "sub_a", Recipe: r, Continuation: original}
	raw, err := encode(sub)
	if err != nil {
		t.Fatal(err)
	}
	back, err := decode(raw)
	if err != nil || back.Continuation == nil || back.Continuation.Cursor != "next" || !back.Continuation.WindowSince.Equal(since) ||
		!back.Continuation.WindowEnd.Equal(until) || back.Continuation.NewestAt == nil || !back.Continuation.NewestAt.Equal(newest) ||
		!slices.Equal(back.Continuation.Cursors, original.Cursors) {
		t.Fatalf("continuation round trip: %+v %v", back.Continuation, err)
	}

	changed := r
	changed.Args = json.RawMessage(`{"since":"{{since}}","limit":20}`)
	current := Subscription{ID: sub.ID, Recipe: changed, ToInbox: true, Continuation: nil}
	applyRun(&current, RunCommit{Sub: sub}, false)
	if current.Continuation != nil {
		t.Fatalf("an old recipe's run replaced the new recipe's position: %+v", current.Continuation)
	}
	current.Recipe = r
	applyRun(&current, RunCommit{Sub: sub}, false)
	if current.Continuation == nil || current.Continuation.Cursor != "next" {
		t.Fatalf("the same recipe's committed position was lost: %+v", current.Continuation)
	}
	current.Continuation.Cursors[0] = "changed"
	if original.Cursors[0] == "changed" {
		t.Fatal("a run commit aliased the prior continuation's history")
	}
}

// runWhenDue moves the clock to the subscription's next run and runs what is due.
func (e *env) runWhenDue(id string) {
	e.t.Helper()
	if next := e.get(id).NextRunAt; next != nil && next.After(e.clock.Now()) {
		e.clock.advance(next.Sub(e.clock.Now()))
	}
	e.svc.RunDue(e.ctx)
}

// expiringCursorSource answers a cursor pull whose page token "second" is refused once expired is set, as a source
// does after a restart, an outage or a long backoff; a pull without a cursor still answers, with newer items (#6310).
func expiringCursorSource(t *testing.T, e *env, expired *bool, refuse func() (string, error)) *[]connector.CatalogQuery {
	var calls []connector.CatalogQuery
	e.conn.setAnswer(func(q connector.CatalogQuery) (string, error) {
		calls = append(calls, q)
		switch cursor, _ := queryArgs(t, q)["cursor"].(string); {
		case cursor == "" && *expired:
			return noticesPage(nil, nil, 5, 4, 3), nil // the first page's n3 again, and what came since
		case cursor == "":
			return noticesPage("second", nil, 3), nil
		case cursor == "second" && !*expired:
			return noticesPage(nil, nil, 2, 1), nil
		default:
			return refuse()
		}
	})
	return &calls
}

func cursorsOf(t *testing.T, calls []connector.CatalogQuery) []string {
	out := make([]string, len(calls))
	for i, q := range calls {
		out[i], _ = queryArgs(t, q)["cursor"].(string)
	}
	return out
}

func TestAnExpiredCursorStartsAFreshPullOnItsSecondFailure(t *testing.T) {
	for _, tc := range []struct {
		name   string
		refuse func() (string, error)
	}{
		{"call error", func() (string, error) { return "", errors.New("invalid page_token: expired") }},
		{"no list", func() (string, error) { return `{"error":"invalid page_token"}`, nil }}, // not a shape miss
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := pagingEnv(t)
			expired := false
			calls := expiringCursorSource(t, e, &expired, tc.refuse)
			sub := e.turnOn(e.create(cursorRecipe()))
			*calls = nil // the dry run's
			e.svc.RunDue(e.ctx)
			window := e.get(sub.ID).Continuation
			if window == nil || window.Cursor != "second" {
				t.Fatalf("first page's continuation: %+v", window)
			}
			expired = true

			e.runWhenDue(sub.ID)
			got := e.get(sub.ID)
			if got.State != StateFailing || got.FailCount != 1 || got.Continuation == nil || got.Continuation.Cursor != "second" {
				t.Fatalf("a first failure kept no position to resume: state %s fails %d %+v", got.State, got.FailCount, got.Continuation)
			}
			e.runWhenDue(sub.ID)
			got = e.get(sub.ID)
			if got.State != StateFailing || got.FailCount != 2 || got.Continuation != nil || got.NextRunAt == nil {
				t.Fatalf("a second failure kept the dead cursor: state %s fails %d %+v", got.State, got.FailCount, got.Continuation)
			}
			e.runWhenDue(sub.ID)
			got = e.get(sub.ID)
			if got.State != StateOK || got.FailCount != 0 || got.Continuation != nil || got.SinceAt == nil || got.InboxCatchup == nil {
				t.Fatalf("the fresh pull did not recover: %+v", got)
			}
			if want := []string{"", "second", "second", ""}; !slices.Equal(cursorsOf(t, *calls), want) {
				t.Fatalf("cursors sent %q, want %q", cursorsOf(t, *calls), want)
			}
			if since := queryArgs(t, (*calls)[3])["since"]; since != window.WindowSince.Format(time.RFC3339) {
				t.Fatalf("the fresh pull read from %v, not the first pull's start %s", since, window.WindowSince)
			}
			// n3 came on the first page and again in the fresh pull: held and delivered once.
			if got := itemUIDs(e.store.Items()); !slices.Equal(got, []string{"n3", "n4", "n5"}) {
				t.Fatalf("items after the fresh pull: %v", got)
			}
			if e.store.SeenCount(sub.ID) != 3 {
				t.Fatalf("seen keys after the fresh pull: %d", e.store.SeenCount(sub.ID))
			}
		})
	}
}

func TestATransientPageFailureResumesItsCursor(t *testing.T) {
	e := pagingEnv(t)
	var calls []connector.CatalogQuery
	refused := false
	e.conn.setAnswer(func(q connector.CatalogQuery) (string, error) {
		calls = append(calls, q)
		switch cursor, _ := queryArgs(t, q)["cursor"].(string); {
		case cursor == "":
			return noticesPage("second", nil, 3), nil
		case !refused:
			refused = true
			return "", errors.New("temporarily unavailable")
		default:
			return noticesPage(nil, nil, 2, 1), nil
		}
	})
	sub := e.turnOn(e.create(cursorRecipe()))
	calls = nil // the dry run's
	e.svc.RunDue(e.ctx)
	e.runWhenDue(sub.ID)
	e.runWhenDue(sub.ID)
	if want := []string{"", "second", "second"}; !slices.Equal(cursorsOf(t, calls), want) {
		t.Fatalf("cursors sent %q, want %q: one failure re-read the committed pages", cursorsOf(t, calls), want)
	}
	got := e.get(sub.ID)
	if got.State != StateOK || got.Continuation != nil || got.SinceAt == nil {
		t.Fatalf("after the resumed page: %+v", got)
	}
	if got := itemUIDs(e.store.Items()); !slices.Equal(got, []string{"n1", "n2", "n3"}) {
		t.Fatalf("items: %v", got)
	}
}

func TestTurningOffDropsTheContinuation(t *testing.T) {
	e := pagingEnv(t)
	expired := false
	calls := expiringCursorSource(t, e, &expired, func() (string, error) { return "", errors.New("invalid page_token: expired") })
	sub := e.turnOn(e.create(cursorRecipe()))
	*calls = nil // the dry run's
	e.svc.RunDue(e.ctx)
	expired = true
	e.runWhenDue(sub.ID)
	got := e.get(sub.ID)
	off, err := e.svc.Update(e.ctx, sub.ID, Patch{Revision: got.Revision, ToInbox: ptr(false)}, Actor{ID: "device:phone"})
	if err != nil {
		t.Fatal(err)
	}
	if got := e.get(sub.ID); got.Continuation != nil || got.NextRunAt != nil {
		t.Fatalf("off kept its position: next %v %+v", got.NextRunAt, got.Continuation)
	}
	e.turnOn(off)
	e.svc.RunDue(e.ctx)
	if want := []string{"", "second", ""}; !slices.Equal(cursorsOf(t, *calls), want) {
		t.Fatalf("cursors sent %q, want %q", cursorsOf(t, *calls), want)
	}
	if got := e.get(sub.ID); got.State != StateOK || got.Continuation != nil {
		t.Fatalf("on again: %+v", got)
	}
}

func TestARunThatEndsAfterTurningOffKeepsNoPosition(t *testing.T) {
	r := cursorRecipe()
	since := time.Date(2026, 11, 9, 9, 0, 0, 0, time.UTC)
	ran := Subscription{ID: "sub_a", Recipe: r, ToInbox: true,
		Continuation: &Continuation{WindowSince: since, WindowEnd: since.Add(24 * time.Hour), Cursor: "next", Pages: 1, Cursors: []string{"next"}}}
	off := Subscription{ID: ran.ID, Recipe: r}
	applyRun(&off, RunCommit{Sub: ran}, false)
	if off.Continuation != nil {
		t.Fatalf("a page that ran while it was turned off kept its position: %+v", off.Continuation)
	}
	ran.Continuation.Manual = true // Run Now while off goes on to the end
	applyRun(&off, RunCommit{Sub: ran}, false)
	if off.Continuation == nil || off.Continuation.Cursor != "next" {
		t.Fatalf("a manual pull's position was dropped: %+v", off.Continuation)
	}
}
