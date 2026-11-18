//go:build !windows

package node

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/nodeproto"
	"github.com/nwverse/persona/server/internal/proto"
	"github.com/nwverse/persona/server/internal/screen"
)

// multiAsk is an AskUserQuestion of four questions, one of them multi-select, the shape Claude Code writes (made-up
// words, the keys in the order the desktop app's Claude Code writes them).
const multiAsk = `{"questions":[` +
	`{"header":"Source","multiSelect":false,"options":[{"description":"The app's own setting","label":"App"},{"description":"The account's","label":"Account"}],"question":"Which setting wins?"},` +
	`{"header":"Folding","multiSelect":false,"options":[{"description":"One line","label":"Fold"},{"description":"All of it","label":"Expand"}],"question":"How are long rows shown?"},` +
	`{"header":"Badges","multiSelect":true,"options":[{"label":"Unread"},{"label":"Mentions"},{"label":"Drafts"}],"question":"Which counts show?"},` +
	`{"header":"Alerts","multiSelect":false,"options":[{"label":"Always"},{"label":"Never"}],"question":"Alert on a mention?"}]}`

func askCall(id, toolUse, input string) string {
	return `{"type":"assistant","uuid":"a-` + toolUse + `","timestamp":"2026-11-04T09:00:09Z","sessionId":"` + id + `","message":{"role":"assistant",` +
		`"content":[{"type":"tool_use","id":"` + toolUse + `","name":"AskUserQuestion","input":` + input + `}]}}`
}

func toolResult(id, toolUse, text string) string {
	return `{"type":"user","uuid":"r-` + toolUse + `","timestamp":"2026-11-04T09:01:00Z","sessionId":"` + id + `","message":{"role":"user",` +
		`"content":[{"type":"tool_result","tool_use_id":"` + toolUse + `","content":"` + text + `"}]}}`
}

// A Claude session in the desktop app (no hook of Persona's runs there) that asks several questions at once: before,
// the apps got only the row's "reply to answer" line and never saw the questions or their options. Now its question is
// a card listed with the Codex cards, every question with its options and multi-select, the row says the apps may
// answer it, and the card's answer goes through the session's inbox at priority now as the user's answers.
func TestAnOutsideClaudesQuestionIsACard(t *testing.T) {
	n, f, home := herdrNode(t)
	noPaneEnv(t)
	shortPeerWait(t, 3*time.Second)
	f.reply("session.snapshot", snapshot(nil, nil, nil, nil))
	p := modeTranscript(t, home, home, "bypassPermissions")
	appendLines(t, p,
		`{"type":"assistant","uuid":"a1","timestamp":"2026-11-04T09:00:05Z","sessionId":"`+sidA+`","message":{"role":"assistant","content":[{"type":"text","text":"A few things first:"}]}}`,
		askCall(sidA, "toolu_multi", multiAsk))
	inbox := newFakeInbox(t, p)
	inbox.setMode(peerModeBypass)
	writeOutsideClaude(t, home, sidA, "claude-desktop-3p", "waiting", inbox, nil)

	s := byID(listSessions(t, n))[sidA]
	if s.Status != proto.StatusBlocked || s.Capabilities == nil || !s.Capabilities.Approve || !s.Capabilities.Send {
		t.Fatalf("asking: row %+v caps %+v", s, s.Capabilities)
	}
	cards := n.outsideAskCards()
	if len(cards) != 1 {
		t.Fatalf("cards %+v", cards)
	}
	ix := cards[0]
	if ix.Kind != proto.IxQuestion || ix.SessionID != sidA || ix.Agent != "claude" || ix.ToolName != askTool || ix.ToolUseID != "toolu_multi" ||
		!strings.HasPrefix(ix.ID, outsideAskPrefix) || ix.CanCancel {
		t.Fatalf("card %+v", ix)
	}
	if len(ix.Questions) != 4 || ix.Questions[0].Question != "Which setting wins?" || ix.Questions[0].Header != "Source" ||
		len(ix.Questions[0].Options) != 2 || ix.Questions[0].Options[1].Label != "Account" || ix.Questions[0].Options[1].Description != "The account's" ||
		!ix.Questions[2].MultiSelect || len(ix.Questions[2].Options) != 3 || ix.Questions[3].MultiSelect {
		t.Fatalf("questions %+v", ix.Questions)
	}
	n.syncCodexCards(context.Background())
	if n.cx.sent != ix.ID {
		t.Fatalf("listed %q, want %q", n.cx.sent, ix.ID)
	}

	answers := map[string]string{"Which setting wins?": "Account", "Which counts show?": "Unread, Mentions",
		"How are long rows shown?": "Only the first two lines", "Alert on a mention?": "Never"}
	params := map[string]any{"session_id": sidA, "id": ix.ID, "resolution": proto.Resolution{Answers: answers}, "owner_input": true}
	if _, err := request(t, n, "codex.answer", params); refusalCode(err) != peerHeld {
		t.Fatalf("unmarked answer: %v", err)
	}
	delete(params, "owner_input")
	if _, err := request(t, n, nodeproto.OwnerCardAnswer, params); err != nil {
		t.Fatalf("owner answer: %v", err)
	}
	if h := envelopeHead(t, inbox, 1); h != `<cross-session-message from-name="Persona" from-mode="bypass">` {
		t.Fatalf("owner answer envelope: %q", h)
	}
	pr, content := inboxPriority(t, inbox, 1)
	if pr != "now" {
		t.Fatalf("the answer went at %q", pr)
	}
	for _, want := range []string{"Which setting wins? → Account", "How are long rows shown? → Only the first two lines",
		"Which counts show? → Unread, Mentions", "Alert on a mention? → Never"} {
		if !strings.Contains(content, want) {
			t.Fatalf("the answer has no %q: %q", want, content)
		}
	}
	if strings.Index(content, "Which setting wins?") > strings.Index(content, "Alert on a mention?") {
		t.Fatalf("answers out of the card's order: %q", content)
	}
	n.syncCodexCards(context.Background())
	if n.cx.sent != "" {
		t.Fatalf("an answered card listed again at once: %q", n.cx.sent)
	}

	// Claude Code ends the question when the answer comes in at now: the call has its result, no card is left.
	appendLines(t, p, toolResult(sidA, "toolu_multi", "Tool permission request aborted"))
	if cards := n.outsideAskCards(); len(cards) != 0 {
		t.Fatalf("an ended question is still a card: %+v", cards)
	}
	if err := n.codexAnswer(context.Background(), sidA, ix.ID, proto.Resolution{Answers: answers}); refusalCode(err) != screen.PromptChanged {
		t.Fatalf("an answer to an ended question: %v", err)
	}
}

func TestOutsideAnswerWithTrailingTextUsesNow(t *testing.T) {
	n, f, home := herdrNode(t)
	noPaneEnv(t)
	shortPeerWait(t, 3*time.Second)
	f.reply("session.snapshot", snapshot(nil, nil, nil, nil))
	p := writeTranscript(t, home, home, sidA, time.Now(),
		`{"type":"user","uuid":"u1","timestamp":"2026-11-04T09:00:00Z","cwd":"`+home+`","sessionId":"`+sidA+`","message":{"role":"user","content":"choose"}}`,
		`{"type":"assistant","uuid":"a1","timestamp":"2026-11-04T09:00:09Z","sessionId":"`+sidA+`","message":{"role":"assistant","content":[{"type":"tool_use","id":"toolu_trailing","name":"AskUserQuestion","input":{"questions":[{"question":"Which one?","header":"Pick","options":[{"label":"A"},{"label":"B"}],"multiSelect":false}]}},{"type":"text","text":"Please choose one."}]}}`)
	inbox := newFakeInbox(t, p)
	writeOutsideClaude(t, home, sidA, "claude-desktop-3p", "waiting", inbox, nil)
	cards := n.outsideAskCards()
	if len(cards) != 1 {
		t.Fatalf("pending question cards = %d, want 1", len(cards))
	}
	if err := n.codexAnswer(context.Background(), sidA, cards[0].ID, proto.Resolution{Answers: map[string]string{"Which one?": "B"}}); err != nil {
		t.Fatalf("answer returned error: %v", err)
	}
	priority, _ := inboxPriority(t, inbox, 0)
	if priority != peerNow {
		t.Fatalf("card exists and answer returned nil, but priority = %q; the answer is queued behind the open question", priority)
	}
}

// No card where the phone could not answer, or nothing is asked: a claude without an inbox (the computer answers), a
// permission prompt (the latest call is not a question), a question answered on the computer, a busy claude. Handed
// back to the computer, a card stays off the phone while the claude asks it.
func TestAnOutsideClaudesQuestionCardOnlyWhileAnswerable(t *testing.T) {
	n, f, home := herdrNode(t)
	noPaneEnv(t)
	shortPeerWait(t, 3*time.Second)
	f.reply("session.snapshot", snapshot(nil, nil, nil, nil))
	p := writeTranscript(t, home, home, sidA, time.Now(),
		`{"type":"user","uuid":"u1","timestamp":"2026-11-04T09:00:00Z","cwd":"`+home+`","sessionId":"`+sidA+`","message":{"role":"user","content":"tidy the inbox"}}`,
		askCall(sidA, "toolu_one", `{"questions":[{"question":"Which one?","header":"Pick","options":[{"label":"A"},{"label":"B"}],"multiSelect":false}]}`))
	inbox := newFakeInbox(t, p)

	writeOutsideClaude(t, home, sidA, "claude-desktop", "waiting", nil, nil) // no inbox
	if cards := n.outsideAskCards(); len(cards) != 0 {
		t.Fatalf("no inbox, yet a card: %+v", cards)
	}
	if s := byID(listSessions(t, n))[sidA]; s.Capabilities == nil || s.Capabilities.Approve || s.AttentionReason != proto.AttentionClaudeComputer {
		t.Fatalf("no inbox: %+v %q", s.Capabilities, s.AttentionReason)
	}

	writeOutsideClaude(t, home, sidA, "claude-desktop", "busy", inbox, nil)
	if cards := n.outsideAskCards(); len(cards) != 0 {
		t.Fatalf("busy, yet a card: %+v", cards)
	}

	writeOutsideClaude(t, home, sidA, "claude-desktop", "waiting", inbox, nil)
	cards := n.outsideAskCards()
	if len(cards) != 1 {
		t.Fatalf("cards %+v", cards)
	}
	id := cards[0].ID
	if err := n.codexAnswer(context.Background(), sidA, id, proto.Resolution{Dismiss: true}); err != nil {
		t.Fatal(err)
	}
	n.syncCodexCards(context.Background())
	if n.cx.sent != "" {
		t.Fatalf("handed back, yet listed: %q", n.cx.sent)
	}
	if len(inbox.users()) != 0 {
		t.Fatalf("a hand-back sent something: %v", inbox.users())
	}
	if err := n.codexAnswer(context.Background(), sidA, id, proto.Resolution{Cancel: true}); err == nil {
		t.Fatal("Skip went through")
	}
	if err := n.codexAnswer(context.Background(), sidA, id, proto.Resolution{Answers: map[string]string{"Which one?": "  "}}); err == nil {
		t.Fatal("an empty answer went through")
	}

	// Answered on the computer, then a permission prompt: no card for either.
	appendLines(t, p, toolResult(sidA, "toolu_one", "A"),
		`{"type":"assistant","uuid":"a3","timestamp":"2026-11-04T09:02:00Z","sessionId":"`+sidA+`","message":{"role":"assistant","content":[{"type":"tool_use","id":"toolu_bash","name":"Bash","input":{"command":"ls"}}]}}`)
	if cards := n.outsideAskCards(); len(cards) != 0 {
		t.Fatalf("a permission prompt is a card: %+v", cards)
	}
	forgetMetas(n)
	if s := byID(listSessions(t, n))[sidA]; s.Capabilities == nil || s.Capabilities.Approve {
		t.Fatalf("a permission prompt can be approved from the phone: %+v", s.Capabilities)
	}
}

// The answer message lists only the questions answered, in the card's order, and says so when the assistant answered.
func TestOutsideAnswerText(t *testing.T) {
	ix := proto.Interaction{Questions: []proto.Question{{Question: "First\nline?"}, {Question: "Second?"}}}
	if got := outsideAnswerText(ix, proto.Resolution{Answers: map[string]string{"Second?": "yes", "First\nline?": " "}}); !strings.Contains(got, "Second? → yes") || strings.Contains(got, "First") {
		t.Fatalf("%q", got)
	}
	if got := outsideAnswerText(ix, proto.Resolution{}); got != "" {
		t.Fatalf("no answers: %q", got)
	}
	got := outsideAnswerText(ix, proto.Resolution{Answers: map[string]string{"First\nline?": "no"}, AnsweredBy: proto.AnsweredByAssistant, Reason: "your rule"})
	if !strings.Contains(got, "First line? → no") || !strings.Contains(got, "your rule") {
		t.Fatalf("by the assistant: %q", got)
	}
}
