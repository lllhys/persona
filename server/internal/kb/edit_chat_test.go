package kb

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"
	"testing"
	"time"
)

const editChatID = "example-thread"

func editChatMessage(i int, at time.Time) SourceMessage {
	return SourceMessage{ID: fmt.Sprintf("m%d", i), SenderID: "u1", SenderName: "Example", Text: fmt.Sprintf("line %d", i),
		At: at.Add(time.Duration(i) * time.Second)}
}

// A hand edit of a chat that writes a linked project while an ingest runs keeps the messages, pending count and last
// message time the ingest wrote: the chat is read again under chatMu and the project is written after it.
func TestApplyChatKeepsAnIngestWrittenMeanwhile(t *testing.T) {
	s, e := replayStore(t)
	at := time.Now().Add(-time.Hour).Truncate(time.Second)
	ingestMessages(t, e, "example", "thread", 1, editChatMessage(0, at))
	if err := s.PutProject(&Project{Slug: "launch", Name: "Launch"}); err != nil {
		t.Fatal(err)
	}
	late := editChatMessage(1, at)
	var once sync.Once
	e.Changed = func(c Change) {
		if c.Kind == KindProject && c.ID == "launch" {
			once.Do(func() { ingestMessages(t, e, "example", "thread", 1, late) })
		}
	}
	e.ObserveChanges()
	editApply(t, e, KindChat, editChatID, Update{Tags: []string{"edited"}, Projects: []string{"launch"}})
	assertArchiveCounts(t, s, editChatID, 2, 2)
	c, _ := s.Chat(editChatID)
	if !c.LastAt.Equal(late.At) || !slices.Equal(c.Tags, []string{"edited"}) || !slices.Contains(c.Projects, "launch") {
		t.Fatalf("chat after edit: last %v tags %v projects %v", c.LastAt, c.Tags, c.Projects)
	}
	if p, _ := s.Project("launch"); !slices.Contains(p.Chats, editChatID) {
		t.Fatalf("project chats %v", p.Chats)
	}
}

// A hand edit finishes an interrupted ingest and edits the chat it wrote, instead of refusing the edit.
func TestApplyChatFinishesAnInterruptedIngest(t *testing.T) {
	s, e := replayStore(t)
	at := time.Now().Add(-time.Hour).Truncate(time.Second)
	ingestMessages(t, e, "example", "thread", 1, editChatMessage(0, at))
	injected := errors.New("synthetic archive write failure")
	calls := 0
	s.ingestWrite = func(path string, raw []byte) error {
		if calls++; calls == 2 { // the intent is durable, the day file is not
			return injected
		}
		return archiveWrite(path, raw)
	}
	if _, err := e.Ingest(context.Background(), named("example"), SourceChat{ID: "thread", Name: "Example", Kind: "group"},
		[]SourceMessage{editChatMessage(1, at)}); !errors.Is(err, injected) {
		t.Fatalf("failure not exercised: %v", err)
	}
	s.ingestWrite = nil
	editApply(t, e, KindChat, editChatID, Update{Tags: []string{"edited"}})
	assertArchiveCounts(t, s, editChatID, 2, 2)
	if c, _ := s.Chat(editChatID); !slices.Equal(c.Tags, []string{"edited"}) {
		t.Fatalf("tags %v", c.Tags)
	}
}

// Hand edits that write a chat (its own tags, a project linking or unlinking it, a person unlinking it) run against
// a stream of single-message ingests; none writes back an older count.
func TestApplyChatEditsRaceIngest(t *testing.T) {
	s, e := replayStore(t)
	at := time.Now().Add(-time.Hour).Truncate(time.Second)
	ingestMessages(t, e, "example", "thread", 1, editChatMessage(0, at))
	if err := s.PutPerson(&Person{ID: "example-u1", Name: "Example"}); err != nil {
		t.Fatal(err)
	}
	const n = 100
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := 1; i <= n; i++ {
			if _, err := e.Ingest(context.Background(), named("example"), SourceChat{ID: "thread", Name: "Example", Kind: "group"},
				[]SourceMessage{editChatMessage(i, at)}); err != nil {
				t.Error(err)
				return
			}
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < n; i++ {
			var err error
			switch i % 4 {
			case 0:
				_, err = e.Apply(KindChat, editChatID, Update{Tags: []string{fmt.Sprintf("t%d", i)}})
			case 1:
				_, err = e.Apply(KindProject, "launch", Update{Name: "Launch", Chats: []string{editChatID}})
			case 2:
				_, err = e.Apply(KindProject, "launch", Update{RemoveChats: []string{editChatID}})
			case 3:
				_, err = e.Apply(KindPerson, "example-u1", Update{RemoveChats: []string{editChatID}})
			}
			if err != nil {
				t.Error(err)
				return
			}
		}
	}()
	wg.Wait()
	assertArchiveCounts(t, s, editChatID, n+1, n+1)
	if c, _ := s.Chat(editChatID); !c.LastAt.Equal(editChatMessage(n, at).At) {
		t.Fatalf("last at %v, want %v", c.LastAt, editChatMessage(n, at).At)
	}
}
