package textfmt

import (
	"strconv"
	"strings"
	"unicode/utf8"

	"golang.org/x/net/html"

	"github.com/nwverse/persona/sdk/go/wire"

	"github.com/nwverse/persona/server/internal/lazyre"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
)

var plainParser = goldmark.New(goldmark.WithExtensions(extension.GFM)).Parser()

// What a clip can leave behind with nothing to pair with: emphasis and strike-through runs, backticks.
var danglingMarks = lazyre.New("\\*{2,}|_{2,}|~{2,}|`+")

// PlainMarkdown is md without its markup, for places that show text as it is (a push, a one-line preview): a line per
// block, emphasis and code without their marks, a link as its text. Marks a clip left unpaired are dropped too.
func PlainMarkdown(md string) string { return plain(md, true, false) }

// SearchMarkdown keeps visible words inside HTML folds (such as details) as
// well as ordinary Markdown. Unlike PushText it does not reinterpret flattened
// tables. Scripts, styles, attributes and comments are not searchable prose.
// PlainMarkdown's existing preview/title behavior remains unchanged.
func SearchMarkdown(md string) string { return plain(md, true, true) }

// PushText is PlainMarkdown for a finished turn's push, whose text node already flattened to one line (snippet): a
// table run into one line says its cells ("item · status · build · passed"), and an answer folded in an HTML block
// (<details>) says its words, not nothing (#4462). Only the push reads text this way: a plan's title (PlanTitle) and
// other one-line views keep PlainMarkdown's.
func PushText(md string) string {
	lines := strings.Split(plain(oneLineTables(md), true, true), "\n")
	for i, l := range lines {
		lines[i] = strings.Trim(l, " ·") // a table's cell next to an HTML block's tag leaves a separator at an end
	}
	return strings.Join(lines, "\n")
}

var (
	// A table's delimiter row inside a line: at least two cells of three dashes or more (| --- | :-: |).
	tableDelimiter = lazyre.New(`\|(\s*:?-{3,}:?\s*\|){2,}`)
	codeSpan       = lazyre.New("`+[^`]*`+")
)

// oneLineTables rewrites each line of md that holds a whole table (its rows run together) as its cells joined by
// " · ". Code spans are left out of both the check and the split: `|---|` in one is its text. Nothing is substituted
// into the text, so whatever it holds comes back as it was.
func oneLineTables(md string) string {
	lines := strings.Split(md, "\n")
	for i, l := range lines {
		// bare is l with its code spans' bytes blanked (same length, so its indexes are l's).
		bare := []byte(l)
		for _, r := range codeSpan.FindAllStringIndex(l, -1) {
			for k := r[0]; k < r[1]; k++ {
				bare[k] = 'x'
			}
		}
		delims := tableDelimiter.FindAllIndex(bare, -1)
		if len(delims) == 0 {
			continue
		}
		var cells []string
		start := 0
		cut := func(end int) {
			if c := strings.TrimSpace(l[start:end]); c != "" {
				cells = append(cells, c)
			}
		}
		for k, d := 0, 0; k < len(bare); k++ {
			if d < len(delims) && k == delims[d][0] { // a delimiter row is one cell boundary
				cut(k)
				k, start, d = delims[d][1]-1, delims[d][1], d+1
				continue
			}
			if bare[k] == '|' {
				cut(k)
				start = k + 1
			}
		}
		cut(len(l))
		if len(cells) < 2 {
			continue // a table's own delimiter row on its line: the parser reads that table as one
		}
		lines[i] = strings.Join(cells, " · ")
	}
	return strings.Join(lines, "\n")
}

// htmlText is an HTML fragment's visible words. hidden carries an inline script or style across Markdown AST nodes:
// Goldmark represents its opening and closing tags separately, with the content parsed as ordinary Markdown.
func htmlText(block string, hidden *string) string {
	// A fragment can start inside an element opened in an earlier AST node. Restore tokenizer raw-text mode so,
	// for example, a closing </script> inside what Markdown calls an HTML comment still closes the HTML script.
	prefix := ""
	if *hidden != "" {
		prefix = "<" + *hidden + ">"
	}
	z := html.NewTokenizer(strings.NewReader(prefix + block))
	if prefix != "" {
		z.Next() // the synthetic opener only restores the tokenizer's state
	}
	var b strings.Builder
	for {
		switch z.Next() {
		case html.ErrorToken:
			return b.String()
		case html.TextToken:
			if *hidden == "" {
				b.Write(z.Text())
				b.WriteByte(' ')
			}
		case html.StartTagToken, html.SelfClosingTagToken:
			name, _ := z.TagName()
			if *hidden == "" && (string(name) == "script" || string(name) == "style") {
				*hidden = string(name) // in HTML, even <script/> begins a non-void element
			}
			b.WriteByte(' ')
		case html.EndTagToken:
			name, _ := z.TagName()
			if string(name) == *hidden {
				*hidden = ""
			} else if *hidden == "" {
				b.WriteByte(' ')
			}
		}
	}
}

// plain is md as plain text; dropDangling also drops emphasis marks and backticks left over after the parse, which a
// clip leaves unpaired but which in an uncut text are its content (__init__.py, a**b).
func plain(md string, dropDangling, htmlWords bool) string {
	md = wire.AssistantCardText(md)
	src := []byte(md)
	var b strings.Builder
	hidden := ""
	_ = ast.Walk(plainParser.Parse(text.NewReader(src)), func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		switch n := n.(type) {
		case *ast.Text:
			if entering && hidden == "" {
				if p := n.Parent(); p != nil && p.Kind() == ast.KindCodeSpan {
					b.Write(n.Segment.Value(src)) // code is its source text
				} else {
					writeResolved(&b, n.Segment.Value(src))
				}
				if n.SoftLineBreak() || n.HardLineBreak() {
					b.WriteByte(' ')
				}
			}
		case *ast.String:
			if entering && hidden == "" {
				b.Write(n.Value)
			}
		case *ast.AutoLink:
			if entering && hidden == "" {
				b.Write(n.URL(src))
			}
			return ast.WalkSkipChildren, nil
		case *ast.FencedCodeBlock, *ast.CodeBlock:
			if entering && hidden == "" {
				lines := n.Lines()
				for i := 0; i < lines.Len(); i++ {
					seg := lines.At(i)
					b.Write(seg.Value(src))
				}
			}
		case *ast.HTMLBlock:
			if entering && htmlWords {
				var raw strings.Builder
				lines := n.Lines()
				for i := 0; i < lines.Len(); i++ {
					seg := lines.At(i)
					raw.Write(seg.Value(src))
				}
				if n.HasClosure() {
					raw.Write(n.ClosureLine.Value(src))
				}
				b.WriteString(strings.ReplaceAll(htmlText(raw.String(), &hidden), "\n", " "))
				b.WriteByte('\n')
			}
			return ast.WalkSkipChildren, nil
		case *ast.RawHTML:
			if entering && htmlWords {
				wasHidden := hidden != ""
				visible := htmlText(string(n.Segments.Value(src)), &hidden)
				if wasHidden && hidden == "" && strings.TrimSpace(visible) != "" {
					b.WriteString(visible)
				}
			}
			return ast.WalkSkipChildren, nil
		}
		if !entering && n.Type() == ast.TypeBlock {
			b.WriteByte('\n')
		}
		return ast.WalkContinue, nil
	})
	var lines []string
	for _, l := range strings.Split(b.String(), "\n") {
		if dropDangling {
			l = danglingMarks.ReplaceAllString(l, "")
		}
		if l = OneLine(l); l != "" {
			lines = append(lines, l)
		}
	}
	return strings.Join(lines, "\n")
}

// writeResolved writes a text node's source the way a CommonMark renderer does: a backslash before ASCII punctuation
// gives the punctuation, and a character reference (&amp;, &#39;, &#x27;) the character it stands for. An escaped &
// starts no reference; a backslash before anything else, an unknown name and a bare & stay as written.
func writeResolved(b *strings.Builder, src []byte) {
	for i := 0; i < len(src); i++ {
		switch c := src[i]; {
		case c == '\\' && i+1 < len(src) && util.IsPunct(src[i+1]):
			i++
			b.WriteByte(src[i])
		case c == '&':
			if n, r := characterReference(src[i:]); n > 0 {
				b.WriteString(r)
				i += n - 1
			} else {
				b.WriteByte(c)
			}
		default:
			b.WriteByte(c)
		}
	}
}

// characterReference reads the reference at the start of s (which starts with &): its length and the text it stands
// for, or 0 when s starts with none. Like a renderer, it takes decimal references of up to 7 digits, hexadecimal ones
// of up to 6 and the HTML5 entity names; a code point that is no character is U+FFFD.
func characterReference(s []byte) (int, string) {
	if len(s) < 3 {
		return 0, ""
	}
	if s[1] == '#' {
		base, digits, from := 10, util.IsNumeric, 2
		if s[2] == 'x' || s[2] == 'X' {
			base, digits, from = 16, util.IsHexDecimal, 3
		}
		end := from
		for end < len(s) && digits(s[end]) {
			end++
		}
		if n := end - from; n == 0 || n > 7 || base == 16 && n > 6 || end >= len(s) || s[end] != ';' {
			return 0, ""
		}
		v, err := strconv.ParseUint(string(s[from:end]), base, 32)
		if err != nil || v == 0 || !utf8.ValidRune(rune(v)) {
			return end + 1, string(utf8.RuneError)
		}
		return end + 1, string(rune(v))
	}
	end := 1
	for end < len(s) && util.IsAlphaNumeric(s[end]) {
		end++
	}
	if end == 1 || end >= len(s) || s[end] != ';' {
		return 0, ""
	}
	if entity, ok := util.LookUpHTML5EntityByName(string(s[1:end])); ok {
		return end + 1, string(entity.Characters)
	}
	return 0, ""
}

// PlanTitle is what a plan card is called where it shows in one line (a push, the live activity, a row in the Agent
// lists): its first line with text, without the Markdown marks (a heading's #s, emphasis, a list item's bullet or
// checkbox, code spans, a link's target) that would show as they are.
func PlanTitle(plan string) string {
	for _, l := range strings.Split(plan, "\n") {
		if l = plain(l, false, false); l != "" { // a plan is not clipped: what is left of its marks is its text
			return l
		}
	}
	return ""
}
