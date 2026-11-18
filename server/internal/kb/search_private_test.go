package kb

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Bookkeeping files (a person's hashed identity hints, a connector's cursor, the Moments verdicts) sit in the data
// dir next to what is searched, but neither Search nor Find may return a line of them: they reach the model through
// kb_search and mean nothing to it.
func TestSearchAndFindSkipBookkeeping(t *testing.T) {
	e := testEngine(t, nil)
	e.HandleKey = []byte("0123456789abcdef0123456789abcdef")
	st := e.Store
	if err := st.PutPerson(&Person{ID: "mail-a", Name: "Alice"}); err != nil {
		t.Fatal(err)
	}
	if !e.addHandles("mail-a", []Handle{{Kind: HandleEmail, Value: handleTestEmail}}) {
		t.Fatal("addHandles")
	}
	if err := st.PutProject(&Project{Slug: "p1", Name: "Plan"}); err != nil {
		t.Fatal(err)
	}
	private := map[string]string{
		filepath.Join("cursors", "imessage.json"):               `{"cursor":"zqxcursortoken"}`,
		filepath.Join(KindProject, "p1", "moments-judged.json"): `{"zqxjudgedpost":true}`,
	}
	for rel, body := range private {
		p := filepath.Join(st.Dir, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if raw, err := os.ReadFile(st.Path(KindPerson, "mail-a", "handles.json")); err != nil || !strings.Contains(string(raw), "email:") {
		t.Fatalf("handles.json not written: %q %v", raw, err)
	}
	skipped := func(path string) bool {
		base := filepath.Base(path)
		return base == "handles.json" || base == "moments-judged.json" || strings.HasPrefix(filepath.ToSlash(path), "cursors/")
	}
	for _, q := range []string{"email:", "zqxcursortoken", "zqxjudgedpost"} {
		for _, h := range e.Search(q, true, 10) {
			if skipped(h.Path) {
				t.Errorf("Search(%q) returned %s: %q", q, h.Path, h.Line)
			}
		}
		for _, r := range e.Find(q, 10) {
			for _, h := range r.Hits {
				if skipped(h.Path) {
					t.Errorf("Find(%q) returned %s: %q", q, h.Path, h.Line)
				}
			}
		}
	}
	// What is not bookkeeping is still found: the person's own meta and the project.
	if hits := e.Search("Alice", false, 10); len(hits) == 0 {
		t.Error("Search no longer finds the person")
	}
	if res := e.Find("Plan", 10); len(res) == 0 {
		t.Error("Find no longer finds the project")
	}
}

// Live ingestion writes its bookkeeping into each chat's directory: the receipts (.receipts.json: the source, every
// message id and time, one long JSON line), the recovery intent while a write is in progress (.ingest.json) and the
// zone the days are in (messages/.zone). Profile files are searched before any day, so when they were searched a query
// made of digits, a date or the source's name filled the limit with the receipts of every chat and returned no
// message (#5991).
func TestSearchSkipsChatIngestBookkeeping(t *testing.T) {
	s, e := replayStore(t)
	if _, err := s.Rezone(goldenZone); err != nil {
		t.Fatal(err)
	}
	at := time.Now().Add(-2 * time.Hour)
	const chats, perChat, limit = 12, 60, 10
	for c := range chats {
		msgs := make([]SourceMessage, perChat)
		for i := range msgs {
			text := "plain words only: weather, lunch, roadmap"
			if i == 7 {
				text = "the budget is 4200 for the cafe"
			}
			msgs[i] = SourceMessage{ID: fmt.Sprintf("om_%d_%d", c, i), At: at.Add(time.Duration(i) * time.Second), SenderName: "Example", Text: text}
		}
		ingestMessages(t, e, "example", fmt.Sprintf("group%d", c), perChat, msgs...)
	}
	// A write cut short leaves its intent behind until the next one recovers it.
	pending := s.path(KindChat, "example-group0", ingestJournal)
	if err := os.WriteFile(pending, []byte(`{"version":1,"chat_id":"example-group0","receipts":{"source":"example","ids":{"om_0_42":"`+at.UTC().Format(time.RFC3339)+`"}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, rel := range []string{ingestReceipts, filepath.Join("messages", zoneFile)} {
		if _, err := os.Stat(s.path(KindChat, "example-group0", rel)); err != nil {
			t.Fatalf("the fixture has no %s: %v", rel, err)
		}
	}
	for rel, want := range map[string]bool{
		"chats/example-group0/" + ingestReceipts: false, "chats/example-group0/" + ingestJournal: false,
		"chats/example-group0/messages/" + zoneFile: false, "chats/example-group0/" + retentionFile: false,
		"chats/example-group0/meta.json": true, "chats/example-group0/summary.md": true,
		"chats/example-group0/messages/2026-11-12.md": true, "people/p1/" + ingestReceipts: true,
	} {
		if searchable(rel) != want {
			t.Errorf("searchable(%s) = %v, want %v", rel, !want, want)
		}
	}
	bookkeeping := func(path string) bool {
		base := filepath.Base(path)
		return base == ingestReceipts || base == ingestJournal || base == zoneFile
	}
	day := func(h Hit) bool {
		return strings.Contains(filepath.ToSlash(h.Path), "/messages/") && strings.HasSuffix(h.Path, ".md")
	}
	for _, q := range []string{"42", "4200", "cafe", at.In(goldenZone).Format("2006-01-02"), at.UTC().Format("2006"), "example", "om_0", "true", goldenZone.String()} {
		hits := e.Search(q, true, limit)
		for _, h := range hits {
			if bookkeeping(h.Path) {
				t.Errorf("Search(%q) returned %s: %q", q, h.Path, clip(h.Line, 80))
			}
		}
		for _, h := range e.SearchKind(q, KindChat, true, limit) {
			if bookkeeping(h.Path) {
				t.Errorf("SearchKind(%q, chats) returned %s: %q", q, h.Path, clip(h.Line, 80))
			}
		}
		for _, r := range e.Find(q, limit) {
			for _, h := range r.Hits {
				if bookkeeping(h.Path) {
					t.Errorf("Find(%q) returned %s: %q", q, h.Path, clip(h.Line, 80))
				}
			}
		}
		// Each chat has one message with these words, more chats than the limit: the hits are those messages (and
		// rarely a chat's meta.json, when its creation time has the digits). "42" is in every meta.json's times.
		if q == "4200" || q == "cafe" {
			n := 0
			for _, h := range hits {
				switch {
				case day(h) && strings.Contains(h.Line, "budget is 4200"):
					n++
				case filepath.Base(h.Path) != "meta.json":
					t.Errorf("Search(%q) returned %s: %q", q, h.Path, clip(h.Line, 80))
				}
			}
			if len(hits) != limit || n < limit/2 {
				t.Errorf("Search(%q) returned %d hits, %d of them messages: %+v", q, len(hits), n, hits)
			}
		}
	}
}
