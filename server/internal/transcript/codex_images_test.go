package transcript

import (
	"strings"
	"testing"

	"github.com/nwverse/persona/server/internal/proto"
)

// A Codex MCP call's pictures (a connector's, computer use's) become image items on the call's card, as a Claude tool
// result's do (#8484); a picture without data or type, or over what the apps keep, is left out.
func TestCodexMcpToolCallImages(t *testing.T) {
	big := strings.Repeat("A", maxImageLen+4)
	line := `{"timestamp":"2026-10-26T05:06:05.000Z","type":"event_msg","payload":{"type":"item_completed","item":{"type":"McpToolCall","id":"c2","server":"persona","tool":"example_frames","arguments":{"note":"n1"},"status":"completed","result":{"content":[` +
		`{"type":"text","text":"a video of 0:42"},{"type":"text","text":"[image 1 of 2] 00:01"},{"type":"image","data":"iVBORw0KGgo=","mimeType":"image/png"},` +
		`{"type":"text","text":"[image 2 of 2] 00:30"},{"type":"image","data":"/9j/4AAQ","mimeType":"image/jpeg"},` +
		`{"type":"image","data":"","mimeType":"image/png"},{"type":"image","data":"AAAA","mimeType":"text/plain"},{"type":"image","data":"` + big + `","mimeType":"image/png"}],"isError":false}}}}`
	items := ParseLine(7, []byte(line), &Meta{Codex: true})
	var got []string
	for _, it := range items {
		got = append(got, it.Role+":"+it.Kind)
	}
	if want := "assistant:tool_use user:tool_result user:image user:image"; strings.Join(got, " ") != want {
		t.Fatalf("items %v, want %s", got, want)
	}
	res, a, b := items[1], items[2], items[3]
	if res.Text != "a video of 0:42\n[image 1 of 2] 00:01\n[image 2 of 2] 00:30" {
		t.Fatalf("result text %q", res.Text)
	}
	for _, im := range []proto.Item{a, b} {
		if im.ToolUseID != "c2" || im.Image == nil || im.ID == res.ID {
			t.Fatalf("image item %+v", im)
		}
	}
	if a.Image.Mime != "image/png" || a.Image.Data != "iVBORw0KGgo=" || b.Image.Mime != "image/jpeg" || a.ID == b.ID {
		t.Fatalf("images %+v %+v", a.Image, b.Image)
	}
}
