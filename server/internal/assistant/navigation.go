package assistant

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/nwverse/persona/server/internal/apierr"
	"github.com/nwverse/persona/server/internal/lazyre"
)

// The user's navigation choices (#9071, docs/protocol/protocol.md "Navigation"): the page the apps open on and how the
// sidebar's pages are arranged (their order, the ones put away, the groups). The default
// manager keeps them for the hub, and a hub has one owner, so they are the user's: every device of theirs follows. Kept
// in the settings table under navigationKey (a key of its own: no migration). A field never stored is left out of the
// answer, so an app tells "never chosen" from "the default chosen" (an empty start) and may hand over a choice it kept
// while there was no record (docs/web/web.md "Start page").
const navigationKey = "navigation"

// navigation is the stored record. A PUT changes only the fields it carries.
type navigation struct {
	// Start is the page the apps open on: "" for the default (the main conversation), else a page's key as the web's
	// sidebar names it (`home`, `inbox`, `todos`, …, or `p:<extension>/<page>` for an extension's nav_page).
	Start *string `json:"start,omitempty"`
	// Pages is the sidebar's arrangement, as the web keeps it (web/src/lib/sidebar-pages.ts): a page's key as for Start.
	Pages *sidebarPages `json:"pages,omitempty"`
}

// sidebarPages: the pages in the user's order; those switched into the list or put under More; the groups in their
// order ("" is the built-in Pages group, a user's own is "u:<id>", any other an extension's nav_page group) with the
// title the user gave (none: the group's own); the group a page was moved to; and the extensions' groups removed.
type sidebarPages struct {
	Order   []string          `json:"order"`
	Shown   []string          `json:"shown"`
	Hidden  []string          `json:"hidden"`
	Groups  []sidebarGroup    `json:"groups,omitempty"`
	Assign  map[string]string `json:"assign,omitempty"`
	Removed []string          `json:"removed,omitempty"`
}

type sidebarGroup struct {
	ID    string `json:"id"`
	Title string `json:"title,omitempty"`
}

const (
	sidebarPagesMost  = 256 // keys in each list, pages moved to a group
	sidebarGroupsMost = 32
)

var sidebarGroupID = lazyre.New(`^(?:u:[a-z0-9]{1,16}|[a-z][a-z0-9-]{0,31})$`)

func validGroupID(id string) bool { return id == "" || sidebarGroupID.MatchString(id) }

// check says what in p is not of the shape the apps write.
func (p *sidebarPages) check() error {
	for _, list := range [][]string{p.Order, p.Shown, p.Hidden} {
		if len(list) > sidebarPagesMost {
			return fmt.Errorf("pages: at most %d keys in a list", sidebarPagesMost)
		}
		for _, k := range list {
			if k == "" || !validStartPage(k) {
				return fmt.Errorf("pages: %q is not a page's key", k)
			}
		}
	}
	if len(p.Groups) > sidebarGroupsMost || len(p.Removed) > sidebarGroupsMost {
		return fmt.Errorf("pages: at most %d groups", sidebarGroupsMost)
	}
	seen := map[string]bool{}
	for _, g := range p.Groups {
		n := utf8.RuneCountInString(strings.TrimSpace(g.Title))
		if !validGroupID(g.ID) || seen[g.ID] || n > 40 || (strings.HasPrefix(g.ID, "u:") && n == 0) {
			return fmt.Errorf("pages: group %q: a group's id once, and its title at most 40 characters (a user's group has one)", g.ID)
		}
		seen[g.ID] = true
	}
	for _, id := range p.Removed {
		if id == "" || strings.HasPrefix(id, "u:") || !validGroupID(id) {
			return fmt.Errorf("pages: %q is not an extension's group", id)
		}
	}
	if len(p.Assign) > sidebarPagesMost {
		return fmt.Errorf("pages: at most %d pages moved to a group", sidebarPagesMost)
	}
	for k, id := range p.Assign {
		if k == "" || !validStartPage(k) || !validGroupID(id) {
			return fmt.Errorf("pages: %q in group %q", k, id)
		}
	}
	return nil
}

// startPagePattern is a start page's shape: a built-in page's key, or an extension page's `p:<extension>/<page>`. Which
// pages there are is the apps' business (an extension's page comes and goes): an app opens on the default for a key it
// does not know or a page that is not there.
var startPagePattern = lazyre.New(`^(?:[a-z][a-z0-9-]{0,63}|p:[^/\x00-\x20\x7f]{1,128}/[^/\x00-\x20\x7f]{1,128})$`)

func validStartPage(s string) bool { return s == "" || startPagePattern.MatchString(s) }

func (s *Service) navigationRoutes() {
	s.handle("GET /api/v1/navigation", s.getNavigation)
	s.handle("PUT /api/v1/navigation", s.putNavigation)
}

func (s *Service) getNavigation(w http.ResponseWriter, r *http.Request) {
	var n navigation
	if st := s.store(); st != nil {
		if _, err := st.Setting(r.Context(), navigationKey, &n); err != nil {
			apierr.Write(w, http.StatusServiceUnavailable, err)
			return
		}
	}
	apierr.WriteJSON(w, n)
}

func (s *Service) putNavigation(w http.ResponseWriter, r *http.Request) {
	if !fullRole(r) {
		apierr.Write(w, http.StatusForbidden, errors.New("only a device with full access may change the start page"))
		return
	}
	var body navigation
	if err := decodeBody(w, r, &body, 64<<10); err != nil || (body.Start == nil && body.Pages == nil) {
		writeBodyErr(w, err, errors.New("start or pages is required"))
		return
	}
	if body.Start != nil && !validStartPage(*body.Start) {
		apierr.Write(w, http.StatusBadRequest, errors.New("start must be empty, a page's key or p:<extension>/<page>"))
		return
	}
	if body.Pages != nil {
		if err := body.Pages.check(); err != nil {
			apierr.Write(w, http.StatusBadRequest, err)
			return
		}
	}
	st := s.store()
	if st == nil {
		apierr.Write(w, http.StatusServiceUnavailable, errors.New("the start page needs a database"))
		return
	}
	s.navMu.Lock()
	defer s.navMu.Unlock()
	var n navigation
	if _, err := st.Setting(r.Context(), navigationKey, &n); err != nil {
		apierr.Write(w, http.StatusServiceUnavailable, err)
		return
	}
	ev := map[string]any{"type": "navigation"}
	if body.Start != nil {
		n.Start = body.Start
		ev["start"] = *n.Start
	}
	if body.Pages != nil {
		n.Pages = body.Pages
		ev["pages"] = n.Pages
	}
	if err := st.PutSetting(r.Context(), navigationKey, n); err != nil {
		apierr.Write(w, http.StatusServiceUnavailable, err)
		return
	}
	// every app follows at once: its copy decides the next opening and draws the sidebar before the hub answers. Only
	// what changed goes out, so an app holding a change of the other field it has not sent yet keeps it.
	s.event(r.Context(), ev)
	apierr.WriteJSON(w, n)
}
