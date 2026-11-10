package assistant

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/proto"
)

// releaseTask is a dispatched task that asks for the release itself (as the main conversation writes one).
const releaseTask = "评审通过后，先跑发布质检，再对任务整体合码上线。跟进发布流水线到完成；需要人审批的卡点如实报给主会话。" +
	"起草给产品的飞书回复，交回主会话，不要发送。"

const releaseWait = `{"phase":"blocked","on":"external","line":"等质检","wait":{"what":"单机房发布质检跑完","then":"推进后续发布验证与节点跟踪","check_minutes":5}}`

// The agent's todo asks for the release: waiting for its quality check is no card for anyone, supervised task or
// not (#3246). It is woken as any wait, the wake says its task authorized the step, and the trace is kept.
func TestTaskAuthorizedWaitIsNoCard(t *testing.T) {
	for _, supervised := range []bool{true, false} {
		s, f := workService(t)
		td := Todo{ID: "todo_release", Title: "名单创建文档合码上线", Notes: releaseTask,
			Agents: []TodoAgent{{MachineID: "box", SessionID: "s1"}}}
		if supervised {
			td.Supervision = &Supervision{Supervisor: "conv-main", DoneWhen: []string{"已上线"}}
		}
		putTodo(s, td)
		f.triage = releaseWait
		f.setSessions("box", stopped("s1", "单机房质检还在跑，跑完我接着推进后续发布节点"))
		triage(t, s, "s1")
		e := entryOf(t, s, "s1")
		if len(s.workCards()) != 0 || e.InteractionID != "" || e.WakeAt == nil || e.Work.On != proto.WaitExternal {
			t.Fatalf("supervised=%v: carded: %+v, cards %d", supervised, e, len(s.workCards()))
		}
		if e.Work.Line != "等「单机房发布质检跑完」，任务已授权，好了就去「推进后续发布验证与节点跟踪」" {
			t.Fatalf("supervised=%v: line %q", supervised, e.Work.Line)
		}
		if n := workEvents(s, "auto_approved"); n != 1 {
			t.Fatalf("supervised=%v: auto_approved events %d", supervised, n)
		}
		s.workTick(context.Background(), time.Now().Add(6*time.Minute))
		got := f.sentToAgents()
		if len(got) != 1 || !strings.Contains(got[0], "你的任务里已经交代要做这一步，Persona 已按任务授权放行：等好了就去「推进后续发布验证与节点跟踪」") {
			t.Fatalf("supervised=%v: wake %v", supervised, got)
		}

		// What it waits for moves on (the next release node) and the model asks the user: still no card.
		s.workStarted(context.Background(), "box", "s1", time.Now())
		f.triage = `{"phase":"blocked","on":"user","line":"等灰度","wait":{"what":"灰度节点跑完","then":"推进全量发布","check_minutes":10,"risky":true},
			"questions":[{"question":"灰度好了要推进全量发布吗？","options":[{"label":"推进"},{"label":"先不"}]}]}`
		next := stopped("s1", "灰度在跑，好了推全量")
		next.UpdatedAt = turnEnd.Add(30 * time.Minute)
		f.setSessions("box", next)
		triage(t, s, "s1")
		e = entryOf(t, s, "s1")
		if len(s.workCards()) != 0 || e.InteractionID != "" || e.WakeAt == nil || e.Work.On != proto.WaitExternal {
			t.Fatalf("supervised=%v: next node carded: %+v", supervised, e)
		}
	}
}

// Irreversible or outward steps stay the user's whatever the task says; a step its task does not ask for (or says not
// to take), or an agent with no todo, gets no card and no wake: the hub only cards a question the Agent wrote (#3290).
func TestUnauthorizedWaitIsNoCardNoWake(t *testing.T) {
	for _, c := range []struct{ name, notes, reply string }{
		{"send", releaseTask, `{"phase":"blocked","on":"external","line":"等上线","wait":{"what":"上线完成","then":"回复产品已经上线","check_minutes":5}}`},
		{"delete", "清理测试泳道，删除旧分支，然后发布。", `{"phase":"blocked","on":"external","line":"等","wait":{"what":"发布完成","then":"删除旧分支","check_minutes":5}}`},
		{"production", releaseTask, `{"phase":"blocked","on":"external","line":"等","wait":{"what":"发布完成","then":"改生产 TCC 打开开关","check_minutes":5}}`},
		{"not asked", "修登录页的样式，跑通单测。", `{"phase":"blocked","on":"external","line":"等 CI","wait":{"what":"CI 通过","then":"合入 master","check_minutes":5}}`},
		{"told not to", "改完提 MR，不要合入，等用户看过。", `{"phase":"blocked","on":"external","line":"等 CI","wait":{"what":"CI 通过","then":"合入 master","check_minutes":5}}`},
		{"no todo", "", releaseWait},
	} {
		s, f := workService(t)
		if c.notes != "" {
			putTodo(s, Todo{ID: "todo_x", Title: "任务", Notes: c.notes, Supervision: &Supervision{Supervisor: "conv-main"},
				Agents: []TodoAgent{{MachineID: "box", SessionID: "s1"}}})
		}
		f.triage = c.reply
		f.setSessions("box", stopped("s1", "等着"))
		triage(t, s, "s1")
		e := entryOf(t, s, "s1")
		if len(s.workCards()) != 0 || e.InteractionID != "" || e.WakeAt != nil || e.Work.On == proto.WaitUser {
			t.Fatalf("%s: carded or woken without a question: %+v", c.name, e)
		}
		if n := workEvents(s, "auto_approved"); n != 0 {
			t.Fatalf("%s: auto_approved %d", c.name, n)
		}
	}
}

func TestTaskAuthorizes(t *testing.T) {
	for _, c := range []struct {
		text, step string
		want       bool
	}{
		{releaseTask, "推进后续发布验证与节点跟踪", true},
		{releaseTask, "合码上线", true},
		{"修 bug，提 MR", "push 到分支", true}, // its own reversible write
		{"修 bug，提 MR", "部署到 PPE", false},
		{"不要发布，交回主会话", "发布", false},
		{"先别部署，之后部署到 PPE 验证", "部署到 PPE", true}, // one occurrence is not negated
		{releaseTask, "发送给产品", false},
		{releaseTask, "跑一下单测", false}, // nothing risky: no approval needed
	} {
		if got := taskAuthorizes(c.text, c.step); got != c.want {
			t.Errorf("taskAuthorizes(%q, %q) = %v", c.text, c.step, got)
		}
	}
}
