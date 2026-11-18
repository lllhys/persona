package node

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/proto"
)

const echoLiteralText = "<system-reminder>literal user text</system-reminder>"

// A runtime can preserve markup that was literally typed instead of treating it as a hook's context. The exact raw
// user line is still the pending send's copy, and every item on that line carries its client_msg_id.
func TestEchoLiteralMarkupOnlyMatchAndTag(t *testing.T) {
	now := time.Now()
	var log echoLog
	if !log.expect(echoRecord{
		SessionID: "s", ClientMsgID: "c", Text: echoLiteralText,
		SentAt: now, In: echoInRuntime,
	}) {
		t.Fatal("expect rejected a new send")
	}
	items := []proto.Item{{
		ID: "user-line#0", Seq: 1, TS: now,
		Role: "user", Kind: proto.KindText, Text: echoLiteralText,
	}}
	found := log.match("s", echoInRuntime, items)
	if len(found) != 1 || found[0].Line != "user-line" {
		t.Fatalf("transcript contains the sent text, but match returned %+v", found)
	}
	sameTags(t, log.tag("s", echoInRuntime, items), map[string]string{"user-line#0": "c"})
}

// The raw-literal fallback must not turn unrelated markup, or two empty normalized strings, into a match. Each of
// these lines arrives while the literal send is still pending and before its real copy.
func TestEchoLiteralDoesNotClaimEarlierUnrelatedLines(t *testing.T) {
	cases := []struct {
		name string
		text string
	}{
		{"reminder only", "<system-reminder>an earlier unrelated reminder</system-reminder>"},
		{"context only", "<persona-context>an earlier unrelated context</persona-context>"},
		{"raw empty", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			now := time.Now()
			var log echoLog
			log.expect(echoRecord{SessionID: "s", ClientMsgID: "c", Text: echoLiteralText, SentAt: now, In: echoInRuntime})
			items := []proto.Item{{ID: "earlier#0", Seq: 1, TS: now, Role: "user", Kind: proto.KindText, Text: tc.text}}
			if found := log.match("s", echoInRuntime, items); len(found) != 0 {
				t.Fatalf("claimed an earlier unrelated line: %+v", found)
			}
			sameTags(t, log.tag("s", echoInRuntime, items), map[string]string{})
		})
	}
}

// Visible words after an unrelated context block must not revive raw-prefix matching: the pending literal's exact
// line is still later in the batch and is the only line that may receive its client_msg_id.
func TestEchoLiteralDoesNotClaimEarlierVisibleContext(t *testing.T) {
	const personaLiteral = "<persona-context>literal user text</persona-context>"
	cases := []struct {
		name    string
		literal string
		earlier string
	}{
		{
			name:    "system reminder",
			literal: echoLiteralText,
			earlier: "<system-reminder>an earlier unrelated reminder</system-reminder>\nvisible ordinary text",
		},
		{
			name:    "persona context",
			literal: personaLiteral,
			earlier: "<persona-context>an earlier unrelated context</persona-context>\nvisible ordinary text",
		},
		{
			name:    "long shared prefix",
			literal: "<system-reminder>" + strings.Repeat("界", echoTextLen) + "</system-reminder>",
			earlier: "<system-reminder>" + strings.Repeat("界", echoTextLen) + "</system-reminder>\nvisible ordinary text",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			now := time.Now()
			var log echoLog
			log.expect(echoRecord{SessionID: "s", ClientMsgID: "c", Text: tc.literal, SentAt: now, In: echoInRuntime})
			items := []proto.Item{
				{ID: "earlier#0", Seq: 1, TS: now, Role: "user", Kind: proto.KindText, Text: tc.earlier},
				{ID: "literal#0", Seq: 2, TS: now, Role: "user", Kind: proto.KindText, Text: tc.literal},
			}
			found := log.match("s", echoInRuntime, items)
			if len(found) != 1 || found[0].Line != "literal" {
				t.Fatalf("matched %+v, want only the later exact literal", found)
			}
			sameTags(t, log.tag("s", echoInRuntime, items), map[string]string{"literal#0": "c"})
		})
	}
}

func echoLiteralRuntime(t *testing.T) (*Node, *runtimeHub, *fakeRuntime) {
	t.Helper()
	n, hub := runtimeNode(t)
	host := dialRuntime(t, n, map[string]any{
		"runtime": "pi-literal", "agent": "pi", "name": "pi",
		"caps": map[string]any{"send": true},
	})
	_ = host.peer.Notify("runtime.sessions", map[string]any{"sessions": []map[string]any{{
		"session_id": piSID, "status": "idle", "cwd": "/w",
	}}})
	waitFor(t, "the pi runtime's session", func() bool { return n.runtimes.ownerOf(piSID) != nil })
	return n, hub, host
}

func echoLiteralRuntimeItems() []proto.Item {
	now := time.Now()
	return []proto.Item{
		{ID: "old-reminder#0", Seq: 1, TS: now, Role: "user", Kind: proto.KindText, Text: "<system-reminder>not the send</system-reminder>"},
		{ID: "old-context#0", Seq: 2, TS: now, Role: "user", Kind: proto.KindText, Text: "<persona-context>not the send</persona-context>"},
		{ID: "old-empty#0", Seq: 3, TS: now, Role: "user", Kind: proto.KindText, Text: ""},
		{ID: "literal#0", Seq: 4, TS: now, Role: "user", Kind: proto.KindText, Text: echoLiteralText},
	}
}

func sendEchoLiteral(t *testing.T, n *Node) {
	t.Helper()
	if _, err := nodeCall(t, n, "session.send", map[string]any{
		"session_id": piSID, "text": echoLiteralText, "client_msg_id": "literal-client",
	}); err != nil {
		t.Fatal(err)
	}
}

// A pi-shaped runtime push goes through echoedRaw before the hub sees it: unrelated raw items stay unsigned, while
// the exact literal line carries the pending send's id.
func TestEchoLiteralRuntimePush(t *testing.T) {
	n, hub, host := echoLiteralRuntime(t)
	sendEchoLiteral(t, n)
	_ = host.peer.Notify("session.items", map[string]any{
		"session_id": piSID, "to_seq": 4, "items": echoLiteralRuntimeItems(),
	})
	waitFor(t, "the runtime item push", func() bool {
		hub.mu.Lock()
		defer hub.mu.Unlock()
		return len(hub.items) == 1
	})
	hub.mu.Lock()
	raw := hub.items[0]
	hub.mu.Unlock()
	var push struct {
		Items []proto.Item `json:"items"`
	}
	if err := json.Unmarshal(raw, &push); err != nil {
		t.Fatal(err)
	}
	sameTags(t, push.Items, map[string]string{"literal#0": "literal-client"})
}

// The same runtime's session.items response goes through runtimeHandle and echoedRaw, so a page discovers and tags
// the literal line even when there was no push.
func TestEchoLiteralRuntimePage(t *testing.T) {
	n, _, host := echoLiteralRuntime(t)
	sendEchoLiteral(t, n)
	host.mu.Lock()
	host.answer = func(method string, params json.RawMessage) (any, error) {
		if method == "session.items" {
			return map[string]any{"to_seq": 4, "items": echoLiteralRuntimeItems()}, nil
		}
		return map[string]any{"ok": true}, nil
	}
	host.mu.Unlock()
	raw, err := nodeCall(t, n, "session.items", map[string]any{"session_id": piSID, "limit": 50})
	if err != nil {
		t.Fatal(err)
	}
	var page struct {
		Items []proto.Item `json:"items"`
	}
	if err := json.Unmarshal(raw, &page); err != nil {
		t.Fatal(err)
	}
	sameTags(t, page.Items, map[string]string{"literal#0": "literal-client"})
}

// session.echo carries the pending literal to the hub and session.echoes restores it after a restart; the same raw
// user line is then matched and tagged by the new node.
func TestEchoLiteralRoundTripAfterRestart(t *testing.T) {
	n := &Node{}
	hub := withEchoHub(t, n)
	if err := n.sendEchoed("s", "c", echoLiteralText, 0, echoAfter{in: echoInRuntime}, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the pending echo", func() bool { return len(hub.got()) == 1 })
	wire, err := json.Marshal(hub.got())
	if err != nil {
		t.Fatal(err)
	}
	n2, _ := testNode(t)
	if _, err := request(t, n2, "session.echoes", map[string]any{"echoes": json.RawMessage(wire)}); err != nil {
		t.Fatal(err)
	}
	items := []proto.Item{{ID: "after-restart#0", Seq: 1, TS: time.Now(), Role: "user", Kind: proto.KindText, Text: echoLiteralText}}
	found := n2.echoes.match("s", echoInRuntime, items)
	if len(found) != 1 || found[0].Line != "after-restart" {
		t.Fatalf("restored echo did not match: %+v", found)
	}
	sameTags(t, n2.echoes.tag("s", echoInRuntime, items), map[string]string{"after-restart#0": "c"})
}

// The optional literal is bounded like Text and is not added to ordinary, mixed hook, image, or whitespace-only
// records. Its raw prefix still matches when the original literal is longer than the bound.
func TestEchoLiteralRecordBounds(t *testing.T) {
	long := "<system-reminder>" + strings.Repeat("界", echoTextLen) + "</system-reminder>"
	var log echoLog
	log.expect(echoRecord{SessionID: "s", ClientMsgID: "long", Text: long, SentAt: time.Now(), In: echoInRuntime})
	r, ok := log.record("s", "long")
	if !ok {
		t.Fatal("missing the pending record")
	}
	wire, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]any
	if err := json.Unmarshal(wire, &fields); err != nil {
		t.Fatal(err)
	}
	literal, ok := fields["literal"].(string)
	if !ok || len([]rune(literal)) != echoTextLen || literal != string([]rune(long)[:echoTextLen]) {
		t.Fatalf("bounded literal %q (%d runes) in %s", literal, len([]rune(literal)), wire)
	}
	items := []proto.Item{{ID: "long#0", Seq: 1, TS: time.Now(), Role: "user", Kind: proto.KindText, Text: long}}
	if found := log.match("s", echoInRuntime, items); len(found) != 1 {
		t.Fatalf("the bounded literal's raw prefix did not match: %+v", found)
	}

	for name, record := range map[string]echoRecord{
		"ordinary":   {Text: "plain words"},
		"mixed hook": {Text: "<system-reminder>hook context</system-reminder>\nplain words"},
		"image":      {Text: echoLiteralText, Images: 1},
		"whitespace": {Text: " \n\t"},
	} {
		t.Run(name, func(t *testing.T) {
			var log echoLog
			record.SessionID, record.ClientMsgID, record.SentAt = "s", "c", time.Now()
			log.expect(record)
			r, _ := log.record("s", "c")
			wire, err := json.Marshal(r)
			if err != nil {
				t.Fatal(err)
			}
			var fields map[string]any
			_ = json.Unmarshal(wire, &fields)
			if _, exists := fields["literal"]; exists {
				t.Fatalf("unexpected literal in %s", wire)
			}
		})
	}
}
