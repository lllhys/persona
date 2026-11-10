package subscriptions

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nwverse/persona/sdk/go/wire"
	"github.com/nwverse/persona/server/internal/connector"
	"github.com/nwverse/persona/server/internal/rpc"
)

// fakeClock moves only when the runner sleeps or a test advances it.
type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *fakeClock) Now() time.Time { c.mu.Lock(); defer c.mu.Unlock(); return c.t }
func (c *fakeClock) Sleep(ctx context.Context, d time.Duration) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	c.advance(d)
	return nil
}
func (c *fakeClock) advance(d time.Duration) { c.mu.Lock(); c.t = c.t.Add(d); c.mu.Unlock() }

// fakeConn is a connector with a catalog: its tools answer what answer says.
type fakeConn struct {
	id      string
	mu      sync.Mutex
	sources []connector.CatalogSource
	answer  func(q connector.CatalogQuery) (string, error)
	calls   []connector.CatalogQuery
}

func (f *fakeConn) ID() string   { return f.id }
func (f *fakeConn) Name() string { return strings.ToUpper(f.id) }
func (f *fakeConn) Manifest() connector.Manifest {
	return connector.Manifest{Capabilities: []connector.Capability{connector.CapCatalog}}
}
func (f *fakeConn) Catalog(context.Context) ([]connector.CatalogSource, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.sources, nil
}
func (f *fakeConn) CatalogCall(_ context.Context, q connector.CatalogQuery) (string, error) {
	f.mu.Lock()
	f.calls = append(f.calls, q)
	answer := f.answer
	f.mu.Unlock()
	return answer(q)
}
func (f *fakeConn) callCount() int { f.mu.Lock(); defer f.mu.Unlock(); return len(f.calls) }
func (f *fakeConn) setSources(s []connector.CatalogSource) {
	f.mu.Lock()
	f.sources = s
	f.mu.Unlock()
}
func (f *fakeConn) setAnswer(a func(connector.CatalogQuery) (string, error)) {
	f.mu.Lock()
	f.answer = a
	f.mu.Unlock()
}

type fakeConns struct {
	mu    sync.Mutex
	infos map[string]*ConnectorInfo
}

func (fc *fakeConns) Find(_ context.Context, id string) (ConnectorInfo, bool) {
	fc.mu.Lock()
	defer fc.mu.Unlock()
	c, ok := fc.infos[id]
	if !ok {
		return ConnectorInfo{}, false
	}
	return *c, true
}
func (fc *fakeConns) All(context.Context) []ConnectorInfo {
	fc.mu.Lock()
	defer fc.mu.Unlock()
	var out []ConnectorInfo
	for _, c := range fc.infos {
		out = append(out, *c)
	}
	return out
}
func (fc *fakeConns) setStatus(id, st string) {
	fc.mu.Lock()
	fc.infos[id].Status = st
	fc.mu.Unlock()
}

func ptr[T any](v T) *T { return &v }

// noticesTool lists notices; noticesSource has it and a tool whose name says it writes.
var noticesSchema = json.RawMessage(`{"type":"object","properties":{"since":{"type":"string"},"limit":{"type":"integer","maximum":50}},"required":["since"]}`)

func noticesSource() connector.CatalogSource {
	return connector.CatalogSource{ID: "notices", Name: "Notices", Icon: "mail", Tools: []connector.CatalogTool{
		{Name: "list_notices", Description: "List notices", ReadOnly: true, InputSchema: noticesSchema},
		{Name: "send_message", Description: "A read tool by its declaration, a write by its name", ReadOnly: true, InputSchema: json.RawMessage(`{"type":"object"}`)},
		{Name: "balance", Description: "The balance, as text", ReadOnly: true, InputSchema: json.RawMessage(`{"type":"object"}`)},
	}}
}

func itemsRecipe() Recipe {
	return Recipe{Mode: "items", Tool: "list_notices", Args: json.RawMessage(`{"since":"{{since}}","limit":25}`), ItemsPath: "$.items",
		Fields: &connector.SubscriptionFields{ID: "$.id", Title: "$.title", Sender: "$.from.name", Time: "$.at", Body: "$.text"},
		Kind:   "sub_notice", GroupBy: "source"}
}

func notices(ids ...int) string {
	var items []map[string]any
	for _, id := range ids {
		items = append(items, map[string]any{"id": fmt.Sprint("n", id), "title": fmt.Sprint("Notice ", id), "from": map[string]any{"name": "Office"},
			"at": fmt.Sprintf("2026-11-10T0%d:00:00Z", id%10), "text": "Made-up notice text"})
	}
	b, _ := json.Marshal(map[string]any{"items": items, "next": nil})
	return string(b)
}

type env struct {
	t     *testing.T
	ctx   context.Context
	clock *fakeClock
	store *MemStore
	conn  *fakeConn
	conns *fakeConns
	svc   *Service
	audit []string
	mu    sync.Mutex
}

func newEnv(t *testing.T) *env {
	e := &env{t: t, ctx: context.Background(), clock: &fakeClock{t: time.Date(2026, 11, 10, 9, 0, 0, 0, time.UTC)}, store: NewMemStore()}
	e.conn = &fakeConn{id: "example", sources: []connector.CatalogSource{noticesSource()}}
	e.conn.answer = func(connector.CatalogQuery) (string, error) { return notices(1, 2, 3), nil }
	e.conns = &fakeConns{infos: map[string]*ConnectorInfo{"example": {ID: "example", Extension: "examplechat", Status: connector.StateOK, Enabled: true, Conn: e.conn}}}
	e.svc = e.service()
	return e
}

// service is a Service over the env's store and connectors, as a manager makes one (again after a restart).
func (e *env) service() *Service {
	return New(Options{Store: e.store, Connectors: e.conns, Clock: e.clock, Location: func() *time.Location { return time.UTC },
		Audit: func(_ context.Context, id, action string, by Actor, _ map[string]any) {
			e.mu.Lock()
			e.audit = append(e.audit, id+" "+action+" "+by.ID)
			e.mu.Unlock()
		}})
}

func (e *env) create(r Recipe) Subscription {
	e.t.Helper()
	sub, err := e.svc.Create(e.ctx, CreateRequest{Connector: "example", Source: "notices", Recipe: &r}, Actor{ID: "device:phone"})
	if err != nil {
		var se *Error
		if errors.As(err, &se) && se.DryRun != nil {
			e.t.Fatalf("create: %v (dry run %+v)", err, se.DryRun.Checks)
		}
		e.t.Fatalf("create: %v", err)
	}
	return sub
}

func (e *env) turnOn(sub Subscription) Subscription {
	e.t.Helper()
	out, err := e.svc.Update(e.ctx, sub.ID, Patch{Revision: sub.Revision, ToInbox: ptr(true)}, Actor{ID: "device:phone"})
	if err != nil {
		e.t.Fatal(err)
	}
	return out
}

func (e *env) get(id string) Subscription {
	e.t.Helper()
	sub, _, err := e.svc.Get(e.ctx, id)
	if err != nil {
		e.t.Fatal(err)
	}
	return sub
}

func codeOf(err error) string {
	var se *Error
	if errors.As(err, &se) {
		return se.Code
	}
	return ""
}

func TestAHandWrittenRecipeRunsOnSchedule(t *testing.T) {
	e := newEnv(t)
	sub := e.create(itemsRecipe())
	if sub.State != StateOff || sub.ToInbox || sub.ToKB || sub.IntervalSeconds != 900 || sub.RecipeOrigin != OriginUser {
		t.Fatalf("a new subscription: %+v", sub)
	}
	e.svc.RunDue(e.ctx)
	if n := len(e.store.Items()); n != 0 {
		t.Fatalf("an off subscription ran: %d items", n)
	}
	before := e.conn.callCount() // the dry run at save
	sub = e.turnOn(sub)
	e.svc.RunDue(e.ctx)
	if got := e.conn.callCount() - before; got != 1 {
		t.Fatalf("calls after turning it on: %d", got)
	}
	if n := len(e.store.Items()); n != 3 {
		t.Fatalf("items: %d", n)
	}
	sub = e.get(sub.ID)
	if sub.State != StateOK || sub.LastNewCount != 3 || sub.NextRunAt == nil || sub.LastOKAt == nil {
		t.Fatalf("after a run: %+v", sub)
	}
	// Not due yet: nothing is called.
	e.clock.advance(10 * time.Minute)
	e.svc.RunDue(e.ctx)
	if got := e.conn.callCount() - before; got != 1 {
		t.Fatalf("ran before its interval: %d calls", got)
	}
	// Due: the same items again and one new.
	e.conn.setAnswer(func(connector.CatalogQuery) (string, error) { return notices(1, 2, 3, 4), nil })
	e.clock.advance(10 * time.Minute)
	e.svc.RunDue(e.ctx)
	if n := len(e.store.Items()); n != 4 {
		t.Fatalf("items after the second run: %d, want 4", n)
	}
	if q := e.conn.calls[len(e.conn.calls)-1]; !strings.Contains(string(q.Args), `"since":"2026-11-10T02:00:00Z"`) {
		t.Fatalf("the second run asked from %s, want an hour before the newest item", q.Args)
	}
	_, runs, _ := e.svc.Get(e.ctx, sub.ID)
	if len(runs) != 2 || runs[0].New != 1 || runs[0].Count != 4 || runs[1].New != 3 || runs[0].Trigger != TriggerSchedule {
		t.Fatalf("runs: %+v", runs)
	}
}

func TestARestartMidRunReplaysWithoutDuplicates(t *testing.T) {
	e := newEnv(t)
	sub := e.turnOn(e.create(itemsRecipe()))
	e.store.FailCommit = errors.New("the manager stopped")
	e.svc.RunDue(e.ctx)
	if n := len(e.store.Items()); n != 0 {
		t.Fatalf("a run that died kept %d items", n)
	}
	e.store.FailCommit = nil
	// The manager restarts: a new service over the same database. The dead run's lease holds for 5 minutes.
	e.svc = e.service()
	calls := e.conn.callCount()
	e.svc.RunDue(e.ctx)
	if e.conn.callCount() != calls {
		t.Fatal("ran while the dead run's lease held")
	}
	e.clock.advance(leaseLength + time.Second)
	e.svc.RunDue(e.ctx)
	e.clock.advance(time.Hour)
	e.svc.RunDue(e.ctx) // and once more: the same answer
	if n := len(e.store.Items()); n != 3 {
		t.Fatalf("items after the replays: %d, want 3", n)
	}
	if e.store.SeenCount(sub.ID) != 3 {
		t.Fatalf("seen keys: %d", e.store.SeenCount(sub.ID))
	}
}

func TestToolsThatMayWriteAreRefusedAtSaveAndBeforeEachRun(t *testing.T) {
	e := newEnv(t)
	r := itemsRecipe()
	r.Tool = "send_message"
	_, err := e.svc.Create(e.ctx, CreateRequest{Connector: "example", Source: "notices", Recipe: &r}, Actor{})
	if codeOf(err) != CodeWriteVerb {
		t.Fatalf("a tool named as a write: %v", err)
	}
	r.Tool = "delete_everything" // not in the catalog at all
	if _, err = e.svc.Create(e.ctx, CreateRequest{Connector: "example", Source: "notices", Recipe: &r}, Actor{}); codeOf(err) != CodeNotReadOnly {
		t.Fatalf("a tool outside the read-only catalog: %v", err)
	}
	// A catalog whose hint says a tool writes is refused as a whole, as catalog.list / catalog.call refuse it.
	src := noticesSource()
	src.Tools[0].Annotations = &connector.CatalogAnnotations{DestructiveHint: ptr(true)}
	e.conn.setSources([]connector.CatalogSource{src})
	if _, err = e.svc.Create(e.ctx, CreateRequest{Connector: "example", Source: "notices", Recipe: ptr(itemsRecipe())}, Actor{}); codeOf(err) != CodeNotReadOnly {
		t.Fatalf("a tool hinted as destructive: %v", err)
	}
	// No hints at all are no reason to refuse.
	e.conn.setSources([]connector.CatalogSource{noticesSource()})
	sub := e.turnOn(e.create(itemsRecipe()))
	e.svc.RunDue(e.ctx)
	// Before the next run the tool is hinted as not read-only: the run calls nothing and waits for a new recipe.
	src = noticesSource()
	src.Tools[0].Annotations = &connector.CatalogAnnotations{ReadOnlyHint: ptr(false)}
	e.conn.setSources([]connector.CatalogSource{src})
	calls := e.conn.callCount()
	e.clock.advance(time.Hour)
	e.svc.RunDue(e.ctx)
	if e.conn.callCount() != calls {
		t.Fatal("called a tool that is no longer read-only")
	}
	// The whole catalog is refused (catalog.list's rule), so the run fails without a call and recovers once the
	// extension declares its tools consistently again.
	if got := e.get(sub.ID); got.State != StateFailing || got.Error.Code != CodeNotReadOnly {
		t.Fatalf("after the tool turned out to write: %+v", got)
	}
	// The tool is gone from the read-only catalog: this subscription's own problem, so it needs a new recipe.
	src = noticesSource()
	src.Tools[0].Name = "list_notices_and_mark_read"
	e.conn.setSources([]connector.CatalogSource{src})
	e.clock.advance(2 * time.Hour)
	e.svc.RunDue(e.ctx)
	if got := e.get(sub.ID); got.State != StateNeedsRecipe || got.NextRunAt != nil || e.conn.callCount() != calls {
		t.Fatalf("after the tool went away: %+v", got)
	}
}

func TestRateLimitedPausesTheWholeConnector(t *testing.T) {
	e := newEnv(t)
	a := e.turnOn(e.create(itemsRecipe()))
	b := e.turnOn(e.create(itemsRecipe()))
	e.conn.setAnswer(func(connector.CatalogQuery) (string, error) {
		return "", &rpc.Error{Code: wire.ConnectorErrorCodeRateLimited, Message: "slow down", Data: json.RawMessage(`{"retry_after":600}`)}
	})
	calls := e.conn.callCount()
	e.svc.RunDue(e.ctx)
	if got := e.conn.callCount() - calls; got != 1 {
		t.Fatalf("calls after rate_limited: %d, want 1", got)
	}
	for _, id := range []string{a.ID, b.ID} {
		s := e.get(id)
		if s.State != StateRateLimited || s.PausedUntil == nil || s.FailCount != 0 {
			t.Fatalf("%s after rate_limited: %+v", id, s)
		}
	}
	// A restarted manager keeps the pause.
	e.svc = e.service()
	e.clock.advance(5 * time.Minute)
	e.svc.RunNow(e.ctx, a.ID, Actor{})
	e.svc.RunDue(e.ctx)
	if got := e.conn.callCount() - calls; got != 1 {
		t.Fatalf("called while paused: %d", got)
	}
	e.conn.setAnswer(func(connector.CatalogQuery) (string, error) { return notices(1), nil })
	e.clock.advance(10 * time.Minute)
	e.svc.RunDue(e.ctx)
	if s := e.get(a.ID); s.State != StateOK {
		t.Fatalf("after the pause: %+v", s)
	}
}

func TestThreeFailedCallsBackTheConnectorOff(t *testing.T) {
	e := newEnv(t)
	var subs []Subscription
	for range 4 {
		subs = append(subs, e.turnOn(e.create(itemsRecipe())))
	}
	e.conn.setAnswer(func(connector.CatalogQuery) (string, error) { return "", errors.New("upstream said no") })
	calls := e.conn.callCount()
	e.svc.RunDue(e.ctx)
	if got := e.conn.callCount() - calls; got != 3 {
		t.Fatalf("calls before the connector backed off: %d, want 3", got)
	}
	failing := 0
	for _, s := range subs {
		s = e.get(s.ID)
		if s.State == StateFailing {
			failing++
			if s.FailCount != 1 || s.Error == nil || s.Error.Code != CodeCallFailed || strings.Contains(s.Error.Message, "upstream") {
				t.Fatalf("a failed run: %+v", s)
			}
			if d := s.NextRunAt.Sub(*s.LastRunAt); d != 30*time.Minute {
				t.Fatalf("retried after %s, want the interval doubled", d)
			}
		}
	}
	paused := 0
	for _, s := range subs {
		if s = e.get(s.ID); s.State == StateRateLimited && s.LastRunAt == nil && s.FailCount == 0 {
			paused++
		}
	}
	if failing != 3 || paused != 1 {
		t.Fatalf("failing %d, paused %d: want 3 and the fourth held back", failing, paused)
	}
}

func TestBackoffNeverStopsAndCapsAtSixHours(t *testing.T) {
	if d := backoff(15*time.Minute, 10); d != 6*time.Hour {
		t.Fatalf("backoff after 10 failures: %s", d)
	}
	if d := backoff(15*time.Minute, 2); d != time.Hour {
		t.Fatalf("backoff after 2 failures: %s", d)
	}
}

func TestTheHourlyBudgetMovesRuns(t *testing.T) {
	e := newEnv(t)
	src := noticesSource()
	src.MaxCallsPerHour = ptr(2)
	e.conn.setSources([]connector.CatalogSource{src})
	var subs []Subscription
	for range 3 {
		subs = append(subs, e.turnOn(e.create(itemsRecipe())))
	}
	calls := e.conn.callCount()
	e.svc.RunDue(e.ctx)
	if got := e.conn.callCount() - calls; got != 2 {
		t.Fatalf("calls in the hour: %d, want 2", got)
	}
	held := 0
	for _, sub := range subs {
		if s := e.get(sub.ID); s.LastRunAt == nil {
			held++
			if s.NextRunAt == nil || s.NextRunAt.Before(e.clock.Now().Add(50*time.Minute)) || s.State != StateOK {
				t.Fatalf("the run over budget: %+v", s)
			}
		}
	}
	if held != 1 {
		t.Fatalf("held back: %d, want 1", held)
	}
}

func TestAConnectorThatCannotBeCalledIsNotAFailure(t *testing.T) {
	e := newEnv(t)
	sub := e.turnOn(e.create(itemsRecipe()))
	e.conns.setStatus("example", connector.StateNotSignedIn)
	calls := e.conn.callCount()
	e.svc.RunDue(e.ctx)
	s := e.get(sub.ID)
	if s.State != StateNotConnected || s.FailCount != 0 || e.conn.callCount() != calls {
		t.Fatalf("signed out: %+v", s)
	}
	if _, runs, _ := e.svc.Get(e.ctx, sub.ID); len(runs) != 0 {
		t.Fatalf("a skipped round was recorded: %+v", runs)
	}
	e.conns.setStatus("example", connector.StateOK)
	e.clock.advance(time.Hour)
	e.svc.RunDue(e.ctx)
	if s := e.get(sub.ID); s.State != StateOK || s.LastNewCount != 3 {
		t.Fatalf("signed in again: %+v", s)
	}
}

func TestTextResultsAreSnapshotsOnly(t *testing.T) {
	e := newEnv(t)
	e.conn.setAnswer(func(q connector.CatalogQuery) (string, error) { return "Balance: 42.00", nil })
	r := itemsRecipe()
	r.Tool, r.Args = "balance", nil
	dr, err := e.svc.DryRun(e.ctx, DryRunRequest{Connector: "example", Source: "notices", Recipe: &r})
	if err != nil || dr.OK || dr.Error == nil || dr.Error.Code != CodeTextNeedsSnap || dr.Shape == nil || dr.Shape.Type != "text" {
		t.Fatalf("an items recipe on text: %+v %v", dr, err)
	}
	snap := Recipe{Mode: "snapshot", Tool: "balance", Kind: "sub_update", GroupBy: "source"}
	sub := e.turnOn(e.create(snap))
	if sub.IntervalSeconds != 86400 || sub.DailyAt != "08:00" || sub.MinIntervalSeconds != 3600 {
		t.Fatalf("a snapshot's schedule: %+v", sub)
	}
	e.svc.RunDue(e.ctx)
	e.svc.RunNow(e.ctx, sub.ID, Actor{})
	e.svc.RunDue(e.ctx)
	if n := len(e.store.Items()); n != 1 {
		t.Fatalf("an unchanged snapshot was kept again: %d items", n)
	}
	e.conn.setAnswer(func(q connector.CatalogQuery) (string, error) { return "Balance: 12.00", nil })
	e.svc.RunNow(e.ctx, sub.ID, Actor{})
	e.svc.RunDue(e.ctx)
	if n := len(e.store.Items()); n != 2 {
		t.Fatalf("a changed snapshot: %d items", n)
	}
	if s := e.get(sub.ID); s.NextRunAt == nil || s.NextRunAt.Hour() != 8 {
		t.Fatalf("the next snapshot is not at 08:00: %+v", s.NextRunAt)
	}
}

func TestDryRunChecksAndRecordsOnlyTheShape(t *testing.T) {
	e := newEnv(t)
	dr, err := e.svc.DryRun(e.ctx, DryRunRequest{Connector: "example", Source: "notices", Recipe: ptr(itemsRecipe())})
	if err != nil || !dr.OK {
		t.Fatalf("dry run: %+v %v", dr, err)
	}
	keys := []string{}
	for _, c := range dr.Checks {
		keys = append(keys, c.Key)
	}
	if strings.Join(keys, ",") != "read_only,args,fields,dedup,interval" {
		t.Fatalf("checks: %v", keys)
	}
	if dr.Count != 3 || len(dr.Items) != 3 || !dr.Items[0].New || dr.WeakDedup || dr.MaybeMore {
		t.Fatalf("dry run items: %+v", dr)
	}
	if dr.Shape.Type != "json" || strings.Join(dr.Shape.Keys, ",") != "items,next" || *dr.Shape.Items != 3 {
		t.Fatalf("shape: %+v", dr.Shape)
	}
	if len(dr.Paths) == 0 || dr.Paths[0].Path != "$.at" {
		t.Fatalf("paths: %+v", dr.Paths)
	}
	sub := e.create(itemsRecipe())
	b, _ := json.Marshal(sub)
	if strings.Contains(string(b), "Notice 1") || strings.Contains(string(b), "Made-up") {
		t.Fatalf("a subscription keeps source values: %s", b)
	}
	// An empty items_path finds the one list in the result, says where, and is saved as found.
	r0 := itemsRecipe()
	r0.ItemsPath = ""
	dr, _ = e.svc.DryRun(e.ctx, DryRunRequest{Connector: "example", Source: "notices", Recipe: &r0})
	if !dr.OK || dr.ItemsPath != "$.items" || strings.Join(dr.Shape.Arrays, ",") != "items" {
		t.Fatalf("items_path found: %+v %+v", dr, dr.Shape)
	}
	if found := e.create(r0); found.Recipe.ItemsPath != "$.items" {
		t.Fatalf("the found items_path was not saved: %q", found.Recipe.ItemsPath)
	}
	e.conn.setAnswer(func(connector.CatalogQuery) (string, error) {
		return `{"items":[{"title":"a"}],"others":[1],"n":2}`, nil
	})
	dr, _ = e.svc.DryRun(e.ctx, DryRunRequest{Connector: "example", Source: "notices", Recipe: &r0})
	if dr.OK || dr.Error.Code != CodeNoList || dr.ItemsPath != "" || strings.Join(dr.Shape.Arrays, ",") != "items,others" {
		t.Fatalf("two lists: %+v %+v", dr, dr.Shape)
	}
	e.conn.setAnswer(func(connector.CatalogQuery) (string, error) { return `[{"id":"1","title":"a"}]`, nil })
	if dr, _ = e.svc.DryRun(e.ctx, DryRunRequest{Connector: "example", Source: "notices", Recipe: &r0}); !dr.OK || dr.ItemsPath != "$" {
		t.Fatalf("a list itself: %+v", dr)
	}
	e.conn.setAnswer(func(connector.CatalogQuery) (string, error) { return notices(1, 2, 3), nil })
	// Without an id, dedup falls back to a hash, marked as weak.
	r := itemsRecipe()
	r.Fields.ID = ""
	dr, _ = e.svc.DryRun(e.ctx, DryRunRequest{Connector: "example", Source: "notices", Recipe: &r})
	if !dr.OK || !dr.WeakDedup {
		t.Fatalf("no id: %+v", dr)
	}
	// Arguments that do not fit the tool, a variable that does not exist, a title path that finds nothing.
	r = itemsRecipe()
	r.Args = json.RawMessage(`{"since":"{{since}}","limit":500}`)
	if dr, _ = e.svc.DryRun(e.ctx, DryRunRequest{Connector: "example", Source: "notices", Recipe: &r}); dr.OK || dr.Checks[1].OK {
		t.Fatalf("limit over the schema's maximum: %+v", dr.Checks)
	}
	r.Args = json.RawMessage(`{"since":"{{yesterday}}"}`)
	if dr, _ = e.svc.DryRun(e.ctx, DryRunRequest{Connector: "example", Source: "notices", Recipe: &r}); dr.OK || dr.Error.Code != CodeBadArgs {
		t.Fatalf("an unknown variable: %+v", dr)
	}
	r = itemsRecipe()
	r.Fields.Title = "$.subject"
	if dr, _ = e.svc.DryRun(e.ctx, DryRunRequest{Connector: "example", Source: "notices", Recipe: &r}); dr.OK || dr.Checks[2].Error.Code != CodeNoTitle {
		t.Fatalf("no title: %+v", dr.Checks)
	}
	r = itemsRecipe()
	r.ItemsPath = "$.next"
	if dr, _ = e.svc.DryRun(e.ctx, DryRunRequest{Connector: "example", Source: "notices", Recipe: &r}); dr.OK || dr.Error.Code != CodeNoList {
		t.Fatalf("not a list: %+v", dr)
	}
}

func TestIntervalFloorsAndRevisions(t *testing.T) {
	e := newEnv(t)
	src := noticesSource()
	src.MinIntervalSeconds = ptr(600)
	e.conn.setSources([]connector.CatalogSource{src})
	sub := e.create(itemsRecipe())
	if sub.MinIntervalSeconds != 600 || sub.IntervalSeconds != 900 {
		t.Fatalf("floors: %+v", sub)
	}
	if _, err := e.svc.Update(e.ctx, sub.ID, Patch{Revision: sub.Revision, IntervalSeconds: ptr(300)}, Actor{}); codeOf(err) != CodeIntervalTooShort {
		t.Fatalf("under the source's minimum: %v", err)
	}
	up, err := e.svc.Update(e.ctx, sub.ID, Patch{Revision: sub.Revision, IntervalSeconds: ptr(600), Label: ptr("Office notices")}, Actor{ID: "device:phone"})
	if err != nil || up.Revision != sub.Revision+1 || up.Label != "Office notices" {
		t.Fatalf("update: %+v %v", up, err)
	}
	_, err = e.svc.Update(e.ctx, sub.ID, Patch{Revision: sub.Revision, ToKB: ptr(true)}, Actor{})
	var se *Error
	if !errors.As(err, &se) || se.Code != CodeConflict || se.Current == nil || se.Current.Revision != up.Revision {
		t.Fatalf("a stale revision: %v", err)
	}
	if !strings.Contains(strings.Join(e.audit, "\n"), "example subscription_updated device:phone") {
		t.Fatalf("audit: %v", e.audit)
	}
}

func TestAChangedToolEitherStillFitsOrNeedsANewRecipe(t *testing.T) {
	e := newEnv(t)
	sub := e.turnOn(e.create(itemsRecipe()))
	e.svc.RunDue(e.ctx)
	// Only the description changed: it carries on.
	src := noticesSource()
	src.Tools[0].Description = "List the office's notices"
	src.Tools[0].InputSchema = json.RawMessage(`{"type":"object","properties":{"since":{"type":"string"},"limit":{"type":"integer"},"page":{"type":"string"}},"required":["since"]}`)
	e.conn.setSources([]connector.CatalogSource{src})
	e.clock.advance(time.Hour)
	e.svc.RunDue(e.ctx)
	if s := e.get(sub.ID); s.State != StateOK {
		t.Fatalf("a compatible change: %+v", s)
	}
	// Now it needs an argument the recipe does not give.
	src.Tools[0].InputSchema = json.RawMessage(`{"type":"object","properties":{"folder":{"type":"string"}},"required":["folder"]}`)
	e.conn.setSources([]connector.CatalogSource{src})
	calls := e.conn.callCount()
	e.clock.advance(time.Hour)
	e.svc.RunDue(e.ctx)
	s := e.get(sub.ID)
	if s.State != StateNeedsRecipe || s.Error.Code != CodeToolChanged || e.conn.callCount() != calls {
		t.Fatalf("an incompatible change: %+v", s)
	}
	// A new recipe that fits brings it back.
	r := itemsRecipe()
	r.Args = json.RawMessage(`{"folder":"inbox"}`)
	s, err := e.svc.Update(e.ctx, sub.ID, Patch{Revision: s.Revision, Recipe: &r}, Actor{})
	if err != nil || s.State != StateOK || s.NextRunAt == nil {
		t.Fatalf("a new recipe: %+v %v", s, err)
	}
}

func TestTwoRunsWithoutTitlesNeedANewRecipe(t *testing.T) {
	e := newEnv(t)
	sub := e.turnOn(e.create(itemsRecipe()))
	e.conn.setAnswer(func(connector.CatalogQuery) (string, error) { return `{"items":[{"id":"x","subject":"moved"}]}`, nil })
	e.svc.RunDue(e.ctx)
	if s := e.get(sub.ID); s.State != StateFailing {
		t.Fatalf("one run without titles: %+v", s)
	}
	e.clock.advance(time.Hour)
	e.svc.RunDue(e.ctx)
	if s := e.get(sub.ID); s.State != StateNeedsRecipe || s.Error.Code != CodeShapeChanged {
		t.Fatalf("two runs without titles: %+v", s)
	}
}

func TestRunsKeepTheNewestTwenty(t *testing.T) {
	e := newEnv(t)
	sub := e.turnOn(e.create(itemsRecipe()))
	for range 25 {
		e.svc.RunNow(e.ctx, sub.ID, Actor{})
		e.svc.RunDue(e.ctx)
		e.clock.advance(time.Minute)
	}
	_, runs, _ := e.svc.Get(e.ctx, sub.ID)
	if len(runs) != RunsKept || !runs[0].At.After(runs[1].At) || runs[0].Trigger != TriggerNow {
		t.Fatalf("runs: %d, first %+v", len(runs), runs[0])
	}
}

func TestSummaryUsesTheCachedCatalogOnly(t *testing.T) {
	e := newEnv(t)
	lines, _ := e.svc.Summary(e.ctx)
	if len(lines) != 1 || lines[0].Subscribable != nil || lines[0].Extension != "examplechat" {
		t.Fatalf("before the catalog was read: %+v", lines)
	}
	e.turnOn(e.create(itemsRecipe())) // reads the catalog
	e.create(itemsRecipe())
	lines, _ = e.svc.Summary(e.ctx)
	if lines[0].Subscribable == nil || !*lines[0].Subscribable || lines[0].On != 1 || lines[0].Failing != 0 {
		t.Fatalf("summary: %+v", lines[0])
	}
	c, err := e.svc.Catalog(e.ctx, "example")
	if err != nil || len(c.Sources) != 1 || len(c.Sources[0].Subscriptions) != 2 {
		t.Fatalf("catalog: %+v %v", c, err)
	}
	if string(c.Sources[0].Tools[0].InputSchema) != string(noticesSchema) {
		t.Fatalf("a tool's schema: %s", c.Sources[0].Tools[0].InputSchema)
	}
	// The cached catalogs: what was read, without a call; a connector never read is left out.
	e.conns.mu.Lock()
	e.conns.infos["other"] = &ConnectorInfo{ID: "other", Status: connector.StateOK, Enabled: true,
		Conn: &fakeConn{id: "other", answer: func(connector.CatalogQuery) (string, error) { return "{}", nil }}}
	e.conns.mu.Unlock()
	list, err := e.svc.Catalogs(e.ctx)
	if err != nil || len(list) != 1 || list[0].Connector != "example" || list[0].Extension != "examplechat" ||
		list[0].Status != connector.StateOK || len(list[0].Sources[0].Subscriptions) != 2 {
		t.Fatalf("cached catalogs: %+v %v", list, err)
	}
	for _, tool := range c.Sources[0].Tools {
		if tool.Name == "send_message" && (tool.Subscribable || tool.Refused.Code != CodeWriteVerb) {
			t.Fatalf("send_message: %+v", tool)
		}
	}
}

func TestDeleteRemovesSeenKeysAndItems(t *testing.T) {
	e := newEnv(t)
	sub := e.turnOn(e.create(itemsRecipe()))
	e.svc.RunDue(e.ctx)
	if err := e.svc.Delete(e.ctx, sub.ID, true, Actor{ID: "device:phone"}); err != nil {
		t.Fatal(err)
	}
	if len(e.store.Items()) != 0 || e.store.SeenCount(sub.ID) != 0 {
		t.Fatal("deleting kept its items or keys")
	}
	if _, _, err := e.svc.Get(e.ctx, sub.ID); codeOf(err) != CodeNotFound {
		t.Fatalf("get after delete: %v", err)
	}
}
