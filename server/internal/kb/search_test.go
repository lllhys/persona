package kb

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestSearchLimitsAndOrder(t *testing.T) {
	dir, extra := t.TempDir(), t.TempDir()
	write := func(root, rel, body string) {
		t.Helper()
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(dir, "docs/a.md", " ALPHA beta first \nalpha only\nbeta alpha second\n")
	write(dir, "docs/b.json", "beta ALPHA third\n")
	write(extra, "guide.md", "Beta alpha extra\n")
	write(dir, "chats/c/messages/2026-10-01.md", "alpha beta older message\n")
	write(dir, "chats/c/messages/2026-10-02.md", "alpha beta newer message\n")
	write(dir, "moments/posts/001.md", "alpha beta older post\n")
	write(dir, "moments/posts/002.md", "alpha beta newer post\n")
	e := &Engine{Store: &Store{Dir: dir}, ExtraDirs: []string{extra}}
	profiles := []Hit{
		{Path: "docs/a.md", Line: "ALPHA beta first"},
		{Path: "docs/a.md", Line: "beta alpha second"},
		{Path: "docs/b.json", Line: "beta ALPHA third"},
		{Path: filepath.Join(extra, "guide.md"), Line: "Beta alpha extra"},
	}
	posts := []Hit{
		{Path: "moments/posts/002.md", Line: "alpha beta newer post"},
		{Path: "moments/posts/001.md", Line: "alpha beta older post"},
	}
	all := append(append([]Hit{}, profiles...),
		Hit{Path: "chats/c/messages/2026-10-02.md", Line: "alpha beta newer message"},
		Hit{Path: "chats/c/messages/2026-10-01.md", Line: "alpha beta older message"})
	all = append(all, posts...)
	for _, tt := range []struct {
		name, query string
		messages    bool
		limit       int
		want        []Hit
	}{
		{name: "empty", limit: 20},
		{name: "whitespace", query: " \t\n", limit: 20},
		{name: "negative", query: "alpha beta", limit: -1},
		{name: "zero", query: "alpha beta"},
		{name: "one", query: "alpha beta", limit: 1, want: all[:1]},
		{name: "across_files", query: "alpha beta", limit: 3, want: all[:3]},
		{name: "extra_dir", query: "alpha beta", limit: 4, want: profiles},
		{name: "message_limit", query: "alpha beta", messages: true, limit: 5, want: all[:5]},
		{name: "all", query: " \tALpha BeTA\n", messages: true, limit: 20, want: all},
		{name: "without_messages", query: "alpha beta", limit: 20, want: append(append([]Hit{}, profiles...), posts...)},
		{name: "no_match", query: "alpha missing", messages: true, limit: 20},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := e.Search(tt.query, tt.messages, tt.limit); !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("Search(%q, %v, %d) = %+v, want %+v", tt.query, tt.messages, tt.limit, got, tt.want)
			}
		})
	}
}

// Search reads files through Find's cache: a file it kept must be read again once it changes, and a file that
// lost the words is no longer found.
func TestSearchSeesAChangedFile(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "chats", "c", "messages", "2026-10-01.md")
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	put := func(body string, at time.Time) {
		t.Helper()
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(p, at, at); err != nil { // settled: old enough for the cache to keep it
			t.Fatal(err)
		}
	}
	e := &Engine{Store: &Store{Dir: dir}}
	old := time.Now().Add(-time.Hour)
	put("first alpha\n", old)
	if hits := e.Search("alpha", true, 10); len(hits) != 1 || hits[0].Line != "first alpha" {
		t.Fatalf("first search: %+v", hits)
	}
	if e.find.texts[p] == nil {
		t.Fatal("the file was not kept")
	}
	put("second beta, longer\n", old.Add(time.Minute))
	if hits := e.Search("alpha", true, 10); len(hits) != 0 {
		t.Fatalf("the old text was searched: %+v", hits)
	}
	if hits := e.Search("BETA", true, 10); len(hits) != 1 || hits[0].Line != "second beta, longer" {
		t.Fatalf("after the change: %+v", hits)
	}
}

// SearchKind reads one kind's files only: a meeting recording is found by the calendar title the phone sent and by
// the name of the calendar item it was matched to, even when the user renamed it, and a chat about it never uses up
// the limit first. The entries whose name matches come first, then as List ranks them (newest recording first).
func TestSearchKind(t *testing.T) {
	e := goldenEngine(t, nil)
	day := func(d, h int) time.Time { return time.Date(2026, 11, d, h, 0, 0, 0, goldenZone) }
	now := day(10, 18)
	c := &Chat{ID: ChatID("lark", "g1"), Source: "lark", SourceID: "g1", Kind: "group", Name: "Team", Created: now}
	if err := e.Store.PutChat(c); err != nil {
		t.Fatal(err)
	}
	if err := e.Store.PutSummary(KindChat, c.ID, "Ada booked the design review for Monday."); err != nil {
		t.Fatal(err)
	}
	if err := e.Store.AppendMessages(c, []Line{{At: day(9, 9), Sender: "Bo", Text: "design review moved to 10:00"}}); err != nil {
		t.Fatal(err)
	}
	if err := e.Store.PutItem(&Item{Kind: KindMeeting, ID: "m1", Title: "Design review", At: day(3, 10)}); err != nil {
		t.Fatal(err)
	}
	const (
		oldest  = "1a8fad5b-d9cb-469f-a165-70867728950e" // named after the phone's calendar title
		matched = "2a8fad5b-d9cb-469f-a165-70867728950e" // renamed; the calendar item it overlaps says Design review
		phone   = "3a8fad5b-d9cb-469f-a165-70867728950e" // renamed; the phone's calendar said Design review
		other   = "4a8fad5b-d9cb-469f-a165-70867728950e"
	)
	up := func(id string, start time.Time, title, calendar string, texts ...string) {
		t.Helper()
		u := recUpload(1, start, true, texts...)
		u.Title = title
		if calendar != "" {
			u.Calendar = &RecordingCalendar{Title: calendar}
		}
		putRecording(t, e, id, u, now)
	}
	up(oldest, day(1, 10), "", "Design review", "the export drops large entries")
	up(matched, day(3, 10), "Ada's notes", "", "we keep the cap")
	up(phone, day(8, 10), "Bo's notes", "Design review", "Bo adds the test")
	up(other, day(9, 10), "Lunch", "", "nothing about it")
	e.matchRecordings(now)
	// The summarizer tags recordings: an older one tagged higher still comes after a newer one (kb_list's order).
	if r := recordingNow(t, e, matched); true {
		r.Tagging = Tagging{Importance: "P0", Relevance: 95}
		if err := e.Store.putEntity(KindRecording, r.ID, r, &r.Tagging, false); err != nil {
			t.Fatal(err)
		}
	}
	if r := recordingNow(t, e, matched); r.Meeting == nil || r.Meeting.Name != "Design review" || r.Name != "Ada's notes" {
		t.Fatalf("matched: %+v %q", r.Meeting, r.Name)
	}

	// Search walks the chats first: one hit is the chat's.
	if hits := e.Search("design review", true, 1); len(hits) != 1 || !strings.HasPrefix(hits[0].Path, "chats/") {
		t.Fatalf("Search: %+v", hits)
	}
	var order []string
	for _, h := range e.SearchKind("Design REVIEW", KindRecording, true, 40) {
		kind, id, _ := entryOf(h.Path)
		if kind != KindRecording {
			t.Fatalf("a hit of another kind: %+v", h)
		}
		if len(order) == 0 || order[len(order)-1] != id {
			order = append(order, id)
		}
	}
	if want := []string{oldest, phone, matched}; !reflect.DeepEqual(order, want) {
		t.Fatalf("recordings found %v, want %v", order, want)
	}
	if hits := e.SearchKind("design review", KindRecording, true, 2); len(hits) != 2 {
		t.Fatalf("limit: %+v", hits)
	}
	if hits := e.SearchKind("design review", KindMeeting, true, 40); len(hits) != 1 || hits[0].Path != "meetings/m1.json" {
		t.Fatalf("meetings: %+v", hits)
	}
	// Chats: their message days only with messages, after the profiles.
	if hits := e.SearchKind("design review", KindChat, false, 40); len(hits) != 1 || hits[0].Path != "chats/lark-g1/summary.md" {
		t.Fatalf("chats without the messages: %+v", hits)
	}
	if hits := e.SearchKind("design review", KindChat, true, 40); len(hits) != 2 || !strings.Contains(hits[1].Path, "/messages/") {
		t.Fatalf("chats: %+v", hits)
	}
	for _, kind := range []string{"", "moments", "recording"} {
		if hits := e.SearchKind("design review", kind, true, 40); hits != nil {
			t.Fatalf("kind %q: %+v", kind, hits)
		}
	}
	if hits := e.SearchKind("  ", KindRecording, true, 40); hits != nil {
		t.Fatalf("no query: %+v", hits)
	}
}
