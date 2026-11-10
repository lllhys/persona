package subscriptions

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/nwverse/persona/sdk/go/wire"
	"github.com/nwverse/persona/server/internal/connector"
	"github.com/nwverse/persona/server/internal/rpc"
)

// Run window: every run re-reads this much before the newest item it had; the first looks back a day.
const (
	overlap   = 60 * time.Minute
	firstBack = 24 * time.Hour
	callLimit = 60 * time.Second
)

// admit finds source and tool in the connector's current catalog and holds the tool to the read-only rule
// (docs/extensions/subscriptions.md "Read-only tools"): only a tool the catalog declares read-only (ReadCatalog has
// already refused a catalog whose hints contradict that), and never one whose name says it writes. Hints only refuse.
func admit(sources []connector.CatalogSource, source, tool string) (connector.CatalogSource, connector.CatalogTool, *Problem) {
	for _, s := range sources {
		if s.ID != source {
			continue
		}
		for _, t := range s.Tools {
			if t.Name != tool {
				continue
			}
			if p := refusal(t); p != nil {
				return s, t, p
			}
			return s, t, nil
		}
		return s, connector.CatalogTool{}, &Problem{CodeNotReadOnly, fmt.Sprintf("The source has no read-only tool %q.", tool)}
	}
	return connector.CatalogSource{}, connector.CatalogTool{}, &Problem{CodeNotReadOnly, "The connector does not offer this source."}
}

// refusal is why a catalog tool cannot be subscribed to, nil when it can.
func refusal(t connector.CatalogTool) *Problem {
	if a := t.Annotations; !t.ReadOnly || a != nil && (a.ReadOnlyHint != nil && !*a.ReadOnlyHint || a.DestructiveHint != nil && *a.DestructiveHint) {
		return &Problem{CodeAnnotation, "The tool is marked as one that changes something."}
	}
	if v := connector.WriteVerb(t.Name); v != "" {
		return &Problem{CodeWriteVerb, fmt.Sprintf("The tool's name says it changes something (%s).", v)}
	}
	return nil
}

// checkRecipe holds a recipe to its own rules before anything is called: its shape (#2037's schema), its paths and
// its template variables.
func checkRecipe(r Recipe) *Problem {
	raw, err := json.Marshal(r)
	if err == nil {
		err = wire.ValidateJSON("ConnectorSubscriptionRecipe", raw)
	}
	if err != nil {
		return &Problem{CodeBadRecipe, "The recipe is not valid: " + firstLine(err.Error())}
	}
	for _, p := range recipePaths(r) {
		if _, err := parsePath(p); err != nil {
			return &Problem{CodeBadRecipe, err.Error()}
		}
	}
	if _, err := fillArgs(r.Args, vars(time.Now(), time.UTC)); err != nil {
		return &Problem{CodeBadArgs, err.Error()}
	}
	return nil
}

func recipePaths(r Recipe) []string {
	ps := []string{cmpOr(r.ItemsPath, "$")}
	if f := r.Fields; f != nil {
		for _, p := range []string{f.ID, f.Thread, f.Title, f.Sender, f.Time, f.Body, f.Link, f.DueDate} {
			if p != "" {
				ps = append(ps, p)
			}
		}
	}
	return ps
}

func firstLine(s string) string {
	s, _, _ = strings.Cut(s, "\n")
	return clip(s, 300)
}

// pulled is what one call of a recipe gave: its items mapped (items mode) or the one snapshot.
type pulled struct {
	args      map[string]any
	itemsPath string // where the items were read
	shape     *Shape
	first     any // the first item, for the field pickers
	items     []mapped
	snapshot  *Item
	snapHash  string
	weak      bool // an item had no id: dedup by content hash
	timeBad   bool
	noTitle   bool // items came, none had a title
	problem   *Problem
	retry     time.Duration // rate_limited: how long the connector asked to wait
	connError bool          // the connector itself could not be reached
}

// pull calls the recipe's tool once for what came after since and reads what it answers.
func pull(ctx context.Context, c connector.Connector, source string, r Recipe, since time.Time, loc *time.Location, label string) pulled {
	var p pulled
	args, err := fillArgs(r.Args, vars(since, loc))
	if err != nil {
		p.problem = &Problem{CodeBadArgs, err.Error()}
		return p
	}
	p.args = args
	raw, _ := json.Marshal(args)
	cctx, cancel := context.WithTimeout(ctx, callLimit)
	defer cancel()
	out, err := connector.CallCatalog(cctx, c, connector.CatalogQuery{Source: source, Tool: r.Tool, Args: raw})
	if err != nil {
		p.problem, p.retry, p.connError = callProblem(err)
		return p
	}
	res := readResult(out)
	p.itemsPath = r.ItemsPath
	if p.itemsPath == "" && r.Mode != wire.ConnectorSubscriptionModeSnapshot && res.isObj {
		p.itemsPath = findItems(res.json)
	}
	p.shape = shapeOf(res, p.itemsPath)
	if r.Mode == wire.ConnectorSubscriptionModeSnapshot {
		content := res.text
		if res.isObj {
			b, _ := json.Marshal(res.json) // keys sorted: the same state written in another order is unchanged
			content = string(b)
		}
		p.snapHash = hashOf(content)
		it := Item{Kind: r.Kind, Snapshot: true, UID: "snap:" + p.snapHash[:24], Title: label, Body: clip(content, bodyMax)}
		if res.isObj && r.Fields != nil && r.Fields.Title != "" {
			if t := oneLine(text(at(res.json, r.Fields.Title))); t != "" {
				it.Title = clip(t, titleMax)
			}
		}
		p.snapshot = &it
		return p
	}
	if !res.isObj {
		p.problem = &Problem{CodeTextNeedsSnap, "The tool answers text, which can only be kept as a snapshot."}
		return p
	}
	if p.itemsPath == "" {
		p.problem = &Problem{CodeNoList, "The result has no single list of items: choose items_path."}
		return p
	}
	list, ok := at(res.json, p.itemsPath)
	arr, isArr := list.([]any)
	if !ok || !isArr {
		p.problem = &Problem{CodeNoList, fmt.Sprintf("%s is not a list in the result.", p.itemsPath)}
		return p
	}
	titled := false
	for i, v := range arr {
		if i == 0 {
			p.first = v
		}
		m := mapItem(v, r.Fields, r.Kind, loc)
		p.items = append(p.items, m)
		if !m.idFound {
			p.weak = true
		}
		if m.timeBad || m.dueBad {
			p.timeBad = true
		}
		titled = titled || m.hasTitle
	}
	p.noTitle = len(arr) > 0 && !titled
	return p
}

// callProblem reads a failed call: rate_limited with its wait, a connector that cannot be reached now, a refusal, or
// any other failure (whose words are not kept: they may quote the source).
func callProblem(err error) (*Problem, time.Duration, bool) {
	var re *rpc.Error
	if errors.As(err, &re) {
		switch re.Code {
		case wire.ConnectorErrorCodeRateLimited:
			var d struct {
				RetryAfter float64 `json:"retry_after"`
			}
			_ = json.Unmarshal(re.Data, &d)
			wait := time.Duration(d.RetryAfter * float64(time.Second))
			if wait <= 0 {
				wait = time.Minute
			}
			return &Problem{CodeRateLimited, "The connector asked to wait before the next call."}, wait, false
		case wire.ConnectorErrorCodeNotReadOnly:
			return &Problem{CodeNotReadOnly, "The connector refused the tool as not read-only."}, 0, false
		case wire.ConnectorErrorCodeNotSignedIn, wire.ConnectorErrorCodeOffline, wire.ConnectorErrorCodeNoMachine:
			return &Problem{CodeNotConnected, "The connector cannot be reached now."}, 0, true
		}
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return &Problem{CodeCallFailed, "The connector did not answer in time."}, 0, false
	}
	return &Problem{CodeCallFailed, "The connector's call failed."}, 0, false
}

// newOnes are the pulled items not seen yet, each once (a page can repeat an item).
func newOnes(items []mapped, seen map[string]bool) []mapped {
	var out []mapped
	in := map[string]bool{}
	for _, m := range items {
		if seen[m.item.UID] || in[m.item.UID] {
			continue
		}
		in[m.item.UID] = true
		out = append(out, m)
	}
	return out
}

func uids(items []mapped) []string {
	out := make([]string, len(items))
	for i, m := range items {
		out[i] = m.item.UID
	}
	return out
}

// statusOf maps a problem to the HTTP status the API answers it with.
func statusOf(code string) int {
	switch code {
	case CodeNotConnected, CodeRateLimited:
		return http.StatusServiceUnavailable
	case CodeBadRecipe, CodeBadRequest:
		return http.StatusBadRequest
	}
	return http.StatusUnprocessableEntity
}
