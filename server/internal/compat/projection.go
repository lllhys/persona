package compat

import (
	"bytes"
	"encoding/json"
	"strings"
)

// ProjectJSON changes only enumerated wire identities for a pre-cutover peer.
// Keys, IDs, ordinary text, unknown object members and scalar values stay intact.
// Session source booleans are emitted by proto.Session's typed adapter instead.
func ProjectJSON(raw []byte, canonical bool) []byte {
	if canonical {
		return raw
	}
	value, changed := project(raw)
	if !changed {
		return raw
	}
	if encoded, err := json.Marshal(value); err == nil {
		return encoded
	}
	return raw
}

// Projects reports whether ProjectJSON(raw, false) changes raw, without encoding what it changes it to. Each object
// is projected by its own members alone, so a message whose own members are left as they are (an id, a type) and
// whose values are side by side in it (a list's rows) is changed exactly when one of those values is: a caller that
// knows that of each row can skip projecting the message.
func Projects(raw []byte) bool {
	_, changed := project(raw)
	return changed
}

// project is raw decoded with what ProjectJSON renames renamed, and whether anything was.
func project(raw []byte) (any, bool) {
	if !bytes.Contains(raw, []byte("persona")) {
		return nil, false
	}
	// Large session lists contain the canonical boolean key, but usually no
	// aliasable scalar. Avoid decoding those snapshots solely for that key.
	if !bytes.Contains(raw, []byte("persona-")) && !bytes.Contains(raw, []byte("persona://")) &&
		!bytes.Contains(raw, []byte("target_kind")) && !bytes.Contains(raw, []byte("tool_name")) &&
		!bytes.Contains(raw, []byte(`"type":"look"`)) {
		return nil, false
	}
	var value any
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if decoder.Decode(&value) != nil {
		return nil, false
	}
	changed := false
	var walk func(any)
	walk = func(v any) {
		switch x := v.(type) {
		case []any:
			for _, item := range x {
				walk(item)
			}
		case map[string]any:
			for key, item := range x {
				s, ok := item.(string)
				if ok {
					out := s
					switch key {
					case "target_kind":
						if s == "persona" {
							out = MCPServer
						}
					case "tool_name":
						out = LegacyTool(s)
					case "id", "effective":
						if x["type"] == "look" {
							out = LegacyLook(s)
						}
					case "look", "persona_id", "app_icon_fallback":
						out = LegacyLook(s)
					case "pair_url", "pair_link", "link", "url":
						if strings.HasPrefix(s, "persona://") {
							out = LegacyURL(s)
						}
					case "text":
						out = LegacyContextTags(s)
					}
					if out != s {
						x[key] = out
						changed = true
					}
				} else {
					walk(item)
				}
			}
		}
	}
	walk(value)
	return value, changed
}

func LegacyLook(name string) string {
	switch name {
	case "persona-visor":
		return "jarvis"
	case "persona-circuit":
		return "jarvis-circuit"
	case "persona-pixel":
		return "jarvis-pixel"
	case "persona-facet":
		return "jarvis-facet"
	case "persona-blob":
		return "jarvis-blob"
	}
	return name
}
