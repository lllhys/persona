package node

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"sync"
	"testing"
	"time"
)

// claudeEnvelopeRe is Claude Code 2.1.326's parser of the cross-session envelope (its jl), with the attribute order
// and the from-mode classes it accepts; an envelope it does not match carries no from-mode.
var claudeEnvelopeRe = regexp.MustCompile(`^<cross-session-message(?: from="([A-Za-z0-9%:_/.\\-]+)")?(?: from-session="([A-Za-z0-9_-]{1,80})")?(?: hop-chain="([0-9a-f]{24}(?:,[0-9a-f]{24}){0,31})")?(?: from-name="([^"<>\n\r]+)")?(?: from-mode="(bypass|prompting)")?(?: from-plugin="([^"<>\n\r]+)")?>\n([\s\S]*)\n</cross-session-message>$`)

// claudeTakes is what a Claude Code 2.1.326 session of class (bypass / prompting), with crossSessionInbound unset,
// does with a peer message: a stated from-mode is taken only when it is the session's class; with none stated a
// bypass session holds it and a prompting one takes it.
func claudeTakes(class, content string) bool {
	m := claudeEnvelopeRe.FindStringSubmatch(content)
	if m != nil && m[5] != "" {
		return m[5] == class
	}
	return class != peerModeBypass
}

// fakeInbox is a Claude Code peer inbox as 2.1.308 runs one (claudepeer.go): a socket or pipe whose connections are
// one JSON object per line; the first line must be {"type":"auth","token":…} where the inbox requires auth; a user
// line is taken (the transcript gets a queue-operation, as the session writes when it queues a message), held (taken
// but not run, nothing written: a bypass session waiting for the person at the computer) or left alone. Nothing is
// ever written back on the connection.
type fakeInbox struct {
	t          *testing.T
	ln         net.Listener
	path       string // messagingSocketPath
	token      string
	requireAut bool
	transcript string // where a taken message is written

	mu    sync.Mutex
	mode  string           // "take" (default), "hold" (its notice written), "discard" (nothing written), "drop-auth" (close at once, like a bad auth line), or a session's class, "bypass" / "prompting" (claudeTakes, else hold); setMode
	lines []map[string]any // every line read, auth lines included
}

// setMode changes how the inbox treats the next connections. The accept loop is already running, and the socket
// between the node and the inbox orders nothing for the race detector, so the mode is written under the lock that
// each connection reads it under.
func (f *fakeInbox) setMode(mode string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.mode = mode
}

// newFakeInbox listens where a claude's messagingSocketPath would be (listenFakeInbox, per platform).
func newFakeInbox(t *testing.T, transcript string) *fakeInbox {
	t.Helper()
	ln, path := listenFakeInbox(t)
	f := &fakeInbox{t: t, ln: ln, path: path, token: "0123456789abcdef0123456789abcdef", requireAut: peerAuthRequired,
		transcript: transcript, mode: "take"}
	go f.serve()
	t.Cleanup(func() { _ = ln.Close() })
	return f
}

func (f *fakeInbox) serve() {
	for {
		c, err := f.ln.Accept()
		if err != nil {
			return
		}
		go f.conn(c)
	}
}

func (f *fakeInbox) conn(c net.Conn) {
	defer c.Close()
	f.mu.Lock()
	mode := f.mode
	f.mu.Unlock()
	if mode == "drop-auth" {
		return
	}
	sc := bufio.NewScanner(c)
	sc.Buffer(make([]byte, 64<<10), peerLineMax+64)
	authed := false
	first := true
	for sc.Scan() {
		var m map[string]any
		if json.Unmarshal(sc.Bytes(), &m) != nil {
			return
		}
		f.mu.Lock()
		f.lines = append(f.lines, m)
		f.mu.Unlock()
		if m["type"] == "auth" {
			if first {
				authed = m["token"] == f.token
				if !authed && f.requireAut {
					return
				}
			}
			first = false
			continue
		}
		first = false
		if f.requireAut && !authed {
			return
		}
		if m["type"] != "user" {
			continue
		}
		content, _ := m["message"].(map[string]any)["content"].(string)
		var line []byte
		switch {
		case mode == "discard":
			continue // refused, dropped or expired: nothing written
		case mode == "take" || (mode == peerModeBypass || mode == peerModePrompting) && claudeTakes(mode, content):
			line, _ = json.Marshal(map[string]any{"type": "queue-operation", "operation": "enqueue",
				"timestamp": time.Now().UTC().Format(time.RFC3339Nano), "content": "Another Claude session sent a message:\n" + content})
		default: // held for approval: Claude Code's notice in the transcript (peerHeldSeen)
			name := ""
			if e := claudeEnvelopeRe.FindStringSubmatch(content); e != nil {
				name = e[4]
			}
			line, _ = json.Marshal(map[string]any{"type": "system", "subtype": "informational", "level": "warning",
				"timestamp": time.Now().UTC().Format(time.RFC3339Nano),
				"content": "Held peer message — from an unidentified session [verified pid 1] (peer claims name: " + name +
					"); preview: «…» — not delivered to Claude (1 held). Review it below."})
		}
		file, err := os.OpenFile(f.transcript, os.O_APPEND|os.O_WRONLY, 0o600)
		if err == nil {
			_, _ = file.Write(append(line, '\n'))
			_ = file.Close()
		}
	}
}

// users are the user lines the inbox read.
func (f *fakeInbox) users() []map[string]any {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []map[string]any
	for _, l := range f.lines {
		if l["type"] == "user" {
			out = append(out, l)
		}
	}
	return out
}

// outsideClaude writes the sessions/<pid>.json of a claude outside Persona's panes (this test's own pid: alive),
// with its inbox, and the key file next to it.
func writeOutsideClaude(t *testing.T, home, id, entrypoint, status string, inbox *fakeInbox, extra map[string]any) {
	t.Helper()
	fields := map[string]any{"sessionId": id, "cwd": home, "kind": "interactive", "entrypoint": entrypoint, "status": status,
		"version": "2.1.308"}
	if inbox != nil {
		fields["messagingSocketPath"], fields["peerProtocol"] = inbox.path, 1
		fields["peerFeatures"] = []string{"notify_idle", "artifact_yield"}
	}
	for k, v := range extra {
		fields[k] = v
	}
	writeClaudeProc(t, home, os.Getpid(), fields)
	if inbox != nil {
		writeInboxKey(t, home, os.Getpid(), inbox.path, inbox.token)
	}
}

// writeInboxKey writes <pid>.<sha256>.key as Claude Code does.
func writeInboxKey(t *testing.T, home string, pid int, socket, token string) {
	t.Helper()
	name := peerKeyName(pid, socket)
	if name == "" {
		t.Fatalf("no key name for %s", socket)
	}
	b, _ := json.Marshal(map[string]any{"peerToken": token, "procStartFt": "1", "pidDomain": "test:host"})
	if err := os.WriteFile(filepath.Join(home, ".claude", "sessions", name), b, 0o600); err != nil {
		t.Fatal(err)
	}
}

// noPaneEnv makes every claude read as running outside the engines' panes (no pane variable in its environment).
func noPaneEnv(t *testing.T) {
	old := paneEnvOfPID
	paneEnvOfPID = func(int) map[string]string { return nil }
	t.Cleanup(func() { paneEnvOfPID = old })
}

// shortPeerWait shortens how long a send waits for the session to take a message.
func shortPeerWait(t *testing.T, d time.Duration) {
	old := peerConfirmWait
	peerConfirmWait = d
	t.Cleanup(func() { peerConfirmWait = old })
}

// fakeSocketName is a short unique name for a test inbox (an AF_UNIX path must stay under ~104 bytes).
func fakeSocketName() string {
	return fmt.Sprintf("p%d-%s", os.Getpid(), strconv.FormatInt(time.Now().UnixNano()%1e9, 36))
}
