package connector

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/nwverse/persona/server/internal/connectorwire"
)

// Defined in the contract package connectorwire (docs/server/architecture.md "Contract packages"); the old names stay.
type (
	GroupAppend         = connectorwire.GroupAppend
	GroupAssign         = connectorwire.GroupAssign
	GroupEvent          = connectorwire.GroupEvent
	GroupFallback       = connectorwire.GroupFallback
	GroupFallbackResult = connectorwire.GroupFallbackResult
	GroupNew            = connectorwire.GroupNew
	GroupToolCall       = connectorwire.GroupToolCall
	InboxReply          = connectorwire.InboxReply
	InboxThing          = connectorwire.InboxThing
	StatusChange        = connectorwire.StatusChange
	Tag                 = connectorwire.Tag
	ThingExtra          = connectorwire.ThingExtra
	ThingGroup          = connectorwire.ThingGroup
	ThingParent         = connectorwire.ThingParent
	Tier                = connectorwire.Tier
)

const (
	DoneRead            = connectorwire.DoneRead
	DoneReplied         = connectorwire.DoneReplied
	GroupMessagesAround = connectorwire.GroupMessagesAround
	GroupMessagesBefore = connectorwire.GroupMessagesBefore
	GroupSearch         = connectorwire.GroupSearch
	TagBranch           = connectorwire.TagBranch
	TagChat             = connectorwire.TagChat
	TagRepo             = connectorwire.TagRepo
	TagSender           = connectorwire.TagSender
	TierAction          = connectorwire.TierAction
	TierFYI             = connectorwire.TierFYI
	TierNoise           = connectorwire.TierNoise
)

var MigratableConnectors = connectorwire.MigratableConnectors

// Grouper is an inbox whose new messages are related to events (CapGroup): the hub's model decides which event each
// message belongs to, reading the conversation through GroupTool, and the connector groups them by structure itself
// when the model can't (GroupFallback). Both may answer that they have no such thing (an unknown method): the model
// is told the tool is not there; without a fallback the batch waits for the model. The hub writes the events itself,
// unless the connector also keeps their meta (CapMeta, EventWriter).
type Grouper interface {
	GroupTool(ctx context.Context, call GroupToolCall) (json.RawMessage, error)
	GroupFallback(ctx context.Context, f GroupFallback) (GroupFallbackResult, error)
}

// EventWriter is a grouping inbox that keeps each event's meta (CapMeta): the hub has it write every event it
// composed (GroupAppend: one event a call, meta in, meta out) and stores what comes back.
type EventWriter interface {
	GroupAppend(ctx context.Context, a GroupAppend) (InboxThing, error)
}

// Inbox is a connector with things for the inbox. Collect answers from the connector's memory (it is called on
// every inbox collection; a connector that polls does so in its own Watch and calls changed(ScopeInbox)). Reply
// sends text the user confirmed (the send button is the confirmation) to the thread.
type Inbox interface {
	Collect(ctx context.Context) ([]InboxThing, error)
	Reply(ctx context.Context, r InboxReply) error
}

// ErrCannotReply is a Reply refused before anything was sent because the thing has no way to be answered through the
// connector (a read-only document, a thread with no chat to post to). Wrap it (fmt.Errorf("%w: …")) without the
// thread or other identifiers: the apps get only its kind (inbox_cannot_reply, #9125).
var ErrCannotReply = errors.New("this item cannot be replied to through its connector")

// InboxSettler is an inbox whose rules can close items the hub still has open (made under older rules, or of a
// thread the connector no longer brings in): Settle gets the open items' threads and returns, for each that is
// over, why. Nothing is deleted: the hub files them as done (read).
type InboxSettler interface {
	Settle(ctx context.Context, threads []string) map[string]string
}
