package kb

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestDayIdentitySurvivesRezoneAndReopen(t *testing.T) {
	s, e := replayStore(t)
	if _, err := s.Rezone(time.UTC); err != nil {
		t.Fatal(err)
	}
	at := time.Now().UTC().Truncate(24 * time.Hour).Add(-23*time.Hour + 12*time.Second)
	ingestMessages(t, e, "example", "thread", 2,
		SourceMessage{ID: "sent-ref", At: at, Text: "Synthetic text\n  continued", Mine: true},
		SourceMessage{ID: "other-ref", At: at, Text: "Different text", SenderName: "Example"})
	check := func(e *Engine, day string, zone *time.Location) {
		t.Helper()
		msgs, _, _, identity, err := e.DayWithIdentity("example-thread", day)
		if err != nil || len(msgs) != 2 || identity.Chat.SourceID != "thread" || identity.Zone.String() != zone.String() {
			t.Fatalf("day: messages=%+v identity=%+v error=%v", msgs, identity, err)
		}
		first, other := identity.Messages[msgs[0].N], identity.Messages[msgs[1].N]
		if first.Ref != "sent-ref" || !first.At.Equal(at) || !first.Mine || first.Ambiguous ||
			other.Ref != "other-ref" || other.Mine || other.Ambiguous {
			t.Fatalf("identities: first=%+v other=%+v", first, other)
		}
	}
	check(e, at.Format("2006-01-02"), time.UTC)
	zone, err := time.LoadLocation("Etc/GMT+8")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Rezone(zone); err != nil {
		t.Fatal(err)
	}
	if at.In(zone).Format("2006-01-02") == at.Format("2006-01-02") {
		t.Fatal("fixture did not cross a day boundary")
	}
	check(e, at.In(zone).Format("2006-01-02"), zone)
	readOnly, err := OpenReadOnly(s.Dir)
	if err != nil {
		t.Fatal(err)
	}
	check(&Engine{Store: readOnly}, at.In(zone).Format("2006-01-02"), zone)
}

func TestDayIdentityRejectsAmbiguityAndLegacyTimestampGuesses(t *testing.T) {
	s, e := replayStore(t)
	year, month, dayOfMonth := time.Now().In(s.Zone()).Date()
	at := time.Date(year, month, dayOfMonth-1, 12, 0, 0, 0, s.Zone())
	ingestMessages(t, e, "example", "thread", 4,
		SourceMessage{ID: "same-one", At: at, Text: "Same text", Mine: true},
		SourceMessage{ID: "same-two", At: at.Add(time.Second), Text: "Same text", Mine: true},
		SourceMessage{ID: "legacy", At: at.Add(time.Minute), Text: "Legacy text", Mine: true},
		SourceMessage{ID: "edited", At: at.Add(2 * time.Minute), Text: "Original text", Mine: true})
	r, err := s.receipts("example", "thread", "example-thread")
	if err != nil {
		t.Fatal(err)
	}
	delete(r.Lines, "legacy") // Old receipts know only the timestamp.
	if err := writeJSON(s.path(KindChat, "example-thread", ingestReceipts), r); err != nil {
		t.Fatal(err)
	}
	day := at.In(s.Zone()).Format("2006-01-02")
	path := s.path(KindChat, "example-thread", "messages", day+".md")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(strings.ReplaceAll(string(raw), "Original text", "Edited text")), 0o600); err != nil {
		t.Fatal(err)
	}
	msgs, _, _, identity, err := e.DayWithIdentity("example-thread", day)
	if err != nil || len(msgs) != 4 {
		t.Fatalf("day: %v, %v", msgs, err)
	}
	for i, m := range msgs {
		got := identity.Messages[m.N]
		if got.Ref != "" || got.Ambiguous != (i < 2) {
			t.Errorf("message %d guessed an identity: %+v", i, got)
		}
	}
	// Removing one indistinguishable line still leaves two possible receipts.
	lines := strings.Split(string(raw), "\n")
	lines = append(lines[:msgs[0].N-1], lines[msgs[0].N:]...)
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")), 0o600); err != nil {
		t.Fatal(err)
	}
	msgs, _, _, identity, err = e.DayWithIdentity("example-thread", day)
	if err != nil || !identity.Messages[msgs[0].N].Ambiguous {
		t.Fatalf("duplicate receipt: %+v, %v", identity, err)
	}
}

func TestDayIdentityFailsClosedOnIncompleteOrUnreadableReceipts(t *testing.T) {
	for _, name := range []string{ingestJournal, ingestReceipts, filepath.Join("messages", zoneFile)} {
		t.Run(name, func(t *testing.T) {
			s, e := replayStore(t)
			at := time.Now().Add(-time.Hour)
			ingestMessages(t, e, "example", "thread", 1, SourceMessage{ID: "ref", At: at, Text: "Synthetic text", Mine: true})
			if err := os.WriteFile(s.path(KindChat, "example-thread", name), []byte("broken"), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, _, _, _, err := e.DayWithIdentity("example-thread", at.In(s.Zone()).Format("2006-01-02")); err == nil {
				t.Fatal("corrupt or incomplete archive returned identity evidence")
			}
		})
	}
}

func TestDayIdentityReadOnlyZoneFallbackAndArchivePrecedence(t *testing.T) {
	at := time.Date(2026, 11, 1, 15, 30, 12, 0, time.UTC)
	zones := make([]*time.Location, 2)
	for i, name := range []string{"Asia/Tokyo", "America/Los_Angeles"} {
		var err error
		if zones[i], err = time.LoadLocation(name); err != nil {
			t.Fatal(err)
		}
	}
	// Pick a user zone whose calendar day differs from the machine's, without changing process-global time.Local.
	if at.In(zones[0]).Format("2006-01-02") == at.In(time.Local).Format("2006-01-02") {
		zones[0], zones[1] = zones[1], zones[0]
	}
	userZone, archivedZone := zones[0], zones[1]
	if at.In(userZone).Format("2006-01-02") == at.In(time.Local).Format("2006-01-02") {
		t.Fatal("fixture did not cross the machine and user day boundary")
	}
	for _, marked := range []bool{false, true} {
		name := "missing marker"
		zone := userZone
		if marked {
			name, zone = "archived marker", archivedZone
		}
		t.Run(name, func(t *testing.T) {
			s, _ := replayStore(t)
			chat := &Chat{ID: "example-thread", Source: "example", SourceID: "thread", Name: "Example"}
			if err := s.PutChat(chat); err != nil {
				t.Fatal(err)
			}
			day := at.In(zone).Format("2006-01-02")
			prev := at.In(zone).AddDate(0, 0, -1).Format("2006-01-02")
			next := at.In(zone).AddDate(0, 0, 1).Format("2006-01-02")
			for d, body := range map[string]string{
				prev: "# Previous day\n", next: "# Next day\n",
				day: "- " + at.In(zone).Format("15:04") + " Me: Synthetic text\n",
			} {
				if err := writeFile(s.path(KindChat, chat.ID, "messages", d+".md"), []byte(body)); err != nil {
					t.Fatal(err)
				}
			}
			if marked {
				if err := writeFile(s.path(KindChat, chat.ID, "messages", zoneFile), []byte(zone.String()+"\n")); err != nil {
					t.Fatal(err)
				}
			}
			r := messageReceipts{Source: chat.Source, Chat: chat.SourceID,
				IDs:   map[string]time.Time{"sent-ref": at},
				Lines: map[string]messageReceiptLine{"sent-ref": {Hash: receiptBodyHash("Me: Synthetic text"), Mine: true}}}
			if err := writeJSON(s.path(KindChat, chat.ID, ingestReceipts), r); err != nil {
				t.Fatal(err)
			}
			readOnly, err := OpenReadOnly(s.Dir)
			if err != nil {
				t.Fatal(err)
			}
			readOnly.zone = userZone
			e := &Engine{Store: readOnly}
			before := dayIdentityArchiveFiles(t, s.Dir)
			plain, plainPrev, plainNext, err := e.Day(chat.ID, day)
			if err != nil {
				t.Fatal(err)
			}
			msgs, gotPrev, gotNext, identity, err := e.DayWithIdentity(chat.ID, day)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(msgs, plain) || gotPrev != plainPrev || gotNext != plainNext || gotPrev != prev || gotNext != next {
				t.Fatalf("identity read changed the day: messages=%+v prev=%q next=%q", msgs, gotPrev, gotNext)
			}
			if len(msgs) != 1 || identity.Zone.String() != zone.String() {
				t.Fatalf("messages=%+v zone=%v, want one message in %s", msgs, identity.Zone, zone)
			}
			got := identity.Messages[msgs[0].N]
			if got.Ref != "sent-ref" || !got.At.Equal(at) || !got.Mine || got.Ambiguous {
				t.Fatalf("identity in %s: %+v", zone, got)
			}
			if !reflect.DeepEqual(before, dayIdentityArchiveFiles(t, s.Dir)) {
				t.Fatal("reading identity changed the read-only archive")
			}
		})
	}
}

func TestDayIdentityReadOnlyRejectsBrokenZone(t *testing.T) {
	for _, directory := range []bool{false, true} {
		s, _ := replayStore(t)
		chat := &Chat{ID: "example-thread", Source: "example", SourceID: "thread"}
		if err := s.PutChat(chat); err != nil {
			t.Fatal(err)
		}
		path := s.path(KindChat, chat.ID, "messages", zoneFile)
		if directory {
			if err := os.MkdirAll(path, 0700); err != nil {
				t.Fatal(err)
			}
		} else if err := writeFile(path, []byte("Not/A_Time_Zone\n")); err != nil {
			t.Fatal(err)
		}
		readOnly, err := OpenReadOnly(s.Dir)
		if err != nil {
			t.Fatal(err)
		}
		readOnly.zone = time.UTC
		if _, _, _, _, err := (&Engine{Store: readOnly}).DayWithIdentity(chat.ID, "2026-11-02"); err == nil || err == ErrNotFound {
			t.Fatalf("zone error was lost (directory=%v): %v", directory, err)
		}
	}
}

func dayIdentityArchiveFiles(t *testing.T, dir string) map[string]string {
	t.Helper()
	files := map[string]string{}
	err := filepath.WalkDir(dir, func(path string, entry os.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		raw, err := os.ReadFile(path)
		if err == nil {
			files[path] = string(raw)
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return files
}
