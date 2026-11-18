package mcpserver

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/nodeclient"
)

func listRequest() request {
	return request{JSONRPC: "2.0", ID: json.RawMessage(`2`), Method: "tools/list"}
}

// fakeNode answers the n-th dial (from 1) with dial(n).
func fakeNode(t *testing.T, dial func(n int) ([]byte, error)) *int {
	t.Helper()
	oldDial, oldEvery := dialNode, retryEvery
	t.Cleanup(func() { dialNode, retryEvery = oldDial, oldEvery })
	retryEvery = time.Millisecond
	n := 0
	dialNode = func(context.Context, any) ([]byte, error) { n++; return dial(n) }
	return &n
}

func shortCtx(t *testing.T) context.Context {
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	t.Cleanup(cancel)
	return ctx
}

func TestToolsListWithoutNodeIsAnError(t *testing.T) {
	n := fakeNode(t, func(int) ([]byte, error) { return nil, errors.New("dial unix node.sock: no such file or directory") })
	res, rerr := handle(shortCtx(t), "test", listRequest())
	if rerr == nil {
		t.Fatalf("no node: want an error, got %+v", res)
	}
	if !strings.Contains(rerr.Message, "no such file") {
		t.Fatalf("error should say why: %q", rerr.Message)
	}
	if *n < 2 {
		t.Fatalf("dialed %d times, want retries", *n)
	}
}

func TestToolsListNodeErrorIsAnError(t *testing.T) {
	fakeNode(t, func(int) ([]byte, error) { return []byte(`{"tools":[],"error":"hub not connected"}`), nil })
	_, rerr := handle(shortCtx(t), "test", listRequest())
	if rerr == nil || !strings.Contains(rerr.Message, "hub not connected") {
		t.Fatalf("node reported an error: want it passed on, got %+v", rerr)
	}
}

func TestToolsListRetriesUntilTheNodeIsUp(t *testing.T) {
	fakeNode(t, func(n int) ([]byte, error) {
		switch {
		case n == 1:
			return nil, errors.New("dial unix node.sock: connection refused")
		case n == 2:
			return []byte(`{"tools":[],"error":"hub not connected"}`), nil
		}
		return []byte(`{"tools":[{"name":"phone_health_summary","description":"health"}]}`), nil
	})
	res, rerr := handle(context.Background(), "test", listRequest())
	if rerr != nil {
		t.Fatalf("unexpected error %+v", rerr)
	}
	tools := res.(map[string]any)["tools"].([]map[string]any)
	if len(tools) != 1 || tools[0]["name"] != "phone_health_summary" || tools[0]["inputSchema"] == nil {
		t.Fatalf("tools = %+v", tools)
	}
}

func TestToolsListAssistantToolsAreAlwaysLoaded(t *testing.T) {
	t.Setenv("PERSONA_ASSISTANT", "1")
	fakeNode(t, func(int) ([]byte, error) { return []byte(`{"tools":[{"name":"ask_user","description":"ask"}]}`), nil })
	res, rerr := handle(context.Background(), "test", listRequest())
	if rerr != nil {
		t.Fatal(rerr)
	}
	tools := res.(map[string]any)["tools"].([]map[string]any)
	meta, ok := tools[0]["_meta"].(map[string]any)
	if !ok || meta["anthropic/alwaysLoad"] != true {
		t.Fatalf("assistant tool lacks alwaysLoad metadata: %+v", tools[0])
	}
}

func TestToolsListAgentToolsHaveNoAlwaysLoadMarker(t *testing.T) {
	t.Setenv("PERSONA_ASSISTANT", "0")
	fakeNode(t, func(int) ([]byte, error) {
		return []byte(`{"tools":[{"name":"phone_health_summary","description":"health"}]}`), nil
	})
	res, rerr := handle(context.Background(), "test", listRequest())
	if rerr != nil {
		t.Fatal(rerr)
	}
	tools := res.(map[string]any)["tools"].([]map[string]any)
	if _, present := tools[0]["_meta"]; present {
		t.Fatalf("a non-assistant tool unexpectedly carries metadata: %+v", tools[0])
	}
}

func TestToolsListEmptyFromAHealthyNodeIsNotAnError(t *testing.T) {
	n := fakeNode(t, func(int) ([]byte, error) { return []byte(`{"tools":[]}`), nil })
	res, rerr := handle(shortCtx(t), "test", listRequest())
	if rerr != nil {
		t.Fatalf("unexpected error %+v", rerr)
	}
	if tools := res.(map[string]any)["tools"].([]map[string]any); len(tools) != 0 || *n != 1 {
		t.Fatalf("tools = %+v after %d dials", tools, *n)
	}
}

func callRequest() request {
	return request{JSONRPC: "2.0", ID: json.RawMessage(`3`), Method: "tools/call", Params: json.RawMessage(`{"name":"notify_user","arguments":{}}`)}
}

// A tool call made while the resident node's socket is gone for a hub restart waits for it; one the node received
// and dropped is not sent again.
func TestToolsCallWaitsForTheNodeSocket(t *testing.T) {
	n := fakeNode(t, func(n int) ([]byte, error) {
		if n < 3 {
			return nil, fmt.Errorf("%w: dial unix node.sock: no such file or directory", nodeclient.ErrNoNode)
		}
		return []byte(`{"content":[{"type":"text","text":"sent"}]}`), nil
	})
	res, rerr := handle(context.Background(), "test", callRequest())
	if rerr != nil || *n != 3 || !strings.Contains(fmt.Sprint(res), "sent") {
		t.Fatalf("call across the gap: %+v %+v after %d dials", res, rerr, *n)
	}
	n = fakeNode(t, func(int) ([]byte, error) { return nil, errors.New("node closed connection: EOF") })
	res, _ = handle(context.Background(), "test", callRequest())
	if *n != 1 || !strings.Contains(fmt.Sprint(res), "may have run") {
		t.Fatalf("a call the node may have run was sent %d times: %+v", *n, res)
	}
}

// A failed tool call tells the model whether the tool may have run: only a call nothing received is safe to retry,
// so mail_send or agent_send are not sent twice after a dropped connection or a timeout.
func TestToolsCallFailureSaysWhetherTheToolMayHaveRun(t *testing.T) {
	oldRedial := callRedialFor
	t.Cleanup(func() { callRedialFor = oldRedial })
	callRedialFor = 0
	expired, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	t.Cleanup(cancel)
	for _, tc := range []struct {
		name      string
		ctx       context.Context
		dial      func(int) ([]byte, error)
		want, not []string
	}{
		{"no node", context.Background(),
			func(int) ([]byte, error) {
				return nil, fmt.Errorf("%w: dial unix node.sock: connection refused", nodeclient.ErrNoNode)
			},
			[]string{"not reachable", "was not run", "connection refused"}, []string{"may have run"}},
		{"connection lost after sending", context.Background(),
			func(int) ([]byte, error) { return nil, errors.New("node closed connection: EOF") },
			[]string{"after sending the call", "may have run", "check", "EOF"}, []string{"not reachable"}},
		{"deadline", expired,
			func(int) ([]byte, error) { return nil, errors.New("node closed connection: read unix: i/o timeout") },
			[]string{"timed out", "may still have run", "check"}, []string{"not reachable"}},
		{"non-JSON reply", context.Background(),
			func(int) ([]byte, error) { return []byte("panic: oops\n"), nil },
			[]string{"bad response from Persona node", "invalid character"}, []string{"not reachable"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fakeNode(t, tc.dial)
			res, rerr := handle(tc.ctx, "test", callRequest())
			if rerr != nil {
				t.Fatalf("unexpected rpc error %+v", rerr)
			}
			r, ok := res.(map[string]any)
			if !ok || r["isError"] != true {
				t.Fatalf("isError not set: %#v", res)
			}
			text := fmt.Sprint(r["content"])
			for _, w := range tc.want {
				if !strings.Contains(text, w) {
					t.Errorf("text %q lacks %q", text, w)
				}
			}
			for _, w := range tc.not {
				if strings.Contains(text, w) {
					t.Errorf("text %q contains %q", text, w)
				}
			}
		})
	}
}

// syncBuf is an io.Writer the test reads while Serve writes.
type syncBuf struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuf) Write(p []byte) (int, error) { s.mu.Lock(); defer s.mu.Unlock(); return s.b.Write(p) }
func (s *syncBuf) String() string              { s.mu.Lock(); defer s.mu.Unlock(); return s.b.String() }

func waitFor(t *testing.T, what string, ok func() bool) {
	t.Helper()
	for deadline := time.Now().Add(3 * time.Second); !ok(); {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(2 * time.Millisecond)
	}
}

// A tool call waiting for the phone holds no other request back; one Claude Code cancels (the user stopped the turn)
// hangs up on the node, so its card comes off the phone, and gets no answer.
func TestServeRunsCallsAtOnceAndCancelsThem(t *testing.T) {
	old := dialNode
	t.Cleanup(func() { dialNode = old })
	var mu sync.Mutex
	hungUp := map[string]bool{}
	dialNode = func(ctx context.Context, req any) ([]byte, error) {
		name, _ := req.(map[string]any)["name"].(string)
		if name == "mail_send" { // waits for the phone until its caller goes away
			<-ctx.Done()
			mu.Lock()
			hungUp[name] = true
			mu.Unlock()
			return nil, ctx.Err()
		}
		return []byte(`{"content":[{"type":"text","text":"here"}]}`), nil
	}
	in, w := io.Pipe()
	out := &syncBuf{}
	done := make(chan error, 1)
	go func() { done <- Serve(context.Background(), "test", in, out) }()
	send := func(s string) { _, _ = io.WriteString(w, s+"\n") }

	send(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"mail_send","arguments":{}}}`)
	send(`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"phone_location_current","arguments":{}}}`)
	waitFor(t, "the second call answered while the first waits", func() bool { return strings.Contains(out.String(), `"id":2`) })
	if strings.Contains(out.String(), `"id":1`) {
		t.Fatalf("the waiting call answered: %s", out.String())
	}

	send(`{"jsonrpc":"2.0","method":"notifications/cancelled","params":{"requestId":1,"reason":"interrupted"}}`)
	waitFor(t, "the cancelled call hung up on the node", func() bool { mu.Lock(); defer mu.Unlock(); return hungUp["mail_send"] })
	send(`{"jsonrpc":"2.0","id":3,"method":"ping"}`)
	waitFor(t, "ping", func() bool { return strings.Contains(out.String(), `"id":3`) })
	_ = w.Close()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.String(), `"id":1`) {
		t.Fatalf("a cancelled call was answered: %s", out.String())
	}
}

// The client going away (stdin closed) ends what still waits: no card is left on the phone for a dead process.
func TestServeEndsCallsWhenTheClientGoes(t *testing.T) {
	old := dialNode
	t.Cleanup(func() { dialNode = old })
	ended := make(chan struct{})
	dialNode = func(ctx context.Context, req any) ([]byte, error) {
		<-ctx.Done()
		close(ended)
		return nil, ctx.Err()
	}
	in := strings.NewReader(`{"jsonrpc":"2.0","id":"a","method":"tools/call","params":{"name":"ask_user","arguments":{}}}` + "\n")
	done := make(chan error, 1)
	go func() { done <- Serve(context.Background(), "test", in, &syncBuf{}) }()
	select {
	case <-ended:
	case <-time.After(3 * time.Second):
		t.Fatal("the call kept waiting after the client went away")
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

// A line that is not a JSON-RPC request gets a JSON-RPC error instead of silence, so the client learns why instead
// of waiting for its own timeout: -32700 when it is not JSON, -32600 when it is JSON but not a request (echoing a usable
// id). Blank lines and notifications stay unanswered, and the server keeps answering what follows.
func TestServeAnswersMalformedLines(t *testing.T) {
	in, w := io.Pipe()
	out := &syncBuf{}
	done := make(chan error, 1)
	go func() { done <- Serve(context.Background(), "test", in, out) }()
	send := func(s string) { _, _ = io.WriteString(w, s+"\n") }

	send(`not json`)
	send(`{"jsonrpc":"2.0","id":4,"method":"ping"`) // cut short
	send(`[1,2]`)
	send(`null`)
	send(`{"jsonrpc":"2.0","id":9}`)
	send(`{"jsonrpc":"2.0","id":"a","method":7}`)
	send(`{"jsonrpc":"2.0","id":{},"method":""}`)
	send(``)
	send(`{"jsonrpc":"2.0"}`)
	send(`{"jsonrpc":"2.0","method":"notifications/initialized"}`)
	send(`{"jsonrpc":"2.0","id":3,"method":"ping"}`)
	waitFor(t, "ping after the malformed lines", func() bool { return strings.Contains(out.String(), `"id":3`) })
	_ = w.Close()
	if err := <-done; err != nil {
		t.Fatal(err)
	}

	got := map[string]int{}
	for _, line := range strings.Split(strings.TrimSpace(out.String()), "\n") {
		var r struct {
			ID    json.RawMessage `json:"id"`
			Error *rpcError       `json:"error"`
		}
		if err := json.Unmarshal([]byte(line), &r); err != nil {
			t.Fatalf("unparsable answer %q: %v", line, err)
		}
		code := 0
		if r.Error != nil {
			code = r.Error.Code
		}
		got[fmt.Sprintf("%d %s", code, r.ID)]++
	}
	want := map[string]int{"-32700 null": 2, "-32600 null": 3, "-32600 9": 1, `-32600 "a"`: 1, "0 3": 1}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("answers (code id: count) = %v, want %v:\n%s", got, want, out.String())
	}
}

func TestIDKey(t *testing.T) {
	if idKey(json.RawMessage(` 7`)) != idKey(json.RawMessage(`7`)) || idKey(json.RawMessage(`"7"`)) == idKey(json.RawMessage(`7`)) {
		t.Fatal("ids are keyed by value, a string apart from a number")
	}
}

// A tool call without a name, or with arguments that are not a JSON object, is invalid params (-32602) and never
// reaches the node: the handlers behind it would run on zero values.
func TestToolsCallRejectsInvalidParams(t *testing.T) {
	for _, params := range []string{
		`{"arguments":{}}`,
		`{"name":"","arguments":{}}`,
		`{"name":"  ","arguments":{}}`,
		`{"name":"conversations_list","arguments":[1]}`,
		`{"name":"conversations_list","arguments":"q"}`,
		`{"name":"conversations_list","arguments":3}`,
		`{"name":"conversations_list","arguments":true}`,
		`{"name":"conversations_list","arguments": [ ]}`,
	} {
		n := fakeNode(t, func(int) ([]byte, error) { return []byte(`{"content":[{"type":"text","text":"ran"}]}`), nil })
		req := request{JSONRPC: "2.0", ID: json.RawMessage(`4`), Method: "tools/call", Params: json.RawMessage(params)}
		res, rerr := handle(context.Background(), "test", req)
		if rerr == nil || rerr.Code != -32602 || *n != 0 {
			t.Fatalf("%s: res %+v, err %+v after %d dials", params, res, rerr, *n)
		}
	}
}

// Absent or null arguments reach the node as an empty object; an object is passed on as sent.
func TestToolsCallSendsArgumentsAsAnObject(t *testing.T) {
	for params, want := range map[string]string{
		`{"name":"notify_user"}`:                            `{}`,
		`{"name":"notify_user","arguments":null}`:           `{}`,
		`{"name":"notify_user","arguments": {"text":"hi"}}`: `{"text":"hi"}`,
	} {
		old := dialNode
		var got string
		dialNode = func(_ context.Context, req any) ([]byte, error) {
			b, _ := json.Marshal(req.(map[string]any)["args"])
			got = string(b)
			return []byte(`{"content":[{"type":"text","text":"sent"}]}`), nil
		}
		req := request{JSONRPC: "2.0", ID: json.RawMessage(`5`), Method: "tools/call", Params: json.RawMessage(params)}
		_, rerr := handle(context.Background(), "test", req)
		dialNode = old
		if rerr != nil || got != want {
			t.Fatalf("%s: node got args %s (want %s), err %+v", params, got, want, rerr)
		}
	}
}

// Over the real request path the client gets a -32602 error answer for its id.
func TestToolsCallInvalidParamsOverServe(t *testing.T) {
	n := fakeNode(t, func(int) ([]byte, error) { return []byte(`{"content":[{"type":"text","text":"ran"}]}`), nil })
	in, w := io.Pipe()
	out := &syncBuf{}
	done := make(chan error, 1)
	go func() { done <- Serve(context.Background(), "test", in, out) }()
	// stdin stays open until the answer is out: Serve does not answer what runs when the client goes away.
	_, _ = io.WriteString(w, `{"jsonrpc":"2.0","id":7,"method":"tools/call","params":{"name":"conversations_list","arguments":[1]}}`+"\n")
	waitFor(t, "the answer", func() bool { return strings.Contains(out.String(), `"id":7`) })
	_ = w.Close()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	var resp struct {
		ID    int       `json:"id"`
		Error *rpcError `json:"error"`
	}
	if err := json.Unmarshal([]byte(out.String()), &resp); err != nil {
		t.Fatalf("answer %q: %v", out.String(), err)
	}
	if resp.ID != 7 || resp.Error == nil || resp.Error.Code != -32602 || *n != 0 {
		t.Fatalf("answer %s after %d dials", out.String(), *n)
	}
}
