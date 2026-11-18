package connector

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"math/rand/v2"
	"strings"
	"testing"

	"github.com/nwverse/persona/server/internal/proto"
)

// Pictures in a read tool's answer (#8484): the text first, then each picture that passes with a line before it, and
// a line after the text for the ones left out.

func jpegOf(t *testing.T, w, h int) []byte {
	t.Helper()
	var b bytes.Buffer
	if err := jpeg.Encode(&b, image.NewRGBA(image.Rect(0, 0, w, h)), nil); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func pngOf(t *testing.T, w, h int, noise bool) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	if noise { // incompressible: as many bytes as pixels hold
		r := rand.New(rand.NewPCG(1, 2))
		for i := range img.Pix {
			img.Pix[i] = byte(r.UintN(256))
		}
	} else {
		img.Set(0, 0, color.White)
	}
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

// webp1x1 is a 1×1 lossless WebP.
const webp1x1 = "UklGRhoAAABXRUJQVlA4TA0AAAAvAAAAEAcQERGIiP4HAA=="

func pic(data []byte, caption string) ToolImage {
	return ToolImage{Mime: "image/jpeg", Data: base64.StdEncoding.EncodeToString(data), Caption: caption}
}

// texts are the result's text blocks, images its image blocks' types.
func blocks(r proto.ToolResult) (texts, images []string) {
	for _, c := range r.Content {
		if c.Type == "image" {
			images = append(images, c.MimeType)
		} else {
			texts = append(texts, c.Text)
		}
	}
	return texts, images
}

func TestToolResultWithoutImagesIsTheText(t *testing.T) {
	if got := ToolResult(ToolAnswer{Text: "two posts"}); len(got.Content) != 1 || got.Content[0] != (proto.Content{Type: "text", Text: "two posts"}) || got.IsError {
		t.Fatalf("%+v", got)
	}
}

func TestToolResultOrdersTextCaptionsAndImages(t *testing.T) {
	a := ToolAnswer{Text: "a video of 0:42", Images: []ToolImage{pic(jpegOf(t, 64, 36), "00:01 the title"), pic(jpegOf(t, 64, 36), "")}}
	got := ToolResult(a)
	want := []proto.Content{{Type: "text", Text: "a video of 0:42"}, {Type: "text", Text: "[image 1 of 2] 00:01 the title"}, {Type: "image"},
		{Type: "text", Text: "[image 2 of 2]"}, {Type: "image"}}
	if len(got.Content) != len(want) {
		t.Fatalf("%d blocks: %+v", len(got.Content), got.Content)
	}
	for i, w := range want {
		c := got.Content[i]
		if c.Type != w.Type || w.Type == "text" && c.Text != w.Text {
			t.Errorf("block %d: %s %q, want %s %q", i, c.Type, c.Text, w.Type, w.Text)
		}
		if c.Type == "image" {
			if c.MimeType != "image/jpeg" || c.Data != a.Images[0].Data {
				t.Errorf("block %d: %s, %d bytes of base64", i, c.MimeType, len(c.Data))
			}
		}
	}
}

func TestToolResultLeavesOutWhatIsOverALimit(t *testing.T) {
	small := jpegOf(t, 32, 32)
	whole := jpegOf(t, 256, 256)
	many := make([]ToolImage, 17)
	for i := range many {
		many[i] = pic(small, "")
	}
	noise := pngOf(t, 420, 420, true) // about 0.7 MiB: six are over 4 MiB in all
	if len(noise) > ToolImageBytesMost || 6*len(noise) <= ToolImagesBytesMost || 5*len(noise) > ToolImagesBytesMost {
		t.Fatalf("a noise PNG of %d bytes does not make the case", len(noise))
	}
	heavy := make([]ToolImage, 6)
	for i := range heavy {
		heavy[i] = pic(noise, "")
	}
	big := pngOf(t, 2000, 1500, false) // 3 million pixels: the third is over 8 million in all
	for _, c := range []struct {
		name   string
		images []ToolImage
		kept   int
		note   string
	}{
		{"a 17th", many, 16, "[1 image left out: more than 16]"},
		{"over 1 MiB", []ToolImage{pic(small, ""), pic(bytes.Repeat([]byte{0xff}, 2<<20), "")}, 1, "[1 image left out: over 1 MiB]"},
		{"over 2000 px a side", []ToolImage{pic(pngOf(t, 2001, 10, false), ""), pic(pngOf(t, 10, 2001, false), "")}, 0, "[2 images left out: over 2000 pixels a side]"},
		{"cut short", []ToolImage{pic(whole[:len(whole)/2], "")}, 0, "[1 image left out: not a whole JPEG, PNG or WebP]"},
		{"not an image", []ToolImage{pic([]byte("hello, not a picture"), "")}, 0, "[1 image left out: not a whole JPEG, PNG or WebP]"},
		{"not base64", []ToolImage{{Data: "not base64!"}}, 0, "[1 image left out: not a whole JPEG, PNG or WebP]"},
		{"a GIF", []ToolImage{pic([]byte("GIF89a\x01\x00\x01\x00\x00\x00\x00;"), "")}, 0, "[1 image left out: not a whole JPEG, PNG or WebP]"},
		{"over 4 MiB in all", heavy, 5, "[1 image left out: over 4 MiB in all]"},
		{"over 8 million pixels in all", []ToolImage{pic(big, ""), pic(big, ""), pic(big, ""), pic(small, "")}, 3, "[1 image left out: over 8 million pixels in all]"},
		{"two reasons, in order", []ToolImage{pic([]byte("x"), ""), pic(bytes.Repeat([]byte{1}, 2<<20), ""), pic(small, "")}, 1,
			"[1 image left out: over 1 MiB; 1 image left out: not a whole JPEG, PNG or WebP]"},
	} {
		got := ToolResult(ToolAnswer{Text: "the post", Images: c.images})
		texts, images := blocks(got)
		if len(images) != c.kept {
			t.Errorf("%s: kept %d, want %d", c.name, len(images), c.kept)
		}
		if want := "the post\n\n" + c.note; texts[0] != want {
			t.Errorf("%s: text %q, want %q", c.name, texts[0], want)
		}
	}
}

// The type is the bytes', whatever the program said: a mislabelled picture goes as what it is.
func TestToolResultTypeIsTheBytes(t *testing.T) {
	webp, _ := base64.StdEncoding.DecodeString(webp1x1)
	got := ToolResult(ToolAnswer{Images: []ToolImage{pic(pngOf(t, 8, 8, false), ""), {Mime: "image/png", Data: webp1x1}, pic(jpegOf(t, 8, 8), "")}})
	if _, types := blocks(got); strings.Join(types, " ") != "image/png image/webp image/jpeg" {
		t.Fatalf("types %v (webp %d bytes)", types, len(webp))
	}
	// No text: the first block is empty, the lines and pictures follow.
	if got.Content[0].Text != "" || got.Content[1].Text != "[image 1 of 3]" {
		t.Fatalf("%+v", got.Content[:2])
	}
	if got := ToolResult(ToolAnswer{Images: []ToolImage{{Data: "AAAA"}}}); got.Content[0].Text != "[1 image left out: not a whole JPEG, PNG or WebP]" {
		t.Fatalf("all left out without text: %q", got.Content[0].Text)
	}
}

func TestCaptionLine(t *testing.T) {
	for in, want := range map[string]string{
		"  00:12 kitchen  ":         "00:12 kitchen",
		"first line\nsecond line":   "first line",
		"cr\rsecond":                "cr",
		"bell\a and ‮right-to-left": "bell and right-to-left",
		strings.Repeat("字", 250):    strings.Repeat("字", ToolImageCaptionMost) + "…",
	} {
		if got := captionLine(in); got != want {
			t.Errorf("captionLine(%q) = %q, want %q", in, got, want)
		}
	}
}

// imageFake is fake whose read tool answers with pictures.
type imageFake struct{ *fake }

func (f imageFake) CallImages(_ context.Context, name string, args json.RawMessage) (ToolAnswer, error) {
	return ToolAnswer{Text: "frames of " + name, Images: []ToolImage{{Mime: "image/png", Data: base64.StdEncoding.EncodeToString(pngOfSize(4)), Caption: "00:00"}}}, nil
}

func pngOfSize(n int) []byte {
	var b bytes.Buffer
	_ = png.Encode(&b, image.NewGray(image.Rect(0, 0, n, n)))
	return b.Bytes()
}

// A connector with pictures answers the model with them; one without answers its text as before.
func TestRegistryCallsWithImages(t *testing.T) {
	ctx := context.Background()
	r := NewRegistry(nil)
	if err := r.Add(imageFake{&fake{id: "pics", status: Status{State: StateOK}}}); err != nil {
		t.Fatal(err)
	}
	if err := r.Add(&fake{id: "words", status: Status{State: StateOK}}); err != nil {
		t.Fatal(err)
	}
	texts, images := blocks(r.Call(ctx, "pics_read", nil, nil))
	if strings.Join(texts, "|") != "frames of pics_read|[image 1 of 1] 00:00" || len(images) != 1 || images[0] != "image/png" {
		t.Fatalf("with pictures: %q %v", texts, images)
	}
	if got := r.Call(ctx, "words_read", json.RawMessage(`{}`), nil); len(got.Content) != 1 || got.Content[0].Text != "read {}" {
		t.Fatalf("without: %+v", got)
	}
}
