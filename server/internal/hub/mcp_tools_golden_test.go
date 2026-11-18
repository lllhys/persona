package hub

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/nwverse/persona/server/internal/proto"
)

// The MCP tools the hub offers, as they go over the wire (json.Marshal compacts the schemas; the golden is indented
// for reading, which is a function of the same bytes). docs/assistant/prompts.md: regenerate with UPDATE_GOLDEN=1. The
// connectors', Cua's and ego's are the manager's (extmgr's connector_tools goldens, its answer to
// extensions.hub_call connector_tools); the hub lists them with its own and the assistant's, in the same order.
func TestGoldenMCPTools(t *testing.T) {
	show := func(tools []proto.Tool) string {
		assertNoEmptyToolEnums(t, tools)
		b, err := json.MarshalIndent(tools, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		return string(b) + "\n"
	}
	h := newTestHub(t)
	// The manager answers as its golden says (extmgr TestGoldenConnectorTools), for the case at hand.
	manager := func(name string) {
		f := fakeExtOf(h)
		f.on(fakeConnectorTools, func(raw json.RawMessage) (any, error) {
			var a struct {
				Assistant bool `json:"assistant"`
			}
			_ = json.Unmarshal(raw, &a)
			file := name
			if !a.Assistant {
				file = "agent"
			}
			b, err := os.ReadFile(filepath.Join("..", "extmgr", "testdata", "prompts", "connector_tools."+file+".golden"))
			if err != nil {
				return nil, err
			}
			var out json.RawMessage = b
			return out, nil
		})
	}
	manager("assistant")
	connectAssistant(t, h) // its tools are its to list (assistant.tools)
	golden(t, "mcp_tools.assistant", show(h.mcpTools(true)))
	golden(t, "mcp_tools.agent", show(h.mcpTools(false)))
	// With GitHub signed in: its read tools after the hub's own, for the assistant only.
	manager("assistant_with_github")
	golden(t, "mcp_tools.assistant_with_github", show(h.mcpTools(true)))
	golden(t, "mcp_tools.agent", show(h.mcpTools(false)))
	// With iMessage handed to Persona (the switch is on by default): its read tools too.
	manager("assistant_with_imessage")
	golden(t, "mcp_tools.assistant_with_imessage", show(h.mcpTools(true)))
	golden(t, "mcp_tools.agent", show(h.mcpTools(false)))
}

func assertNoEmptyToolEnums(t *testing.T, tools []proto.Tool) {
	t.Helper()
	var walk func(node any, path string)
	walk = func(node any, path string) {
		obj, ok := node.(map[string]any)
		if !ok {
			return
		}
		if raw, has := obj["enum"]; has {
			vals, ok := raw.([]any)
			if !ok || len(vals) == 0 {
				t.Errorf("%s.enum must be a non-empty array, got %#v", path, raw)
			}
			for _, v := range vals {
				if s, ok := v.(string); ok && s == "" {
					t.Errorf(`%s.enum contains an empty string ""`, path)
				}
			}
		}
		for k, v := range obj {
			switch k {
			case "enum":
				continue
			case "properties", "$defs", "definitions", "patternProperties":
				if m, ok := v.(map[string]any); ok {
					for ck, cv := range m {
						walk(cv, path+"."+k+"."+ck)
					}
				}
			default:
				switch x := v.(type) {
				case map[string]any:
					walk(x, path+"."+k)
				case []any:
					for _, item := range x {
						walk(item, path+"."+k+"[]")
					}
				}
			}
		}
	}
	for _, tl := range tools {
		if len(tl.InputSchema) == 0 {
			continue
		}
		var schema any
		if err := json.Unmarshal(tl.InputSchema, &schema); err != nil {
			t.Fatalf("%s: invalid input_schema: %v", tl.Name, err)
		}
		walk(schema, tl.Name)
	}
}
