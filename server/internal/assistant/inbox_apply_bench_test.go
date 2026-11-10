package assistant

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/connector"
)

// benchInbox is a busy week's inbox: n Feishu chat items of two accounts (one in four still open, the rest done
// within inboxForget) and as many items of a plugin connector.
func benchInbox(n int, now time.Time) *inboxBook {
	b := &inboxBook{}
	b.init()
	done := now.Add(-time.Hour)
	for i := range n {
		at := now.Add(-time.Duration(i) * time.Minute)
		chat := &InboxItem{ID: fmt.Sprintf("lark:%d", i), Source: "lark", UpdatedAt: at, Last: "m1",
			Lark: &InboxLark{AccountID: 1 + i%2, FeedID: fmt.Sprintf("feed%d", i%(n/4))}}
		thing := &InboxItem{ID: fmt.Sprintf("github:%d", i), Source: "github", UpdatedAt: at, Last: "m1",
			Connector: &InboxConnector{Thread: fmt.Sprintf("thread%d", i%(n/4))}}
		if i >= n/4 {
			chat.DoneAt, thing.DoneAt = &done, &done
		}
		b.items[chat.ID], b.items[thing.ID] = chat, thing
	}
	return b
}

// benchCollection is one collection of 300 chats (Feishu's unread and @me pages) and 300 connector threads that
// moved nowhere: the open items of benchInbox(2000) say the same again.
func benchCollection(now time.Time) (chats, threads []connector.InboxThing) {
	for i := range 300 {
		account := 1 + i%2
		thread := fmt.Sprintf("chat:%d:feed%d", account, i)
		if account == 1 {
			thread = fmt.Sprintf("chat:feed%d", i)
		}
		block, _ := json.Marshal(InboxLark{AccountID: account, FeedID: fmt.Sprintf("feed%d", i)})
		chats = append(chats, connector.InboxThing{Thread: thread, Last: "m1", At: now, Extra: &connector.ThingExtra{Lark: block}})
		threads = append(threads, connector.InboxThing{Thread: fmt.Sprintf("thread%d", i), Last: "m1", At: now})
	}
	return chats, threads
}

// BenchmarkInboxApply is benchCollection against an inbox of 2000 items of each.
func BenchmarkInboxApply(b *testing.B) {
	now := time.Now()
	ib := benchInbox(2000, now)
	chats, threads := benchCollection(now)
	b.Run("feishu", func(b *testing.B) {
		for b.Loop() {
			ib.applyMigrated("lark", chats, now)
		}
	})
	b.Run("connector", func(b *testing.B) {
		for b.Loop() {
			ib.applyConnector("github", "GitHub", threads, now)
		}
	})
}
