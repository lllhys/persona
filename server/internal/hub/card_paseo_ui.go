package hub

import (
	"bytes"
	"encoding/json"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/nwverse/persona/sdk/go/wire"
	"github.com/nwverse/persona/server/internal/i18n"
)

// Paseo tells the agents it runs to draw tables, charts, numbers, progress, checklists, steps and choices as a
// ```paseo-ui block of one JSON object (#8855), which Paseo's own app draws and no Persona app knows. On their way out
// (cardCompatItems) such blocks become what every app draws in every chat: a ```persona-view of the same parts, with a
// plain-text fallback the hub writes. Nothing is stored: the transcript keeps the block.
//
//   - A block that is not one object of a known type, or not closed, stays the code it is (as Paseo shows it).
//   - Past the apps' three cards a reply, or before one of the reply's own persona-view blocks (whose keys count the
//     blocks before them, docs/assistant/assistant-views.md "Card identity"), a block becomes its fallback.
//   - table → table; bar, line, pie, donut → a chart of that kind; stat → stat_grid; progress → a progress per bar;
//     checklist, steps → a row per item with its state as a badge; buttons → a row per choice under "reply with one".
//     A card's title is its section's; its caption is the table's or chart's own, else the section's footer.
//   - Past the components' limits (8 columns and 100 rows, 4 series of 120 points, 8 slices, 6 stats a grid) the
//     first ones are kept and the card says what was left out; slices past 7 are summed as "Other".
//   - Buttons cannot answer from here: a card's actions are the assistant chat's, so the choices are shown to reply
//     with.
const paseoUILanguage = "paseo-ui"

// paseoUIConverted is md with each complete, readable paseo-ui block written as a persona-view (or its fallback).
func paseoUIConverted(l i18n.Lang, md string) string {
	if !containsFoldASCII(md, paseoUILanguage) {
		return md
	}
	if !i18n.Valid(l) {
		l = i18n.ZhHans
	}
	blocks := wire.FencedBlocks(md, "persona-view", paseoUILanguage)
	views, lastView := 3, -1
	for _, blk := range blocks {
		if blk.Language == "persona-view" {
			views--
			lastView = blk.Start
		}
	}
	var b strings.Builder
	at := 0
	for _, blk := range blocks {
		if blk.Language != paseoUILanguage || !blk.Complete || len(blk.Raw) > wire.AssistantViewMost {
			continue
		}
		v := paseoUIView(l, blk.Raw)
		if v == nil {
			continue
		}
		b.WriteString(md[at:blk.Start])
		raw, err := json.Marshal(v)
		if blk.Start > lastView && views > 0 && err == nil && wire.CheckAssistantView(v) == nil {
			b.WriteString("```persona-view\n" + string(raw) + "\n```")
			views--
		} else {
			b.WriteString(v.Fallback)
		}
		if strings.HasSuffix(md[blk.Start:blk.End], "\n") {
			b.WriteString("\n")
		}
		at = blk.End
	}
	if at == 0 {
		return md
	}
	b.WriteString(md[at:])
	return b.String()
}

// paseoUI is a paseo-ui block's object: the fields of every type together.
type paseoUI struct {
	Type       string            `json:"type"`
	Title      paseoText         `json:"title"`
	Caption    paseoText         `json:"caption"`
	Unit       paseoText         `json:"unit"`
	Columns    []json.RawMessage `json:"columns"`
	Rows       []json.RawMessage `json:"rows"`
	Footer     []json.RawMessage `json:"footer"`
	Data       []paseoPoint      `json:"data"`
	Categories []paseoText       `json:"categories"`
	Series     []paseoSeries     `json:"series"`
	Stacked    bool              `json:"stacked"`
	Area       bool              `json:"area"`
	Center     *struct {
		Value paseoText `json:"value"`
		Label paseoText `json:"label"`
	} `json:"center"`
	Items   []json.RawMessage `json:"items"`
	Steps   []json.RawMessage `json:"steps"` // what agents write for a steps card's items as often as items
	Buttons []struct {
		Label   paseoText `json:"label"`
		Message paseoText `json:"message"`
	} `json:"buttons"`
}

type paseoPoint struct {
	Label paseoText `json:"label"`
	Value paseoNum  `json:"value"`
	Tone  string    `json:"tone"`
}

type paseoSeries struct {
	Name   paseoText  `json:"name"`
	Values []paseoNum `json:"values"`
	Tone   string     `json:"tone"`
}

// paseoText is a string, or a number or a boolean written as text; null and anything else are empty.
type paseoText string

func (t *paseoText) UnmarshalJSON(raw []byte) error {
	*t = paseoText(cellText(raw))
	return nil
}

func (t paseoText) String() string { return strings.TrimSpace(string(t)) }

// paseoNum is a number, or numeric text ("1,234", "12.5%", "¥3,000") with that text kept to show; null is not ok.
type paseoNum struct {
	value float64
	text  string // the text it was written as, "" for a JSON number
	ok    bool
}

func (n *paseoNum) UnmarshalJSON(raw []byte) error {
	*n = paseoNum{}
	var f float64
	if string(bytes.TrimSpace(raw)) == "null" {
		return nil
	}
	if json.Unmarshal(raw, &f) == nil {
		n.value, n.ok = f, true
		return nil
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		n.value, n.ok = numberIn(s)
		n.text = strings.TrimSpace(s)
	}
	return nil
}

// numberIn reads the number in text like "¥3,000", "12.5%" or "−0.3": its digits, point and sign.
func numberIn(s string) (float64, bool) {
	var b strings.Builder
	for _, r := range strings.TrimSpace(s) {
		switch {
		case r >= '0' && r <= '9' || r == '.':
			b.WriteRune(r)
		case (r == '-' || r == '−') && b.Len() == 0:
			b.WriteByte('-')
		}
	}
	f, err := strconv.ParseFloat(b.String(), 64)
	return f, err == nil
}

// paseoUIView is the block as a View, nil when it is not one object of a known type with something to draw.
func paseoUIView(l i18n.Lang, raw json.RawMessage) *wire.View {
	var p paseoUI
	if json.Unmarshal(relaxedJSON(raw), &p) != nil {
		return nil
	}
	c := paseoCard{l: l, p: p, section: wire.Section{Title: cut(p.Title.String(), 2000)}}
	if t := p.Title.String(); t != "" {
		c.words = append(c.words, t)
	}
	ok := false
	switch strings.ToLower(strings.TrimSpace(p.Type)) {
	case "table":
		ok = c.table()
	case "bar":
		ok = c.chart(wire.ChartKindBar)
	case "line":
		kind := wire.ChartKindLine
		if p.Area {
			kind = wire.ChartKindArea
		}
		ok = c.chart(kind)
	case "pie":
		ok = c.pie(wire.ChartKindPie)
	case "donut":
		ok = c.pie(wire.ChartKindDonut)
	case "stat":
		ok = c.stats()
	case "progress":
		ok = c.progress()
	case "checklist":
		ok = c.checklist()
	case "steps":
		ok = c.steps()
	case "buttons":
		ok = c.buttons()
	}
	if !ok || len(c.section.Items) == 0 {
		return nil
	}
	if caption := p.Caption.String(); caption != "" && !c.captioned {
		c.section.Footer = cut(caption, 2000)
		c.words = append(c.words, caption)
	}
	fallback := strings.Join(c.words, "\n")
	if utf8.RuneCountInString(fallback) > 2000 {
		fallback = string([]rune(fallback)[:1999]) + "…"
	}
	if strings.TrimSpace(fallback) == "" {
		fallback = tt(l, "paseo_ui_card")
	}
	return &wire.View{Sections: []wire.Section{c.section}, Fallback: fallback}
}

// paseoCard builds one card's section and, line by line, its fallback.
type paseoCard struct {
	l         i18n.Lang
	p         paseoUI
	section   wire.Section
	words     []string
	captioned bool // the caption went into the table or the chart
}

func (c *paseoCard) add(items ...wire.Component) { c.section.Items = append(c.section.Items, items...) }

func (c *paseoCard) table() bool {
	var cols []wire.TableColumn
	var labels []string
	for _, raw := range c.p.Columns {
		var col struct {
			Label paseoText `json:"label"`
			Title paseoText `json:"title"`
			Align string    `json:"align"`
		}
		if json.Unmarshal(raw, &col) != nil {
			col.Label = paseoText(cellText(raw))
		}
		label := firstOf(col.Label.String(), col.Title.String())
		align := ""
		switch strings.ToLower(col.Align) {
		case "right", "end":
			align = wire.TableAlignEnd
		case "left", "start":
			align = wire.TableAlignStart
		}
		cols = append(cols, wire.TableColumn{Title: cut(label, 200), Align: align})
		labels = append(labels, label)
	}
	if len(cols) == 0 {
		return false
	}
	rows := c.p.Rows
	if len(c.p.Footer) > 0 {
		footer, _ := json.Marshal(c.p.Footer)
		rows = append(append([]json.RawMessage{}, rows...), footer)
	}
	var cells [][]string
	numeric := make([]int, len(cols)) // per column: 1 while every cell so far is a number, -1 once one is not
	for _, raw := range rows {
		row := make([]string, len(cols))
		var list []json.RawMessage
		var keyed map[string]json.RawMessage
		if json.Unmarshal(raw, &list) == nil {
			for i := range min(len(list), len(cols)) {
				row[i] = cut(cellText(list[i]), 300)
				numeric[i] = numericCell(numeric[i], list[i])
			}
		} else if json.Unmarshal(raw, &keyed) == nil {
			for i, label := range labels {
				if v, ok := keyed[label]; ok {
					row[i] = cut(cellText(v), 300)
					numeric[i] = numericCell(numeric[i], v)
				}
			}
		} else {
			continue
		}
		cells = append(cells, row)
		c.words = append(c.words, strings.Join(row, " · "))
	}
	for i := range cols {
		if cols[i].Align == "" && numeric[i] == 1 {
			cols[i].Align = wire.TableAlignEnd
		}
	}
	var notes []string
	if len(cols) > 8 {
		notes = append(notes, tt(c.l, "paseo_ui_columns_cut", i18n.Args{"shown": 8, "total": len(cols)}))
		cols = cols[:8]
		for i := range cells {
			cells[i] = cells[i][:8]
		}
	}
	if len(cells) > 100 {
		notes = append(notes, tt(c.l, "paseo_ui_rows_cut", i18n.Args{"shown": 100, "total": len(cells)}))
		cells = cells[:100]
	}
	if cells == nil {
		cells = [][]string{}
	}
	caption := strings.Join(append(nonEmpty(c.p.Caption.String()), notes...), "\n")
	c.captioned = true
	if caption != "" {
		c.words = append(c.words, caption)
	}
	c.add(wire.Component{Type: wire.ComponentTable, Columns: cols, Rows: cells, Caption: cut(caption, 2000)})
	return true
}

// numericCell is a column's numeric state after one more cell: a JSON number or numeric text keeps it numeric, an
// empty cell leaves it as it is, anything else makes it not.
func numericCell(state int, raw json.RawMessage) int {
	if state < 0 {
		return state
	}
	text := strings.TrimSpace(cellText(raw))
	if text == "" {
		return state
	}
	var f float64
	if json.Unmarshal(raw, &f) == nil {
		return 1
	}
	if _, ok := numberIn(text); ok && utf8.RuneCountInString(text) <= 16 {
		return 1
	}
	return -1
}

func (c *paseoCard) chart(kind string) bool {
	labels, series := c.series()
	if len(labels) == 0 || len(series) == 0 {
		return false
	}
	var notes []string
	if len(series) > 4 {
		notes = append(notes, tt(c.l, "paseo_ui_series_cut", i18n.Args{"shown": 4, "total": len(series)}))
		series = series[:4]
	}
	// A point any series leaves out (null) is left out of all of them: every series has the same labels.
	var keep []int
	for i := range labels {
		all := true
		for _, s := range series {
			if i >= len(s.Values) || !s.Values[i].ok {
				all = false
			}
		}
		if all {
			keep = append(keep, i)
		}
	}
	if len(keep) > 120 {
		notes = append(notes, tt(c.l, "paseo_ui_points_cut", i18n.Args{"shown": 120, "total": len(keep)}))
		keep = keep[:120]
	}
	if len(keep) == 0 {
		return false
	}
	unit := c.p.Unit.String()
	var out []wire.ChartSeries
	for n, s := range series {
		name := s.Name.String()
		if name == "" && len(series) > 1 {
			name = tt(c.l, "paseo_ui_series", i18n.Args{"n": n + 1})
		}
		cs := wire.ChartSeries{Name: cut(name, 40)}
		cs.Tone, cs.Color = paseoTone(s.Tone)
		var line []string
		for _, i := range keep {
			v := s.Values[i]
			cs.Points = append(cs.Points, wire.ChartPoint{Label: cut(labels[i], 40), Value: v.value, Display: cut(v.text, 40)})
			line = append(line, labels[i]+" "+numberWords(v, unit))
		}
		if name != "" {
			c.words = append(c.words, name+": "+strings.Join(line, ", "))
		} else {
			c.words = append(c.words, strings.Join(line, ", "))
		}
		out = append(out, cs)
	}
	detail := strings.Join(append(nonEmpty(c.p.Caption.String()), notes...), "\n")
	c.captioned = true
	if detail != "" {
		c.words = append(c.words, detail)
	}
	c.add(wire.Component{Type: wire.ComponentChart, Kind: kind, Series: out, Unit: cut(unit, 2000),
		Stacked: c.p.Stacked && kind == wire.ChartKindBar && len(out) > 1, Detail: cut(detail, 2000)})
	return true
}

// series is a bar or line card's labels and series: categories with series, or data as one series.
func (c *paseoCard) series() ([]string, []paseoSeries) {
	if len(c.p.Series) > 0 {
		labels := make([]string, len(c.p.Categories))
		for i, t := range c.p.Categories {
			labels[i] = t.String()
		}
		return labels, c.p.Series
	}
	var labels []string
	one := paseoSeries{}
	for _, d := range c.p.Data {
		labels = append(labels, d.Label.String())
		one.Values = append(one.Values, d.Value)
	}
	return labels, []paseoSeries{one}
}

func (c *paseoCard) pie(kind string) bool {
	unit := c.p.Unit.String()
	var slices []paseoPoint
	for _, d := range c.p.Data {
		if !d.Value.ok || d.Value.value < 0 {
			return false
		}
		slices = append(slices, d)
	}
	for _, d := range slices {
		c.words = append(c.words, d.Label.String()+" "+numberWords(d.Value, unit))
	}
	if len(slices) < 2 {
		// One slice is no pie: its row.
		for _, d := range slices {
			c.add(wire.Component{Type: wire.ComponentRow, Title: cut(firstOf(d.Label.String(), "—"), 2000), Value: cut(numberWords(d.Value, unit), 2000)})
		}
		return len(slices) == 1
	}
	if len(slices) > 8 {
		other := 0.0
		for _, d := range slices[7:] {
			other += d.Value.value
		}
		slices = append(append([]paseoPoint{}, slices[:7]...), paseoPoint{Label: paseoText(tt(c.l, "card_other")), Value: paseoNum{value: other, ok: true}})
	}
	s := wire.ChartSeries{}
	for _, d := range slices {
		s.Points = append(s.Points, wire.ChartPoint{Label: cut(d.Label.String(), 40), Value: d.Value.value, Display: cut(d.Value.text, 40)})
	}
	center := ""
	if kind == wire.ChartKindDonut && c.p.Center != nil {
		value, label := c.p.Center.Value.String(), c.p.Center.Label.String()
		for _, try := range []string{strings.TrimSpace(value + " " + label), value} {
			if try != "" && utf8.RuneCountInString(try) <= 20 {
				center = try
				break
			}
		}
	}
	caption := c.p.Caption.String()
	c.captioned = true
	if caption != "" {
		c.words = append(c.words, caption)
	}
	c.add(wire.Component{Type: wire.ComponentChart, Kind: kind, Series: []wire.ChartSeries{s}, Unit: cut(unit, 2000),
		Center: center, Detail: cut(caption, 2000)})
	return true
}

func (c *paseoCard) stats() bool {
	var stats []wire.Stat
	for _, raw := range c.p.Items {
		var it struct {
			Label paseoText `json:"label"`
			Value paseoText `json:"value"`
			Delta paseoText `json:"delta"`
			Note  paseoText `json:"note"`
			Good  string    `json:"good"`
			Tone  string    `json:"tone"`
		}
		if json.Unmarshal(raw, &it) != nil || it.Label.String() == "" {
			continue
		}
		st := wire.Stat{Label: it.Label.String(), Value: firstOf(it.Value.String(), "—")}
		delta := it.Delta.String()
		var detail []string
		if delta != "" {
			arrow, sign := "", 0
			switch {
			case strings.HasPrefix(delta, "-") || strings.HasPrefix(delta, "−"):
				arrow, sign = "↓ ", -1
			case strings.HasPrefix(delta, "+"):
				arrow, sign = "↑ ", 1
			default:
				if f, ok := numberIn(delta); ok && f != 0 {
					arrow, sign = "↑ ", 1
				}
			}
			if strings.EqualFold(it.Good, "down") {
				sign = -sign
			}
			switch sign {
			case 1:
				st.Tone = wire.TonePositive
			case -1:
				st.Tone = wire.ToneDanger
			}
			detail = append(detail, arrow+delta)
		}
		if note := it.Note.String(); note != "" {
			detail = append(detail, note)
		}
		st.Detail = strings.Join(detail, " ")
		if tone, _ := paseoTone(it.Tone); tone != "" {
			st.Tone = tone
		}
		stats = append(stats, st)
		c.words = append(c.words, strings.TrimSpace(st.Label+": "+st.Value+" "+st.Detail))
	}
	// A grid holds six: more go in two even grids.
	per := len(stats)
	if per > 6 {
		per = (per + 1) / 2
	}
	for len(stats) > 0 {
		n := min(per, len(stats))
		c.add(wire.Component{Type: wire.ComponentStatGrid, Stats: stats[:n]})
		stats = stats[n:]
	}
	return true
}

func (c *paseoCard) progress() bool {
	unit := c.p.Unit.String()
	for _, raw := range c.p.Items {
		var it struct {
			Label paseoText `json:"label"`
			Value paseoNum  `json:"value"`
			Max   paseoNum  `json:"max"`
			Note  paseoText `json:"note"`
			Tone  string    `json:"tone"`
		}
		if json.Unmarshal(raw, &it) != nil || !it.Value.ok {
			continue
		}
		most := 100.0
		if it.Max.ok && it.Max.value > 0 {
			most = it.Max.value
		}
		ratio := min(max(it.Value.value/most, 0), 1)
		amount := strconv.FormatFloat(ratio*100, 'f', 0, 64) + "%"
		if it.Max.ok && it.Max.value > 0 {
			amount = numberWords(it.Value, "") + " / " + numberWords(it.Max, unit)
		}
		detail := strings.TrimSpace(amount + " " + it.Note.String())
		label := firstOf(it.Label.String(), "—")
		p := wire.Component{Type: wire.ComponentProgress, Label: cut(label, 2000), Ratio: &ratio, Detail: cut(detail, 2000)}
		if ratio >= 1 {
			p.Tone = wire.TonePositive
		}
		if tone, _ := paseoTone(it.Tone); tone != "" {
			p.Tone = tone
		}
		c.add(p)
		c.words = append(c.words, label+": "+detail)
	}
	return true
}

func (c *paseoCard) checklist() bool {
	for _, raw := range c.p.Items {
		var text, state, note string
		var line string
		if json.Unmarshal(raw, &line) == nil {
			text, state = checkboxLine(line)
		} else {
			var it struct {
				Text  paseoText `json:"text"`
				Title paseoText `json:"title"`
				Label paseoText `json:"label"`
				State string    `json:"state"`
				Done  *bool     `json:"done"`
				Note  paseoText `json:"note"`
			}
			if json.Unmarshal(raw, &it) != nil {
				continue
			}
			text, state, note = firstOf(it.Text.String(), it.Title.String(), it.Label.String()), strings.ToLower(it.State), it.Note.String()
			if state == "" && it.Done != nil {
				state = map[bool]string{true: "done", false: "todo"}[*it.Done]
			}
		}
		if text == "" {
			continue
		}
		c.stateRow(text, note, "", firstOf(state, "todo"))
	}
	return true
}

// checkboxLine is a checklist string's text and state: "[x] done", "[~] doing", "[ ] todo", "[-] skipped", "[!] blocked".
func checkboxLine(line string) (string, string) {
	line = strings.TrimSpace(line)
	for mark, state := range map[string]string{"[x]": "done", "[X]": "done", "[~]": "doing", "[ ]": "todo", "[-]": "skipped", "[!]": "blocked"} {
		if rest, ok := strings.CutPrefix(line, mark); ok {
			return strings.TrimSpace(rest), state
		}
	}
	return line, "todo"
}

func (c *paseoCard) steps() bool {
	n := 0
	for _, raw := range append(c.p.Items, c.p.Steps...) {
		var it struct {
			Title  paseoText `json:"title"`
			Text   paseoText `json:"text"`
			State  string    `json:"state"`
			Time   paseoText `json:"time"`
			Detail paseoText `json:"detail"`
		}
		if json.Unmarshal(raw, &it) != nil {
			var line string
			if json.Unmarshal(raw, &line) != nil {
				continue
			}
			it.Title = paseoText(line)
		}
		title := firstOf(it.Title.String(), it.Text.String())
		if title == "" {
			continue
		}
		n++
		c.stateRow(strconv.Itoa(n)+". "+title, it.Detail.String(), it.Time.String(), strings.ToLower(it.State))
	}
	return true
}

// stateRow is a checklist item or a step: its words, a note under them, a time on the right and its state as a badge.
func (c *paseoCard) stateRow(title, note, value, state string) {
	row := wire.Component{Type: wire.ComponentRow, Title: cut(title, 2000), Subtitle: cut(note, 2000), Value: cut(value, 2000)}
	tones := map[string]string{"done": wire.TonePositive, "doing": wire.ToneCaution, "blocked": wire.ToneDanger,
		"todo": wire.ToneNeutral, "skipped": wire.ToneNeutral}
	words := ""
	if tone, ok := tones[state]; ok {
		words = tt(c.l, "paseo_ui_state_"+state)
		row.Badge = &wire.Badge{Text: words, Tone: tone}
	}
	c.add(row)
	line := title
	if words != "" {
		line = "[" + words + "] " + title
	}
	for _, s := range []string{value, note} {
		if s != "" {
			line += " · " + s
		}
	}
	c.words = append(c.words, line)
}

func (c *paseoCard) buttons() bool {
	var rows []wire.Component
	var words []string
	for _, btn := range c.p.Buttons {
		label := btn.Label.String()
		if label == "" {
			continue
		}
		row := wire.Component{Type: wire.ComponentRow, Title: cut(label, 2000)}
		if msg := btn.Message.String(); msg != "" && msg != label {
			row.Subtitle = cut(msg, 2000)
		}
		rows = append(rows, row)
		words = append(words, "· "+label)
	}
	if len(rows) == 0 {
		return false
	}
	reply := tt(c.l, "paseo_ui_reply_with")
	c.add(wire.Component{Type: wire.ComponentText, Text: reply, Style: wire.TextStyleSecondary})
	c.add(rows...)
	c.words = append(append(c.words, reply), words...)
	return true
}

// paseoTone is a Paseo tone name as a View tone, or else a named color; accent, info, special and unknown names are
// the app's own colors.
func paseoTone(name string) (tone, color string) {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "success":
		return wire.TonePositive, ""
	case "warning":
		return wire.ToneCaution, ""
	case "danger":
		return wire.ToneDanger, ""
	case "neutral":
		return wire.ToneNeutral, ""
	case "blue", "sky", "cyan":
		return "", wire.ColorBlue
	case "teal":
		return "", "teal"
	case "green", "lime":
		return "", "green"
	case "amber":
		return "", wire.ColorAmber
	case "orange":
		return "", "orange"
	case "red":
		return "", wire.ColorRed
	case "pink", "fuchsia":
		return "", "pink"
	case "violet":
		return "", wire.ColorPurple
	case "indigo":
		return "", "indigo"
	case "gray":
		return "", wire.ColorGray
	}
	return "", ""
}

// numberWords is a value as the fallback writes it: as it was written, else the number with the unit.
func numberWords(n paseoNum, unit string) string {
	if n.text != "" {
		return n.text
	}
	s := strconv.FormatFloat(n.value, 'f', -1, 64)
	if unit != "" {
		s += " " + unit
	}
	return s
}

// cellText is a JSON value as a table cell: a string as it is, a number as written, true ✓ and false ✗, null empty,
// anything else its JSON.
func cellText(raw []byte) string {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	switch raw[0] {
	case '"':
		var s string
		if json.Unmarshal(raw, &s) == nil {
			return s
		}
	case 't':
		return "✓"
	case 'f':
		return "✗"
	}
	return string(raw)
}

// relaxedJSON is raw without the comments and trailing commas Paseo tolerates; strings are left as they are.
func relaxedJSON(raw []byte) []byte {
	if json.Valid(raw) {
		return raw
	}
	out := make([]byte, 0, len(raw))
	for i := 0; i < len(raw); i++ {
		ch := raw[i]
		switch {
		case ch == '"':
			j := i + 1
			for j < len(raw) && raw[j] != '"' {
				if raw[j] == '\\' {
					j++
				}
				j++
			}
			end := min(j+1, len(raw))
			out = append(out, raw[i:end]...)
			i = end - 1
		case ch == '/' && i+1 < len(raw) && raw[i+1] == '/':
			for i < len(raw) && raw[i] != '\n' {
				i++
			}
			i--
		case ch == '/' && i+1 < len(raw) && raw[i+1] == '*':
			end := bytes.Index(raw[i+2:], []byte("*/"))
			if end < 0 {
				return out
			}
			i += end + 3
		case ch == ',':
			j := i + 1
			for j < len(raw) && (raw[j] == ' ' || raw[j] == '\t' || raw[j] == '\n' || raw[j] == '\r') {
				j++
			}
			if j < len(raw) && (raw[j] == '}' || raw[j] == ']') {
				continue
			}
			out = append(out, ch)
		default:
			out = append(out, ch)
		}
	}
	return out
}

// cut is s in at most n characters, the last one an ellipsis when it was longer.
func cut(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return string([]rune(s)[:n-1]) + "…"
}

// firstOf is the first of ss that is not empty.
func firstOf(ss ...string) string {
	for _, s := range ss {
		if s != "" {
			return s
		}
	}
	return ""
}

func nonEmpty(s string) []string {
	if s == "" {
		return nil
	}
	return []string{s}
}

// containsFoldASCII reports whether s contains the lowercase ASCII word, in any letter case.
func containsFoldASCII(s, word string) bool {
	for at := 0; ; {
		n := strings.IndexAny(s[at:], word[:1]+strings.ToUpper(word[:1]))
		if n < 0 || at+n+len(word) > len(s) {
			return false
		}
		if strings.EqualFold(s[at+n:at+n+len(word)], word) {
			return true
		}
		at += n + 1
	}
}
