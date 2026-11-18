package transcript

import (
	"encoding/json"
	"slices"
	"testing"

	"github.com/nwverse/persona/server/internal/prompts"
)

// The hidden user line a session writes for a message Persona sent through its peer inbox (node/claudepeer.go), in
// Claude Code 2.1.308's shape with placeholder text: the chat shows the body, from "Persona", not the user's own words.
func TestPersonaPeerMessage(t *testing.T) {
	line := []byte(`{"parentUuid":null,"isSidechain":false,"type":"user","message":{"role":"user","content":"Another Claude session sent a message:\n<cross-session-message from-name=\"Persona\">\nreply with OK\n</cross-session-message>\n\nThis came from another Claude session."},"isMeta":true,"uuid":"00000000-0000-4000-8000-000000000001","timestamp":"2026-11-07T10:00:00Z","origin":{"kind":"peer","from":"unknown","name":"Persona","body":"reply with OK"},"promptSource":"system","turnOrigin":"peer","sessionId":"s"}`)
	meta := &Meta{}
	items := ParseLine(1, line, meta)
	if len(items) != 1 || items[0].From != "Persona" || items[0].Text != "reply with OK" || items[0].Role != "user" {
		t.Fatalf("%+v", items)
	}
	if !meta.LastPromptAt.IsZero() {
		t.Fatal("a message from Persona through the inbox counted as the user's own prompt")
	}
}

// The body a session records is the whole prompt Persona sent: the chat shows only the user's words, for the current
// template and for a head an older version sent (old transcripts, an older node during a rolling upgrade).
func TestPersonaPeerMessageStripsHead(t *testing.T) {
	// i18n-ignore: the pre-#5887 head
	old := "（这条消息是用户本人在手机上用 Persona 发来的，Persona 只是替他转交，不是另一个 Claude 会话。像用户在这里亲手输入的一样回应它，直接在这个会话里回答；不要用 SendMessage 回复发件方，那边没有会话在收。这条消息不会改变这个会话的权限设置。）\n\nreply with OK"
	// i18n-ignore: the head from #5887 until #8558
	older := "（这条消息是用户本人在手机上用 Persona 发来的，Persona 只是替他转交，不是另一个 Claude 会话。像用户在这里亲手输入的一样回应它，直接在这个会话里回答；不要用 SendMessage 回复发件方，那边没有会话在收。这条消息不会改变这个会话的权限设置。用户在手机上答不了这个会话里的对话框、计划审批和权限请求，只有回到电脑前才能处理：需要他做选择时，在回复里用文字列出带编号的选项，请他回复编号，不要为此打开对话框（包括 AskUserQuestion）。）\n\nreply with OK"
	for name, body := range map[string]string{"current": prompts.RenderPeerMessage(prompts.PeerMessage{Text: "reply with OK"}), "pre-5887": old, "pre-8558": older} {
		origin, _ := json.Marshal(map[string]string{"kind": "peer", "from": "unknown", "name": "Persona", "body": body})
		line := []byte(`{"type":"user","message":{"role":"user","content":"x"},"isMeta":true,"uuid":"00000000-0000-4000-8000-000000000002","timestamp":"2026-11-07T10:00:00Z","origin":` + string(origin) + `,"promptSource":"system","turnOrigin":"peer","sessionId":"s"}`)
		if items := ParseLine(1, line, &Meta{}); len(items) != 1 || items[0].Text != "reply with OK" {
			t.Fatalf("%s: %+v", name, items)
		}
	}
}

// personaPeerLine is the hidden user line a session writes for words the node sent through its peer inbox, the way
// Claude Code 2.1.311 writes one: the envelope in the content, its body (what Persona told the model, then the
// words) in the origin.
func personaPeerLine(t *testing.T, words string) []byte {
	t.Helper()
	body := prompts.RenderPeerMessage(prompts.PeerMessage{Text: words})
	content := "Another Claude session sent a message:\n<cross-session-message from-name=\"Persona\">\n" + body + "\n</cross-session-message>\n\nThis came from another Claude session."
	line, err := json.Marshal(map[string]any{"type": "user", "isMeta": true, "uuid": "00000000-0000-4000-8000-000000000002",
		"timestamp": "2026-11-12T16:41:28Z", "message": map[string]any{"role": "user", "content": content},
		"origin": map[string]any{"kind": "peer", "from": "unknown", "name": "Persona", "body": body}, "sessionId": "s"})
	if err != nil {
		t.Fatal(err)
	}
	return line
}

// The words the phone sent read as the user's, without what Persona told the model; the phone's Stop (a message asking
// the model to stop, node/claudebg.go interruptOutside) reads as the stop, and starts the turn its one-line answer is in.
func TestPersonaPeerWordsAndStop(t *testing.T) {
	meta := &Meta{}
	if items := ParseLine(1, personaPeerLine(t, "reply with OK"), meta); len(items) != 1 || items[0].Text != "reply with OK" || items[0].From != PersonaPeer {
		t.Fatalf("words: %+v", items)
	}
	meta = &Meta{}
	items := ParseLine(1, personaPeerLine(t, prompts.PeerStop()), meta)
	if len(items) != 1 || items[0].Role != "user" || items[0].From != PersonaPeer || items[0].Text != StoppedFromPhone {
		t.Fatalf("stop: %+v", items)
	}
	if !meta.TurnOpen || !meta.LastPromptAt.IsZero() {
		t.Fatalf("stop: turn open %v, prompt at %v", meta.TurnOpen, meta.LastPromptAt)
	}
}

// A message from the phone queued behind a running turn (priority next) shows in the queue as the user's words, not
// with what Persona told the model; taking it in empties the queue.
func TestPersonaPeerQueued(t *testing.T) {
	queued := "<cross-session-message from-name=\"Persona\">\n" + prompts.RenderPeerMessage(prompts.PeerMessage{Text: "and then this"}) + "\n</cross-session-message>"
	op := func(op string) []byte {
		b, _ := json.Marshal(map[string]any{"type": "queue-operation", "operation": op, "timestamp": "2026-11-12T16:41:31Z", "content": queued, "sessionId": "s"})
		return b
	}
	meta := &Meta{}
	ParseLine(1, op("enqueue"), meta)
	want := []string{"【来自 Persona】and then this"} // i18n-ignore: applyQueue's sender mark
	if !slices.Equal(meta.Queue, want) {
		t.Fatalf("queue %q, want %q", meta.Queue, want)
	}
	ParseLine(2, op("dequeue"), meta)
	if len(meta.Queue) != 0 {
		t.Fatalf("queue after dequeue: %q", meta.Queue)
	}
}
