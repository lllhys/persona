package assistant

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/ai"
	"github.com/nwverse/persona/server/internal/apierr"
	"github.com/nwverse/persona/server/internal/assistantwire"
	"github.com/nwverse/persona/server/internal/prompts"
	"github.com/nwverse/persona/server/internal/proto"
	"github.com/nwverse/persona/server/internal/rpc"
)

func agentHub(t *testing.T) *inboxHub {
	h := newInboxHub(t)
	if err := h.inboxReady(context.Background()); err != nil {
		t.Fatal(err)
	}
	waitNeedsYouIdle(t, h.Service)
	return h
}

func actionItem(id string, at time.Time) *InboxItem {
	return &InboxItem{ID: id, Kind: KindDM, Tier: TierAction, Last: "12", TriagedAt: "12", Context: "张三：1013 合了吗？",
		UpdatedAt: at, Lark: &InboxLark{FeedID: "f1"}}
}

// Every newly triaged Needs you item is escalated to the manager (no judge filters it): queued, In progress; new content goes
// to the same conversation; old, untriaged, not Needs you, done ones are not.
func TestInboxEscalates(t *testing.T) {
	now := time.Now()
	h := agentHub(t)
	it := actionItem("lark:f1:11", now)
	it.Summary, it.Reason = "张三问 1013 合了没有", "私聊直接提问"
	h.inbox.items[it.ID] = it
	if got := h.inbox.escalate(now.Add(-time.Hour), now); len(got) != 1 || it.Agent == nil || it.Agent.State != AgentQueued ||
		it.Agent.Task != "张三问 1013 合了没有" || it.Agent.Of != "12" || it.Handling == nil || it.Handling.State != HandlingLooking ||
		it.status() != StatusWorking {
		t.Fatalf("escalated %d: agent %+v handling %+v", len(got), it.Agent, it.Handling)
	}
	if got := h.inbox.escalate(now.Add(-time.Hour), now); len(got) != 0 {
		t.Fatalf("escalated twice: %d", len(got))
	}
	// Its conversation is there: new messages are told it, not started again.
	it.Agent.State, it.Agent.SessionID, it.Agent.Pending = AgentRunning, "s1", nil
	h.inbox.setHandling(it, HandlingWaiting, "", now)
	it.Last, it.Context = "13", "张三：好的，那我等你"
	it.TriagedAt = "13" // handled: triage leaves it
	if got := h.inbox.escalate(now.Add(-time.Hour), now); len(got) != 1 || len(it.Agent.Pending) != 1 ||
		it.Agent.Pending[0].Name != prompts.NoteNewMessages || !it.Agent.Wake || it.Handling.State != HandlingLooking || it.Agent.SessionID != "s1" {
		t.Fatalf("new messages: %+v / %+v", it.Agent, it.Handling)
	}

	fyi := actionItem("a", now)
	fyi.Tier = TierFYI
	old := actionItem("b", now.Add(-48*time.Hour))
	stale := actionItem("c", now)
	stale.Last = "14"
	done := actionItem("d", now)
	done.done(now, "user")
	h.inbox.items = map[string]*InboxItem{"a": fyi, "b": old, "c": stale, "d": done}
	if got := h.inbox.escalate(now.Add(-24*time.Hour), now); len(got) != 0 {
		t.Fatalf("escalated %+v", got)
	}
}

// The judge only links an event to one of the user's tasks: a failure leaves it unlinked.
func TestInboxLinkTaskModelFailure(t *testing.T) {
	h := agentHub(t)
	it := actionItem("x", time.Now())
	it.Agent = &InboxAgent{State: AgentQueued}
	h.inbox.items[it.ID] = it
	h.in.model = fakeModel(t, "no json here")
	if h.linkTask(context.Background(), it, []prompts.InboxTask{{ID: "todo_1"}}) || it.Agent.TodoID != "" {
		t.Fatalf("linked on a failed call: %+v", it.Agent)
	}
}

// An agent runtime that is already running answers before startInboxAgent has recorded the conversation: the draft
// waits for it instead of failing.
func TestInboxDraftBeforeTheStartReturned(t *testing.T) {
	h := agentHub(t)
	it := actionItem("x", time.Now())
	it.Agent = &InboxAgent{State: AgentRunning, Task: "t"} // starting: no conversation yet
	h.inbox.items[it.ID] = it
	go func() {
		time.Sleep(300 * time.Millisecond)
		h.inbox.mu.Lock()
		it.Agent.SessionID = "session-r1"
		h.inbox.mu.Unlock()
	}()
	if id, refused, _ := h.inboxDraft(context.Background(), "session-r1", "好的", "", nil); id != it.ID || refused != nil {
		t.Fatalf("drafted on %q, refused %+v", id, refused)
	}
	if it.Agent.State != AgentDrafted || it.Agent.Draft != "好的" {
		t.Fatalf("agent = %+v", it.Agent)
	}
	old := inboxDraftWait
	inboxDraftWait = 200 * time.Millisecond
	t.Cleanup(func() { inboxDraftWait = old })
	other := actionItem("y", time.Now())
	other.Agent = &InboxAgent{State: AgentRunning}
	h.inbox.mu.Lock()
	h.inbox.items[other.ID] = other
	h.inbox.mu.Unlock()
	if id, _, _ := h.inboxDraft(context.Background(), "session-nobody", "hi", "", nil); id != "" {
		t.Fatal("a conversation no item gets drafted")
	}
}

// The draft (inbox_draft) goes on the item whose agent runs in the conversation, with the moves of a document's rows
// the item offers; the hub is told which conversations are the AI twin's (no finished push for them).
func TestInboxDraft(t *testing.T) {
	h := agentHub(t)
	ctx := context.Background()
	it := actionItem("x", time.Now())
	it.Agent = &InboxAgent{State: AgentRunning, SessionID: "s1", MachineID: "server", Task: "t"}
	h.inbox.items[it.ID] = it

	if id, _, _ := h.inboxDraft(ctx, "other", "hi", "", nil); id != "" {
		t.Fatalf("drafted from a conversation that is not an inbox agent: %q", id)
	}
	// A change the item does not list is refused, and nothing is kept.
	change := DocChange{Ref: "r1", Field: "状态", Value: "完成"}
	if id, refused, _ := h.inboxDraft(ctx, "s1", "好了", "", []DocChange{change}); id != it.ID || refused == nil || *refused != change ||
		it.Agent.State != AgentRunning || it.Agent.Draft != "" {
		t.Fatalf("a change the item does not offer: %q %v, agent %+v", id, refused, it.Agent)
	}
	if id, refused, _ := h.inboxDraft(ctx, "s1", "合了，MR 已经进主干", "agent_read 看到已合入", nil); id != it.ID || refused != nil {
		t.Fatalf("draft: %q %v", id, refused)
	}
	if a := it.Agent; a.State != AgentDrafted || a.Draft != "合了，MR 已经进主干" || a.Note != "agent_read 看到已合入" || a.DraftedAt == nil {
		t.Fatalf("agent = %+v", a)
	}
	// A document item's own rows and values are taken with the draft.
	doc := actionItem("doc", time.Now())
	doc.Doc = &InboxDoc{Rows: []DocRow{{Ref: "r1", Status: []DocStatus{{Field: "状态", Options: []DocOption{{Name: "完成"}}}}}}}
	doc.Agent = &InboxAgent{State: AgentRunning, SessionID: "s2", Task: "t"}
	h.inbox.mu.Lock()
	h.inbox.items[doc.ID] = doc
	h.inbox.mu.Unlock()
	if id, refused, _ := h.inboxDraft(ctx, "s2", "好了", "", []DocChange{change}); id != doc.ID || refused != nil {
		t.Fatalf("a change the item offers: %q %v", id, refused)
	}
	if a := doc.Agent; a.State != AgentDrafted || len(a.Changes) != 1 || a.Changes[0] != change {
		t.Fatalf("doc agent = %+v", a)
	}
	r := h.inboxReport(ctx)
	if r == nil || len(r.Agents) != 2 || r.Agents[0] != (SessionRef{MachineID: "server", SessionID: "s1"}) || r.Agents[1].SessionID != "s2" {
		t.Fatalf("the agents the hub is told: %+v", r)
	}
	// The list carries the draft but none of the bookkeeping.
	it.JudgedAt = "12"
	l := it.listed()
	if l.Agent == it.Agent || l.Agent.Draft != it.Agent.Draft || l.JudgedAt != "" || l.Context != "" {
		t.Fatalf("listed = %+v", l)
	}
}

func TestInboxAgentTimesOut(t *testing.T) {
	h := agentHub(t)
	started := time.Now().Add(-20 * time.Minute)
	it := actionItem("x", time.Now())
	it.Agent = &InboxAgent{State: AgentRunning, SessionID: "s1", MachineID: "nowhere", StartedAt: &started}
	h.inbox.items[it.ID] = it
	h.sweepInboxAgents(context.Background(), InboxAgentConfig{}, time.Now())
	if it.Agent.State != AgentFailed || it.Agent.Error == "" {
		t.Fatalf("agent = %+v", it.Agent)
	}
}

// An agent that ended its turn without a draft fails (after its first minute).
func TestInboxAgentDoneWithoutADraft(t *testing.T) {
	h := agentHub(t)
	started := time.Now().Add(-2 * time.Minute)
	it := actionItem("x", time.Now())
	it.Agent = &InboxAgent{State: AgentRunning, SessionID: "s1", MachineID: "server", StartedAt: &started}
	h.inbox.items[it.ID] = it
	h.f.setSessions("server", proto.Session{SessionID: "s1", Status: proto.StatusIdle})
	h.sweepInboxAgents(context.Background(), InboxAgentConfig{}, time.Now())
	if it.Agent.State != AgentFailed || it.Agent.Error != "做完了但没有给出建议" || it.Handling == nil || it.Handling.State != HandlingFailed ||
		it.status() != StatusAction || it.Chain[len(it.Chain)-1].Kind != ChainFailed {
		t.Fatalf("agent = %+v", it.Agent)
	}
}

func TestInboxAgentNoMachineFails(t *testing.T) {
	h := agentHub(t)
	h.f.mu.Lock()
	h.f.hello.Assistant.MachineID = ""
	delete(h.f.sessions, "server")
	h.f.mu.Unlock()
	it := actionItem("x", time.Now())
	it.Agent = &InboxAgent{State: AgentQueued, Task: "t"}
	h.inbox.items[it.ID] = it
	h.startInboxAgents(context.Background(), InboxAgentConfig{Enabled: true})
	// A definite refusal fails at once, with a localized sentence instead of the English error.
	if it.Agent.State != AgentFailed || it.Agent.Attempts != 1 || it.Agent.Error != "没能启动，未开始处理" {
		t.Fatalf("agent = %+v", it.Agent)
	}
}

// definiteStartError separates refusals that cannot have started anything from failures whose result is unknown.
func TestInboxDefiniteStartError(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"no inbox machine", errNoInboxMachine, true},
		{"no device", ErrNoDevice, true},
		{"no machine code", &rpc.Error{Code: proto.CodeNoMachine}, true},
		{"machine pending", &rpc.Error{Code: proto.CodeMachinePending}, true},
		{"claude missing", &rpc.Error{Code: proto.CodeClaudeMissing}, true},
		{"launcher missing", &rpc.Error{Code: proto.CodeLauncherMissing}, true},
		{"node outdated", &rpc.Error{Code: proto.CodeNodeOutdated}, true},
		{"deadline", context.DeadlineExceeded, false},
		{"dropped", rpc.ErrDropped, false},
		{"closed", rpc.ErrClosed, false},
		{"offline", apierr.ErrOffline, false},
		{"timeout code", &rpc.Error{Code: "timeout"}, false},
		{"unreachable code", &rpc.Error{Code: "node_unreachable"}, false},
		{"plain error", errors.New("upstream 502 bad gateway"), false},
		{"empty code", &rpc.Error{Message: "boom"}, false},
	}
	for _, c := range cases {
		if got := definiteStartError(c.err); got != c.want {
			t.Errorf("%s: definite = %v, want %v", c.name, got, c.want)
		}
	}
}

// A start whose result is unknown (a timeout) queues the item again with one more attempt and a backoff, and stores
// no error text for the wait.
func TestInboxAgentStartUnknownResultRetries(t *testing.T) {
	h := agentHub(t)
	h.f.failAssistant = context.DeadlineExceeded
	it := actionItem("x", time.Now())
	it.Agent = &InboxAgent{State: AgentQueued, Task: "t"}
	h.inbox.items[it.ID] = it
	before := time.Now()
	h.startInboxAgents(context.Background(), InboxAgentConfig{Enabled: true})
	a := it.Agent
	if a.State != AgentQueued || a.Attempts != 1 || a.Error != "" || a.SessionID != "" || a.RetryAfter == nil {
		t.Fatalf("agent = %+v", a)
	}
	if got := a.RetryAfter.Sub(before); got < 50*time.Second || got > 70*time.Second {
		t.Fatalf("retry after %s, want about 1m", got)
	}
}

// A round does not start an agent whose backoff has not passed; once it is due, the next round starts it and clears
// the backoff.
func TestInboxAgentRetryWaitsForBackoff(t *testing.T) {
	h := agentHub(t)
	it := actionItem("x", time.Now())
	future := time.Now().Add(time.Hour)
	it.Agent = &InboxAgent{State: AgentQueued, Task: "t", Attempts: 1, RetryAfter: &future}
	h.inbox.items[it.ID] = it
	h.startInboxAgents(context.Background(), InboxAgentConfig{Enabled: true})
	if it.Agent.State != AgentQueued || h.f.sent() != 0 {
		t.Fatalf("started before the backoff passed: %+v sends %d", it.Agent, h.f.sent())
	}
	past := time.Now().Add(-time.Minute)
	it.Agent.RetryAfter = &past
	h.startInboxAgents(context.Background(), InboxAgentConfig{Enabled: true})
	if it.Agent.State != AgentRunning || it.Agent.Attempts != 2 || it.Agent.RetryAfter != nil || it.Agent.SessionID == "" {
		t.Fatalf("agent = %+v", it.Agent)
	}
}

// Three unknown results in a row (waits of about 1m and 5m) fail the item with a localized sentence, never the
// transport error's words.
func TestInboxAgentStartGivesUpAfterThreeAttempts(t *testing.T) {
	h := agentHub(t)
	h.f.failAssistant = rpc.ErrDropped
	it := actionItem("x", time.Now())
	it.Agent = &InboxAgent{State: AgentQueued, Task: "t"}
	h.inbox.items[it.ID] = it
	wantWaits := []time.Duration{time.Minute, 5 * time.Minute}
	for attempt := 1; attempt <= inboxAgentStartMaxAttempts; attempt++ {
		before := time.Now()
		h.startInboxAgents(context.Background(), InboxAgentConfig{Enabled: true})
		if attempt < inboxAgentStartMaxAttempts {
			if it.Agent.State != AgentQueued || it.Agent.Attempts != attempt || it.Agent.RetryAfter == nil {
				t.Fatalf("attempt %d: agent = %+v", attempt, it.Agent)
			}
			if got := it.Agent.RetryAfter.Sub(before); got < wantWaits[attempt-1]-10*time.Second ||
				got > wantWaits[attempt-1]+10*time.Second {
				t.Fatalf("attempt %d: retry after %s, want %s", attempt, got, wantWaits[attempt-1])
			}
			due := before // already in the past by the next round
			it.Agent.RetryAfter = &due
		}
	}
	if it.Agent.State != AgentFailed || it.Agent.Attempts != inboxAgentStartMaxAttempts {
		t.Fatalf("agent = %+v", it.Agent)
	}
	if it.Agent.Error != "多次启动都没成功" || strings.Contains(strings.ToLower(it.Agent.Error), "rpc") {
		t.Fatalf("error = %q", it.Agent.Error)
	}
}

// A coded setup refusal (the launcher is missing) fails immediately instead of burning retries, in plain words.
func TestInboxAgentStartRefusalFails(t *testing.T) {
	h := agentHub(t)
	h.f.failAssistant = &rpc.Error{Code: proto.CodeLauncherMissing, Message: "launcher foo not found in zshrc"}
	it := actionItem("x", time.Now())
	it.Agent = &InboxAgent{State: AgentQueued, Task: "t"}
	h.inbox.items[it.ID] = it
	h.startInboxAgents(context.Background(), InboxAgentConfig{Enabled: true})
	if it.Agent.State != AgentFailed || it.Agent.Attempts != 1 || it.Agent.RetryAfter != nil {
		t.Fatalf("agent = %+v", it.Agent)
	}
	if it.Agent.Error != "没能启动，未开始处理" {
		t.Fatalf("error = %q", it.Agent.Error)
	}
}

// An item filed (replied or done) while its start was in flight never comes back queued: the failed start marks it
// failed, without a backoff.
func TestInboxAgentStartFailureOnAFiledItem(t *testing.T) {
	h := agentHub(t)
	it := actionItem("x", time.Now())
	it.done(time.Now(), "replied")
	a := &InboxAgent{State: AgentRunning, Attempts: 1}
	it.Agent = a
	h.inbox.items[it.ID] = it
	h.inbox.mu.Lock()
	h.requeueAgentStart(context.Background(), a, context.DeadlineExceeded, time.Now(), it.open())
	h.inbox.mu.Unlock()
	if a.State != AgentFailed || a.RetryAfter != nil || a.Error != "没能启动，未开始处理" {
		t.Fatalf("agent = %+v", a)
	}
}

// The retry bookkeeping survives an item's JSON round-trip (stored inbox rows): a restarted manager keeps the
// attempts and the backoff.
func TestInboxAgentRetryFieldsPersist(t *testing.T) {
	retryAfter := time.Now().UTC().Add(5 * time.Minute)
	a := InboxAgent{State: AgentQueued, Attempts: 2, RetryAfter: &retryAfter}
	raw, err := json.Marshal(a)
	if err != nil {
		t.Fatal(err)
	}
	var got InboxAgent
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if got.State != AgentQueued || got.Attempts != 2 || got.RetryAfter == nil ||
		!got.RetryAfter.Equal(retryAfter) {
		t.Fatalf("round-trip lost the retry fields: %+v from %s", got, raw)
	}
}

// A queued item's agent is the assistant's own conversation, named after the item, on the inbox agent's model and the
// assistant's machine; the hub learns it is the AI twin's from the next snapshot.
func TestInboxAgentStarts(t *testing.T) {
	h := agentHub(t)
	it := actionItem("x", time.Now())
	it.Agent = &InboxAgent{State: AgentQueued, Task: "查 1013 是否合入"}
	h.inbox.items[it.ID] = it
	h.startInboxAgents(context.Background(), InboxAgentConfig{Enabled: true})
	if it.Agent.State != AgentRunning || it.Agent.MachineID != "server" || it.Agent.SessionID == "" {
		t.Fatalf("agent = %+v", it.Agent)
	}
	send := h.f.lastSend(t)
	if !strings.Contains(send["text"].(string), "查 1013 是否合入") {
		t.Fatalf("the first message: %v", send)
	}
	snap := h.f.lastSnapshot()
	if snap.Inbox == nil || len(snap.Inbox.Agents) != 1 || snap.Inbox.Agents[0].SessionID != it.Agent.SessionID {
		t.Fatalf("the snapshot's inbox: %+v", snap.Inbox)
	}
	// Named after its item from the start (#3264), not after its first turn by the model.
	h.titles.mu.Lock()
	title := h.titles.m[it.Agent.SessionID]
	h.titles.mu.Unlock()
	if title == nil || title.By != titleByInbox || !strings.Contains(title.Title, " | 收件箱 | ") {
		t.Fatalf("the AI 分身's conversation's title: %+v", title)
	}
}

func TestInboxAgentPrompt(t *testing.T) {
	it := actionItem("x", time.Now())
	it.Title = `群 "A" <b>`
	it.Agent = &InboxAgent{Task: "查 1013 是否合入"}
	p := newInbox().inboxAgentPrompt(it, "小明", nil)
	for _, want := range []string{`title="飞书 · 群 &quot;A&quot; &lt;b&gt;"`, "飞书会话 id：f1", "查 1013 是否合入",
		"inbox_suggest", "张三：1013 合了吗？", "小明", "事件 x"} {
		if !strings.Contains(p, want) {
			t.Errorf("prompt lacks %q:\n%s", want, p)
		}
	}
}

// TestInboxModelsLive runs models 1 and 2 on a sample item against a real gateway:
// PERSONA_LIVE_AI='{"base_url":"…","api_key_file":"…","model":"…"}' go test -run InboxModelsLive -v ./internal/assistant
func TestInboxModelsLive(t *testing.T) {
	raw := os.Getenv("PERSONA_LIVE_AI")
	if raw == "" {
		t.Skip("PERSONA_LIVE_AI not set")
	}
	var cfg ai.Config
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		t.Fatal(err)
	}
	h := agentHub(t)
	h.in.model = ai.New(cfg)
	it := &InboxItem{ID: "x", Kind: KindDM, Last: "12", UpdatedAt: time.Now(), Lark: &InboxLark{FeedID: "f1"},
		Context: "张三（10:02）：你那边 codex 的改动合进 1013 分支了吗？今天下午要封版，合了的话把 MR 发我一下"}
	h.inbox.items[it.ID] = it
	start := time.Now()
	if !h.triageOne(context.Background(), it) || it.Reason == "按规则" {
		t.Fatalf("triage failed: %+v", it)
	}
	t.Logf("model 1 (%s): tier=%s summary=%q reason=%q", time.Since(start).Round(time.Millisecond), it.Tier, it.Summary, it.Reason)
	if it.Tier != TierAction {
		t.Fatalf("tier = %s", it.Tier)
	}
}

// An item about one of the user's tasks (docs/assistant/unified-entry.md): the judge sees the tasks and links the item; the AI
// twin is told the task's progress; its draft puts a card into the task's conversation.
func TestInboxItemAboutATask(t *testing.T) {
	ctx := context.Background()
	s, f := newTaskService(t)
	in := newFakeInbox()
	f.inbox, in.svc = in, s
	h := &inboxHub{Service: s, f: f, in: in}
	h.inboxReady(ctx)
	todo := startedTask(t, s, f)
	model, calls := captureModel(t, `{"handle":true,"task":"回复修复进度","reason":"问进度","task_id":"`+todo.ID+`"}`)
	in.setModel(model)
	it := actionItem("lark:f1:12", time.Now())
	it.Title, it.Sender, it.Preview, it.Source, it.AccountName = "客户端发版群", "陈晓", "登录那个崩溃今天能修好吗？", "lark", "飞书"
	h.inbox.mu.Lock() // the started task's snapshot reads the inbox meanwhile
	h.inbox.items[it.ID] = it
	h.inbox.mu.Unlock()
	it.Agent = &InboxAgent{State: AgentQueued}
	if !h.linkTask(ctx, it, h.activeTasks(ctx)) {
		t.Fatal("not linked")
	}
	if c := onlyCall(t, calls()); !strings.Contains(c.User, todo.ID+"：修登录崩溃") {
		t.Fatalf("the judge was not shown the task:\n%s", c.User)
	}
	if it.Agent == nil || it.Agent.TodoID != todo.ID {
		t.Fatalf("agent = %+v", it.Agent)
	}
	linked := &prompts.InboxTask{ID: todo.ID, Title: "修登录崩溃", State: "在做", Last: "测试通过"}
	if p := h.inbox.inboxAgentPromptFor(it, "示例用户", nil, linked, 1, time.UTC); !strings.Contains(p, "正在做的任务「修登录崩溃」") || !strings.Contains(p, "测试通过") {
		t.Fatalf("the agent is not told the task:\n%s", p)
	}

	// A task id it was not shown is not taken.
	model2, _ := captureModel(t, `{"handle":true,"task":"x","reason":"r","task_id":"todo_made_up"}`)
	in.setModel(model2)
	other := actionItem("lark:f2:12", time.Now())
	h.inbox.mu.Lock()
	h.inbox.items[other.ID] = other
	h.inbox.mu.Unlock()
	other.Agent = &InboxAgent{State: AgentQueued}
	h.linkTask(ctx, other, h.activeTasks(ctx))
	if other.Agent == nil || other.Agent.TodoID != "" {
		t.Fatalf("made-up task linked: %+v", other.Agent)
	}

	// The draft: the card goes into the task's conversation.
	it.Agent.State, it.Agent.SessionID = AgentRunning, "s_agent"
	if out, err := runTool(s, "s_agent", "inbox_draft", map[string]any{"text": "测试已经过了，十点前提 PR"}); err != nil || !strings.Contains(out, "in the user's inbox") {
		t.Fatalf("inbox_draft: %s %v", out, err)
	}
	var cards []FeedItem // its messages (the task's plan and card are there too)
	s.tasks.mu.Lock()
	for _, fi := range s.tasks.feed["conv1"] {
		if fi.Kind == feedMessage {
			cards = append(cards, fi)
		}
	}
	s.tasks.mu.Unlock()
	if len(cards) != 1 || cards[0].Ref != it.ID {
		t.Fatalf("cards = %+v", cards)
	}
	var mb MessageCard
	_ = json.Unmarshal(cards[0].Body, &mb)
	if mb.TodoID != todo.ID || mb.Draft != "测试已经过了，十点前提 PR" || mb.Sender != "陈晓" || mb.Chat != "客户端发版群" {
		t.Fatalf("card = %+v", mb)
	}
}

// The AI twin's first message names the knowledge directory the hub keeps (PERSONA_HOME, kb_dir), not the default
// ~/.persona/data, where it points at the chat's background.
func TestInboxAgentPromptNamesTheKnowledgeDir(t *testing.T) {
	ctx := context.Background()
	h, e := newInboxHubKB(t)
	if err := h.inboxReady(ctx); err != nil {
		t.Fatal(err)
	}
	waitNeedsYouIdle(t, h.Service)
	goldenKB(t, e)
	dir := h.kb.dir
	if dir == "" || strings.Contains(dir, ".persona") {
		t.Fatalf("the test needs a custom knowledge directory: %q", dir)
	}
	it := *goldenItems(t)["mention"]
	it.Agent = &InboxAgent{State: AgentQueued}
	h.inbox.items[it.ID] = &it
	h.startInboxAgents(ctx, InboxAgentConfig{Enabled: true})
	if it.Agent.SessionID == "" {
		t.Fatalf("the agent did not start: %+v", it.Agent)
	}
	text := h.f.lastSend(t)["text"].(string)
	if !strings.Contains(text, "## 知识库里的背景") {
		t.Fatalf("the first message has the knowledge base's part: %q", text)
	}
	if strings.Contains(text, "~/.persona/data") || !strings.Contains(text, "## 知识库里的背景（"+dir+"）") {
		t.Fatalf("the heading should name %s: %q", dir, text)
	}
}

// A pass asked for while one runs is run after it, not dropped: every caller is an event, and nothing else would come
// back for what it queued (a new message for an event's conversation waited for the next event). Still one pass at a
// time, and the calls made meanwhile are one more pass, not one each.
func TestInboxPassAskedDuringOneRunsAfterIt(t *testing.T) {
	h := agentHub(t)
	h.setConfig(func(c *assistantwire.InboxConfig) { c.Agent.Enabled = true })
	ctx := context.Background()
	var passes, running, overlap atomic.Int32
	started, release := make(chan struct{}), make(chan struct{})
	h.inbox.passHook = func() {
		if running.Add(1) > 1 {
			overlap.Add(1)
		}
		defer running.Add(-1)
		if passes.Add(1) == 1 {
			close(started)
			<-release // the first pass is held open
		}
	}
	h.handInbox(ctx)
	<-started
	h.handInbox(ctx) // while it runs: not dropped
	h.handInbox(ctx)
	close(release)
	deadline := time.Now().Add(3 * time.Second)
	for passes.Load() < 2 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	time.Sleep(50 * time.Millisecond) // a third pass would have started by now
	if n := passes.Load(); n != 2 || overlap.Load() != 0 {
		t.Fatalf("%d passes (%d overlapping), want 2 one after the other", n, overlap.Load())
	}
	// Idle again: a call runs a pass of its own.
	h.handInbox(ctx)
	for passes.Load() < 3 && time.Now().Before(deadline.Add(3*time.Second)) {
		time.Sleep(5 * time.Millisecond)
	}
	if n := passes.Load(); n != 3 {
		t.Fatalf("a call after the passes ended ran %d", n-2)
	}
}
