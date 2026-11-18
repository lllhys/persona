package node

import (
	"path/filepath"
	"strings"
	"unicode"
	"unicode/utf16"

	"github.com/nwverse/persona/server/internal/proto"
)

// A runtime's title is its own word (#8133). Paseo titles an agent nobody named after its first prompt and keeps any
// title it was given: a rename in its app, the title another agent created it with (create_agent), Persona's rename.
// Its record says nothing of which one a title is, but a title it derived is exactly what its create-agent-title.js
// makes of the prompt, the test Paseo's own workspace naming uses to tell its derived titles from given ones. So the
// row of a runtime session whose conversation is a transcript here counts its title as the first prompt (title_source
// "prompt", which Persona's naming may replace) only when it is that; any other stays of unknown provenance, which
// nothing replaces.

// paseoTitleMax is the most UTF-16 code units of a title Paseo derives from a prompt (MAX_INITIAL_AGENT_TITLE_CHARS).
const paseoTitleMax = 60

// runtimePromptTitle reports whether title, a runtime session's (its row's own, cwd its folder), is its first prompt as
// Paseo titles an agent after it, the prompt as the node reads it (m.first: one that opens with one of Persona's
// context blocks is titled after the block's first line), or the folder name the bridge stands in with for an agent
// Paseo has not titled.
func runtimePromptTitle(title, cwd string, m quickMeta) bool {
	if title == "" {
		return false
	}
	return title == paseoPromptTitle(m.first) || cwd != "" && title == filepath.Base(cwd)
}

// paseoPromptTitle is the title Paseo gives an agent from its first prompt when nobody named it (deriveInitialAgentTitle):
// the first line with anything but white space on it, its white space runs one space each, at most paseoTitleMax
// UTF-16 code units, trimmed. White space is JavaScript's. A cut inside a character outside the BMP keeps the first
// half of its surrogate pair, which reaches Go as U+FFFD. "" for a prompt without such a line.
func paseoPromptTitle(prompt string) string {
	var line string
	for l := range strings.SplitSeq(prompt, "\n") {
		if l = strings.TrimFunc(l, jsSpace); l != "" {
			line = l
			break
		}
	}
	var b strings.Builder
	units, space := 0, false
	for _, r := range line {
		if jsSpace(r) {
			space = true
			continue
		}
		if space {
			if units == paseoTitleMax {
				break
			}
			b.WriteByte(' ')
			units++
			space = false
		}
		n := utf16.RuneLen(r)
		if n < 0 {
			n = 1
		}
		if units+n > paseoTitleMax {
			if units < paseoTitleMax {
				b.WriteRune(unicode.ReplacementChar)
			}
			break
		}
		b.WriteRune(r)
		units += n
	}
	return strings.TrimRightFunc(b.String(), jsSpace)
}

// jsSpace is JavaScript's white space (\s, String.prototype.trim): Go's, without U+0085 and with U+FEFF.
func jsSpace(r rune) bool {
	return r == '\uFEFF' || r != '\u0085' && unicode.IsSpace(r)
}

// runtimeTitleSource is s's title provenance once it is known whether its title is the first prompt (setRuntimeContext):
// the transcript's word for it unless that is "prompt", which holds only for a title that is that prompt.
func runtimeTitleSource(s *proto.Session, m quickMeta) {
	if s.TitleSource == proto.TitleSourcePrompt && !runtimePromptTitle(s.Title, s.Cwd, m) {
		s.TitleSource = proto.TitleSourceUnknown
	}
}
