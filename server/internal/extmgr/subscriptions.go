package extmgr

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"

	"github.com/nwverse/persona/server/internal/connector"
	"github.com/nwverse/persona/server/internal/subscriptions"
)

// Source subscriptions (#2038, docs/extensions/subscriptions.md): the core is internal/subscriptions; here it gets the
// manager's connectors, database, audit and broadcasts, and answers /api/v1/subscriptions. Reading is for every
// device; changing anything needs full access (read_only_device otherwise).

// subscriptionsFeature is in the manager's report once subscriptions deliver to the Inbox and the knowledge base
// (#2039): the apps show their entry rows only then, so a subscription the user turns on does what its switch says.
const subscriptionsFeature = "source_subscriptions"

// subscriptionsShown says whether the report carries subscriptionsFeature.
func (h *Manager) subscriptionsShown() bool { return h.subs != nil && h.subs.Features().Deliver }

// startSubscriptions makes the service and starts its runner (after the connectors, so it finds them).
func (h *Manager) startSubscriptions(ctx context.Context) {
	var store subscriptions.Store
	if d, ok := h.store.(subscriptions.DB); ok {
		store = subscriptions.NewDBStore(d)
	} else {
		store = subscriptions.NewMemStore() // no database (tests): kept in memory only
	}
	h.subs = subscriptions.New(subscriptions.Options{
		Store:      store,
		Connectors: subscriptionConnectors{h},
		Location:   h.userLocation,
		Audit: func(ctx context.Context, id, action string, by subscriptions.Actor, detail map[string]any) {
			h.audit(ctx, id, action, by.ID, detail)
		},
		// A subscription changed (saved, ran, deleted): the apps showing it read it again.
		Changed: func(s subscriptions.Subscription, removed bool) {
			h.broadcast(map[string]any{"type": "subscription", "id": s.ID, "connector": s.Connector, "revision": s.Revision,
				"state": s.State, "removed": removed})
		},
		Logf: h.logf,
	})
	go h.subs.Run(ctx)
}

// Subscriptions is the service, for the declarative pages (#2041) and tests; nil before the manager runs.
func (h *Manager) Subscriptions() *subscriptions.Service { return h.subs }

// subscriptionConnectors finds the registered connectors and the extension that brings each.
type subscriptionConnectors struct{ h *Manager }

// info is c as subscriptions see it: its own status only (a list of every connector's would ask each of them).
func (sc subscriptionConnectors) info(ctx context.Context, c connector.Connector) subscriptions.ConnectorInfo {
	r := sc.h.connectors
	out := subscriptions.ConnectorInfo{ID: c.ID(), Conn: c, Status: connector.StatusOf(ctx, c).State, Enabled: r.Enabled(c.ID()),
		Direct: subscriptions.Direct{
			Inbox:   c.Manifest().Has(connector.CapInbox) && r.Feeds(c.ID(), connector.FeedInbox),
			Archive: c.Manifest().Has(connector.CapArchive) && r.Feeds(c.ID(), connector.FeedArchive),
		}}
	if hp := sc.h.extensions.ownerOf(c); hp != nil {
		out.Extension = hp.id
	}
	return out
}

func (sc subscriptionConnectors) Find(ctx context.Context, id string) (subscriptions.ConnectorInfo, bool) {
	c := sc.h.connectors.Get(id)
	if c == nil {
		return subscriptions.ConnectorInfo{}, false
	}
	return sc.info(ctx, c), true
}

// All is every registered connector without asking any for its status: the summary needs only who brings it.
func (sc subscriptionConnectors) All(ctx context.Context) []subscriptions.ConnectorInfo {
	var out []subscriptions.ConnectorInfo
	for _, c := range sc.h.connectors.All() {
		info := subscriptions.ConnectorInfo{ID: c.ID(), Conn: c, Enabled: sc.h.connectors.Enabled(c.ID())}
		if hp := sc.h.extensions.ownerOf(c); hp != nil {
			info.Extension = hp.id
		}
		out = append(out, info)
	}
	return out
}

func (h *Manager) subscriptionRoutes(mux *http.ServeMux) {
	api := subscriptionMux{mux, h}
	api.HandleFunc("GET /api/v1/subscriptions", func(w http.ResponseWriter, r *http.Request) {
		list, features, err := h.subs.List(r.Context(), subscriptions.Filter{Connector: r.URL.Query().Get("connector"),
			Extension: r.URL.Query().Get("extension")})
		if err != nil {
			writeSubscriptionErr(w, err)
			return
		}
		writeJSON(w, map[string]any{"subscriptions": list, "features": features})
	})
	api.HandleFunc("GET /api/v1/subscriptions/summary", func(w http.ResponseWriter, r *http.Request) {
		lines, err := h.subs.Summary(r.Context())
		if err != nil {
			writeSubscriptionErr(w, err)
			return
		}
		writeJSON(w, map[string]any{"connectors": lines})
	})
	api.HandleFunc("GET /api/v1/subscriptions/catalog", func(w http.ResponseWriter, r *http.Request) {
		id := r.URL.Query().Get("connector")
		if id == "" { // every connector's, from what was read before: never a call to a connector
			list, err := h.subs.Catalogs(r.Context())
			if err != nil {
				writeSubscriptionErr(w, err)
				return
			}
			writeJSON(w, map[string]any{"catalogs": list})
			return
		}
		c, err := h.subs.Catalog(r.Context(), id)
		if err != nil {
			writeSubscriptionErr(w, err)
			return
		}
		writeJSON(w, c)
	})
	api.HandleFunc("GET /api/v1/subscriptions/{id}", func(w http.ResponseWriter, r *http.Request) {
		s, runs, err := h.subs.Get(r.Context(), r.PathValue("id"))
		if err != nil {
			writeSubscriptionErr(w, err)
			return
		}
		writeJSON(w, map[string]any{"subscription": s, "runs": runs})
	})
	// The pages (#2041): every device may draw them; the actions they carry check full access themselves.
	api.HandleFunc("POST /api/v1/subscriptions/-/ui", h.subscriptionUI)
	api.HandleFunc("POST /api/v1/subscriptions/-/dry-run", h.subscriptionChange(func(w http.ResponseWriter, r *http.Request, _ subscriptions.Actor) {
		var in subscriptions.DryRunRequest
		if !readSubscriptionJSON(w, r, &in) {
			return
		}
		dr, err := h.subs.DryRun(r.Context(), in)
		if err != nil {
			writeSubscriptionErr(w, err)
			return
		}
		writeJSON(w, dr)
	}))
	api.HandleFunc("POST /api/v1/subscriptions", h.subscriptionChange(func(w http.ResponseWriter, r *http.Request, by subscriptions.Actor) {
		var in subscriptions.CreateRequest
		if !readSubscriptionJSON(w, r, &in) {
			return
		}
		s, err := h.subs.Create(r.Context(), in, by)
		if err != nil {
			writeSubscriptionErr(w, err)
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(s)
	}))
	api.HandleFunc("PUT /api/v1/subscriptions/{id}", h.subscriptionChange(func(w http.ResponseWriter, r *http.Request, by subscriptions.Actor) {
		var in subscriptions.Patch
		if !readSubscriptionJSON(w, r, &in) {
			return
		}
		s, err := h.subs.Update(r.Context(), r.PathValue("id"), in, by)
		if err != nil {
			writeSubscriptionErr(w, err)
			return
		}
		writeJSON(w, s)
	}))
	api.HandleFunc("POST /api/v1/subscriptions/{id}/run", h.subscriptionChange(func(w http.ResponseWriter, r *http.Request, by subscriptions.Actor) {
		s, err := h.subs.RunNow(r.Context(), r.PathValue("id"), by)
		if err != nil {
			writeSubscriptionErr(w, err)
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusAccepted)
		_ = json.NewEncoder(w).Encode(s)
	}))
	api.HandleFunc("DELETE /api/v1/subscriptions/{id}", h.subscriptionChange(func(w http.ResponseWriter, r *http.Request, by subscriptions.Actor) {
		data, _ := strconv.ParseBool(r.URL.Query().Get("data"))
		if err := h.subs.Delete(r.Context(), r.PathValue("id"), data, by); err != nil {
			writeSubscriptionErr(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
}

// subscriptionChange lets only full-access devices through; the others can read but not change (or dry-run, which
// calls the source).
func (h *Manager) subscriptionChange(fn func(http.ResponseWriter, *http.Request, subscriptions.Actor)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p := principalOf(r.Context())
		if p.Role != roleFull {
			writeErrCode(w, http.StatusForbidden, subscriptions.CodeReadOnlyDevice, errors.New("this device can read subscriptions but not change them"))
			return
		}
		fn(w, r, subscriptions.Actor{ID: actorOf(r.Context()), Name: p.Name})
	}
}

func readSubscriptionJSON(w http.ResponseWriter, r *http.Request, v any) bool {
	if err := json.NewDecoder(io.LimitReader(r.Body, 256<<10)).Decode(v); err != nil {
		writeErrCode(w, http.StatusBadRequest, subscriptions.CodeBadRequest, err)
		return false
	}
	return true
}

// writeSubscriptionErr writes the service's error as the API's usual body ({error, code}) with what it carries: the
// dry run that refused a recipe, the subscription as it is now on a conflict, how long to wait on rate_limited.
func writeSubscriptionErr(w http.ResponseWriter, err error) {
	var e *subscriptions.Error
	if !errors.As(err, &e) {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	body := map[string]any{"error": e.Message, "code": e.Code}
	if e.DryRun != nil {
		body["dry_run"] = e.DryRun
	}
	if e.Current != nil {
		body["subscription"] = e.Current
	}
	if e.RetryAfter > 0 {
		body["retry_after"] = e.RetryAfter
	}
	writeErrorBody(w, e.Status, body)
}

// subscriptionMux answers 503 while the service is not made yet (a manager that is starting, or handlers in tests
// registered without Run).
type subscriptionMux struct {
	mux *http.ServeMux
	h   *Manager
}

func (m subscriptionMux) HandleFunc(pattern string, fn http.HandlerFunc) {
	m.mux.HandleFunc(pattern, func(w http.ResponseWriter, r *http.Request) {
		if m.h.subs == nil {
			writeErrCode(w, http.StatusServiceUnavailable, "unavailable", errors.New("subscriptions are not running yet"))
			return
		}
		fn(w, r)
	})
}
