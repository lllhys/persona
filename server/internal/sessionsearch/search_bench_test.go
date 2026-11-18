package sessionsearch

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/proto"
	"github.com/nwverse/persona/server/internal/sessionlist"
)

// benchItems is a fleet of n runtime rows whose full payload carries a long last answer, as real rows do.
func benchItems(b *testing.B, n int) []sessionlist.Item {
	b.Helper()
	at := time.Date(2026, 11, 14, 9, 0, 0, 0, time.UTC)
	items := make([]sessionlist.Item, n)
	for k := range items {
		s := proto.Session{SessionID: fmt.Sprintf("0e000000-0000-4000-8000-%012d", k), Agent: "claude", Status: proto.StatusIdle,
			Title: fmt.Sprintf("Fix the upload retry %d", k), Cwd: fmt.Sprintf("/work/project-%d/app", k%40), RepoRoot: fmt.Sprintf("/work/project-%d", k%40),
			Model: "claude-sonnet-4-5", LastText: strings.Repeat("The test waits for the write to land before it asserts. ", 30),
			Asks: strings.Repeat("make the upload test pass on slow disks ", 20), UpdatedAt: at.Add(time.Duration(k) * time.Second)}
		raw, err := json.Marshal(s)
		if err != nil {
			b.Fatal(err)
		}
		items[k] = sessionlist.Item{ID: s.SessionID, Machine: fmt.Sprintf("machine-%d", k%10), Project: s.RepoRoot, State: "idle",
			Source: "runtime", UpdatedAt: s.UpdatedAt, SearchText: s.Title, Full: json.RawMessage(raw), Summary: json.RawMessage(raw)}
	}
	return items
}

// BenchmarkSearch is one Palette search (sessions_search) over 2,000 indexed rows: broad matches every row (a
// page of 40 hits with evidence), nomatch none, and cold is the first search after the rows changed, which decodes
// every row's document. replace-during-cold is a cold search during which a session push (Replace) lands, and reports
// how long that push waited (replace-ns/op): the index's lock is shared with every push and list read.
func BenchmarkSearch(b *testing.B) {
	const n = 2000
	options := func(ctx *sessionlist.SearchContext, search string) sessionlist.Options {
		return sessionlist.Options{Search: search, SearchContext: ctx, Limit: 40}
	}
	warm := func(b *testing.B, search string) {
		var index sessionlist.Index
		index.Replace("batch", benchItems(b, n))
		o := options(&sessionlist.SearchContext{Document: Document, MachineNames: map[string]string{}}, search)
		if _, err := index.Query(o); err != nil {
			b.Fatal(err)
		}
		b.ReportAllocs()
		for b.Loop() {
			if _, err := index.Query(o); err != nil {
				b.Fatal(err)
			}
		}
	}
	b.Run("broad", func(b *testing.B) { warm(b, "upload slow") })
	b.Run("nomatch", func(b *testing.B) { warm(b, "kubernetes") })
	b.Run("cold", func(b *testing.B) {
		items := benchItems(b, n)
		o := options(&sessionlist.SearchContext{Document: Document, MachineNames: map[string]string{}}, "upload slow")
		b.ReportAllocs()
		for b.Loop() {
			b.StopTimer()
			var index sessionlist.Index
			index.Replace("batch", items)
			b.StartTimer()
			if _, err := index.Query(o); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("replace-during-cold", func(b *testing.B) {
		items := benchItems(b, n)
		other := benchItems(b, 1)
		other[0].ID, other[0].Source = "other", "other"
		var waited time.Duration
		for b.Loop() {
			b.StopTimer()
			var index sessionlist.Index
			index.Replace("batch", items)
			decoding := make(chan struct{})
			var once sync.Once
			o := options(&sessionlist.SearchContext{Document: func(item sessionlist.Item) (sessionlist.SearchDocument, error) {
				once.Do(func() { close(decoding) })
				return Document(item)
			}, MachineNames: map[string]string{}}, "upload slow")
			done := make(chan error, 1)
			b.StartTimer()
			go func() { _, err := index.Query(o); done <- err }()
			<-decoding
			start := time.Now()
			index.Replace("other", other)
			waited += time.Since(start)
			if err := <-done; err != nil {
				b.Fatal(err)
			}
		}
		b.ReportMetric(float64(waited.Nanoseconds())/float64(b.N), "replace-ns/op")
	})
}
