package kb

import (
	"strings"
	"testing"
)

// A reader that writes to other people gets a person's facts and commitments, never the user's words about them
// nor the guesses: in summary.md as kb_get shows it, and among search hits, which skip them without using up the limit.
func TestPrivateProfileLines(t *testing.T) {
	e := editEngine(t)
	_ = e.Store.PutPerson(&Person{ID: "p1", Name: "Example Person"})
	extApply(t, e, "people", KindPerson, "p1", Update{Profile: sampleProfile("leads the example project")})
	sum := e.Store.Summary(KindPerson, "p1")
	shared := WithoutPrivateProfile(sum)
	for _, gone := range []string{"explains things clearly", "prefers mornings", "replies before 10am", "用户说过的评价", "推断"} {
		if strings.Contains(shared, gone) {
			t.Errorf("%q left in\n%s", gone, shared)
		}
	}
	for _, kept := range []string{"## 画像（扩展 people整理", "### 事实\n- [事实] leads the example project",
		"### 承诺和待办\n- [承诺·用户答应对方，截止 2026-11-20] send the review notes"} {
		if !strings.Contains(shared, kept) {
			t.Errorf("%q dropped\n%s", kept, shared)
		}
	}
	if got := WithoutPrivateProfile("no profile here\n- [事实] x"); got != "no profile here\n- [事实] x" {
		t.Errorf("text without private lines changed: %q", got)
	}

	// Every line of the profile has "（出处：": one hit per part, the private ones passed over.
	all := e.Search("出处", false, 2)
	if len(all) != 2 {
		t.Fatalf("unfiltered hits: %+v", all)
	}
	hits := e.Search("出处", false, 2, PrivateProfileLine)
	if len(hits) != 2 || !strings.Contains(hits[0].Line, "[事实]") || !strings.Contains(hits[1].Line, "[承诺·") {
		t.Fatalf("filtered hits: %+v", hits)
	}
	if hits := e.SearchKind("prefers mornings", KindPerson, false, 10, PrivateProfileLine); len(hits) != 0 {
		t.Fatalf("an inference found by kind: %+v", hits)
	}
	if hits := e.SearchKind("prefers mornings", KindPerson, false, 10); len(hits) != 1 {
		t.Fatalf("the full reader lost the inference: %+v", hits)
	}
}

// A profile edited by hand into several lines is still shown one line an item, so no part of a view or an inference
// is left on a line without its label.
func TestPrivateProfileHandEditedLines(t *testing.T) {
	out := renderProfile("user", storedProfile{Profile: PersonProfile{
		Views:      []ProfileItem{{Text: "first\nsecond", Sources: []string{"user"}}},
		Inferences: []ProfileItem{{Text: "guess", Basis: "one\ntwo", Sources: []string{"a\nb"}}},
	}})
	if shared := WithoutPrivateProfile(out); strings.Contains(shared, "second") || strings.Contains(shared, "two") || strings.Contains(shared, "b）") {
		t.Fatalf("a private line left:\n%s", shared)
	}
}
