package extension

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/nwverse/persona/sdk/go/wire"
	"github.com/nwverse/persona/server/internal/i18n"
)

func v8Example(t *testing.T, name string) *View {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "schema", "extension", "examples", "View.v8-"+name+".json"))
	if err != nil {
		t.Fatal(err)
	}
	v, err := ParseView(raw)
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	return v
}

var v8Examples = []string{"today", "section-header"}

// An app of UI version 8 gets every example as the extension drew it. Every older one gets a stack with none of
// version 8's section fields, each section's link as its last row with the link's action, and nothing else changed by
// this version: the same components, in the same order, before that row.
func TestFitV8Examples(t *testing.T) {
	page, _ := Slot("nav_page")
	for _, name := range v8Examples {
		v := v8Example(t, name)
		want, _ := json.Marshal(v)
		if dropped, err := Fit(v, page, 8); err != nil || len(dropped) > 0 {
			t.Fatalf("%s for 8: dropped %v, %v", name, dropped, err)
		}
		if got, _ := json.Marshal(v); string(got) != string(want) {
			t.Errorf("%s: an app of 8 gets it changed\nwant %s\n got %s", name, want, got)
		}
		for version := 1; version < 8; version++ {
			drawn := v8Example(t, name)
			v := v8Example(t, name)
			if _, err := Fit(v, page, version); err != nil {
				t.Fatalf("%s for %d: %v", name, version, err)
			}
			if v.Layout == wire.LayoutColumns {
				t.Errorf("%s for %d: layout %q left", name, version, v.Layout)
			}
			if len(v.Sections) != len(drawn.Sections) {
				t.Fatalf("%s for %d: %d sections of %d", name, version, len(v.Sections), len(drawn.Sections))
			}
			for i, s := range v.Sections {
				d := drawn.Sections[i]
				if s.Span != "" || s.Icon != "" || s.IconColor != "" || s.Link != nil || s.Columns != 0 {
					t.Errorf("%s for %d: section %d keeps a field of 8: %+v", name, version, i, s)
				}
				if s.Title != d.Title {
					t.Errorf("%s for %d: section %d titled %q, not %q", name, version, i, s.Title, d.Title)
				}
				if d.Link == nil {
					continue
				}
				last := s.Items[len(s.Items)-1]
				if last.Type != "row" || last.Title != d.Link.Title || last.Action == nil || last.Action.ID != d.Link.Action.ID {
					t.Errorf("%s for %d: section %d ends with %+v, not its link %+v", name, version, i, last, d.Link)
				}
				if version >= 6 {
					if got, want := len(s.Items), len(d.Items)+1; got != want {
						t.Errorf("%s for %d: section %d has %d components, want %d", name, version, i, got, want)
					}
					if version >= 3 && !jsonEqual(last.Action, &d.Link.Action) {
						t.Errorf("%s for %d: the link's action changed: %+v", name, version, last.Action)
					}
				}
			}
		}
	}
}

func jsonEqual(a, b any) bool {
	x, _ := json.Marshal(a)
	y, _ := json.Marshal(b)
	return string(x) == string(y)
}

// A section's link is a row only where an older app could press it on a page: a card and a widget open a page of
// their own, a native stream's events go back by the source's node keys, and a page never goes past ItemsMost.
func TestFitV8LinkOnlyOnAPage(t *testing.T) {
	linked := func() *View {
		return &View{Layout: wire.LayoutColumns, Sections: []Section{{
			Key: "s", Title: "Calendar", Icon: "calendar", IconColor: "red", Span: wire.SectionSpanFull, Columns: 2,
			Link:  &SectionLink{Title: "Calendar", Action: UIAction{ID: "calendar"}},
			Items: []UIElem{{Type: "text", Key: "t", Text: "3 events today"}},
		}}}
	}
	rows := func(v *View) int {
		n := 0
		for _, e := range items(v) {
			if e.Type == "row" {
				n++
			}
		}
		return n
	}
	for _, name := range []string{"nav_page", "extension_page", "machine_section", "launcher_section"} {
		slot, _ := Slot(name)
		v := linked()
		if _, err := Fit(v, slot, 7); err != nil || rows(v) != 1 {
			t.Errorf("%s: %d link rows, %v", name, rows(v), err)
		}
	}
	card, _ := Slot("overview_card")
	v := linked()
	if _, err := Fit(v, card, 7); err != nil || rows(v) != 0 || v.Sections[0].Link != nil {
		t.Errorf("a card: %d link rows, link %+v, %v", rows(v), v.Sections[0].Link, err)
	}
	v = linked()
	if _, err := FitWidget(v, "medium", 8); err != nil || rows(v) != 0 || v.Sections[0].Link != nil {
		t.Errorf("a widget: %d link rows, link %+v, %v", rows(v), v.Sections[0].Link, err)
	}

	// a page already at ItemsMost components keeps them all and gets no row
	full := linked()
	for len(full.Sections[0].Items) < ItemsMost {
		full.Sections[0].Items = append(full.Sections[0].Items, UIElem{Type: "divider"})
	}
	page, _ := Slot("nav_page")
	if _, err := Fit(full, page, 7); err != nil || rows(full) != 0 || len(full.Sections[0].Items) != ItemsMost {
		t.Errorf("a full page: %d link rows of %d components, %v", rows(full), len(full.Sections[0].Items), err)
	}

	// a native stream of 7 gets neither the link nor a row for it; one of 8 keeps it as drawn
	raw, _ := json.Marshal(linked())
	for version, want := range map[int]bool{7: false, 8: true} {
		fitted, err := FitNative(raw, page, version, i18n.En)
		if err != nil {
			t.Fatalf("native %d: %v", version, err)
		}
		var got View
		_ = json.Unmarshal(fitted, &got)
		s := got.Sections[0]
		if has := s.Link != nil && s.Icon != "" && s.Span != "" && s.Columns == 2 && got.Layout == wire.LayoutColumns; has != want || rows(&got) != 0 {
			t.Errorf("native %d: %s", version, fitted)
		}
		if !want && (got.Layout != wire.LayoutStack || s.Link != nil || s.Icon != "" || s.IconColor != "" || s.Span != "" || s.Columns != 0) {
			t.Errorf("native %d: a field of 8 left: %s", version, fitted)
		}
	}
}

// A link's action is one of the view's: its push is checked as any other's, its values reach an app of 4 and later,
// and an assistant-only one is taken out of an extension's view.
func TestSectionLinkAction(t *testing.T) {
	v := &View{Sections: []Section{{Items: []UIElem{}, Link: &SectionLink{Title: "All", Action: UIAction{ID: "all", Push: &Push{State: "all"}}}}}}
	if !slices.ContainsFunc(Actions(v), func(a *UIAction) bool { return a.ID == "all" && a.Push != nil }) {
		t.Fatalf("Actions misses the link: %+v", Actions(v))
	}
	assistant := &View{Sections: []Section{{Items: []UIElem{{Type: "text", Text: "x"}}, Link: &SectionLink{Title: "Reply", Action: UIAction{ID: "r", Reply: "yes"}}}}}
	page, _ := Slot("nav_page")
	dropped, err := Fit(assistant, page, 8)
	if err != nil || assistant.Sections[0].Link != nil || !slices.Contains(dropped, "assistant_action") {
		t.Errorf("an assistant-only link: %+v, dropped %v, %v", assistant.Sections[0].Link, dropped, err)
	}
	old := &View{Sections: []Section{{Items: []UIElem{}, Link: &SectionLink{Title: "All", Action: UIAction{ID: "all", Values: map[string]string{"range": "week"}, Push: &Push{State: "all"}}}}}}
	if _, err := Fit(old, page, 2); err != nil {
		t.Fatal(err)
	}
	if a := old.Sections[0].Items[0].Action; a.Push != nil || len(a.Values) != 0 || !strings.EqualFold(a.ID, "all") {
		t.Errorf("an app of 2 gets the link's row with %+v", a)
	}
}
