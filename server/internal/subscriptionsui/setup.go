package subscriptionsui

import (
	"bytes"
	"cmp"
	"encoding/json"
	"slices"
	"strconv"
	"strings"

	"github.com/nwverse/persona/sdk/go/wire"
	"github.com/nwverse/persona/server/internal/subscriptions"
)

// param is one property of a tool's input schema, in the schema's own order.
type param struct {
	Name        string
	Type        string
	Title       string
	Description string
	Format      string
	Enum        []string
	Default     json.RawMessage
	Required    bool
}

// params reads a tool's input schema's properties in their written order (a Go map would lose it).
func params(schema json.RawMessage) []param {
	var top struct {
		Properties json.RawMessage `json:"properties"`
		Required   []string        `json:"required"`
	}
	if json.Unmarshal(schema, &top) != nil || len(top.Properties) == 0 {
		return nil
	}
	dec := json.NewDecoder(bytes.NewReader(top.Properties))
	if t, err := dec.Token(); err != nil || t != json.Delim('{') {
		return nil
	}
	var out []param
	for dec.More() {
		t, err := dec.Token()
		if err != nil {
			return out
		}
		name, _ := t.(string)
		var raw struct {
			Type        any             `json:"type"`
			Title       string          `json:"title"`
			Description string          `json:"description"`
			Format      string          `json:"format"`
			Enum        []any           `json:"enum"`
			Default     json.RawMessage `json:"default"`
		}
		if dec.Decode(&raw) != nil {
			return out
		}
		pm := param{Name: name, Title: raw.Title, Description: raw.Description, Format: raw.Format, Default: raw.Default,
			Required: slices.Contains(top.Required, name)}
		switch ty := raw.Type.(type) {
		case string:
			pm.Type = ty
		case []any: // ["string", "null"]: the first that is not null
			for _, x := range ty {
				if s, _ := x.(string); s != "" && s != "null" {
					pm.Type = s
					break
				}
			}
		}
		for _, e := range raw.Enum {
			if s, ok := e.(string); ok {
				pm.Enum = append(pm.Enum, s)
			}
		}
		out = append(out, pm)
	}
	return out
}

// argValue is an argument as its form field holds it.
func argValue(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	return string(raw)
}

// argsJSON turns the form's arg.* values back into the arguments object, typed by the schema.
func argsJSON(ps []param, values map[string]string) json.RawMessage {
	var b bytes.Buffer
	b.WriteByte('{')
	n := 0
	for _, pm := range ps {
		v, ok := values["arg."+pm.Name]
		if !ok || strings.TrimSpace(v) == "" {
			continue
		}
		var val json.RawMessage
		switch pm.Type {
		case "integer", "number":
			if _, err := strconv.ParseFloat(v, 64); err == nil {
				val = json.RawMessage(v)
			}
		case "boolean":
			if v == "true" || v == "false" {
				val = json.RawMessage(v)
			}
		case "object", "array":
			if json.Valid([]byte(v)) {
				val = json.RawMessage(v)
			}
		}
		if val == nil {
			val, _ = json.Marshal(v)
		}
		if n > 0 {
			b.WriteByte(',')
		}
		k, _ := json.Marshal(pm.Name)
		b.Write(k)
		b.WriteByte(':')
		b.Write(val)
		n++
	}
	b.WriteByte('}')
	return b.Bytes()
}

// The fields a recipe maps, in the order the pickers show them.
var fieldNames = []string{"title", "time", "sender", "body", "link", "due_date", "id", "thread"}

// guesses are names a field's path often ends with, best first, for preselecting the pickers.
var guesses = map[string][]string{
	"title":    {"subject", "title", "summary", "name", "headline"},
	"time":     {"receiveddatetime", "received_at", "date", "datetime", "created_at", "createdat", "time", "updated", "start"},
	"sender":   {"from.name", "from", "sender", "author", "from.address", "user", "owner"},
	"body":     {"bodypreview", "preview", "snippet", "body", "text", "content", "description"},
	"link":     {"weblink", "web_link", "htmllink", "url", "link", "href"},
	"due_date": {"due", "due_date", "deadline", "duedate"},
	"id":       {"id", "uid", "message_id", "messageid"},
	"thread":   {"conversationid", "conversation_id", "thread_id", "threadid", "thread"},
}

func guess(field string, paths []subscriptions.PathValue) string {
	for _, g := range guesses[field] {
		for _, pv := range paths {
			low := strings.ToLower(shortPath(pv.Path))
			if low == g || strings.HasSuffix(low, "."+g) {
				return pv.Path
			}
		}
	}
	return ""
}

func getField(f *wire.ConnectorSubscriptionFields, name string) string {
	if f == nil {
		return ""
	}
	return map[string]string{"title": f.Title, "time": f.Time, "sender": f.Sender, "body": f.Body, "link": f.Link,
		"due_date": f.DueDate, "id": f.ID, "thread": f.Thread}[name]
}

// recipeFrom is the recipe the setup form describes.
func (p *pages) recipeFrom(ps []param, values map[string]string) subscriptions.Recipe {
	r := subscriptions.Recipe{
		Mode:      cmp.Or(values["mode"], wire.ConnectorSubscriptionModeItems),
		Tool:      p.st.T,
		Args:      argsJSON(ps, values),
		ItemsPath: values["items_path"],
		Kind:      cmp.Or(values["kind"], wire.ConnectorSubscriptionKindSubUpdate),
		GroupBy:   cmp.Or(values["group_by"], "source"),
	}
	f := wire.ConnectorSubscriptionFields{Title: values["f.title"], Time: values["f.time"], Sender: values["f.sender"],
		Body: values["f.body"], Link: values["f.link"], DueDate: values["f.due_date"], ID: values["f.id"], Thread: values["f.thread"]}
	if f != (wire.ConnectorSubscriptionFields{}) {
		r.Fields = &f
	}
	return r
}

// tool is the catalog tool the setup page is for, with its source; nil when the catalog no longer has it.
func (p *pages) tool() (*subscriptions.CatalogSource, *subscriptions.CatalogTool, error) {
	cat, err := p.svc.Catalog(p.ctx, p.st.C)
	if err != nil {
		return nil, nil, err
	}
	for i := range cat.Sources {
		if cat.Sources[i].ID != p.st.S {
			continue
		}
		for j := range cat.Sources[i].Tools {
			if t := &cat.Sources[i].Tools[j]; t.Name == p.st.T && t.Subscribable {
				return &cat.Sources[i], t, nil
			}
		}
	}
	return nil, nil, nil
}

// ⑤ Set up a tool: its arguments, then a dry run, then which fields are which, then save. values are what the form
// held when it was sent (nil on first draw); dry is the dry run it asked for.
func (p *pages) setup(values map[string]string, dry *subscriptions.DryRun, dryErr error) wire.View {
	_, tool, err := p.tool()
	if err != nil {
		return p.failed(p.t("title.setup"), err)
	}
	if tool == nil {
		return p.gone()
	}
	v := wire.View{Title: cmp.Or(tool.Title, tool.Name), State: p.st.String()}
	var existing *subscriptions.Recipe
	if p.st.ID != "" && values == nil {
		if s, _, err := p.svc.Get(p.ctx, p.st.ID); err == nil && s.Recipe.Tool == p.st.T {
			existing = &s.Recipe
		}
	}
	held := func(name, fallback string) string {
		if values != nil {
			if v, ok := values[name]; ok {
				return v
			}
		}
		return fallback
	}
	ps := params(tool.InputSchema)
	var given map[string]json.RawMessage
	if existing != nil {
		_ = json.Unmarshal(existing.Args, &given)
	}
	var args []wire.Component
	for _, pm := range ps {
		def := ""
		switch {
		case given[pm.Name] != nil:
			def = argValue(given[pm.Name])
		case pm.Format == "date-time":
			def = "{{since}}"
		case pm.Format == "date":
			def = "{{since_date}}"
		case pm.Default != nil:
			def = argValue(pm.Default)
		case pm.Required && len(pm.Enum) > 0: // a required choice starts on its first, as every app then shows the same
			def = pm.Enum[0]
		}
		f := wire.Component{Type: "field", Key: "a." + pm.Name, Name: "arg." + pm.Name, Label: cmp.Or(pm.Title, pm.Name),
			Value: held("arg."+pm.Name, def), Detail: pm.Description, Kind: "text"}
		switch {
		case len(pm.Enum) > 0:
			f.Kind = "choice"
			if !pm.Required {
				f.Choices = append(f.Choices, wire.Choice{Value: "", Title: p.t("setup.unset")})
			}
			for _, e := range pm.Enum {
				f.Choices = append(f.Choices, wire.Choice{Value: e, Title: e})
			}
		case pm.Type == "integer" || pm.Type == "number":
			f.Kind = "number"
		case pm.Type == "boolean":
			f.Kind = "toggle"
			if f.Value == "" {
				f.Value = "false"
			}
		}
		args = append(args, f)
	}
	if len(args) == 0 {
		args = []wire.Component{text("a.none", p.t("setup.no_args"), "secondary")}
	}
	v.Sections = append(v.Sections, wire.Section{Key: "args", Title: p.t("setup.args"), Footer: p.t("setup.args_footer"), Items: args})

	// Before a dry run there is nothing to map yet.
	if dry == nil && dryErr == nil && existing == nil && values == nil {
		try := button("b.dry", p.t("setup.dry_run"), "dry_run")
		try.Action.Submit = true
		v.Sections = append(v.Sections, wire.Section{Key: "go", Footer: p.t("setup.dry_footer"), Items: []wire.Component{try}})
		return v
	}
	if dryErr != nil {
		v.Sections = append(v.Sections, wire.Section{Key: "dry", Title: p.t("dry.title"), Items: []wire.Component{
			text("dry.err", p.t("dry.failed"), "secondary")}})
	}
	var paths []subscriptions.PathValue
	itemsPath := cmp.Or(held("items_path", ""), p.st.I)
	mode := held("mode", "")
	if existing != nil {
		itemsPath, mode = existing.ItemsPath, existing.Mode
	}
	if dry != nil {
		v.Sections = append(v.Sections, p.dryItems(dry)...)
		paths = dry.Paths
		if dry.ItemsPath != "" {
			itemsPath = dry.ItemsPath
		}
		if dry.Shape != nil && dry.Shape.Type == "text" {
			mode = wire.ConnectorSubscriptionModeSnapshot
		}
	}
	hidden := []wire.Component{}
	if dry != nil && dry.Shape != nil && len(dry.Shape.Arrays) > 1 && dry.ItemsPath == "" {
		where := wire.Component{Type: "field", Key: "m.items", Name: "items_path", Label: p.t("setup.items_path"), Kind: "choice",
			Value: itemsPath}
		for _, k := range dry.Shape.Arrays {
			where.Choices = append(where.Choices, wire.Choice{Value: "$." + k, Title: k})
		}
		hidden = append(hidden, where)
	}
	if mode == wire.ConnectorSubscriptionModeSnapshot {
		v.Sections = append(v.Sections, wire.Section{Key: "snap", Items: []wire.Component{text("snap", p.t("setup.snapshot_only"), "secondary")}})
	} else {
		var pickers []wire.Component
		pickers = append(pickers, hidden...)
		for _, name := range fieldNames {
			cur, shown := values["f."+name]
			if !shown { // the pickers' first showing: the recipe's own, or a good guess from the sample
				cur = cmp.Or(getField(fieldsOf(existing), name), guess(name, paths))
			}
			f := wire.Component{Type: "field", Key: "m." + name, Name: "f." + name, Label: p.t("field." + name), Kind: "choice", Value: cur}
			f.Choices = append(f.Choices, wire.Choice{Value: "", Title: p.t("setup.none")})
			seen := map[string]bool{"": true}
			for _, pv := range paths {
				f.Choices = append(f.Choices, wire.Choice{Value: pv.Path, Title: choiceTitle(pv)})
				seen[pv.Path] = true
			}
			if !seen[cur] { // the recipe's own path, not in this sample
				f.Choices = append(f.Choices, wire.Choice{Value: cur, Title: shortPath(cur)})
			}
			pickers = append(pickers, f)
		}
		v.Sections = append(v.Sections, wire.Section{Key: "map", Title: p.t("setup.fields"), Footer: p.t("setup.fields_footer"), Items: pickers})
	}
	kind := wire.Component{Type: "field", Key: "k.kind", Name: "kind", Label: p.t("setup.kind"), Kind: "choice",
		Value: held("kind", cmp.Or(kindOf(existing), wire.ConnectorSubscriptionKindSubUpdate))}
	for _, k := range wire.ConnectorSubscriptionKinds {
		if k != wire.ConnectorSubscriptionKindSubDigest {
			kind.Choices = append(kind.Choices, wire.Choice{Value: k, Title: p.t("kind." + k)})
		}
	}
	group := wire.Component{Type: "field", Key: "k.group", Name: "group_by", Label: p.t("setup.group_by"), Kind: "choice",
		Value:   held("group_by", cmp.Or(groupOf(existing), "source")),
		Choices: []wire.Choice{{Value: "thread", Title: p.t("group.thread")}, {Value: "source", Title: p.t("group.source")}}}
	v.Sections = append(v.Sections, wire.Section{Key: "kind", Title: p.t("setup.what"), Items: []wire.Component{kind, group,
		{Type: "field", Key: "k.mode", Name: "mode", Label: p.t("setup.mode"), Kind: "choice", Value: cmp.Or(mode, wire.ConnectorSubscriptionModeItems),
			Choices: []wire.Choice{{Value: "items", Title: p.t("mode.items")}, {Value: "snapshot", Title: p.t("mode.snapshot")}}},
	}})
	// Where the items are rides in the page's state, so the save uses what the dry run found.
	st := p.st
	st.I = itemsPath
	v.State = st.String()
	try := button("b.dry", p.t("setup.dry_again"), "dry_run")
	try.Action.Submit = true
	save := button("b.save", p.t("setup.save"), "save")
	save.Action.Submit = true
	v.Sections = append(v.Sections, wire.Section{Key: "save", Footer: p.t("setup.save_footer"), Items: []wire.Component{try, save}})
	return v
}

func fieldsOf(r *subscriptions.Recipe) *wire.ConnectorSubscriptionFields {
	if r == nil {
		return nil
	}
	return r.Fields
}

func kindOf(r *subscriptions.Recipe) string {
	if r == nil {
		return ""
	}
	return r.Kind
}

func groupOf(r *subscriptions.Recipe) string {
	if r == nil {
		return ""
	}
	return r.GroupBy
}

func choiceTitle(pv subscriptions.PathValue) string {
	ex := pv.Example
	if r := []rune(ex); len(r) > 40 {
		ex = string(r[:40]) + "…"
	}
	if ex == "" {
		return shortPath(pv.Path)
	}
	return shortPath(pv.Path) + " — " + ex
}

// The raw recipe, for people who read JSON: edited, dry-run and saved as a whole.
func (p *pages) raw(typed string) wire.View {
	s, _, err := p.svc.Get(p.ctx, p.st.ID)
	if err != nil {
		return p.gone()
	}
	if typed == "" {
		b, _ := json.MarshalIndent(s.Recipe, "", "  ")
		typed = string(b)
	}
	save := button("b.save", p.t("raw.save"), "raw_save")
	save.Action.Submit = true
	return wire.View{Title: p.t("recipe.raw"), Sections: []wire.Section{
		{Key: "raw", Footer: p.t("raw.footer"), Items: []wire.Component{{Type: "field", Key: "raw", Name: "recipe", Label: p.t("raw.label"),
			Kind: "multiline", Value: typed}}},
		{Key: "save", Items: []wire.Component{save}},
	}}
}

func itoa(n int) string { return strconv.Itoa(n) }

// join is parts that say something, with the middle dot between them.
func join(parts []string) string {
	var out []string
	for _, s := range parts {
		if s = strings.TrimSpace(s); s != "" {
			out = append(out, s)
		}
	}
	return strings.Join(out, " · ")
}
