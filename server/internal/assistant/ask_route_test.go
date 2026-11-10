package assistant

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/db"
	"github.com/nwverse/persona/server/internal/prompts"
	"github.com/nwverse/persona/server/internal/proto"
)

// routeEnv is a manager with one task, "修登录崩溃", handed out by leader conversation lead1 (unless leaders is false: then
// the main conversation conv1 supervises it and no leader is configured), its agents s1 and s2 on mac.
type routeEnv struct {
	*fleetTest
	todo   Todo
	agents []proto.Session // mac's sessions
}

func newRouteEnv(t *testing.T, st Store, leaders bool) *routeEnv {
	t.Helper()
	var ft *fleetTest
	if st == nil {
		ft = newFleetTest(t)
	} else {
		ft = newFleetTestOn(t, st)
	}
	s, f := ft.s, ft.f
	shortSupervise(t, s)
	old := askHoldFor
	askHoldFor = time.Hour // tests move cards up by hand (askTick)
	t.Cleanup(func() { askHoldFor = old })
	ctx := context.Background()
	f.hello.Assistant.MachineID = "server"
	f.known["conv1"], f.known["lead1"] = true, true
	ft.d.add("server", "Server")
	const home = "/home/u/.persona/assistant" // where the node's sessions are the assistant's conversations
	convs := []proto.Session{{SessionID: "conv1", MachineID: "server", Cwd: home, Assistant: true, Status: proto.StatusIdle},
		{SessionID: "lead1", MachineID: "server", Cwd: home, Assistant: true, Status: proto.StatusIdle}}
	ft.sessions("server", convs...)
	f.setSessions("server", convs...)
	agents := []proto.Session{
		{SessionID: "worker-1", MachineID: "mac", Title: "修登录", Cwd: "/code/app", Status: proto.StatusBlocked, UpdatedAt: time.Now().UTC()},
		{SessionID: "worker-2", MachineID: "mac", Title: "补测试", Cwd: "/code/app", Status: proto.StatusBlocked, UpdatedAt: time.Now().UTC()}}
	ft.sessions("mac", agents...)
	f.setSessions("mac", agents...)
	supervisor := "conv1"
	setSettings(t, s, func(v *Settings) {
		v.Main, v.MachineID = "conv1", "server"
		if leaders {
			v.Leaders, supervisor = []string{"lead1"}, "lead1"
		} else {
			off := false
			v.MainTasksQuiet = &off // its card is pushed, as any non-quiet task's
		}
	})
	todo, err := s.createTodo(ctx, "修登录崩溃", "iPad 横屏时登录页崩溃", supervisor, "")
	if err != nil {
		t.Fatal(err)
	}
	s.superviseTodo(ctx, todo.ID, supervisor, []string{"PR 合并到 main"}, 30)
	for _, a := range agents {
		if err := s.linkTodoAgent(ctx, todo.ID, "mac", a.SessionID, a.Title, a.Cwd, "claude"); err != nil {
			t.Fatal(err)
		}
	}
	return &routeEnv{fleetTest: ft, todo: todoNow(s, todo.ID), agents: agents}
}

func askQuestion(id, session string) proto.Interaction {
	return proto.Interaction{ID: id, MachineID: "mac", SessionID: session, Kind: proto.IxQuestion,
		Questions: []proto.Question{{Question: "用哪个分支？", Options: []proto.QuestionOption{{Label: "main"}, {Label: "feat/x"}}}},
		CreatedAt: time.Now().UTC(), ExpiresAt: time.Now().Add(time.Hour).UTC()}
}

func askPermission(id, session string) proto.Interaction {
	return proto.Interaction{ID: id, MachineID: "mac", SessionID: session, Kind: proto.IxPermission, ToolName: "Bash",
		ToolInput: []byte(`{"command":"git push origin feat/x"}`), CreatedAt: time.Now().UTC(), ExpiresAt: time.Now().Add(time.Hour).UTC()}
}

// raise asks ix as a hook would; the channel has its answer once it has one.
func (e *routeEnv) raise(ix proto.Interaction) <-chan proto.Resolution {
	out := make(chan proto.Resolution, 1)
	go func() {
		res, _ := e.s.waitInteraction(context.Background(), ix)
		out <- res
	}()
	e.waitPendingID(ix.ID)
	return out
}

func (e *routeEnv) waitPendingID(id string) {
	e.t.Helper()
	waitFor(e.t, "card "+id, func() bool { _, ok := e.s.pendingIx(id); return ok })
}

// pushesOf counts the pushes of the cards ids.
func (e *routeEnv) pushesOf(ids ...string) int {
	n := 0
	for _, p := range e.f.pushed() {
		for _, id := range ids {
			if p.Interaction != nil && p.Interaction.ID == id {
				n++
			}
		}
	}
	return n
}

// lastShown is the card id as the apps were told of it last.
func (e *routeEnv) lastShown(id string) proto.Interaction {
	df := &delegateFleet{fleetTest: e.fleetTest}
	list := df.shownAs(id)
	if len(list) == 0 {
		e.t.Fatalf("card %s was never shown", id)
	}
	return list[len(list)-1]
}

// noticeWith waits for a message into conversation to that says want, and answers it.
func (e *routeEnv) noticeWith(to, want string) string {
	e.t.Helper()
	var got string
	waitFor(e.t, to+" told "+want, func() bool {
		for _, n := range e.f.noticesTo(to) {
			if strings.Contains(n, want) {
				got = n
				return true
			}
		}
		return false
	})
	return got
}

// settle waits until the supervision messages due have gone out: nothing pending and no digest armed, then the one
// being delivered (sendDigest holds dedupMu while it delivers) done.
func (e *routeEnv) settle() {
	e.t.Helper()
	b := &e.s.supervise
	waitFor(e.t, "the supervision messages due to go out", func() bool {
		b.mu.Lock()
		defer b.mu.Unlock()
		return len(b.pending) == 0 && b.timer == nil
	})
	b.dedupMu.Lock()
	b.dedupMu.Unlock()
}

// A worker's question goes to its leader, not the user: no push, the card shown as delegating, the main
// conversation not told.
func TestAskGoesToTheLeaderFirst(t *testing.T) {
	e := newRouteEnv(t, nil, true)
	e.raise(askQuestion("ix1", "worker-1"))
	n := e.noticeWith("lead1", "interaction_id=ix1")
	for _, want := range []string{"先交给你处理，用户还没收到", "用哪个分支？", "machine=Mac", "session_id=worker-1", "ask_escalate", "自动上交给主对话"} {
		if !strings.Contains(n, want) {
			t.Errorf("the leader's message lacks %q:\n%s", want, n)
		}
	}
	e.settle()
	if got := e.pushesOf("ix1"); got != 0 {
		t.Fatalf("the user was pushed %d times", got)
	}
	if !e.lastShown("ix1").Delegating {
		t.Fatal("the card is not shown as delegating")
	}
	e.s.chans.mu.Lock()
	carded := e.s.chans.announced["ix1"]
	e.s.chans.mu.Unlock()
	if carded {
		t.Fatal("the Feishu bot and the channels were given the card")
	}
	if got := e.f.noticesTo("conv1"); len(got) != 0 {
		t.Fatalf("the main conversation was told: %q", got)
	}
}

// The leader answers the question: the agent gets the answer, nothing reaches the user, and the answer is the task's
// decision (the leader's call, not the user's).
func TestLeaderAnswersAnAsk(t *testing.T) {
	database := todoTestDB(t)
	e := newRouteEnv(t, database, true)
	got := e.raise(askQuestion("ix1", "worker-1"))
	e.noticeWith("lead1", "interaction_id=ix1")
	if _, err := runJSON(t, e.s, "conv2", "interaction_answer", map[string]any{"machine": "mac", "session_id": "worker-1",
		"interaction_id": "ix1", "answers": map[string]any{"用哪个分支？": "main"}}); err != nil {
		t.Fatalf("another conversation may still answer a question as before: %v", err)
	}
	if res := answerOf(t, got); res.Answers["用哪个分支？"] != "main" || res.AnsweredBy != proto.AnsweredByAssistant {
		t.Fatalf("the agent got %+v", res)
	}
	// conv2 is not its rung: no decision of its call.
	if list, _ := database.ListTodoDecisions(context.Background(), e.todo.ID, false); len(list) != 0 {
		t.Fatalf("an answer off the ladder became a decision: %+v", list)
	}

	got = e.raise(askQuestion("ix2", "worker-1"))
	e.noticeWith("lead1", "interaction_id=ix2")
	res, err := runJSON(t, e.s, "lead1", "interaction_answer", map[string]any{"machine": "mac", "session_id": "worker-1",
		"interaction_id": "ix2", "answers": map[string]any{"用哪个分支？": "feat/x"}, "reason": "任务就在 feat/x 上"})
	if err != nil || !strings.Contains(res, "feat/x") {
		t.Fatalf("the leader's answer: %s %v", res, err)
	}
	if r := answerOf(t, got); r.Answers["用哪个分支？"] != "feat/x" || r.Reason != "任务就在 feat/x 上" {
		t.Fatalf("the agent got %+v", r)
	}
	var list []db.TodoDecisionRow
	waitFor(t, "the decision", func() bool {
		list, _ = database.ListTodoDecisions(context.Background(), e.todo.ID, false)
		return len(list) == 1
	})
	d := list[0]
	if d.Question != "用哪个分支？" || d.Choice != "feat/x" || d.Source != db.DecisionAgent || d.SourceSession != "lead1" ||
		d.SourceMessage != "ix2" || d.Reason != "任务就在 feat/x 上" {
		t.Fatalf("decision = %+v", d)
	}
	e.settle()
	if n := e.pushesOf("ix1", "ix2"); n != 0 {
		t.Fatalf("the user was pushed %d times", n)
	}
}

// The leader cannot approve a tool; it escalates: the user gets one push with its reason and recommendation, the main
// conversation an escalated entry, and the user's approval goes to the agent and is kept as the task's decision.
func TestLeaderEscalatesAnApproval(t *testing.T) {
	database := todoTestDB(t)
	e := newRouteEnv(t, database, true)
	got := e.raise(askPermission("ix1", "worker-1"))
	n := e.noticeWith("lead1", "interaction_id=ix1")
	if !strings.Contains(n, "，权限，session_id=worker-1") || !strings.Contains(n, "Bash") {
		t.Fatalf("the leader's message:\n%s", n)
	}
	if _, err := runJSON(t, e.s, "lead1", "interaction_answer", map[string]any{"machine": "mac", "session_id": "worker-1",
		"interaction_id": "ix1", "answers": map[string]any{"x": "y"}}); err == nil || !strings.Contains(err.Error(), "only the user") {
		t.Fatalf("the leader approved a tool: %v", err)
	}
	if _, err := runJSON(t, e.s, "conv2", "ask_escalate", map[string]any{"todo_id": e.todo.ID, "reason": "x"}); err == nil {
		t.Fatal("a conversation not supervising the task escalated its cards")
	}
	res, err := runJSON(t, e.s, "lead1", "ask_escalate", map[string]any{"todo_id": e.todo.ID,
		"reason": "要推到远端分支，需要你同意", "recommend": "允许"})
	if err != nil || !strings.Contains(res, "1 card(s)") {
		t.Fatalf("escalate: %s %v", res, err)
	}
	waitFor(t, "the push", func() bool { return e.pushesOf("ix1") == 1 })
	shown := e.lastShown("ix1")
	if shown.Delegating || shown.Delegate == nil || shown.Delegate.Reason != "要推到远端分支，需要你同意" || shown.Delegate.Recommend != "允许" {
		t.Fatalf("the user's card: %+v %+v", shown, shown.Delegate)
	}
	m := e.noticeWith("conv1", "负责它的 leader 对话判断要用户决定")
	for _, want := range []string{"它的 Agent 在等的卡片已经发给用户", "要推到远端分支", "leader 推荐：允许", "interaction_id=ix1", "leader 升级上来的"} {
		if !strings.Contains(m, want) {
			t.Errorf("the main conversation's message lacks %q:\n%s", want, m)
		}
	}
	if _, err := runJSON(t, e.s, "lead1", "ask_escalate", map[string]any{"todo_id": e.todo.ID, "reason": "again"}); err == nil {
		t.Fatal("escalated again what the user was told of")
	}

	yes := true
	if err := e.s.resolve("ix1", proto.Resolution{Allow: &yes}); err != nil {
		t.Fatal(err)
	}
	if r := answerOf(t, got); r.Allow == nil || !*r.Allow {
		t.Fatalf("the agent got %+v", r)
	}
	var list []db.TodoDecisionRow
	waitFor(t, "the decision", func() bool {
		list, _ = database.ListTodoDecisions(context.Background(), e.todo.ID, false)
		return len(list) == 1
	})
	if d := list[0]; d.Source != db.DecisionCard || d.SourceMessage != "ix1" || d.SourceSession != "worker-1" || d.Question == "" || d.Choice == "" {
		t.Fatalf("decision = %+v", d)
	}
	e.settle()
	if n := e.pushesOf("ix1"); n != 1 {
		t.Fatalf("pushed %d times", n)
	}
}

// Two cards of one task, from two of its agents, are one entry for the leader and one push for the user.
func TestAsksOfOneTaskRingAsOne(t *testing.T) {
	e := newRouteEnv(t, nil, true)
	e.raise(askQuestion("ix1", "worker-1"))
	e.raise(askPermission("ix2", "worker-2"))
	n := e.noticeWith("lead1", "interaction_id=ix2")
	if !strings.Contains(n, "interaction_id=ix1") || strings.Count(n, "任务「修登录崩溃」") != 1 {
		t.Fatalf("not one entry with both cards:\n%s", n)
	}
	if _, err := runJSON(t, e.s, "lead1", "ask_escalate", map[string]any{"todo_id": e.todo.ID, "reason": "两件事要你定"}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the push", func() bool { return e.pushesOf("ix1", "ix2") == 1 })
	e.settle()
	if n := e.pushesOf("ix1", "ix2"); n != 1 {
		t.Fatalf("%d pushes for one escalation", n)
	}
	for _, id := range []string{"ix1", "ix2"} {
		if ix := e.lastShown(id); ix.Delegating || ix.Delegate == nil {
			t.Fatalf("%s not handed to the user: %+v", id, ix)
		}
	}
}

// A card nobody handles goes up a rung at a time: from the leader to the main conversation, then to the user.
func TestUnhandledAskGoesUp(t *testing.T) {
	e := newRouteEnv(t, nil, true)
	e.raise(askPermission("ix1", "worker-1"))
	e.noticeWith("lead1", "interaction_id=ix1")
	e.s.askTick(time.Now().Add(2 * askHoldFor))
	m := e.noticeWith("conv1", "interaction_id=ix1")
	for _, want := range []string{"下一层没有及时处理，上交到你这里", "自动上交给用户"} {
		if !strings.Contains(m, want) {
			t.Errorf("the main conversation's message lacks %q:\n%s", want, m)
		}
	}
	if strings.Contains(m, "你是这些任务的 leader 对话") {
		t.Fatalf("the main conversation is told it is the leader:\n%s", m)
	}
	e.settle()
	if n := e.pushesOf("ix1"); n != 0 {
		t.Fatalf("the user was pushed before the main conversation had its time: %d", n)
	}
	e.s.askTick(time.Now().Add(4 * askHoldFor))
	waitFor(t, "the push", func() bool { return e.pushesOf("ix1") == 1 })
	if ix := e.lastShown("ix1"); ix.Delegating {
		t.Fatal("still shown as delegating")
	}
}

// Without leaders nothing changes: the card is the user's at once.
func TestAskWithoutLeadersIsTheUsers(t *testing.T) {
	e := newRouteEnv(t, nil, false)
	e.raise(askQuestion("ix1", "worker-1"))
	waitFor(t, "the push", func() bool { return e.pushesOf("ix1") == 1 })
	if e.lastShown("ix1").Delegating {
		t.Fatal("shown as delegating")
	}
	if _, err := runJSON(t, e.s, "conv1", "ask_escalate", map[string]any{"todo_id": e.todo.ID, "reason": "x"}); err == nil {
		t.Fatal("ask_escalate works without leaders")
	}
	e.settle()
	for _, to := range []string{"conv1", "lead1"} {
		for _, n := range e.f.noticesTo(to) {
			if strings.Contains(n, "interaction_id=ix1") {
				t.Fatalf("%s was handed the card:\n%s", to, n)
			}
		}
	}
}

// What a leader reads of the cards handed to it, and what the main conversation reads of the leader's escalation.
func TestGoldenSuperviseAsk(t *testing.T) {
	golden(t, "supervise.ask", prompts.RenderSupervise(prompts.Supervise{Leader: true, Asked: true, AskMinutes: 5,
		Entries: []prompts.SuperviseEntry{{ID: "todo_1", Title: "修登录崩溃", Kind: sigAsk, DoneWhen: []string{"PR 合并到 main"},
			Machine: "box", Session: "a1b2c3d4", Last: "改好了，要推到远端",
			Asks: []prompts.SuperviseAsk{
				{ID: "ix_1", Kind: proto.IxQuestion, Machine: "box", Session: "a1b2c3d4", Text: "- 用哪个分支？（选项：main / feat/x）"},
				{ID: "ix_2", Kind: proto.IxPermission, Machine: "box", Session: "e5f6a7b8", Text: "允许运行 Bash：git push", Raised: true}},
			Decisions: "- 用哪个分支？→ feat/x（用户在卡片上选的）"}}}))
	golden(t, "supervise.ask_escalated", prompts.RenderSupervise(prompts.Supervise{Escalated: true,
		Entries: []prompts.SuperviseEntry{{ID: "todo_1", Title: "修登录崩溃", Kind: sigEscalated, Detail: "要推到远端分支，需要你同意",
			Recommend: "允许", Asks: []prompts.SuperviseAsk{{ID: "ix_2", Kind: proto.IxPermission, Machine: "box", Session: "e5f6a7b8",
				Text: "允许运行 Bash：git push"}}}}}))
}

// With leaders configured, a quiet task of the main conversation's still has its card go to the main conversation
// first; past its time the card only goes on the apps: a quiet task's card is never pushed unless the main
// conversation escalates it.
func TestQuietTaskAskIsNotPushedOnTimeout(t *testing.T) {
	e := newRouteEnv(t, nil, true)
	ctx := context.Background()
	w := proto.Session{SessionID: "worker-3", MachineID: "mac", Title: "整理材料", Cwd: "/code/doc", Status: proto.StatusBlocked, UpdatedAt: time.Now().UTC()}
	list := append(append([]proto.Session(nil), e.agents...), w)
	e.sessions("mac", list...)
	e.f.setSessions("mac", list...)
	quiet, err := e.s.createTodo(ctx, "整理材料", "", "conv1", "")
	if err != nil || !quiet.Quiet {
		t.Fatalf("not a quiet task: %+v %v", quiet, err)
	}
	e.s.superviseTodo(ctx, quiet.ID, "conv1", nil, 0)
	if err := e.s.linkTodoAgent(ctx, quiet.ID, "mac", w.SessionID, w.Title, w.Cwd, "claude"); err != nil {
		t.Fatal(err)
	}
	e.raise(askQuestion("ix1", "worker-3"))
	n := e.noticeWith("conv1", "interaction_id=ix1")
	if !strings.Contains(n, "自动上交给用户") {
		t.Fatalf("the main conversation's message:\n%s", n)
	}
	e.s.askTick(time.Now().Add(2 * askHoldFor))
	waitFor(t, "the card the user's", func() bool { return !e.lastShown("ix1").Delegating })
	e.settle()
	if n := e.pushesOf("ix1"); n != 0 {
		t.Fatalf("a quiet task's card was pushed %d times", n)
	}
}

// A card the assistant is deciding on for the user (agents.delegate) is not the supervisor's to escalate: the
// delegation tells the user itself, once.
func TestEscalateLeavesADelegatedCard(t *testing.T) {
	e := newRouteEnv(t, nil, true)
	on := true
	e.f.hello.Agents.Delegate.Enabled = &on
	release := make(chan struct{})
	e.s.delegateHook = func(context.Context, DelegateParams) (DelegateResult, error) {
		<-release
		return DelegateResult{Ask: &proto.Delegate{Reason: "要你定"}}, nil
	}
	// Waiting for the user's decision already: its cards are not routed.
	if _, _, err := e.s.updateSupervision(context.Background(), e.todo.ID, func(v *Supervision) bool { v.State = reviewNeedsUser; return true }); err != nil {
		t.Fatal(err)
	}
	e.raise(askQuestion("ix1", "worker-1"))
	waitFor(t, "the delegation", func() bool { return e.lastShown("ix1").Delegating })
	if _, err := runJSON(t, e.s, "lead1", "ask_escalate", map[string]any{"todo_id": e.todo.ID, "reason": "x"}); err == nil {
		t.Fatal("escalated a card the assistant is deciding on")
	}
	close(release)
	waitFor(t, "the push", func() bool { return e.pushesOf("ix1") == 1 })
	e.settle()
	if n := e.pushesOf("ix1"); n != 1 {
		t.Fatalf("pushed %d times", n)
	}
}
