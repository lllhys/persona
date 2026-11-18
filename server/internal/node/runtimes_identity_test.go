package node

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/proto"
	"github.com/nwverse/persona/server/internal/rpc"
)

// helloAt connects a fake runtime and says hello, returning the node's answer instead of failing the test on it.
func helloAt(t *testing.T, path string, hello map[string]any) (*fakeRuntime, error) {
	t.Helper()
	c, err := net.Dial("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	f := &fakeRuntime{conn: c, params: map[string][]json.RawMessage{}}
	f.peer = rpc.New("d", c)
	f.peer.OnRequest = func(ctx context.Context, method string, params json.RawMessage) (any, error) {
		f.mu.Lock()
		f.calls = append(f.calls, method)
		f.params[method] = append(f.params[method], params)
		answer := f.answer
		f.mu.Unlock()
		if answer != nil {
			return answer(method, params)
		}
		return map[string]any{"ok": true}, nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { _ = f.peer.Serve(ctx, c); close(done) }()
	t.Cleanup(func() { cancel(); _ = c.Close() })
	cctx, ccancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer ccancel()
	var res map[string]any
	err = f.peer.Call(cctx, "runtime.hello", hello, &res)
	f.helloR = res
	f.closed = done
	return f, err
}

func withInstance(hello map[string]any, instance string) map[string]any {
	out := map[string]any{}
	for k, v := range hello {
		out[k] = v
	}
	out["instance"] = instance
	return out
}

func readyIDs(n *Node) map[string]int {
	out := map[string]int{}
	for _, h := range n.runtimes.ready() {
		out[h.id()]++
	}
	return out
}

func TestRuntimeIDHeldByAnotherProcessIsRefused(t *testing.T) {
	n, _ := runtimeNode(t)
	first, err := helloAt(t, n.runtimeSock, withInstance(dshHello(), "proc-a"))
	if err != nil {
		t.Fatal(err)
	}
	_ = first.peer.Notify("runtime.sessions", map[string]any{"sessions": []map[string]any{{"session_id": "session-a", "status": "idle"}}})
	waitFor(t, "listed", func() bool { _, ok := listed(t, n)["session-a"]; return ok })

	_, err = helloAt(t, n.runtimeSock, withInstance(dshHello(), "proc-b"))
	var re *rpc.Error
	if !errors.As(err, &re) || re.Code != "runtime_conflict" {
		t.Fatalf("a second process with the same runtime id: %v", err)
	}
	if ids := readyIDs(n); ids["dsh"] != 1 {
		t.Fatalf("ready runtimes %v", ids)
	}
	if _, ok := listed(t, n)["session-a"]; !ok {
		t.Fatal("the first process's sessions are gone")
	}
}

func TestRuntimeReconnectReplacesItsOldConnection(t *testing.T) {
	n, _ := runtimeNode(t)
	old, err := helloAt(t, n.runtimeSock, withInstance(dshHello(), "proc-a"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := helloAt(t, n.runtimeSock, withInstance(dshHello(), "proc-a")); err != nil {
		t.Fatalf("the same process reconnecting: %v", err)
	}
	select {
	case <-old.closed:
	case <-time.After(5 * time.Second):
		t.Fatal("the old connection of the same process stays open")
	}
	waitFor(t, "one connection", func() bool { return readyIDs(n)["dsh"] == 1 })
}

func TestRuntimeWithoutInstanceConnectsAsBefore(t *testing.T) {
	n, _ := runtimeNode(t)
	for i := 0; i < 2; i++ {
		if _, err := helloAt(t, n.runtimeSock, dshHello()); err != nil {
			t.Fatalf("an older runtime (no instance), #%d: %v", i+1, err)
		}
	}
	if ids := readyIDs(n); ids["dsh"] != 2 {
		t.Fatalf("ready runtimes %v", ids)
	}
}

func TestRuntimeLauncherNamesHaveOneOwner(t *testing.T) {
	n, _ := runtimeNode(t)
	launcherCache.Lock()
	launcherCache.at, launcherCache.list = time.Now(), []proto.Launcher{{Name: "claude"}, {Name: "shared"}}
	launcherCache.Unlock()
	t.Cleanup(func() {
		launcherCache.Lock()
		launcherCache.at, launcherCache.list = time.Time{}, nil
		launcherCache.Unlock()
	})
	hello := func(rt, launcher string) map[string]any {
		return map[string]any{"runtime": rt, "launcher": launcher, "caps": map[string]any{"start": true, "send": true}}
	}
	first := dialRuntime(t, n, hello("dsh-a", "dsh"))
	second := dialRuntime(t, n, hello("dsh-b", "dsh"))
	_ = dialRuntime(t, n, hello("other", "shared"))
	for _, f := range []*fakeRuntime{first, second} {
		f.answer = func(method string, params json.RawMessage) (any, error) {
			return map[string]any{"session": map[string]any{"session_id": "session-n", "cwd": "/w", "status": "idle"}}, nil
		}
	}
	for i := 0; i < 5; i++ { // the same answer every time, whatever order the map iterates in
		raw, err := nodeCall(t, n, "launchers.list", map[string]any{})
		if err != nil {
			t.Fatal(err)
		}
		var ls struct {
			Launchers []proto.Launcher `json:"launchers"`
		}
		_ = json.Unmarshal(raw, &ls)
		dsh, shared := 0, 0
		for _, l := range ls.Launchers {
			switch l.Name {
			case "dsh":
				dsh++
				if l.Source != "dsh-a" {
					t.Fatalf("dsh offered by %s, not the runtime connected first", l.Source)
				}
			case "shared":
				shared++
				if l.Source != "" {
					t.Fatalf("a runtime's launcher named like the shell's is listed: %+v", l)
				}
			}
		}
		if dsh != 1 || shared != 1 {
			t.Fatalf("launchers %s", raw)
		}
		if h := n.runtimes.launcherRuntime("dsh"); h == nil || h.id() != "dsh-a" {
			t.Fatal("session.start with dsh would not reach the runtime the form shows")
		}
	}
	if _, _, _, ok := n.runtimeStart(context.Background(), runtimeStartParams{Launcher: "shared", Cwd: "/w"}); ok {
		t.Fatal("a launcher named like the shell's started on the runtime")
	}
	if _, err := nodeCall(t, n, "session.start", map[string]any{"cwd": "/w", "launcher": "dsh", "async": true}); err != nil {
		t.Fatal(err)
	}
	if len(first.got("session.start")) != 1 || len(second.got("session.start")) != 0 {
		t.Fatalf("started on a=%d b=%d", len(first.got("session.start")), len(second.got("session.start")))
	}
}

// companion is a runtime listing sessions from disk (offline) that hands every send over.
func companion(t *testing.T, n *Node, ids ...string) *fakeRuntime {
	t.Helper()
	f := dialRuntime(t, n, map[string]any{"runtime": "dsh-files", "name": "DSH", "caps": map[string]any{"send": true}})
	var rows []map[string]any
	for _, id := range ids {
		rows = append(rows, map[string]any{"session_id": id, "status": "offline", "cwd": "/w"})
	}
	_ = f.peer.Notify("runtime.sessions", map[string]any{"sessions": rows})
	for _, id := range ids {
		waitFor(t, "listed offline", func() bool { s, ok := listed(t, n)[id]; return ok && s.Status == proto.StatusOffline })
	}
	return f
}

func handoverError() error {
	return &rpc.Error{Code: runtimeHandover, Message: "DSH is starting", Data: json.RawMessage(`{"delivered":"none"}`)}
}

func TestRuntimeHandoverReachesTheRuntimeThatRunsIt(t *testing.T) {
	n, _ := runtimeNode(t)
	files := companion(t, n, "session-h1")
	web := dialRuntime(t, n, dshHello())
	web.answer = func(method string, params json.RawMessage) (any, error) {
		if method == "session.items" { // not its session until it runs it
			return nil, errors.New("not found")
		}
		return map[string]any{"ok": true}, nil
	}
	var once sync.Once
	files.answer = func(method string, params json.RawMessage) (any, error) {
		if method == "session.send" {
			// Woken: the program that runs it starts, and its runtime lists the session as running.
			once.Do(func() {
				go func() {
					time.Sleep(300 * time.Millisecond)
					_ = web.peer.Notify("runtime.sessions", map[string]any{"sessions": []map[string]any{{"session_id": "session-h1", "status": "done"}}})
				}()
			})
			return nil, handoverError()
		}
		return map[string]any{"ok": true}, nil
	}
	if _, err := nodeCall(t, n, "session.send", map[string]any{"session_id": "session-h1", "text": "hi", "client_msg_id": "m-h1"}); err != nil {
		t.Fatal(err)
	}
	sent := web.got("session.send")
	if len(sent) != 1 {
		t.Fatalf("the runtime running it got %d sends", len(sent))
	}
	var p struct {
		Text        string `json:"text"`
		ClientMsgID string `json:"client_msg_id"`
	}
	_ = json.Unmarshal(sent[0], &p)
	if p.Text != "hi" || p.ClientMsgID != "m-h1" {
		t.Fatalf("forwarded %s", sent[0])
	}
	if s := listed(t, n)["session-h1"]; s.Status != "done" || s.Agent != "dsh" {
		t.Fatalf("listed %+v", s)
	}
}

func TestRuntimeHandoverAsksTheOthersAboutAnOlderSession(t *testing.T) {
	n, _ := runtimeNode(t)
	files := companion(t, n, "session-h2")
	files.answer = func(method string, params json.RawMessage) (any, error) {
		if method == "session.send" {
			return nil, handoverError()
		}
		return map[string]any{"ok": true}, nil
	}
	web := dialRuntime(t, n, dshHello()) // it does not list the session, but has it
	web.answer = func(method string, params json.RawMessage) (any, error) {
		if method == "session.items" {
			return map[string]any{"items": []proto.Item{}, "history": "dsh:session-h2", "to_seq": 4}, nil
		}
		return map[string]any{"ok": true}, nil
	}
	if _, err := nodeCall(t, n, "session.send", map[string]any{"session_id": "session-h2", "text": "hi", "client_msg_id": "m-h2"}); err != nil {
		t.Fatal(err)
	}
	if len(web.got("session.send")) != 1 {
		t.Fatalf("the runtime that has it got %d sends", len(web.got("session.send")))
	}
}

func TestRuntimeHandoverNobodyTakesItNothingSent(t *testing.T) {
	old := runtimeHandoverWait
	runtimeHandoverWait = 700 * time.Millisecond
	t.Cleanup(func() { runtimeHandoverWait = old })
	n, _ := runtimeNode(t)
	files := companion(t, n, "session-h3")
	files.answer = func(method string, params json.RawMessage) (any, error) {
		if method == "session.send" {
			return nil, handoverError()
		}
		return map[string]any{"ok": true}, nil
	}
	_, err := nodeCall(t, n, "session.send", map[string]any{"session_id": "session-h3", "text": "hi", "client_msg_id": "m-h3"})
	var re *rpc.Error
	if !errors.As(err, &re) || re.Code != "not_running" || maybeDelivered(err) {
		t.Fatalf("nobody took it: %v", err)
	}
	// Not delivered: the same message may be sent again (it reaches the companion again).
	_, _ = nodeCall(t, n, "session.send", map[string]any{"session_id": "session-h3", "text": "hi", "client_msg_id": "m-h3"})
	if len(files.got("session.send")) != 2 {
		t.Fatalf("the resend reached the companion %d times", len(files.got("session.send")))
	}
}
