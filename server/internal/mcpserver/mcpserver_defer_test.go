package mcpserver

import (
	"context"
	"testing"
)

// An assistant tool the manager deferred (#7317) gets no alwaysLoad marker, so Claude Code keeps it behind ToolSearch;
// the others of the same list keep theirs (#5838).
func TestToolsListDeferredAssistantToolHasNoAlwaysLoad(t *testing.T) {
	t.Setenv("PERSONA_ASSISTANT", "1")
	fakeNode(t, func(int) ([]byte, error) {
		return []byte(`{"tools":[{"name":"ask_user","description":"ask"},{"name":"inbox_move","description":"move","defer_loading":true}]}`), nil
	})
	res, rerr := handle(context.Background(), "test", listRequest())
	if rerr != nil {
		t.Fatal(rerr)
	}
	tools := res.(map[string]any)["tools"].([]map[string]any)
	if len(tools) != 2 {
		t.Fatalf("tools = %+v", tools)
	}
	if meta, ok := tools[0]["_meta"].(map[string]any); !ok || meta["anthropic/alwaysLoad"] != true {
		t.Fatalf("a tool not deferred lacks alwaysLoad metadata: %+v", tools[0])
	}
	if _, present := tools[1]["_meta"]; present || tools[1]["name"] != "inbox_move" {
		t.Fatalf("a deferred assistant tool is still marked alwaysLoad: %+v", tools[1])
	}
}
