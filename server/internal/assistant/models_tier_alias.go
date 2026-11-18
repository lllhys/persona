package assistant

import (
	"cmp"
	"strings"

	"github.com/nwverse/persona/server/internal/i18n"
)

// apiTierAliases: the Claude Code tier aliases a model field can hold (the picker offers them for a launcher's CLI,
// whose node resolves them through ANTHROPIC_DEFAULT_*_MODEL, node/launcherenv.go resolveTier), and which of a
// launcher's tier fields each runs. As on a computer (node's providerEnv), haiku falls back to the small model and
// best runs what opus does (Claude Code 2.1.321); opusplan is opus's tier too.
var apiTierAliases = map[string]func(storedLauncher) string{
	"opus":     func(l storedLauncher) string { return l.OpusModel },
	"best":     func(l storedLauncher) string { return l.OpusModel },
	"opusplan": func(l storedLauncher) string { return l.OpusModel },
	"sonnet":   func(l storedLauncher) string { return l.SonnetModel },
	"haiku":    func(l storedLauncher) string { return cmp.Or(l.HaikuModel, l.SmallModel) },
	"fable":    func(l storedLauncher) string { return l.FableModel },
}

// apiTierAliasKey is m's entry in apiTierAliases ("Sonnet", "opus[1m]" alike), and whether it has one.
func apiTierAliasKey(m string) (string, bool) {
	k := strings.ToLower(strings.TrimSpace(m))
	k = strings.TrimSuffix(k, "[1m]")
	_, ok := apiTierAliases[k]
	return k, ok
}

// resolveAPITierAlias is model as a direct API call to l's gateway sends it (#9165): a tier alias becomes the model
// l's tier field names, since no gateway serves a model called "sonnet"; any other model as it is. A tier alias l
// names no model for (or names another alias for) is an error, not a request the gateway refuses with a vague 4xx.
func resolveAPITierAlias(l storedLauncher, model string) (string, error) {
	k, ok := apiTierAliasKey(model)
	if !ok {
		return model, nil
	}
	if v := strings.TrimSpace(apiTierAliases[k](l)); v != "" {
		if _, alias := apiTierAliasKey(v); !alias {
			return v, nil
		}
	}
	return "", i18n.Err("assistant-models.err_tier_alias_unresolved", i18n.Args{"model": model, "provider": l.Name})
}
