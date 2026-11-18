package transcript

import (
	"encoding/json"
	"strings"
	"unicode/utf8"

	"github.com/nwverse/persona/server/internal/proto"
)

// foldSourceTitleMax bounds a source's title, in characters.
const foldSourceTitleMax = 200

// foldSources is the web pages a run's successful web calls used, so a reply shows its sources whether its work came
// folded or not (web/src/lib/turn-sources.ts reads the same calls; both read spec/fixtures/fold-sources). The pages it
// opened (WebFetch, and Codex's open page, which comes as one) come first, then the results its searches returned
// (WebSearch: Claude Code's "Links: [...]" line, Codex's "title  url" lines), each URL once, a later mention giving
// one without a title its title, at most proto.FoldSourcesMax. Only http(s) URLs count. Nil when there are none.
func foldSources(run []proto.Item) []proto.FoldSource {
	results := map[string]proto.Item{}
	for _, it := range run {
		if it.Kind == proto.KindToolResult && it.ToolUseID != "" && !it.IsError {
			results[it.ToolUseID] = it
		}
	}
	var out []proto.FoldSource
	at := map[string]int{}
	add := func(url, title string) {
		url, title = strings.TrimSpace(url), sourceTitle(title)
		if !webURL(url) {
			return
		}
		if i, seen := at[url]; seen {
			if out[i].Title == "" {
				out[i].Title = title
			}
			return
		}
		at[url] = len(out)
		out = append(out, proto.FoldSource{URL: url, Title: title})
	}
	calls := func(tool string) []proto.Item {
		var list []proto.Item
		for _, it := range run {
			if it.Kind != proto.KindToolUse || it.ToolName != tool {
				continue
			}
			if _, ok := results[it.ToolUseID]; ok {
				list = append(list, it)
			}
		}
		return list
	}
	for _, it := range calls("WebFetch") {
		var in struct {
			URL string `json:"url"`
		}
		if json.Unmarshal(it.ToolInput, &in) == nil {
			add(in.URL, "")
		}
	}
	for _, it := range calls("WebSearch") {
		for _, s := range searchResults(results[it.ToolUseID]) {
			add(s.URL, s.Title)
		}
	}
	if len(out) > proto.FoldSourcesMax {
		out = out[:proto.FoldSourcesMax]
	}
	return out
}

// searchResults is the pages a web search's result names. In a result the hub cut (FullSize, its note at the end) a
// "Links:" list keeps its entries before the cut one, and a Codex list loses its last line, which may be cut.
func searchResults(res proto.Item) []proto.FoldSource {
	text := res.Text
	cut := false
	if i := strings.LastIndex(text, "\n…（已截断"); res.FullSize > 0 && i >= 0 {
		text, cut = text[:i], true
	}
	lines := strings.Split(text, "\n")
	var out []proto.FoldSource
	links := false
	for _, line := range lines {
		if rest, ok := strings.CutPrefix(line, "Links: "); ok {
			links = true
			out = append(out, linksList(rest)...)
		}
	}
	if links {
		return out
	}
	// Codex: one "title  url" line each (transcript/codex.go codexWebSearch)
	if cut {
		lines = lines[:len(lines)-1]
	}
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if webURL(line) {
			out = append(out, proto.FoldSource{URL: line})
			continue
		}
		if i := strings.LastIndex(line, "  "); i >= 0 {
			out = append(out, proto.FoldSource{URL: strings.TrimSpace(line[i+2:]), Title: line[:i]})
		}
	}
	return out
}

// linksList is the complete {"title","url"} entries of a JSON array, read until one is cut or broken.
func linksList(s string) []proto.FoldSource {
	dec := json.NewDecoder(strings.NewReader(s))
	if tok, err := dec.Token(); err != nil || tok != json.Delim('[') {
		return nil
	}
	var out []proto.FoldSource
	for dec.More() {
		var e struct {
			Title string `json:"title"`
			URL   string `json:"url"`
		}
		if dec.Decode(&e) != nil {
			break
		}
		out = append(out, proto.FoldSource{URL: e.URL, Title: e.Title})
	}
	return out
}

// webURL: an http(s) URL with something after the scheme and no white space.
func webURL(s string) bool {
	l := strings.ToLower(s)
	rest, ok := strings.CutPrefix(l, "https://")
	if !ok {
		rest, ok = strings.CutPrefix(l, "http://")
	}
	return ok && rest != "" && !strings.ContainsAny(s, " \t\r\n")
}

// sourceTitle is a title trimmed, at most foldSourceTitleMax characters (an ellipsis ending a cut one).
func sourceTitle(s string) string {
	s = strings.TrimSpace(s)
	if utf8.RuneCountInString(s) <= foldSourceTitleMax {
		return s
	}
	r := []rune(s)
	return string(r[:foldSourceTitleMax-1]) + "…"
}
