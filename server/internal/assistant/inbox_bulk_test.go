package assistant

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
)

func bulkBody(done bool, targets ...inboxBulkTarget) string {
	var parts []string
	for _, t := range targets {
		parts = append(parts, fmt.Sprintf(`{"id":%q,"version":%q}`, t.ID, t.Version))
	}
	return fmt.Sprintf(`{"done":%t,"items":[%s]}`, done, strings.Join(parts, ","))
}

func listedVersion(h *inboxHub, id string) string {
	h.inbox.mu.Lock()
	defer h.inbox.mu.Unlock()
	return h.inbox.items[id].listed().Version
}

func bulkIDs(list []any) []string {
	var out []string
	for _, v := range list {
		switch x := v.(type) {
		case string:
			out = append(out, x)
		case map[string]any:
			out = append(out, fmt.Sprint(x["id"], ":", x["reason"]))
		}
	}
	return out
}

// Filing what the list shows: only the items named, each as it was listed; a repeat changes nothing; "Undo" brings
// back what was filed.
func TestInboxBulkDone(t *testing.T) {
	h := muteHub(t)
	mux := h.routes()
	for _, id := range []string{"a", "b", "c", "d"} {
		h.inbox.items[id] = mailItem(id, "shop@example.com", "示例商城")
		h.inbox.items[id].Last = "1"
	}
	earlier := time.Now().Add(-time.Hour)
	h.inbox.items["d"].done(earlier, "read")

	_, out := inboxCall(t, mux, "GET", "/api/v1/inbox", "")
	if !strings.Contains(fmt.Sprint(out["items"]), "version:"+listedVersion(h, "a")) {
		t.Fatalf("listed items carry no version: %v", out["items"])
	}
	stale := listedVersion(h, "b")
	h.inbox.items["b"].Last = "2" // a new mail came into b after the list was drawn

	body := bulkBody(true, inboxBulkTarget{"a", listedVersion(h, "a")}, inboxBulkTarget{"b", stale},
		inboxBulkTarget{"d", listedVersion(h, "d")}, inboxBulkTarget{"x", "v"}, inboxBulkTarget{"a", ""})
	code, out := inboxCall(t, mux, "POST", "/api/v1/inbox/done", body)
	if code != 200 {
		t.Fatalf("bulk done: %d %v", code, out)
	}
	changed, _ := out["changed"].([]any)
	if len(changed) != 1 || changed[0].(map[string]any)["id"] != "a" || changed[0].(map[string]any)["done_by"] != "user" {
		t.Fatalf("changed = %v", out["changed"])
	}
	if got := fmt.Sprint(bulkIDs(out["unchanged"].([]any))); got != "[d]" {
		t.Fatalf("unchanged = %s", got)
	}
	if got := fmt.Sprint(bulkIDs(out["skipped"].([]any))); got != "[b:changed x:missing]" {
		t.Fatalf("skipped = %s", got)
	}
	if h.inbox.items["b"].DoneAt != nil || h.inbox.items["c"].DoneAt != nil {
		t.Fatal("an item outside the request, or with new mail, was filed")
	}
	if d := h.inbox.items["d"]; !d.DoneAt.Equal(earlier) || d.DoneBy != "read" {
		t.Fatalf("an item already done was filed again: %v %s", d.DoneAt, d.DoneBy)
	}

	// The same request again (a retry, another device): nothing moves, a keeps when it was filed.
	at := *h.inbox.items["a"].DoneAt
	_, out = inboxCall(t, mux, "POST", "/api/v1/inbox/done", body)
	if len(out["changed"].([]any)) != 0 || fmt.Sprint(bulkIDs(out["unchanged"].([]any))) != "[a d]" || !h.inbox.items["a"].DoneAt.Equal(at) {
		t.Fatalf("a repeat changed something: %v", out)
	}

	// "Undo": what was filed comes back.
	_, out = inboxCall(t, mux, "POST", "/api/v1/inbox/done", bulkBody(false, inboxBulkTarget{"a", ""}))
	if len(out["changed"].([]any)) != 1 || !h.inbox.items["a"].open() || h.inbox.items["a"].DoneBy != "" {
		t.Fatalf("undo: %v", out)
	}
}

func TestInboxBulkDoneBadRequests(t *testing.T) {
	h := muteHub(t)
	mux := h.routes()
	if code, _ := inboxCall(t, mux, "POST", "/api/v1/inbox/done", `{"items":[{"id":"a"}]}`); code != 400 {
		t.Fatalf("no done: %d", code)
	}
	var many []inboxBulkTarget
	for i := 0; i <= inboxBulkMax; i++ {
		many = append(many, inboxBulkTarget{ID: fmt.Sprint(i)})
	}
	if code, _ := inboxCall(t, mux, "POST", "/api/v1/inbox/done", bulkBody(true, many...)); code != 413 {
		t.Fatalf("too many: %d", code)
	}
	if code, out := inboxCall(t, mux, "POST", "/api/v1/inbox/done", `{"done":true,"items":[]}`); code != 200 || len(out["changed"].([]any)) != 0 {
		t.Fatalf("nothing to do: %d %v", code, out)
	}
}

// Concurrent bulk requests over the same items: each item is filed once, by exactly one of them.
func TestInboxBulkDoneConcurrent(t *testing.T) {
	h := muteHub(t)
	var targets []inboxBulkTarget
	for i := 0; i < 50; i++ {
		id := fmt.Sprint("m", i)
		h.inbox.items[id] = mailItem(id, "shop@example.com", "示例商城")
		targets = append(targets, inboxBulkTarget{id, listedVersion(h, id)})
	}
	var wg sync.WaitGroup
	var mu sync.Mutex
	filed := map[string]int{}
	for g := 0; g < 8; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			res := h.setInboxDoneBulk(context.Background(), targets, true, time.Now())
			mu.Lock()
			defer mu.Unlock()
			for _, it := range res.Changed {
				filed[it.ID]++
			}
		}()
	}
	wg.Wait()
	if len(filed) != 50 {
		t.Fatalf("filed %d of 50", len(filed))
	}
	for id, n := range filed {
		if n != 1 {
			t.Fatalf("%s filed %d times", id, n)
		}
	}
}

// POST /api/v1/inbox/bulk with a filter files what the list as of as_of showed under it: an item that arrived after
// (a new one, or new mail into a listed one) stays open; "Ignore" is done_by=dismissed; the undo it answers brings back
// exactly what it filed.
func TestInboxBulkFilterAsOf(t *testing.T) {
	h := muteHub(t)
	mux := h.routes()
	for _, id := range []string{"a", "b", "c"} {
		h.inbox.items[id] = mailItem(id, "shop@example.com", "示例商城")
		h.inbox.items[id].Last, h.inbox.items[id].Tier = "1", TierFYI
	}
	h.inbox.items["c"].Tier = TierAction // another section
	h.inbox.items["l"] = &InboxItem{ID: "l", Source: "lark", Kind: "dm", Account: "feishu", Last: "9", Tier: TierFYI, UpdatedAt: time.Now()}

	_, list := inboxCall(t, mux, "GET", "/api/v1/inbox", "")
	asOf, _ := list["as_of"].(string)
	if asOf == "" {
		t.Fatalf("the list says no as_of: %v", list)
	}
	time.Sleep(2 * time.Millisecond)
	h.inbox.mu.Lock()
	h.inbox.items["b"].Last = "2" // new mail into b after the list
	h.inbox.items["n"] = mailItem("n", "new@example.com", "新的")
	h.inbox.items["n"].Last, h.inbox.items["n"].Tier = "1", TierFYI // a new item, not listed yet
	h.inbox.mu.Unlock()
	before := len(h.in.changes())

	body := fmt.Sprintf(`{"action":"dismiss","filter":{"source":"mail","tier":"fyi"},"as_of":%q}`, asOf)
	code, out := inboxCall(t, mux, "POST", "/api/v1/inbox/bulk", body)
	if code != 200 || out["action"] != "dismiss" {
		t.Fatalf("bulk dismiss: %d %v", code, out)
	}
	if got := fmt.Sprint(out["ids"]); got != "[a]" {
		t.Fatalf("filed %s, want only a", got)
	}
	if got := fmt.Sprint(bulkIDs(out["skipped"].([]any))); got != "[b:changed n:changed]" {
		t.Fatalf("skipped = %s", got)
	}
	if a := h.inbox.items["a"]; a.open() || a.DoneBy != "dismissed" {
		t.Fatalf("a: %v %s", a.DoneAt, a.DoneBy)
	}
	for _, id := range []string{"b", "c", "l", "n"} {
		if !h.inbox.items[id].open() {
			t.Fatalf("%s was filed", id)
		}
	}
	if !h.st.saved("a") || len(h.in.changes()) != before+1 {
		t.Fatalf("filed but not saved and announced once: saved %v, %d announcements", h.st.ids, len(h.in.changes())-before)
	}

	undo, _ := out["undo"].(map[string]any)
	raw, _ := json.Marshal(undo)
	if code, out := inboxCall(t, mux, "POST", "/api/v1/inbox/bulk", string(raw)); code != 200 || fmt.Sprint(out["ids"]) != "[a]" || out["undo"] != nil {
		t.Fatalf("undo %s: %d %v", raw, code, out)
	}
	if a := h.inbox.items["a"]; !a.open() || a.DoneBy != "" {
		t.Fatalf("a after the undo: %v %s", a.DoneAt, a.DoneBy)
	}

	// The same list again, everything "Done": b and n still did not arrive by then.
	body = fmt.Sprintf(`{"action":"done","filter":{},"as_of":%q}`, asOf)
	_, out = inboxCall(t, mux, "POST", "/api/v1/inbox/bulk", body)
	if got := fmt.Sprint(out["ids"]); got != "[a c l]" || h.inbox.items["a"].DoneBy != "user" {
		t.Fatalf("all done: %s", got)
	}
}

// ids file what they name (no version check), dismissed items list as such, and the single item's "Ignore" matches.
func TestInboxBulkIDsAndSingleDismiss(t *testing.T) {
	h := muteHub(t)
	mux := h.routes()
	for _, id := range []string{"a", "b"} {
		h.inbox.items[id] = mailItem(id, "shop@example.com", "示例商城")
	}
	if code, out := inboxCall(t, mux, "POST", "/api/v1/inbox/bulk", `{"action":"dismiss","ids":["a","x"]}`); code != 200 ||
		fmt.Sprint(out["ids"]) != "[a]" || fmt.Sprint(bulkIDs(out["skipped"].([]any))) != "[x:missing]" {
		t.Fatalf("ids: %d %v", code, out)
	}
	if code, out := inboxCall(t, mux, "POST", "/api/v1/inbox/b/done", `{"done":true,"dismiss":true}`); code != 200 || out["done_by"] != "dismissed" {
		t.Fatalf("one item dismissed: %d %v", code, out)
	}
	if code, out := inboxCall(t, mux, "POST", "/api/v1/inbox/bulk", `{"action":"done","ids":["a","b"]}`); code != 200 ||
		fmt.Sprint(out["unchanged"]) != "[a b]" || h.inbox.items["a"].DoneBy != "dismissed" {
		t.Fatalf("filing dismissed items again: %v", out)
	}
}

// Clients released before #5114 still send Mark all as done (action=done) and Dismiss all (action=dismiss) with the listed
// items and versions; newer ones send only Clean up all (dismiss). Both keep working, each filed as it says, and each
// undo brings back what it filed.
func TestInboxBulkOldClientsDoneAndDismiss(t *testing.T) {
	h := muteHub(t)
	mux := h.routes()
	for _, id := range []string{"a", "b", "c", "d"} {
		h.inbox.items[id] = mailItem(id, "shop@example.com", "示例商城")
		h.inbox.items[id].Last = "1"
	}
	items := func(ids ...string) string {
		var parts []string
		for _, id := range ids {
			parts = append(parts, fmt.Sprintf(`{"id":%q,"version":%q}`, id, inboxVersion(h.inbox.items[id])))
		}
		return "[" + strings.Join(parts, ",") + "]"
	}
	for _, c := range []struct {
		action, by string
		ids        []string
	}{{"done", "user", []string{"a", "b"}}, {"dismiss", "dismissed", []string{"c", "d"}}} {
		code, out := inboxCall(t, mux, "POST", "/api/v1/inbox/bulk", fmt.Sprintf(`{"action":%q,"items":%s}`, c.action, items(c.ids...)))
		if code != 200 || out["action"] != c.action || fmt.Sprint(out["ids"]) != fmt.Sprint(c.ids) {
			t.Fatalf("%s: %d %v", c.action, code, out)
		}
		for _, id := range c.ids {
			if it := h.inbox.items[id]; it.open() || it.DoneBy != c.by {
				t.Fatalf("%s: %s is %v %q, want done_by=%s", c.action, id, it.DoneAt, it.DoneBy, c.by)
			}
		}
		raw, _ := json.Marshal(out["undo"])
		if code, out := inboxCall(t, mux, "POST", "/api/v1/inbox/bulk", string(raw)); code != 200 || fmt.Sprint(out["ids"]) != fmt.Sprint(c.ids) {
			t.Fatalf("%s undo %s: %d %v", c.action, raw, code, out)
		}
		for _, id := range c.ids {
			if !h.inbox.items[id].open() {
				t.Fatalf("%s: %s stayed filed after the undo", c.action, id)
			}
		}
	}
}

func TestInboxBulkBadRequests(t *testing.T) {
	h := muteHub(t)
	mux := h.routes()
	for _, body := range []string{
		`{"ids":["a"]}`, // no action
		`{"action":"archive","ids":["a"]}`,
		`{"action":"done"}`, // neither ids nor a filter
		`{"action":"done","ids":["a"],"filter":{}}`,
		`{"action":"done","filter":{}}`,                                  // no as_of
		`{"action":"reopen","filter":{},"as_of":"2026-11-05T10:00:00Z"}`, // reopen takes ids
		`{"action":"done","filter":{"tier":"urgent"},"as_of":"2026-11-05T10:00:00Z"}`,
		`not json`,
	} {
		if code, _ := inboxCall(t, mux, "POST", "/api/v1/inbox/bulk", body); code != 400 {
			t.Errorf("%s: %d, want 400", body, code)
		}
	}
	ids := make([]string, inboxBulkMax+1)
	for i := range ids {
		ids[i] = fmt.Sprint(i)
	}
	raw, _ := json.Marshal(map[string]any{"action": "done", "ids": ids})
	if code, _ := inboxCall(t, mux, "POST", "/api/v1/inbox/bulk", string(raw)); code != 413 {
		t.Fatalf("too many: %d", code)
	}
}

// inboxPutFailStore fails writes for all ids or a chosen set. The embedded inboxBatchMemStore always loses the
// batch (batchFail), so production takes its batch-fails -> one-by-one path and the failures surface per id.
type inboxPutFailStore struct {
	*inboxBatchMemStore
	failAll  bool
	fail     map[string]bool
	failOnce map[string]bool // the next single write of the id fails once
	failHook func(id string) // runs inside the failing write (b.saving held, b.mu free)
}

func (s *inboxPutFailStore) PutInboxItem(ctx context.Context, id string, data []byte, updatedAt time.Time, doneAt *time.Time) error {
	fail := s.failAll || s.fail[id]
	if s.failOnce[id] {
		delete(s.failOnce, id)
		fail = true
	}
	if fail {
		if s.failHook != nil {
			s.failHook(id)
		}
		return errMuteStoreDown
	}
	return s.inboxBatchMemStore.PutInboxItem(ctx, id, data, updatedAt, doneAt)
}

func (s *inboxPutFailStore) hasRow(id string) bool {
	s.imu.Lock()
	defer s.imu.Unlock()
	_, ok := s.rows[id]
	return ok
}

func (s *inboxPutFailStore) doneAt(id string) *time.Time {
	s.imu.Lock()
	defer s.imu.Unlock()
	return s.done[id]
}

func newBulkFailHub(t *testing.T) (*inboxHub, *inboxPutFailStore) {
	t.Helper()
	f := newFakeHub().withMachine()
	in := newFakeInbox()
	f.inbox = in
	base := &inboxBatchMemStore{inboxMemStore: newInboxMemStore(), batchFail: errMuteStoreDown}
	st := &inboxPutFailStore{inboxBatchMemStore: base}
	h := New(Options{Hub: f, Store: func() Store { return st }, Logf: testLogf(t)})
	if err := h.inboxReady(context.Background()); err != nil {
		t.Fatal(err)
	}
	waitNeedsYouIdle(t, h)
	return &inboxHub{Service: h, f: f, in: in, st: st.inboxMemStore}, st
}

func seedFailItems(h *inboxHub, ids ...string) {
	for _, id := range ids {
		h.inbox.items[id] = mailItem(id, "shop@example.com", "Shop")
	}
}

func jsonIDs(v any) []string {
	raw, _ := v.([]any)
	out := make([]string, 0, len(raw))
	for _, x := range raw {
		out = append(out, x.(string))
	}
	return out
}

// When every bulk write fails the answer is still 200: failed lists every id, the items stay open and unannounced,
// and the same request files all of them after recovery.
func TestInboxBulkAllWritesFail(t *testing.T) {
	h, st := newBulkFailHub(t)
	mux := h.routes()
	seedFailItems(h, "a", "b", "c")
	before := len(h.in.changes())
	st.failAll = true
	code, out := inboxCall(t, mux, "POST", "/api/v1/inbox/bulk", `{"action":"done","ids":["a","b","c"]}`)
	if code != 200 {
		t.Fatalf("bulk done: %d %v", code, out)
	}
	if got := jsonIDs(out["failed"]); fmt.Sprint(got) != "[a b c]" {
		t.Fatalf("failed = %v", got)
	}
	if len(out["changed"].([]any)) != 0 || len(out["ids"].([]any)) != 0 || out["undo"] != nil {
		t.Fatalf("failed items were still changed/undoable: %v", out)
	}
	for _, id := range []string{"a", "b", "c"} {
		if !h.inbox.items[id].open() || st.hasRow(id) {
			t.Fatalf("item %s was filed or stored though its write failed", id)
		}
	}
	if evs := h.in.changes()[before:]; len(evs) != 1 || len(evs[0].Items) != 0 {
		t.Fatalf("the apps were told about failed items: %+v", evs)
	}

	st.failAll = false
	_, out = inboxCall(t, mux, "POST", "/api/v1/inbox/bulk", `{"action":"done","ids":["a","b","c"]}`)
	if got := jsonIDs(out["failed"]); len(got) != 0 {
		t.Fatalf("the retry failed again: %v", got)
	}
	if got := jsonIDs(out["ids"]); fmt.Sprint(got) != "[a b c]" || out["undo"] == nil {
		t.Fatalf("the retry did not file everything: %v", out)
	}
	for _, id := range []string{"a", "b", "c"} {
		if h.inbox.items[id].open() {
			t.Fatalf("item %s stayed open after the retry", id)
		}
	}
}

// A partial failure files only what was written: the failed id stays open and out of changed/ids/undo, the others
// are stored and announced.
func TestInboxBulkPartialWriteFailure(t *testing.T) {
	h, st := newBulkFailHub(t)
	mux := h.routes()
	seedFailItems(h, "a", "b", "c")
	before := len(h.in.changes())
	st.fail = map[string]bool{"b": true}
	_, out := inboxCall(t, mux, "POST", "/api/v1/inbox/bulk", `{"action":"done","ids":["a","b","c"]}`)
	if got := jsonIDs(out["failed"]); fmt.Sprint(got) != "[b]" {
		t.Fatalf("failed = %v", got)
	}
	if got := jsonIDs(out["ids"]); fmt.Sprint(got) != "[a c]" {
		t.Fatalf("ids = %v, want [a c]", got)
	}
	if undo := out["undo"].(map[string]any); fmt.Sprint(jsonIDs(undo["ids"])) != "[a c]" {
		t.Fatalf("undo = %v, want [a c]", undo)
	}
	if !h.inbox.items["b"].open() || h.inbox.items["a"].open() || h.inbox.items["c"].open() {
		t.Fatalf("memory after partial failure: a open=%v b open=%v c open=%v",
			h.inbox.items["a"].open(), h.inbox.items["b"].open(), h.inbox.items["c"].open())
	}
	if st.hasRow("b") || st.doneAt("a") == nil || st.doneAt("c") == nil {
		t.Fatalf("stored state: b present=%v, a done=%v, c done=%v", st.hasRow("b"), st.doneAt("a"), st.doneAt("c"))
	}
	told := h.in.changes()[before].Items
	if len(told) != 2 {
		t.Fatalf("the apps were told about %d items, want 2: %+v", len(told), told)
	}
}

// One dismiss request spans every bucket and the failed id comes first among the changed items: an open id with a
// stale version is skipped, a missing id is skipped, a filed id is unchanged, one open id is kept and one whose
// write fails is restored open with no dismissed marker. Restoring by the wrong parallel index would file f and
// restore g; the batch is attempted before the one-by-one fallback.
func TestInboxBulkDismissMixedWriteFailure(t *testing.T) {
	h, st := newBulkFailHub(t)
	mux := h.routes()
	seedFailItems(h, "v", "u", "f", "g")
	if _, err := h.setInboxDone(context.Background(), "u", true, false); err != nil {
		t.Fatal(err)
	}
	before := len(h.in.changes())
	st.fail = map[string]bool{"f": true}
	body := `{"action":"dismiss","items":[{"id":"v","version":"stale"}],"ids":["x","u","f","g"]}`
	_, out := inboxCall(t, mux, "POST", "/api/v1/inbox/bulk", body)
	if got := jsonIDs(out["failed"]); fmt.Sprint(got) != "[f]" ||
		fmt.Sprint(jsonIDs(out["ids"])) != "[g]" || fmt.Sprint(out["unchanged"]) != "[u]" ||
		fmt.Sprint(bulkIDs(out["skipped"].([]any))) != "[v:changed x:missing]" {
		t.Fatalf("mixed dismiss buckets: %v", out)
	}
	if f, g := h.inbox.items["f"], h.inbox.items["g"]; !f.open() || f.DoneBy != "" || g.open() || g.DoneBy != "dismissed" {
		t.Fatalf("memory: f open=%v by=%q, g open=%v by=%q", f.open(), f.DoneBy, g.open(), g.DoneBy)
	}
	if st.doneAt("f") != nil || st.doneAt("g") == nil {
		t.Fatalf("stored: f done=%v, g done=%v", st.doneAt("f"), st.doneAt("g"))
	}
	st.imu.Lock()
	lastBatch := fmt.Sprint(st.batches[len(st.batches)-1])
	st.imu.Unlock()
	if lastBatch != "[f g]" {
		t.Fatalf("the batch path was not taken with the changed ids: %s", lastBatch)
	}
	if told := h.in.changes()[before].Items; len(told) != 1 || told[0].ID != "g" {
		t.Fatalf("the apps were told %+v, want only g", told)
	}
}

// A failed bring-back restores the filed state in memory too, and retries after recovery.
func TestInboxBulkReopenWriteFailure(t *testing.T) {
	h, st := newBulkFailHub(t)
	mux := h.routes()
	seedFailItems(h, "d")
	if _, err := h.setInboxDone(context.Background(), "d", true, false); err != nil {
		t.Fatal(err)
	}
	st.failAll = true
	_, out := inboxCall(t, mux, "POST", "/api/v1/inbox/bulk", `{"action":"reopen","ids":["d"]}`)
	if got := jsonIDs(out["failed"]); fmt.Sprint(got) != "[d]" || len(out["changed"].([]any)) != 0 ||
		h.inbox.items["d"].open() {
		t.Fatalf("reopen failure: %v open=%v", out, h.inbox.items["d"].open())
	}
	st.failAll = false
	_, out = inboxCall(t, mux, "POST", "/api/v1/inbox/bulk", `{"action":"reopen","ids":["d"]}`)
	if len(jsonIDs(out["failed"])) != 0 || len(out["changed"].([]any)) != 1 || !h.inbox.items["d"].open() {
		t.Fatalf("reopen retry: %v open=%v", out, h.inbox.items["d"].open())
	}
}

// One item's "Done" that cannot be stored answers 500 and leaves the item open; after recovery it answers 200 and
// the done state is stored. Bringing it back is symmetric: a failed reopen answers 500 and leaves it filed.
func TestSetInboxDoneWriteFailure(t *testing.T) {
	h, st := newBulkFailHub(t)
	mux := h.routes()
	seedFailItems(h, "s")
	st.failAll = true
	if code, _ := inboxCall(t, mux, "POST", "/api/v1/inbox/s/done", `{"done":true}`); code != http.StatusInternalServerError {
		t.Fatalf("done with the store down: %d, want 500", code)
	}
	if !h.inbox.items["s"].open() || st.hasRow("s") {
		t.Fatal("a failed done filed the item in memory or wrote a row")
	}
	st.failAll = false
	if code, _ := inboxCall(t, mux, "POST", "/api/v1/inbox/s/done", `{"done":true}`); code != 200 || h.inbox.items["s"].open() {
		t.Fatalf("retry: code=%d open=%v", code, h.inbox.items["s"].open())
	}
	if st.doneAt("s") == nil {
		t.Fatal("the retry did not store the done state")
	}
	st.failAll = true
	if code, _ := inboxCall(t, mux, "POST", "/api/v1/inbox/s/done", `{"done":false}`); code != http.StatusInternalServerError {
		t.Fatalf("reopen with the store down: %d, want 500", code)
	}
	if h.inbox.items["s"].open() {
		t.Fatal("a failed reopen brought the item back in memory")
	}
	st.failAll = false
	if code, _ := inboxCall(t, mux, "POST", "/api/v1/inbox/s/done", `{"done":false}`); code != 200 || !h.inbox.items["s"].open() {
		t.Fatalf("reopen retry: code=%d open=%v", code, h.inbox.items["s"].open())
	}
	if st.doneAt("s") != nil {
		t.Fatal("the reopen retry left the row done")
	}
}

// Rows stored before arrivals were stamped count as arrived when they were last updated.
func TestInboxArrivedOfLegacyRows(t *testing.T) {
	it := &InboxItem{ID: "a", Last: "1"}
	if !it.arrivedAfter(time.Now()) {
		t.Fatal("an unstamped item counts as arrived")
	}
	now := time.Now()
	it.arrived(now)
	if it.arrivedAfter(now) || !it.arrivedAfter(now.Add(-time.Second)) {
		t.Fatal("stamped arrival")
	}
	it.arrived(now.Add(time.Hour)) // the same Last: kept
	if !it.ArrivedAt.Equal(now) {
		t.Fatal("re-stamped without anything new")
	}
	if l := it.listed(); l.ArrivedOf != "" || !l.ArrivedAt.IsZero() {
		t.Fatal("bookkeeping listed")
	}
}
