package subscriptionsui

import (
	"cmp"
	"context"
	"slices"

	"github.com/nwverse/persona/sdk/go/wire"
	"github.com/nwverse/persona/server/internal/i18n"
	"github.com/nwverse/persona/server/internal/subscriptions"
)

// Graph is Inbox Sources as relations (#5741), for the apps to draw: what brings each source (an extension, or
// Persona itself), how it is read (one of the extension's read-only tools, or directly), the source, and where its
// things go. The same sources, words and rules as the list: its subtitles and badges, its possible duplicates, its
// pages. Sources with neither destination on are left out; they stay in the list.
type Graph struct {
	Extensions   []GraphExtension   `json:"extensions"`
	Reads        []GraphRead        `json:"reads"`
	Sources      []GraphSource      `json:"sources"`
	Destinations []GraphDestination `json:"destinations"`
}

// GraphExtension is what brings sources: an extension, or Persona itself (ID GraphPersona, no Extension).
type GraphExtension struct {
	ID string `json:"id"`
	// Extension is the extension's id, for its icon; "" for Persona's own.
	Extension string `json:"extension,omitempty"`
	Name      string `json:"name"`
	// Line says how many sources it brings, in words.
	Line string `json:"line"`
}

// GraphRead is how sources are read: a tool of a catalog source, Persona reading directly (Direct), or a device
// handing items in (Push).
type GraphRead struct {
	ID        string `json:"id"`
	Extension string `json:"extension"` // a GraphExtension's id
	Title     string `json:"title"`
	Direct    bool   `json:"direct,omitempty"`
	Push      bool   `json:"push,omitempty"`
}

// GraphSource is one source as the list has it: ID is its row's key there ("s.<subscription>" or "b.<connector>").
type GraphSource struct {
	ID    string `json:"id"`
	Read  string `json:"read"` // a GraphRead's id
	Kind  string `json:"kind"` // "subscription" or "builtin"
	Name  string `json:"name"`
	Icon  string `json:"icon"`
	Color string `json:"color"`
	// Line and Badge are its row's subtitle and badge in the list.
	Line  string      `json:"line"`
	Badge *wire.Badge `json:"badge,omitempty"`
	// Reading: it reads now without a problem. Not signed in, failing, waiting for a new recipe or not connected is
	// not reading; being slowed down by its service is.
	Reading bool `json:"reading"`
	// To are its destinations: "inbox", "kb".
	To []string `json:"to"`
	// Duplicates are the sources that may bring the same things (the list's "Possible duplicate").
	Duplicates []string `json:"duplicates,omitempty"`
	// State is the page that shows it, as the apps open a -subscriptions page.
	State string `json:"state"`
	// Ask is Set Up with the assistant's open_url, for a subscription (its page's row).
	Ask string `json:"ask,omitempty"`
}

// GraphDestination is where things go: the Inbox or the knowledge base, with how many sources bring things there
// (Sources) and how many of them are reading now (Reading).
type GraphDestination struct {
	ID      string `json:"id"` // "inbox" or "kb"
	Name    string `json:"name"`
	Line    string `json:"line"`
	Sources int    `json:"sources"`
	Reading int    `json:"reading"`
}

// GraphPersona is the GraphExtension of what Persona reads itself: the hub's own connectors and a device's push
// source.
const GraphPersona = "persona"

// DrawGraph is Graph for env.
func DrawGraph(ctx context.Context, svc Service, env Env) (Graph, error) {
	p := &pages{ctx: ctx, svc: svc, env: prepared(env)}
	subs, _, err := svc.List(ctx, subscriptions.Filter{})
	if err != nil {
		return Graph{}, err
	}
	subs = ownRows(subs) // a built-in's default subscription is its built-in (#6345)
	g := Graph{Extensions: []GraphExtension{}, Reads: []GraphRead{}, Sources: []GraphSource{}}
	exts := map[string]*GraphExtension{}
	reads := map[string]bool{}
	ext := func(id, connector string) string {
		key := cmp.Or(id, GraphPersona)
		if exts[key] == nil {
			e := &GraphExtension{ID: key, Extension: id, Name: p.t("graph.persona")}
			if id != "" {
				e.Name = cmp.Or(p.env.Name(connector), connector)
			}
			exts[key] = e
		}
		return key
	}
	read := func(r GraphRead) string {
		if !reads[r.ID] {
			reads[r.ID] = true
			g.Reads = append(g.Reads, r)
		}
		return r.ID
	}
	for _, s := range subs {
		to := subscriptionTo(s)
		if s.State == subscriptions.StateOff || len(to) == 0 {
			continue
		}
		src := GraphSource{ID: "s." + s.ID, Kind: "subscription", Name: s.Label, Icon: p.icon(s.Icon), Color: colorFor(s.Connector + "/" + s.Source),
			Line: p.stateLine(s), Badge: p.stateBadge(s.State), To: to, State: state{P: pageSource, ID: s.ID}.String(),
			Reading: s.State == subscriptions.StateOK || s.State == subscriptions.StateRateLimited}
		if b, where := p.native(s.Service); where != "" {
			if src.Badge == nil {
				src.Badge = badge(p.t("badge.maybe_duplicate"), "caution")
			}
			if b != "" {
				src.Duplicates = []string{"b." + b}
			}
		}
		if subscriptions.IsPush(s) {
			e := ext("", "")
			src.Read = read(GraphRead{ID: "push", Extension: e, Title: p.t("graph.pushed"), Push: true})
		} else {
			e := ext(s.Extension, s.Connector)
			_, cs := p.cached(s.Connector, s.Source)
			src.Read = read(GraphRead{ID: "t." + s.Connector + "/" + s.Source + "/" + s.Recipe.Tool, Extension: e, Title: toolTitle(cs, s.Recipe.Tool)})
			q := *p
			q.st = state{P: pageSource, ID: s.ID}
			if text, err := q.askText(); err == nil {
				src.Ask = askOpenURL(text)
			}
		}
		g.Sources = append(g.Sources, src)
	}
	for _, b := range p.builtins() {
		to := builtinTo(b)
		if b.Status == "disabled" || len(to) == 0 {
			continue
		}
		r := p.builtinRow(b, subs)
		e := ext(b.Extension, b.ID)
		how := GraphRead{ID: "d." + e, Extension: e, Title: p.t("graph.direct"), Direct: true}
		if b.Via != nil && b.Via.On { // read through its default subscription now (#6345)
			how = GraphRead{ID: "v." + e, Extension: e, Title: p.t("graph.via")}
		}
		src := GraphSource{ID: r.Key, Kind: "builtin", Name: b.Name, Icon: r.Icon, Color: r.IconColor, Line: r.Subtitle, Badge: r.Badge,
			Reading: p.builtinTrouble(b) == "", To: to, State: state{P: pageBuiltin, C: b.ID}.String(),
			Read: read(how)}
		for _, d := range p.duplicates(b.ID, subs) {
			src.Duplicates = append(src.Duplicates, "s."+d.ID)
		}
		g.Sources = append(g.Sources, src)
	}
	// Persona's own first, then the extensions by name; a source after its extension's others, by name.
	order := func(id string) (int, string) {
		if id == GraphPersona {
			return 0, ""
		}
		return 1, exts[id].Name
	}
	byExt := func(a, b string) int {
		ra, na := order(a)
		rb, nb := order(b)
		return cmp.Or(cmp.Compare(ra, rb), cmp.Compare(na, nb), cmp.Compare(a, b))
	}
	readOf := map[string]GraphRead{}
	for _, r := range g.Reads {
		readOf[r.ID] = r
	}
	extOf := func(read string) string { return readOf[read].Extension }
	// Within an extension, the sources of one read together (Persona's own reading first), so no link crosses another.
	kind := func(r GraphRead) int {
		if r.Direct || r.Push {
			return 0
		}
		return 1
	}
	slices.SortStableFunc(g.Sources, func(a, b GraphSource) int {
		ra, rb := readOf[a.Read], readOf[b.Read]
		return cmp.Or(byExt(ra.Extension, rb.Extension), cmp.Compare(kind(ra), kind(rb)), cmp.Compare(ra.Title, rb.Title),
			cmp.Compare(ra.ID, rb.ID), cmp.Compare(a.Name, b.Name), cmp.Compare(a.ID, b.ID))
	})
	// Reads in the order of their first source, so the columns read top to bottom without crossing.
	first := map[string]int{}
	for i, s := range g.Sources {
		if _, ok := first[s.Read]; !ok {
			first[s.Read] = i
		}
	}
	slices.SortStableFunc(g.Reads, func(a, b GraphRead) int { return cmp.Compare(first[a.ID], first[b.ID]) })
	count := map[string]int{}
	for _, s := range g.Sources {
		count[extOf(s.Read)]++
	}
	for id, e := range exts {
		e.Line = p.t("graph.sources", i18n.Args{"count": count[id]})
		g.Extensions = append(g.Extensions, *e)
	}
	slices.SortFunc(g.Extensions, func(a, b GraphExtension) int { return byExt(a.ID, b.ID) })
	for _, d := range []struct{ id, name string }{{"inbox", "dest.inbox"}, {"kb", "dest.kb"}} {
		dest := GraphDestination{ID: d.id, Name: p.t(d.name)}
		for _, s := range g.Sources {
			if slices.Contains(s.To, d.id) {
				dest.Sources++
				if s.Reading {
					dest.Reading++
				}
			}
		}
		dest.Line = join([]string{p.t("graph.sources", i18n.Args{"count": dest.Sources}), p.t("graph.reading", i18n.Args{"count": dest.Reading})})
		g.Destinations = append(g.Destinations, dest)
	}
	return g, nil
}

// subscriptionTo and builtinTo are where a source's things go now.
func subscriptionTo(s subscriptions.Subscription) []string {
	var to []string
	if s.ToInbox {
		to = append(to, "inbox")
	}
	if s.ToKB {
		to = append(to, "kb")
	}
	return to
}

func builtinTo(b Builtin) []string {
	var to []string
	if b.InboxAlways || b.Inbox != nil && *b.Inbox {
		to = append(to, "inbox")
	}
	if b.Archive != nil && *b.Archive {
		to = append(to, "kb")
	}
	return to
}
