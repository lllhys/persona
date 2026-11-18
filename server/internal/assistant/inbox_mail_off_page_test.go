package assistant

import (
	"context"
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/assistantwire"
)

// Mail items whose mail left the account's latest INBOX page (archived or deleted in the mail client, or pushed off
// by newer mail) are read elsewhere and aged like those on it (#5993). No Postgres.

// offPageHub is a hub with one account whose INBOX holds an unread "Weekly digest" (UID 1) and two read mails, after
// a round made the digest's item, triaged under tier.
func offPageHub(t *testing.T, tier string, sent time.Time) (*inboxHub, *InboxItem) {
	t.Helper()
	h := newInboxHub(t)
	h.in.mail = &fakeMail{accounts: []assistantwire.InboxMailAccount{{ID: "a1", Email: "me@example.com", Messages: []assistantwire.InboxMailSummary{
		{Mailbox: "INBOX", UID: 3, Subject: "Invoice 1042", Seen: true, From: MailAddress{Name: "Shop", Email: "shop@example.com"}, Date: sent.Add(2 * time.Minute).Format(time.RFC3339)},
		{Mailbox: "INBOX", UID: 2, Subject: "Lunch on Friday", Seen: true, From: MailAddress{Name: "Alex", Email: "alex@example.com"}, Date: sent.Add(time.Minute).Format(time.RFC3339)},
		{Mailbox: "INBOX", UID: 1, Subject: "Weekly digest", From: MailAddress{Name: "News", Email: "news@example.com"}, Date: sent.Format(time.RFC3339)},
	}}}}
	h.collectInbox(context.Background(), roundFull)
	waitTriage(h) // the round's triage pass (no model: the fallback tier) runs in the background
	h.inbox.mu.Lock()
	defer h.inbox.mu.Unlock()
	var it *InboxItem
	for _, x := range h.inbox.items {
		if x.Mail != nil && slices.Equal(x.Mail.UIDs, []uint32{1}) {
			it = x
		}
	}
	if it == nil || !it.open() {
		t.Fatalf("setup: no open item for the digest among %d", len(h.inbox.items))
	}
	it.Tier, it.TriagedAt = tier, it.Last // what triage would have said
	return h, it
}

// leaves takes UID 1 off the page; box is the INBOX the hub looks mail up in (nil: it can't say).
func (h *inboxHub) leaves(box map[uint32]bool) {
	h.in.mu.Lock()
	defer h.in.mu.Unlock()
	acc := &h.in.mail.accounts[0]
	acc.Messages = slices.DeleteFunc(slices.Clone(acc.Messages), func(m assistantwire.InboxMailSummary) bool { return m.UID == 1 })
	h.in.mail.box = box
}

func (h *inboxHub) itemState(it *InboxItem) (open, read bool, doneBy string) {
	waitTriage(h)
	h.inbox.mu.Lock()
	defer h.inbox.mu.Unlock()
	return it.open(), it.Read, it.DoneBy
}

// The digest is archived or deleted in the mail client: INBOX keeps the two read mails, far under a page. The next
// round asks the hub about it and files its FYI item, as one read elsewhere on the page is.
func TestInboxMailGoneFromTheInboxIsFiled(t *testing.T) {
	h, it := offPageHub(t, TierFYI, time.Now().Add(-time.Hour))
	h.leaves(map[uint32]bool{2: true, 3: true})
	h.collectInbox(context.Background(), roundFull)
	if open, read, by := h.itemState(it); open || !read || by != "read" {
		t.Fatalf("archived elsewhere: open %v, read %v, done_by %q; want filed as read", open, read, by)
	}
	if asks := h.in.mail.flagAsks; len(asks) != 1 || asks[0].AccountID != "a1" || asks[0].Mailbox != "INBOX" || !slices.Equal(asks[0].UIDs, []uint32{1}) {
		t.Fatalf("lookups asked: %+v", asks)
	}
	h.collectInbox(context.Background(), roundFull)
	if n := len(h.in.mail.flagAsks); n != 1 {
		t.Fatalf("a filed item is asked about again: %d lookups", n)
	}
}

// Control, unchanged: read in the mail client while still on the page, it is filed with no lookup.
func TestInboxMailReadElsewhereOnThePageIsFiled(t *testing.T) {
	h, it := offPageHub(t, TierFYI, time.Now().Add(-time.Hour))
	h.in.mu.Lock()
	h.in.mail.accounts[0].Messages[2].Seen = true
	h.in.mail.box = map[uint32]bool{}
	h.in.mu.Unlock()
	h.collectInbox(context.Background(), roundFull)
	if open, read, by := h.itemState(it); open || !read || by != "read" {
		t.Fatalf("read on the page: open %v, read %v, done_by %q", open, read, by)
	}
	if n := len(h.in.mail.flagAsks); n != 0 {
		t.Fatalf("an item on the page was looked up %d times", n)
	}
}

// Pushed off the page by newer mail: read elsewhere it is filed; still unread in INBOX it stays, and so it does while
// the hub can't say (a hub from before the lookup, or a failed one).
func TestInboxMailOffThePageFollowsTheMailbox(t *testing.T) {
	for _, c := range []struct {
		name     string
		box      map[uint32]bool
		open     bool
		read     bool
		doneBy   string
		lookedUp int
	}{
		{"read elsewhere", map[uint32]bool{1: true}, false, true, "read", 1},
		{"still unread", map[uint32]bool{1: false}, true, false, "", 1},
		{"the hub can't say", nil, true, false, "", 0},
	} {
		t.Run(c.name, func(t *testing.T) {
			h, it := offPageHub(t, TierNoise, time.Now().Add(-time.Hour))
			h.leaves(c.box)
			h.collectInbox(context.Background(), roundFull)
			if open, read, by := h.itemState(it); open != c.open || read != c.read || by != c.doneBy {
				t.Fatalf("open %v, read %v, done_by %q; want %v, %v, %q", open, read, by, c.open, c.read, c.doneBy)
			}
			if n := len(h.in.mail.flagAsks); n != c.lookedUp {
				t.Fatalf("%d lookups, want %d", n, c.lookedUp)
			}
		})
	}
}

// Past the window off the page, an item is filed as one past the window on the page is, without asking.
func TestInboxMailOffThePagePastTheWindowIsFiled(t *testing.T) {
	h, it := offPageHub(t, TierFYI, time.Now().Add(-time.Hour))
	h.leaves(nil) // the hub can't say: the window alone files it
	h.inbox.mu.Lock()
	it.UpdatedAt = time.Now().Add(-inboxWindow - time.Hour)
	h.inbox.mu.Unlock()
	h.collectInbox(context.Background(), roundFull)
	if open, read, by := h.itemState(it); open || !read || by != "read" {
		t.Fatalf("past the window off the page: open %v, read %v, done_by %q", open, read, by)
	}
}

// Needs you gets nothing beyond the rule on the page: gone or past the window, it is read and stays open.
func TestInboxMailOffThePageNeedsYouStaysOpen(t *testing.T) {
	for _, c := range []struct {
		name string
		box  map[uint32]bool
		age  time.Duration
	}{
		{"gone", map[uint32]bool{}, 0},
		{"past the window", nil, inboxWindow + time.Hour},
	} {
		t.Run(c.name, func(t *testing.T) {
			h, it := offPageHub(t, TierAction, time.Now().Add(-time.Hour))
			h.leaves(c.box)
			if c.age > 0 {
				h.inbox.mu.Lock()
				it.UpdatedAt = time.Now().Add(-c.age)
				h.inbox.mu.Unlock()
			}
			h.collectInbox(context.Background(), roundFull)
			if open, read, by := h.itemState(it); !open || !read || by != "" {
				t.Fatalf("Needs you off the page: open %v, read %v, done_by %q; want open and read", open, read, by)
			}
		})
	}
}

// One lookup asks about at most InboxMailFlagsMost mails; the ones left out are asked first next round.
func TestInboxMailOffThePageLookupsAreBounded(t *testing.T) {
	h := newInboxHub(t)
	a := assistantwire.InboxMailAccount{ID: "a1", Email: "me@example.com"}
	now := time.Now()
	const items = assistantwire.InboxMailFlagsMost + 50
	var page []assistantwire.InboxMailSummary
	for uid := uint32(1); uid <= items; uid++ {
		page = append(page, assistantwire.InboxMailSummary{Mailbox: "INBOX", UID: uid, Subject: fmt.Sprintf("Offer %d", uid),
			From: MailAddress{Name: "Shop", Email: "shop@example.com"}, Date: now.Add(-time.Hour).Format(time.RFC3339)})
	}
	h.inbox.applyMail(a, page, now)
	h.in.mail = &fakeMail{box: map[uint32]bool{}} // every one still unread in INBOX
	for uid := uint32(1); uid <= items; uid++ {
		h.in.mail.box[uid] = false
	}
	ctx := context.Background()
	h.settleMailOffPage(ctx, a, now)
	h.settleMailOffPage(ctx, a, now.Add(time.Minute))
	asks := h.in.mail.flagAsks
	if len(asks) != 2 || len(asks[0].UIDs) != assistantwire.InboxMailFlagsMost || len(asks[1].UIDs) != assistantwire.InboxMailFlagsMost {
		t.Fatalf("lookups: %d, sizes %v", len(asks), func() (n []int) {
			for _, a := range asks {
				n = append(n, len(a.UIDs))
			}
			return n
		}())
	}
	asked := map[uint32]bool{}
	for _, ask := range asks {
		for _, uid := range ask.UIDs {
			asked[uid] = true
		}
	}
	if len(asked) != items {
		t.Fatalf("two rounds asked about %d different mails of %d: the ones left out were not first", len(asked), items)
	}
}

// A mailbox with 60 marketing mails a day whose owner reads all of it every evening in the mail client: every mail
// gets its item, triage collapses it, the evening's round finds the newest 40 read on the page and the rest read off
// it. The open side stays at what is unread.
func TestInboxMailOpenItemsDoNotGrowWithTheMailbox(t *testing.T) {
	const page, perDay, days = 40, 60, 30
	h := newInboxHub(t)
	h.in.mail = &fakeMail{box: map[uint32]bool{}}
	a := assistantwire.InboxMailAccount{ID: "a1", Email: "me@example.com"}
	ctx := context.Background()
	start := time.Now().Add(-days * 24 * time.Hour)
	var box []assistantwire.InboxMailSummary // INBOX, oldest first
	round := func(now time.Time) {
		h.in.mu.Lock()
		for _, m := range box {
			h.in.mail.box[m.UID] = m.Seen
		}
		h.in.mu.Unlock()
		h.inbox.applyMail(a, slices.Clone(box[max(len(box)-page, 0):]), now)
		h.settleMailOffPage(ctx, a, now)
		h.inbox.mu.Lock()
		for _, it := range h.inbox.items {
			if it.open() && it.Tier == "" {
				it.Tier, it.TriagedAt = TierNoise, it.Last
			}
		}
		h.inbox.mu.Unlock()
	}
	uid := uint32(0)
	var now time.Time
	for day := range days {
		for i := range perDay {
			uid++
			now = start.Add(time.Duration(day)*24*time.Hour + time.Duration(i)*20*time.Minute)
			box = append(box, assistantwire.InboxMailSummary{Mailbox: "INBOX", UID: uid, Subject: fmt.Sprintf("Offer %d", uid),
				From: MailAddress{Name: "Shop", Email: "shop@example.com"}, Date: now.Format(time.RFC3339)})
			round(now)
		}
		for i := range box { // the evening: everything is read in the mail client
			box[i].Seen = true
		}
		now = now.Add(time.Hour)
		round(now)
	}
	h.inbox.mu.Lock()
	defer h.inbox.mu.Unlock()
	open := 0
	for _, it := range h.inbox.items {
		if it.open() {
			open++
		}
	}
	if open != 0 {
		t.Fatalf("after %d days of %d mails a day, every one read in the mail client the same evening, %d mail items are still open", days, perDay, open)
	}
}
