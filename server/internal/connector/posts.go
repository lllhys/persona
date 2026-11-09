package connector

import (
	"context"

	"github.com/nwverse/persona/server/internal/connectorwire"
)

// What a connector's program may ask of the knowledge base, besides handing it conversations (Archive): keep a
// feed's public posts in its post archive (docs/assistant/knowledge.md "Pipeline", External posts), find which posts' comments to read again,
// and see the conversations already archived under its names (to go on from where they are). Only for a connector
// that declares an archive, and only while the user lets it into the knowledge base ("Add to knowledge base").

// Defined in the contract package connectorwire (docs/server/architecture.md "Contract packages"); the old names stay.
type (
	ArchivedChat = connectorwire.ArchivedChat
	PendingPost  = connectorwire.PendingPost
	Post         = connectorwire.Post
	PostComment  = connectorwire.PostComment
	PostPut      = connectorwire.PostPut
)

const (
	PendingPostsMost = connectorwire.PendingPostsMost
	PostChanged      = connectorwire.PostChanged
	PostCommentsMost = connectorwire.PostCommentsMost
	PostNew          = connectorwire.PostNew
	PostRefused      = connectorwire.PostRefused
	PostSame         = connectorwire.PostSame
	PostTextMost     = connectorwire.PostTextMost
	PostsPerPut      = connectorwire.PostsPerPut
)

// KnowledgeBase is the hub's side of those calls, for one connector (bound to it: nothing the program sends picks
// another).
type KnowledgeBase interface {
	PutPosts(ctx context.Context, posts []PostPut) ([]string, error)
	// PutComments replaces the comments of a post already archived (one of this connector's), read when it had count
	// comments; the post itself stays as it is.
	PutComments(ctx context.Context, id string, count int, comments []PostComment) error
	PendingPosts(ctx context.Context, limit int) ([]PendingPost, error)
	ArchivedChats(ctx context.Context, source string) ([]ArchivedChat, error)
}
