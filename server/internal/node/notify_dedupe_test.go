package node

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/proto"
	"github.com/nwverse/persona/server/internal/rpc"
)

// wireRec is the node's side of a hub connection: every message the node writes, in order. fail makes writes fail.
type wireRec struct {
	mu      sync.Mutex
	fail    bool
	lines   [][]byte
	pending *atomic.Int32 // the node's sent.pending
	answers []int32       // pending as each answer to the hub was written
}

func (w *wireRec) Write(b []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.fail {
		return 0, errors.New("broken pipe")
	}
	w.lines = append(w.lines, bytes.Clone(b))
	if w.pending != nil && bytes.HasPrefix(b, []byte(`{"id":"h`)) {
		w.answers = append(w.answers, w.pending.Load())
	}
	return len(b), nil
}

// changed is the sessions.changed lines written so far.
func (w *wireRec) changed() [][]byte {
	w.mu.Lock()
	defer w.mu.Unlock()
	var out [][]byte
	for _, l := range w.lines {
		if bytes.HasPrefix(l, []byte(`{"event":"sessions.changed"`)) {
			out = append(out, l)
		}
	}
	return out
}

// dedupeNode is a node whose hub connection is recorded; list asks it for sessions.list from the hub, as the manager
// does, and returns once the answer is written and the node has seen it go.
func dedupeNode(t *testing.T) (n *Node, w *wireRec, list func()) {
	t.Helper()
	w = &wireRec{}
	n, hub := runtimeNodeTapped(t, nil, w)
	w.mu.Lock()
	w.pending = &n.sent.pending
	w.mu.Unlock()
	list = func() {
		if err := hub.nodePeer.Call(context.Background(), "sessions.list", map[string]any{}, nil); err != nil {
			t.Fatal(err)
		}
		// sentList.answering counts on rpc ending the request's ctx only after the answer is written (Peer.handle).
		w.mu.Lock()
		at := w.answers[len(w.answers)-1]
		w.mu.Unlock()
		if at == 0 {
			t.Fatal("the sessions.list answer was written after its request ended")
		}
		waitFor(t, "the answer seen written", func() bool { return n.sent.pending.Load() == 0 })
	}
	return n, w, list
}

// A list goes to the manager unless the manager holds those very bytes: the last sessions.changed written on this
// connection, with nothing written since that the manager applies to its copy (revs, a sessions.list answer), no
// answer still pending, no engine switch and no failed write in between.
func TestSessionsChangedOnlyWhenTheBytesDiffer(t *testing.T) {
	base := []proto.Session{{SessionID: "a", Status: proto.StatusIdle, Title: "one", Rev: 10}, {SessionID: "b", Status: proto.StatusDone, Title: "two", Rev: 20}}
	with := func(f func(*proto.Session)) []proto.Session {
		l := append([]proto.Session(nil), base...)
		f(&l[0])
		return l
	}
	mode := with(func(s *proto.Session) { s.PermissionMode = "plan" }) // not in listKey
	grew := with(func(s *proto.Session) { s.Rev = 11 })                // revs only
	type step struct {
		// notify / publish: that path with list; list: a sessions.list answer; pending / answered: an answer starts / is
		// written; reconnect; break / mend: writes fail / work
		do   string
		list []proto.Session
		want bool // a sessions.changed was written
	}
	for _, tc := range []struct {
		name  string
		steps []step
	}{
		{"the same bytes go once", []step{{"notify", base, true}, {"notify", base, false}, {"publish", base, false}, {"notify", base, false}}},
		{"a field listKey leaves out still goes", []step{{"notify", base, true}, {"notify", mode, true}, {"notify", base, true}}},
		{"after revs", []step{{"notify", base, true}, {"publish", grew, false}, {"notify", base, true}, {"notify", base, false}}},
		{"after a sessions.list answer", []step{{"notify", base, true}, {"list", nil, false}, {"notify", base, true}, {"notify", base, false}}},
		{"while an answer is pending", []step{{"notify", base, true}, {"pending", nil, false}, {"notify", base, true}, {"notify", base, true}, {"answered", nil, false}, {"notify", base, true}, {"notify", base, false}}},
		{"on another connection", []step{{"notify", base, true}, {"reconnect", nil, false}, {"notify", base, true}}},
		{"after a failed write", []step{{"notify", base, true}, {"break", nil, false}, {"notify", mode, false}, {"mend", nil, false}, {"notify", base, true}}},
		{"not the list that failed", []step{{"notify", base, true}, {"break", nil, false}, {"notify", mode, false}, {"mend", nil, false}, {"notify", mode, true}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			n, w, list := dedupeNode(t)
			answered := func() {}
			for i, s := range tc.steps {
				before := len(w.changed())
				switch s.do {
				case "notify":
					n.notifyList(s.list)
				case "publish":
					n.publish(s.list)
				case "list":
					list()
				case "pending": // an answer being built or written
					ctx, cancel := context.WithCancel(context.Background())
					n.sent.answering(ctx)
					answered = cancel
				case "answered":
					answered()
					waitFor(t, "the answer seen written", func() bool { return n.sent.pending.Load() == 0 })
				case "reconnect":
					w = &wireRec{}
					n.peer = rpc.New("n", w)
					before = 0
				case "break", "mend":
					w.mu.Lock()
					w.fail = s.do == "break"
					w.mu.Unlock()
				}
				got := w.changed()
				if sent := len(got) > before; sent != s.want {
					t.Fatalf("step %d (%s): sent %v, want %v", i, s.do, sent, s.want)
				}
				if s.want { // the bytes Notify always wrote for the list
					params, _ := json.Marshal(map[string]any{"sessions": s.list})
					want := `{"event":"sessions.changed","params":` + string(params) + "}\n"
					if string(got[len(got)-1]) != want {
						t.Fatalf("step %d: wrote %s\nwant %s", i, got[len(got)-1], want)
					}
				}
			}
		})
	}
}

// engine.use pushes the new engine's list once (protocol.md), also when it is the very list pushed before the switch.
func TestEngineSwitchPushesItsListOnce(t *testing.T) {
	t.Setenv("HC_SOCKET", filepath.Join(t.TempDir(), "none.sock")) // no Herdr App to reach
	n, w, _ := dedupeNode(t)
	writeTranscript(t, n.home, "/w/app", sidA, time.Now().Add(-time.Minute), `{"type":"custom-title","customTitle":"a"}`)
	n.setKept([]string{sidA}) // listed however short
	list := n.sessionList(context.Background(), false)
	if len(list) != 1 {
		t.Fatalf("listed %d sessions, want the one transcript", len(list))
	}
	n.notifyList(list)
	n.notifyList(list)
	if got := len(w.changed()); got != 1 {
		t.Fatalf("%d lists before the switch, want 1", got)
	}
	if err := n.useEngine(context.Background(), proto.EngineHerdrCore); err != nil {
		t.Fatal(err)
	}
	n.engLists.Wait()
	got := w.changed()
	if len(got) != 2 || !bytes.Equal(got[0], got[1]) {
		t.Fatalf("after the switch: %d lists (want 2, the same bytes)", len(got))
	}
}
