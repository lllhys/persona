package node

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"time"

	"github.com/nwverse/persona/server/internal/prompts"
)

// The assistant's live snapshot (the time, agents waiting, unread chats, today's meetings…) goes at the start of each
// message, in a <persona-context> block, not into the system prompt (docs/assistant/assistant.md "Live snapshots and prefix caching"). The prompt cache
// is a prefix: the system prompt comes before the whole conversation, so a system prompt that differs from one start
// to the next would make every request after a restart write the entire history again. Claude Code (2.1.314) keeps a
// resumed conversation on the system prompt it was first started with instead, so the snapshot there was the one
// from the conversation's first day.
//
// Only the sections that changed since the conversation last got them are sent (the time always): what it has seen
// is kept per conversation in memory and forgotten when it is compacted, which summarizes the old blocks away. A
// node restart forgets it too, and the next message carries the whole snapshot again.

// snapshotSection is one top-level line of the snapshot with the indented lines under it; key is what it is about
// (the "- Lark calendar" part of "- Lark calendar today next: ..." or "- Lark calendar: ...", in Chinese).
type snapshotSection struct{ key, text string }

func snapshotSections(text string) []snapshotSection {
	var out []snapshotSection
	for line := range strings.SplitSeq(text, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		if (strings.HasPrefix(line, " ") || strings.HasPrefix(line, "\t")) && len(out) > 0 {
			out[len(out)-1].text += "\n" + line
			continue
		}
		out = append(out, snapshotSection{key: sectionKey(line), text: line})
	}
	return out
}

// sectionKey is a line's subject: up to the first full-width colon, cut before the word "today", so "Lark calendar today next:" and
// "Lark calendar: no more meetings today" (both in Chinese) are the same section.
func sectionKey(line string) string {
	k, _, _ := strings.Cut(line, "：")
	k, _, _ = strings.Cut(k, "今天")
	return strings.TrimSpace(k)
}

const timeSection = "- 现在"

// contextDelta is the block for a message and what the conversation will have seen once it is sent: everything when
// seen is nil, else the sections that are new or changed, the time always. "" when there is no snapshot.
func contextDelta(seen map[string]string, snapshot string) (string, map[string]string) {
	sections := snapshotSections(snapshot)
	if len(sections) == 0 {
		return "", seen
	}
	next := make(map[string]string, len(sections))
	for k, v := range seen {
		next[k] = v
	}
	var out []string
	for _, s := range sections {
		if seen == nil || s.key == timeSection || seen[s.key] != s.text {
			out = append(out, s.text)
		}
		next[s.key] = s.text
	}
	return prompts.RenderTurnContext(prompts.TurnContext{Full: seen == nil, Sections: out}), next
}

// turnContext is the context for conversation id's next message from the hub's answer (assistantContext): the
// <persona-context> block, then the <persona-view-errors> one when its last replies drew rich views that failed their
// check. commit, called once the message is on its way, records the snapshot as seen and acks the view errors (the
// hub keeps them until then: a message that fails to go out leaves them for the next one).
func (n *Node) turnContext(id string, c contextAnswer) (block string, commit func()) {
	n.mu.Lock()
	seen := n.ctxSent[id]
	n.mu.Unlock()
	block, next := contextDelta(seen, c.Text)
	snapshot := block != ""
	if c.ViewErrors != "" {
		block = strings.TrimSpace(block + "\n" + c.ViewErrors)
	}
	return block, func() {
		if len(c.Receipts) > 0 {
			n.ackViewErrors(id, c.Receipts)
		}
		if !snapshot {
			return
		}
		n.mu.Lock()
		if n.ctxSent == nil {
			n.ctxSent = map[string]map[string]string{}
		}
		n.ctxSent[id] = next
		n.mu.Unlock()
	}
}

// contextCompacted forgets what conversation id has seen: its old blocks were summarized away.
func (n *Node) contextCompacted(id string) {
	n.mu.Lock()
	delete(n.ctxSent, id)
	n.mu.Unlock()
}

// toolsAtStart notes the tools a conversation's process started with (claude's init) and says so when they differ
// from its last start: the tool definitions come before the history in every request, so a different set (a tool
// added or gone: a deploy, another phone's tools) writes the whole conversation again, like a different system
// prompt would. Measured 2026-11-01: one MCP tool fewer, 0 of 275K tokens read from the cache; the same tools with
// their schema keys in another order, all of it read.
func (n *Node) toolsAtStart(id string, tools []string) {
	tools = slices.Sorted(slices.Values(tools))
	n.mu.Lock()
	if n.ctxTools == nil {
		n.ctxTools = map[string][]string{}
	}
	before, known := n.ctxTools[id]
	n.ctxTools[id] = tools
	n.mu.Unlock()
	if !known || slices.Equal(before, tools) {
		return
	}
	var added, gone []string
	for _, t := range tools {
		if !slices.Contains(before, t) {
			added = append(added, t)
		}
	}
	for _, t := range before {
		if !slices.Contains(tools, t) {
			gone = append(gone, t)
		}
	}
	n.logger.Printf("assistant %s: tools changed since its last start (+%v -%v): its prompt cache starts over", short(id), added, gone)
}

// contextAnswer is the hub's assistant.context answer (assistantwire.ContextResult).
type contextAnswer struct {
	Text       string          `json:"text"`
	ViewErrors string          `json:"view_errors"`
	Receipts   json.RawMessage `json:"view_error_receipts"`
}

// assistantContext asks the hub for the live snapshot (prompts.AssistantContext) and the view errors to report. The
// hub answers from what it read lately and waits only briefly for a new read, so this doesn't hold the message up.
func (n *Node) assistantContext(id string) contextAnswer {
	if n.peer == nil {
		return contextAnswer{}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	var res contextAnswer
	if err := n.peer.Call(ctx, "assistant.context", map[string]any{"session_id": id}, &res); err != nil {
		n.logger.Printf("assistant context: %v", err)
		return contextAnswer{}
	}
	if string(res.Receipts) == "null" {
		res.Receipts = nil
	}
	return res
}

// ackViewErrors tells the hub the message carrying the view errors of receipts was delivered. The hub scopes it to
// this machine; a lost ack only means they are told again with the next message.
func (n *Node) ackViewErrors(id string, receipts json.RawMessage) {
	if n.peer == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := n.peer.Call(ctx, "assistant.context", map[string]any{"session_id": id, "ack": receipts}, nil); err != nil {
		n.logger.Printf("assistant view errors ack: %v", err)
	}
}
