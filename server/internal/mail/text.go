package mail

import (
	"bytes"
	"html"
	"io"
	"strings"
	"unicode/utf8"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
	gomessage "github.com/emersion/go-message"

	"github.com/nwverse/persona/server/internal/lazyre"
)

// snippetOf turns the fetched header + body sections into a short preview.
func snippetOf(sections []imapclient.FetchBodySectionBuffer) string {
	var header, body []byte
	for _, sec := range sections {
		if sec.Section == nil {
			continue
		}
		switch sec.Section.Specifier {
		case imap.PartSpecifierHeader:
			header = sec.Bytes
		case imap.PartSpecifierText:
			body = sec.Bytes
		}
	}
	if len(body) == 0 {
		return ""
	}
	raw := append(append([]byte{}, header...), body...)
	text := textFromRaw(raw)
	if text == "" {
		text = textFromRaw(body) // no usable header: try the body alone
	}
	text = strings.Join(strings.Fields(text), " ")
	return truncateRunes(text, snippetRunes)
}

// textFromRaw extracts readable text from a possibly truncated raw message.
func textFromRaw(raw []byte) string {
	if len(raw) == 0 {
		return ""
	}
	// Tolerate truncation and unknown charsets: message.Read still returns a
	// usable entity for the leading bytes.
	e, _ := gomessage.Read(bytes.NewReader(raw))
	if e == nil {
		return ""
	}
	return textFromEntity(e, 0)
}

const textFromEntityMaxDepth = 20 // real mail is shallow; a cap stops a crafted nest from blowing the stack

// textFromEntity returns the first non-empty text/* part, depth-first; multipart parts (e.g. alternative inside mixed)
// are descended into.
func textFromEntity(e *gomessage.Entity, depth int) string {
	if depth > textFromEntityMaxDepth {
		return ""
	}
	if mr := e.MultipartReader(); mr != nil {
		for {
			p, err := mr.NextPart()
			if gomessage.IsUnknownEncoding(err) {
				continue // a transfer encoding go-message cannot decode: skip the part, the next one is still readable
			}
			// A part in an unknown charset comes back with its raw body, as go-message intends.
			if p == nil || (err != nil && !gomessage.IsUnknownCharset(err)) {
				return "" // end of this multipart (or a truncated tail)
			}
			if text := textFromEntity(p, depth+1); text != "" {
				return text
			}
		}
	}
	mt, _, _ := e.Header.ContentType()
	if !strings.HasPrefix(mt, "text/") {
		return ""
	}
	return renderText(mt, readAllTolerant(e.Body, 8<<10))
}

// readAllTolerant reads at most n bytes, ignoring decode errors at a truncated
// tail.
func readAllTolerant(r io.Reader, n int) string {
	if r == nil {
		return ""
	}
	buf := &bytes.Buffer{}
	_, _ = io.CopyN(buf, r, int64(n))
	return buf.String()
}

// renderText converts text/plain or text/html to preview text.
func renderText(mediaType string, s string) string {
	if strings.HasPrefix(mediaType, "text/html") {
		return stripHTML(s)
	}
	return s
}

// stripHTML removes tags and script/style content and unescapes entities.
func stripHTML(s string) string {
	var b strings.Builder
	lower := asciiLower(s) // same length as s: tags are matched at s's offsets
	i := 0
	for i < len(s) {
		if s[i] != '<' {
			b.WriteByte(s[i])
			i++
			continue
		}
		end := strings.IndexByte(s[i:], '>')
		if end < 0 {
			break
		}
		tag := lower[i : i+end+1]
		i += end + 1
		if strings.HasPrefix(tag, "<script") || strings.HasPrefix(tag, "<style") {
			closing := "</script"
			if strings.HasPrefix(tag, "<style") {
				closing = "</style"
			}
			j := strings.Index(lower[i:], closing)
			if j < 0 {
				break // a preview cut off inside a <style> block: the rest is CSS, not text
			}
			i += j
		} else if strings.HasPrefix(tag, "<br") || strings.HasPrefix(tag, "</p") || strings.HasPrefix(tag, "</div") {
			b.WriteByte(' ')
		}
	}
	return html.UnescapeString(b.String())
}

// asciiLower lowers A–Z only, byte for byte. strings.ToLower can change the length (U+0130 and the Kelvin sign lower
// to one byte, an invalid byte becomes U+FFFD's three), and stripHTML indexes the lowered copy with s's offsets.
func asciiLower(s string) string {
	b := []byte(s)
	for i, c := range b {
		if 'A' <= c && c <= 'Z' {
			b[i] = c + ('a' - 'A')
		}
	}
	return string(b)
}

func truncateRunes(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	rs := []rune(s)
	return string(rs[:n])
}

// PlainText is an HTML body as readable text (styles and scripts dropped, whitespace collapsed).
func PlainText(html string) string { return strings.Join(strings.Fields(stripHTML(html)), " ") }

var (
	blockTag   = lazyre.New(`(?i)<\s*(br|/p|/div|/ul|/ol|/tr|/h[1-6]|/blockquote|/table)\b[^>]*>`)
	listItem   = lazyre.New(`(?i)<\s*li\b[^>]*>`)
	spaces     = lazyre.New(`[ \t\x{00a0}]+`)
	blankLines = lazyre.New(`\n{3,}`)
)

// ReadableText is an HTML mail as text that keeps its paragraphs and list items (PlainText flattens everything onto
// one line, fine for a preview but not for reading or translating).
func ReadableText(h string) string {
	h = strings.NewReplacer("\r\n", " ", "\n", " ", "\r", " ").Replace(h) // line breaks in the source are only spaces
	h = blockTag.ReplaceAllString(h, "\n$0")
	h = listItem.ReplaceAllString(h, "\n• $0")
	var lines []string
	for _, l := range strings.Split(strings.ReplaceAll(stripHTML(h), "\r", ""), "\n") {
		lines = append(lines, strings.TrimSpace(spaces.ReplaceAllString(l, " ")))
	}
	return strings.TrimSpace(blankLines.ReplaceAllString(strings.Join(lines, "\n"), "\n\n"))
}
