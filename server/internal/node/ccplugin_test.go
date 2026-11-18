package node

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/acceptloop"
)

// pluginSocket serves n's node socket at a short path of its own (the node's own path is the user's), and gives a
// client for it: what the Persona plugin's $.http.fetch is, from this test's process (the claude whose sessions/<pid>.json
// writeOutsideClaude writes).
func pluginSocket(t *testing.T, n *Node) *http.Client {
	t.Helper()
	dir, err := os.MkdirTemp("", "ccp")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "n.sock")
	ln, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	go acceptloop.Run(ctx, ln, t.Logf, func(c net.Conn) { go n.serveSock(ctx, c) })
	t.Cleanup(func() { cancel(); _ = ln.Close(); _ = os.RemoveAll(dir) })
	return &http.Client{Timeout: 10 * time.Second, Transport: &http.Transport{DisableKeepAlives: true,
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, "unix", path)
		}}}
}

// pluginPost is one request of the plugin's: the status and the JSON answer.
func pluginPost(t *testing.T, c *http.Client, what string, body map[string]any) (int, map[string]any) {
	t.Helper()
	b, _ := json.Marshal(body)
	res, err := c.Post("http://persona/claude-plugin/v1/"+what, "application/json", strings.NewReader(string(b)))
	if err != nil {
		t.Fatalf("%s: %v", what, err)
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(res.Body)
	out := map[string]any{}
	_ = json.Unmarshal(raw, &out)
	return res.StatusCode, out
}

// fakePlugin is Persona's plugin in a session: it polls, and does with each message what its mode says. "submit"
// writes the prompt into the transcript as Claude Code does for a plugin's asUser prompt, then says submitted;
// "refuse" says it did not submit; "silent" takes the message and does nothing.
type fakePlugin struct {
	t          *testing.T
	c          *http.Client
	session    string
	transcript string

	mu   sync.Mutex
	mode string
	got  []map[string]any
}

func startFakePlugin(t *testing.T, c *http.Client, session, transcript, mode string) *fakePlugin {
	p := &fakePlugin{t: t, c: c, session: session, transcript: transcript, mode: mode}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	t.Cleanup(func() { cancel(); <-done })
	go func() {
		defer close(done)
		for ctx.Err() == nil {
			b, _ := json.Marshal(map[string]any{"session_id": session, "plugin_version": "0.1.0", "engine_version": "2.1.308", "wait_ms": 1000})
			req, _ := http.NewRequestWithContext(ctx, http.MethodPost, "http://persona/claude-plugin/v1/poll", strings.NewReader(string(b)))
			res, err := c.Do(req)
			if err != nil {
				select {
				case <-ctx.Done():
				case <-time.After(20 * time.Millisecond):
				}
				continue
			}
			var answer struct {
				Messages []map[string]any `json:"messages"`
			}
			_ = json.NewDecoder(res.Body).Decode(&answer)
			res.Body.Close()
			for _, m := range answer.Messages {
				p.take(m)
			}
		}
	}()
	return p
}

func (p *fakePlugin) take(m map[string]any) {
	p.mu.Lock()
	p.got = append(p.got, m)
	mode := p.mode
	p.mu.Unlock()
	text, _ := m["text"].(string)
	switch mode {
	case "submit":
		line, _ := json.Marshal(map[string]any{"type": "user", "uuid": newUUID(), "sessionId": p.session,
			"timestamp": time.Now().UTC().Format(time.RFC3339Nano), "message": map[string]any{"role": "user", "content": text},
			"origin": map[string]any{"kind": "plugin", "name": "persona", "asUser": true}, "promptSource": "system"})
		f, err := os.OpenFile(p.transcript, os.O_APPEND|os.O_WRONLY, 0o600)
		if err == nil {
			_, _ = f.Write(append(line, '\n'))
			_ = f.Close()
		}
		pluginPost(p.t, p.c, "ack", map[string]any{"session_id": p.session, "id": m["id"], "submitted": true})
	case "refuse":
		pluginPost(p.t, p.c, "ack", map[string]any{"session_id": p.session, "id": m["id"], "submitted": false, "reason": "late"})
	}
}

func (p *fakePlugin) messages() []map[string]any {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]map[string]any(nil), p.got...)
}

// waitPolling waits until the node has the plugin's poll waiting.
func waitPolling(t *testing.T, n *Node, session string) {
	t.Helper()
	waitFor(t, "the plugin's poll", func() bool {
		n.ccPlugins.mu.Lock()
		defer n.ccPlugins.mu.Unlock()
		s := n.ccPlugins.sessions[session]
		return s != nil && s.poll != nil
	})
}

func shortPluginWaits(t *testing.T) {
	oldPick, oldSubmit, oldConfirm := ccPluginPickupWait, ccPluginSubmitWait, ccPluginConfirmWait
	ccPluginPickupWait, ccPluginSubmitWait, ccPluginConfirmWait = 500*time.Millisecond, 300*time.Millisecond, 600*time.Millisecond
	t.Cleanup(func() { ccPluginPickupWait, ccPluginSubmitWait, ccPluginConfirmWait = oldPick, oldSubmit, oldConfirm })
}

// The owner's own message to a Claude at rest in the desktop app goes in through Persona's plugin, as the person's
// input: the inbox gets nothing, the answer says so, and the session's row says the plugin is there.
func TestCCPluginTakesTheOwnersMessage(t *testing.T) {
	n, f, home := herdrNode(t)
	noPaneEnv(t)
	shortPeerWait(t, 2*time.Second)
	shortPluginWaits(t)
	p := bgTranscript(t, home, home, sidA, "plan")
	inbox := newFakeInbox(t, p)
	writeOutsideClaude(t, home, sidA, "claude-desktop", "idle", inbox, nil)
	f.reply("session.snapshot", snapshot(nil, nil, nil, nil))
	c := pluginSocket(t, n)

	if s := byID(listSessions(t, n))[sidA]; s.Capabilities == nil || s.Capabilities.PluginInput {
		t.Fatalf("plugin_input before any poll: %+v", s.Capabilities)
	}
	plugin := startFakePlugin(t, c, sidA, p, "submit")
	waitPolling(t, n, sidA)
	if s := byID(listSessions(t, n))[sidA]; s.Capabilities == nil || !s.Capabilities.PluginInput || !s.Capabilities.Send {
		t.Fatalf("plugin_input with the plugin polling: %+v", s.Capabilities)
	}

	res, err := request(t, n, "session.send", map[string]any{"session_id": sidA, "text": "yes, go ahead and merge", "client_msg_id": "m-1", "owner_input": true})
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	if res["ok"] != true || res["delivered_as"] != "user_input" || res["relay_reason"] != nil {
		t.Errorf("answer %v", res)
	}
	if got := plugin.messages(); len(got) != 1 || got[0]["text"] != "yes, go ahead and merge" {
		t.Errorf("the plugin got %v", got)
	}
	if len(inbox.users()) != 0 {
		t.Errorf("the inbox got %v as well", inbox.users())
	}
}

// Everyone else's message keeps the inbox, the plugin there or not: one without owner_input never reaches the plugin
// and its answer says nothing of the way it went.
func TestCCPluginOnlyForTheOwner(t *testing.T) {
	n, f, home := herdrNode(t)
	noPaneEnv(t)
	shortPeerWait(t, 2*time.Second)
	shortPluginWaits(t)
	p := bgTranscript(t, home, home, sidA, "plan")
	inbox := newFakeInbox(t, p)
	writeOutsideClaude(t, home, sidA, "cli", "idle", inbox, nil)
	f.reply("session.snapshot", snapshot(nil, nil, nil, nil))
	plugin := startFakePlugin(t, pluginSocket(t, n), sidA, p, "submit")
	waitPolling(t, n, sidA)

	res, err := request(t, n, "session.send", map[string]any{"session_id": sidA, "text": "the assistant says hi", "client_msg_id": "m-1"})
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	if res["delivered_as"] != nil || len(plugin.messages()) != 0 || len(inbox.users()) != 1 {
		t.Errorf("answer %v, plugin %v, inbox %d", res, plugin.messages(), len(inbox.users()))
	}
}

// When the plugin route does not go, the owner's message goes through the inbox and the answer says why: a session
// at work, no plugin, a plugin that will not submit it, one that takes it and shows nothing before its deadline.
func TestCCPluginFallsBackToTheInbox(t *testing.T) {
	for _, c := range []struct {
		name, status, mode string // mode "": no plugin
		reason             string
		pluginGets         int
	}{
		{"busy", "busy", "submit", relayBusy, 0},
		{"behind a dialog", "waiting", "submit", relayBusy, 0},
		{"no plugin", "idle", "", relayNoPlugin, 0},
		{"refused", "idle", "refuse", relayRefused, 1},
		{"silent", "idle", "silent", relayUnconfirmed, 1},
	} {
		t.Run(c.name, func(t *testing.T) {
			n, f, home := herdrNode(t)
			noPaneEnv(t)
			shortPeerWait(t, 2*time.Second)
			shortPluginWaits(t)
			p := bgTranscript(t, home, home, sidA, "plan")
			inbox := newFakeInbox(t, p)
			writeOutsideClaude(t, home, sidA, "claude-desktop", c.status, inbox, nil)
			f.reply("session.snapshot", snapshot(nil, nil, nil, nil))
			var plugin *fakePlugin
			if c.mode != "" {
				plugin = startFakePlugin(t, pluginSocket(t, n), sidA, p, c.mode)
				waitPolling(t, n, sidA)
			}
			res, err := request(t, n, "session.send", map[string]any{"session_id": sidA, "text": "merge it", "client_msg_id": "m-1", "owner_input": true})
			if err != nil {
				t.Fatalf("send: %v", err)
			}
			if res["ok"] != true || res["delivered_as"] != "relayed" || res["relay_reason"] != c.reason {
				t.Errorf("answer %v", res)
			}
			if len(inbox.users()) != 1 {
				t.Errorf("the inbox got %d messages", len(inbox.users()))
			}
			if plugin != nil && len(plugin.messages()) != c.pluginGets {
				t.Errorf("the plugin got %v", plugin.messages())
			}
		})
	}
}

// The node hands a session's messages only to the claude that has it open: a poll naming another session is refused,
// and a claude the node does not deliver to this way (one an SDK runs) is told to come back much later.
func TestCCPluginPollsAreTheSessionsOwn(t *testing.T) {
	n, f, home := herdrNode(t)
	noPaneEnv(t)
	p := bgTranscript(t, home, home, sidA, "plan")
	writeOutsideClaude(t, home, sidA, "cli", "idle", newFakeInbox(t, p), nil)
	f.reply("session.snapshot", snapshot(nil, nil, nil, nil))
	c := pluginSocket(t, n)

	status, out := pluginPost(t, c, "poll", map[string]any{"session_id": sidB, "wait_ms": 100})
	if status != http.StatusForbidden || out["error"] != "not_this_session" {
		t.Errorf("another session's poll: %d %v", status, out)
	}
	if status, _ := pluginPost(t, c, "poll", map[string]any{"session_id": "../x", "wait_ms": 100}); status != http.StatusBadRequest {
		t.Errorf("a poll with no session id: %d", status)
	}
	if status, _ := pluginPost(t, c, "nothing", map[string]any{"session_id": sidA}); status != http.StatusNotFound {
		t.Errorf("an unknown request: %d", status)
	}
	status, out = pluginPost(t, c, "poll", map[string]any{"session_id": sidA, "wait_ms": 100})
	if status != http.StatusOK || out["retry_ms"] != nil {
		t.Errorf("its own poll: %d %v", status, out)
	}

	writeOutsideClaude(t, home, sidA, "sdk-ts", "idle", nil, nil)
	status, out = pluginPost(t, c, "poll", map[string]any{"session_id": sidA, "wait_ms": 100})
	if status != http.StatusOK || out["retry_ms"] != float64(ccPluginOtherRetry.Milliseconds()) {
		t.Errorf("an SDK's claude: %d %v", status, out)
	}
}

// claude-plugin.status lists the sessions whose plugin polls, for installers and the doctor.
func TestCCPluginStatus(t *testing.T) {
	n, f, home := herdrNode(t)
	noPaneEnv(t)
	p := bgTranscript(t, home, home, sidA, "plan")
	writeOutsideClaude(t, home, sidA, "claude-desktop", "idle", newFakeInbox(t, p), nil)
	f.reply("session.snapshot", snapshot(nil, nil, nil, nil))
	c := pluginSocket(t, n)
	ask := func() map[string]any {
		dir, _ := os.MkdirTemp("", "ccs")
		t.Cleanup(func() { _ = os.RemoveAll(dir) })
		path := filepath.Join(dir, "n.sock")
		ln, err := net.Listen("unix", path)
		if err != nil {
			t.Fatal(err)
		}
		defer ln.Close()
		go func() {
			if s, err := ln.Accept(); err == nil {
				n.serveSock(context.Background(), s)
			}
		}()
		conn, err := net.Dial("unix", path)
		if err != nil {
			t.Fatal(err)
		}
		defer conn.Close()
		fmt.Fprintln(conn, `{"kind":"claude-plugin.status"}`)
		line, _ := bufio.NewReader(conn).ReadBytes('\n')
		out := map[string]any{}
		_ = json.Unmarshal(line, &out)
		return out
	}
	if out := ask(); out["protocol"] != float64(1) || len(out["sessions"].([]any)) != 0 {
		t.Errorf("before a poll: %v", out)
	}
	startFakePlugin(t, c, sidA, p, "submit")
	waitPolling(t, n, sidA)
	out := ask()
	list, _ := out["sessions"].([]any)
	if len(list) != 1 {
		t.Fatalf("status %v", out)
	}
	s := list[0].(map[string]any)
	if s["session_id"] != sidA || s["pid"] != float64(os.Getpid()) || s["plugin_version"] != "0.1.0" || s["engine_version"] != "2.1.308" || s["polling"] != true {
		t.Errorf("status entry %v", s)
	}
}

// Only Persona's own prompt submitted as the person's words counts as taken: not another plugin's, not one framed as
// a plugin's message, not a peer's; Claude Code's queue entry for it does.
func TestCCPluginLineSeen(t *testing.T) {
	const needle = "merge it"
	for _, c := range []struct {
		line string
		want bool
	}{
		{`{"type":"user","message":{"role":"user","content":"merge it now"},"origin":{"kind":"plugin","name":"persona","asUser":true}}`, true},
		{`{"type":"user","message":{"role":"user","content":"The persona plugin sent a message:\nmerge it"},"origin":{"kind":"plugin","name":"persona"}}`, false},
		{`{"type":"user","message":{"role":"user","content":"merge it"},"origin":{"kind":"plugin","name":"other","asUser":true}}`, false},
		{`{"type":"user","message":{"role":"user","content":"merge it"},"isMeta":true,"origin":{"kind":"peer","name":"Persona"}}`, false},
		{`{"type":"queue-operation","operation":"enqueue","content":"merge it"}`, true},
		{`{"type":"queue-operation","operation":"dequeue","content":"merge it"}`, false},
		{`{"type":"user","message":{"role":"user","content":"something else"},"origin":{"kind":"plugin","name":"persona","asUser":true}}`, false},
	} {
		if got := ccPluginLineSeen([]byte(c.line), needle); got != c.want {
			t.Errorf("%s: %v", c.line, got)
		}
	}
}

// Only an HTTP request line is the plugin's; the JSON requests keep their way.
func TestIsHTTPRequestLine(t *testing.T) {
	for line, want := range map[string]bool{
		"POST /claude-plugin/v1/poll HTTP/1.1\r\n": true,
		"GET / HTTP/1.1\r\n":                       true,
		`{"kind":"ping"}` + "\n":                   false,
		"POSTAL\n":                                 false,
	} {
		if isHTTPRequestLine([]byte(line)) != want {
			t.Errorf("%q", line)
		}
	}
}
