package kbsent

import (
	"encoding/json"
	"testing"
	"time"

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
