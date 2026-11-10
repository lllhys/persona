package assistant

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"runtime"
	"runtime/debug"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/apierr"
	"github.com/nwverse/persona/server/internal/connector"
)

// memoItems are items of every shape the list writes, with the characters encoding/json escapes or replaces.
func memoItems(now time.Time) []*InboxItem {
	done, old := now.Add(-time.Hour), now.Add(-inboxKeepDone-time.Hour)
	drafted := now.Add(-2 * time.Minute)
	return []*InboxItem{
		{ID: "lark-1", Source: "lark", Kind: KindDM, Title: "<b>Tom</b> & Jerry", Preview: "line\u2028next\u2029end", Last: "m1",
			Lark: &InboxLark{FeedID: "oc_1", AccountID: 2, OLAccount: 2, IsGroup: true, SenderID: "ou_1"}, Sender: "Tom",
			Agent:   &InboxAgent{State: AgentDrafted, Draft: "ok <sure>", DraftedAt: &drafted, Pending: []inboxNote{{Name: "x"}}, Wake: true},
			Context: "not listed", Rules: "not listed", Meta: json.RawMessage(`{"a":1}`)},
		{ID: "mail-1", Source: "mail", Kind: KindMail, Title: "Invoice \xff bad", Summary: "a\x00b", Last: "uid9",
			Mail:         &InboxMail{AccountID: "acc", UIDs: []uint32{7, 9}, From: MailAddress{Name: "Ann", Email: "Ann@Example.com"}},
			Translations: map[string]*InboxTranslation{"en": {Subject: "S", Preview: "P", Text: "body", Of: "uid9"}, "ja": {Subject: "old", Of: "uid1"}}},
		{ID: "gh-1", Source: "github", Kind: "pr", Title: "Fix", Last: "n1", Tier: TierFYI,
			Connector: &InboxConnector{Thread: "t", Tags: []connector.Tag{{Key: connector.TagRepo, Value: "r", Name: "repo"}}},
			Chain:     []InboxChainEntry{{Kind: ChainConcluded, Text: "done"}}, Handoffs: []InboxHandoff{{SessionID: "s", Op: "op"}}},
		{ID: "done-1", Source: "mail", Kind: KindMail, Title: "filed", Last: "u1", DoneAt: &done, DoneBy: "user"},
		{ID: "gone-1", Source: "mail", Kind: KindMail, Title: "filed long ago", Last: "u2", DoneAt: &old, DoneBy: "user"},
	}
}

// memoInbox is an inbox holding items, each a minute older than the one before.
func memoInbox(t *testing.T, items []*InboxItem) *inboxHub {
	t.Helper()
	h := newInboxHub(t)
	if err := h.inboxReady(context.Background()); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	h.inbox.mu.Lock()
	for i, it := range items {
		it.UpdatedAt = now.Add(-time.Duration(i) * time.Minute)
		h.inbox.items[it.ID] = it
	}
	h.inbox.mu.Unlock()
	return h
}

// getInbox is GET /api/v1/inbox's body, and the body apierr.WriteJSON wrote of the same list at the same as_of.
func getInbox(t *testing.T, h *inboxHub, query string) (got, want []byte) {
	t.Helper()
	w := httptest.NewRecorder()
	h.Service.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/inbox"+query, nil))
	if w.Code != http.StatusOK || w.Header().Get("Content-Type") != "application/json; charset=utf-8" {
		t.Fatalf("GET: %d %q", w.Code, w.Header().Get("Content-Type"))
	}
	var head struct {
		AsOf time.Time `json:"as_of"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &head); err != nil {
		t.Fatal(err)
	}
	items, _ := h.inboxListAt(query == "?sources=all")
	ref := httptest.NewRecorder()
	apierr.WriteJSON(ref, map[string]any{"items": items, "as_of": head.AsOf, "triage": h.inboxTriage()})
	return w.Body.Bytes(), ref.Body.Bytes()
}

// The answer is the bytes apierr.WriteJSON wrote of the list, when the items are encoded and when they come from the
// memo, with and without the connectors' items, and for an empty list ([], not null).
func TestInboxListBytesAreWriteJSON(t *testing.T) {
	for _, tc := range []struct {
		name  string
		items []*InboxItem
		query string
		n     int
	}{
		{"all", memoItems(time.Now()), "?sources=all", 4},
		{"no connectors", memoItems(time.Now()), "", 3},
		{"empty", nil, "?sources=all", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := memoInbox(t, tc.items)
			for round := range 2 { // encoded, then kept
				got, want := getInbox(t, h, tc.query)
				if !bytes.Equal(got, want) {
					t.Fatalf("round %d:\n got %s\nwant %s", round, got, want)
				}
				var out struct{ Items []json.RawMessage }
				if err := json.Unmarshal(got, &out); err != nil || len(out.Items) != tc.n || tc.n == 0 && !bytes.Contains(got, []byte(`"items":[]`)) {
					t.Fatalf("round %d: %d items (%v): %s", round, len(out.Items), err, got)
				}
			}
		})
	}
}

// An unchanged list encodes no item again; a saved or stamped change encodes that item; an item replaced under its id
// or dropped is not listed from the bytes of the one before.
func TestInboxListEncodesOnlyChangedItems(t *testing.T) {
	h := memoInbox(t, memoItems(time.Now()))
	b := &h.inbox
	encoded := func() int {
		b.mu.Lock()
		defer b.mu.Unlock()
		n := b.listMemo.encoded
		b.listMemo.encoded = 0
		return n
	}
	get := func(want int) []byte {
		t.Helper()
		w := httptest.NewRecorder()
		h.Service.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/inbox?sources=all", nil))
		if n := encoded(); n != want {
			t.Fatalf("encoded %d items, want %d", n, want)
		}
		return w.Body.Bytes()
	}
	get(4)
	get(0)

	b.mu.Lock()
	it := b.items["lark-1"]
	it.Summary = "changed"
	b.mu.Unlock()
	h.saveInbox(context.Background(), []*InboxItem{it})
	if body := get(1); !bytes.Contains(body, []byte(`"summary":"changed"`)) {
		t.Fatalf("the saved change is not listed: %s", body)
	}

	// A change the rollback bookkeeping stamps is listed before its save is written, as without the memo.
	b.mu.Lock()
	it.Summary = "stamped"
	b.touched(it.ID)
	b.mu.Unlock()
	if body := get(1); !bytes.Contains(body, []byte(`"summary":"stamped"`)) {
		t.Fatalf("the stamped change is not listed: %s", body)
	}

	b.mu.Lock()
	b.items["gh-1"] = &InboxItem{ID: "gh-1", Source: "github", Kind: "pr", Title: "Replaced", Last: "n2", UpdatedAt: time.Now(),
		Connector: &InboxConnector{Thread: "t"}}
	b.drop("mail-1")
	b.mu.Unlock()
	body := get(1)
	if !bytes.Contains(body, []byte(`"title":"Replaced"`)) || bytes.Contains(body, []byte(`"id":"mail-1"`)) {
		t.Fatalf("a replaced or dropped item is listed from old bytes: %s", body)
	}
	get(0)
}

// discardWriter keeps no body, so an allocation count is the handler's own.
type discardWriter struct{ h http.Header }

func (w *discardWriter) Header() http.Header         { return w.h }
func (w *discardWriter) Write(p []byte) (int, error) { return len(p), nil }
func (w *discardWriter) WriteHeader(int)             {}

// A GET of 400 unchanged items allocated 1.86 MB when every item was copied and encoded again; from the kept bytes it
// allocates 43,816 bytes (the rows to sort and the answer's frame).
func TestInboxListUnchangedAllocations(t *testing.T) {
	if raceEnabled || !isolated(t) {
		return
	}
	items := make([]*InboxItem, 400)
	for i := range items {
		items[i] = memoItems(time.Now())[i%3]
		items[i].ID = fmt.Sprintf("%s-%03d", items[i].ID, i)
	}
	h := memoInbox(t, items)
	inboxMemoVerify = false
	r := httptest.NewRequest(http.MethodGet, "/api/v1/inbox?sources=all", nil)
	h.Service.ServeHTTP(&discardWriter{h: http.Header{}}, r)
	const runs = 50
	defer debug.SetGCPercent(debug.SetGCPercent(-1))
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	for range runs {
		h.Service.ServeHTTP(&discardWriter{h: http.Header{}}, r)
	}
	runtime.ReadMemStats(&after)
	if per := (after.TotalAlloc - before.TotalAlloc) / runs; per > 45130 {
		t.Fatalf("one GET of 400 unchanged items allocated %d bytes, want at most 45130", per)
	}
}
