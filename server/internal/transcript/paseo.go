package transcript

import (
	"encoding/json"
	"strings"

	"github.com/nwverse/persona/server/internal/lazyre"
	"github.com/nwverse/persona/server/internal/proto"
)

// Paseo (docs/integrations/paseo.md) gives an agent what other agents send it as user input, each message in an
// envelope (Paseo's agent-messages/envelope.ts), a blank line between those it delivers together:
//
//	<paseo-system version="1" kind="agent-notification" source-agent-id="…" message-id="…" source-server-id="…" source-agent-title="…" event="finished">
//	Agent … (…) finished.
//	…
//	</paseo-system>
//
// kind agent-message is another agent's message (source-agent-title when Paseo knows its title); agent-notification a
// child agent that finished, errored, waits for a permission or was closed (event), with its last answer; user-message
// a person's message that would read as an envelope. Attributes and body are XML-escaped. A schedule's run comes in a
// bare <paseo-system> with its body as it is ("Schedule "<name>" fired (id=…, run=…)." and the prompt), and so did a
// child's notice before the versioned envelope ("Agent <id> (<title>) finished.").
//
// Read as typed by the user, all of it was the user's bubble, the model's instructions and a permission's JSON
// included, and everything that tells the user's words from others took it for theirs. A message is a peer's
// (Item.From), a notice a subagent's task note, a schedule's run a notice, and so is a bare envelope of another kind
// (Paseo shows any bare envelope as its own; a person's is quoted as a user-message). Text around envelopes, and an
// envelope Paseo's own parseAgentMessages would not take, stay the user's words, as Paseo leaves them.
const (
	paseoOpen  = "<paseo-system"
	paseoBare  = "<paseo-system>\n"
	paseoClose = "\n</paseo-system>"
	// paseoBatch numbers the items of a batch's later envelopes past any other item's of a line: a block's index,
	// a tool result's pictures (paseoInput).
	paseoBatch = 1_000_000
)

// PaseoPermissionNotice heads the notice of a child agent waiting for a permission, its title after " · ";
// PaseoScheduleNotice the notice of a schedule's run, its name (else its prompt's first line) after "：". The hub gives
// both in the reader's language.
const (
	PaseoPermissionNotice = "子 Agent 等待批准"
	PaseoScheduleNotice   = "定时任务到点"
)

var (
	// Paseo's own expressions (parseAgentMessage): the opening tag with its attributes, and each attribute.
	paseoHeadRe = lazyre.New(`^<paseo-system((?:\s+[a-z-]+="[^"<>]*")+)>\n`)
	paseoAttrRe = lazyre.New(`\s+([a-z-]+)="([^"<>]*)"`)
	// A notice's first line (formatFinishNotificationBody): the child's id, its title (or its id again) and how it went.
	paseoStatusRe = lazyre.New(`^Agent (\S+) \((.*)\) (finished|errored|needs permission|was closed|is unavailable|was canceled)\.$`)
	// A schedule's run (buildScheduleFireBody), the prompt on the lines after it.
	paseoScheduleRe = lazyre.New(`^Schedule (?:"(.*)" )?fired \(id=[^,)]*, run=[^)]*\)\.$`)
	// What Paseo tells the model when it cut a child's answer.
	paseoCutRe = lazyre.New(`\n\[truncated \d+ chars; use get_agent_activity for the full response\]$`)
)

// paseoEvents is the event a notice's status line says, for the bare notices that have only that line (Paseo's
// finishNotificationEvents).
var paseoEvents = map[string]string{"finished": "finished", "errored": "errored", "needs permission": "permission-required",
	"was closed": "closed", "is unavailable": "closed", "was canceled": "errored"}

// paseoEnvelope is one envelope: its kind (a bare one's is agent-notification, paseoScheduled or paseoOther), the
// agent it came from, that agent's title, the notice's event and the body, unescaped.
type paseoEnvelope struct {
	kind, agentID, title, event, body string
}

// paseoScheduled is the kind of a schedule's run, paseoOther of a bare envelope of a kind not known here.
const (
	paseoScheduled = "schedule"
	paseoOther     = "other"
)

// paseoInput is s read as Paseo's envelopes when it is nothing else, an item each; ok false for any other text. Item
// numbers come from part (a block's index, 0 for a whole line): the first envelope's is part, the others' past any
// other item's of the line (paseoBatch). The caller marks the notices of input that started a turn as wakes, and the
// messages of input absorbed mid-turn as queued.
func paseoInput(mk func(int, string, string) proto.Item, part int, s string) ([]proto.Item, bool) {
	envelopes := paseoEnvelopes(s)
	if envelopes == nil {
		return nil, false
	}
	var out []proto.Item
	for j, e := range envelopes {
		n := part
		if j > 0 {
			n = (part+1)*paseoBatch + j
		}
		switch e.kind {
		case "agent-message", "user-message":
			body := strings.TrimSpace(e.body)
			if body == "" {
				continue
			}
			it := mk(n, "user", proto.KindText)
			it.Text = clip(body)
			if e.kind == "agent-message" {
				it.From = FromSession
				if t := paseoTitle(e); t != "" {
					it.From = t
				}
			}
			out = append(out, it)
		case "agent-notification":
			it := mk(n, "system", proto.KindNotice)
			it.Task, it.Text = paseoNote(e)
			out = append(out, it)
		case paseoScheduled:
			it := mk(n, "system", proto.KindNotice)
			it.Text = paseoScheduleText(e.body)
			out = append(out, it)
		case paseoOther:
			if line := snippetLine(e.body, 200); line != "" {
				it := mk(n, "system", proto.KindNotice)
				it.Text = line
				out = append(out, it)
			}
		}
	}
	return out, true
}

// paseoEnvelopes is s split into the envelopes it is made of, nil when it holds anything else: nothing around them,
// line breaks between them, as Paseo's parseAgentMessages and isSystemInjectedEnvelope take them. A versioned body is
// escaped, so the first closing tag ends it; a bare one is not, so it is the whole of s.
func paseoEnvelopes(s string) []paseoEnvelope {
	if !strings.HasPrefix(s, paseoOpen) {
		return nil
	}
	if strings.HasPrefix(s, paseoBare) {
		if len(s) < len(paseoBare)+len(paseoClose) || !strings.HasSuffix(s, paseoClose) {
			return nil
		}
		return []paseoEnvelope{paseoBareEnvelope(s[len(paseoBare) : len(s)-len(paseoClose)])}
	}
	var out []paseoEnvelope
	for s != "" {
		end := strings.Index(s, paseoClose)
		if end < 0 {
			return nil
		}
		e, ok := paseoVersioned(s[:end])
		if !ok {
			return nil
		}
		out = append(out, e)
		rest := s[end+len(paseoClose):]
		if rest == "" {
			return out
		}
		if s = strings.TrimLeft(rest, "\n"); s == rest || !strings.HasPrefix(s, paseoOpen+" ") {
			return nil
		}
	}
	return out
}

// paseoVersioned reads a versioned envelope without its closing tag, as Paseo's parseAgentMessage does: version 1, a
// message id, every value and the body as escapeXml writes them, the attributes its kind has.
func paseoVersioned(s string) (paseoEnvelope, bool) {
	head := paseoHeadRe.FindStringSubmatch(s)
	if head == nil {
		return paseoEnvelope{}, false
	}
	attrs := map[string]string{}
	for _, m := range paseoAttrRe.FindAllStringSubmatch(head[1], -1) {
		v, ok := paseoUnescape(m[2])
		if _, dup := attrs[m[1]]; !ok || dup {
			return paseoEnvelope{}, false
		}
		attrs[m[1]] = v
	}
	body, ok := paseoUnescape(s[len(head[0]):])
	if !ok || attrs["version"] != "1" || attrs["message-id"] == "" {
		return paseoEnvelope{}, false
	}
	e := paseoEnvelope{kind: attrs["kind"], agentID: attrs["source-agent-id"], title: attrs["source-agent-title"],
		event: attrs["event"], body: body}
	if e.kind == "user-message" {
		_, agent := attrs["source-agent-id"]
		_, event := attrs["event"]
		return e, !agent && !event
	}
	// An agent's: its id, and a title or server only when it has one (Paseo's schema takes no empty value).
	for _, k := range []string{"source-agent-title", "source-server-id"} {
		if v, ok := attrs[k]; ok && v == "" {
			return paseoEnvelope{}, false
		}
	}
	switch e.kind {
	case "agent-message":
		return e, e.agentID != ""
	case "agent-notification":
		known := map[string]bool{"finished": true, "errored": true, "permission-required": true, "closed": true}[e.event]
		return e, e.agentID != "" && known
	}
	return paseoEnvelope{}, false
}

// paseoBareEnvelope reads a bare envelope's body: a child's notice, a schedule's run, or Paseo's words of another kind.
func paseoBareEnvelope(body string) paseoEnvelope {
	first, _, _ := strings.Cut(body, "\n")
	if m := paseoStatusRe.FindStringSubmatch(first); m != nil {
		return paseoEnvelope{kind: "agent-notification", agentID: m[1], title: m[2], event: paseoEvents[m[3]], body: body}
	}
	if paseoScheduleRe.MatchString(first) {
		return paseoEnvelope{kind: paseoScheduled, body: body}
	}
	return paseoEnvelope{kind: paseoOther, body: body}
}

// paseoEntities undoes Paseo's escapeXml.
var paseoEntities = strings.NewReplacer("&amp;", "&", "&lt;", "<", "&gt;", ">", "&quot;", `"`, "&apos;", "'")

// paseoUnescape is s with Paseo's escapes undone, false for text escapeXml does not write (a bare < or >, an & that
// starts none of its entities), as its unescapeXml.
func paseoUnescape(s string) (string, bool) {
	if strings.ContainsAny(s, "<>") {
		return "", false
	}
	for rest := s; ; rest = rest[1:] {
		i := strings.IndexByte(rest, '&')
		if i < 0 {
			break
		}
		rest = rest[i:]
		if !strings.HasPrefix(rest, "&amp;") && !strings.HasPrefix(rest, "&lt;") && !strings.HasPrefix(rest, "&gt;") &&
			!strings.HasPrefix(rest, "&quot;") && !strings.HasPrefix(rest, "&apos;") {
			return "", false
		}
	}
	return paseoEntities.Replace(s), true
}

// paseoTitle is the title of the agent an envelope came from, in a line; "" when Paseo knew none (a notice then names
// the agent by its id).
func paseoTitle(e paseoEnvelope) string {
	t := oneLine(e.title, noteTitleLen)
	if t == strings.TrimSpace(e.agentID) {
		return ""
	}
	return t
}

// paseoNote reads a child's notice (formatFinishNotificationBody): its status line ("Agent <id> (<title>) finished."),
// the server the child runs on, for a permission the request and how to answer it, and the child's last answer. The
// note keeps the child's title, how it went (by the status line, which tells a cancel from an error, else the event)
// and, as its detail, what a permission asks for and that answer; the rest is for the model.
func paseoNote(e paseoEnvelope) (*proto.TaskNote, string) {
	first, _, _ := strings.Cut(e.body, "\n")
	reason := ""
	if m := paseoStatusRe.FindStringSubmatch(first); m != nil {
		reason = m[3]
	}
	n := &proto.TaskNote{Kind: "agent", Title: paseoTitle(e)}
	switch {
	case reason == "finished" || reason == "" && e.event == "finished":
		n.Status = "completed"
	case reason == "needs permission" || reason == "" && e.event == "permission-required":
		n.Status = "waiting"
	case reason == "was canceled" || reason == "was closed" || reason == "is unavailable" || reason == "" && e.event == "closed":
		n.Status = "stopped"
	default: // errored, and a host Paseo lost
		n.Status = "failed"
	}
	var detail []string
	if n.Status == "waiting" {
		if ask := paseoAsk(firstTag(e.body, "permission-request")); ask != "" {
			detail = append(detail, ask)
		}
	}
	if answer := paseoAnswer(e.body); answer != "" {
		detail = append(detail, answer)
	}
	n.Detail = clip(strings.Join(detail, "\n\n"))
	if n.Status != "waiting" {
		return n, noteText(n)
	}
	if n.Title == "" {
		return n, PaseoPermissionNotice
	}
	return n, PaseoPermissionNotice + " · " + n.Title
}

// paseoAnswer is a child's last answer, the notice's last section (<agent-response>), without Paseo's note to the
// model that it was cut.
func paseoAnswer(body string) string {
	const open, close = "<agent-response>\n", "\n</agent-response>"
	i := strings.Index(body, open)
	if i < 0 || !strings.HasSuffix(body, close) || i+len(open) > len(body)-len(close) {
		return ""
	}
	answer := body[i+len(open) : len(body)-len(close)]
	if cut := paseoCutRe.FindStringIndex(answer); cut != nil {
		answer = strings.TrimRight(answer[:cut[0]], " \t\n") + "…"
	}
	return strings.TrimSpace(answer)
}

// paseoAsk is what a child's permission request asks for, in a line: its title (Codex's "Run command: …"), else the
// tool and the call's description or command.
func paseoAsk(raw string) string {
	var r struct {
		Request struct {
			Name  string          `json:"name"`
			Title string          `json:"title"`
			Input json.RawMessage `json:"input"`
		} `json:"request"`
	}
	if raw == "" || json.Unmarshal([]byte(raw), &r) != nil {
		return ""
	}
	if t := oneLine(r.Request.Title, noteTitleLen); t != "" {
		return t
	}
	var in struct {
		Description string `json:"description"`
		Command     string `json:"command"`
	}
	_ = json.Unmarshal(r.Request.Input, &in)
	what := oneLine(in.Description, noteTitleLen)
	if what == "" {
		what = oneLine(in.Command, noteTitleLen)
	}
	switch name := strings.TrimSpace(r.Request.Name); {
	case name == "":
		return what
	case what == "":
		return name
	default:
		return name + ": " + what
	}
}

// paseoScheduleText is the notice of a schedule's run: its name, else its prompt's first line.
func paseoScheduleText(body string) string {
	first, prompt, _ := strings.Cut(body, "\n")
	what := ""
	if m := paseoScheduleRe.FindStringSubmatch(first); m != nil {
		what = snippetLine(m[1], 80)
	}
	if what == "" {
		what = snippetLine(prompt, 80)
	}
	if what == "" {
		return PaseoScheduleNotice
	}
	return PaseoScheduleNotice + "：" + what
}
