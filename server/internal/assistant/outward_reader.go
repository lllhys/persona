package assistant

import (
	"context"
	"slices"

	"github.com/nwverse/persona/server/internal/kb"
)

// Conversations that write to other people (docs/assistant/knowledge.md "Who reads the private parts"). The inbox's
// AI twin drafts and sends replies to the people who wrote, Lark /agent answers in chats other people read, and a
// conversation an extension opens hands its answers to that extension, which may send them anywhere. None of them
// reads the private parts of a person's profile: the user's own words about someone, and the guesses about them
// (kb.PrivateProfileLine). The hub drops those lines from everything its tools give such a conversation, whatever the
// tool (kb_get and kb_search, and conversation_read, agent_read or any other that could quote them), and kb_search
// passes over them so they do not take up its limit. The heading of a profile asking the model not to pass it on stays,
// but it is no longer what keeps them out.
//
// When it cannot be told where a conversation comes from, it is taken for one of these: reading a little less is the
// smaller harm. That is the case while the origins cannot be read, and for a conversation not noted yet while a start
// on its machine is under way (its first tool calls can come before its origin is noted).

// outwardOrigins are the origins of conversations that write to other people (as attendeeNeverOrigins, but for the
// channels: a channel conversation is the user's own, with the user, in a private chat).
var outwardOrigins = []string{OriginInbox, OriginLarkAgent, OriginExtension}

// outwardReader says whether conversation session on machine writes to other people, so reads no private part of a
// profile. A call that names no conversation (the hub's own, or an Agent's) is not one.
func (s *Service) outwardReader(ctx context.Context, machine, session string) bool {
	if session == "" {
		return false
	}
	b := &s.origins
	b.mu.Lock()
	if !s.originsLocked(ctx) {
		b.mu.Unlock()
		return true
	}
	o := b.m[session]
	b.mu.Unlock()
	if o != nil {
		return slices.Contains(outwardOrigins, o.Origin) || o.Ext != ""
	}
	return s.assistantStarting(machine)
}

// assistantStarting says whether a conversation is being started on machine (beginAssistantStart).
func (s *Service) assistantStarting(machine string) bool {
	if machine == "" {
		return false
	}
	if s.devs != nil {
		machine = s.devs.Canonical(machine)
	}
	b := &s.startingAssistants
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.machines[machine] > 0
}

// outwardReaderKey marks the context of a tool call from a conversation that writes to other people (Tool).
type outwardReaderKey struct{}

func withOutwardReader(ctx context.Context) context.Context {
	return context.WithValue(ctx, outwardReaderKey{}, true)
}

// isOutwardReader says whether ctx is that of a tool call from a conversation that writes to other people.
func isOutwardReader(ctx context.Context) bool {
	v, _ := ctx.Value(outwardReaderKey{}).(bool)
	return v
}

// outwardText is text as a tool gives it to the conversation: without the private parts of profiles when outward.
func outwardText(outward bool, text string) string {
	if !outward {
		return text
	}
	return kb.WithoutPrivateProfile(text)
}

// profileSkip is what kb_search passes over for the conversation of ctx: the lines of the private parts of
// profiles for one that writes to other people, so they do not take up its limit; nothing for the others.
func profileSkip(ctx context.Context) []func(string) bool {
	if isOutwardReader(ctx) {
		return []func(string) bool{kb.PrivateProfileLine}
	}
	return nil
}
