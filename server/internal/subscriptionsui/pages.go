// Package subscriptionsui draws the source subscription pages (#2041, docs/extensions/subscriptions.md "Pages") as
// declarative UI: Inbox Sources (#5278: every built-in source and subscription), a source, its recipe, picking a tool
// and setting it up, its runs, and the small pickers.
// extmgr serves it at POST /api/v1/subscriptions/-/ui with the request and answer of an extension page, so every app
// draws it with the renderer it already has. Everything people read is localized here; the service gives codes.
package subscriptionsui

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/nwverse/persona/sdk/go/wire"
	"github.com/nwverse/persona/server/internal/i18n"
	"github.com/nwverse/persona/server/internal/subscriptions"
)

// Service is what the pages use of subscriptions.Service.
type Service interface {
	List(ctx context.Context, f subscriptions.Filter) ([]subscriptions.Subscription, subscriptions.Features, error)
	Get(ctx context.Context, id string) (subscriptions.Subscription, []subscriptions.Run, error)
	Catalog(ctx context.Context, connector string) (subscriptions.Catalog, error)
	// Catalogs is every connector's catalog as cached, without asking any connector.
	Catalogs(ctx context.Context) ([]subscriptions.Catalog, error)
	DryRun(ctx context.Context, in subscriptions.DryRunRequest) (subscriptions.DryRun, error)
	Create(ctx context.Context, in subscriptions.CreateRequest, by subscriptions.Actor) (subscriptions.Subscription, error)
	Update(ctx context.Context, id string, p subscriptions.Patch, by subscriptions.Actor) (subscriptions.Subscription, error)
	RunNow(ctx context.Context, id string, by subscriptions.Actor) (subscriptions.Subscription, error)
	Delete(ctx context.Context, id string, withData bool, by subscriptions.Actor) error
	// ModelOf is what a model purpose runs on now ("kb", "inbox.triage").
	ModelOf(ctx context.Context, purpose string) subscriptions.ModelInfo
	// PreviewThings are a dry run's items as sub would hand them to the Inbox, for trying triage on them (#5278).
	PreviewThings(sub subscriptions.Subscription, items []subscriptions.Preview) subscriptions.TriagePreview
	// Push makes or fills the device's push source (#3848); no items only makes it.
	Push(ctx context.Context, by subscriptions.Actor, in subscriptions.PushRequest) (subscriptions.PushResult, error)
}

// Env is who asks and what the pages need from the rest of the manager.
type Env struct {
	Lang i18n.Lang
	Loc  *time.Location
	Now  time.Time
	// CanEdit: a full-access device. Others see every page with its switches and buttons disabled.
	CanEdit bool
	By      subscriptions.Actor
	// Name is a connector's name as people know it (its extension's); the id when it says "".
	Name func(connector string) string
	// Native says where Persona already reads a catalog source's service ("mail:example.com") itself, such as
	// "me@example.com" for a mail account; "" when it does not. Builtin's answer comes first when there is one.
	Native func(service string) string
	// Builtin are the sources that bring things in themselves (#5278); nil: the pages list subscriptions only.
	Builtin BuiltinSources
	// Assistant is the name the app shows for the assistant ("Ask {name}…"), for Set Up with it; "" names it the
	// assistant.
	Assistant string
	// Ask: the app opens Set Up with …'s persona://ask (it sent context.assistant, a name or ""). Only then do the pages
	// draw it: an app that can't (iOS before #5278's part, Android before #5308) would drop the link and the row would
	// do nothing (#5661).
	Ask bool
	// Triage has the triage model try a preview's items (#5278): one verdict each, nothing kept; nil: not offered.
	Triage func(ctx context.Context, tp subscriptions.TriagePreview) ([]Verdict, error)
	// UIVersion is the UI version the app draws (6 when 0): an older app gets icons it knows.
	UIVersion int
	// Platform is the asking app's (the UI context's): only "ios" offers to add its own Shortcuts source (#3848).
	Platform string
	// Graph: the app has its own relationship view (#5741), which the list's Source Relationships row opens (GraphURL).
	Graph bool
}

// Request is the page's state and, when something was pressed, the action and its values.
type Request struct {
	State  string
	Action string
	Values map[string]string
}

// ErrReadOnly answers an action from a device that may only look.
var ErrReadOnly = errors.New("read_only_device")

// looks are the actions that only show something, which a device that may only look may press too: the relationship
// view's row (#5741).
var looks = map[string]bool{"graph": true}

// Handle draws the page the state names, or does the action and answers what the app shows next.
func Handle(ctx context.Context, svc Service, env Env, req Request) (wire.ActionResult, error) {
	p := &pages{ctx: ctx, svc: svc, env: prepared(env), st: parseState(req.State)}
	if req.Action != "" {
		if !env.CanEdit && !looks[req.Action] {
			return wire.ActionResult{}, ErrReadOnly
		}
		return p.act(req.Action, req.Values)
	}
	v := p.draw()
	return wire.ActionResult{View: &v}, nil
}

// prepared is env with what the manager left out filled in.
func prepared(env Env) Env {
	if env.Loc == nil {
		env.Loc = time.UTC // the manager passes the user's; never the machine's
	}
	if env.UIVersion == 0 {
		env.UIVersion = 6
	}
	if env.Now.IsZero() {
		env.Now = time.Now()
	}
	if env.Name == nil {
		env.Name = func(c string) string { return c }
	}
	if env.Native == nil {
		env.Native = func(string) string { return "" }
	}
	return env
}

// state is a page: P names it ("" the list), the rest says which subscription, connector, source or tool.
type state struct {
	P   string `json:"p,omitempty"`
	ID  string `json:"id,omitempty"`
	C   string `json:"c,omitempty"`
	S   string `json:"s,omitempty"`
	T   string `json:"t,omitempty"`
	Ext string `json:"ext,omitempty"`
	// I is where a tool's items are (a recipe's items_path), as the setup page's dry run found it.
	I string `json:"i,omitempty"`
}

const (
	pageList     = ""
	pageSource   = "source"
	pageRecipe   = "recipe"
	pageTools    = "tools"
	pageSetup    = "setup"
	pageRuns     = "runs"
	pageInterval = "interval"
	pageFallback = "fallback"
	pageRules    = "rules"
	pageRaw      = "raw"
	pageBuiltin  = "builtin" // a built-in source (c: its connector)
	pageMutes    = "mutes"   // what is muted of a source (c: its connector or subscription)
	pageMore     = "more"    // the sources to subscribe to by choosing a tool
)

// PagesID is the id the apps draw the pages under, as if an extension's (their SubscriptionsUI.id / SUBSCRIPTIONS_UI):
// an `extension_ui` invalidate for it redraws them.
const PagesID = "-subscriptions"

func parseState(raw string) state {
	var s state
	if raw != "" {
		_ = json.Unmarshal([]byte(raw), &s) // an unreadable state is the list
	}
	return s
}

func (s state) String() string {
	b, _ := json.Marshal(s)
	return string(b)
}

type pages struct {
	ctx context.Context
	svc Service
	env Env
	st  state
	// models are the purposes' models as asked once for this request.
	models map[string]subscriptions.ModelInfo
	// listed are the built-in sources as read once for this request.
	listed *[]Builtin
	// try is the preview a source's page was asked for (its dry run), shown on it.
	try *tried
}

// modelOf is what a purpose runs on, asked of the assistant at most once a request.
func (p *pages) modelOf(purpose string) subscriptions.ModelInfo {
	if m, ok := p.models[purpose]; ok {
		return m
	}
	m := p.svc.ModelOf(p.ctx, purpose)
	if p.models == nil {
		p.models = map[string]subscriptions.ModelInfo{}
	}
	p.models[purpose] = m
	return m
}

func (p *pages) t(key string, args ...i18n.Args) string {
	return i18n.T(p.env.Lang, "subscriptions-pages."+key, args...)
}

func (p *pages) draw() wire.View {
	var v wire.View
	switch p.st.P {
	case pageSource:
		v = p.source()
	case pageRecipe:
		v = p.recipe(nil, nil)
	case pageTools:
		v = p.tools()
	case pageSetup:
		v = p.setup(nil, nil, nil)
	case pageRuns:
		v = p.runs()
	case pageInterval:
		v = p.interval()
	case pageFallback:
		v = p.fallback()
	case pageRules:
		v = p.rules()
	case pageRaw:
		v = p.raw("")
	case pageBuiltin:
		v = p.builtinPage()
	case pageMutes:
		v = p.mutes()
	case pageMore:
		v = p.more()
	default:
		v = p.list()
	}
	if v.State == "" {
		v.State = p.st.String()
	}
	if !p.env.CanEdit {
		p.readOnly(&v)
	}
	return v
}

// readOnly is a page for a device that may only look: switches disabled, fields shown as their values, buttons and
// pickers that would change something gone, and a line at the top saying why.
func (p *pages) readOnly(v *wire.View) {
	for i := range v.Sections {
		items := v.Sections[i].Items[:0]
		for _, c := range v.Sections[i].Items {
			switch {
			case c.Control != nil:
				c.Control.Disabled = true
			case c.Type == "button" && (c.Action == nil || c.Action.Push == nil):
				continue
			case c.Type == "row" && c.Action != nil && c.Action.Push == nil && !looks[c.Action.ID]:
				c.Action = nil
			case c.Type == "field":
				val := c.Value
				for _, ch := range c.Choices {
					if ch.Value == c.Value {
						val = ch.Title
					}
				}
				c = wire.Component{Type: "row", Key: c.Key, Title: c.Label, Subtitle: val}
			}
			items = append(items, c)
		}
		v.Sections[i].Items = items
	}
	note := wire.Section{Key: "read_only", Items: []wire.Component{text("read_only", p.t("read_only"), "secondary")}}
	v.Sections = append([]wire.Section{note}, v.Sections...)
}

// problemText is a service problem in words people read.
func (p *pages) problemText(pr *subscriptions.Problem) string {
	if pr == nil {
		return ""
	}
	key := "problem." + pr.Code
	if i18n.Has("subscriptions-pages." + key) {
		return p.t(key)
	}
	return p.t("problem.other")
}

// errorResult is a failed action: the page drawn again, with a toast saying what went wrong.
func (p *pages) errorResult(err error) (wire.ActionResult, error) {
	var se *subscriptions.Error
	if !errors.As(err, &se) {
		return wire.ActionResult{}, err
	}
	msg := p.problemText(&subscriptions.Problem{Code: se.Code, Message: se.Message})
	if se.Status == http.StatusConflict {
		msg = p.t("toast.conflict")
	}
	v := p.draw()
	return wire.ActionResult{View: &v, Toast: msg}, nil
}
