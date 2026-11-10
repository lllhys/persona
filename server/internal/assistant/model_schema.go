package assistant

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/nwverse/persona/server/internal/jsonsubset"
)

// A JSON Schema subset for model.complete's json_schema (#6102): what a reply is checked against before it goes back
// to the extension. The rules live in internal/jsonsubset, shared with the subscriptions' tool arguments. The schema
// goes to the model as the extension wrote it (tools/json_schema).

// compileSchema reads a json_schema: a JSON object, whose local $refs resolve.
func compileSchema(raw json.RawMessage) (map[string]any, error) { return jsonsubset.Compile(raw) }

// validateSchema says what in v does not match schema, "" when it does.
func validateSchema(root, schema map[string]any, v any, at string, depth int) string {
	return jsonsubset.ValidateAt(root, schema, v, at, depth)
}

// decodeReply finds the JSON value in a model's reply: the whole reply, else inside a code fence, else its first
// object or array.
func decodeReply(text string) (any, error) {
	try := func(s string) (any, bool) {
		dec := json.NewDecoder(strings.NewReader(s))
		dec.UseNumber()
		var v any
		if dec.Decode(&v) != nil || dec.More() {
			return nil, false
		}
		return v, true
	}
	s := strings.TrimSpace(text)
	if v, ok := try(s); ok {
		return v, nil
	}
	if i := strings.Index(s, "```"); i >= 0 {
		body := s[i+3:]
		if nl := strings.IndexByte(body, '\n'); nl >= 0 {
			body = body[nl+1:]
		}
		if j := strings.Index(body, "```"); j >= 0 {
			if v, ok := try(body[:j]); ok {
				return v, nil
			}
		}
	}
	for _, open := range []byte{'{', '['} {
		if i := strings.IndexByte(s, open); i >= 0 {
			dec := json.NewDecoder(strings.NewReader(s[i:]))
			dec.UseNumber()
			var v any
			if dec.Decode(&v) == nil {
				return v, nil
			}
		}
	}
	return nil, fmt.Errorf("the reply has no JSON value")
}
