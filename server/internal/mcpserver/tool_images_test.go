package mcpserver

import (
	"encoding/json"
	"testing"

	"github.com/nwverse/persona/server/internal/proto"
)

// A tool's pictures (computer use's screenshots, a connector's, #8484) reach the model as MCP image content, in order,
// after the text, with no structuredContent (Codex gives the model only that when it is there).
func TestToolResultImages(t *testing.T) {
	res := proto.ToolResult{Content: []proto.Content{{Type: "text", Text: "a post"}, {Type: "text", Text: "[image 1 of 1] cover"},
		{Type: "image", Data: "iVBORw0KGgo=", MimeType: "image/png"}}}
	b, _ := json.Marshal(toolResult(res))
	want := `{"content":[{"text":"a post","type":"text"},{"text":"[image 1 of 1] cover","type":"text"},{"data":"iVBORw0KGgo=","mimeType":"image/png","type":"image"}],"isError":false}`
	if string(b) != want {
		t.Fatalf("got  %s\nwant %s", b, want)
	}
}
