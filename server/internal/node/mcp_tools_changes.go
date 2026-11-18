package node

import "encoding/json"

// A watch cursor is not caller identity. Always use the node's current resolution
// and discard the cursor when an initially unknown pane becomes identifiable.
func mcpToolsParams(input json.RawMessage, session string, assistant, guest bool) map[string]any {
	var cursor struct {
		Revision string `json:"revision"`
		Session  string `json:"session"`
	}
	_ = json.Unmarshal(input, &cursor)
	params := map[string]any{"assistant": assistant, "session_id": session, "guest": guest}
	if cursor.Session == session {
		params["revision"] = cursor.Revision
	}
	return params
}
