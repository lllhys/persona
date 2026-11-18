package assistantskills

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"encoding/xml"
	"errors"
	"strings"

	"github.com/nwverse/persona/server/internal/compat"
	"github.com/nwverse/persona/server/internal/lazyre"
)

// Entry is an executable environment's inventory entry. Paths stay on the node.
type Entry struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Source      string `json:"source"`
	Path        string `json:"-"`
}

// Catalog deliberately distinguishes a bounded local inventory from a runtime's complete answer.
type Catalog struct {
	Version  int     `json:"catalog_version"`
	Skills   []Entry `json:"skills"`
	Complete bool    `json:"complete"`
}

// Bind makes a skill's identity specific to its assistant, machine, launcher and profile.
// No lookup table, client path, or display-name matching is involved.
func Bind(scope string, entries []Entry) []Entry {
	out := make([]Entry, len(entries))
	for i, e := range entries {
		raw, _ := json.Marshal([]string{scope, e.Path, e.Name, e.Source})
		sum := sha256.Sum256(raw)
		e.ID = hex.EncodeToString(sum[:])
		out[i] = e
	}
	return out
}

var leadingReference = lazyre.New(`(?s)^\s*<` + compat.ContextPattern() + `\b([^>]*)>.*?</` + compat.ContextPattern() + `>`)

var ErrReference = errors.New("selected skill is unavailable or its execution environment changed; remove it and select it again")

// References reads only explicit leading context selections, never @name, paths, quoted tags or later prose.
func References(text string) ([]string, error) {
	var refs []string
	for {
		m := leadingReference.FindStringSubmatchIndex(text)
		if m == nil {
			return refs, nil
		}
		tag := text[m[2]:m[3]]
		text = text[m[1]:]
		decoder := xml.NewDecoder(strings.NewReader("<context" + tag + "/>"))
		token, err := decoder.Token()
		if err != nil {
			if strings.Contains(tag, `"skill"`) {
				return nil, ErrReference
			}
			continue
		}
		start, ok := token.(xml.StartElement)
		if !ok {
			continue
		}
		skill := false
		for _, a := range start.Attr {
			skill = skill || a.Name.Local == "kind" && a.Value == "skill"
		}
		if !skill {
			continue
		}
		attrs := map[string]string{}
		for _, a := range start.Attr {
			key := a.Name.Local
			if _, duplicate := attrs[key]; duplicate || a.Name.Space != "" {
				return nil, ErrReference
			}
			switch key {
			case "kind", "title", "url", "ref":
			default:
				return nil, ErrReference
			}
			attrs[key] = a.Value
		}
		if attrs["ref"] == "" || len(attrs["ref"]) > 128 || len(refs) >= 16 {
			return nil, ErrReference
		}
		refs = append(refs, attrs["ref"])
	}
}

// Resolve rechecks every selected identity against the target's current inventory; never substitute by name.
func Resolve(text string, catalog []Entry) ([]Entry, error) {
	refs, err := References(text)
	if err != nil {
		return nil, err
	}
	var out []Entry
	seen := map[string]bool{}
	for _, ref := range refs {
		found := false
		for _, e := range catalog {
			if e.ID == ref {
				found = true
				if !seen[ref] {
					seen[ref] = true
					out = append(out, e)
				}
				break
			}
		}
		if !found {
			return nil, ErrReference
		}
	}
	return out, nil
}
