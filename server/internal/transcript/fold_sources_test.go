package transcript

import (
	"encoding/json"
	"os"
	"reflect"
	"testing"

	"github.com/nwverse/persona/server/internal/proto"
)

// The cases the web reads too (web/src/lib/turn-sources.test.ts): a folded run names the pages its web calls used
// exactly as the chat reads the same calls unfolded, so the Sources row says the same before and after a reload.
func TestFoldSourcesSharedCases(t *testing.T) {
	raw, err := os.ReadFile("../../../spec/fixtures/fold-sources/cases.json")
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Cases []struct {
			Name  string `json:"name"`
			Input struct {
				Items []proto.Item `json:"items"`
			} `json:"input"`
			Expected struct {
				Sources []proto.FoldSource `json:"sources"`
			} `json:"expected"`
		} `json:"cases"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	if len(doc.Cases) == 0 {
		t.Fatal("no cases")
	}
	for _, c := range doc.Cases {
		t.Run(c.Name, func(t *testing.T) {
			got := foldSources(c.Input.Items)
			if len(c.Expected.Sources) == 0 {
				if got != nil {
					t.Fatalf("got %+v, want none (nil, so the field is left out)", got)
				}
				return
			}
			if !reflect.DeepEqual(got, c.Expected.Sources) {
				t.Fatalf("got %+v, want %+v", got, c.Expected.Sources)
			}
		})
	}
}

// A fold carries its run's sources, and leaves the field out when the run used none.
func TestFoldWorkCarriesSources(t *testing.T) {
	use := func(id, tool, input string) proto.Item {
		return proto.Item{ID: id + "u#0", Seq: 1, Role: "assistant", Kind: proto.KindToolUse, ToolUseID: id, ToolName: tool, ToolInput: json.RawMessage(input)}
	}
	res := func(id, text string) proto.Item {
		return proto.Item{ID: id + "r#0", Seq: 1, Role: "user", Kind: proto.KindToolResult, ToolUseID: id, Text: text}
	}
	f := fold([]proto.Item{use("a", "WebFetch", `{"url":"https://page.example.com/"}`), res("a", "summary"), use("b", "Read", `{"file_path":"/x"}`), res("b", "x")})
	if want := []proto.FoldSource{{URL: "https://page.example.com/"}}; !reflect.DeepEqual(f.Fold.Sources, want) {
		t.Fatalf("sources %+v, want %+v", f.Fold.Sources, want)
	}
	none := fold([]proto.Item{use("c", "Read", `{"file_path":"/x"}`), res("c", "x")})
	b, _ := json.Marshal(none.Fold)
	var m map[string]any
	_ = json.Unmarshal(b, &m)
	if _, ok := m["sources"]; ok {
		t.Fatalf("a run without web calls sent sources: %s", b)
	}
}
