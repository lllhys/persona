package assistant

import (
	"context"
	"strings"
	"unicode/utf8"

	"github.com/nwverse/persona/server/internal/i18n"
	"github.com/nwverse/persona/server/internal/proto"
)

// A wait whose next step is risky (riskyWords) used to be a card to the user every time ("it waits for X, then does
// Y: go ahead?"), also when the task the agent works on asked for Y in so many words, and again at each change of what
// it waits for (#3246). Now the hub lets it go on by itself when the agent's open todo authorizes the step, and keeps
// a trace (the work event auto_approved, the work line); the wake says the task authorized it. What no task text can
// authorize — reaching other people, deleting, spending, changing production — stays the user's, and so does any step
// of an agent with no todo (docs/assistant/agent-work.md "Safety boundary").

// userOnlyWords mark a step only the user may let an agent take, whatever its task says: it is seen by other people,
// deletes, costs money or changes production.
var userOnlyWords = []string{"发消息", "回复", "评论", "comment", "send", "发送", "删除", "delete", "购买", "付费", "续费", "充值", "生产", "production"} // i18n-ignore: matched

// ownWriteWords mark a reversible write of the agent's own (pushing its branch): a dispatched task may do it without
// naming it.
var ownWriteWords = []string{"push", "推送", "推到"} // i18n-ignore: matched

// negations before a word in the task text say not to do it ("不要发布").
var negations = []string{"不要", "不用", "不得", "禁止", "别", "勿", "不", "don't", "do not", "never", "not "} // i18n-ignore: matched

// taskAuthorizes: todo text (its title and notes, as the user or the main conversation wrote it) authorizes step:
// none of step's risky words is the user's alone, and each one that is not the agent's own reversible write is in the
// text, not right after a negation.
func taskAuthorizes(text, step string) bool {
	words := riskyWordsIn(step)
	if len(words) == 0 {
		return false
	}
	text = strings.ToLower(text)
	for _, w := range words {
		switch {
		case containsAny(w, userOnlyWords):
			return false
		case containsAny(w, ownWriteWords):
		case !mentionedToDo(text, w):
			return false
		}
	}
	return true
}

func containsAny(w string, list []string) bool {
	for _, x := range list {
		if strings.Contains(w, x) || strings.Contains(x, w) {
			return true
		}
	}
	return false
}

// mentionedToDo: word is in text (lower case) at least once with no negation in the few runes before it.
func mentionedToDo(text, word string) bool {
	for i := 0; ; {
		j := strings.Index(text[i:], word)
		if j < 0 {
			return false
		}
		at := i + j
		before := text[:at]
		for n := 0; n < 8 && before != ""; n++ { // at most 8 runes back, within its clause
			r, size := utf8.DecodeLastRuneInString(before)
			if strings.ContainsRune("，。；：！？,.;:!?\n", r) { // i18n-ignore: clause punctuation
				break
			}
			before = before[:len(before)-size]
		}
		if !containsAnyText(text[len(before):at], negations) {
			return true
		}
		i = at + len(word)
	}
}

func containsAnyText(t string, list []string) bool {
	for _, x := range list {
		if strings.Contains(t, x) {
			return true
		}
	}
	return false
}

// approveByTask lets w's risky next step go ahead without a card when the agent's open todo authorizes it
// (taskAuthorizes) and the hub may wake it (wake: the switch is on): it is approved as the user's "Do it once it's
// ready" would be, a card the triage made of it is taken back to a wait, and the trace kept. False: nothing is
// approved; the caller neither wakes a risky step nor opens a card of its own (#3290).
func (s *Service) approveByTask(ctx context.Context, k [2]string, w *proto.WorkState, wake bool) bool {
	if !wake || w.Phase != proto.WorkBlocked || w.Wait == "" || w.Link != nil || w.ResetsAt != nil || !risky(w.Then) {
		return false
	}
	switch w.On {
	case proto.WaitExternal, proto.WaitAgent:
	case proto.WaitUser:
		if len(w.Questions) > 1 { // a second question is the user's all the same
			return false
		}
	default:
		return false
	}
	t, ok := s.todoOfSession(ctx, k[0], k[1])
	if !ok || !taskAuthorizes(t.Title+"\n"+t.Notes, w.Then) {
		return false
	}
	s.work.mu.Lock()
	s.work.init()
	s.work.approved[k] = approval{wait: w.Wait, then: w.Then, task: t.ID}
	s.work.mu.Unlock()
	if w.On == proto.WaitUser {
		w.On, w.Questions = proto.WaitExternal, nil
		if w.Agent != nil {
			w.On = proto.WaitAgent
		}
	}
	w.Line = clip(oneLine(workT(s.lang(ctx), "task_approved_line", i18n.Args{"wait": w.Wait, "then": w.Then})), 60)
	s.workEvent(k[0], k[1], "auto_approved", map[string]any{"todo": t.ID, "supervised": t.Supervision != nil, "wait": w.Wait, "then": w.Then})
	return true
}
