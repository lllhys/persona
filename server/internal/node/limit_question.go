package node

import (
	"context"
	"strings"
	"time"

	"github.com/nwverse/persona/server/internal/proto"
)

// limitQuestionWait bounds how long a switch waits, once it answered Claude Code's usage-limit question, for the
// question to go and the agent to be idle.
const limitQuestionWait = 10 * time.Second

// limitQuestionStop is the first choice Claude Code offers when a turn stops on a subscription usage limit:
//
//	What do you want to do?
//	❯ 1. Stop and wait for limit to reset
//	  2. Wait here, then continue automatically at <time>
//	  3. Switch to usage credits
const limitQuestionStop = "Stop and wait for limit to reset"

// limitQuestionKeys answers that question with its first choice when the screen shows it with that choice selected;
// nil for any other screen (another dialog, or another choice selected: someone is answering it at the computer).
func limitQuestionKeys(screen string) []string {
	for line := range strings.SplitSeq(screen, "\n") {
		l := strings.TrimSpace(line)
		if strings.HasPrefix(l, "❯") && strings.Contains(l, limitQuestionStop) {
			return []string{"enter"}
		}
	}
	return nil
}

// leaveLimitQuestion is the start of a launcher switch of session id in pane: an agent that stopped on its usage limit
// shows Claude Code's question about it, and the terminal host reports it blocked, so the switch would wait for good.
// The switch is what moves it off the limit, so the question is answered with stop and wait (never the credits, never
// the wait in the terminal), and the switch goes on once the question is gone and the agent is idle (#6237). Any other
// screen is left as it is: the switch's own checks decide.
func (n *Node) leaveLimitQuestion(ctx context.Context, id, pane string) {
	screen, err := n.readPane(ctx, pane, "visible", 60)
	if err != nil {
		return
	}
	keys := limitQuestionKeys(screen)
	if keys == nil {
		return
	}
	n.logf("session %s: Claude Code asks what to do about its usage limit; the launcher switch answers stop and wait", id)
	if err := n.sendKeys(ctx, pane, keys...); err != nil {
		n.logf("session %s: answering the usage-limit question: %v", id, err)
		return
	}
	deadline := time.Now().Add(limitQuestionWait)
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return
		case <-time.After(300 * time.Millisecond):
		}
		if s, err := n.readPane(ctx, pane, "visible", 60); err != nil || limitQuestionKeys(s) != nil {
			continue
		}
		_ = n.refreshHosts(ctx)
		if a, ok := n.agent(id); !ok || a.AgentStatus != proto.StatusBlocked {
			return
		}
	}
}
