package subscriptionsui

import (
	"cmp"

	"github.com/nwverse/persona/sdk/go/wire"
	"github.com/nwverse/persona/server/internal/i18n"
	"github.com/nwverse/persona/server/internal/subscriptions"
)

// cached is the cached catalog source a subscription or a page names: nil when the catalog has not been read yet.
func (p *pages) cached(connector, source string) (*subscriptions.Catalog, *subscriptions.CatalogSource) {
	cats, err := p.svc.Catalogs(p.ctx)
	if err != nil {
		return nil, nil
	}
	for i := range cats {
		if cats[i].Connector != connector {
			continue
		}
		for j := range cats[i].Sources {
			if cats[i].Sources[j].ID == source {
				return &cats[i], &cats[i].Sources[j]
			}
		}
		return &cats[i], nil
	}
	return nil, nil
}

func subscribable(src subscriptions.CatalogSource) int {
	n := 0
	for _, t := range src.Tools {
		if t.Subscribable {
			n++
		}
	}
	return n
}

func (p *pages) gone() wire.View {
	return wire.View{Title: p.t("title.list"), Sections: []wire.Section{{Items: []wire.Component{
		{Type: "empty", Key: "gone", Title: p.t("gone.title"), Detail: p.t("gone.detail"), Icon: p.icon("info")}}}}}
}

func (p *pages) failed(title string, err error) wire.View {
	return wire.View{Title: title, Sections: []wire.Section{{Items: []wire.Component{
		{Type: "empty", Key: "failed", Title: p.t("failed.title"), Detail: p.t("failed.detail"), Icon: "warning", IconColor: "red"}}}}}
}

// confirm is what a destination's switch asks before it turns on: what, how often, where it is kept, which model.
func (p *pages) confirm(dest, label, kind, every string, push bool) string {
	purpose := "inbox.triage" // Information filtering triages the Inbox's items
	if dest == "to_kb" {
		purpose = "kb" // Information processing summarizes the knowledge base's
	}
	provider := ""
	if m := p.modelOf(purpose); m.OK && m.Provider != "" {
		provider = p.t("confirm.provider", i18n.Args{"name": modelName(m)})
	}
	key := "confirm." + dest
	if push {
		key = "confirm.push_" + dest // nothing is pulled: what a shortcut sends is kept as it comes
	}
	return p.t(key, i18n.Args{"source": label, "noun": p.noun(kind), "every": every, "provider": provider})
}

func (p *pages) switches(id, label, kind, every string, inbox, kb, enabled, push bool) wire.Section {
	sw := func(name string, on bool, subtitle string) wire.Component {
		r := row("d."+name, p.t("dest."+name))
		r.Subtitle = subtitle
		act := wire.Action{ID: "dest", Values: map[string]string{"to": name}}
		if !on {
			act.Confirm = p.confirm(name, label, kind, every, push)
		}
		r.Control = &wire.RowControl{Kind: "switch", Name: name, On: on, Disabled: !enabled, Action: act}
		return r
	}
	return wire.Section{Key: "dest", Title: p.t("source.dest"), Items: []wire.Component{
		sw("to_inbox", inbox, p.t("source.inbox_detail", i18n.Args{"noun": p.noun(kind)})),
		sw("to_kb", kb, p.t("source.kb_detail")),
	}}
}

// ② A source: its switches, how often, how it is doing, how it pulls and triages, and pull now / delete.
func (p *pages) source() wire.View {
	if p.st.ID == "" {
		return p.newSource()
	}
	s, _, err := p.svc.Get(p.ctx, p.st.ID)
	if err != nil {
		return p.gone()
	}
	if subscriptions.IsPush(s) {
		return p.pushSource(s)
	}
	_, src := p.cached(s.Connector, s.Source)
	v := wire.View{Title: s.Label, State: state{P: pageSource, ID: s.ID}.String()}
	if where := p.env.Native(s.Service); where != "" {
		v.Sections = append(v.Sections, wire.Section{Key: "native", Items: []wire.Component{
			text("native", p.t("source.native", i18n.Args{"where": where}), "secondary")}})
	}
	switch s.State {
	case subscriptions.StateFailing, subscriptions.StateNeedsRecipe, subscriptions.StateRateLimited, subscriptions.StateNotConnected:
		v.Sections = append(v.Sections, p.trouble(s))
	}
	dest := p.switches(s.ID, s.Label, s.Recipe.Kind, p.schedule(s), s.ToInbox, s.ToKB, true, false)
	if s.Recipe.Mode == wire.ConnectorSubscriptionModeThings {
		dest.Items = dest.Items[:1] // its rows are the Inbox's only: the knowledge base reads the source itself (#6296)
	}
	if subscriptions.IsMigration(s.ID) {
		// A built-in's default subscription: switched on its built-in's page, its own switches kept off (#6345).
		r := row("d.to_inbox", p.t("dest.to_inbox"))
		r.Value = p.t("source.via_off")
		if s.Migrated != nil {
			r.Value = p.t("source.via_on")
		}
		r.Subtitle = p.t("source.via_detail", i18n.Args{"source": s.Label})
		r.Action = open(state{P: pageBuiltin, C: s.Connector}, s.Label)
		dest = wire.Section{Key: "dest", Title: p.t("source.dest"), Items: []wire.Component{r}}
	}
	v.Sections = append(v.Sections, dest)
	freq := row("f.interval", p.t("source.interval"))
	freq.Value = p.schedule(s)
	freq.Action = open(state{P: pageInterval, ID: s.ID}, p.t("source.interval"))
	freqSec := wire.Section{Key: "freq", Title: p.t("source.freq"), Items: []wire.Component{freq}}
	if s.MinIntervalSeconds > 300 {
		freqSec.Footer = p.t("source.min_interval", i18n.Args{"span": p.span(s.MinIntervalSeconds)})
	}
	v.Sections = append(v.Sections, freqSec)
	if s.State == subscriptions.StateOK {
		last, next := row("st.last", p.t("source.last")), row("st.next", p.t("source.next"))
		last.Value, next.Value = p.lastLine(s), p.soon(s.NextRunAt)
		runs := row("st.runs", p.t("source.runs"))
		runs.Action = open(state{P: pageRuns, ID: s.ID}, p.t("source.runs"))
		v.Sections = append(v.Sections, wire.Section{Key: "status", Title: p.t("source.status"), Items: []wire.Component{last, next, runs}})
	}
	recipe := row("h.recipe", p.t("source.recipe"))
	recipe.Subtitle = p.t("source.recipe_line", i18n.Args{"tool": toolTitle(src, s.Recipe.Tool), "origin": p.originShort(s.RecipeOrigin)})
	if s.Recipe.Mode == wire.ConnectorSubscriptionModeThings { // no tool name for people: what the rows are (#6296)
		recipe.Subtitle = p.t("source.recipe_line_things", i18n.Args{"origin": p.originShort(s.RecipeOrigin)})
	}
	recipe.Action = open(state{P: pageRecipe, ID: s.ID}, p.t("source.recipe"))
	rules := row("h.rules", p.t("source.rules"))
	rules.Subtitle = s.TriageRules
	if rules.Subtitle == "" {
		rules.Subtitle = p.t("source.rules_default")
	}
	rules.Action = open(state{P: pageRules, ID: s.ID}, p.t("source.rules"))
	fb := row("h.fallback", p.t("source.fallback"))
	fb.Value = p.t("tier." + cmp.Or(s.FallbackTier, "default"))
	fb.Action = open(state{P: pageFallback, ID: s.ID}, p.t("source.fallback"))
	how := wire.Section{Key: "how", Title: p.t("source.how"), Items: []wire.Component{recipe, rules, fb}}
	if p.env.Builtin != nil { // who is muted of it, by sender (#5306), as a built-in source's
		how.Items = append(how.Items, p.mutesRow(s.ID))
	}
	how.Items = append(how.Items, p.askRows("ask.line_subscription")...)
	v.Sections = append(v.Sections, how)
	v.Sections = append(v.Sections, wire.Section{Key: "try", Items: []wire.Component{p.previewPress()}})
	v.Sections = append(v.Sections, p.previewOf(s.Recipe.Kind, s.FallbackTier)...)
	del := button("b.delete", p.t("source.delete"), "delete")
	del.Role = "destructive"
	del.Action.Confirm = p.t("source.delete_confirm", i18n.Args{"source": s.Label})
	v.Sections = append(v.Sections, wire.Section{Key: "act", Items: []wire.Component{button("b.run", p.t("source.run_now"), "run_now"), del}})
	return v
}

// trouble is the section at the top of a source that is not pulling: what is wrong and when it tries again.
func (p *pages) trouble(s subscriptions.Subscription) wire.Section {
	sec := wire.Section{Key: "trouble"}
	r := row("e.now", "")
	r.Badge = p.stateBadge(s.State)
	var why string
	switch s.State {
	case subscriptions.StateFailing:
		sec.Title = p.t("trouble.failing_title")
		r.Title = p.t("trouble.failing", i18n.Args{"since": p.clock(s.FailingSince)})
		r.Subtitle = p.t("trouble.failing_line", i18n.Args{"problem": p.problemText(s.Error), "count": s.FailCount})
		r.Icon, r.IconColor = "warning", "red"
		why = p.t("trouble.failing_why", i18n.Args{"time": p.clock(s.NextRunAt)})
	case subscriptions.StateNeedsRecipe:
		sec.Title = p.t("trouble.paused_title")
		r.Title = p.t("trouble.needs_recipe")
		r.Subtitle = p.problemText(s.Error)
		r.Icon, r.IconColor = "warning", "orange"
		r.Action = open(state{P: pageRecipe, ID: s.ID}, p.t("source.recipe"))
		why = p.t("trouble.needs_recipe_why")
	case subscriptions.StateRateLimited:
		sec.Title = p.t("trouble.paused_title")
		r.Title = p.t("trouble.rate_limited", i18n.Args{"time": p.clock(s.PausedUntil)})
		r.Icon, r.IconColor = "clock", "orange"
		why = p.t("trouble.rate_limited_why")
	case subscriptions.StateNotConnected:
		sec.Title = p.t("trouble.paused_title")
		r.Title = p.t("trouble.not_connected")
		r.Icon, r.IconColor = "link", "gray"
		why = p.t("trouble.not_connected_why")
	}
	sec.Items = []wire.Component{r, text("e.why", why, "caption")}
	runs := row("e.runs", p.t("source.runs"))
	runs.Action = open(state{P: pageRuns, ID: s.ID}, p.t("source.runs"))
	sec.Items = append(sec.Items, runs)
	return sec
}

// ②b A source nobody subscribed to yet: its own recipe if the extension brings one, else pick a tool first.
func (p *pages) newSource() wire.View {
	cat, err := p.svc.Catalog(p.ctx, p.st.C)
	if err != nil {
		return p.failed(p.t("title.list"), err)
	}
	var src *subscriptions.CatalogSource
	for i := range cat.Sources {
		if cat.Sources[i].ID == p.st.S {
			src = &cat.Sources[i]
		}
	}
	if src == nil {
		return p.gone()
	}
	v := wire.View{Title: src.Name}
	if where := p.env.Native(src.Service); where != "" {
		v.Sections = append(v.Sections, wire.Section{Key: "native", Items: []wire.Component{
			text("native", p.t("source.native", i18n.Args{"where": where}), "secondary")}})
	}
	if cat.Direct.Inbox || cat.Direct.Archive {
		v.Sections = append(v.Sections, wire.Section{Key: "direct", Items: []wire.Component{text("direct", p.t("source.direct"), "secondary")}})
	}
	min := 300
	if src.MinIntervalSeconds != nil && *src.MinIntervalSeconds > min {
		min = *src.MinIntervalSeconds
	}
	every := p.every(max(900, min), "")
	kind := ""
	how := wire.Section{Key: "how", Title: p.t("source.recipe")}
	if len(src.Recipes) > 0 {
		r := src.Recipes[0]
		kind = r.Kind
		if r.Mode == wire.ConnectorSubscriptionModeSnapshot {
			every = p.t("every.snapshot_daily", i18n.Args{"time": "08:00"})
		}
		own := row("h.own", p.t("new.own"))
		own.Subtitle = p.t("source.recipe_line", i18n.Args{"tool": toolTitle(src, r.Tool), "origin": p.originShort(subscriptions.OriginExtension)})
		own.Icon, own.IconColor = "check", "green"
		other := row("h.pick", p.t("new.other"))
		other.Action = open(state{P: pageTools, C: p.st.C, S: p.st.S}, p.t("title.tools"))
		how.Items = append(append([]wire.Component{own}, p.askRows("ask.line_source")...), other)
	} else {
		how.Items = p.askRows("ask.line_source")
		pick := row("h.pick", p.t("new.pick"))
		pick.Subtitle = p.t("list.tools", i18n.Args{"count": subscribable(*src)})
		pick.Icon, pick.IconColor = "search", "blue"
		pick.Action = open(state{P: pageTools, C: p.st.C, S: p.st.S}, p.t("title.tools"))
		how.Items = append(how.Items, pick)
		how.Footer = p.t("new.pick_footer")
	}
	v.Sections = append(v.Sections, how)
	if len(src.Recipes) > 0 {
		v.Sections = append(v.Sections, wire.Section{Key: "try", Items: []wire.Component{p.previewPress()}})
		v.Sections = append(v.Sections, p.previewOf(kind, "")...)
	}
	v.Sections = append(v.Sections, p.switches("", src.Name, kind, every, false, false, len(src.Recipes) > 0, false))
	freq := row("f.interval", p.t("source.interval"))
	freq.Value = every
	v.Sections = append(v.Sections, wire.Section{Key: "freq", Title: p.t("source.freq"), Items: []wire.Component{freq},
		Footer: p.t("new.freq_footer")})
	return v
}

// ③ A recipe: what it does in words, its fields; a dry run on request, with its items and checks.
func (p *pages) recipe(dry *subscriptions.DryRun, dryErr error) wire.View {
	s, _, err := p.svc.Get(p.ctx, p.st.ID)
	if err != nil {
		return p.gone()
	}
	_, src := p.cached(s.Connector, s.Source)
	v := wire.View{Title: p.t("source.recipe"), State: state{P: pageRecipe, ID: s.ID}.String()}
	v.Sections = append(v.Sections, wire.Section{Key: "what", Title: p.t("recipe.what"), Items: []wire.Component{
		text("plain", p.plain(s, src), "body")}})
	examples := map[string]string{}
	if dry != nil && len(dry.Items) > 0 {
		it := dry.Items[0]
		for k, val := range map[string]string{"title": it.Title, "sender": it.Sender, "link": it.Link, "body": it.Body,
			"thread": it.Thread, "id": it.UID} {
			examples[k] = val
		}
		if it.Time != nil {
			examples["time"] = *it.Time
		}
		if it.DueDate != nil {
			examples["due_date"] = *it.DueDate
		}
	}
	cols := []wire.TableColumn{{Title: p.t("recipe.col_shown")}, {Title: p.t("recipe.col_from")}}
	if len(examples) > 0 {
		cols = append(cols, wire.TableColumn{Title: p.t("recipe.col_example")})
	}
	var rows [][]string
	for _, kv := range fieldRows(s.Recipe.Fields) {
		r := []string{p.t("field." + kv[0]), shortPath(kv[1])}
		if len(examples) > 0 {
			r = append(r, examples[kv[0]])
		}
		rows = append(rows, r)
	}
	if len(rows) > 0 {
		title := p.t("recipe.fields")
		if len(examples) > 0 {
			title = p.t("recipe.fields_example")
		}
		v.Sections = append(v.Sections, wire.Section{Key: "map", Title: title, Items: []wire.Component{
			{Type: "table", Key: "fields", Columns: cols, Rows: rows}}})
	}
	switch {
	case dryErr != nil:
		v.Sections = append(v.Sections, wire.Section{Key: "dry", Title: p.t("dry.title"), Items: []wire.Component{
			text("dry.err", p.t("dry.failed"), "secondary")}})
	case dry != nil:
		v.Sections = append(v.Sections, p.preview(dry, s.Recipe.Kind, s.FallbackTier)...)
	}
	try := button("b.dry", p.t("recipe.dry_run"), "dry_run")
	edit := row("x.tool", p.t("recipe.edit"))
	edit.Action = open(state{P: pageSetup, ID: s.ID, C: s.Connector, S: s.Source, T: s.Recipe.Tool}, p.t("recipe.edit_title"))
	other := row("x.tools", p.t("recipe.other_tool"))
	other.Action = open(state{P: pageTools, ID: s.ID, C: s.Connector, S: s.Source}, p.t("title.tools"))
	raw := row("x.raw", p.t("recipe.raw"))
	raw.Action = open(state{P: pageRaw, ID: s.ID}, p.t("recipe.raw"))
	edits := append(p.askRows("ask.line_recipe"), edit, other, raw)
	switch {
	case subscriptions.IsMigration(s.ID): // a built-in's default: its rows must stay the built-in's, so only tried (#6345)
		edits = nil
	case s.Recipe.Mode == wire.ConnectorSubscriptionModeThings:
		edits = append(p.askRows("ask.line_recipe"), other, raw) // the setup form maps fields, which a things recipe has none of (#6296)
	}
	v.Sections = append(v.Sections, wire.Section{Key: "edit", Footer: p.origin(s) + " " + p.t("recipe.no_model"),
		Items: append([]wire.Component{try}, edits...)})
	return v
}

// ④ Pick a tool: the ones that can be subscribed, then the ones that can't and why.
func (p *pages) tools() wire.View {
	v := wire.View{Title: p.t("title.tools")}
	cat, err := p.svc.Catalog(p.ctx, p.st.C)
	if err != nil {
		return p.failed(v.Title, err)
	}
	var src *subscriptions.CatalogSource
	for i := range cat.Sources {
		if cat.Sources[i].ID == p.st.S {
			src = &cat.Sources[i]
		}
	}
	if src == nil {
		return p.gone()
	}
	v.Sections = append(v.Sections, wire.Section{Items: []wire.Component{text("intro", p.t("tools.intro"), "secondary")}})
	ok := wire.Section{Key: "ok", Title: p.t("tools.ok")}
	no := wire.Section{Key: "no", Title: p.t("tools.no"), Footer: p.t("tools.no_footer")}
	for _, t := range src.Tools {
		r := row("t."+t.Name, cmp.Or(t.Title, t.Name))
		if t.Subscribable {
			r.Subtitle = join([]string{t.Name, t.Description})
			r.Action = open(state{P: pageSetup, ID: p.st.ID, C: p.st.C, S: p.st.S, T: t.Name}, r.Title)
			ok.Items = append(ok.Items, r)
		} else {
			r.Subtitle = join([]string{t.Name, p.problemText(t.Refused)})
			no.Items = append(no.Items, r)
		}
	}
	if len(ok.Items) > 0 {
		v.Sections = append(v.Sections, ok)
	}
	if len(no.Items) > 0 {
		v.Sections = append(v.Sections, no)
	}
	return v
}

// ⑥ Runs: the last twenty, newest first.
func (p *pages) runs() wire.View {
	v := wire.View{Title: p.t("source.runs")}
	s, runs, err := p.svc.Get(p.ctx, p.st.ID)
	if err != nil {
		return p.gone()
	}
	if len(runs) == 0 {
		v.Sections = []wire.Section{{Items: []wire.Component{{Type: "empty", Key: "none", Title: p.t("runs.none"),
			Detail: p.t("runs.none_detail"), Icon: "clock"}}}}
		return v
	}
	okN, failN, newN := 0, 0, 0
	list := wire.Section{Key: "runs", Title: p.t("runs.each"), Footer: p.t("runs.footer")}
	if subscriptions.IsPush(s) {
		list.Footer = p.t("runs.push_footer") // nothing is pulled: no rounds skipped, no rate limit
	}
	for i, r := range runs {
		c := row("r."+itoa(i), p.t("runs.when", i18n.Args{"time": p.clock(&r.At), "trigger": p.t("trigger." + r.Trigger)}))
		pushed := r.Trigger == subscriptions.TriggerPush // kept as it came: no call to time
		if r.Error != nil {
			failN++
			c.Subtitle = join([]string{p.problemText(r.Error), p.took(r.MS, pushed)})
			c.Badge = badge(p.t("badge.failing"), "danger")
		} else {
			okN++
			newN += r.New
			if r.New > 0 {
				c.Subtitle = join([]string{p.t("runs.new", i18n.Args{"count": r.Count, "new": r.New}), p.took(r.MS, pushed)})
				c.Badge = badge("+"+itoa(r.New), "positive")
			} else {
				c.Subtitle = join([]string{p.t("runs.none_new", i18n.Args{"count": r.Count}), p.took(r.MS, pushed)})
			}
		}
		list.Items = append(list.Items, c)
	}
	stats := []wire.Stat{{Label: p.t("runs.ok"), Value: itoa(okN)}, {Label: p.t("runs.new_items"), Value: itoa(newN)},
		{Label: p.t("runs.failed"), Value: itoa(failN)}}
	if failN > 0 {
		stats[2].Tone = "danger"
	}
	v.Sections = []wire.Section{{Key: "sum", Title: p.t("runs.summary", i18n.Args{"count": len(runs)}),
		Items: []wire.Component{{Type: "stat_grid", Key: "stats", Stats: stats}}}, list}
	return v
}

// intervals offered, in seconds; snapshots also offer a time of day.
var (
	intervals = []int{300, 600, 900, 1800, 3600, 10800, 21600, 43200, 86400}
	dailyAts  = []string{"06:00", "08:00", "12:00", "18:00", "21:00"}
)

// ⑦ How often: pick one and it applies at once (the page stays, the choice moves).
func (p *pages) interval() wire.View {
	s, _, err := p.svc.Get(p.ctx, p.st.ID)
	if err != nil {
		return p.gone()
	}
	v := wire.View{Title: p.t("source.interval")}
	sec := wire.Section{Key: "pick"}
	chosen := func(r *wire.Component, on bool) {
		if on {
			r.Badge = badge(p.t("pick.current"), "positive")
		}
	}
	snapshot := s.Recipe.Mode == wire.ConnectorSubscriptionModeSnapshot
	if snapshot {
		for _, at := range dailyAts {
			r := row("i.at."+at, p.t("every.daily_at", i18n.Args{"time": at}))
			r.Action = &wire.Action{ID: "interval", Values: map[string]string{"daily_at": at}}
			chosen(&r, s.DailyAt == at)
			sec.Items = append(sec.Items, r)
		}
	}
	for _, sec2 := range intervals {
		if sec2 < s.MinIntervalSeconds || snapshot && sec2 < 3600 {
			continue
		}
		r := row("i."+itoa(sec2), p.every(sec2, ""))
		if sec2 == max(900, s.MinIntervalSeconds) && !snapshot {
			r.Subtitle = p.t("pick.default")
		}
		r.Action = &wire.Action{ID: "interval", Values: map[string]string{"seconds": itoa(sec2)}}
		chosen(&r, s.DailyAt == "" && s.IntervalSeconds == sec2)
		sec.Items = append(sec.Items, r)
	}
	sec.Footer = p.t("pick.interval_footer", i18n.Args{"span": p.span(max(300, s.MinIntervalSeconds))})
	v.Sections = []wire.Section{sec}
	return v
}

// ⑧ Without a triage model: where new items go.
func (p *pages) fallback() wire.View {
	s, _, err := p.svc.Get(p.ctx, p.st.ID)
	if err != nil {
		return p.gone()
	}
	sec := wire.Section{Key: "pick", Footer: p.t("pick.fallback_footer")}
	for _, tier := range []string{"", "fyi", "action", "wait"} {
		r := row("t."+cmp.Or(tier, "default"), p.t("tier."+cmp.Or(tier, "default")))
		r.Subtitle = p.t("tier_detail." + cmp.Or(tier, "default"))
		r.Action = &wire.Action{ID: "fallback", Values: map[string]string{"tier": tier}}
		if s.FallbackTier == tier {
			r.Badge = badge(p.t("pick.current"), "positive")
		}
		sec.Items = append(sec.Items, r)
	}
	return wire.View{Title: p.t("source.fallback"), Sections: []wire.Section{sec}}
}

// ⑨ Triage rules: free text, this subscription only (or a built-in source's own).
func (p *pages) rules() wire.View {
	if p.st.ID == "" && p.st.C != "" {
		return p.builtinRulesPage()
	}
	s, _, err := p.svc.Get(p.ctx, p.st.ID)
	if err != nil {
		return p.gone()
	}
	field := wire.Component{Type: "field", Key: "r.text", Name: "triage_rules", Label: p.t("rules.label"), Kind: "multiline",
		Value: s.TriageRules, Placeholder: p.t("rules.placeholder")}
	save := button("b.save", p.t("rules.save"), "rules")
	save.Action.Submit = true
	return wire.View{Title: p.t("source.rules"), Sections: []wire.Section{
		{Key: "rules", Footer: p.t("rules.footer"), Items: []wire.Component{field}},
		{Key: "save", Items: []wire.Component{save}},
	}}
}

// modelName is a model as the consents name it: its connection, and the model when the connection has several.
func modelName(m subscriptions.ModelInfo) string {
	if m.Model == "" || m.Model == m.Provider {
		return m.Provider
	}
	return m.Provider + " · " + m.Model
}
