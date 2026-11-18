package assistant

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/nwverse/persona/server/internal/kb"
	"github.com/nwverse/persona/server/internal/prompts"
)

// kb_get's bounds on what it hands the model (#7316, kb_get_fit.go).

func peopleIDs(n int) []string {
	ids := make([]string, n)
	for i := range ids {
		ids[i] = fmt.Sprintf("lark-%019d", 7000000000000000000+i)
	}
	return ids
}

// metaOf is the meta.json printed in a kb_get result (between "meta.json:" and the first blank line), decoded.
func metaOf(t *testing.T, out string) map[string]json.RawMessage {
	t.Helper()
	_, rest, ok := strings.Cut(out, "meta.json:\n")
	if !ok {
		t.Fatalf("no meta.json:\n%s", out)
	}
	end := strings.Index(rest, "\n}\n")
	if end < 0 {
		t.Fatalf("meta.json does not end:\n%s", out)
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal([]byte(rest[:end+2]), &m); err != nil {
		t.Fatalf("meta.json: %v\n%s", err, out)
	}
	return m
}

// topKeys is the top-level keys of the JSON object raw starts with, in order.
func topKeys(t *testing.T, raw []byte) string {
	t.Helper()
	dec := json.NewDecoder(bytes.NewReader(raw))
	var keys []string
	if _, err := dec.Token(); err != nil {
		t.Fatal(err)
	}
	for dec.More() {
		k, err := dec.Token()
		if err != nil {
			t.Fatal(err)
		}
		var v json.RawMessage
		if err := dec.Decode(&v); err != nil {
			t.Fatal(err)
		}
		keys = append(keys, fmt.Sprint(k))
	}
	return strings.Join(keys, ",")
}

// A project a large group chat was linked to: kb_get shows 20 of its 3,000 people, the ranked ones first, says how many
// there are and where the whole list is, and fits; the file, kb.Engine.Get (what the app and web read) and the rest of
// its meta.json stay as they were.
func TestKBGetFoldsALargeProjectsPeople(t *testing.T) {
	s := newKBService(t, newFakeHub())
	e := kbEng(t, s)
	ids := peopleIDs(3000)
	for _, p := range []*kb.Person{
		{ID: ids[2999], Name: "Example Lead", Tagging: kb.Tagging{Relevance: 90}},
		{ID: ids[1500], Name: "Example Owner", Tagging: kb.Tagging{Importance: "P1"}},
		{ID: ids[7], Name: "Example Member", LastSeen: time.Now()},
	} {
		if err := e.Store.PutPerson(p); err != nil {
			t.Fatal(err)
		}
	}
	if err := e.Store.PutProject(&kb.Project{Slug: "big", Name: "Big project", Kind: "long", Status: "active", Goal: "ship it",
		People: ids, Chats: []string{"lark-group"}, Created: time.Now()}); err != nil {
		t.Fatal(err)
	}
	out, err := runTool(s, "c1", "kb_get", map[string]any{"kind": "projects", "id": "big"})
	if err != nil {
		t.Fatal(err)
	}
	path := e.Store.Path(kb.KindProject, "big")
	m := metaOf(t, out)
	var shown []string
	if err := json.Unmarshal(m["people"], &shown); err != nil {
		t.Fatal(err)
	}
	want := append([]string{ids[1500], ids[2999], ids[7]}, ids[:7]...)
	want = append(want, ids[8:18]...)
	if strings.Join(shown, ",") != strings.Join(want, ",") {
		t.Errorf("people shown:\n%v\nwant\n%v", shown, want)
	}
	note := prompts.RenderKBGetPeople(prompts.KBGetPeople{Shown: 20, Total: 3000, Meta: filepath.Join(path, "meta.json")})
	if !strings.Contains(out, "\n}\n"+note+"\n\n") || !strings.Contains(note, "20 of the project's 3000 people") {
		t.Errorf("no note under meta.json:\n%s", out)
	}
	raw, _ := os.ReadFile(filepath.Join(path, "meta.json"))
	var file map[string]json.RawMessage
	if err := json.Unmarshal(raw, &file); err != nil || len(file) != len(m) {
		t.Fatalf("meta.json's keys: %d, the file's %d (%v)", len(m), len(file), err)
	}
	for k, v := range file {
		var a, b bytes.Buffer
		if k == "people" {
			continue
		}
		if json.Compact(&a, v) != nil || json.Compact(&b, m[k]) != nil || a.String() != b.String() {
			t.Errorf("%s: %s, the file has %s", k, m[k], v)
		}
	}
	if printed, stored := topKeys(t, []byte(out[strings.Index(out, "{"):])), topKeys(t, raw); printed != stored {
		t.Errorf("meta.json's keys reordered: %s, the file has %s", printed, stored)
	}
	if n := estTokens(out); n > 2000 {
		t.Errorf("%d tokens", n)
	}
	var stored, got kb.Project
	if err := json.Unmarshal(raw, &stored); err != nil || len(stored.People) != 3000 {
		t.Errorf("the file changed: %d people, %v", len(stored.People), err)
	}
	d, err := e.Get(kb.KindProject, "big", 12000)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(d.Meta, &got); err != nil || len(got.People) != 3000 {
		t.Errorf("kb.Engine.Get changed: %d people, %v", len(got.People), err)
	}
}

// oldKBGet is a kb_get result for an entry with only its meta.json, as kb_get wrote it before the bounds.
func oldKBGet(t *testing.T, e *kb.Engine, kind, id string) string {
	t.Helper()
	d, err := e.Get(kind, id, 12000)
	if err != nil {
		t.Fatal(err)
	}
	var indented bytes.Buffer
	if err := json.Indent(&indented, bytes.TrimSpace(d.Meta), "", "  "); err != nil {
		t.Fatal(err)
	}
	return fmt.Sprintf("Path: %s\n\nmeta.json:\n%s\n\n", d.Path, indented.Bytes())
}

// A project with up to 20 people reads byte for byte as before; one more and it is folded.
func TestKBGetLeavesASmallProjectAsItWas(t *testing.T) {
	s := newKBService(t, newFakeHub())
	e := kbEng(t, s)
	ids := peopleIDs(21)
	for _, p := range []*kb.Project{
		{Slug: "small", Name: "Small", Kind: "short", Status: "active", People: ids[:20], Created: time.Now()},
		{Slug: "none", Name: "None", Kind: "short", Status: "active", Created: time.Now()},
		{Slug: "edge", Name: "Edge", Kind: "short", Status: "active", People: ids, Created: time.Now()},
	} {
		if err := e.Store.PutProject(p); err != nil {
			t.Fatal(err)
		}
	}
	for _, id := range []string{"small", "none"} {
		out, err := runTool(s, "c1", "kb_get", map[string]any{"kind": "projects", "id": id})
		if want := oldKBGet(t, e, kb.KindProject, id); err != nil || out != want {
			t.Errorf("%s:\n%s\nwant\n%s (%v)", id, out, want, err)
		}
	}
	out, err := runTool(s, "c1", "kb_get", map[string]any{"kind": "projects", "id": "edge"})
	if err != nil || !strings.Contains(out, "20 of the project's 21 people") {
		t.Errorf("21 people:\n%s (%v)", out, err)
	}
}

// Any other entry past the ceiling, a chat with thousands of members here, is cut at a line's end under it, with the
// line saying how much is left out and where the files are; just under it, an entry reads as before.
func TestKBGetCutsAResultPastTheCeiling(t *testing.T) {
	s := newKBService(t, newFakeHub())
	e := kbEng(t, s)
	for _, c := range []*kb.Chat{
		{ID: "lark-big", Source: "lark", SourceID: "big", Kind: "group", Name: "Big group", People: peopleIDs(3000), Created: time.Now()},
		{ID: "lark-small", Source: "lark", SourceID: "small", Kind: "group", Name: "Small group", People: peopleIDs(30), Created: time.Now()},
	} {
		if err := e.Store.PutChat(c); err != nil {
			t.Fatal(err)
		}
	}
	full := oldKBGet(t, e, kb.KindChat, "lark-big")
	out, err := runTool(s, "c1", "kb_get", map[string]any{"kind": "chats", "id": "lark-big"})
	if err != nil {
		t.Fatal(err)
	}
	path := e.Store.Path(kb.KindChat, "lark-big")
	kept, cut, ok := strings.Cut(out, "[kb_get cut here")
	if !ok || !strings.HasSuffix(kept, "\n") || !strings.HasPrefix(full, kept) {
		t.Fatalf("not cut at a line's end:\n%s", out)
	}
	rest := full[len(kept):]
	line := prompts.RenderKBGetCut(prompts.KBGetCut{Chars: utf8.RuneCountInString(rest), Tokens: estTokens(rest), Path: path})
	if "[kb_get cut here"+cut != line+"\n" {
		t.Errorf("cut line %q, want %q", "[kb_get cut here"+cut, line)
	}
	if n := estTokens(out); n > kbGetMaxTokens || n < kbGetMaxTokens-kbGetCutReserve-estTokens(path)-50 {
		t.Errorf("%d tokens after the cut", n)
	}
	if out, err := runTool(s, "c1", "kb_get", map[string]any{"kind": "chats", "id": "lark-small"}); err != nil || out != oldKBGet(t, e, kb.KindChat, "lark-small") {
		t.Errorf("a small chat:\n%s (%v)", out, err)
	}
}

// At the ceiling a result is left as it is; one token past it, it is cut, and what is kept and left out add up to it.
func TestFitKBGetAtTheCeiling(t *testing.T) {
	line := strings.Repeat("中", 78) + "aaaa\n" // 100 tokens
	at := strings.Repeat(line, kbGetMaxTokens/100)
	if estTokens(at) != kbGetMaxTokens || fitKBGet(at, "/data/x") != at {
		t.Fatalf("%d tokens, at the ceiling, changed", estTokens(at))
	}
	over := at + "a"
	got := fitKBGet(over, "/data/x")
	kept, _, ok := strings.Cut(got, "[kb_get cut here")
	if !ok || !strings.HasPrefix(over, kept) || len(kept)%len(line) != 0 {
		t.Fatalf("one past the ceiling:\n%.300s", got)
	}
	rest := over[len(kept):]
	if !strings.HasSuffix(got, prompts.RenderKBGetCut(prompts.KBGetCut{Chars: utf8.RuneCountInString(rest), Tokens: estTokens(rest), Path: "/data/x"})+"\n") {
		t.Errorf("cut line:\n%s", got[len(kept):])
	}
	if estTokens(got) > kbGetMaxTokens {
		t.Errorf("%d tokens after the cut", estTokens(got))
	}
	// One line past the ceiling: cut inside it, at a character.
	long := strings.Repeat("中", kbGetMaxTokens+10)
	if got := fitKBGet(long, "/data/x"); !utf8.ValidString(got) || estTokens(got) > kbGetMaxTokens {
		t.Errorf("one long line: %d tokens", estTokens(got))
	}
	// A long line early (a one-paragraph summary.md) is cut inside, not dropped whole for the line before it: the result
	// keeps nearly the whole budget, and the cut line starts a line of its own.
	early := "Path: /data/x\n\nmeta.json:\n{}\n\nsummary.md:\n" + strings.Repeat("中文字", 10000) + "\n" + strings.Repeat(line, 50)
	got = fitKBGet(early, "/data/x")
	kept, _, ok = strings.Cut(got, "[kb_get cut here")
	if !ok || !utf8.ValidString(got) || !strings.HasSuffix(kept, "\n") || !strings.HasPrefix(early, strings.TrimSuffix(kept, "\n")) {
		t.Fatalf("a long line early:\n%.300s", got)
	}
	rest = early[len(kept)-1:]
	if !strings.HasSuffix(got, prompts.RenderKBGetCut(prompts.KBGetCut{Chars: utf8.RuneCountInString(rest), Tokens: estTokens(rest), Path: "/data/x"})+"\n") {
		t.Errorf("cut line:\n%s", got[len(kept):])
	}
	if n := estTokens(got); n > kbGetMaxTokens || n < kbGetMaxTokens-kbGetCutReserve-10 {
		t.Errorf("a long line early: %d tokens after the cut", n)
	}
}

// The estimate counts a CJK character as 1.25 tokens and two ASCII bytes as one, rounding up, so a CJK result is not
// taken for a third of its size.
func TestEstTokensCountsCJK(t *testing.T) {
	for s, want := range map[string]int{
		"": 0, "a": 1, "ab": 1, "abc": 2, "中": 2, "中文": 3, "ab中": 3, "「项目」": 5, "周五灰度 ok": 7,
		strings.Repeat("知", 10000): 12500, strings.Repeat("x", 10000): 5000,
		`    "lark-7000000000000000000",` + "\n": 16,
	} {
		if got := estTokens(s); got != want {
			t.Errorf("estTokens(%.20q) = %d, want %d", s, got, want)
		}
	}
}

func TestGoldenKBGetLines(t *testing.T) {
	golden(t, "kb_get_people", prompts.RenderKBGetPeople(prompts.KBGetPeople{Shown: 20, Total: 3708,
		Meta: "/home/me/.persona/data/projects/sample-project/meta.json"}))
	golden(t, "kb_get_cut", prompts.RenderKBGetCut(prompts.KBGetCut{Chars: 58231, Tokens: 29116,
		Path: "/home/me/.persona/data/chats/lark-sample"}))
}
