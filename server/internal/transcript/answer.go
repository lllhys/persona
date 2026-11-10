package transcript

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// A turn's answer is every block of words the agent addressed to the user in it, in order, from the first: a report
// written before a tool call is part of it, not only the line after the last call (a long report, then agent_send,
// then "sent it" showed only "sent it" in the push and the Lark answer). The one kind of block left out is a passing
// line said right before a tool call ("我先查一下", "Let me check the calendar."): Narration. The last block is always
// kept. One rule for the hub's push body (Meta.TurnAnswer) and the Lark bot's answer (channel.Answer).

// AnswerPart is one block of an agent's words in a turn, and whether a tool call came after it before its next words.
type AnswerPart struct {
	Text       string
	BeforeTool bool
}

// narrationMost is the most runes a passing line before a tool call has; longer words are addressed to the user.
const narrationMost = 120

// narrationSentences is the most sentences a passing line has.
const narrationSentences = 2

// AnswerParts is which of a turn's parts make its answer, by index, in order: all but the narration before a tool
// call (Narration); the last part always.
func AnswerParts(parts []AnswerPart) []int {
	var keep []int
	for i, p := range parts {
		if strings.TrimSpace(p.Text) == "" {
			continue
		}
		if i == len(parts)-1 || !p.BeforeTool || !Narration(p.Text) {
			keep = append(keep, i)
		}
	}
	return keep
}

// Narration reports whether text, said right before a tool call, is a passing line rather than words for the user:
// one paragraph of at most two sentences and narrationMost runes, with no Markdown structure (heading, list, table,
// quote, code).
func Narration(text string) bool {
	t := strings.TrimSpace(text)
	if t == "" || utf8.RuneCountInString(t) > narrationMost || strings.Contains(t, "\n\n") {
		return false
	}
	for line := range strings.SplitSeq(t, "\n") {
		if structured(strings.TrimSpace(line)) {
			return false
		}
	}
	return sentences(t) <= narrationSentences
}

// structured: a Markdown line that is more than a sentence (heading, list item, table row, quote, code fence, a bold
// lead-in, a "(1)" item).
func structured(line string) bool {
	for _, p := range []string{"#", "- ", "* ", "+ ", "> ", "|", "```", "~~~", "**", "__", "（", "("} {
		if strings.HasPrefix(line, p) {
			return true
		}
	}
	digits := strings.TrimLeftFunc(line, unicode.IsDigit)
	return len(digits) < len(line) && (strings.HasPrefix(digits, ". ") || strings.HasPrefix(digits, ") "))
}

// sentences counts the sentences in t: runs of sentence-ending marks (a "." only before a space or the end, so
// "v1.2" and "example.com" are one word), plus the words after the last one.
func sentences(t string) int {
	n, open := 0, false
	rs := []rune(t)
	for i, r := range rs {
		end := strings.ContainsRune("。！？!?；;…", r) || r == '.' && (i == len(rs)-1 || unicode.IsSpace(rs[i+1]))
		switch {
		case end && open:
			n++
			open = false
		case !end && !unicode.IsSpace(r):
			open = true
		}
	}
	if open {
		n++
	}
	return n
}

// turnPartMost bounds a part kept for TurnAnswer: the push body it feeds shows a couple of hundred runes.
const turnPartMost = 4 << 10

// turnSaid notes a block of the agent's words in the latest turn.
func (m *Meta) turnSaid(t string) {
	if strings.TrimSpace(t) == "" {
		return
	}
	if len(t) > turnPartMost {
		t = clipTo(t, turnPartMost)
	}
	m.turnParts = append(m.turnParts, AnswerPart{Text: t})
}

// turnTool notes a tool call in the latest turn: the words before it may be narration.
func (m *Meta) turnTool() {
	if n := len(m.turnParts); n > 0 {
		m.turnParts[n-1].BeforeTool = true
	}
}

// TurnAnswer is the latest turn's answer as read (AnswerParts), its parts joined by a blank line. LastText when the
// lines read held none of its words, or when the two disagree (a turn gone back over, Tail.Settle): it is what stands.
func (m *Meta) TurnAnswer() string {
	n := len(m.turnParts)
	if n == 0 || m.turnParts[n-1].Text != clipTo(m.LastText, turnPartMost) {
		return m.LastText
	}
	keep := AnswerParts(m.turnParts)
	out := make([]string, 0, len(keep))
	for _, i := range keep {
		out = append(out, strings.TrimSpace(m.turnParts[i].Text))
	}
	return strings.Join(out, "\n\n")
}
