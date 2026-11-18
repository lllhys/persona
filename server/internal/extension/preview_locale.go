package extension

import (
	"slices"

	"github.com/nwverse/persona/server/internal/i18n"
)

// In returns a presentation copy of a staged preview. Consent digests, manifests, staging data and
// the captured old manifest stay unchanged, so switching languages never changes what gets installed.
func (pv *Preview) In(l i18n.Lang) *Preview {
	if pv == nil || l == "" {
		return pv
	}
	out := *pv
	if pv.Manifest != nil && pv.l10n != nil {
		// a copy of the manifest with the name and description in l (#897); the one approved is pv.Manifest's
		m := *pv.Manifest
		m.Name, m.Description = pv.l10n.Name(l, m.Name), pv.l10n.Description(l, m.Description)
		out.Manifest = &m
	}
	if pv.Manifest != nil {
		out.Permissions = ExplainWith(l, pv.Manifest, pv.config)
		if pv.previous != nil {
			out.AddedPermissions = AddedWith(l, pv.previous, pv.Manifest, pv.config)
		}
	}
	if pv.RenameFrom != "" && pv.Manifest != nil {
		out.Warnings = slices.Clone(pv.Warnings)
		for i, warning := range out.Warnings {
			out.Warnings[i] = i18n.ExtensionStatusText(l, warning)
		}
		notice := i18n.T(l, "extension-identity.confirmation", i18n.Args{"from": pv.RenameFrom, "to": pv.Manifest.ID})
		if !slices.Contains(out.Warnings, notice) {
			out.Warnings = append([]string{notice}, out.Warnings...)
		}
	}
	if pv.Bundle != nil {
		out.Bundle = make([]*Preview, len(pv.Bundle))
		for i, child := range pv.Bundle {
			out.Bundle[i] = child.In(l)
		}
	}
	return &out
}
