package extension

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"
)

// In-app tunnels (docs/extensions/extensions.md"In-app tunnels"): an extension that names a tunnel the apps can reach this hub through
// when its own addresses do not answer. The tunnel itself is a core built into the app; the hub only says it has one
// and where it goes (the configuration's target_host and target_port). Declarative: nothing runs on the hub.

// TunnelItem is a contribution to tunnels.
type TunnelItem struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// TunnelNameMost is the longest tunnel name (runes): the apps show it as a settings row's title.
const TunnelNameMost = 40

func init() {
	Points["tunnels"] = Point{Name: "tunnels", Namespace: "tunnels", Version: 1, Item: func(raw json.RawMessage) (string, error) {
		var t TunnelItem
		if err := strict(raw, &t); err != nil {
			return "", err
		}
		if !idPattern.MatchString(t.ID) {
			return t.ID, fmt.Errorf("tunnel id %q: a-z0-9_-", t.ID)
		}
		if n := utf8.RuneCountInString(strings.TrimSpace(t.Name)); n == 0 || n > TunnelNameMost {
			return t.ID, fmt.Errorf("tunnel %s: name: 1 to %d characters", t.ID, TunnelNameMost)
		}
		return t.ID, nil
	}}
}

// Tunnels are the manifest's contributions to tunnels.
func (m *Manifest) Tunnels() []TunnelItem {
	var out []TunnelItem
	_ = json.Unmarshal(m.Contributes["tunnels"], &out)
	return out
}

// tunnelTargetProblems checks that a manifest with tunnels says where they go: target_host, a required string field
// of the configuration, and target_port and ssh_port (the hub machine's SSH port, for an SSH tunnel the app dials through
// this tunnel), integer fields when there are.
func (m *Manifest) tunnelTargetProblems() []string {
	if len(m.Tunnels()) == 0 {
		return nil
	}
	var sc *Schema
	if s, err := ParseSchema(m.Configuration); err == nil {
		sc = s
	}
	field := func(name string) *Field {
		if sc == nil {
			return nil
		}
		if i := slices.IndexFunc(sc.Fields, func(f Field) bool { return f.Name == name }); i >= 0 {
			return &sc.Fields[i]
		}
		return nil
	}
	var errs []string
	if host := field("target_host"); host == nil || host.Type != "string" || !host.Required {
		errs = append(errs, "contributes.tunnels: configuration needs target_host, a required string field")
	}
	for _, name := range []string{"target_port", "ssh_port"} {
		if port := field(name); port != nil && port.Type != "integer" {
			errs = append(errs, "contributes.tunnels: configuration's "+name+" must be an integer field")
		}
	}
	return errs
}
