package wire

import (
	"encoding/json"
	"fmt"
	"testing"
)

func TestProgressStyleRoundTrip(t *testing.T) {
	for _, style := range []string{"", ProgressStyleBar, ProgressStyleRing} {
		t.Run(fmt.Sprintf("style=%q", style), func(t *testing.T) {
			ratio := 0.625
			v := View{Sections: []Section{{Items: []Component{{Type: "progress", Label: "Goal", Ratio: &ratio, Style: style}}}}}
			if err := CheckView(&v); err != nil {
				t.Fatal(err)
			}
			raw, err := json.Marshal(v)
			if err != nil {
				t.Fatal(err)
			}
			if err := CheckViewJSON(raw); err != nil {
				t.Fatal(err)
			}
			var got View
			if err := json.Unmarshal(raw, &got); err != nil {
				t.Fatal(err)
			}
			if item := got.Sections[0].Items[0]; item.Style != style || item.Ratio == nil || *item.Ratio != ratio {
				t.Fatalf("round trip: %+v", item)
			}
		})
	}
}

func TestProgressStyleValidation(t *testing.T) {
	ratio := 0.5
	v := View{Sections: []Section{{Items: []Component{{Type: "progress", Label: "Goal", Ratio: &ratio, Style: "donut"}}}}}
	if err := CheckView(&v); err == nil {
		t.Fatal("CheckView accepted an unknown progress style")
	}
	// Component.Style is a string with omitempty: typed validation cannot distinguish
	// an empty string from omission, or retain null and fields of the wrong type.
	for _, style := range []string{`"donut"`, `""`, `42`, `null`} {
		t.Run(style, func(t *testing.T) {
			raw := []byte(`{"sections":[{"items":[{"type":"progress","label":"Goal","ratio":0.5,"style":` + style + `}]}]}`)
			if err := CheckViewJSON(raw); err == nil {
				t.Fatalf("CheckViewJSON accepted style %s", style)
			}
		})
	}
}

func TestProgressStyleStreamBoundary(t *testing.T) {
	for _, style := range []string{ProgressStyleBar, ProgressStyleRing} {
		t.Run(style, func(t *testing.T) {
			item := `{"type":"progress","key":"goal","label":"Goal","ratio":0.5,"style":"` + style + `"}`
			raw := []byte(`{"sections":[{"key":"main","items":[` + item + `]}]}`)
			if err := CheckViewJSON(raw); err != nil {
				t.Fatalf("snapshot: %v", err)
			}
			if err := ValidateJSON("UIStreamItem", []byte(item)); err == nil {
				t.Fatal("stream item accepted progress style")
			}
			if _, err := DecodeCheckedStreamViewJSON(raw); err == nil {
				t.Fatal("stream view accepted progress style")
			}
		})
	}
}
