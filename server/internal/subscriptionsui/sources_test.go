package subscriptionsui

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/extension"
	"github.com/nwverse/persona/server/internal/i18n"
	"github.com/nwverse/persona/server/internal/subscriptions"
)

// fakeBuiltins are the manager's built-in sources as the pages see them: mail (every account always in the Inbox, its
// archive switch), GitHub, a Lark plugin with its own rules, and a Telegram plugin that is signed out.
type fakeBuiltins struct {
	list     []Builtin
	feeds    []string // "<id>/<inbox|kb>=<on>" as set
	rules    map[string]string
	mutes    []Mute
	unmuted  []string
	mutesErr error
	setErr   error
	migrated []string // "<id>=<on>" as switched (#6345)
}

func newBuiltins() *fakeBuiltins {
	yes, no := true, false
	return &fakeBuiltins{rules: map[string]string{}, list: []Builtin{
		{ID: "mail", Name: "Mail", Icon: "mail", Status: "ok", InboxAlways: true, Archive: &yes, InboxEvery: 180, ArchiveEvery: 360, Mutable: true,
			Accounts: []Account{{ID: "a1", Name: "me@example.com", Read: &Read{At: now.Add(-time.Minute), OK: at(-time.Minute)}},
				{ID: "a2", Name: "work@example.org", Read: &Read{At: now.Add(-2 * time.Minute), Error: "timeout", OK: at(-31 * time.Minute)}}}},
		{ID: "github", Name: "GitHub", Icon: "code", Status: "ok", Inbox: &yes, Archive: &no, InboxEvery: 120, ArchiveEvery: 600,
			Read: &Read{At: now.Add(-2 * time.Minute), OK: at(-2 * time.Minute)}, Mutable: true},
		{ID: "lark", Name: "Lark", Icon: "chat", Extension: "lark", Status: "ok", Inbox: &yes, InboxEvery: 120,
			Read: &Read{At: now.Add(-time.Minute), OK: at(-time.Minute)}, RulesOwn: true, Rules: "Mentions in project chats need me", Mutable: true},
		{ID: "telegram", Name: "Telegram", Icon: "chat", Extension: "telegram", Status: "not_signed_in", Inbox: &yes, InboxEvery: 120,
			RulesOwn: true, RulesLocked: true, Mutable: true},
	}, mutes: []Mute{{ID: "m1", Source: "mail", Tag: "sender", Name: "news@example.com", At: now.Add(-time.Hour)},
		{ID: "m2", Source: "github", Tag: "repo", Name: "example/site", At: now.Add(-2 * time.Hour)}}}
}

func (b *fakeBuiltins) List(context.Context) []Builtin { return b.list }

func (b *fakeBuiltins) SetMigrated(_ context.Context, id string, on bool, _ subscriptions.Actor) error {
	if b.setErr != nil {
		return b.setErr
	}
	b.migrated = append(b.migrated, id+"="+map[bool]string{true: "on", false: "off"}[on])
	for i := range b.list {
		if b.list[i].ID == id && b.list[i].Via != nil {
			b.list[i].Via.On, b.list[i].Via.Subscription = on, subscriptions.MigrationID(id)
		}
	}
	return nil
}

func (b *fakeBuiltins) SetFeed(_ context.Context, id string, inbox, on bool, _ subscriptions.Actor) error {
	if b.setErr != nil {
		return b.setErr
	}
	feed := "kb"
	if inbox {
		feed = "inbox"
	}
	b.feeds = append(b.feeds, id+"/"+feed+"="+map[bool]string{true: "on", false: "off"}[on])
	for i := range b.list {
		if b.list[i].ID == id {
			v := on
			if inbox {
				b.list[i].Inbox = &v
			} else {
				b.list[i].Archive = &v
			}
		}
	}
	return nil
}

func (b *fakeBuiltins) SetRules(_ context.Context, id, rules string, _ subscriptions.Actor) error {
	b.rules[id] = rules
	return nil
}

func (b *fakeBuiltins) Mutes(_ context.Context, source string) ([]Mute, error) {
	if b.mutesErr != nil {
		return nil, b.mutesErr
	}
	var out []Mute
	for _, m := range b.mutes {
		if source == "" || m.Source == source {
			out = append(out, m)
		}
	}
	return out, nil
}

func (b *fakeBuiltins) Unmute(_ context.Context, id string) error {
	b.unmuted = append(b.unmuted, id)
	return nil
}

func (b *fakeBuiltins) Native(service string) (string, string) {
	if service == "mail:example.com" {
		return "mail", "me@example.com"
	}
	return "", ""
}

// sourcesEnv is env with the built-in sources and the assistant's name.
func sourcesEnv(lang i18n.Lang, edit bool, b *fakeBuiltins) Env {
	e := env(lang, edit)
	e.Builtin, e.Assistant, e.Ask = b, "Claude", true
	return e
}

// withDuplicate gives Outlook the mail service Persona reads, so it and the mail source both say so.
func withDuplicate(f *fake) *fake {
	s := f.subs["sub_outlook"]
	s.Service = "mail:example.com"
	f.subs["sub_outlook"] = s
	return f
}

// Inbox Sources lists what needs attention first, then what Persona reads itself, the subscriptions, the
// recommended ones and the way to more; a possible duplicate is marked on both sides.
func TestInboxSourcesList(t *testing.T) {
	f, b := withDuplicate(newFake()), newBuiltins()
	v := draw(t, f, sourcesEnv(i18n.En, true, b), state{})
	if v.Title != "Inbox Sources" {
		t.Errorf("title: %q", v.Title)
	}
	if got := strings.Join(keys(section(v, "attention")), ","); got != "b.mail,b.telegram,s.sub_slack,s.sub_github" {
		t.Errorf("attention (an account that failed to read, a signed-out plugin, then subscriptions): %s", got)
	}
	if got := strings.Join(keys(section(v, "builtin")), ","); got != "b.github,b.lark" {
		t.Errorf("read by Persona: %s", got)
	}
	if r := item(v, "b.telegram"); r.Badge == nil || r.Badge.Text != "Not signed in" || !strings.Contains(r.Subtitle, "Not signed in") {
		t.Errorf("a signed-out source says so: %+v", r)
	}
	if r := item(v, "b.github"); !strings.Contains(r.Subtitle, "Inbox") || !strings.Contains(r.Subtitle, "every 2 minutes") ||
		!strings.Contains(r.Subtitle, "read 2 minutes ago") {
		t.Errorf("a built-in row says where, how often and when it read: %q", r.Subtitle)
	}
	if r := item(v, "s.sub_outlook"); r.Badge == nil || r.Badge.Text != "Possible duplicate" {
		t.Errorf("a subscription reading what Persona reads: %+v", r.Badge)
	}
	if got := strings.Join(keys(section(v, "recommended")), ","); got != "r.apps/calendar" {
		t.Errorf("recommended: %s", got)
	}
	if item(v, "more") == nil {
		t.Error("no way to more sources")
	}
	b.list[0].Accounts[1].Read.Error = "" // mail reads again: it leaves Needs attention and still says it may duplicate
	v = draw(t, f, sourcesEnv(i18n.En, true, b), state{})
	if r := item(v, "b.mail"); r == nil || r.Badge == nil || r.Badge.Text != "Possible duplicate" {
		t.Errorf("mail with a subscription on its service: %+v", r)
	}
	v = draw(t, f, sourcesEnv(i18n.En, true, b), state{Ext: "lark"})
	if item(v, "b.lark") == nil || item(v, "b.github") != nil || v.Title != "Subscriptions" {
		t.Errorf("one extension's list has its own built-in source only: %v %v", keys(section(v, "builtin")), v.Title)
	}
	if v := draw(t, f, env(i18n.En, true), state{}); section(v, "builtin") != nil || section(v, "on") == nil {
		t.Error("without built-in sources the list is the subscriptions' as before")
	}
}

// A built-in source's page has a subscription's sections: its connector's switches (asking before turning one on),
// how often (said, not chosen), each account's read, its rules and its mutes.
func TestBuiltinPage(t *testing.T) {
	f, b := withDuplicate(newFake()), newBuiltins()
	v := draw(t, f, sourcesEnv(i18n.En, true, b), state{P: pageBuiltin, C: "mail"})
	if r := item(v, "d.to_inbox"); r == nil || r.Control != nil || r.Value != "Always" {
		t.Errorf("mail's Inbox has no switch: %+v", r)
	}
	if r := item(v, "d.to_kb"); r == nil || r.Control == nil || !r.Control.On || r.Control.Action.Confirm != "" {
		t.Errorf("an on switch turns off without asking: %+v", r)
	}
	if r := item(v, "f.inbox"); r == nil || r.Value != "every 3 minutes" || r.Action != nil {
		t.Errorf("how often is said, not chosen: %+v", r)
	}
	if r := item(v, "st.a.a2"); r == nil || !strings.Contains(r.Subtitle, "took too long") || r.Value == "" {
		t.Errorf("an account that failed says why and when it last read: %+v", r)
	}
	if section(v, "trouble") == nil || item(v, "dup.sub_outlook") == nil {
		t.Error("the page starts with what is wrong and the subscription it may duplicate")
	}
	if r := item(v, "h.rules"); r == nil || r.Action != nil || r.Subtitle != "Uses your general Inbox rules" {
		t.Errorf("mail has no rules of its own: %+v", r)
	}
	if r := item(v, "h.mutes"); r == nil || r.Value != "1" {
		t.Errorf("mutes: %+v", r)
	}
	v = draw(t, f, sourcesEnv(i18n.ZhHans, true, b), state{P: pageBuiltin, C: "github"})
	kb := item(v, "d.to_kb")
	if kb.Control == nil || kb.Control.On || !strings.Contains(kb.Control.Action.Confirm, "信息处理") ||
		!strings.Contains(kb.Control.Action.Confirm, "每 10 分钟") {
		t.Errorf("an off switch says what turning it on does, how often and which model: %+v", kb.Control)
	}
	res := handle(t, f, sourcesEnv(i18n.En, true, b), state{P: pageBuiltin, C: "github"}, "builtin_dest", map[string]string{"to": "to_kb", "to_kb": "true"})
	if strings.Join(b.feeds, ",") != "github/kb=on" {
		t.Fatalf("the connector's own switch: %v", b.feeds)
	}
	if c := item(*res.View, "d.to_kb").Control; c == nil || !c.On {
		t.Error("the page drawn after does not show the switch on")
	}
	v = draw(t, f, sourcesEnv(i18n.En, true, b), state{P: pageBuiltin, C: "lark"})
	if r := item(v, "h.rules"); r.Subtitle != "Mentions in project chats need me" || r.Action == nil {
		t.Errorf("a plugin's own rules open their page: %+v", r)
	}
	res = handle(t, f, sourcesEnv(i18n.En, true, b), state{P: pageRules, C: "lark"}, "rules", map[string]string{"triage_rules": "Collapse chatter"})
	if b.rules["lark"] != "Collapse chatter" || !res.Pop {
		t.Errorf("rules saved: %v %+v", b.rules, res)
	}
	if r := item(draw(t, f, sourcesEnv(i18n.En, true, b), state{P: pageBuiltin, C: "telegram"}), "h.rules"); r.Action != nil {
		t.Error("rules set in manager.json are not changed here")
	}
	if v := draw(t, f, sourcesEnv(i18n.En, true, b), state{P: pageBuiltin, C: "gone"}); item(v, "gone") == nil {
		t.Error("a source that is gone")
	}
	b.setErr = errors.New("boom")
	res = handle(t, f, sourcesEnv(i18n.En, true, b), state{P: pageBuiltin, C: "github"}, "builtin_dest", map[string]string{"to": "to_inbox", "to_inbox": "false"})
	if res.Toast == "" || res.View == nil {
		t.Errorf("a switch that did not change says so on the page: %+v", res)
	}
}

// A source's mutes list each with Unmute, which asks first; the Inbox away is said, not an empty list.
func TestBuiltinMutes(t *testing.T) {
	f, b := newFake(), newBuiltins()
	v := draw(t, f, sourcesEnv(i18n.En, true, b), state{P: pageMutes, C: "mail"})
	m := item(v, "m.m1")
	if m == nil || m.Action == nil || m.Action.ID != "unmute" || !strings.Contains(m.Action.Confirm, "news@example.com") {
		t.Fatalf("a mute with Unmute: %+v", m)
	}
	res := handle(t, f, sourcesEnv(i18n.En, true, b), state{P: pageMutes, C: "mail"}, "unmute", map[string]string{"mute": "m1"})
	if strings.Join(b.unmuted, ",") != "m1" || res.Toast == "" {
		t.Errorf("unmuted: %v %+v", b.unmuted, res)
	}
	b.mutesErr = errors.New("away")
	if r := item(draw(t, f, sourcesEnv(i18n.En, true, b), state{P: pageBuiltin, C: "mail"}), "h.mutes"); r.Value != "Unavailable" {
		t.Errorf("mutes while the Inbox is away: %+v", r)
	}
}

// A recommended source turns on in one switch that asks first (each new item one model call); off does nothing.
func TestRecommendedTurnsOn(t *testing.T) {
	f, b := newFake(), newBuiltins()
	v := draw(t, f, sourcesEnv(i18n.En, true, b), state{})
	r := item(v, "r.apps/calendar")
	if r.Control == nil || r.Control.On || !strings.Contains(r.Control.Action.Confirm, "one call to the Information filtering model") {
		t.Fatalf("the switch asks first and says what it costs: %+v", r.Control)
	}
	if r.Action == nil || r.Action.Push == nil {
		t.Error("the row still opens the source's page")
	}
	handle(t, f, sourcesEnv(i18n.En, true, b), state{}, "recommend", map[string]string{"c": "apps", "s": "calendar", "r.apps/calendar": "false"})
	if len(f.created) != 0 {
		t.Fatal("turning the switch off made a subscription")
	}
	res := handle(t, f, sourcesEnv(i18n.En, true, b), state{}, "recommend", map[string]string{"c": "apps", "s": "calendar", "r.apps/calendar": "true"})
	if len(f.created) != 1 || f.created[0].Recipe != nil || !f.subs["sub_new"].ToInbox || f.subs["sub_new"].ToKB {
		t.Fatalf("created with the extension's recipe and the Inbox on: %+v %+v", f.created, f.subs["sub_new"])
	}
	if item(*res.View, "r.apps/calendar") != nil || res.Toast == "" {
		t.Error("the list after does not have it under Subscriptions")
	}
}

// Set Up with the assistant opens its main conversation with what the page is about; the button has its name.
func TestAskTheAssistant(t *testing.T) {
	f, b := newFake(), newBuiltins()
	v := draw(t, f, sourcesEnv(i18n.ZhHans, true, b), state{P: pageSource, ID: "sub_outlook"})
	if r := item(v, "x.ask"); r == nil || r.Title != "用 Claude 配置" || r.Subtitle == "" {
		t.Fatalf("named after the assistant: %+v", r)
	}
	e := sourcesEnv(i18n.ZhHans, true, b)
	e.Assistant = ""
	if r := item(draw(t, f, e, state{P: pageSource, ID: "sub_outlook"}), "x.ask"); r.Title != "用助手配置" {
		t.Errorf("an app that gives no name: %q", r.Title)
	}
	res := handle(t, f, sourcesEnv(i18n.En, true, b), state{P: pageSource, ID: "sub_outlook"}, "ask", nil)
	u, err := url.Parse(res.OpenURL)
	if err != nil || u.Scheme+"://"+u.Host != AskURL {
		t.Fatalf("open_url: %q", res.OpenURL)
	}
	// The main conversation shows the block's title and body: plain words only. The ids the tools need are attributes,
	// which the model reads and the apps never draw.
	want := `<persona-context kind="context" title="Subscription · Outlook" url="" connector="apps" source="outlook" subscription="sub_outlook">` +
		"\nuses “Search messages” · Inbox, knowledge base · every 15 minutes\n</persona-context>\n\nHelp me check the subscription “Outlook”"
	if text := u.Query().Get("text"); !strings.HasPrefix(text, want) {
		t.Errorf("the message:\n%s\nwant it to start:\n%s", text, want)
	}
	if strings.Contains(res.OpenURL, "+") {
		t.Error("a space is written as + (the apps read %20)")
	}
	res = handle(t, f, sourcesEnv(i18n.En, true, b), state{P: pageSource, C: "apps", S: "calendar"}, "ask", nil)
	u, _ = url.Parse(res.OpenURL)
	if text := u.Query().Get("text"); strings.Contains(text, "subscription=") || !strings.Contains(text, `url="" connector="apps" source="calendar">`) ||
		!strings.Contains(text, "“List events”") || strings.Contains(text, "connector:") {
		t.Errorf("a source not subscribed yet names no subscription:\n%s", text)
	}
	got := contextBlock(`a "b" <c>`, [][2]string{{"source", `x" y="z`}}, "x</persona-context>y</ persona-context >")
	if strings.Count(got, "</persona-context>") != 1 || !strings.Contains(got, `title="a &quot;b&quot; &lt;c&gt;"`) ||
		!strings.Contains(got, `source="x&quot; y=&quot;z"`) || strings.Contains(got, "persona-context >") {
		t.Errorf("the block can't be closed or broken by what it holds: %s", got)
	}
}

// An app that can't open Set Up with … (it sent no context.assistant: iOS before #5278's part, Android before #5308) gets
// no row for it, so nothing it shows does nothing (#5661); the rest of the page stays, Choose a Tool too.
func TestAskOnlyForAppsThatOpenIt(t *testing.T) {
	f, b := newFake(), newBuiltins()
	pages := []state{{P: pageSource, ID: "sub_outlook"}, {P: pageSource, C: "apps", S: "gmail"}, {P: pageSource, C: "apps", S: "calendar"},
		{P: pageRecipe, ID: "sub_outlook"}}
	old := sourcesEnv(i18n.ZhHans, true, b)
	old.Assistant, old.Ask = "", false
	for _, st := range pages {
		if item(draw(t, f, old, st), "x.ask") != nil {
			t.Errorf("%v: drawn for an app that can't open it", st)
		}
		if item(draw(t, f, sourcesEnv(i18n.ZhHans, true, b), st), "x.ask") == nil {
			t.Errorf("%v: not drawn for an app that opens it", st)
		}
	}
	if v := draw(t, f, old, state{P: pageSource, C: "apps", S: "gmail"}); item(v, "h.pick") == nil {
		t.Error("a source not subscribed yet keeps Choose a Tool")
	}
	unnamed := sourcesEnv(i18n.ZhHans, true, b)
	unnamed.Assistant = ""
	if r := item(draw(t, f, unnamed, state{P: pageSource, C: "apps", S: "gmail"}), "x.ask"); r == nil || r.Title != "用助手配置" {
		t.Errorf("an app that opens it but has no name: %+v", r)
	}
}

// The pages no longer draft with a model: no press for it anywhere.
func TestNoDrafting(t *testing.T) {
	f, b := newFake(), newBuiltins()
	for _, st := range []state{{P: pageSource, C: "apps", S: "gmail"}, {P: pageSource, C: "apps", S: "calendar"}, {P: pageRecipe, ID: "sub_outlook"}} {
		v := draw(t, f, sourcesEnv(i18n.En, true, b), st)
		raw, _ := json.Marshal(v)
		if strings.Contains(string(raw), `"draft"`) || strings.Contains(string(raw), `"regenerate"`) || item(v, "x.ask") == nil {
			t.Errorf("%v: %s", st, raw)
		}
	}
}

// The preview draws a dry run's items as Inbox rows, says what each could not read and what the recipe says that does
// nothing; the model tries them only when asked, after saying what it sends.
func TestPreview(t *testing.T) {
	f, b := newFake(), newBuiltins()
	f.dry = subscriptions.DryRun{OK: true, Count: 2, Items: []subscriptions.Preview{
		{UID: "1", New: true, Title: "Invoice", Sender: "billing@example.com", Summary: "Pay by the 20th", Time: ptr(now.Add(-time.Hour).Format(time.RFC3339)), DueDate: ptr("2026-11-20T00:00:00Z")},
		{UID: "2", Title: "Weekly news", Summary: "This week…", Problems: []subscriptions.FieldProblem{{Key: "fields.sender", Code: subscriptions.FieldEmpty},
			{Key: "fields.time", Code: subscriptions.FieldBadTime}}}},
		Warnings: []subscriptions.Warning{{Code: subscriptions.WarnUnknownKey, Key: "fields.author"}, {Code: subscriptions.WarnEmptyField, Key: "fields.link", Path: "$.webLink"}}}
	var asked []subscriptions.TriagePreview
	e := sourcesEnv(i18n.En, true, b)
	e.Triage = func(_ context.Context, tp subscriptions.TriagePreview) ([]Verdict, error) {
		asked = append(asked, tp)
		return []Verdict{{Tier: "noise", Summary: "A bill, nothing to do", Reason: "automated"}, {State: "paused", Why: "model out of quota"}}, nil
	}
	v := draw(t, f, e, state{P: pageSource, ID: "sub_outlook"})
	if section(v, "dry") != nil || item(v, "x.preview") == nil {
		t.Fatal("the preview pulls only when asked")
	}
	res := handle(t, f, e, state{P: pageSource, ID: "sub_outlook"}, "dry_run", nil)
	v = *res.View
	one, two := item(v, "dry.0"), item(v, "dry.1")
	if one == nil || one.Avatar == nil || one.Subtitle != "billing@example.com: Pay by the 20th" || one.Badge == nil || one.Badge.Text != "Needs You" {
		t.Errorf("an item with a due date as its Inbox row: %+v", one)
	}
	if two.Badge == nil || two.Badge.Text != "FYI" {
		t.Errorf("without a model an item goes to FYI by default: %+v", two.Badge)
	}
	if n := item(v, "dry.n.1"); n == nil || !strings.Contains(n.Text, "Sender: empty in this one") || !strings.Contains(n.Text, "Time couldn't be read as a time") {
		t.Errorf("what one item could not read: %+v", n)
	}
	if w := item(v, "w.0"); w == nil || !strings.Contains(w.Title, "“author” isn't a field the Inbox knows") {
		t.Errorf("an unknown field: %+v", w)
	}
	if w := item(v, "w.1"); w == nil || w.Title != "Link is empty in every item" || w.Subtitle != "From webLink" {
		t.Errorf("a field empty everywhere: %+v", w)
	}
	press := item(v, "x.triage")
	if press == nil || press.Action == nil || !strings.Contains(press.Action.Confirm, "Each item is one call") ||
		!strings.Contains(press.Action.Confirm, "Example Cloud") || len(asked) != 0 {
		t.Fatalf("trying the model asks first, naming it: %+v", press)
	}
	res = handle(t, f, e, state{P: pageSource, ID: "sub_outlook"}, "triage_preview", nil)
	v = *res.View
	if len(asked) != 1 || len(asked[0].Things) != 2 || asked[0].Source != "sub_outlook" || f.previewed[0].ID != "sub_outlook" {
		t.Fatalf("the model was handed: %+v", asked)
	}
	if r := item(v, "dry.0"); r.Badge.Text != "Collapsed" || r.Subtitle != "✦ A bill, nothing to do" {
		t.Errorf("the model's verdict on the row: %+v", r)
	}
	if n := item(v, "dry.n.1"); !strings.Contains(n.Text, "Model: model out of quota") {
		t.Errorf("an item the model could not sort says why: %+v", n)
	}
	if item(v, "x.triage") != nil || section(v, "dry").Footer == "" {
		t.Error("after trying, the rows say they are the model's")
	}
	e.Triage = func(context.Context, subscriptions.TriagePreview) ([]Verdict, error) { return nil, errors.New("down") }
	res = handle(t, f, e, state{P: pageSource, ID: "sub_outlook"}, "triage_preview", nil)
	if item(*res.View, "dry.model_err") == nil || item(*res.View, "dry.0").Badge.Text != "Needs You" {
		t.Error("a model that can't be asked leaves the rule's sections and says so")
	}
	e.Triage = nil
	res = handle(t, f, e, state{P: pageSource, ID: "sub_outlook"}, "dry_run", nil)
	if item(*res.View, "x.triage") != nil {
		t.Error("offered without a way to ask the model")
	}
}

// The new pages fit every app version in every language, with nothing unfilled.
func TestSourcesPagesFit(t *testing.T) {
	slot, _ := extension.Slot("extension_page")
	pages := []state{{}, {Ext: "lark"}, {P: pageMore}, {P: pageBuiltin, C: "mail"}, {P: pageBuiltin, C: "github"}, {P: pageBuiltin, C: "telegram"},
		{P: pageMutes, C: "mail"}, {P: pageMutes, C: "lark"}, {P: pageRules, C: "lark"}}
	for _, lang := range i18n.Langs {
		for _, edit := range []bool{true, false} {
			for _, st := range pages {
				for _, version := range []int{extension.UIVersion, 4} {
					e := sourcesEnv(lang, edit, newBuiltins())
					e.UIVersion = version
					v := draw(t, withDuplicate(newFake()), e, st)
					b, _ := json.Marshal(v)
					for _, ph := range []string{"{count}", "{time}", "{source}", "{noun}", "{every}", "{name}", "{when}", "{account}", "{problem}", "{field}", "{key}"} {
						if strings.Contains(string(b), ph) {
							t.Errorf("%s %v: %s unfilled", lang, st, ph)
						}
					}
					if strings.Contains(string(b), "subscriptions-pages.") {
						t.Errorf("%s %v: a key shows through", lang, st)
					}
					if _, err := extension.Fit(&v, slot, version, lang); err != nil {
						t.Errorf("%s %v v%d: %v", lang, st, version, err)
					}
				}
			}
		}
	}
}

// A subscription's page lists who is muted of it, as a built-in source's does (#5306): by sender, with the mutes page.
func TestSubscriptionMutes(t *testing.T) {
	f, b := newFake(), newBuiltins()
	b.mutes = append(b.mutes, Mute{ID: "m9", Source: "sub_outlook", Tag: "sender", Name: "news@example.com", At: now})
	r := item(draw(t, f, sourcesEnv(i18n.En, true, b), state{P: pageSource, ID: "sub_outlook"}), "h.mutes")
	if r == nil || r.Value != "1" || r.Action == nil || r.Action.Push == nil || !strings.Contains(r.Action.Push.State, `"c":"sub_outlook"`) {
		t.Fatalf("the Muted row: %+v", r)
	}
	if item(draw(t, f, sourcesEnv(i18n.En, true, b), state{P: pageMutes, C: "sub_outlook"}), "m.m9") == nil {
		t.Error("its mutes page lists it")
	}
	if item(draw(t, f, env(i18n.En, true), state{P: pageSource, ID: "sub_outlook"}), "h.mutes") != nil {
		t.Error("a Muted row without the Inbox to ask")
	}
}
