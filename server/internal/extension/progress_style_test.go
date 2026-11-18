package extension

import (
	"fmt"
	"testing"
)

func progressStyleView(style string) *View {
	ratio := 0.625
	return &View{Sections: []Section{{Items: []UIElem{
		{Type: "progress", Label: "Goal", Ratio: &ratio, Style: style},
		{Type: "row", Title: "Details", Action: &UIAction{ID: "details"}},
	}}}}
}

func TestFitProgressStyle(t *testing.T) {
	page, _ := Slot("nav_page")
	for _, version := range []int{1, 3, 5, 9} {
		for _, style := range []string{"", "bar", "ring"} {
			t.Run(fmt.Sprintf("v%d/style=%q", version, style), func(t *testing.T) {
				v := progressStyleView(style)
				if _, err := Fit(v, page, version); err != nil {
					t.Fatal(err)
				}
				item := v.Sections[0].Items[0]
				if item.Type != "progress" || item.Style != style || item.Ratio == nil || *item.Ratio != 0.625 {
					t.Fatalf("progress changed: %+v", item)
				}
			})
		}
	}
}

func TestFitWidgetProgressStyle(t *testing.T) {
	for _, version := range []int{1, 3, 5, 9} {
		for _, style := range []string{"", "bar", "ring"} {
			for _, family := range []string{"small", "medium", "large", "accessory_circular", "accessory_rectangular", "accessory_inline"} {
				t.Run(fmt.Sprintf("v%d/%s/style=%q", version, family, style), func(t *testing.T) {
					v := progressStyleView(style)
					v.Sections[0].Items = append(v.Sections[0].Items, UIElem{Type: "text", Text: "Summary"})
					if _, err := FitWidget(v, family, version); err != nil {
						t.Fatal(err)
					}
					item := v.Sections[0].Items[0]
					if family == "accessory_inline" {
						if item.Type != "text" || item.Text != "Summary" {
							t.Fatalf("inline must skip progress: %+v", item)
						}
					} else if item.Type != "progress" || item.Style != style || item.Ratio == nil || *item.Ratio != 0.625 {
						t.Fatalf("progress changed: %+v", item)
					}
					for _, section := range v.Sections {
						for _, item := range section.Items {
							if item.Action != nil {
								t.Fatalf("widget retained an action: %+v", item)
							}
						}
					}
					if family == "small" || family == "medium" || family == "large" {
						if items := v.Sections[0].Items; len(items) != 3 || items[1].Type != "row" || items[1].Title != "Details" {
							t.Fatalf("widget must keep the row without its action: %+v", items)
						}
					}
				})
			}
		}
	}
}
