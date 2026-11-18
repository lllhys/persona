package extmgr

// Hosts the owner allows a sandboxed extension (#5697, docs/extensions/extensions.md "Egress"): in the admin UI only,
// per extension, one specific host each (any port, or the one written). Stored in the hub setting
// extension_egress_allow, audited (egress_allow / egress_revoke), applied to the egress proxy at once and dropped with
// the extension. The proxy reaches them like a settings address: every answer of the name checked on each connection.

import (
	"context"
	"maps"
	"slices"
	"time"

	"github.com/nwverse/persona/server/internal/extension"
	"github.com/nwverse/persona/server/internal/extension/egress"
	"github.com/nwverse/persona/server/internal/i18n"
)

// extensionEgressAllowKey is the hub setting with the hosts the owner allowed, by extension (its storage key).
const extensionEgressAllowKey = "extension_egress_allow"

// ownerHostsMost is how many hosts one extension may be allowed.
const ownerHostsMost = 32

// AllowedHost is one host the owner allowed and when.
type AllowedHost struct {
	extension.OwnerHost
	At time.Time `json:"at"`
}

// egressAllowSettings is the stored form.
type egressAllowSettings struct {
	Hosts map[string][]AllowedHost `json:"hosts,omitempty"`
}

// loadEgressAllow reads the owner's allowed hosts (with the sandbox choices, loadSandboxes).
func (ps *extensions) loadEgressAllow(ctx context.Context) {
	if ps.h == nil || ps.h.store == nil {
		return
	}
	sb := &ps.sandboxes
	var in egressAllowSettings
	if _, err := ps.h.store.Setting(ctx, extensionEgressAllowKey, &in); err != nil {
		ps.logf("extensions: allowed hosts: %v", err)
		return
	}
	sb.mu.Lock()
	sb.allow = in.Hosts
	sb.mu.Unlock()
}

// ownerHosts are the hosts the owner allowed the extension with storage key key, oldest first.
func (ps *extensions) ownerHosts(key string) []AllowedHost {
	sb := &ps.sandboxes
	sb.mu.Lock()
	defer sb.mu.Unlock()
	return slices.Clone(sb.allow[key])
}

// ownerTargets are those hosts as the egress proxy's rule (egress.Rule.Owner).
func (ps *extensions) ownerTargets(key string) []egress.Target {
	var out []egress.Target
	for _, h := range ps.ownerHosts(key) {
		out = append(out, h.Target())
	}
	return out
}

// saveEgressAllow writes the allowed hosts as they are now (one write at a time, of the newest).
func (ps *extensions) saveEgressAllow(ctx context.Context) error {
	sb := &ps.sandboxes
	sb.save.Lock()
	defer sb.save.Unlock()
	sb.mu.Lock()
	out := egressAllowSettings{Hosts: maps.Clone(sb.allow)}
	sb.mu.Unlock()
	return ps.h.store.PutSetting(ctx, extensionEgressAllowKey, out)
}

// allowHost lets p reach raw (ParseOwnerHost) through the egress proxy from now on. Allowing a host already allowed
// changes nothing.
func (ps *extensions) allowHost(ctx context.Context, p *extension.Extension, raw, who string) error {
	o, err := extension.ParseOwnerHost(raw)
	if err != nil {
		return err
	}
	return ps.changeOwnerHosts(ctx, p, who, "egress_allow", o, func(hs []AllowedHost) ([]AllowedHost, bool, error) {
		if slices.ContainsFunc(hs, func(h AllowedHost) bool { return h.OwnerHost == o }) {
			return hs, false, nil
		}
		if len(hs) >= ownerHostsMost {
			return nil, false, i18n.Err("extension-sandbox.allow_too_many", i18n.Args{"count": ownerHostsMost})
		}
		return append(hs, AllowedHost{OwnerHost: o, At: time.Now().UTC()}), true, nil
	})
}

// revokeHost takes back a host the owner allowed p; its open connections through the proxy are closed.
func (ps *extensions) revokeHost(ctx context.Context, p *extension.Extension, raw, who string) error {
	o, err := extension.ParseOwnerHost(raw)
	if err != nil {
		return err
	}
	return ps.changeOwnerHosts(ctx, p, who, "egress_revoke", o, func(hs []AllowedHost) ([]AllowedHost, bool, error) {
		out := slices.DeleteFunc(slices.Clone(hs), func(h AllowedHost) bool { return h.OwnerHost == o })
		return out, len(out) != len(hs), nil
	})
}

func (ps *extensions) changeOwnerHosts(ctx context.Context, p *extension.Extension, who, action string, o extension.OwnerHost,
	change func([]AllowedHost) ([]AllowedHost, bool, error)) error {
	if !sandboxApplies(p) && !p.Manifest.Runtime.IsDeno() {
		return i18n.Err("hub-extension_sandbox.not_applicable")
	}
	if ps.h == nil || ps.h.store == nil {
		return i18n.Err("hub-extension_sandbox.no_database")
	}
	key := p.Record.StorageKey()
	sb := &ps.sandboxes
	sb.mu.Lock()
	next, changed, err := change(sb.allow[key])
	if err != nil || !changed {
		sb.mu.Unlock()
		return err
	}
	if sb.allow == nil {
		sb.allow = map[string][]AllowedHost{}
	}
	if len(next) == 0 {
		delete(sb.allow, key)
	} else {
		sb.allow[key] = next
	}
	px := sb.proxy
	sb.mu.Unlock()
	if err := ps.saveEgressAllow(ctx); err != nil {
		return err
	}
	if px != nil {
		px.SetOwner(p.Record.ID, ps.ownerTargets(key))
	}
	ps.reg.Audit(ctx, p.Record.ID, action, who, map[string]any{"host": o.String()})
	ps.logf("extension %s: %s %s (by %s)", p.Record.ID, action, o.String(), who)
	return nil
}

// forgetOwnerHosts drops the hosts allowed an extension no longer installed: installed again, it starts with none.
func (ps *extensions) forgetOwnerHosts(ctx context.Context, key string) {
	sb := &ps.sandboxes
	sb.mu.Lock()
	_, ok := sb.allow[key]
	delete(sb.allow, key)
	sb.mu.Unlock()
	if ok && ps.h != nil && ps.h.store != nil {
		if err := ps.saveEgressAllow(ctx); err != nil {
			ps.logf("extensions: allowed hosts: %v", err)
		}
	}
}
