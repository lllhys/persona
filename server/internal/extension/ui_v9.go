package extension

import (
	"math"
	"strconv"
	"unicode/utf8"

	"github.com/nwverse/persona/sdk/go/wire"
)

// UI version 9 (docs/extensions/extensions.md "UI version 9", #9337): a tile's gauge, a ring filled to a ratio with the
// tile's value in its centre, for a quota or a usage. downgrade9 draws it with version 8's for an older app.

// Gauge is a tile's ring (UI version 9).
type Gauge = wire.Gauge

// The longest a tile's value and subtitle may be (view.schema.json Tile), in characters.
const (
	tileValueMost    = 40
	tileSubtitleMost = 80
)

// downgrade9 draws every tile's gauge with version 8's fields, in place: the tile stays, the gauge goes, and what it
// said moves onto the tile (gaugeTile). Below 6 the tile then becomes a row as any other (downgrade6), with that text.
func downgrade9(v *View) {
	for i := range v.Sections {
		for j := range v.Sections[i].Items {
			e := &v.Sections[i].Items[j]
			for k := range e.Tiles {
				gaugeTile(&e.Tiles[k])
			}
		}
	}
}

// gaugeTile takes t's gauge out and keeps what it said: the unit after the value ("4.69 USD"), the percentage in the
// subtitle ("Resets at 18:00 · 94%"), or as the value when the tile has none ("94%"), and the gauge's color as the
// tile's when the tile has none of its own, so a quota running low still shows in its color. Text that would pass a
// field's length is left as it was rather than cut.
func gaugeTile(t *Tile) {
	g := t.Gauge
	if g == nil {
		return
	}
	t.Gauge = nil
	ratio := 0.0
	if g.Ratio != nil {
		ratio = *g.Ratio
	}
	pct := gaugePercent(ratio)
	if t.Value == "" {
		t.Value = pct
	} else {
		if g.Unit != "" {
			t.Value = fitsOr(t.Value, t.Value+" "+g.Unit, tileValueMost)
		}
		if t.Subtitle == "" {
			t.Subtitle = pct
		} else {
			t.Subtitle = fitsOr(t.Subtitle, t.Subtitle+" · "+pct, tileSubtitleMost)
		}
	}
	if t.Color == "" {
		t.Color = g.Color
	}
}

// fitsOr is longer when it has at most most characters, else short.
func fitsOr(short, longer string, most int) string {
	if utf8.RuneCountInString(longer) > most {
		return short
	}
	return longer
}

// gaugePercent is how full a gauge is as a whole percentage ("94%"), as the apps read it out: the ratio held to 0–1.
func gaugePercent(ratio float64) string {
	if math.IsNaN(ratio) {
		ratio = 0
	}
	return strconv.Itoa(int(math.Round(min(1, max(0, ratio))*100))) + "%"
}
