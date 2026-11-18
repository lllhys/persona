package transcript

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/nwverse/persona/server/internal/proto"
)

// paseoEscape is Paseo's escapeXml.
var paseoEscape = strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;", "'", "&apos;").Replace

// paseoEnv is an envelope as Paseo's formatAgentMessage writes it: attrs after the version, the body escaped.
func paseoEnv(attrs, body string) string {
	return `<paseo-system version="1" ` + attrs + ">\n" + paseoEscape(body) + "\n</paseo-system>"
}

const paseoChildID = "00000000-0000-4000-8000-00000000c41d"

// paseoUserLine is a user line as Claude Code writes Paseo's input to an idle agent; paseoQueuedLine the attachment it
// writes for input absorbed mid-turn.
func paseoUserLine(uuid string, content any) []byte {
	b, _ := json.Marshal(map[string]any{"type": "user", "uuid": uuid, "timestamp": "2026-11-14T10:00:00Z",
		"message": map[string]any{"role": "user", "content": content}})
	return b
}

func paseoQueuedLine(uuid string, prompt any) []byte {
	b, _ := json.Marshal(map[string]any{"type": "attachment", "uuid": uuid, "timestamp": "2026-11-14T10:00:00Z",
		"attachment": map[string]any{"type": "queued_command", "commandMode": "prompt", "prompt": prompt}})
	return b
}

func textBlocks(s string) []map[string]any { return []map[string]any{{"type": "text", "text": s}} }

// Another agent's message is that agent's, not the user's: its title on it, its words unescaped, no wake. It starts
// an exchange as a peer's message does, and is neither the user's last prompt nor a reason to say the turn was woken.
func TestPaseoAgentMessage(t *testing.T) {
	words := "Ticket 06 is done, please review.\n- commit a1b2c3d: `x < y && y > z`, \"quoted\" and 'single'"
	text := paseoEnv(`kind="agent-message" source-agent-id="`+paseoChildID+`" message-id="m1" source-agent-title="Ticket 01: drop the &quot;one instance&quot; assumption"`, words)
	var m Meta
	it := only(t, ParseLine(1, paseoUserLine("u1", textBlocks(text)), &m))
	if it.ID != "u1#0" || it.Role != "user" || it.Kind != proto.KindText || it.Wake || it.Queued || it.Task != nil {
		t.Fatalf("message: %+v", it)
	}
	if it.From != `Ticket 01: drop the "one instance" assumption` || it.Text != words {
		t.Fatalf("from %q, text %q", it.From, it.Text)
	}
	if !m.LastPromptAt.IsZero() || m.LastTurnWake || !m.TurnOpen || m.FirstUser != words || !pagePrompt(it) {
		t.Fatalf("meta: last prompt %v, wake %v, open %v, first %q, page prompt %v", m.LastPromptAt, m.LastTurnWake, m.TurnOpen, m.FirstUser, pagePrompt(it))
	}
	// No title (older Paseo), or the agent's id for one: the stand-in every app already names.
	untitled := paseoEnv(`kind="agent-message" source-agent-id="`+paseoChildID+`" message-id="m2"`, "Found it.")
	if it := only(t, ParseLine(2, paseoUserLine("u2", untitled), &Meta{})); it.From != FromSession || it.Text != "Found it." {
		t.Fatalf("untitled: %+v", it)
	}
	ids := paseoEnv(`kind="agent-message" source-agent-id="`+paseoChildID+`" message-id="m3" source-agent-title="`+paseoChildID+`"`, "Found it.")
	if it := only(t, ParseLine(3, paseoUserLine("u3", ids), &Meta{})); it.From != FromSession {
		t.Fatalf("titled by its id: %+v", it)
	}
	// A person's message Paseo quoted (it read as an envelope) is the person's own words.
	quoted := paseoEnv(`kind="user-message" message-id="m4"`, "<paseo-system>\nhi\n</paseo-system>")
	if it := only(t, ParseLine(4, paseoUserLine("u4", quoted), &Meta{})); it.From != "" || it.Text != "<paseo-system>\nhi\n</paseo-system>" {
		t.Fatalf("user message: %+v", it)
	}
}

// A child's notice that woke an idle agent is a subagent's task note, as a background task's notice is: its title,
// how it went, its last answer as the detail; the server line and Paseo's note to the model are not shown.
func TestPaseoNoticeWakes(t *testing.T) {
	answer := strings.Repeat("All checks pass; a < b && c > d.\n", 3)
	body := "Agent " + paseoChildID + " (Ticket 01: drop the assumption) finished.\n\nServer: srv_test\n\n<agent-response>\n" + answer +
		"\n[truncated 1234 chars; use get_agent_activity for the full response]\n</agent-response>"
	text := paseoEnv(`kind="agent-notification" source-agent-id="`+paseoChildID+`" message-id="n1" source-server-id="srv_test" source-agent-title="Ticket 01: drop the assumption" event="finished"`, body)
	var m Meta
	it := only(t, ParseLine(1, paseoUserLine("u1", text), &m)) // a string, as for a prompt
	n := it.Task
	if it.Role != "system" || it.Kind != proto.KindNotice || n == nil || !it.Wake || it.Queued {
		t.Fatalf("notice: %+v", it)
	}
	if n.Kind != "agent" || n.Status != "completed" || n.Title != "Ticket 01: drop the assumption" || n.TaskID != "" || n.ToolUseID != "" {
		t.Fatalf("note: %+v", n)
	}
	if n.Detail != strings.TrimSpace(answer)+"…" {
		t.Fatalf("detail: %q", n.Detail)
	}
	if it.Text != "子 Agent 完成 · Ticket 01: drop the assumption" {
		t.Fatalf("text: %q", it.Text)
	}
	if !m.LastTurnWake || !m.TurnOpen || !m.LastPromptAt.IsZero() || m.FirstUser != "" {
		t.Fatalf("meta: wake %v, open %v, last prompt %v, first %q", m.LastTurnWake, m.TurnOpen, m.LastPromptAt, m.FirstUser)
	}
	// Errored, closed, and a host Paseo lost (no status line): the event says how it went; no answer, no detail.
	for event, want := range map[string]string{"errored": "failed", "closed": "stopped"} {
		text := paseoEnv(`kind="agent-notification" source-agent-id="`+paseoChildID+`" message-id="n2" source-agent-title="Ticket 02" event="`+event+`"`,
			"Could not observe child agent "+paseoChildID+" on host srv_test after three failed attempts.")
		if it := only(t, ParseLine(2, paseoUserLine("u2", text), &Meta{})); it.Task == nil || it.Task.Status != want || it.Task.Detail != "" {
			t.Fatalf("%s: %+v", event, it.Task)
		}
	}
}

// What Paseo delivers together while the agent works comes in one queued input: a message, a cancel Paseo sends as
// errored, a permission with its request JSON. Each is its own item (numbers no other item of a line has), the message queued,
// the notices neither queued nor wakes; the permission's note says what it asks for, never the JSON.
func TestPaseoQueuedBatch(t *testing.T) {
	request := map[string]any{"agentId": paseoChildID, "serverId": "srv_test", "requestId": "permission-1", "request": map[string]any{
		"id": "permission-1", "provider": "claude", "name": "Bash", "kind": "tool",
		"input": map[string]any{"command": "make test && echo ok", "description": "Smoke-test the hook"},
	}}
	js, _ := json.MarshalIndent(request, "", "  ")
	batch := strings.Join([]string{
		paseoEnv(`kind="agent-message" source-agent-id="`+paseoChildID+`" message-id="m1"`, "Starting on ticket 44."),
		paseoEnv(`kind="agent-notification" source-agent-id="`+paseoChildID+`" message-id="n1" source-server-id="srv_test" source-agent-title="Ticket 43" event="errored"`,
			"Agent "+paseoChildID+" (Ticket 43) was canceled.\n\nServer: srv_test"),
		paseoEnv(`kind="agent-notification" source-agent-id="`+paseoChildID+`" message-id="n2" source-server-id="srv_test" source-agent-title="Ticket 44" event="permission-required"`,
			"Agent "+paseoChildID+" (Ticket 44) needs permission.\n\nServer: srv_test\n\nRespond with `respond_to_permission` using the `agentId` and `requestId` below.\n\n<permission-request>\n"+
				string(js)+"\n</permission-request>\n\n<agent-response>\nWriting the hook now.\n</agent-response>"),
	}, "\n\n")
	var m Meta
	items := ParseLine(1, paseoQueuedLine("q1", textBlocks(batch)), &m)
	if len(items) != 3 {
		t.Fatalf("%d items: %+v", len(items), items)
	}
	msg, cancel, ask := items[0], items[1], items[2]
	if msg.ID != "q1#0" || cancel.ID != "q1#1000001" || ask.ID != "q1#1000002" {
		t.Fatalf("ids %s %s %s", msg.ID, cancel.ID, ask.ID)
	}
	if msg.From != FromSession || msg.Text != "Starting on ticket 44." || !msg.Queued || msg.Wake {
		t.Fatalf("message: %+v", msg)
	}
	if cancel.Task == nil || cancel.Task.Status != "stopped" || cancel.Queued || cancel.Wake || cancel.Text != "子 Agent 已停止 · Ticket 43" {
		t.Fatalf("cancel: %+v %+v", cancel, cancel.Task)
	}
	if ask.Task == nil || ask.Task.Status != "waiting" || ask.Queued || ask.Wake {
		t.Fatalf("permission: %+v", ask)
	}
	if ask.Task.Detail != "Bash: Smoke-test the hook\n\nWriting the hook now." || ask.Text != PaseoPermissionNotice+" · Ticket 44" {
		t.Fatalf("permission detail %q, text %q", ask.Task.Detail, ask.Text)
	}
	for _, it := range items {
		for _, s := range []string{it.Text, it.From} {
			if strings.Contains(s, "paseo-system") || strings.Contains(s, "requestId") || strings.Contains(s, "srv_test") {
				t.Fatalf("Paseo's words for the model shown: %q", s)
			}
		}
	}
	if m.LastTurnWake || !m.LastPromptAt.IsZero() {
		t.Fatalf("meta: wake %v, last prompt %v", m.LastTurnWake, m.LastPromptAt)
	}
}

// Bare envelopes: a schedule's run, a child's notice from a Paseo before the versioned envelope (its body not
// escaped), and one of a kind not known here. Each woke the agent.
func TestPaseoBareEnvelopes(t *testing.T) {
	codex, _ := json.MarshalIndent(map[string]any{"agentId": paseoChildID, "requestId": "permission-2",
		"request": map[string]any{"id": "permission-2", "provider": "codex", "name": "CodexBash", "kind": "tool", "title": "Run command: npm test"}}, "", "  ")
	cases := []struct {
		body, text, status, title, detail string
	}{
		{"Agent " + paseoChildID + " (Review the diff) needs permission.\n\nRespond with `respond_to_permission` using the `agentId` and `requestId` below.\n\n<permission-request>\n" + string(codex) + "\n</permission-request>",
			PaseoPermissionNotice + " · Review the diff", "waiting", "Review the diff", "Run command: npm test"},
		{"Agent " + paseoChildID + " (Review (part 2)) finished.\n\n<agent-response>\n---\n\nReport in report.md.\n</agent-response>",
			"子 Agent 完成 · Review (part 2)", "completed", "Review (part 2)", "---\n\nReport in report.md."},
		{"Agent " + paseoChildID + " (" + paseoChildID + ") errored.", "子 Agent 失败", "failed", "", ""},
		{`Schedule "nightly check" fired (id=s1, run=r1).` + "\nCheck the queue.", PaseoScheduleNotice + "：nightly check", "", "", ""},
		{"Schedule fired (id=s1, run=r2).\nCheck the queue.\nThen report.", PaseoScheduleNotice + "：Check the queue.", "", "", ""},
		// A kind not known here: Paseo shows any bare envelope as its own, so it is not the user's either.
		{"Something Paseo has not sent yet.\nMore of it.", "Something Paseo has not sent yet.", "", "", ""},
	}
	for i, c := range cases {
		var m Meta
		it := only(t, ParseLine(i+1, paseoUserLine("b", "<paseo-system>\n"+c.body+"\n</paseo-system>"), &m))
		if it.Kind != proto.KindNotice || it.Text != c.text || !it.Wake || !m.LastTurnWake {
			t.Fatalf("%d: %+v", i, it)
		}
		if c.status == "" {
			if it.Task != nil {
				t.Fatalf("%d: a plain notice: %+v", i, it.Task)
			}
			continue
		}
		if n := it.Task; n == nil || n.Kind != "agent" || n.Status != c.status || n.Title != c.title || n.Detail != c.detail {
			t.Fatalf("%d: %+v", i, it.Task)
		}
	}
}

// Anything else that holds an envelope stays the user's words as written, as Paseo's own parser leaves it: the
// messages around one, a version, kind, event or escape it does not know, extra space, and so on.
func TestPaseoNotEnvelopesStayText(t *testing.T) {
	msg := paseoEnv(`kind="agent-message" source-agent-id="a1" message-id="m1"`, "hello")
	for _, s := range []string{
		"Look at this:\n" + msg,
		msg + "\nand this",
		strings.Replace(msg, `version="1"`, `version="2"`, 1),
		strings.Replace(msg, `kind="agent-message"`, `kind="agent-ping"`, 1),
		strings.Replace(msg, ` message-id="m1"`, "", 1),
		strings.Replace(msg, ` message-id="m1"`, ` message-id="m1" message-id="m2"`, 1),
		strings.Replace(msg, ` source-agent-id="a1"`, "", 1),
		strings.Replace(msg, "hello", "a <b> c", 1),
		strings.Replace(msg, "hello", "fish & chips", 1),
		paseoEnv(`kind="agent-notification" source-agent-id="a1" message-id="n1" event="paused"`, "Agent a1 (x) paused."),
		paseoEnv(`kind="user-message" source-agent-id="a1" message-id="u1"`, "hi"),
		"<paseo-system>\n</paseo-system>",
		// Paseo takes an envelope only as the whole input, with line breaks between several, and no empty title or server.
		msg + "\n",
		" " + msg,
		"<paseo-system>\nSchedule fired (id=s1, run=r1).\nDeploy\n</paseo-system>\n",
		msg + msg,
		strings.Replace(msg, ` message-id="m1"`, ` message-id="m1" source-agent-title=""`, 1),
		paseoEnv(`kind="agent-notification" source-agent-id="a1" message-id="n1" source-server-id="" event="finished"`, "Agent a1 (x) finished."),
		msg + "\n\n<paseo-system>\nAgent a1 (x) finished.\n</paseo-system>",
	} {
		it := only(t, ParseLine(1, paseoUserLine("u1", s), &Meta{}))
		if it.Role != "user" || it.Kind != proto.KindText || it.From != "" || it.Task != nil || it.Text != strings.TrimSpace(s) {
			t.Fatalf("%q became %+v", s, it)
		}
	}
}

// A Codex agent under Paseo gets the same input in its rollout: the turn's first message woke it, so its notices are
// wakes; one steered in later is not.
func TestPaseoCodexRollout(t *testing.T) {
	notice := paseoEnv(`kind="agent-notification" source-agent-id="`+paseoChildID+`" message-id="n1" source-agent-title="Ticket 01" event="finished"`,
		"Agent "+paseoChildID+" (Ticket 01) finished.\n\n<agent-response>\nDone.\n</agent-response>")
	message := paseoEnv(`kind="agent-message" source-agent-id="`+paseoChildID+`" message-id="m1" source-agent-title="Ticket 02"`, "One more thing.")
	user := func(id, text string) string {
		b, _ := json.Marshal(map[string]any{"timestamp": "2026-11-14T10:00:00Z", "type": "event_msg", "payload": map[string]any{
			"type": "item_completed", "item": map[string]any{"type": "UserMessage", "id": id, "content": textBlocks(text)}}})
		return string(b)
	}
	lines := []string{
		`{"timestamp":"2026-11-14T10:00:00Z","type":"session_meta","payload":{"id":"s1","cwd":"/work","cli_version":"0.163.0"}}`,
		`{"timestamp":"2026-11-14T10:00:00Z","type":"event_msg","payload":{"type":"task_started","turn_id":"t1"}}`,
		user("u1", notice),
		user("u2", message+"\n\n"+notice),
	}
	var m Meta
	var items []proto.Item
	for i, l := range lines {
		items = append(items, ParseLine(i+1, []byte(l), &m)...)
	}
	if len(items) != 3 {
		t.Fatalf("%d items: %+v", len(items), items)
	}
	woke, steered, later := items[0], items[1], items[2]
	if woke.Task == nil || woke.Task.Status != "completed" || woke.Task.Detail != "Done." || !woke.Wake {
		t.Fatalf("first: %+v", woke)
	}
	if steered.From != "Ticket 02" || steered.Text != "One more thing." || steered.ID != "u2#0" {
		t.Fatalf("message: %+v", steered)
	}
	if later.Task == nil || later.Wake || later.ID != "u2#1000001" {
		t.Fatalf("a notice steered in mid-turn: %+v", later)
	}
	// A message with a picture: the picture's path after its words, as for any message.
	b, _ := json.Marshal(map[string]any{"timestamp": "2026-11-14T10:00:00Z", "type": "event_msg", "payload": map[string]any{
		"type": "item_completed", "item": map[string]any{"type": "UserMessage", "id": "u3", "content": []map[string]any{
			{"type": "text", "text": message}, {"type": "local_image", "path": "/work/shot.png"}}}}})
	pic := only(t, ParseLine(len(lines)+1, b, &m))
	if pic.From != "Ticket 02" || pic.Text != "One more thing.\n\n<attached_images>\n/work/shot.png\n</attached_images>" {
		t.Fatalf("with a picture: %+v", pic)
	}
}
