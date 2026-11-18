package subscriptionsui

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/nwverse/persona/sdk/go/wire"
	"github.com/nwverse/persona/server/internal/subscriptions"
)

// act does what was pressed and answers what the app shows next: the page drawn again (most), the page below
// (Pop, after saving or deleting), or the new subscription's own page.
func (p *pages) act(action string, values map[string]string) (wire.ActionResult, error) {
	switch action {
	case "dest":
		return p.setDestination(values)
	case "interval":
		patch := subscriptions.Patch{}
		if at := values["daily_at"]; at != "" {
			patch.DailyAt = &at
		} else {
			n, err := strconv.Atoi(values["seconds"])
			if err != nil {
				return wire.ActionResult{}, errBadValues
			}
			empty := ""
			patch.IntervalSeconds, patch.DailyAt = &n, &empty
		}
		return p.patch(patch, false, "")
	case "fallback":
		tier := values["tier"]
		return p.patch(subscriptions.Patch{FallbackTier: &tier}, false, "")
	case "rules":
		rules := values["triage_rules"]
		if p.st.ID == "" && p.st.C != "" { // a built-in source's own rules
			return p.builtinRules(rules)
		}
		return p.patch(subscriptions.Patch{TriageRules: &rules}, true, p.t("toast.saved"))
	case "builtin_dest":
		return p.builtinDestination(values)
	case "builtin_via":
		return p.builtinSwitchVia(values)
	case "unmute":
		return p.unmute(values["mute"])
	case "recommend":
		return p.recommend(values)
	case "ask":
		return p.ask()
	case "graph":
		return wire.ActionResult{OpenURL: GraphURL}, nil
	case "triage_preview":
		return p.triagePreview()
	case "run_now":
		if _, err := p.svc.RunNow(p.ctx, p.st.ID, p.env.By); err != nil {
			return p.errorResult(err)
		}
		v := p.draw()
		return wire.ActionResult{View: &v, Toast: p.t("toast.queued")}, nil
	case "delete":
		if err := p.svc.Delete(p.ctx, p.st.ID, true, p.env.By); err != nil {
			return p.errorResult(err)
		}
		return wire.ActionResult{Pop: true, Toast: p.t("toast.deleted")}, nil
	case "dry_run":
		return p.dryRun(values)
	case "save":
		return p.saveRecipe(values)
	case "raw_save":
		return p.saveRaw(values["recipe"])
	case "push_add":
		// This device's own Shortcuts source, before its first item (#3848): the list again, with it under Off.
		if _, err := p.svc.Push(p.ctx, p.env.By, subscriptions.PushRequest{}); err != nil {
			return p.errorResult(err)
		}
		v := p.draw()
		return wire.ActionResult{View: &v, Toast: p.t("toast.push_added")}, nil
	}
	// A push row pressed on an app too old to push by itself: draw the page it names.
	v := p.draw()
	return wire.ActionResult{View: &v}, nil
}

var errBadValues = errors.New("bad_request")

// patch changes the subscription at its current revision; pop goes back a page after (a form), otherwise this page
// is drawn again with the change in place (a picker, where the choice moves).
func (p *pages) patch(patch subscriptions.Patch, pop bool, toast string) (wire.ActionResult, error) {
	s, _, err := p.svc.Get(p.ctx, p.st.ID)
	if err != nil {
		return p.errorResult(err)
	}
	patch.Revision = s.Revision
	if _, err := p.svc.Update(p.ctx, s.ID, patch, p.env.By); err != nil {
		return p.errorResult(err)
	}
	if pop {
		return wire.ActionResult{Pop: true, Toast: toast}, nil
	}
	v := p.draw()
	return wire.ActionResult{View: &v, Toast: toast}, nil
}

// setDestination turns the Inbox or the knowledge base on or off; on a source nobody subscribed to yet it first makes
// the subscription with the extension's own recipe, and the page becomes the subscription's.
func (p *pages) setDestination(values map[string]string) (wire.ActionResult, error) {
	to := values["to"]
	on := values[to] == "true"
	if to != "to_inbox" && to != "to_kb" {
		return wire.ActionResult{}, errBadValues
	}
	if p.st.ID == "" {
		s, err := p.svc.Create(p.ctx, subscriptions.CreateRequest{Connector: p.st.C, Source: p.st.S}, p.env.By)
		if err != nil {
			return p.errorResult(err)
		}
		p.st = state{P: pageSource, ID: s.ID}
	}
	patch := subscriptions.Patch{}
	if to == "to_inbox" {
		patch.ToInbox = &on
	} else {
		patch.ToKB = &on
	}
	return p.patch(patch, false, "")
}

// dryRun tries the recipe on the page: the recipe page's own, or what the setup form describes.
func (p *pages) dryRun(values map[string]string) (wire.ActionResult, error) {
	if p.st.P == pageSource { // the preview on a source's page: the subscription's recipe, or the extension's own
		req := subscriptions.DryRunRequest{ID: p.st.ID}
		if p.st.ID == "" {
			cat, err := p.svc.Catalog(p.ctx, p.st.C)
			if err != nil {
				return p.errorResult(err)
			}
			for _, src := range cat.Sources {
				if src.ID == p.st.S && len(src.Recipes) > 0 {
					r := src.Recipes[0]
					req = subscriptions.DryRunRequest{Connector: p.st.C, Source: p.st.S, Recipe: &r}
				}
			}
			if req.Recipe == nil {
				return wire.ActionResult{}, errBadValues
			}
		}
		dry, err := p.svc.DryRun(p.ctx, req)
		if err != nil && !isProblem(err) {
			return wire.ActionResult{}, err
		}
		p.try = &tried{err: err}
		if err == nil {
			p.try.dry = &dry
		}
		v := p.draw()
		return wire.ActionResult{View: &v}, nil
	}
	if p.st.P == pageRecipe {
		dry, err := p.svc.DryRun(p.ctx, subscriptions.DryRunRequest{ID: p.st.ID})
		if err != nil && !isProblem(err) {
			return wire.ActionResult{}, err
		}
		var d *subscriptions.DryRun
		if err == nil {
			d = &dry
		}
		v := p.recipe(d, err)
		return wire.ActionResult{View: &v}, nil
	}
	_, tool, err := p.tool()
	if err != nil || tool == nil {
		return p.errorResult(err)
	}
	if values == nil {
		values = map[string]string{}
	}
	if values["items_path"] == "" {
		values["items_path"] = p.st.I
	}
	r := p.recipeFrom(params(tool.InputSchema), values)
	dry, err := p.svc.DryRun(p.ctx, subscriptions.DryRunRequest{ID: p.st.ID, Connector: p.st.C, Source: p.st.S, Recipe: &r})
	if err != nil && !isProblem(err) {
		return wire.ActionResult{}, err
	}
	var d *subscriptions.DryRun
	if err == nil {
		d = &dry
	}
	v := p.setup(values, d, err)
	return wire.ActionResult{View: &v}, nil
}

// saveRecipe makes the subscription (a new source) or changes its recipe; a recipe that fails its checks stays on
// the form, with the dry run that refused it.
func (p *pages) saveRecipe(values map[string]string) (wire.ActionResult, error) {
	_, tool, err := p.tool()
	if err != nil || tool == nil {
		return p.errorResult(err)
	}
	if values["items_path"] == "" {
		values["items_path"] = p.st.I
	}
	r := p.recipeFrom(params(tool.InputSchema), values)
	if p.st.ID == "" {
		s, err := p.svc.Create(p.ctx, subscriptions.CreateRequest{Connector: p.st.C, Source: p.st.S, Recipe: &r}, p.env.By)
		if err != nil {
			return p.refused(values, err)
		}
		p.st = state{P: pageSource, ID: s.ID}
		v := p.draw()
		return wire.ActionResult{View: &v, Toast: p.t("toast.created")}, nil
	}
	s, _, err := p.svc.Get(p.ctx, p.st.ID)
	if err != nil {
		return p.errorResult(err)
	}
	if _, err := p.svc.Update(p.ctx, s.ID, subscriptions.Patch{Revision: s.Revision, Recipe: &r}, p.env.By); err != nil {
		return p.refused(values, err)
	}
	return wire.ActionResult{Pop: true, Toast: p.t("toast.saved")}, nil
}

// refused is the setup form again after a save the service turned down: its dry run when it has one.
func (p *pages) refused(values map[string]string, err error) (wire.ActionResult, error) {
	var se *subscriptions.Error
	if !errors.As(err, &se) {
		return wire.ActionResult{}, err
	}
	if se.Status == http.StatusConflict {
		return p.errorResult(err)
	}
	v := p.setup(values, se.DryRun, nil)
	return wire.ActionResult{View: &v, Toast: p.problemText(&subscriptions.Problem{Code: se.Code})}, nil
}

func (p *pages) saveRaw(typed string) (wire.ActionResult, error) {
	var r subscriptions.Recipe
	if err := json.Unmarshal([]byte(typed), &r); err != nil {
		v := p.raw(typed)
		return wire.ActionResult{View: &v, Toast: p.t("raw.not_json")}, nil
	}
	s, _, err := p.svc.Get(p.ctx, p.st.ID)
	if err != nil {
		return p.errorResult(err)
	}
	if _, err := p.svc.Update(p.ctx, s.ID, subscriptions.Patch{Revision: s.Revision, Recipe: &r}, p.env.By); err != nil {
		var se *subscriptions.Error
		if errors.As(err, &se) && se.Status != http.StatusConflict {
			v := p.raw(typed)
			return wire.ActionResult{View: &v, Toast: p.problemText(&subscriptions.Problem{Code: se.Code})}, nil
		}
		return p.errorResult(err)
	}
	return wire.ActionResult{Pop: true, Toast: p.t("toast.saved")}, nil
}

func isProblem(err error) bool {
	var se *subscriptions.Error
	return errors.As(err, &se)
}
