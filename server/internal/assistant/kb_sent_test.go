package assistant

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/assistantwire"
	"github.com/nwverse/persona/server/internal/db"
	"github.com/nwverse/persona/server/internal/kb"
)

func TestKBMarkSent(t *testing.T) {
	at := time.Date(2026, 1, 2, 12, 0, 0, 0, time.UTC)
	chat := &kb.Chat{Source: "example", SourceID: "thread"}
	base := db.SentAsUser{ID: 1, At: at.Add(30 * time.Second), Connector: "example", Target: json.RawMessage(`{"chat":"thread"}`), Text: "same text", MessageRef: "ref"}
	for _, tc := range []struct {
		name    string
		refs    []string
		text    []string
		offsets []time.Duration
		rows    []db.SentAsUser
		want    []int64
	}{
		{"exact ref beats text", []string{"ref", "human"}, []string{"changed text", "same text"}, nil, []db.SentAsUser{base}, []int64{1, 0}},
		{"legacy normalized text", nil, []string{"same\n  text"}, nil, []db.SentAsUser{base}, []int64{1}},
		{"different ref cannot fallback", []string{"human"}, []string{"same text"}, nil, []db.SentAsUser{base}, []int64{0}},
		{"duplicate text is ambiguous", nil, []string{"same text", "same text"}, []time.Duration{0, time.Minute}, []db.SentAsUser{base}, []int64{0, 0}},
		{"one ref cannot mark two rows", []string{"ref", "ref"}, []string{"same text", "changed text"}, nil, []db.SentAsUser{base}, []int64{0, 0}},
		{"duplicate ledger is ambiguous", []string{"ref"}, []string{"same text"}, nil, []db.SentAsUser{base, base}, []int64{0}},
		{"outside window remains human", nil, []string{"same text", "same text"}, []time.Duration{0, 10 * time.Minute}, []db.SentAsUser{base}, []int64{1, 0}},
		{"no prefix match", nil, []string{"same text and more"}, nil, []db.SentAsUser{base}, []int64{0}},
		{"case matters", nil, []string{"Same text"}, nil, []db.SentAsUser{base}, []int64{0}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			messages := make([]kb.DayMessage, len(tc.text))
			lines := make([]kbSentLine, len(tc.text))
			for i, text := range tc.text {
				offset := time.Duration(0)
				if tc.offsets != nil {
					offset = tc.offsets[i]
				}
				ref := ""
				if tc.refs != nil {
					ref = tc.refs[i]
				}
				lines[i] = kbSentLine{message: &messages[i], identity: kb.MessageIdentity{Ref: ref, Mine: true}, minute: at.Add(offset), body: kbSentText(text), mine: true}
			}
			kbMarkSent(lines, chat, tc.rows)
			for i, m := range messages {
				if m.SentLedgerID != tc.want[i] {
					t.Errorf("row %d = %+v, want ledger %d", i, m, tc.want[i])
				}
			}
		})
	}
	for _, mutation := range []func(*db.SentAsUser){
		func(r *db.SentAsUser) { r.Connector = "other" },
		func(r *db.SentAsUser) { r.Target = json.RawMessage(`{"chat":"other"}`) },
		func(r *db.SentAsUser) { r.Target = json.RawMessage(`{"subject":"thread"}`) },
	} {
		row := base
		mutation(&row)
		m := kb.DayMessage{}
		kbMarkSent([]kbSentLine{{message: &m, identity: kb.MessageIdentity{Ref: "ref"}, mine: true, minute: at, body: "sametext"}}, chat, []db.SentAsUser{row})
		if m.SentBy != "" {
			t.Fatalf("wrong destination matched: %+v", row)
		}
	}
}

func TestKBMarkSentMissingReferenceAndRecall(t *testing.T) {
	at := time.Date(2026, 1, 2, 12, 0, 0, 0, time.UTC)
	recalled := at.Add(time.Hour)
	m := kb.DayMessage{}
	row := db.SentAsUser{ID: 3, At: at, Connector: "example", Target: json.RawMessage(`{"chat":"thread"}`), Text: "content", RecalledAt: &recalled}
	kbMarkSent([]kbSentLine{{message: &m, identity: kb.MessageIdentity{Ref: "archive-only"}, mine: true, minute: at, body: "content"}}, &kb.Chat{Source: "example", SourceID: "thread"}, []db.SentAsUser{row})
	if m.SentBy != "assistant" || m.SentLedgerID != 3 || m.RecalledAt == nil || !m.RecalledAt.Equal(recalled) {
		t.Fatalf("missing ref or recall: %+v", m)
	}
}

// An unavailable store must not look like a successful empty provenance read.
type kbSentFailure struct{ *kbStore }

func (*kbSentFailure) SentAsUsersForArchive(context.Context, string, time.Time, time.Time) ([]db.SentAsUser, error) {
	return nil, errors.New("ledger unavailable")
}

func TestKBDaySentDatabaseAndTimezone(t *testing.T) {
	database, err := db.Open(context.Background(), newDatabase(t), db.ManagerSet)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(database.Close)
	s := kbServiceOn(t, newFakeHub(), database, t.TempDir(), nil)
	e := kbEng(t, s)
	zone, err := time.LoadLocation("Etc/GMT-8")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.Store.Rezone(zone); err != nil {
		t.Fatal(err)
	}
	chat := &kb.Chat{ID: "example-sent-thread", Source: "example", SourceID: "thread", Name: "Example"}
	if err := e.Store.PutChat(chat); err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 1, 3, 0, 0, 0, 0, zone)
	// i18n-ignore: the archive's canonical owner label
	owner := "我"
	if err := e.Store.AppendMessages(chat, []kb.Line{
		{At: at, Sender: owner, Text: "unique\n  content"},
		{At: at.Add(10 * time.Minute), Sender: owner, Text: "unique content"},
		{At: at.Add(-time.Minute), Sender: owner, Text: "boundary duplicate"},
		{At: at, Sender: owner, Text: "boundary duplicate"},
		{At: at, Sender: "Example", Text: "someone else"},
	}); err != nil {
		t.Fatal(err)
	}
	id, err := database.RecordSentAsUser(context.Background(), db.SentAsUser{At: at.Add(-15 * time.Second), Connector: "example", Target: json.RawMessage(`{"chat":"thread"}`), Text: "unique content"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.MarkSentAsUserRecalled(context.Background(), id, at.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	for _, text := range []string{"boundary duplicate", "someone else"} {
		if _, err := database.RecordSentAsUser(context.Background(), db.SentAsUser{At: at.Add(-15 * time.Second), Connector: "example", Target: json.RawMessage(`{"chat":"thread"}`), Text: text}); err != nil {
			t.Fatal(err)
		}
	}
	for _, day := range []string{"2026-01-02", "2026-01-03"} {
		params, _ := json.Marshal(assistantwire.KBDayParams{ID: chat.ID, Day: day})
		out, ok, err := s.kbRequest(context.Background(), assistantwire.MethodKBExtensionDay, params)
		if err != nil || !ok {
			t.Fatalf("RPC: %v %v", ok, err)
		}
		got := out.(assistantwire.KBDayResult)
		if got.SentOrigin != "available" {
			t.Fatal(got)
		}
		for i, m := range got.Messages {
			want := day == "2026-01-03" && i == 0
			if (m.SentBy == "assistant") != want || (want && (m.SentLedgerID != id || m.RecalledAt == nil)) {
				t.Fatalf("%s row %d: %+v", day, i, m)
			}
		}
	}
	// Direct, ordinary kb.day never leaks privileged markers.
	out, _, err := s.kbRequest(context.Background(), assistantwire.MethodKBDay, json.RawMessage(`{"id":"example-sent-thread","day":"2026-01-03"}`))
	if err != nil {
		t.Fatal(err)
	}
	plain := out.(assistantwire.KBDayResult)
	if plain.SentOrigin != "unavailable" || plain.Messages[0].SentBy != "" {
		t.Fatal(plain)
	}
}

func TestKBDaySentStoreFailure(t *testing.T) {
	for _, store := range []Store{newKBStore(), &kbSentFailure{newKBStore()}} {
		s := kbServiceOn(t, newFakeHub(), store, t.TempDir(), nil)
		e := kbEng(t, s)
		chat := &kb.Chat{ID: "example-thread", Source: "example", SourceID: "thread"}
		if err := e.Store.PutChat(chat); err != nil {
			t.Fatal(err)
		}
		at := time.Now().Add(-time.Hour)
		if err := e.Store.AppendMessages(chat, []kb.Line{{At: at, Sender: "Example", Text: "content"}}); err != nil {
			t.Fatal(err)
		}
		got, err := s.kbDaySent(context.Background(), e, assistantwire.KBDayParams{ID: chat.ID, Day: at.In(e.Store.Zone()).Format("2006-01-02")})
		_, failure := store.(*kbSentFailure)
		if failure && err == nil || !failure && (err != nil || got.SentOrigin != "unavailable") {
			t.Fatalf("got=%+v err=%v", got, err)
		}
	}
}

func TestKBMarkSentDoesNotReassignDuplicateLedger(t *testing.T) {
	at := time.Date(2026, 1, 2, 12, 0, 0, 0, time.UTC)
	messages := make([]kb.DayMessage, 2)
	lines := []kbSentLine{
		{message: &messages[0], identity: kb.MessageIdentity{Ref: "ref"}, mine: true, minute: at, body: "same"},
		{message: &messages[1], mine: true, minute: at.Add(time.Minute), body: "same"},
	}
	row := db.SentAsUser{ID: 1, At: at, Connector: "example", Target: json.RawMessage(`{"chat":"thread"}`), Text: "same", MessageRef: "ref"}
	duplicate := row
	duplicate.ID = 2
	duplicate.MessageRef = ""
	kbMarkSent(lines, &kb.Chat{Source: "example", SourceID: "thread"}, []db.SentAsUser{row, duplicate})
	if messages[0].SentLedgerID != 1 || messages[1].SentBy != "" {
		t.Fatalf("duplicate reassigned: %+v", messages)
	}
}

func TestKBSentRepeatedMinutes(t *testing.T) {
	for _, tc := range []struct {
		zone, clock string
		repeated    bool
	}{
		{"America/New_York", "2026-11-01 01:30", true},
		{"Australia/Lord_Howe", "2026-04-05 01:45", true},
		{"Australia/Lord_Howe", "2026-04-05 02:15", false},
		{"Etc/GMT-8", "2026-01-02 00:00", false},
	} {
		zone, err := time.LoadLocation(tc.zone)
		if err != nil {
			t.Fatal(err)
		}
		at, err := time.ParseInLocation("2006-01-02 15:04", tc.clock, zone)
		if err != nil {
			t.Fatal(err)
		}
		if got := kbSentRepeatedMinute(at, zone); got != tc.repeated {
			t.Errorf("%s %s repeated=%v", tc.zone, tc.clock, got)
		}
	}
}

type kbSentSource struct{}

func (kbSentSource) Name() string                                        { return "example" }
func (kbSentSource) Chats(context.Context, int) ([]kb.SourceChat, error) { return nil, nil }
func (kbSentSource) Messages(context.Context, kb.SourceChat, string, int) ([]kb.SourceMessage, string, bool, error) {
	return nil, "", false, nil
}
func (kbSentSource) Download(context.Context, string, string) error { return nil }

func TestKBDaySentIngestedReference(t *testing.T) {
	database, err := db.Open(context.Background(), newDatabase(t), db.ManagerSet)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(database.Close)
	s := kbServiceOn(t, newFakeHub(), database, t.TempDir(), nil)
	e := kbEng(t, s)
	recent := time.Now().In(e.Store.Zone()).AddDate(0, 0, -1)
	at := time.Date(recent.Year(), recent.Month(), recent.Day(), 12, 0, 0, 0, e.Store.Zone())
	n, err := e.Ingest(context.Background(), kbSentSource{}, kb.SourceChat{ID: "thread", Name: "Example", Kind: "group"}, []kb.SourceMessage{
		{ID: "assistant-ref", At: at, Text: "archived rendering", Mine: true},
		{ID: "human-ref", At: at.Add(time.Minute), Text: "ledger words", Mine: true},
	})
	if err != nil || n != 2 {
		t.Fatalf("ingest %d: %v", n, err)
	}
	id, err := database.RecordSentAsUser(context.Background(), db.SentAsUser{At: at, Connector: "example", Target: json.RawMessage(`{"chat":"thread"}`), Text: "ledger words", MessageRef: "assistant-ref"})
	if err != nil {
		t.Fatal(err)
	}
	params, _ := json.Marshal(assistantwire.KBDayParams{ID: "example-thread", Day: at.In(e.Store.Zone()).Format("2006-01-02")})
	out, ok, err := s.kbRequest(context.Background(), assistantwire.MethodKBExtensionDay, params)
	if err != nil || !ok {
		t.Fatalf("RPC handled=%v error=%v", ok, err)
	}
	got := out.(assistantwire.KBDayResult)
	if len(got.Messages) != 2 || got.Messages[0].SentLedgerID != id || got.Messages[1].SentBy != "" {
		t.Fatalf("ref matching: %+v", got)
	}
}

func TestKBSentAmbiguousMinuteBlocksNearbyHumanText(t *testing.T) {
	zone, err := time.LoadLocation("Australia/Lord_Howe")
	if err != nil {
		t.Fatal(err)
	}
	messages := []kb.DayMessage{{N: 1, Text: "01:59 我: same"}, {N: 2, Text: "02:00 我: same"}} // i18n-ignore: archive owner data
	lines := kbSentLines(messages, kb.DayIdentity{Zone: zone, Messages: map[int]kb.MessageIdentity{}}, "2026-04-05")
	at, err := time.Parse(time.RFC3339, "2026-04-05T01:59:30+10:30")
	if err != nil {
		t.Fatal(err)
	}
	kbMarkSent(lines, &kb.Chat{Source: "example", SourceID: "thread"}, []db.SentAsUser{{ID: 1, At: at, Connector: "example", Target: json.RawMessage(`{"chat":"thread"}`), Text: "same"}})
	for _, m := range messages {
		if m.SentBy != "" {
			t.Fatalf("ambiguous minute reassigned: %+v", messages)
		}
	}
}

func TestKBSentProviderReferencesStayInTheirSource(t *testing.T) {
	for _, tc := range []struct{ connector, action, ref string }{
		{"github", "github_comment", "c42"},
		{"github", "github_review", "r42"},
		{"lark", "send", "42"},
	} {
		for _, source := range []string{tc.connector, "posts"} {
			message := kb.DayMessage{}
			kbMarkSent([]kbSentLine{{message: &message, mine: true, identity: kb.MessageIdentity{Ref: tc.ref}}}, &kb.Chat{Connector: tc.connector, Source: source, SourceID: "thread"}, []db.SentAsUser{{ID: 1, Connector: tc.connector, Action: tc.action, MessageRef: "42"}})
			if (message.SentBy == "assistant") != (source == tc.connector) {
				t.Errorf("%s/%s: %+v", tc.connector, source, message)
			}
		}
	}
}
