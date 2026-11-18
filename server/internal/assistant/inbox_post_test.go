package assistant

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/proto"
)

func postHub(t *testing.T) *inboxHub {
	t.Helper()
	h := newInboxHub(t)
	if err := h.inboxReady(context.Background()); err != nil {
		t.Fatal(err)
	}
	waitNeedsYouIdle(t, h.Service)
	return h
}

func postItems(h *inboxHub) (out []*InboxItem) {
	h.inbox.mu.Lock()
	defer h.inbox.mu.Unlock()
	for _, it := range h.inbox.items {
		if it.Source == inboxPostSource {
			out = append(out, it)
		}
	}
	return out
}

func TestInboxPostMakesAGenericRow(t *testing.T) {
	ctx := context.Background()
	h := postHub(t)
	out, err := h.toolInboxPost(ctx, "m1", "s1", json.RawMessage(`{"title":"Price drop","summary":"20% off until Sunday","tier":"action","reason":"you asked to hear about drops"}`))
	if err != nil || !strings.Contains(out, "Added") {
		t.Fatalf("post: %q %v", out, err)
	}
	items := postItems(h)
	if len(items) != 1 {
		t.Fatalf("%d rows", len(items))
	}
	it := items[0]
	if it.Connector == nil || it.Connector.Thread != "m1/s1" || it.Account != inboxPostSource || it.Tier != TierAction || it.Title != "Price drop" ||
		it.Reason != "you asked to hear about drops" || !it.open() || it.Count != 1 || it.Summary == "" {
		t.Fatalf("row %+v connector %+v", it, it.Connector)
	}
	if it.Sender != "" || it.Summary != "20% off until Sunday" || it.Context != "Price drop\n20% off until Sunday" {
		t.Fatalf("no sender (the Assistant tag says who), the summary is the whole text and the context only title and summary: %q %q %q", it.Sender, it.Summary, it.Context)
	}
	if it.Kind != inboxPostKind || it.Connector.Label == "" || it.AccountName == "" {
		t.Fatalf("an unknown source draws as a generic row: kind %q label %q name %q", it.Kind, it.Connector.Label, it.AccountName)
	}
}

// A post's text is the assistant's Markdown (#9041): the row keeps it, line breaks and all, as body for the detail, and
// says it without the marks on one line wherever a line is drawn (summary, preview, the chain's received message).
func TestInboxPostKeepsMarkdownInBody(t *testing.T) {
	ctx := context.Background()
	h := postHub(t)
	md := "**Morning briefing · Mon**\nToday: one tutorial.\n\n## Classes\n- **Tutorial: 10:30–12:20**, room B2\n- Short quiz, `no` make-up\n"
	if _, err := h.postToInbox(ctx, "m1", "s1", inboxPostArgs{Title: "Morning briefing", Summary: md, Reason: "daily"}, time.Now()); err != nil {
		t.Fatal(err)
	}
	items := postItems(h)
	if len(items) != 1 {
		t.Fatalf("%d rows", len(items))
	}
	it := items[0]
	if it.Body != strings.TrimSpace(md) {
		t.Fatalf("body is the Markdown as written: %q", it.Body)
	}
	const plain = "Morning briefing · Mon Today: one tutorial. Classes Tutorial: 10:30–12:20, room B2 Short quiz, no make-up"
	if it.Summary != plain || it.Preview != plain {
		t.Fatalf("summary and preview are its words on one line:\n%q\n%q", it.Summary, it.Preview)
	}
	if it.Context != "Morning briefing\n"+strings.TrimSpace(md) {
		t.Fatalf("the context keeps the Markdown for the assistant: %q", it.Context)
	}
	var said []string
	for _, e := range it.Chain {
		if e.Kind == ChainReceived {
			for _, m := range e.Messages {
				said = append(said, m.Text)
			}
		}
	}
	if len(said) != 1 || said[0] != plain {
		t.Fatalf("the chain's received message says the words: %q", said)
	}
	raw, err := json.Marshal(it.listed())
	if err != nil || !strings.Contains(string(raw), `"body":"**Morning briefing`) {
		t.Fatalf("the list carries body, so the detail draws it at once: %s %v", raw, err)
	}

	// A long briefing: the summary is cut for the row, the body keeps all of it.
	long := "# Weekly\n" + strings.Repeat("- **item** with a few words\n", 60)
	if _, err := h.postToInbox(ctx, "m1", "s2", inboxPostArgs{Title: "Weekly", Summary: long, Reason: "weekly"}, time.Now()); err != nil {
		t.Fatal(err)
	}
	for _, it := range postItems(h) {
		if it.Title != "Weekly" {
			continue
		}
		if it.Body != strings.TrimSpace(long) || len([]rune(it.Summary)) != inboxPostSummaryMax+1 || !strings.HasSuffix(it.Summary, "…") ||
			strings.ContainsAny(it.Summary, "*#") {
			t.Fatalf("long: body %d runes, summary %q", len([]rune(it.Body)), it.Summary)
		}
	}
}

func TestInboxPostRepeatUpdatesOneRow(t *testing.T) {
	ctx := context.Background()
	h := postHub(t)
	now := time.Now()
	a := inboxPostArgs{Title: "Briefing", Summary: "first", Reason: "daily"}
	if _, err := h.postToInbox(ctx, "m1", "s1", a, now); err != nil {
		t.Fatal(err)
	}
	// Handled meanwhile: the repeat opens it again with the newest content.
	h.inbox.mu.Lock()
	for _, it := range h.inbox.items {
		if it.Source == inboxPostSource {
			it.done(now, "user")
		}
	}
	h.inbox.mu.Unlock()
	a.Summary = "second"
	got, err := h.postToInbox(ctx, "m1", "s1", a, now.Add(10*time.Minute))
	if err != nil || !got.updated {
		t.Fatalf("repeat: %+v %v", got, err)
	}
	items := postItems(h)
	if len(items) != 1 || !items[0].open() || !strings.Contains(items[0].Context, "second") || items[0].Read {
		t.Fatalf("one row, open, newest content: %d %+v", len(items), items)
	}
	// Past the window: a row of its own; another conversation: another row.
	if got, err := h.postToInbox(ctx, "m1", "s1", a, now.Add(10*time.Minute+inboxPostWindow+time.Minute)); err != nil || got.updated {
		t.Fatalf("after the window: %+v %v", got, err)
	}
	if _, err := h.postToInbox(ctx, "m1", "s2", a, now); err != nil {
		t.Fatal(err)
	}
	if n := len(postItems(h)); n != 3 {
		t.Fatalf("%d rows, want 3", n)
	}
}

func TestInboxPostRefusals(t *testing.T) {
	ctx := context.Background()
	h := postHub(t)
	for _, args := range []string{`{"title":"x","summary":"y"}`, `{"summary":"y","reason":"r"}`, `{"title":"x","summary":"y","reason":"r","tier":"noise"}`} {
		if _, err := h.toolInboxPost(ctx, "m1", "s1", json.RawMessage(args)); err == nil {
			t.Errorf("%s: want an error", args)
		}
	}
	off := false
	if _, err := h.updateSettings(ctx, func(v *Settings) error { v.InboxPost = &off; return nil }); err != nil {
		t.Fatal(err)
	}
	out, err := h.toolInboxPost(ctx, "m1", "s1", json.RawMessage(`{"title":"x","summary":"y","reason":"r"}`))
	if err != nil || !strings.Contains(out, "turned off") || len(postItems(h)) != 0 {
		t.Fatalf("switch off: %q %v, %d rows", out, err, len(postItems(h)))
	}
}

func TestInboxModeArgs(t *testing.T) {
	for in, want := range map[string]string{"": inboxOff, "off": inboxOff, "worthy": inboxWorthy, "every": inboxEvery} {
		if got, err := inboxArg(in); err != nil || got != want {
			t.Errorf("inboxArg(%q) = %q, %v", in, got, err)
		}
	}
	if _, err := inboxArg("always"); err == nil {
		t.Error("an unknown setting is refused")
	}
	raw := map[string]json.RawMessage{"inbox": json.RawMessage(`"worthy"`)}
	if m, has, err := inboxIn(raw); err != nil || !has || m != inboxWorthy {
		t.Errorf("inboxIn: %q %v %v", m, has, err)
	}
	if _, has, err := inboxIn(map[string]json.RawMessage{}); has || err != nil {
		t.Errorf("a body without it leaves it: %v %v", has, err)
	}
	if _, _, err := inboxIn(map[string]json.RawMessage{"inbox": json.RawMessage(`null`)}); err == nil {
		t.Error("null is refused")
	}
	if err := checkInbox("sometimes"); err == nil {
		t.Error("checkInbox")
	}
}

// A task's inbox setting through the app: POST and PUT take it; a PUT without it (an app that does not know the field)
// and the old app's routines PUT keep it; a bad word is refused.
func TestTaskInboxThroughTheApp(t *testing.T) {
	h := newStorelessService(t, newFakeHub())
	do := func(method, path, body string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(method, path, strings.NewReader(body)))
		return rec
	}
	created := func(rec *httptest.ResponseRecorder) Task {
		t.Helper()
		var out struct{ Task Task }
		if rec.Code != 200 || json.Unmarshal(rec.Body.Bytes(), &out) != nil {
			t.Fatalf("%d %s", rec.Code, rec.Body)
		}
		return out.Task
	}
	mode := func(id string) string {
		task, ok := taskByID(h, id)
		if !ok {
			t.Fatalf("no task %s", id)
		}
		return task.Inbox
	}
	plain := created(do("POST", "/api/v1/assistant/tasks", `{"title":"早报","prompt":"给我早报","repeat":"daily","time":"09:00","enabled":true}`))
	if plain.Inbox != inboxOff {
		t.Fatalf("a task made without the setting puts nothing in the Inbox: %+v", plain)
	}
	watch := created(do("POST", "/api/v1/assistant/tasks", `{"title":"价格","prompt":"看看降价","repeat":"interval","every_minutes":30,"enabled":true,"push":"asked","inbox":"worthy"}`))
	if watch.Inbox != inboxWorthy || watch.Push != pushAsked {
		t.Fatalf("created with inbox worthy: %+v", watch)
	}
	body := `{"title":"价格","prompt":"看看降价了没","repeat":"interval","every_minutes":30,"enabled":true}`
	if rec := do("PUT", "/api/v1/assistant/tasks/"+watch.ID, body); rec.Code != 200 || mode(watch.ID) != inboxWorthy {
		t.Fatalf("a PUT without it keeps it: %d %s, %q", rec.Code, rec.Body, mode(watch.ID))
	}
	for _, bad := range []string{`null`, `1`, `"sometimes"`} {
		with := strings.TrimSuffix(body, "}") + `,"inbox":` + bad + `}`
		if rec := do("PUT", "/api/v1/assistant/tasks/"+watch.ID, with); rec.Code != 400 || mode(watch.ID) != inboxWorthy {
			t.Fatalf("inbox %s is refused and changes nothing: %d %s, %q", bad, rec.Code, rec.Body, mode(watch.ID))
		}
	}
	if rec := do("PUT", "/api/v1/assistant/tasks/"+watch.ID, strings.TrimSuffix(body, "}")+`,"inbox":"every"}`); rec.Code != 200 || mode(watch.ID) != inboxEvery {
		t.Fatalf("every: %d %s, %q", rec.Code, rec.Body, mode(watch.ID))
	}
	if rec := do("PUT", "/api/v1/assistant/tasks/"+watch.ID, strings.TrimSuffix(body, "}")+`,"inbox":"off"}`); rec.Code != 200 || mode(watch.ID) != inboxOff {
		t.Fatalf("off: %d %s, %q", rec.Code, rec.Body, mode(watch.ID))
	}
	daily := created(do("POST", "/api/v1/assistant/tasks", `{"title":"日报","prompt":"总结","repeat":"daily","time":"18:00","enabled":true,"inbox":"every"}`))
	routines := `{"routines":[{"id":"` + daily.ID + `","title":"日报","prompt":"总结","time":"18:30","enabled":true}]}`
	if rec := do("PUT", "/api/v1/assistant/routines", routines); rec.Code != 200 {
		t.Fatalf("routines: %d %s", rec.Code, rec.Body)
	}
	if got, _ := taskByID(h, daily.ID); got.Inbox != inboxEvery || got.Time != "18:30" {
		t.Fatalf("the old app's routines PUT keeps the setting: %+v", got)
	}
}

// A task set to "every" ends each run with a row: the run's conversation settles with an answer → one row titled by the
// task, tier FYI, what the run said; a failed run and a task not set to it put nothing there.
func TestInboxPostEveryAfterARun(t *testing.T) {
	ctx := context.Background()
	h := newSpawnHarness(t)
	sc := h.s.loadSchedule(ctx)
	sc.mu.Lock()
	sc.tasks = append(sc.tasks,
		Task{ID: "task_every", Title: "早报", Prompt: "p", Repeat: repeatDaily, Time: "09:00", Enabled: true, Inbox: inboxEvery, RunMachineID: "server", RunSessionID: "run-1"},
		Task{ID: "task_plain", Title: "别的", Prompt: "p", Repeat: repeatDaily, Time: "09:00", Enabled: true, RunMachineID: "server", RunSessionID: "run-2"})
	sc.mu.Unlock()
	h.f.known["run-1"], h.f.known["run-2"] = true, true
	rows := func() []*InboxItem {
		h.s.inbox.mu.Lock()
		defer h.s.inbox.mu.Unlock()
		var out []*InboxItem
		for _, it := range h.s.inbox.items {
			if it.Source == inboxPostSource {
				out = append(out, it)
			}
		}
		return out
	}
	run := func(id, text, turnErr string) {
		h.f.setSessions("server", h.parent, proto.Session{SessionID: id, MachineID: "server", Assistant: true, Status: proto.StatusIdle, LastText: text, TurnError: turnErr})
		h.s.assistantTurnSettled(ctx, "server", id)
	}
	run("run-2", "今天三个会", "")
	run("run-1", "", "")
	run("run-1", "今天三个会", "model overloaded")
	if n := len(rows()); n != 0 {
		t.Fatalf("a task not set to it, an empty run and a failed run put nothing in the Inbox: %d rows", n)
	}
	run("run-1", "今天三个会，设计评审改到 15:00", "")
	got := rows()
	if len(got) != 1 || got[0].Title != "早报" || got[0].Tier != TierFYI || !strings.Contains(got[0].Context, "15:00") || got[0].Connector.Thread != "server/run-1" {
		t.Fatalf("rows: %+v", got)
	}
}

// The row of a task set to "every" holds the run's whole reply as written, read from its conversation: the session
// list's last_text is one line cut at 200 characters, which ran a briefing's Markdown together (#9041).
func TestInboxPostEveryKeepsTheWholeReply(t *testing.T) {
	ctx := context.Background()
	h := newSpawnHarness(t)
	sc := h.s.loadSchedule(ctx)
	sc.mu.Lock()
	sc.tasks = append(sc.tasks, Task{ID: "task_every", Title: "早报", Prompt: "p", Repeat: repeatDaily, Time: "09:00", Enabled: true,
		Inbox: inboxEvery, RunMachineID: "server", RunSessionID: "run-1"})
	sc.mu.Unlock()
	h.f.known["run-1"] = true
	reply := "**早报 · 周一**\n结论：上午一节辅导课。\n\n**今天的课**\n- **辅导课：10:30–12:20**\n- " + strings.Repeat("随堂小测，", 60)
	h.f.answerNode(DeviceSessionItems, func(string, json.RawMessage) (any, error) {
		return map[string]any{"items": []proto.Item{
			{ID: "1", Kind: proto.KindText, Role: "user", Text: "p"},
			{ID: "2", Kind: proto.KindText, Role: "assistant", Text: "我先看看邮件。"},
			{ID: "3", Kind: proto.KindToolUse, Role: "assistant"},
			{ID: "4", Kind: proto.KindText, Role: "assistant", Text: reply},
		}}, nil
	})
	h.f.setSessions("server", h.parent, proto.Session{SessionID: "run-1", MachineID: "server", Assistant: true, Status: proto.StatusIdle,
		LastText: clip(oneLine(reply), 200)})
	h.s.assistantTurnSettled(ctx, "server", "run-1")
	h.s.inbox.mu.Lock()
	var got []InboxItem
	for _, it := range h.s.inbox.items {
		if it.Source == inboxPostSource {
			got = append(got, *it)
		}
	}
	h.s.inbox.mu.Unlock()
	if len(got) != 1 || got[0].Body != reply || strings.Contains(got[0].Summary, "**") || !strings.HasPrefix(got[0].Summary, "早报 · 周一 结论：上午一节辅导课。 今天的课 辅导课") {
		t.Fatalf("rows: %+v", got)
	}
}
