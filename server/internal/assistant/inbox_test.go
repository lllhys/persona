package assistant

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/assistantwire"
	"github.com/nwverse/persona/server/internal/connector"
)

func TestInboxMailReplyTargetConflict(t *testing.T) {
	h := newInboxHub(t)
	mux := h.routes()
	item := &InboxItem{ID: "mail-item", Mail: &InboxMail{AccountID: "account", Mailbox: "INBOX", UIDs: []uint32{7}, MessageID: "original@example.com", From: MailAddress{Email: "sender@example.com"}}}
	if err := h.inboxReady(context.Background()); err != nil {
		t.Fatal(err)
	}
	waitNeedsYouIdle(t, h.Service)
	h.inbox.items[item.ID] = item
	get := httptest.NewRecorder()
	mux.ServeHTTP(get, httptest.NewRequest(http.MethodGet, "/api/v1/inbox/mail-item", nil))
	var detail struct {
		ReplyTarget inboxMailTarget `json:"reply_target"`
	}
	if err := json.Unmarshal(get.Body.Bytes(), &detail); err != nil || detail.ReplyTarget.UID != 7 || detail.ReplyTarget.Version == "" {
		t.Fatalf("target: %s %v", get.Body, err)
	}
	version := detail.ReplyTarget.Version
	item.Mail.UIDs = append(item.Mail.UIDs, 8)
	for _, body := range []string{
		`{"text":"reply","expected_uid":7}`,
		`{"text":"reply","expected_version":"` + version + `"}`,
	} {
		response := httptest.NewRecorder()
		mux.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/v1/inbox/mail-item/reply", strings.NewReader(body)))
		if response.Code != 409 || !strings.Contains(response.Body.String(), `"code":"mail_reply_target_changed"`) || !strings.Contains(response.Body.String(), `"delivered":"none"`) || item.DoneAt != nil {
			t.Fatalf("conflict: %d %s", response.Code, response.Body)
		}
	}
	// Changing the recipient without changing the UID must invalidate the version too.
	item.Mail.UIDs = []uint32{7}
	item.Mail.From.Email = "other@example.com"
	if mailTarget(*item).Version == version {
		t.Fatal("recipient change retained version")
	}
	h.in.mail = &fakeMail{} // the hub's mail sends the reply (inbox.reply_mail)
	current := mailTarget(*item)
	body, _ := json.Marshal(map[string]any{"text": "reply", "expected_uid": current.UID, "expected_version": current.Version})
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/v1/inbox/mail-item/reply", strings.NewReader(string(body))))
	if response.Code != http.StatusOK || len(h.in.mail.sent) != 1 || item.DoneBy != "replied" {
		t.Fatalf("confirmed reply: %d %s, sends=%d", response.Code, response.Body, len(h.in.mail.sent))
	}
	if s := h.in.mail.sent[0]; s["account_id"] != "account" || s["uid"] != float64(7) || s["mailbox"] != "INBOX" || s["text"] != "reply" {
		t.Fatalf("the reply asked of the hub: %v", s)
	}
	// Legacy callers continue to send without target validation.
	response = httptest.NewRecorder()
	mux.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/v1/inbox/mail-item/reply", strings.NewReader(`{"text":"legacy"}`)))
	if response.Code != http.StatusOK || len(h.in.mail.sent) != 2 {
		t.Fatalf("legacy reply: %d %s", response.Code, response.Body)
	}
}

func newInbox() *inboxBook {
	b := &inboxBook{}
	b.init()
	return b
}

func TestInboxMailThreads(t *testing.T) {
	if mailThread("Re: 周报") != mailThread("周报") || mailThread("回复：[team] Fwd: 周报") != mailThread("周报") {
		t.Fatal("reply prefixes should join the thread")
	}
	if mailThread("周报") == mailThread("月报") {
		t.Fatal("different subjects")
	}
	now := time.Date(2026, 10, 27, 10, 0, 0, 0, time.UTC)
	date := func(d time.Duration) string { return now.Add(d).Format(time.RFC3339) }
	a := assistantwire.InboxMailAccount{ID: "a1", Email: "me@example.com"}
	msgs := []assistantwire.InboxMailSummary{
		{UID: 3, Subject: "Re: 周报", From: MailAddress{Name: "李四", Email: "li@x"}, To: []MailAddress{{Email: "me@example.com"}}, Date: date(-time.Hour)},
		{UID: 2, Subject: "周报", From: MailAddress{Email: "wang@x"}, Date: date(-2 * time.Hour)},
		{UID: 1, Subject: "已读的", Seen: true, Date: date(-3 * time.Hour)},
	}
	b := newInbox()
	ch := b.applyMail(a, msgs, now)
	if len(ch) != 1 {
		t.Fatalf("items = %d", len(ch))
	}
	it := ch[0]
	if it.Count != 2 || it.Sender != "李四" || !it.Mail.Direct || it.Last != "3" || len(it.Mail.UIDs) != 2 || it.AccountName != "me@example.com" {
		t.Fatalf("item = %+v %+v", it, it.Mail)
	}
	if ch := b.applyMail(a, msgs, now); len(ch) != 0 {
		t.Fatal("unchanged mail changed")
	}
	// Read in another client: an FYI is done, something that needs me stays.
	it.Tier = TierFYI
	msgs[0].Seen, msgs[1].Seen = true, true
	if ch := b.applyMail(a, msgs, now); len(ch) != 1 || it.open() || it.DoneBy != "read" {
		t.Fatalf("read elsewhere: %+v", it)
	}
}

func TestInboxMailReadKeepsConclusionUntilGotIt(t *testing.T) {
	now := time.Date(2026, 11, 12, 10, 0, 0, 0, time.UTC)
	a := assistantwire.InboxMailAccount{ID: "a1", Email: "me@example.com"}
	msgs := []assistantwire.InboxMailSummary{{UID: 88, Subject: "周报", Date: now.Add(-time.Hour).Format(time.RFC3339)}}
	b := newInbox()
	it := b.applyMail(a, msgs, now)[0]
	it.Tier = TierFYI
	it.Suggestion = &InboxSuggestion{Version: 1, Kind: SuggestConclusion, Text: "只读结论", At: now}

	msgs[0].Seen = true // opening the embedded body marks it seen; the next collection must not file the conclusion
	changed := b.applyMail(a, msgs, now)
	if len(changed) != 1 || !it.open() || !it.Read || it.DoneBy != "" || currentSuggestion(it) == nil {
		t.Fatalf("the conclusion was filed before Got it: %+v", it)
	}
}

// A round's mail: new mail becomes items, and the new ones' full text is read (once) into their preview and context.
func TestInboxMailRoundReadsTheText(t *testing.T) {
	h := newInboxHub(t)
	now := time.Now().UTC()
	h.in.mail = &fakeMail{
		accounts: []assistantwire.InboxMailAccount{{ID: "a1", Email: "me@example.com", Messages: []assistantwire.InboxMailSummary{
			{Mailbox: "INBOX", UID: 9, Subject: "示例周报", From: MailAddress{Name: "李四", Email: "li@example.com"},
				To: []MailAddress{{Email: "me@example.com"}}, Date: now.Add(-time.Hour).Format(time.RFC3339), Snippet: "<style>…"}}}},
		bodies: map[uint32]assistantwire.InboxMailMessage{9: {From: MailAddress{Name: "李四", Email: "li@example.com"},
			To: []MailAddress{{Email: "me@example.com"}}, Subject: "示例周报", Text: "这周做完了示例的上线"}},
	}
	h.collectInbox(context.Background(), roundFull)
	list := h.inboxList(false)
	if len(list) != 1 || list[0].Preview != "这周做完了示例的上线" || list[0].Source != "mail" || !list[0].Mail.Direct {
		t.Fatalf("the list: %+v", list)
	}
	h.inbox.mu.Lock()
	it := h.inbox.items[list[0].ID]
	bodyOf, context_ := it.BodyOf, it.Context
	h.inbox.mu.Unlock()
	if bodyOf != "9" || !strings.Contains(context_, "这周做完了示例的上线") || !strings.Contains(context_, "邮箱 me@example.com") {
		t.Fatalf("read into the item: body_of %q, context %q", bodyOf, context_)
	}
	h.collectInbox(context.Background(), roundFull)
	if h.in.mail.reads != 1 {
		t.Fatalf("the text was read %d times", h.in.mail.reads)
	}
	// A poll round does not read mail: the item stays as it is.
	h.collectInbox(context.Background(), roundPoll)
	if n := len(h.inboxList(false)); n != 1 {
		t.Fatalf("%d items after a poll", n)
	}
}

func TestInboxRuleTier(t *testing.T) {
	cases := []struct {
		it   InboxItem
		want string
	}{
		{InboxItem{Kind: KindDM}, TierAction},
		{InboxItem{Kind: KindMention}, TierAction},
		{InboxItem{Kind: KindMention, Lark: &InboxLark{AtAll: true}}, TierFYI},                // only @all: an announcement
		{InboxItem{Kind: KindMention, Lark: &InboxLark{AtAll: true, AtMe: true}}, TierAction}, // @me as well
		{InboxItem{Kind: KindGroup}, TierFYI},
		{InboxItem{Kind: KindBot}, TierNoise},
		{InboxItem{Kind: KindMail, Mail: &InboxMail{Direct: true}}, TierFYI},
		{InboxItem{Kind: KindMail, Mail: &InboxMail{}}, TierNoise},
	}
	b := newInbox()
	for i, c := range cases {
		if got := b.ruleTier(&c.it); got != c.want {
			t.Errorf("%d: %q, want %q", i, got, c.want)
		}
	}
}

// The inbox is kept across a restart of the manager: what it stored comes back, and the mutes with it; a database
// that does not answer leaves it to be read again (not an empty inbox for good), and until then the inbox takes and
// writes nothing: its routes answer 503 inbox_unavailable, a round is refused (the hub hands it again), the mutes are
// neither listed nor set, and what is stored is not overwritten.
func TestInboxKeptAcrossRestarts(t *testing.T) {
	h := newInboxHub(t)
	ctx := context.Background()
	h.inboxReady(ctx)
	it := &InboxItem{ID: "example:1", Source: "example", Kind: "review_requested", Title: "示例", Last: "a", UpdatedAt: time.Now(),
		Connector: &InboxConnector{Thread: "t1"}}
	h.inbox.mu.Lock()
	h.inbox.items[it.ID] = it
	h.inbox.mu.Unlock()
	h.saveInbox(ctx, []*InboxItem{it})
	if _, _, err := h.addMute(ctx, InboxMute{Source: "example", Tag: "repo", Value: "x", Name: "x"}); err != nil {
		t.Fatal(err)
	}

	h.st.fail = errMuteStoreDown
	again := New(Options{Hub: h.f, Store: func() Store { return h.st }, Logf: testLogf(t)})
	h.in.svc = again
	if n := len(again.inboxList(true)); n != 0 {
		t.Fatalf("%d items before it was read", n)
	}
	if err := again.inboxReady(ctx); !errors.Is(err, errInboxUnavailable) || again.inbox.loaded.Load() || again.inboxReport(ctx) != nil {
		t.Fatalf("a failed read counts as read: %v", err)
	}
	stored := string(h.st.rows["example:1"])
	round := assistantwire.InboxRound{Round: roundPoll, Sources: []assistantwire.InboxSource{{ID: "example", Name: "Example",
		Things: []connector.InboxThing{thing("b", time.Now())}}}}
	if err := again.InboxCollected(ctx, round); !errors.Is(err, errInboxUnavailable) || len(again.inboxList(true)) != 0 {
		t.Fatalf("a round taken before the inbox was read: %v", err)
	}
	for _, c := range []struct{ method, path, body string }{{"GET", "/api/v1/inbox", ""}, {"GET", "/api/v1/inbox/mutes", ""},
		{"POST", "/api/v1/inbox/mutes", `{"item_id":"example:1","tag":"repo"}`}, {"POST", "/api/v1/inbox/example:1/done", `{"done":true}`},
		{"POST", "/api/v1/inbox/done", `{"done":true,"items":[{"id":"example:1"}]}`}} {
		if code, out := inboxCall(t, again, c.method, c.path, c.body); code != http.StatusServiceUnavailable || out["code"] != assistantwire.CodeInboxUnavailable {
			t.Fatalf("%s %s before the inbox was read: %d %v", c.method, c.path, code, out)
		}
	}
	if _, err := again.InboxRepoMutes(ctx, assistantwire.InboxRepoMutes{Source: "github", Set: true, Repos: []string{}}); !errors.Is(err, errInboxUnavailable) {
		t.Fatalf("the mutes set before they were read: %v", err)
	}
	again.saveInbox(ctx, []*InboxItem{{ID: "example:1", Title: "覆盖"}})
	if string(h.st.rows["example:1"]) != stored {
		t.Fatal("what is stored was overwritten before it was read")
	}
	h.st.fail = nil
	again.inboxReady(ctx)
	if list := again.inboxList(true); len(list) != 1 || list[0].ID != "example:1" || len(again.listedMutes()) != 1 {
		t.Fatalf("after the restart: %+v, mutes %+v", list, again.listedMutes())
	}
}

var errMuteStoreDown = &storeDown{}

type storeDown struct{}

func (*storeDown) Error() string { return "the database is down" }

// A source's data deleted (inbox.forget): its items go, from memory and the database; the others stay. A source read
// with nothing (switched off for the inbox) keeps what it had.
func TestInboxForgetsASource(t *testing.T) {
	h := newInboxHub(t)
	ctx := context.Background()
	c := &inboxConnector{things: []connector.InboxThing{thing("a", time.Now())}}
	h.addConnector(c, false)
	h.in.mail = &fakeMail{accounts: []assistantwire.InboxMailAccount{{ID: "a1", Email: "me@example.com", Messages: []assistantwire.InboxMailSummary{
		{Mailbox: "INBOX", UID: 3, Subject: "示例", From: MailAddress{Email: "x@example.com"}, Date: time.Now().UTC().Format(time.RFC3339)}}}},
		bodies: map[uint32]assistantwire.InboxMailMessage{3: {Text: "示例正文"}}}
	h.collectInbox(ctx, roundFull)
	if n := len(h.inboxList(true)); n != 2 {
		t.Fatalf("%d items", n)
	}
	h.in.off["example"] = true
	h.collectInbox(ctx, roundPoll)
	if n := len(h.inboxList(true)); n != 2 {
		t.Fatalf("a source read with nothing lost its items: %d left", n)
	}
	h.inbox.triaging.Lock() // the mail's triage pass runs in the background, writing the store and telling the apps: wait for it (#4236)
	h.inbox.triaging.Unlock()
	before := len(h.in.changes())
	if n, err := h.InboxForget(ctx, "example"); err != nil || n != 1 {
		t.Fatalf("forget: %d %v", n, err)
	}
	if list := h.inboxList(true); len(list) != 1 || list[0].Source != "mail" || len(h.st.rows) != 1 || len(h.in.changes()) != before+1 {
		t.Fatalf("after forgetting: %+v, %d rows stored", list, len(h.st.rows))
	}
	if n, _ := h.InboxForget(ctx, "example"); n != 0 {
		t.Fatalf("forgot %d again", n)
	}
}

// A round older than one applied of the hub's run is dropped (it was held up on the way): it would put back what a
// newer round moved on; a new run of the hub counts afresh.
func TestInboxDropsAStaleRound(t *testing.T) {
	h := newInboxHub(t)
	ctx := context.Background()
	round := func(epoch string, seq uint64, last string) assistantwire.InboxRound {
		return assistantwire.InboxRound{Epoch: epoch, Seq: seq, Round: roundPoll, Names: map[string]string{"example": "Example"},
			Sources: []assistantwire.InboxSource{{ID: "example", Name: "Example", Things: []connector.InboxThing{thing(last, time.Now())}}}}
	}
	last := func() string {
		h.inbox.mu.Lock()
		defer h.inbox.mu.Unlock()
		for _, it := range h.inbox.items {
			if it.Source == "example" {
				return it.Last
			}
		}
		return ""
	}
	for _, c := range []struct {
		epoch string
		seq   uint64
		last  string
		want  string
	}{{"e1", 2, "b", "b"}, {"e1", 1, "a", "b"}, {"e1", 2, "a", "b"}, {"e1", 3, "c", "c"}, {"e2", 1, "d", "d"}} {
		if err := h.InboxCollected(ctx, round(c.epoch, c.seq, c.last)); err != nil {
			t.Fatal(err)
		}
		if got := last(); got != c.want {
			t.Fatalf("after round %s/%d: the item is at %q, want %q", c.epoch, c.seq, got, c.want)
		}
	}
}

// From the manager's start on, the plugins' connectors are known from the hub's settings (before a round lists them):
// their items get no fallback tier (only a model sorts them), and neither does an item of a source not known yet; a
// built-in connector a round read gets its fallback.
func TestInboxKnowsThePluginsFromTheStart(t *testing.T) {
	h := newInboxHub(t)
	ctx := context.Background()
	h.setConfig(func(c *assistantwire.InboxConfig) { c.Extensions = []string{"telegram"} })
	if err := h.inboxReady(ctx); err != nil {
		t.Fatal(err)
	}
	b := &h.inbox
	for _, source := range []string{"telegram", "later"} {
		if got := b.ruleTier(&InboxItem{Source: source, Connector: &InboxConnector{Thread: "t"}}); got != "" {
			t.Fatalf("%s's item without a model: %q", source, got)
		}
	}
	h.addConnector(&inboxConnector{}, false)
	h.collectInbox(ctx, roundPoll)
	if got := b.ruleTier(&InboxItem{Source: "example", Connector: &InboxConnector{Thread: "t"}}); got != TierNoise {
		t.Fatalf("a built-in connector's item: %q", got)
	}
}

// A reply whose answer was cut off after it went out (the link to the hub dropped) is in doubt: 502
// assistant_in_doubt with delivered unknown, the item not filed. Asked again it carries the same send id, which the
// hub sent already: nothing goes twice, and the item is filed as replied.
func TestInboxReplyInDoubtIsSentOnce(t *testing.T) {
	h := newInboxHub(t)
	mux := h.routes()
	if err := h.inboxReady(context.Background()); err != nil {
		t.Fatal(err)
	}
	item := &InboxItem{ID: "mail-item", Source: "mail", Kind: KindMail, Last: "7", Mail: &InboxMail{AccountID: "account", Mailbox: "INBOX", UIDs: []uint32{7}}}
	h.inbox.mu.Lock()
	h.inbox.items[item.ID] = item
	h.inbox.mu.Unlock()
	h.in.mail = &fakeMail{}
	h.in.inDoubt = true
	code, out := inboxCall(t, mux, "POST", "/api/v1/inbox/mail-item/reply", `{"text":"收到"}`)
	if code != http.StatusBadGateway || out["code"] != assistantwire.CodeAssistantInDoubt || out["delivered"] != "unknown" || item.DoneAt != nil ||
		len(h.in.mail.sent) != 1 {
		t.Fatalf("a reply cut off: %d %v, %d sent", code, out, len(h.in.mail.sent))
	}
	code, out = inboxCall(t, mux, "POST", "/api/v1/inbox/mail-item/reply", `{"text":"收到"}`)
	if code != http.StatusOK || out["done_by"] != "replied" || len(h.in.mail.sent) != 1 {
		t.Fatalf("the same reply asked again: %d %v, %d sent", code, out, len(h.in.mail.sent))
	}
	// Derived: the item, the words and the moves (not the message answered: a retry after a new one came in is a
	// retry, #1828); the app's own id when it gave one.
	if replySendID("i", "收到", "", nil) != replySendID("i", "收到", "", nil) || replySendID("i", "收到", "", nil) == replySendID("i", "又一条", "", nil) {
		t.Fatal("the derived send id")
	}
	if a, b := replySendID("i", "收到", "00000000-0000-4000-8000-000000000001", nil), replySendID("i", "又一条", "00000000-0000-4000-8000-000000000001", nil); a != b {
		t.Fatal("the app's own send id was not taken")
	}
}

// The reply box's AI draft with nothing the hub can call for it: a coded 503 the apps can explain, saying where to set a
// model up, not a bare 502 whose text never reaches the screen. With a model the draft comes back; an unknown item is 404.
func TestInboxDraftRouteWithoutAModel(t *testing.T) {
	h := agentHub(t)
	it := actionItem("lark:f1:11", time.Now())
	h.inbox.mu.Lock()
	h.inbox.items[it.ID] = it
	h.inbox.mu.Unlock()
	code, out := inboxCall(t, h.routes(), "POST", "/api/v1/inbox/lark:f1:11/draft", `{}`)
	if msg, _ := out["error"].(string); code != http.StatusServiceUnavailable || out["code"] != assistantwire.CodeModelUnavailable ||
		!strings.Contains(msg, "设置 › 模型供应商") {
		t.Fatalf("a draft with no model: %d %v", code, out)
	}
	h.in.mu.Lock()
	h.in.models[purposeDraft] = fakeModel(t, "好的，周四给")
	h.in.mu.Unlock()
	if code, out := inboxCall(t, h.routes(), "POST", "/api/v1/inbox/lark:f1:11/draft", `{}`); code != http.StatusOK || out["text"] != "好的，周四给" {
		t.Fatalf("a draft: %d %v", code, out)
	}
	if code, _ := inboxCall(t, h.routes(), "POST", "/api/v1/inbox/lark:f1:99/draft", `{}`); code != http.StatusNotFound {
		t.Fatalf("an unknown item: %d", code)
	}
}
