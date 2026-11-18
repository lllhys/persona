package subscriptionsui

import (
	"cmp"
	"slices"

	"github.com/nwverse/persona/sdk/go/wire"
	"github.com/nwverse/persona/server/internal/i18n"
	"github.com/nwverse/persona/server/internal/subscriptions"
)

// ① Inbox Sources (#5278): everything that brings new things into the Inbox or the knowledge base. What needs
// attention first, then what Persona reads itself, the subscriptions, the recommended ones a plugin brings, and the
// way to more. {"ext": id} or {"c": id} narrows it to one extension or connector (its page's Subscriptions row).
// ownRows are the subscriptions that are rows of their own: not a built-in's default one, which its built-in's page
// shows (#6345), so a source is listed once.
func ownRows(subs []subscriptions.Subscription) []subscriptions.Subscription {
	return slices.DeleteFunc(slices.Clone(subs), func(s subscriptions.Subscription) bool { return subscriptions.IsMigration(s.ID) })
}

func (p *pages) list() wire.View {
	filtered := p.st.C != "" || p.st.Ext != ""
	v := wire.View{Title: p.t("title.sources")}
	if filtered {
		v.Title = p.t("title.list")
	}
	subs, _, err := p.svc.List(p.ctx, subscriptions.Filter{Connector: p.st.C, Extension: p.st.Ext})
	if err != nil {
		return p.failed(v.Title, err)
	}
	subs = ownRows(subs)
	intro := "list.intro_sources"
	if filtered {
		intro = "list.intro"
	}
	v.Sections = append(v.Sections, wire.Section{Items: []wire.Component{text("intro", p.t(intro), "secondary")}})
	if p.env.Graph && !filtered {
		// The app's own relationship view (#5741): what brings each source, how it is read, where it goes.
		r := row("x.graph", p.t("graph.row"))
		r.Subtitle, r.Icon, r.IconColor = p.t("graph.row_detail"), p.icon("link"), "blue"
		r.Action = &wire.Action{ID: "graph"}
		v.Sections = append(v.Sections, wire.Section{Key: "graph", Items: []wire.Component{r}})
	}
	all := subs
	if filtered { // a duplicate is told by any subscription, not only this connector's
		all, _, _ = p.svc.List(p.ctx, subscriptions.Filter{})
	}
	var attention, builtin, on, off []wire.Component
	for _, b := range p.builtins() {
		if p.st.C != "" && b.ID != p.st.C || p.st.Ext != "" && b.Extension != p.st.Ext {
			continue
		}
		r := p.builtinRow(b, all)
		if p.builtinTrouble(b) != "" {
			attention = append(attention, r)
		} else {
			builtin = append(builtin, r)
		}
	}
	for _, s := range subs {
		r := row("s."+s.ID, s.Label)
		r.Icon, r.IconColor = p.icon(s.Icon), colorFor(s.Connector+"/"+s.Source)
		r.Subtitle, r.Badge = p.stateLine(s), p.stateBadge(s.State)
		if _, where := p.native(s.Service); where != "" && r.Badge == nil && s.State != subscriptions.StateOff {
			r.Badge = badge(p.t("badge.maybe_duplicate"), "caution")
		}
		r.Action = open(state{P: pageSource, ID: s.ID}, s.Label)
		switch s.State {
		case subscriptions.StateFailing, subscriptions.StateNeedsRecipe:
			attention = append(attention, r)
		case subscriptions.StateOff:
			off = append(off, r)
		default:
			on = append(on, r)
		}
	}
	add := func(key, title string, items []wire.Component) {
		if len(items) > 0 {
			v.Sections = append(v.Sections, wire.Section{Key: key, Title: title, Items: items})
		}
	}
	add("attention", p.t("list.attention"), attention)
	add("builtin", p.t("list.builtin"), builtin)
	if p.env.Builtin != nil {
		if len(builtin) > 0 {
			v.Sections[len(v.Sections)-1].Footer = p.t("list.builtin_footer")
		}
		add("on", p.t("list.subs_on"), on)
		add("off", p.t("list.subs_off"), off)
	} else {
		add("on", p.t("list.on"), on)
		add("off", p.t("list.off"), off)
	}
	cats, _ := p.svc.Catalogs(p.ctx)
	slices.SortFunc(cats, func(a, b subscriptions.Catalog) int {
		return cmp.Compare(p.env.Name(a.Connector), p.env.Name(b.Connector))
	})
	recommended := p.recommended(cats)
	add("recommended", p.t("list.recommended"), recommended)
	if len(recommended) > 0 {
		v.Sections[len(v.Sections)-1].Footer = p.t("list.recommended_footer")
	}
	available := len(recommended)
	// This device's Shortcuts source (#3848) before the connectors' tools: it is the one source of its own.
	if !filtered {
		if sec, ok := p.pushSection(subs); ok {
			v.Sections = append(v.Sections, sec)
			available++
		}
	}
	more := p.available(cats)
	for _, sec := range more {
		available += len(sec.Items)
	}
	switch {
	case filtered:
		v.Sections = append(v.Sections, more...) // one connector's: few enough to list here
	case len(more) > 0:
		r := row("more", p.t("list.more"))
		r.Subtitle, r.Icon, r.IconColor = p.t("list.more_detail"), "search", "blue"
		r.Action = open(state{P: pageMore}, p.t("list.more"))
		v.Sections = append(v.Sections, wire.Section{Key: "more", Items: []wire.Component{r}})
	}
	if len(subs) == 0 && available == 0 && len(attention)+len(builtin) == 0 {
		v.Sections = append(v.Sections, wire.Section{Key: "none", Items: []wire.Component{
			{Type: "empty", Key: "none", Title: p.t("list.none_title"), Detail: p.t("list.none_detail"), Icon: "bell"}}})
	} else if n := len(v.Sections); n > 0 && v.Sections[n-1].Footer == "" {
		v.Sections[n-1].Footer = p.t("list.footer")
	}
	return v
}

// recommendable says a catalog source is one to recommend: its extension brings a recipe for it, nobody subscribed
// to it, and it has a tool to pull with.
func recommendable(src subscriptions.CatalogSource) bool {
	return len(src.Recipes) > 0 && len(src.Subscriptions) == 0 && subscribable(src) > 0
}

// recommended are the rows of the sources a plugin brings its own recipe for (#5278 phase 2): off until the user
// turns one on, and the switch asks first what it pulls, how often, where it keeps it and which model triages it.
func (p *pages) recommended(cats []subscriptions.Catalog) []wire.Component {
	var out []wire.Component
	for _, c := range cats {
		if p.st.C != "" && c.Connector != p.st.C || p.st.Ext != "" && c.Extension != p.st.Ext {
			continue
		}
		for _, src := range c.Sources {
			if !recommendable(src) {
				continue
			}
			r := row("r."+c.Connector+"/"+src.ID, src.Name)
			r.Icon, r.IconColor = p.icon(src.Icon), colorFor(c.Connector+"/"+src.ID)
			rec := src.Recipes[0]
			every := p.newEvery(src, rec)
			r.Subtitle = p.t("list.recommended_line", i18n.Args{"name": p.env.Name(c.Connector), "every": every, "noun": p.noun(rec.Kind)})
			if _, where := p.native(src.Service); where != "" {
				r.Subtitle, r.Badge = p.t("list.native"), badge(p.t("badge.maybe_duplicate"), "caution")
			}
			act := wire.Action{ID: "recommend", Values: map[string]string{"c": c.Connector, "s": src.ID},
				Confirm: p.recommendConfirm(src.Name, rec.Kind, every)}
			r.Control = &wire.RowControl{Kind: "switch", Name: recommendName(c.Connector, src.ID), Action: act}
			r.Action = open(state{P: pageSource, C: c.Connector, S: src.ID}, src.Name)
			out = append(out, r)
		}
	}
	return out
}

// newEvery is how often a source nobody subscribed to yet would pull with recipe r: every 15 minutes, or its minimum
// when longer; a snapshot daily at 08:00.
func (p *pages) newEvery(src subscriptions.CatalogSource, r subscriptions.Recipe) string {
	if r.Mode == wire.ConnectorSubscriptionModeSnapshot {
		return p.t("every.snapshot_daily", i18n.Args{"time": "08:00"})
	}
	least := 300
	if src.MinIntervalSeconds != nil && *src.MinIntervalSeconds > least {
		least = *src.MinIntervalSeconds
	}
	return p.every(max(900, least), "")
}

// recommendConfirm is what turning a recommended source on asks: the Inbox's consent, and that each new item is one
// call to the triage model.
func (p *pages) recommendConfirm(source, kind, every string) string {
	provider := ""
	if m := p.modelOf("inbox.triage"); m.OK && m.Provider != "" {
		provider = p.t("confirm.provider", i18n.Args{"name": modelName(m)})
	}
	return p.t("confirm.recommend", i18n.Args{"source": source, "noun": p.noun(kind), "every": every, "provider": provider})
}

// available are the sections of sources that can be subscribed to by choosing a tool: per connector, the ones with
// a subscribable tool, no subscription yet and nothing recommended for them; from cached catalogs only.
func (p *pages) available(cats []subscriptions.Catalog) []wire.Section {
	var out []wire.Section
	for _, c := range cats {
		if p.st.C != "" && c.Connector != p.st.C || p.st.Ext != "" && c.Extension != p.st.Ext {
			continue
		}
		var items []wire.Component
		for _, src := range c.Sources {
			n := subscribable(src)
			if n == 0 || len(src.Subscriptions) > 0 || recommendable(src) {
				continue
			}
			r := row("a."+c.Connector+"/"+src.ID, src.Name)
			r.Icon, r.IconColor = p.icon(src.Icon), colorFor(c.Connector+"/"+src.ID)
			r.Subtitle = p.t("list.tools", i18n.Args{"count": n})
			if _, where := p.native(src.Service); where != "" {
				r.Subtitle, r.Badge = p.t("list.native"), badge(p.t("badge.duplicate"), "caution")
			}
			r.Action = open(state{P: pageSource, C: c.Connector, S: src.ID}, src.Name)
			items = append(items, r)
		}
		if len(items) > 0 {
			out = append(out, wire.Section{Key: "avail." + c.Connector,
				Title: p.t("list.available", i18n.Args{"name": p.env.Name(c.Connector)}), Items: items})
		}
	}
	return out
}

// ①b More to subscribe: every connector's sources that can be subscribed to by choosing a tool.
func (p *pages) more() wire.View {
	v := wire.View{Title: p.t("list.more")}
	cats, err := p.svc.Catalogs(p.ctx)
	if err != nil {
		return p.failed(v.Title, err)
	}
	slices.SortFunc(cats, func(a, b subscriptions.Catalog) int {
		return cmp.Compare(p.env.Name(a.Connector), p.env.Name(b.Connector))
	})
	v.Sections = append(v.Sections, wire.Section{Items: []wire.Component{text("intro", p.t("list.intro"), "secondary")}})
	more := p.available(cats)
	if len(more) == 0 {
		v.Sections = append(v.Sections, wire.Section{Key: "none", Items: []wire.Component{
			{Type: "empty", Key: "none", Title: p.t("list.none_title"), Detail: p.t("list.none_detail"), Icon: "bell"}}})
		return v
	}
	more[len(more)-1].Footer = p.t("list.more_footer")
	v.Sections = append(v.Sections, more...)
	return v
}

// recommend turns a recommended source on (the switch asked first): the subscription with its extension's own recipe,
// Add to Inbox on. The list again, with it under Subscriptions.
func (p *pages) recommend(values map[string]string) (wire.ActionResult, error) {
	c, src := values["c"], values["s"]
	if c == "" || src == "" {
		return wire.ActionResult{}, errBadValues
	}
	if values[recommendName(c, src)] == "false" { // the switch's new value: off asks nothing and does nothing
		v := p.draw()
		return wire.ActionResult{View: &v}, nil
	}
	s, err := p.svc.Create(p.ctx, subscriptions.CreateRequest{Connector: c, Source: src}, p.env.By)
	if err != nil {
		return p.errorResult(err)
	}
	on := true
	if _, err := p.svc.Update(p.ctx, s.ID, subscriptions.Patch{Revision: s.Revision, ToInbox: &on}, p.env.By); err != nil {
		return p.errorResult(err)
	}
	v := p.draw()
	return wire.ActionResult{View: &v, Toast: p.t("toast.recommended", i18n.Args{"source": s.Label})}, nil
}

// recommendName is a recommended row's switch: one name a row, as a page's controls need.
func recommendName(connector, source string) string { return "r." + connector + "/" + source }
