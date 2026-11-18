package assistant

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/assistantwire"
	"github.com/nwverse/persona/server/internal/db"
	"github.com/nwverse/persona/server/internal/prompts"
	"github.com/nwverse/persona/server/internal/proto"
)

// callStore is the fleet store with urgent calls kept in memory, versioned as db.UpdateUrgentCall does.
type callStore struct {
	*fleetMem
	cmu   sync.Mutex
	calls map[string]db.UrgentCall
}

func (m *callStore) InsertUrgentCall(_ context.Context, c db.UrgentCall) error {
	m.cmu.Lock()
	defer m.cmu.Unlock()
	m.calls[c.ID] = c
	return nil
}

func (m *callStore) UrgentCall(_ context.Context, id string) (db.UrgentCall, bool, error) {
	m.cmu.Lock()
	defer m.cmu.Unlock()
	c, ok := m.calls[id]
	return c, ok, nil
}

func (m *callStore) UrgentCallsSince(_ context.Context, since time.Time) ([]db.UrgentCall, error) {
	m.cmu.Lock()
	defer m.cmu.Unlock()
	var out []db.UrgentCall
	for _, c := range m.calls {
		if !c.CreatedAt.Before(since) {
			out = append(out, c)
		}
	}
	slices.SortFunc(out, func(a, b db.UrgentCall) int { return a.CreatedAt.Compare(b.CreatedAt) })
	return out, nil
}

func (m *callStore) UrgentCallsDue(_ context.Context, now time.Time) ([]db.UrgentCall, time.Time, error) {
	m.cmu.Lock()
	defer m.cmu.Unlock()
	var due []db.UrgentCall
	var next time.Time
	for _, c := range m.calls {
		switch {
		case c.NextAt == nil:
		case !c.NextAt.After(now):
			due = append(due, c)
		case next.IsZero() || c.NextAt.Before(next):
			next = *c.NextAt
		}
	}
	return due, next, nil
}

func (m *callStore) UpdateUrgentCall(_ context.Context, c db.UrgentCall) (bool, error) {
	m.cmu.Lock()
	defer m.cmu.Unlock()
	cur, ok := m.calls[c.ID]
	if !ok || cur.Version != c.Version {
		return false, nil
	}
	c.Version++
	m.calls[c.ID] = c
	return true, nil
}

func (m *callStore) only(t *testing.T) db.UrgentCall {
	t.Helper()
	m.cmu.Lock()
	defer m.cmu.Unlock()
	if len(m.calls) != 1 {
		t.Fatalf("%d calls, want 1", len(m.calls))
	}
	for _, c := range m.calls {
		return c
	}
	return db.UrgentCall{}
}

// ringHub is the fake hub with call.ring: phones is how many take each ring.
type ringHub struct {
	*fakeHub
	rmu    sync.Mutex
	phones int
	rings  []assistantwire.CallRing
}

func (h *ringHub) CallRing(_ context.Context, c assistantwire.CallRing) (assistantwire.CallRingResult, error) {
	h.rmu.Lock()
	defer h.rmu.Unlock()
	h.rings = append(h.rings, c)
	if h.phones == 0 {
		return assistantwire.CallRingResult{Reason: assistantwire.CallNoPhone}, nil
	}
	return assistantwire.CallRingResult{Devices: h.phones}, nil
}

func (h *ringHub) ringCount() int {
	h.rmu.Lock()
	defer h.rmu.Unlock()
	return len(h.rings)
}

type callTest struct {
	*fleetTest
	st  *callStore
	hub *ringHub
}

// newCallTest is a manager with urgent calls on (when on) and one phone that takes calls.
func newCallTest(t *testing.T, on bool, edit func(*urgentCallSettings)) *callTest {
	t.Helper()
	ft := newFleetTest(t)
	st := &callStore{fleetMem: ft.st, calls: map[string]db.UrgentCall{}}
	ft.s.store = func() Store { return st }
	h := &ringHub{fakeHub: ft.f, phones: 1}
	setHub(ft.s, h)
	set := defaultUrgentCallSettings()
	set.Enabled = on
	if edit != nil {
		edit(&set)
	}
	if err := st.PutSetting(context.Background(), urgentCallSettingsKey, set); err != nil {
		t.Fatal(err)
	}
	return &callTest{fleetTest: ft, st: st, hub: h}
}

func (ct *callTest) call(args map[string]any) string {
	ct.t.Helper()
	b, _ := json.Marshal(args)
	out, err := ct.s.toolUrgentCall(context.Background(), "mac", "conv", b)
	if err != nil {
		ct.t.Fatalf("urgent_call: %v", err)
	}
	return out
}

func (ct *callTest) pushTitles() []string {
	ct.f.mu.Lock()
	defer ct.f.mu.Unlock()
	var out []string
	for _, p := range ct.f.pushes {
		out = append(out, p.Title)
	}
	return out
}

var moneyLoss = map[string]any{"title": "云账单扣费失控", "why": "rule", "rule": "money_loss",
	"happening": "GPU 实例近 1 小时扣了 2300 元还在涨", "body": "要不要现在停掉这批实例？"}

// TestUrgentCallOffPushesInstead: with urgent calls off nothing rings; the matter goes as a P0 push and the model hears why.
func TestUrgentCallOffPushesInstead(t *testing.T) {
	ct := newCallTest(t, false, nil)
	out := ct.call(moneyLoss)
	if !strings.Contains(out, "Did not ring") || !strings.Contains(out, skipOff) || ct.hub.ringCount() != 0 {
		t.Fatalf("answer %q, rings %d", out, ct.hub.ringCount())
	}
	ct.f.mu.Lock()
	defer ct.f.mu.Unlock()
	if len(ct.f.pushes) != 1 || ct.f.pushes[0].Level != assistantwire.LevelP0 || ct.f.pushes[0].Title != "云账单扣费失控" {
		t.Fatalf("pushes %+v", ct.f.pushes)
	}
}

// TestUrgentCallRuleRings: a matter that matches a rule rings once, and the call is kept with the rule's words.
func TestUrgentCallRuleRings(t *testing.T) {
	ct := newCallTest(t, true, nil)
	out := ct.call(moneyLoss)
	if !strings.Contains(out, "Ringing") || ct.hub.ringCount() != 1 {
		t.Fatalf("answer %q, rings %d", out, ct.hub.ringCount())
	}
	c := ct.st.only(t)
	if c.State != callRinging || c.Attempt != 1 || c.Step != stepRingTimeout || c.RuleID != "money_loss" || c.RuleText == "" || c.Conversation != "conv" {
		t.Fatalf("call %+v", c)
	}
	if r := ct.hub.rings[0]; r.ID != c.ID || r.Title != "云账单扣费失控" || r.ConversationID != "conv" || r.Attempt != 1 {
		t.Fatalf("ring %+v", r)
	}
	if len(ct.pushTitles()) != 0 {
		t.Fatalf("pushed %v while ringing", ct.pushTitles())
	}
}

// TestUrgentCallGates: each gate stops the call and the matter is pushed instead, with the reason.
func TestUrgentCallGates(t *testing.T) {
	with := func(change map[string]any) map[string]any {
		out := map[string]any{}
		for k, v := range moneyLoss {
			out[k] = v
		}
		for k, v := range change {
			if v == nil {
				delete(out, k)
			} else {
				out[k] = v
			}
		}
		return out
	}
	for _, c := range []struct {
		name   string
		args   map[string]any
		edit   func(*urgentCallSettings)
		reason string
	}{
		{"unknown rule", with(map[string]any{"rule": "weather"}), nil, skipNoRule},
		{"deleted rule", moneyLoss, func(s *urgentCallSettings) { s.Rules = s.Rules[1:] }, skipNoRule},
		{"no happening", with(map[string]any{"happening": "涨了"}), nil, skipNoHappening},
		{"nothing to do", with(map[string]any{"body": nil}), nil, skipNoAction},
		{"card not pending", with(map[string]any{"interaction_id": "ix_gone"}), nil, skipNoAction},
		{"quiet hours", moneyLoss, func(s *urgentCallSettings) { s.Quiet = &urgentQuiet{Start: "00:00", End: "23:59"} }, skipQuiet},
	} {
		t.Run(c.name, func(t *testing.T) {
			ct := newCallTest(t, true, c.edit)
			if c.name == "quiet hours" && (&urgentQuiet{Start: "00:00", End: "23:59"}).inQuiet(time.Now().In(ct.s.location(context.Background()))) == false {
				t.Skip("the one minute outside the quiet hours")
			}
			out := ct.call(c.args)
			if !strings.Contains(out, "("+c.reason+":") || ct.hub.ringCount() != 0 || len(ct.pushTitles()) != 1 {
				t.Fatalf("answer %q, rings %d, pushes %v", out, ct.hub.ringCount(), ct.pushTitles())
			}
		})
	}
}

// TestUrgentCallOwnerQuote: owner_asked counts only the owner's own words in the conversation: not the assistant's, not
// a message forwarded in or from someone in a group, not one older than a week, not a few characters.
func TestUrgentCallOwnerQuote(t *testing.T) {
	now := time.Now()
	quote := "部署出问题就打给我"
	for _, c := range []struct {
		name  string
		item  proto.Item
		quote string
		want  bool
	}{
		{"owner's words", proto.Item{Role: "user", Kind: proto.KindText, Text: "今晚上线，部署出问题就打给我", TS: now.Add(-time.Hour)}, quote, true},
		{"spacing differs", proto.Item{Role: "user", Kind: proto.KindText, Text: "部署出问题就  打给我", TS: now}, "部署出问题就 打给我", true},
		{"assistant's words", proto.Item{Role: "assistant", Kind: proto.KindText, Text: quote, TS: now}, quote, false},
		{"forwarded", proto.Item{Role: "user", Kind: proto.KindText, Text: quote, TS: now, Source: &proto.MessageSource{Kind: proto.SourceConversation, ConversationID: "x"}}, quote, false},
		{"someone in a group", proto.Item{Role: "user", Kind: proto.KindText, Text: quote, TS: now, From: "Ann"}, quote, false},
		{"older than a week", proto.Item{Role: "user", Kind: proto.KindText, Text: quote, TS: now.Add(-8 * 24 * time.Hour)}, quote, false},
		{"not said", proto.Item{Role: "user", Kind: proto.KindText, Text: "今晚上线", TS: now}, quote, false},
	} {
		if got := ownerWrote([]proto.Item{c.item}, c.quote, now); got != c.want {
			t.Errorf("%s: ownerWrote = %v, want %v", c.name, got, c.want)
		}
	}
	// A quote of a few characters is refused before the conversation is read, and the matter is pushed instead.
	ct := newCallTest(t, true, nil)
	out := ct.call(map[string]any{"title": "部署出问题", "why": "owner_asked", "owner_quote": "打给", "body": "登录失败率 18%，要不要回滚？"})
	if !strings.Contains(out, skipNoQuote) || ct.hub.ringCount() != 0 || len(ct.pushTitles()) != 1 {
		t.Fatalf("answer %q", out)
	}
}

// TestUrgentCallCapAndRepeats: one matter rings once; the day's cap counts matters, and two matters keep 10 minutes
// apart.
func TestUrgentCallCapAndRepeats(t *testing.T) {
	ct := newCallTest(t, true, func(s *urgentCallSettings) { s.DailyCap = 1 })
	ct.call(moneyLoss)
	if out := ct.call(moneyLoss); !strings.Contains(out, skipAlreadyCalled) {
		t.Fatalf("same matter: %q", out)
	}
	other := map[string]any{"title": "删库", "why": "rule", "rule": "agent_disaster", "happening": "Agent 在生产库删了 users 表", "body": "已停掉它，要不要从备份恢复？"}
	if out := ct.call(other); !strings.Contains(out, skipCap) {
		t.Fatalf("over the cap: %q", out)
	}
	ct2 := newCallTest(t, true, nil)
	ct2.call(moneyLoss)
	if out := ct2.call(other); !strings.Contains(out, skipSpacing) {
		t.Fatalf("within 10 minutes: %q", out)
	}
}

// TestUrgentCallNoPhone: a ring no phone took is failed and not counted; the matter goes as a P0 push at once.
func TestUrgentCallNoPhone(t *testing.T) {
	ct := newCallTest(t, true, nil)
	ct.hub.phones = 0
	out := ct.call(moneyLoss)
	if !strings.Contains(out, "Could not ring") || len(ct.pushTitles()) != 1 {
		t.Fatalf("answer %q, pushes %v", out, ct.pushTitles())
	}
	if c := ct.st.only(t); c.State != callFailed || c.NextAt != nil {
		t.Fatalf("call %+v", c)
	}
	ct.hub.phones = 1
	if out := ct.call(moneyLoss); !strings.Contains(out, "Ringing") {
		t.Fatalf("a failed call is not the matter's call: %q", out)
	}
}

func (ct *callTest) report(id, body string) {
	ct.t.Helper()
	if w := serve(ct.s, "POST", "/api/v1/assistant/calls/"+id+"/state", body); w.Code != 200 {
		ct.t.Fatalf("report %s: %d %s", body, w.Code, w.Body)
	}
}

// TestUrgentCallMissedRetriesThenReminds: a missed ring rings once more after the retry wait; missed again, the matter
// is pushed and reminded at 10 and 30 minutes, and then left alone.
func TestUrgentCallMissedRetriesThenReminds(t *testing.T) {
	ct := newCallTest(t, true, nil)
	ctx := context.Background()
	ct.call(moneyLoss)
	id := ct.st.only(t).ID
	ct.report(id, `{"state":"missed"}`)
	c := ct.st.only(t)
	if c.State != callMissed || c.Step != stepRetry || len(ct.pushTitles()) != 0 {
		t.Fatalf("after the first miss %+v, pushes %v", c, ct.pushTitles())
	}
	ct.s.urgentDue(ctx, c.NextAt.Add(time.Second))
	c = ct.st.only(t)
	if c.State != callRinging || c.Attempt != 2 || ct.hub.ringCount() != 2 {
		t.Fatalf("the retry %+v, rings %d", c, ct.hub.ringCount())
	}
	ct.report(id, `{"state":"missed"}`)
	c = ct.st.only(t)
	if c.State != callMissed || c.Step != stepRemind || len(ct.pushTitles()) != 1 || !strings.Contains(ct.pushTitles()[0], "云账单扣费失控") {
		t.Fatalf("after the second miss %+v, pushes %v", c, ct.pushTitles())
	}
	ct.s.urgentDue(ctx, c.NextAt.Add(time.Second))
	c = ct.st.only(t)
	ct.s.urgentDue(ctx, c.NextAt.Add(time.Second))
	c = ct.st.only(t)
	if c.NextAt != nil || c.Reminders != 2 || len(ct.pushTitles()) != 3 || ct.hub.ringCount() != 2 {
		t.Fatalf("after the reminders %+v, pushes %v, rings %d", c, ct.pushTitles(), ct.hub.ringCount())
	}
}

// TestUrgentCallRingTimeout: a ring no phone reports on counts as missed when its timeout passes.
func TestUrgentCallRingTimeout(t *testing.T) {
	ct := newCallTest(t, true, nil)
	ct.call(moneyLoss)
	c := ct.st.only(t)
	ct.s.urgentDue(context.Background(), c.NextAt.Add(time.Second))
	if c = ct.st.only(t); c.State != callMissed || c.Step != stepRetry {
		t.Fatalf("after the timeout %+v", c)
	}
}

// TestUrgentCallDeclinedAndAnswered: declined pushes the matter once and never rings again; answered then ended keeps
// the transcript, and the apps hear each change.
func TestUrgentCallDeclinedAndAnswered(t *testing.T) {
	ct := newCallTest(t, true, nil)
	ct.call(moneyLoss)
	id := ct.st.only(t).ID
	ct.report(id, `{"state":"declined"}`)
	if c := ct.st.only(t); c.State != callDeclined || c.NextAt != nil || len(ct.pushTitles()) != 1 {
		t.Fatalf("declined %+v, pushes %v", c, ct.pushTitles())
	}
	if w := serve(ct.s, "POST", "/api/v1/assistant/calls/"+id+"/state", `{"state":"answered"}`); w.Code != 409 {
		t.Fatalf("answered after declined: %d", w.Code)
	}

	ct = newCallTest(t, true, nil)
	ct.call(moneyLoss)
	id = ct.st.only(t).ID
	ct.report(id, `{"state":"answered"}`)
	ct.report(id, `{"state":"answered"}`) // a second phone, or the same report again
	ct.report(id, `{"state":"ended","seconds":125,"outcome":"停掉了实例","transcript":"Persona：GPU 实例在扣费…\n你：停掉"}`)
	w := serve(ct.s, "GET", "/api/v1/assistant/calls/"+id, "")
	var v map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &v)
	if w.Code != 200 || v["state"] != callEnded || v["seconds"] != 125.0 || !strings.Contains(v["transcript"].(string), "停掉") || v["rule"] == nil {
		t.Fatalf("view %d %v", w.Code, v)
	}
	ct.f.mu.Lock()
	events := slices.Clone(ct.f.events)
	ct.f.mu.Unlock()
	if n := len(slices.DeleteFunc(events, func(e string) bool { return e != "urgent_call" })); n != 2 {
		t.Fatalf("urgent_call messages %d, want 2 (answered, ended): %v", n, events)
	}
	if w := serveAs(ct.s, "read", "GET", "/api/v1/assistant/calls/"+id, ""); w.Code != 403 {
		t.Fatalf("read-only device: %d", w.Code)
	}
	w = serve(ct.s, "GET", "/api/v1/assistant/calls?conversation=conv", "")
	var list struct {
		Calls []map[string]any `json:"calls"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &list)
	if w.Code != 200 || len(list.Calls) != 1 || list.Calls[0]["id"] != id {
		t.Fatalf("list %d %s", w.Code, w.Body)
	}
	if w := serve(ct.s, "GET", "/api/v1/assistant/calls?conversation=other", ""); !strings.Contains(w.Body.String(), `"calls":[]`) {
		t.Fatalf("another conversation's list: %s", w.Body)
	}
}

// TestUrgentCardDone: a card answered elsewhere cancels the call that still rings about it.
func TestUrgentCardDone(t *testing.T) {
	ct := newCallTest(t, true, nil)
	now := time.Now()
	ctx := context.Background()
	c := db.UrgentCall{ID: "call_card", MachineID: "mac", Conversation: "conv", Matter: "ix:ix_1", Title: "t", Why: urgentWhyRule,
		InteractionID: "ix_1", State: callRinging, Attempt: 1, CreatedAt: now}
	if _, err := ct.s.ringNew(ctx, ct.st, &c, now); err != nil {
		t.Fatal(err)
	}
	ct.s.urgentCardDone("ix_1")
	if got := ct.st.only(t); got.State != callCancelled || got.NextAt != nil {
		t.Fatalf("after the card %+v", got)
	}
}

// TestUrgentSettingsRoutes: the settings start off with the three built-in rules; a change is checked, needs a full
// device, and brings the tool and the instructions' section.
func TestUrgentSettingsRoutes(t *testing.T) {
	ct := newCallTest(t, false, nil)
	w := serve(ct.s, "GET", "/api/v1/assistant/urgent-call", "")
	var got struct {
		Settings urgentCallSettings `json:"settings"`
		CanWrite bool               `json:"can_write"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &got)
	if w.Code != 200 || got.Settings.Enabled || got.Settings.DailyCap != 3 || len(got.Settings.Rules) != 3 || !got.CanWrite {
		t.Fatalf("defaults %d %s", w.Code, w.Body)
	}
	for _, bad := range []string{`{"daily_cap":11}`, `{"retry_minutes":31}`, `{"quiet":{"start":"25:00","end":"07:00"}}`,
		`{"rules":[{"id":"r_1","text":""}]}`, `{"rules":[{"id":"r_1","text":"a"},{"id":"r_1","text":"b"}]}`} {
		if w := serve(ct.s, "PUT", "/api/v1/assistant/urgent-call", bad); w.Code != 400 {
			t.Errorf("%s: %d", bad, w.Code)
		}
	}
	if w := serveAs(ct.s, "read", "PUT", "/api/v1/assistant/urgent-call", `{"enabled":true}`); w.Code != 403 {
		t.Fatalf("read-only device: %d", w.Code)
	}
	if tools := ct.s.Tools(context.Background()); slices.ContainsFunc(tools, func(t proto.Tool) bool { return t.Name == "urgent_call" }) {
		t.Fatal("urgent_call listed while off")
	}
	if ct.s.urgentPrompt(context.Background()) != nil {
		t.Fatal("instructions' section while off")
	}
	body := `{"enabled":true,"daily_cap":5,"rules":[{"id":"harm","builtin":true},{"id":"r_kids","text":"接孩子相关的事"}]}`
	if w := serve(ct.s, "PUT", "/api/v1/assistant/urgent-call", body); w.Code != 200 {
		t.Fatalf("put %d %s", w.Code, w.Body)
	}
	if tools := ct.s.Tools(context.Background()); !slices.ContainsFunc(tools, func(t proto.Tool) bool { return t.Name == "urgent_call" }) {
		t.Fatal("urgent_call not listed once on")
	}
	ct.f.mu.Lock()
	told := slices.ContainsFunc(ct.f.told, func(e told) bool { return e.event == assistantwire.EventToolsChanged })
	ct.f.mu.Unlock()
	if !told {
		t.Fatal("the hub was not told the tools changed")
	}
	p := ct.s.urgentPrompt(context.Background())
	if p == nil || len(p.Rules) != 2 || p.Rules[0].Text != prompts.UrgentRuleBuiltin("harm") || p.Rules[1].Text != "接孩子相关的事" {
		t.Fatalf("prompt %+v", p)
	}
	md := prompts.RenderAssistantClaudeMD(prompts.AssistantClaudeMD{UrgentCall: p})
	if !strings.Contains(md, "## 加急来电") || !strings.Contains(md, "`r_kids`：接孩子相关的事") || !strings.Contains(md, "`harm`：人身伤害") {
		t.Fatalf("instructions:\n%s", md)
	}
	if md := prompts.RenderAssistantClaudeMD(prompts.AssistantClaudeMD{}); strings.Contains(md, "加急来电") {
		t.Fatal("the section shows while urgent calls are off")
	}
}

// TestUrgentRulesSetting: persona_settings adds, rewords and removes a rule only through the change the owner approves.
func TestUrgentRulesSetting(t *testing.T) {
	ct := newCallTest(t, true, nil)
	ctx := context.Background()
	apply := func(action, value string) string {
		t.Helper()
		ch, err := ct.s.changeUrgentRules(ctx, action, json.RawMessage(value))
		if err != nil {
			t.Fatalf("%s %s: %v", action, value, err)
		}
		if ch.apply == nil {
			return ch.summary
		}
		if strings.TrimSpace(ch.card("zh-Hans")) == "" {
			t.Fatalf("%s: no card text", action)
		}
		out, err := ch.apply(ctx)
		if err != nil {
			t.Fatalf("%s apply: %v", action, err)
		}
		return out
	}
	apply("add", `{"text":"接孩子相关的事"}`)
	if out := apply("add", `{"text":"接孩子相关的事"}`); !strings.Contains(out, "already") {
		t.Fatalf("added twice: %q", out)
	}
	set, _ := ct.s.urgentSettings(ctx)
	kids := set.Rules[len(set.Rules)-1]
	if len(set.Rules) != 4 || kids.Text != "接孩子相关的事" || kids.Builtin {
		t.Fatalf("rules %+v", set.Rules)
	}
	apply("update", `{"id":"money_loss","text":"信用卡出现陌生扣款"}`)
	apply("remove", `{"id":"harm"}`)
	set, _ = ct.s.urgentSettings(ctx)
	if len(set.Rules) != 3 || set.Rules[0].Text != "信用卡出现陌生扣款" || slices.ContainsFunc(set.Rules, func(r urgentRule) bool { return r.ID == "harm" }) {
		t.Fatalf("rules %+v", set.Rules)
	}
	if _, err := ct.s.changeUrgentRules(ctx, "remove", json.RawMessage(`{"id":"nope"}`)); err == nil {
		t.Fatal("removed a rule that is not there")
	}
	// A custom rule rings like a built-in one.
	out := ct.call(map[string]any{"title": "提前放学", "why": "rule", "rule": kids.ID, "happening": "学校群说 15 分钟后提前放学，还没人去接", "body": "要不要让人去接？"})
	if !strings.Contains(out, "Ringing") || ct.st.only(t).RuleText != "接孩子相关的事" {
		t.Fatalf("custom rule: %q", out)
	}
}

// TestUrgentCallNorthStar: a matter that rang is one urgent_call interruption; one the gates stopped is excluded by
// why; a test call is a probe.
func TestUrgentCallNorthStar(t *testing.T) {
	now := time.Now()
	q := northStarQuery{Since: now.Add(-time.Hour), Until: now.Add(time.Hour)}
	events := []db.WorkEvent{
		{At: now, MachineID: "mac", SessionID: "conv", Kind: urgentCallEvent, Detail: map[string]any{"why": urgentWhyRule}},
		{At: now, MachineID: "mac", SessionID: "conv", Kind: urgentCallEvent, Detail: map[string]any{"why": urgentWhyTest, "test": true}},
		{At: now, MachineID: "mac", SessionID: "conv", Kind: urgentSkippedEvent, Detail: map[string]any{"reason": skipCap}},
	}
	in := interruptions(q, convRoles{}, nil, events, func(time.Time) bool { return true })
	if in.Categories[nsUrgentCall].Count != 1 || in.Total != 1 || in.Excluded.CallSkipped[skipCap] != 1 || in.Excluded.Probe != 1 {
		t.Fatalf("interruptions %+v (excluded %+v)", in, in.Excluded)
	}
}
