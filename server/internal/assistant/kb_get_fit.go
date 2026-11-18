package assistant

import (
	"bytes"
	"encoding/json"
	"strings"
	"unicode/utf8"

	"github.com/nwverse/persona/server/internal/kb"
	"github.com/nwverse/persona/server/internal/prompts"
)

// Two bounds on what kb_get hands the model (#7316), both only in this tool's text: kb.Engine.Get, the store and the
// app's and web's detail pages are left as they are. A project linked to a large group chat takes in all its members
// (thousands of ids, over nine tenths of its kb_get), so its people are folded to the first few; and the whole result is
// cut below the size at which Claude Code refuses an MCP tool's output.

const (
	// kbGetPeople is how many of a project's people kb_get shows: 20 ids are about 300 tokens.
	kbGetPeople = 20
	// kbGetMaxTokens is the ceiling on a kb_get result, in estTokens. Claude Code refuses an MCP tool's output past
	// 25,000 tokens (MAX_MCP_OUTPUT_TOKENS, which Persona does not set); estTokens ran up to 15% under the real count
	// on this kind of text, and the ceiling allows at most 40,000 ASCII characters.
	kbGetMaxTokens = 20000
	// kbGetCutReserve is what the cut line takes of the ceiling, its path aside.
	kbGetCutReserve = 100
	// kbGetCutSlack is how far back, in bytes, a cut moves to end on a whole line: about 1,000 tokens at most.
	kbGetCutSlack = 2000
)

// estTokens is a conservative token count for text a tool returns: a rune past ASCII (CJK text, full-width
// punctuation) as 1.25 tokens, an ASCII byte as half of one. Measured with Claude's tokenizer on knowledge-base text,
// it was at most 15% under the real count: CJK alone is 1.08 tokens a character, Chinese mixed with English words,
// digits and markdown costs more, and indented JSON is 1.8 characters a token. One token a CJK character and 3.5 ASCII
// characters a token put the same text at two thirds of its real count.
func estTokens(s string) int {
	n := 0
	for _, r := range s {
		n += runeQuarters(r)
	}
	return (n + 3) / 4
}

// runeQuarters is what r counts for in estTokens, in quarters of a token.
func runeQuarters(r rune) int {
	if r < utf8.RuneSelf {
		return 2
	}
	return 5
}

// foldPeople cuts the top-level "people" array of a project's meta.json to kbGetPeople ids: first those the detail's
// links rank (importance, relevance, then last seen: kb.Rank), then the rest in stored order. It returns the line saying
// how many there are and where the whole list is (metaPath), or meta as it is and "" when the project has no more people
// than that, or meta.json is not an object with a list of ids.
func foldPeople(meta []byte, links []kb.Entry, metaPath string) ([]byte, string) {
	var m struct {
		People []string `json:"people"`
	}
	if json.Unmarshal(meta, &m) != nil || len(m.People) <= kbGetPeople {
		return meta, ""
	}
	listed := make(map[string]bool, len(m.People))
	for _, id := range m.People {
		listed[id] = true
	}
	shown := make([]string, 0, kbGetPeople)
	taken := map[string]bool{}
	take := func(id string) {
		if len(shown) < kbGetPeople && listed[id] && !taken[id] {
			shown = append(shown, id)
			taken[id] = true
		}
	}
	for _, l := range links {
		if l.Kind == kb.KindPerson {
			take(l.ID)
		}
	}
	for _, id := range m.People {
		take(id)
	}
	short, err := json.Marshal(shown)
	if err != nil {
		return meta, ""
	}
	out, ok := replaceTopKey(meta, "people", short)
	if !ok {
		return meta, ""
	}
	return out, prompts.RenderKBGetPeople(prompts.KBGetPeople{Shown: len(shown), Total: len(m.People), Meta: metaPath}) + "\n"
}

// replaceTopKey is obj, a JSON object, with the value of its top-level key replaced by value and every other byte as
// it was (so the indented meta.json keeps its keys' order).
func replaceTopKey(obj []byte, key string, value []byte) ([]byte, bool) {
	dec := json.NewDecoder(bytes.NewReader(obj))
	if t, err := dec.Token(); err != nil || t != json.Delim('{') {
		return nil, false
	}
	var out []byte
	last := 0
	for dec.More() {
		k, err := dec.Token()
		if err != nil {
			return nil, false
		}
		var v json.RawMessage
		if err := dec.Decode(&v); err != nil {
			return nil, false
		}
		if k != key {
			continue
		}
		end := int(dec.InputOffset())
		start := end - len(v)
		if start < last || !bytes.Equal(obj[start:end], v) {
			return nil, false
		}
		out = append(append(out, obj[last:start]...), value...)
		last = end
	}
	if last == 0 {
		return nil, false
	}
	return append(out, obj[last:]...), true
}

// fitKBGet is out, a kb_get result, as it is when it is within kbGetMaxTokens; else cut at the end of the last line
// that fits, or inside that line at a character when it began more than kbGetCutSlack back (one long line, a paragraph
// or a meta.json edited into one line, must not take the budget with it), with the line saying how much was left out
// and that the rest is in the files at path.
func fitKBGet(out, path string) string {
	if estTokens(out) <= kbGetMaxTokens {
		return out
	}
	budget := 4 * (kbGetMaxTokens - kbGetCutReserve - estTokens(path))
	end := 0
	for i, r := range out {
		if budget -= runeQuarters(r); budget < 0 {
			end = i
			break
		}
	}
	if nl := strings.LastIndexByte(out[:end], '\n'); nl >= 0 && end-(nl+1) <= kbGetCutSlack {
		end = nl + 1
	}
	kept, rest := out[:end], out[end:]
	if kept != "" && !strings.HasSuffix(kept, "\n") {
		kept += "\n"
	}
	return kept + prompts.RenderKBGetCut(prompts.KBGetCut{Chars: utf8.RuneCountInString(rest), Tokens: estTokens(rest), Path: path}) + "\n"
}
