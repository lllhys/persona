package extension

import (
	"encoding/json"
	"fmt"
	"slices"
	"testing"

	"github.com/nwverse/persona/sdk/go/wire"
	"github.com/nwverse/persona/server/internal/i18n"
)

func tabActionFixture(action UIAction) *View {
	return &View{
		Tabs: &Tabs{
			Name: "view", Value: "one",
			Items:  []Tab{{Value: "one", Title: "One"}, {Value: "two", Title: "Two"}},
			Action: action,
		},
		Sections: []Section{{Title: "keep me", Items: []UIElem{{Type: "text", Text: "body"}}}},
	}
}

func TestFitDropsAssistantOnlyTabs(t *testing.T) {
	cases := map[string]UIAction{
		"reply":     {ID: "switch", Reply: "One"},
		"open":      {ID: "switch", Open: "https://example.com/one"},
		"pop":       {ID: "switch", Pop: true},
		"push page": {ID: "switch", Push: &Push{Page: "one"}},
	}
	page, _ := Slot("nav_page")
	for name, action := range cases {
		for _, version := range []int{UIVersion, 3} {
			t.Run(fmt.Sprintf("%s/ui%d", name, version), func(t *testing.T) {
				v := tabActionFixture(action)
				dropped, err := Fit(v, page, version)
				if err != nil {
					t.Fatalf("Fit rejected assistant-only tabs: %v", err)
				}
				for _, a := range Actions(v) {
					if wire.AssistantOnlyAction(a) {
						t.Fatalf("assistant-only action survived UI %d: action=%#v tabs=%#v", version, a, v.Tabs)
					}
				}
				if v.Tabs != nil || !slices.Contains(dropped, "assistant_action") {
					t.Fatalf("assistant-only tabs survived UI %d: tabs=%#v dropped=%v", version, v.Tabs, dropped)
				}
				if len(v.Sections) != 1 || v.Sections[0].Title != "keep me" || len(v.Sections[0].Items) != 1 {
					t.Fatalf("sections changed while dropping tabs: %#v", v.Sections)
				}
			})
		}
	}
}

func TestFitNativeDropsAssistantOnlyTabs(t *testing.T) {
	raw := []byte(`{"tabs":{"name":"view","value":"one","items":[{"value":"one","title":"One"},{"value":"two","title":"Two"}],"action":{"id":"switch","reply":"One"}},"sections":[{"key":"main","title":"keep me","items":[{"key":"body","type":"text","text":"body"}]}]}`)
	page, _ := Slot("nav_page")
	fitted, err := FitNative(raw, page, UIVersion, i18n.En)
	if err != nil {
		t.Fatalf("FitNative rejected assistant-only tabs: %v", err)
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal(fitted, &object); err != nil {
		t.Fatal(err)
	}
	if _, ok := object["tabs"]; ok {
		t.Fatalf("FitNative emitted assistant-only tabs: %s", fitted)
	}
	v, err := wire.DecodeCheckedViewJSON(fitted)
	if err != nil {
		t.Fatalf("fitted output invalid: %v; output=%s", err, fitted)
	}
	for _, a := range wire.Actions(&v) {
		if wire.AssistantOnlyAction(a) {
			t.Fatalf("FitNative emitted an assistant-only action: %s", fitted)
		}
	}
	if len(v.Sections) != 1 || v.Sections[0].Title != "keep me" || len(v.Sections[0].Items) != 1 {
		t.Fatalf("sections changed while dropping tabs: %s", fitted)
	}
}

func TestFitKeepsOrdinaryExtensionTabs(t *testing.T) {
	page, _ := Slot("nav_page")
	for _, version := range []int{UIVersion, 3} {
		t.Run(fmt.Sprintf("ui%d", version), func(t *testing.T) {
			v := tabActionFixture(UIAction{ID: "switch", Values: map[string]string{"source": "tabs"}})
			dropped, err := Fit(v, page, version)
			if err != nil {
				t.Fatalf("Fit rejected ordinary extension tabs: %v", err)
			}
			if slices.Contains(dropped, "assistant_action") {
				t.Fatalf("ordinary extension action misclassified: dropped=%v", dropped)
			}
			if version == UIVersion {
				if v.Tabs == nil || v.Tabs.Name != "view" || v.Tabs.Value != "one" || len(v.Tabs.Items) != 2 ||
					v.Tabs.Action.ID != "switch" || v.Tabs.Action.Values["source"] != "tabs" {
					t.Fatalf("ordinary tabs changed: %#v", v.Tabs)
				}
			} else {
				if v.Tabs != nil || len(v.Sections) != 2 || len(v.Sections[0].Items) != 1 {
					t.Fatalf("ordinary tabs did not downgrade: %#v", v)
				}
				segmented := v.Sections[0].Items[0]
				if segmented.Type != "segmented" || segmented.Name != "view" || segmented.Value != "one" ||
					len(segmented.Choices) != 2 || segmented.Action == nil || segmented.Action.ID != "switch" || segmented.Action.Values != nil {
					t.Fatalf("ordinary tab downgrade changed: %#v", segmented)
				}
			}
			body := v.Sections[len(v.Sections)-1]
			if body.Title != "keep me" || len(body.Items) != 1 || body.Items[0].Text != "body" {
				t.Fatalf("ordinary sections changed: %#v", v.Sections)
			}
		})
	}
}
