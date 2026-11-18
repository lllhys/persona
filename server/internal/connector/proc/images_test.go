package proc

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/png"
	"strings"
	"testing"

	"github.com/nwverse/persona/server/internal/connector"
)

// imageProgram is exampleProgram whose read tools answer with two pictures besides the text (#8484).
type imageProgram struct{ *exampleProgram }

func (p imageProgram) CallImages(ctx context.Context, name string, args json.RawMessage) (connector.ToolAnswer, error) {
	text, err := p.exampleProgram.Call(ctx, name, args)
	var b bytes.Buffer
	_ = png.Encode(&b, image.NewGray(image.Rect(0, 0, 6, 4)))
	data := base64.StdEncoding.EncodeToString(b.Bytes())
	return connector.ToolAnswer{Text: text, Images: []connector.ToolImage{
		{Mime: "image/png", Data: data, Caption: "00:01"}, {Mime: "image/jpeg", Data: data, Caption: "00:09"}}}, err
}

// A program's pictures come through the protocol, over a pipe and as a plugin; a program without any answers its text
// alone, as before.
func TestReadToolAnswersWithImages(t *testing.T) {
	for _, tc := range []struct {
		name   string
		env    []string
		images int
	}{
		{"a program with pictures", []string{"PERSONA_PROC_KIND=images"}, 2},
		{"a program without", nil, 0},
	} {
		for _, way := range []string{"pipe", "plugin"} {
			t.Run(tc.name+" over the "+way, func(t *testing.T) {
				sec := &memSecrets{}
				c := New(program(t, sec, tc.env...))
				if way == "plugin" {
					c = linked(t, sec, tc.env...)
				}
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				go c.Run(ctx)
				waitUntil(t, "the program's status", func() bool { return c.Status(ctx).State == connector.StateNotSignedIn })
				a, err := c.CallImages(ctx, "example_chats", json.RawMessage(`{}`))
				if err != nil || len(a.Images) != tc.images || a.Text == "" {
					t.Fatalf("CallImages: %d pictures, text %q, %v", len(a.Images), a.Text, err)
				}
				if tc.images > 0 && (a.Images[0].Caption != "00:01" || a.Images[1].Mime != "image/jpeg") {
					t.Fatalf("pictures as sent: %+v", a.Images)
				}
				res := connector.ToolResult(a)
				var kinds []string
				for _, b := range res.Content {
					kinds = append(kinds, b.Type+":"+b.MimeType)
				}
				want := "text:"
				if tc.images > 0 {
					want = "text: text: image:image/png text: image:image/png" // the second is a PNG whatever it said
				}
				if got := strings.Join(kinds, " "); got != want {
					t.Fatalf("blocks %s, want %s", got, want)
				}
				if text, err := c.Call(ctx, "example_chats", json.RawMessage(`{}`)); err != nil || text != a.Text {
					t.Fatalf("Call is the text alone: %q %v", text, err)
				}
			})
		}
	}
}
