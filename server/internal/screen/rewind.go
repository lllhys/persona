package screen

import (
	"strconv"
	"strings"
	"unicode"

	"github.com/nwverse/persona/server/internal/lazyre"
)

// Claude Code's /rewind (recorded on 2.1.318, testdata/claude-2.1.318-rewind-*): a full-width panel under a ▔ rule,
// in three shapes.
//
//	Rewind                                                    Rewind
//	Restore the code and/or conversation to the point before…  Confirm you want to restore to the point before you sent this message:
//	  ↑ 2 more                                                 │ <the message, wrapped>
//	  <prompt, cut with … when long>                           │ (47s ago)
//	  notes.txt +1 -1  (or: No code changes)                   This message and everything after it will be removed.
//	❯ <prompt>                                                 The code will be restored +1 -1 in notes.txt.
//	  ...                                                    ❯ 1. Restore code and conversation
//	  ↓ 1 more                                                 2. Restore conversation ...
//	❯ (current)                                               Rewind
//	Enter to continue · Esc to cancel                          Nothing to rewind to yet.
//
// The list opens on (current), the row below the last prompt; Up moves to earlier prompts and does not wrap. Which
// choices the second screen offers depends on the prompt: with code changes after it there are "Restore code and
// conversation", "Restore conversation", "Restore code" and two summaries; without, "Restore conversation", two
// summaries and "Never mind". The wording changed between versions (2.1.304: "The conversation will be forked"),
// so only the headings and the choice labels are relied on.

// Screens of /rewind.
const (
	RewindList    = "list"    // the prompts to go back to
	RewindOptions = "options" // what to restore, for the prompt picked
	RewindNothing = "nothing" // "Nothing to rewind to yet."
)

// Labels of the choices of the second screen we press.
const (
	RewindChoiceConversation = "Restore conversation"
	RewindChoiceBoth         = "Restore code and conversation"
)

// RewindRow is a prompt in the list.
type RewindRow struct {
	Text string // as shown: one line, cut with … when it is long
	// Changes is the row under it: "No code changes", "notes.txt +1 -1".
	Changes  string
	Current  bool // the (current) row below the last prompt
	Selected bool // the ❯ is on it
}

// RewindChoice is a choice of the second screen.
type RewindChoice struct {
	Number   int
	Label    string
	Selected bool
}

// RewindScreen is what /rewind shows.
type RewindScreen struct {
	Kind string
	// List: the rows on screen (a window of the prompts) and how many more there are above and below it.
	Rows         []RewindRow
	Above, Below int
	// Options: the message as quoted (wrapped rows joined with a space) and the choices.
	Message string
	Choices []RewindChoice
}

// rewindListQuestion is the line under the list's heading.
const rewindListQuestion = "Restore the code and/or conversation to the point before…"

var (
	rewindHeadRE   = lazyre.New(`^\s{2,4}Rewind\s*$`)
	rewindMoreRE   = lazyre.New(`^([↑↓]) (\d+) more$`)
	rewindChoiceRE = lazyre.New(`^\s{0,4}(❯|↑|↓)?\s*(\d)\.\s+(\S.*)$`)
	rewindAgeRE    = lazyre.New(`^\(.+\)$`)
)

// Selected returns the row the ❯ is on, nil when there is none.
func (r *RewindScreen) Selected() *RewindRow {
	for i := range r.Rows {
		if r.Rows[i].Selected {
			return &r.Rows[i]
		}
	}
	return nil
}

// SelectedChoice returns the choice the ❯ is on, nil when there is none.
func (r *RewindScreen) SelectedChoice() *RewindChoice {
	for i := range r.Choices {
		if r.Choices[i].Selected {
			return &r.Choices[i]
		}
	}
	return nil
}

// Choice returns the choice with exactly this label, nil when the screen has none.
func (r *RewindScreen) Choice(label string) *RewindChoice {
	for i := range r.Choices {
		if r.Choices[i].Label == label {
			return &r.Choices[i]
		}
	}
	return nil
}

// ParseRewind reads the /rewind panel at the bottom of a plain screen, nil when the screen shows none.
func ParseRewind(plain string) *RewindScreen {
	lines := strings.Split(strings.ReplaceAll(plain, "\r", ""), "\n")
	head := -1
	for i := len(lines) - 1; i >= 0; i-- {
		if rewindHeadRE.MatchString(strings.TrimRight(strings.ReplaceAll(lines[i], "\u00a0", " "), " ")) {
			head = i
			break
		}
	}
	if head < 0 {
		return nil
	}
	var body []string
	for _, l := range lines[head+1:] {
		l = strings.TrimRight(strings.ReplaceAll(l, "\u00a0", " "), " ")
		if strings.TrimSpace(l) != "" {
			body = append(body, l)
		}
	}
	if len(body) == 0 {
		return nil
	}
	switch first := strings.TrimSpace(body[0]); {
	case strings.HasPrefix(first, "Nothing to rewind"):
		return &RewindScreen{Kind: RewindNothing}
	case strings.HasPrefix(first, "Restore the code and/or conversation"):
		return parseRewindList(body[1:])
	case strings.HasPrefix(first, "Confirm you want to restore"):
		return parseRewindOptions(body[1:])
	}
	return nil
}

type rewindItem struct {
	text     string
	selected bool
}

func parseRewindList(body []string) *RewindScreen {
	var items []rewindItem
	for _, l := range body {
		t := strings.TrimSpace(l)
		if strings.HasPrefix(t, "Enter to ") || strings.HasPrefix(t, "Esc to ") {
			break
		}
		it := rewindItem{text: t}
		if rest, ok := strings.CutPrefix(strings.TrimLeft(l, " "), "❯"); ok {
			it = rewindItem{text: strings.TrimSpace(rest), selected: true}
		}
		items = append(items, it)
	}
	r := &RewindScreen{Kind: RewindList}
	for i := 0; i < len(items); i++ {
		it := items[i]
		if m := rewindMoreRE.FindStringSubmatch(it.text); m != nil {
			n, _ := strconv.Atoi(m[2])
			if m[1] == "↑" {
				r.Above = n
			} else {
				r.Below = n
			}
			continue
		}
		row := RewindRow{Text: it.text, Selected: it.selected}
		if it.text == "(current)" {
			row.Current = true
		} else if i+1 < len(items) && !items[i+1].selected && !rewindMoreRE.MatchString(items[i+1].text) {
			row.Changes = items[i+1].text
			i++
		}
		r.Rows = append(r.Rows, row)
	}
	return r
}

func parseRewindOptions(body []string) *RewindScreen {
	r := &RewindScreen{Kind: RewindOptions}
	var quote []string
	for _, l := range body {
		t := strings.TrimSpace(l)
		if rest, ok := strings.CutPrefix(t, "│"); ok {
			quote = append(quote, strings.TrimSpace(rest))
			continue
		}
		if m := rewindChoiceRE.FindStringSubmatch(l); m != nil {
			n, _ := strconv.Atoi(m[2])
			r.Choices = append(r.Choices, RewindChoice{Number: n, Label: strings.TrimSpace(m[3]), Selected: m[1] == "❯"})
		}
	}
	// The last quoted row is how long ago it was sent: "(47s ago)".
	if len(quote) > 1 && rewindAgeRE.MatchString(quote[len(quote)-1]) {
		quote = quote[:len(quote)-1]
	}
	r.Message = strings.Join(quote, " ")
	return r
}

// squash drops every space: a line wrap in the middle of a word, or between CJK characters, makes the shown text
// differ from the prompt only in spaces.
func squash(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) {
			return -1
		}
		return r
	}, s)
}

// RewindShows says whether text as /rewind shows a prompt is that prompt: equal up to spaces, or — when it ends with
// … because it was cut — the start of it.
func RewindShows(shown, prompt string) bool {
	s, p := squash(shown), squash(prompt)
	if s == "" || p == "" {
		return false
	}
	if cut, ok := strings.CutSuffix(s, "…"); ok {
		return cut != "" && strings.HasPrefix(p, cut)
	}
	return s == p
}

// RewindDraftIs says whether the text in Claude's input box is the prompt that was put back there (equal up to
// spaces), or a paste / image placeholder standing for part of it.
func RewindDraftIs(draft, prompt string) bool {
	d, p := squash(draft), squash(prompt)
	if d == "" {
		return false
	}
	return d == p || placeholderRE.MatchString(draft)
}
