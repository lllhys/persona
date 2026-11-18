package assistant

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/proto"
	"github.com/nwverse/persona/server/internal/rpc"
)

// What triage reads of a session (agents_triage.go triageRead, #8132): a turn once, and nothing that would start a
// stopped agent. An engine that lets idle agents go (Paseo) loads one to answer for its items, which relaunches its
// provider's process; with a triage model that kept failing, every retry read the turn again.

func itemReads(f *fakeHub) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.itemReads
}

func triageCalls(f *fakeHub) []modelCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]modelCall(nil), f.triageAsked...)
}

func setTriageAnswer(f *fakeHub, answer string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.triage = answer
}

// A failed model call is tried again on the same turn (the backfill does, every workRetryTriage): the retry judges the
// turn an attempt before read, and asks the node for it again only when that read failed. The next turn is read anew.
func TestTriageRetryReadsTheTurnOnce(t *testing.T) {
	s, f := workService(t)
	f.items = []proto.Item{
		{Seq: 1, ID: "1#0", Kind: proto.KindText, Role: "user", Text: "把登录修好"},
		{Seq: 2, ID: "2#0", Kind: proto.KindText, Role: "assistant", Text: "修好了，测试都过了"},
	}
	f.itemsFail = 1 // the node does not answer the first read
	f.setSessions("box", stopped("s1", "修好了"))
	triage(t, s, "s1") // no model answer: it fails
	if _, ok := s.workEntry([2]string{"box", "s1"}); ok {
		t.Fatal("a turn whose model call failed was triaged")
	}
	if n, asked := itemReads(f), triageCalls(f); n != 1 || len(asked) != 1 {
		t.Fatalf("first attempt: %d reads, %d model calls", n, len(asked))
	}
	triage(t, s, "s1")
	if n := itemReads(f); n != 2 {
		t.Fatalf("a read that failed is read again: %d reads, want 2", n)
	}
	setTriageAnswer(f, `{"phase":"done","line":"登录修好了"}`)
	triage(t, s, "s1")
	if n := itemReads(f); n != 2 {
		t.Fatalf("the retry read the turn again: %d reads, want 2", n)
	}
	asked := triageCalls(f)
	if len(asked) != 3 || !strings.Contains(asked[2].User, "修好了，测试都过了") || !strings.Contains(asked[2].User, "把登录修好") {
		t.Fatalf("the retry did not judge the turn read before: %+v", asked)
	}
	if e := entryOf(t, s, "s1"); e.Work.Phase != proto.WorkDone {
		t.Fatalf("work %+v", e.Work)
	}
	s.work.mu.Lock()
	kept := s.work.tried[[2]string{"box", "s1"}].read
	s.work.mu.Unlock()
	if kept != nil {
		t.Fatal("a triaged turn is not tried again: its read is not kept")
	}

	next := stopped("s1", "又改了一处")
	next.UpdatedAt = turnEnd.Add(time.Hour)
	f.setSessions("box", next)
	triage(t, s, "s1")
	if n := itemReads(f); n != 3 {
		t.Fatalf("the next turn: %d reads, want 3", n)
	}
}

// Triage never has a stopped agent started to read its turn. A session not running is not triaged; a read its runtime
// refuses because it would start the agent (not_loaded: Paseo loads an agent it let go to answer for its items) counts
// as read with nothing in it: the row's last words stand in for the reply, and the retry asks the node nothing.
func TestTriageStartsNoStoppedAgent(t *testing.T) {
	s, f := workService(t)
	off := stopped("s1", "做完了")
	off.Status, off.State = proto.StatusOffline, proto.StateOffline
	f.setSessions("box", off)
	triage(t, s, "s1")
	if n, asked := itemReads(f), triageCalls(f); n != 0 || len(asked) != 0 {
		t.Fatalf("a session not running: %d reads, %d model calls", n, len(asked))
	}

	f.answerNode(DeviceSessionItems, func(string, json.RawMessage) (any, error) {
		return nil, &rpc.Error{Code: proto.CodeNotLoaded, Message: "reading it would start the agent"}
	})
	f.setSessions("box", stopped("s2", "部署完了，等你确认"))
	triage(t, s, "s2") // no model answer: it fails
	asked := triageCalls(f)
	if n := itemReads(f); n != 1 || len(asked) != 1 || !strings.Contains(asked[0].User, "部署完了，等你确认") {
		t.Fatalf("a refused read: %d reads, the model asked %+v", n, asked)
	}
	setTriageAnswer(f, `{"phase":"done","line":"部署完了"}`)
	triage(t, s, "s2")
	if n := itemReads(f); n != 1 {
		t.Fatalf("the retry asked the node again: %d reads", n)
	}
	if e := entryOf(t, s, "s2"); e.Work.Phase != proto.WorkDone {
		t.Fatalf("work %+v", e.Work)
	}
}
