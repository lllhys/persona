package channel

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/nwverse/persona/server/internal/proto"
	"github.com/nwverse/persona/server/internal/transcript"
)

// pagedSource is a transcript read page items at a time (more_after while more follow), its cut items whole from full.
type pagedSource struct {
	items []proto.Item
	page  int
	full  map[string]string
	fail  error
	after []int    // the seq each page was asked after
	fulls []string // the items asked whole
}

func (p *pagedSource) ItemsAfter(_ context.Context, after int) ([]proto.Item, bool, int, error) {
	p.after = append(p.after, after)
	if p.fail != nil {
		return nil, false, 0, p.fail
	}
	var page []proto.Item
	for _, it := range p.items {
		if it.Seq <= after {
			continue
		}
		if len(page) == p.page {
			return page, true, page[len(page)-1].Seq, nil
		}
		page = append(page, it)
	}
	to := after
	if len(page) > 0 {
		to = page[len(page)-1].Seq
	}
	return page, false, to, nil
}

func (p *pagedSource) Full(_ context.Context, id string) (proto.Item, error) {
	p.fulls = append(p.fulls, id)
	text, ok := p.full[id]
	if !ok {
		return proto.Item{}, errors.New("no such item")
	}
	return proto.Item{ID: id, Text: text}, nil
}

// numbered numbers items from seq 1 on, with ids.
func numbered(items ...proto.Item) []proto.Item {
	for i := range items {
		items[i].Seq = i + 1
		if items[i].ID == "" {
			items[i].ID = "i" + string(rune('a'+i))
		}
	}
	return items
}

func userSays(id, text string) proto.Item {
	return proto.Item{Role: "user", Kind: proto.KindText, Text: text, ClientMsgID: id}
}

func agentSays(text string) proto.Item {
	return proto.Item{Role: "assistant", Kind: proto.KindText, Text: text}
}

var (
	toolUse    = proto.Item{Role: "assistant", Kind: proto.KindToolUse, ToolName: "Bash"}
	toolResult = proto.Item{Role: "user", Kind: proto.KindToolResult}
)

func texts(items []proto.Item) string {
	var out []string
	for _, it := range items {
		out = append(out, it.Text)
	}
	return strings.Join(out, "|")
}

// A turn longer than a page is read to the end: from the run's base, its message found on one page and its answer on
// a later one; another turn before the message (someone else's) is not its answer.
func TestReadTurnPagesToTheEnd(t *testing.T) {
	src := &pagedSource{page: 2, items: numbered(
		userSays("wechat-9", "someone else's"), agentSays("their answer"),
		userSays("voice-1", "fix the test"), toolUse, toolResult, agentSays("looking"), toolUse, toolResult,
		agentSays("Fixed."), agentSays("All green."))}
	rd, err := ReadTurn(context.Background(), src, &Run{Base: 0, MsgIDs: []string{"voice-1"}})
	if err != nil || !rd.Found || rd.Answer != "Fixed.\n\nAll green." || rd.Last != 10 {
		t.Fatalf("%+v %v", rd, err)
	}
	if !slices.Equal(src.after, []int{0, 2, 4, 6, 8}) {
		t.Fatalf("pages asked after %v", src.after)
	}
	// From a base past the other turn: the pages start there.
	src.after = nil
	if rd, _ := ReadTurn(context.Background(), src, &Run{Base: 2, MsgIDs: []string{"voice-1"}}); rd.Answer != "Fixed.\n\nAll green." || src.after[0] != 2 {
		t.Fatalf("%+v after %v", rd, src.after)
	}
	// A run that names no message (held: its background work's turn) takes the last words after its base.
	if rd, _ := ReadTurn(context.Background(), src, &Run{Base: 6}); !rd.Found || rd.Answer != "Fixed.\n\nAll green." {
		t.Fatalf("unnamed: %+v", rd)
	}
}

// What the Lark bot posts for a long report, a tool call and a short closing line: both, the report first.
func TestReadTurnAnswerKeepsTheReportBeforeATool(t *testing.T) {
	report := "## 今天的进展\n\n" + strings.Repeat("第一项已经合并，第二项在等 CI。", 6)
	src := &pagedSource{page: 50, items: numbered(
		userSays("lark-1", "汇报一下"), agentSays("我先查一下。"), toolUse, toolResult,
		agentSays(report), toolUse, toolResult, agentSays("已经发给它了。"))}
	rd, err := ReadTurn(context.Background(), src, &Run{MsgIDs: []string{"lark-1"}})
	if err != nil || !rd.Found || rd.Answer != report+"\n\n已经发给它了。" {
		t.Fatalf("%q %v", rd.Answer, err)
	}
}

// The answer's cut items are read whole (/full); a message found by its probe may be cut before what the user wrote.
func TestReadTurnReadsCutItemsWhole(t *testing.T) {
	long := agentSays("The answer, cut…")
	long.ID, long.FullSize = "long", 4000
	forwarded := proto.Item{ID: "fwd", Role: "user", Kind: proto.KindText, Text: "[a forwarded chat, cut…]", FullSize: 9000}
	src := &pagedSource{page: 50, full: map[string]string{"long": "The answer, whole.", "fwd": "[a forwarded chat]\nsum it up"},
		items: numbered(userSays("", "earlier"), agentSays("old"), forwarded, toolUse, toolResult, long)}
	rd, err := ReadTurn(context.Background(), src, &Run{Probe: "sum it up"})
	if err != nil || !rd.Found || rd.Answer != "The answer, whole." {
		t.Fatalf("%+v %v", rd, err)
	}
	if !slices.Contains(src.fulls, "fwd") || !slices.Contains(src.fulls, "long") {
		t.Fatalf("asked whole: %v", src.fulls)
	}
	// The whole item can't be read: the page's text is the answer.
	delete(src.full, "long")
	if rd, _ := ReadTurn(context.Background(), src, &Run{Probe: "sum it up"}); rd.Answer != "The answer, cut…" {
		t.Fatalf("%+v", rd)
	}
}

// Not answered yet: the message not in the conversation, in but nothing after it, or one of a folded run's messages
// not in; a page that can't be read is an error, read again later from the same base.
func TestReadTurnWaits(t *testing.T) {
	src := &pagedSource{page: 10, items: numbered(userSays("voice-1", "one"), agentSays("answer one"), userSays("voice-2", "two"))}
	for name, r := range map[string]*Run{
		"not in":            {MsgIDs: []string{"voice-3"}},
		"no words after it": {MsgIDs: []string{"voice-2"}},
		"one of two not in": {MsgIDs: []string{"voice-1", "voice-3"}},
	} {
		if rd, err := ReadTurn(context.Background(), src, r); err != nil || rd.Found || rd.Answer != "" || rd.Last != 3 {
			t.Errorf("%s: %+v %v", name, rd, err)
		}
	}
	src.fail = errors.New("the machine is away")
	if rd, err := ReadTurn(context.Background(), src, &Run{Base: 2, MsgIDs: []string{"voice-2"}}); err == nil || rd.Last != 2 {
		t.Fatalf("%+v %v", rd, err)
	}
}

// A message sent while an earlier one waited is folded into one run: once both are in, each message's own answer, in
// the conversation's order, read across pages.
func TestReadTurnFoldedAcrossPages(t *testing.T) {
	first := &Run{Base: 1, MsgIDs: []string{"voice-1"}, Started: t0}
	second := &Run{Base: 4, MsgIDs: []string{"voice-2"}, Started: t0.Add(30e9)}
	second.Fold(first)
	duration := proto.Item{Role: "system", Kind: "turn_duration", TurnDuration: &proto.TurnDuration{DurationMS: 5}}
	src := &pagedSource{page: 2, items: numbered(agentSays("before"),
		userSays("voice-1", "fix it"), toolUse, toolResult, agentSays("fixed"), duration,
		userSays("voice-2", "and lint"), toolUse, toolResult, agentSays("lint clean"), duration)}
	rd, err := ReadTurn(context.Background(), src, second)
	if err != nil || !rd.Found || rd.Answer != "fixed\n\nlint clean" {
		t.Fatalf("%+v %v", rd, err)
	}
	if src.after[0] != 1 {
		t.Fatalf("read from %d, not the earlier base", src.after[0])
	}
}

// A folded run's answer: each of its messages' own answer, in the conversation's order, once all are in; a part the
// conversation went on from (it took the next message in mid-turn) has its answer in the next one; an older node
// (no client message ids on the items) gets the last words, as before.
func TestFoldedRunAnswer(t *testing.T) {
	r := &Run{MsgIDs: []string{"wechat-1", "wechat-2"}}
	for _, c := range []struct {
		name  string
		items []proto.Item
		want  string
		all   bool
	}{
		{"two turns", []proto.Item{userSays("wechat-1", "一"), toolUse, toolResult, agentSays("答一"), userSays("wechat-2", "二"), toolUse, toolResult, agentSays("答二")}, "答一|答二", true},
		{"the later one first", []proto.Item{userSays("wechat-2", "二"), agentSays("答二"), userSays("wechat-1", "一"), agentSays("答一")}, "答二|答一", true},
		{"taken in mid-turn", []proto.Item{userSays("wechat-1", "一"), toolUse, userSays("wechat-2", "二"), toolResult, agentSays("一起答")}, "一起答", true},
		{"one not in yet", []proto.Item{userSays("wechat-1", "一"), agentSays("答一")}, "", false},
		{"queued mid-turn", []proto.Item{userSays("wechat-1", "一"), {Role: "user", Kind: proto.KindText, Text: "二", ClientMsgID: "wechat-2", Queued: true}, agentSays("一起答")}, "一起答", true},
		{"an older node", []proto.Item{userSays("", "一"), agentSays("答一"), userSays("", "二"), agentSays("答二")}, "答二", true},
	} {
		got, all := RunAnswer(r, c.items)
		if all != c.all || texts(got) != c.want {
			t.Errorf("%s: %q, %v; want %q, %v", c.name, texts(got), all, c.want, c.all)
		}
	}
	if got, _ := RunAnswer(&Run{MsgIDs: []string{"wechat-1"}}, []proto.Item{userSays("wechat-1", "一"), agentSays("答一"), userSays("x", "y"), agentSays("答y")}); texts(got) != "答y" {
		t.Errorf("one message: %q (as before: the last words)", texts(got))
	}
}

// Whole-turn durations follow the answers but do not turn them into unfinished parts.
func TestFoldedRunAnswerTurnDuration(t *testing.T) {
	r := &Run{MsgIDs: []string{"a", "b"}}
	items := []proto.Item{
		{Seq: 1, Role: "user", Kind: "text", Text: "one", ClientMsgID: "a"},
		{Seq: 2, Role: "assistant", Kind: "text", Text: "answer one"},
		{Seq: 3, Role: "system", Kind: "turn_duration", TurnDuration: &proto.TurnDuration{DurationMS: 5}},
		{Seq: 4, Role: "user", Kind: "text", Text: "two", ClientMsgID: "b"},
		{Seq: 5, Role: "assistant", Kind: "text", Text: "answer two"},
		{Seq: 6, Role: "system", Kind: "turn_duration", TurnDuration: &proto.TurnDuration{DurationMS: 5}},
	}
	got, all := RunAnswer(r, items)
	if !all || texts(got) != "answer one|answer two" {
		t.Fatalf("%q, %v; want both answers, true", texts(got), all)
	}
}

// Only rows that say nothing about how a turn ended can be passed over, as in Stopped.
func TestFoldedRunAnswerEndings(t *testing.T) {
	r := &Run{MsgIDs: []string{"a", "b"}}
	for _, c := range []struct {
		name string
		end  proto.Item
		both bool
	}{
		{"thinking", proto.Item{Role: "assistant", Kind: proto.KindThinking, Text: "thinking"}, true},
		{"empty text", agentSays(" \n\t"), true},
		{"compact marker", proto.Item{Role: "system", Kind: proto.KindNotice, Compact: &proto.Compact{Trigger: "auto"}}, true},
		{"compacted text", proto.Item{Role: "system", Kind: proto.KindNotice, Text: transcript.CompactedNotice}, true},
		{"compact summary", proto.Item{Role: "system", Kind: proto.KindNotice, Text: transcript.CompactSummaryNotice}, true},
		{"compact command", proto.Item{Role: "system", Kind: proto.KindNotice, Text: transcript.CompactNotice}, true},
		{"tool use", toolUse, false},
		{"tool result", toolResult, false},
		{"user text", userSays("", "another message"), false},
		{"system text", proto.Item{Role: "system", Kind: proto.KindText, Text: "system message"}, false},
		{"API error", proto.Item{Role: "system", Kind: proto.KindNotice, Text: transcript.APIErrorPrefix + "overloaded"}, false},
		{"interrupted", proto.Item{Role: "system", Kind: proto.KindNotice, Text: transcript.InterruptedNotice}, false},
		{"other notice", proto.Item{Role: "system", Kind: proto.KindNotice, Text: "another event"}, false},
		{"task with compact text", proto.Item{Role: "system", Kind: proto.KindNotice, Text: transcript.CompactedNotice, Task: &proto.TaskNote{}}, false},
		{"compact on non-notice", proto.Item{Role: "system", Kind: proto.KindText, Text: transcript.CompactedNotice, Compact: &proto.Compact{}}, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			items := numbered(userSays("a", "one"), agentSays("answer one"), c.end,
				proto.Item{Role: "system", Kind: "turn_duration", TurnDuration: &proto.TurnDuration{DurationMS: 5}},
				proto.Item{Role: "system", Kind: proto.KindNotice, Text: transcript.CompactSummaryNotice},
				userSays("b", "two"), agentSays("answer two"))
			want := "answer two"
			if c.both {
				want = "answer one|answer two"
			}
			got, all := RunAnswer(r, items)
			if !all || texts(got) != want {
				t.Fatalf("%q, %v; want %q, true", texts(got), all, want)
			}
		})
	}
}

// A turn stopped before the agent said anything (#4505): its message is followed only by the "Interrupted" notice. The turn is
// over: found, without words, stopped, so the run ends now instead of waiting as one still queued.
func TestReadTurnStoppedBeforeWords(t *testing.T) {
	stopped := proto.Item{Role: "system", Kind: proto.KindNotice, Text: transcript.InterruptedNotice}
	src := &pagedSource{page: 10, items: numbered(userSays("voice-1", "one"), agentSays("answer one"), userSays("voice-2", "two"), stopped)}
	rd, err := ReadTurn(context.Background(), src, &Run{MsgIDs: []string{"voice-2"}})
	if err != nil || !rd.Found || rd.Answer != "" || rd.Stopped == "" || rd.Last != 4 {
		t.Fatalf("%+v %v", rd, err)
	}
	// A folded run whose next message is not in yet still waits: that message's turn is still to come.
	if rd, _ := ReadTurn(context.Background(), src, &Run{MsgIDs: []string{"voice-2", "voice-3"}}); rd.Found {
		t.Fatalf("a folded run with a message still to come: %+v", rd)
	}
	// Once both are in and the turn was stopped before any words, the folded run is over too.
	folded := &pagedSource{page: 10, items: numbered(userSays("voice-1", "one"), userSays("voice-2", "two"), stopped)}
	if rd, _ := ReadTurn(context.Background(), folded, &Run{MsgIDs: []string{"voice-1", "voice-2"}}); !rd.Found || rd.Stopped == "" {
		t.Fatalf("a folded run stopped before words: %+v", rd)
	}
	// A long API error still being retried: its suffix is read on the whole text, not the clipped reason.
	long := proto.Item{Role: "system", Kind: proto.KindNotice, Text: transcript.APIErrorPrefix + strings.Repeat("x", 400) + transcript.APIRetrying}
	src2 := &pagedSource{page: 10, items: numbered(userSays("voice-2", "two"), long)}
	if rd, _ := ReadTurn(context.Background(), src2, &Run{MsgIDs: []string{"voice-2"}}); rd.Found {
		t.Fatalf("a long API error being retried: %+v", rd)
	}
	// An API error Claude Code is still retrying is not the end of the turn.
	src.items[3] = proto.Item{Seq: 4, ID: "id", Role: "system", Kind: proto.KindNotice, Text: transcript.APIErrorPrefix + "Overloaded（正在重试）"}
	if rd, _ := ReadTurn(context.Background(), src, &Run{MsgIDs: []string{"voice-2"}}); rd.Found {
		t.Fatalf("an API error being retried: %+v", rd)
	}
	// Some other notice after it (not a stop) still waits.
	src.items[3] = proto.Item{Seq: 4, ID: "id", Role: "system", Kind: proto.KindNotice, Text: "something else"}
	if rd, _ := ReadTurn(context.Background(), src, &Run{MsgIDs: []string{"voice-2"}}); rd.Found {
		t.Fatalf("a plain notice is not a stop: %+v", rd)
	}
}

// A message that started the conversation has no client message id (node assistant.send ties none to it); one sent
// before it was answered is folded in with its own. The folded run still answers the first: from the conversation's
// start, each message's own answer (#4746).
func TestReadTurnFoldsTheConversationsFirstMessage(t *testing.T) {
	first := &Run{Started: t0} // started the conversation: no id, no probe, base 0
	second := &Run{Base: 3, MsgIDs: []string{"feishu-2"}, Probe: "second", Started: t0.Add(5e9)}
	second.Fold(first)
	duration := proto.Item{Role: "system", Kind: "turn_duration", TurnDuration: &proto.TurnDuration{DurationMS: 5}}
	src := &pagedSource{page: 2, items: numbered(userSays("", "first"), toolUse, toolResult, agentSays("first answer"),
		userSays("feishu-2", "second"), agentSays("second answer"), duration)}
	rd, err := ReadTurn(context.Background(), src, second)
	if err != nil || !rd.Found || rd.Answer != "first answer\n\nsecond answer" {
		t.Fatalf("%+v %v", rd, err)
	}
	if src.after[0] != 0 {
		t.Fatalf("read from %d, not the conversation's start", src.after[0])
	}
	// The second message not in yet: wait for it, not answer the first alone.
	src = &pagedSource{page: 10, items: numbered(userSays("", "first"), agentSays("first answer"))}
	if rd, err := ReadTurn(context.Background(), src, second); err != nil || rd.Found {
		t.Fatalf("answered before the second message was in: %+v %v", rd, err)
	}
	// An older node (no ids on the items): the last words, as before.
	if got, all := RunAnswer(second, []proto.Item{userSays("", "first"), agentSays("first answer"), userSays("", "second"),
		agentSays("second answer")}); !all || texts(got) != "second answer" {
		t.Fatalf("older node: %q %v", texts(got), all)
	}
	// A held run names no message either, but it was answered already: folding it in does not add a part.
	held := &Run{Held: true, Started: t0}
	next := &Run{Base: 3, MsgIDs: []string{"feishu-3"}, Started: t0.Add(5e9)}
	next.Fold(held)
	if next.Head {
		t.Fatal("a held run folded in as an unanswered message")
	}
	// Asked again: the run now waits on the ask-again message alone, from its new base; the conversation's first
	// message is not a part of it any more.
	again := &Run{Base: 3, MsgIDs: []string{"feishu-2"}, Started: t0.Add(5e9)}
	again.Fold(&Run{Started: t0})
	again.AskAgain(4, "feishu-2-again", "", t0.Add(6e9))
	if again.Head {
		t.Fatal("asked again, still reading from the conversation's start")
	}
}

// Voice starts a new conversation with its message as the first line: named by its probe alone (no client message id,
// base 0). One sent before it was answered is folded in with its own id; the one answer still holds the first's.
func TestReadTurnFoldsAProbedFirstMessage(t *testing.T) {
	first := &Run{Probe: "first", Started: t0} // as voice handOver leaves a run that started the conversation
	second := &Run{Base: 4, MsgIDs: []string{"voice-2"}, Probe: "second", Started: t0.Add(5e9)}
	second.Fold(first)
	duration := proto.Item{Role: "system", Kind: "turn_duration", TurnDuration: &proto.TurnDuration{DurationMS: 5}}
	src := &pagedSource{page: 2, items: numbered(userSays("", "first"), toolUse, toolResult, agentSays("first answer"),
		userSays("voice-2", "second"), agentSays("second answer"), duration)}
	rd, err := ReadTurn(context.Background(), src, second)
	if err != nil || !rd.Found || rd.Answer != "first answer\n\nsecond answer" {
		t.Fatalf("%+v %v", rd, err)
	}
	// A probed run that is not the conversation's start (an older node's, after its base) is no head.
	mid := &Run{Base: 4, MsgIDs: []string{"voice-3"}, Started: t0.Add(5e9)}
	mid.Fold(&Run{Base: 2, Probe: "first", Started: t0})
	if mid.Head {
		t.Fatal("a probed run after the conversation's start folded in as its first message")
	}
	// Held: answered already.
	held := &Run{Base: 4, MsgIDs: []string{"voice-3"}, Started: t0.Add(5e9)}
	held.Fold(&Run{Held: true, Started: t0})
	if held.Head {
		t.Fatal("a held run folded in as an unanswered message")
	}
}

// A turn that ended with a turn_duration only (#4895): no words, not stopped, but recorded as finished.
func TestReadTurnTurnDurationWithoutWords(t *testing.T) {
	duration := proto.Item{Role: "system", Kind: "turn_duration", TurnDuration: &proto.TurnDuration{DurationMS: 500}}
	src := &pagedSource{page: 10, items: numbered(userSays("voice-1", "one"), duration)}
	rd, err := ReadTurn(context.Background(), src, &Run{MsgIDs: []string{"voice-1"}})
	if err != nil || !rd.Found || rd.Answer != "" || rd.Stopped != "" || rd.Last != 2 {
		t.Fatalf("turn_duration only: %+v %v", rd, err)
	}
}

// A folded run where a queued message is present when a prior turn is stopped (#4895):
// the queued message has not started its turn yet, so the run must not finish early.
func TestFoldedRunQueuedInterruptedDoesNotFinishEarly(t *testing.T) {
	stopped := proto.Item{Role: "system", Kind: proto.KindNotice, Text: transcript.InterruptedNotice}
	queued := proto.Item{Role: "user", Kind: proto.KindText, Text: "two", ClientMsgID: "voice-2", Queued: true}
	src := &pagedSource{page: 10, items: numbered(userSays("voice-1", "one"), queued, stopped)}
	rd, err := ReadTurn(context.Background(), src, &Run{MsgIDs: []string{"voice-1", "voice-2"}})
	if err != nil {
		t.Fatal(err)
	}
	if rd.Found {
		t.Fatalf("folded run with queued message should not finish early on stopped notice: %+v", rd)
	}
}
