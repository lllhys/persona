package assistant

import (
	"context"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/assistantwire"
)

// Mail already read when the inbox first sees it (read in another client, or by a forwarding rule, before a round
// came) and an item whose rule tier stands in for a model that failed (#8298). No Postgres.

// seenMail is one mail on the fake account's page, sent ago.
func seenMail(uid uint32, subject string, seen bool, ago time.Duration, to string) assistantwire.InboxMailSummary {
	return assistantwire.InboxMailSummary{Mailbox: "INBOX", UID: uid, Subject: subject, Seen: seen,
		From: MailAddress{Name: "Example Sender", Email: "sender@example.com"}, To: []MailAddress{{Email: to}},
		Date: time.Now().Add(-ago).UTC().Format(time.RFC3339), Snippet: subject}
}

// addMail puts mail on top of the fake account's page.
func (h *inboxHub) addMail(msgs ...assistantwire.InboxMailSummary) {
	h.in.mu.Lock()
	defer h.in.mu.Unlock()
	acc := &h.in.mail.accounts[0]
	acc.Messages = append(msgs, acc.Messages...)
}

// mailItems are the account's items by their newest UID.
func (h *inboxHub) mailItems() map[uint32]*InboxItem {
	h.inbox.mu.Lock()
	defer h.inbox.mu.Unlock()
	out := map[uint32]*InboxItem{}
	for _, it := range h.inbox.items {
		if it.Mail != nil && len(it.Mail.UIDs) > 0 {
			out[it.Mail.UIDs[len(it.Mail.UIDs)-1]] = it
		}
	}
	return out
}

// needsYouAndUnread counts what the apps' badges and the hub's Needs you push count: open items in "Needs you", and
// open unread items.
func (h *inboxHub) needsYouAndUnread() (needsYou, unread int) {
	h.inbox.mu.Lock()
	defer h.inbox.mu.Unlock()
	for _, it := range h.inbox.items {
		if it.status() == StatusAction {
			needsYou++
		}
		if it.open() && !it.Read {
			unread++
		}
	}
	return needsYou, unread
}

// Read mail that arrives after the account's first round becomes an item read and done: listed in Done and stored,
// with no unread, no "Needs you" and no model asked. The read mail the first round found stays out (no backfill of
// the window when an account is turned on), and so does read mail past the window.
func TestInboxMailReadOnArrivalIsFiled(t *testing.T) {
	model, client := newTriageModel(t, "ok", `{"tier":"action","summary":"示例","reason":"示例"}`)
	h, st := admitHub(t, client)
	h.in.mail = &fakeMail{accounts: []assistantwire.InboxMailAccount{{ID: "a1", Email: "me@example.com", Messages: []assistantwire.InboxMailSummary{
		seenMail(2, "Unread before", false, time.Hour, "me@example.com"),
		seenMail(1, "Read before", true, 2*time.Hour, "me@example.com"),
	}}}}
	h.collectInbox(context.Background(), roundFull)
	waitTriage(h)
	items := h.mailItems()
	if len(items) != 1 || items[2] == nil {
		t.Fatalf("first round: items for UIDs %v, want only the unread mail's", keysOf(items))
	}
	var kept map[string]uint32
	if ok, err := st.Setting(context.Background(), inboxMailBaselinesKey, &kept); !ok || err != nil || kept["a1"] != 2 {
		t.Fatalf("baseline stored: %v %v %v", kept, ok, err)
	}
	asked := len(model.asked())
	needsYou, unread := h.needsYouAndUnread()

	// A forwarded verification code, read by a rule before the round, and an old read mail that turned up again.
	h.addMail(seenMail(4, "Your verification code", true, time.Minute, "someone-else@example.com"),
		seenMail(3, "Old newsletter", true, 8*24*time.Hour, "me@example.com"))
	h.collectInbox(context.Background(), roundFull)
	waitTriage(h)
	items = h.mailItems()
	it := items[4]
	if it == nil || items[3] != nil || len(items) != 2 {
		t.Fatalf("second round: items for UIDs %v, want the read code's added", keysOf(items))
	}
	h.inbox.mu.Lock()
	open, read, by, tier, status := it.open(), it.Read, it.DoneBy, it.Tier, it.status()
	h.inbox.mu.Unlock()
	if open || !read || by != "read" || tier != TierNoise || status == StatusAction || !st.saved(it.ID) {
		t.Fatalf("the read code: open %v read %v done_by %q tier %q status %q saved %v", open, read, by, tier, status, st.saved(it.ID))
	}
	if n, u := h.needsYouAndUnread(); n != needsYou || u != unread {
		t.Fatalf("Needs you %d → %d, unread %d → %d: the read code counted", needsYou, n, unread, u)
	}
	if n := len(model.asked()); n != asked {
		t.Fatalf("%d model call(s) about mail filed on arrival", n-asked)
	}
	if kinds := h.chainKinds(it); !hasKind(kinds, ChainReceived) || !hasKind(kinds, ChainDone) {
		t.Fatalf("chain %v", kinds)
	}

	// The next round finds the same mail: nothing new.
	h.collectInbox(context.Background(), roundFull)
	if items := h.mailItems(); len(items) != 2 {
		t.Fatalf("third round: items for UIDs %v", keysOf(items))
	}
}

// A mailbox renumbered (its newest UID under the baseline) starts a new baseline instead of filing what is there.
func TestInboxMailBaselineRenumbered(t *testing.T) {
	h := newInboxHub(t)
	h.in.mail = &fakeMail{accounts: []assistantwire.InboxMailAccount{{ID: "a1", Email: "me@example.com", Messages: []assistantwire.InboxMailSummary{
		seenMail(500, "Before", true, time.Hour, "me@example.com"),
	}}}}
	h.collectInbox(context.Background(), roundFull)
	h.in.mu.Lock()
	h.in.mail.accounts[0].Messages = []assistantwire.InboxMailSummary{seenMail(7, "After", true, time.Minute, "me@example.com")}
	h.in.mu.Unlock()
	h.collectInbox(context.Background(), roundFull)
	if items := h.mailItems(); len(items) != 0 {
		t.Fatalf("after renumbering: items for UIDs %v", keysOf(items))
	}
	h.addMail(seenMail(8, "Next", true, 0, "me@example.com"))
	h.collectInbox(context.Background(), roundFull)
	if items := h.mailItems(); len(items) != 1 || items[8] == nil {
		t.Fatalf("after the new baseline: items for UIDs %v", keysOf(items))
	}
}

// A model that answers an error (here 404, a model that was removed) leaves the item in its rule tier with TriagedAt
// behind; the passes while it keeps failing don't ask again before the backoff, and once it answers the item gets
// the model's tier.
func TestInboxRuleTierIsRetriedAfterAModelError(t *testing.T) {
	model, client := newTriageModel(t, "gone", `{"tier":"action","summary":"示例","reason":"示例"}`)
	h, _ := admitHub(t, client)
	h.in.mail = &fakeMail{accounts: []assistantwire.InboxMailAccount{{ID: "a1", Email: "me@example.com", Messages: []assistantwire.InboxMailSummary{
		seenMail(1, "Invitation to App Store Connect", false, time.Minute, "someone-else@example.com"),
	}}}}
	h.collectInbox(context.Background(), roundFull)
	waitTriage(h)
	it := h.mailItems()[1]
	if it == nil {
		t.Fatal("no item for the mail")
	}
	state := func() (tier, reason string, triaged bool, ruleOf string) {
		h.inbox.mu.Lock()
		defer h.inbox.mu.Unlock()
		return it.Tier, it.Reason, it.TriagedAt == it.Last, it.RuleOf
	}
	if tier, reason, triaged, ruleOf := state(); tier != TierNoise || reason != "按规则" || triaged || ruleOf != "1" || len(model.asked()) != 1 {
		t.Fatalf("after the 404: tier %q reason %q triaged %v rule_of %q, %d call(s)", tier, reason, triaged, ruleOf, len(model.asked()))
	}

	// Still failing: the next rounds don't ask about it before the backoff is over.
	for range 3 {
		h.collectInbox(context.Background(), roundFull)
		waitTriage(h)
	}
	if n := len(model.asked()); n != 1 {
		t.Fatalf("%d call(s) during the backoff", n)
	}
	// The backoff over and still failing: asked once, then the wait doubles.
	h.inbox.mu.Lock()
	h.inbox.ruledRetryAt = time.Now().Add(-time.Second)
	h.inbox.mu.Unlock()
	h.collectInbox(context.Background(), roundFull)
	waitTriage(h)
	h.collectInbox(context.Background(), roundFull)
	waitTriage(h)
	h.inbox.mu.Lock()
	wait, fails := time.Until(h.inbox.ruledRetryAt), h.inbox.ruledFails
	h.inbox.mu.Unlock()
	if n := len(model.asked()); n != 2 || wait < ruledBackoffMin-time.Minute || fails != 1 {
		t.Fatalf("after the backoff: %d call(s), next retry in %v, %d failed pass(es)", n, wait, fails)
	}
	if tier, _, triaged, ruleOf := state(); tier != TierNoise || triaged || ruleOf != "1" {
		t.Fatalf("after a failed retry: tier %q triaged %v rule_of %q", tier, triaged, ruleOf)
	}

	// The model is back: the next retry sorts it by the model.
	model.mode.Store("ok")
	h.inbox.mu.Lock()
	h.inbox.ruledRetryAt = time.Now().Add(-time.Second)
	h.inbox.mu.Unlock()
	h.collectInbox(context.Background(), roundFull)
	waitTriage(h)
	if tier, reason, triaged, ruleOf := state(); tier != TierAction || reason != "示例" || !triaged || ruleOf != "" || len(model.asked()) != 3 {
		t.Fatalf("after the model is back: tier %q reason %q triaged %v rule_of %q, %d call(s)", tier, reason, triaged, ruleOf, len(model.asked()))
	}
	h.inbox.mu.Lock()
	fails, at := h.inbox.ruledFails, h.inbox.ruledRetryAt
	h.inbox.mu.Unlock()
	if fails != 0 || !at.IsZero() {
		t.Fatalf("backoff not reset by the answer: %d, %v", fails, at)
	}
}

func keysOf(m map[uint32]*InboxItem) []uint32 {
	var out []uint32
	for k := range m {
		out = append(out, k)
	}
	return out
}
