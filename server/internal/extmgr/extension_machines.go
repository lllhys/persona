package extmgr

// Plugin settings that name the hub's machines (schema format machine, docs/extensions/extensions.md): stored by machine id. Older
// settings were typed in by hand, often as a machine's name ("macstudio" for Mac Studio), which nothing matched; when
// the hub attaches its plugins those become the id of the one machine they name.

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/nwverse/persona/server/internal/extension"
)

// machineKey is how a typed machine is compared with a machine's id, name or host: case, spaces, - and _ ignored.
func machineKey(s string) string {
	return strings.NewReplacer(" ", "", "-", "", "_", "").Replace(strings.ToLower(strings.TrimSpace(s)))
}

// machineIDFor is the id of the one machine typed names (its id as is, else its id, name, ssh host or hostname
// compared by machineKey); "" when none or several.
func (h *Manager) machineIDFor(typed string) string {
	var found string
	for _, m := range h.machinesNow() {
		if m.cfg.ID == typed {
			return typed
		}
		m.mu.Lock()
		host := m.info.Hostname
		m.mu.Unlock()
		k := machineKey(typed)
		for _, name := range []string{m.cfg.ID, m.name(), m.sshHost(), host} {
			if name != "" && machineKey(name) == k {
				if found != "" && found != m.cfg.ID {
					return "" // more than one: not this one's to guess
				}
				found = m.cfg.ID
			}
		}
	}
	return found
}

// fixMachineSettings puts machine ids in place of the names typed into plugins' machine fields, once, when the hub
// attaches its plugins.
func (h *Manager) fixMachineSettings(ctx context.Context, reg *extension.Registry) {
	for _, p := range reg.List() {
		if p.Schema == nil || len(p.Record.Config) == 0 {
			continue
		}
		var cfg map[string]any
		if json.Unmarshal(p.Record.Config, &cfg) != nil {
			continue
		}
		if !h.fixMachineFields(p.Schema.Fields, cfg) {
			continue
		}
		raw, _ := json.Marshal(cfg)
		if err := reg.SetConfig(ctx, p.Record.ID, raw, "hub"); err != nil {
			h.logf("plugin %s: machine settings: %v", p.Record.ID, err)
			continue
		}
		h.logf("plugin %s: machine names in its settings are machine ids now", p.Record.ID)
	}
}

// fixMachineFields rewrites cfg's machine values in place; true when one changed.
func (h *Manager) fixMachineFields(fields []extension.Field, cfg map[string]any) bool {
	changed := false
	fix := func(v string) string {
		if id := h.machineIDFor(v); id != "" && id != v {
			changed = true
			return id
		}
		return v
	}
	for _, f := range fields {
		switch {
		case f.Type == "object":
			if sub, ok := cfg[f.Name].(map[string]any); ok && h.fixMachineFields(f.Fields, sub) {
				changed = true
			}
		case f.Format == "machine":
			if v, ok := cfg[f.Name].(string); ok {
				cfg[f.Name] = fix(v)
			}
		case f.Type == "array" && f.Items != nil && f.Items.Format == "machine":
			list, _ := cfg[f.Name].([]any)
			var out []any
			seen := map[string]bool{}
			for _, x := range list {
				v, ok := x.(string)
				if !ok {
					out = append(out, x)
					continue
				}
				if v = fix(v); !seen[v] {
					seen[v] = true
					out = append(out, v)
				} else {
					changed = true // two names of one machine
				}
			}
			if list != nil {
				cfg[f.Name] = out
			}
		}
	}
	return changed
}
