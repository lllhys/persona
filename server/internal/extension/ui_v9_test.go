package extension

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nwverse/persona/server/internal/i18n"
)

func v9Example(t *testing.T, name string) *View {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "schema", "extension", "examples", "View.v9-"+name+".json"))
	if err != nil {
		t.Fatal(err)
	}
	v, err := ParseView(raw)
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	return v
}

var v9Examples = []string{"quota", "gauge-percent"}

// tilesOf is every tile of v, in order.
func tilesOf(v *View) []Tile {
	var out []Tile
	for _, s := range v.Sections {
		for _, e := range s.Items {
			out = append(out, e.Tiles...)
		}
	}
	return out
}

// An app of UI version 9 gets every example as the extension drew it. An app of 6 to 8 gets the same sections,
// components and tiles with no gauge left, each gauge's tile carrying what the ring said (gaugeTile); every other tile
// as it was.
func TestFitV9Examples(t *testing.T) {
	page, _ := Slot("nav_page")
	for _, name := range v9Examples {
		v := v9Example(t, name)
		want, _ := json.Marshal(v)
		if dropped, err := Fit(v, page, 9); err != nil || len(dropped) > 0 {
			t.Fatalf("%s for 9: dropped %v, %v", name, dropped, err)
		}
		if got, _ := json.Marshal(v); string(got) != string(want) {
			t.Errorf("%s: an app of 9 gets it changed\nwant %s\n got %s", name, want, got)
		}
		drawn := tilesOf(v9Example(t, name))
		for version := 6; version < 9; version++ {
			v := v9Example(t, name)
			if _, err := Fit(v, page, version); err != nil {
				t.Fatalf("%s for %d: %v", name, version, err)
			}
			got := tilesOf(v)
			if len(got) != len(drawn) {
				t.Fatalf("%s for %d: %d tiles of %d", name, version, len(got), len(drawn))
			}
			for i, tile := range got {
				d := drawn[i]
				if tile.Gauge != nil {
					t.Errorf("%s for %d: tile %q keeps its gauge", name, version, tile.Title)
				}
				if d.Gauge == nil {
					if a, b := mustJSON(tile), mustJSON(d); a != b {
						t.Errorf("%s for %d: a tile without a gauge changed\nwant %s\n got %s", name, version, b, a)
					}
					continue
				}
				pct := gaugePercent(*d.Gauge.Ratio)
				if tile.Title != d.Title || !strings.Contains(tile.Value+" "+tile.Subtitle, pct) {
					t.Errorf("%s for %d: tile %q reads %q / %q, without %s", name, version, d.Title, tile.Value, tile.Subtitle, pct)
				}
				if d.Value != "" && d.Gauge.Unit != "" && tile.Value != d.Value+" "+d.Gauge.Unit {
					t.Errorf("%s for %d: tile %q's value %q, not with its unit %q", name, version, d.Title, tile.Value, d.Gauge.Unit)
				}
				if want := orStr(d.Color, d.Gauge.Color); tile.Color != want {
					t.Errorf("%s for %d: tile %q colored %q, want %q", name, version, d.Title, tile.Color, want)
				}
			}
		}
	}
}

func mustJSON(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

// What a gauge leaves on its tile for an older app, case by case: nothing is cut, and the tile's own fields win.
func TestGaugeTile(t *testing.T) {
	long := strings.Repeat("x", 38)
	for name, c := range map[string]struct{ in, want Tile }{
		"value and unit": {
			Tile{Title: "5 hours", Value: "4.69", Gauge: &Gauge{Ratio: num(0.94), Unit: "USD", Color: "green"}},
			Tile{Title: "5 hours", Value: "4.69 USD", Subtitle: "94%", Color: "green"}},
		"no value": {
			Tile{Title: "Disk", Subtitle: "of 512 GB", Gauge: &Gauge{Ratio: num(0.5), Unit: "GB"}},
			Tile{Title: "Disk", Subtitle: "of 512 GB", Value: "50%"}},
		"subtitle kept, percentage after it": {
			Tile{Title: "Week", Subtitle: "Resets Monday", Value: "38.20", Gauge: &Gauge{Ratio: num(0.755)}},
			Tile{Title: "Week", Subtitle: "Resets Monday · 76%", Value: "38.20"}},
		"the tile's own color": {
			Tile{Title: "Fable", Value: "0.62", Color: "blue", Gauge: &Gauge{Ratio: num(0.12), Color: "amber"}},
			Tile{Title: "Fable", Value: "0.62", Subtitle: "12%", Color: "blue"}},
		"a unit past 40 characters": {
			Tile{Title: "t", Value: long, Gauge: &Gauge{Ratio: num(0), Unit: "USD"}},
			Tile{Title: "t", Value: long, Subtitle: "0%"}},
		"a subtitle past 80 characters": {
			Tile{Title: "t", Value: "1", Subtitle: strings.Repeat("长", 76), Gauge: &Gauge{Ratio: num(1)}},
			Tile{Title: "t", Value: "1", Subtitle: strings.Repeat("长", 76)}},
		"no gauge": {
			Tile{Title: "t", Value: "1"},
			Tile{Title: "t", Value: "1"}},
	} {
		got := c.in
		gaugeTile(&got)
		if a, b := mustJSON(got), mustJSON(c.want); a != b {
			t.Errorf("%s:\nwant %s\n got %s", name, b, a)
		}
	}
	for ratio, want := range map[float64]string{0: "0%", 0.004: "0%", 0.005: "1%", 0.5: "50%", 0.999: "100%", 1: "100%"} {
		if got := gaugePercent(ratio); got != want {
			t.Errorf("gaugePercent(%v) = %q, want %q", ratio, got, want)
		}
	}
}

// Below 6 a gauge's tile is a row as any other tile, with the text the gauge left: nothing of the ring is lost.
func TestFitV9TileAsARow(t *testing.T) {
	page, _ := Slot("nav_page")
	v := v9Example(t, "quota")
	if _, err := Fit(v, page, 5); err != nil {
		t.Fatal(err)
	}
	var row *UIElem
	for _, e := range items(v) {
		if e.Type == "grid" || e.Type == "carousel" {
			t.Fatalf("an app of 5 gets a %s", e.Type)
		}
		if e.Title == "Fable this week" {
			row = &e
		}
	}
	if row == nil || row.Type != "row" || row.Value != "0.62 USD" || row.Subtitle != "12%" || row.Action == nil || row.Action.ID != "open_limit" {
		t.Fatalf("Fable this week as a row: %+v", row)
	}
}

// A ring takes the place of a tile's picture and its spark, so a tile has none of them with a gauge; an icon is
// fine (an older app draws it). The ratio is 0 to 1 and the color a named one, as the schema says.
func TestCheckViewGauge(t *testing.T) {
	grid := func(tile Tile) *View {
		return &View{Sections: []Section{{Items: []UIElem{{Type: "grid", Tiles: []Tile{tile}}}}}}
	}
	for name, tile := range map[string]Tile{
		"with an image": {Title: "t", Image: &ImageSource{Ref: "a"}, Gauge: &Gauge{Ratio: num(0.5)}},
		"with a spark":  {Title: "t", Spark: &Spark{Values: []float64{1, 2}}, Gauge: &Gauge{Ratio: num(0.5)}},
		"ratio above 1": {Title: "t", Gauge: &Gauge{Ratio: num(1.01)}},
		"ratio below 0": {Title: "t", Gauge: &Gauge{Ratio: num(-0.01)}},
		"a hex color":   {Title: "t", Gauge: &Gauge{Ratio: num(0.5), Color: "#22aa55"}},
		"a long unit":   {Title: "t", Gauge: &Gauge{Ratio: num(0.5), Unit: "kilowatts"}},
		"no ratio":      {Title: "t", Value: "4.69", Gauge: &Gauge{Unit: "USD"}},
	} {
		if err := CheckView(grid(tile)); err == nil {
			t.Errorf("%s: taken", name)
		}
	}
	for name, tile := range map[string]Tile{
		"ratio 0": {Title: "t", Gauge: &Gauge{Ratio: num(0)}},
		"full, with an icon, a badge and an action": {Title: "t", Icon: "gauge", Value: "4.69", Badge: &Badge{Text: "Low", Color: "amber"},
			Action: &UIAction{ID: "open"}, Gauge: &Gauge{Ratio: num(1), Unit: "USD", Color: "amber"}},
	} {
		if err := CheckView(grid(tile)); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
}

// A native stream of 8 gets the tile without its gauge (with what it said); one of 9 keeps the gauge as drawn.
func TestFitNativeV9(t *testing.T) {
	raw, err := json.Marshal(&View{Sections: []Section{{Key: "s", Items: []UIElem{{Type: "grid", Key: "g", Tiles: []Tile{
		{Key: "a", Title: "5 hours", Value: "4.69", Gauge: &Gauge{Ratio: num(0.94), Unit: "USD", Color: "green"}},
	}}}}}})
	if err != nil {
		t.Fatal(err)
	}
	page, _ := Slot("nav_page")
	for version, keeps := range map[int]bool{8: false, 9: true} {
		fitted, err := FitNative(raw, page, version, i18n.En)
		if err != nil {
			t.Fatalf("native %d: %v", version, err)
		}
		var got View
		_ = json.Unmarshal(fitted, &got)
		tile := got.Sections[0].Items[0].Tiles[0]
		if (tile.Gauge != nil) != keeps {
			t.Errorf("native %d: %s", version, fitted)
		}
		if !keeps && (tile.Value != "4.69 USD" || tile.Subtitle != "94%" || tile.Color != "green") {
			t.Errorf("native %d: the tile reads %+v", version, tile)
		}
	}
}
