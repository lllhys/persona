package subscriptions

import (
	"context"
	"net/http"
	"strings"
)

// A built-in source's Inbox feed switched to its default subscription and back (#6265,
// docs/design/proposals/inbox-sources-migration.md "3g. Switching, compatibility and rollback"). The default
// subscription has a fixed id (MigrationID); while it is Migrated it is pulled, and its rows go to the Inbox under the
// built-in's own source id with the items' own threads and ids, so the item ids, mutes, policies, done threads and
// replies the owner has keep matching. Its destination switches stay off in storage: a manager that does not know
// Migrated leaves it off, and the built-in's feed, never switched off, comes back with it.

// migrationPrefix starts a default subscription's id; a newID's rest is hex, so no other id starts so.
const migrationPrefix = "sub_m_"

// MigrationID is the id of the built-in connector's default subscription.
func MigrationID(connector string) string { return migrationPrefix + connector }

// IsMigration says whether id is a built-in's default subscription, which the pages show on its built-in (#6345).
func IsMigration(id string) bool { return strings.HasPrefix(id, migrationPrefix) }

// MigratedFrom is the built-in connector whose Inbox feed sub is now; false while it is none's.
func MigratedFrom(sub Subscription) (string, bool) {
	if sub.Migrated == nil {
		return "", false
	}
	id, ok := strings.CutPrefix(sub.ID, migrationPrefix)
	return id, ok && id != ""
}

// Migration is a built-in's default subscription as the manager makes it: each source's step brings its own.
type Migration struct {
	Connector string // the built-in's id, whose Inbox feed it takes over (and its connector)
	Source    string // the catalog source it pulls
	// SourceName, Label and Icon name it as the built-in is named.
	SourceName, Label, Icon string
	Recipe                  Recipe
}

// Migrated are the built-in connectors whose Inbox feed is their default subscription now: the manager leaves them out
// of the Inbox round.
func (s *Service) Migrated(ctx context.Context) (map[string]bool, error) {
	all, err := s.o.Store.ListSubscriptions(ctx)
	if err != nil {
		return nil, err
	}
	out := map[string]bool{}
	for _, sub := range all {
		if id, ok := MigratedFrom(sub); ok {
			out[id] = true
		}
	}
	return out, nil
}

// Migrate switches m.Connector's Inbox feed to its default subscription (on) or back. On makes the subscription from m
// when there is none, both destinations off, and marks it Migrated from now: it is pulled at once, its catch-up reaches
// a day back, and its rows go to the Inbox under the built-in's id. Off clears the mark: the subscription stays, not
// pulled, with its seen keys, and the built-in's feed comes back. Switching to where it is changes nothing.
func (s *Service) Migrate(ctx context.Context, m Migration, on bool, by Actor) (Subscription, error) {
	if m.Connector == "" || m.Recipe.Tool == "" {
		return Subscription{}, errorf(http.StatusBadRequest, CodeBadRequest, "a migration needs its connector and recipe")
	}
	id := MigrationID(m.Connector)
	for range 3 {
		cur, err := s.o.Store.GetSubscription(ctx, id)
		if err != nil {
			return Subscription{}, err
		}
		now := s.now()
		if cur == nil {
			if !on {
				return Subscription{}, notFound("subscription")
			}
			sub := Subscription{ID: id, Connector: m.Connector, Source: m.Source, SourceName: m.SourceName, Label: m.Label,
				Icon: m.Icon, Recipe: m.Recipe, RecipeOrigin: OriginPersona, RecipeAt: now, Tool: m.Recipe.Tool,
				IntervalSeconds: defaultIntervalOf(m.Recipe.Mode, 0), RunState: StateOK, Revision: 1, CreatedAt: now, UpdatedAt: now}
			if err := s.o.Store.CreateSubscription(ctx, sub); err != nil {
				return Subscription{}, err
			}
			s.o.Audit(ctx, m.Connector, "subscription_created", by, map[string]any{"subscription": id, "source": m.Source,
				"tool": m.Recipe.Tool, "origin": "migration"})
			cur = &sub
		}
		if (cur.Migrated != nil) == on {
			return shown(*cur), nil
		}
		next, prev := *cur, cur.Revision
		if on {
			next.Migrated, next.NextRunAt = &now, &now
		} else {
			next.Migrated = nil
			if !next.ToInbox && !next.ToKB {
				next.NextRunAt = nil
			}
		}
		next.Revision, next.UpdatedAt = prev+1, now
		ok, err := s.o.Store.UpdateSubscription(ctx, next, prev)
		if err != nil {
			return Subscription{}, err
		}
		if !ok {
			continue // changed meanwhile: read it again
		}
		s.o.Audit(ctx, m.Connector, "subscription_migrated", by, map[string]any{"subscription": id, "on": on})
		s.o.Logf("subscription %s: %s's Inbox feed switched %s", id, m.Connector, map[bool]string{true: "to it", false: "back"}[on])
		s.o.Changed(shown(next), false)
		if on {
			s.poke()
		}
		return shown(next), nil
	}
	return Subscription{}, errorf(http.StatusConflict, CodeConflict, "the subscription was changed meanwhile")
}
