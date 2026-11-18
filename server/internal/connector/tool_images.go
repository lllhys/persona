package connector

// Pictures in a read tool's answer (#8484, docs/protocol/connector-protocol.md "tools.call"): a connector can hand the
// model what it should look at (frames of a video, the pictures of a post) after the text. The hub holds each to what
// every model it serves takes, and leaves out the rest with a line that says so: a broken or oversized picture sent to
// a model can fail every later turn of the conversation.

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
	_ "image/jpeg" // the decoders ToolResult checks pictures with
	_ "image/png"
	"net/http"
	"slices"
	"strings"
	"unicode"

	_ "golang.org/x/image/webp"

	"github.com/nwverse/persona/server/internal/proto"
)

// ToolImage is one picture of a read tool's answer (tools.call's images): base64 data, the type a hint only (the
// bytes say it), and a caption of one line said before it.
type ToolImage struct {
	Mime    string `json:"mime,omitempty"`
	Data    string `json:"data"`
	Caption string `json:"caption,omitempty"`
}

// ToolAnswer is a read tool's answer with its pictures (ImageTools).
type ToolAnswer struct {
	Text   string
	Images []ToolImage
}

// ImageTools are ReadTools whose answers can carry pictures for the model: a connector program's (proc), or an
// in-process connector that has them. Not a capability of its own: a connector with tools that implements it is
// asked this way.
type ImageTools interface {
	ReadTools
	CallImages(ctx context.Context, name string, args json.RawMessage) (ToolAnswer, error)
}

// The limits of a tool's pictures, each one's and all of them together. 1 MiB keeps a picture whole in what the
// transcript keeps for the apps (transcript maxImageLen); 2000 pixels a side is the most Claude takes in a request
// with more than 20 pictures, which a conversation soon is; 4 MiB in all fits a source runtime's 8 MiB message with
// base64's third on top; 8 million pixels (about 11,000 tokens) keeps an answer well inside Claude Code's MCP output
// limit.
const (
	ToolImagesMost       = 16
	ToolImageBytesMost   = 1 << 20
	ToolImageSideMost    = 2000
	ToolImagesBytesMost  = 4 << 20
	ToolImagesPixelsMost = 8_000_000
	// ToolImageCaptionMost is how much of a caption's first line is kept (runes).
	ToolImageCaptionMost = 200
)

// Why a picture was left out, in the order the note says them.
const (
	leftTooMany   = "more than 16"
	leftTooBig    = "over 1 MiB"
	leftTooWide   = "over 2000 pixels a side"
	leftNotImage  = "not a whole JPEG, PNG or WebP"
	leftAllBytes  = "over 4 MiB in all"
	leftAllPixels = "over 8 million pixels in all"
)

var leftReasons = []string{leftTooMany, leftTooBig, leftTooWide, leftNotImage, leftAllBytes, leftAllPixels}

// toolImageTypes are the types a picture may be, as http.DetectContentType says them.
var toolImageTypes = []string{"image/jpeg", "image/png", "image/webp"}

// ToolResult is a read tool's answer as the model gets it: its text, then each picture that passes the limits, a line
// before each ("[image 1 of 2] its caption"). A picture over a limit is left out, and a line after the text says how
// many and why. Without pictures it is the text alone, as before.
func ToolResult(a ToolAnswer) proto.ToolResult {
	if len(a.Images) == 0 {
		return proto.TextResult(a.Text, false)
	}
	type kept struct {
		data    []byte
		mime    string
		caption string
	}
	var keep []kept
	left := map[string]int{}
	bytesSoFar, pixelsSoFar := 0, 0
	for _, img := range a.Images {
		if len(keep) >= ToolImagesMost {
			left[leftTooMany]++
			continue
		}
		data, mime, w, h, why := checkToolImage(img)
		switch {
		case why != "":
		case bytesSoFar+len(data) > ToolImagesBytesMost:
			why = leftAllBytes
		case pixelsSoFar+w*h > ToolImagesPixelsMost:
			why = leftAllPixels
		case !decodes(data):
			why = leftNotImage
		}
		if why != "" {
			left[why]++
			continue
		}
		bytesSoFar, pixelsSoFar = bytesSoFar+len(data), pixelsSoFar+w*h
		keep = append(keep, kept{data: data, mime: mime, caption: captionLine(img.Caption)})
	}
	text := a.Text
	if note := leftNote(left); note != "" {
		if strings.TrimSpace(text) != "" {
			text = strings.TrimRight(text, "\n") + "\n\n"
		}
		text += note
	}
	out := proto.TextResult(text, false)
	for i, k := range keep {
		line := fmt.Sprintf("[image %d of %d]", i+1, len(keep))
		if k.caption != "" {
			line += " " + k.caption
		}
		out.Content = append(out.Content, proto.Content{Type: "text", Text: line},
			proto.Content{Type: "image", Data: base64.StdEncoding.EncodeToString(k.data), MimeType: k.mime})
	}
	return out
}

// checkToolImage is img's bytes, type and size, or why it is left out (its own limits; the totals and the full decode
// are ToolResult's).
func checkToolImage(img ToolImage) (data []byte, mime string, w, h int, why string) {
	if base64.StdEncoding.DecodedLen(len(img.Data)) > ToolImageBytesMost+3 {
		return nil, "", 0, 0, leftTooBig
	}
	data, err := base64.StdEncoding.DecodeString(img.Data)
	if err != nil || len(data) == 0 {
		return nil, "", 0, 0, leftNotImage
	}
	if len(data) > ToolImageBytesMost {
		return nil, "", 0, 0, leftTooBig
	}
	mime = http.DetectContentType(data)
	if !slices.Contains(toolImageTypes, mime) {
		return nil, "", 0, 0, leftNotImage
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || cfg.Width <= 0 || cfg.Height <= 0 {
		return nil, "", 0, 0, leftNotImage
	}
	if cfg.Width > ToolImageSideMost || cfg.Height > ToolImageSideMost {
		return nil, "", 0, 0, leftTooWide
	}
	return data, mime, cfg.Width, cfg.Height, ""
}

// decodes says whether data decodes whole: a header that reads with a body cut short or broken is refused here, not
// by the model's provider in every later turn.
func decodes(data []byte) bool {
	_, _, err := image.Decode(bytes.NewReader(data))
	return err == nil
}

// captionLine is a caption as said before its picture: its first line, without control or format characters, cut to
// ToolImageCaptionMost runes.
func captionLine(s string) string {
	s, _, _ = strings.Cut(strings.ReplaceAll(s, "\r", "\n"), "\n")
	s = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
			return -1
		}
		return r
	}, s)
	s = strings.TrimSpace(s)
	if r := []rune(s); len(r) > ToolImageCaptionMost {
		s = strings.TrimSpace(string(r[:ToolImageCaptionMost])) + "…"
	}
	return s
}

// leftNote is the line that says which pictures were left out and why ("" for none).
func leftNote(left map[string]int) string {
	var parts []string
	for _, why := range leftReasons {
		switch n := left[why]; {
		case n == 1:
			parts = append(parts, "1 image left out: "+why)
		case n > 1:
			parts = append(parts, fmt.Sprintf("%d images left out: %s", n, why))
		}
	}
	if len(parts) == 0 {
		return ""
	}
	return "[" + strings.Join(parts, "; ") + "]"
}
