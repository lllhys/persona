package assistant

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/assistantwire"
)

// assistant.memory (memory_rpc.go): an extension granted assistant.memory.read lists the assistant's own memory,
// never the health space's, with the frontmatter read and the text bounded.
func TestExtensionMemoryListsTheAssistantsOwn(t *testing.T) {
	s, f := conversationsService(t)
	created := convDay.Add(-time.Hour)
	var asked []map[string]any
	f.answerNode(DeviceAssistantMemory, func(_ string, params json.RawMessage) (any, error) {
		var p map[string]any
		_ = json.Unmarshal(params, &p)
		asked = append(asked, p)
		return map[string]any{"files": []map[string]any{
			{"name": "MEMORY.md", "content": "- [Travel](travel.md) — window seats", "updated_at": convDay},
			{"name": "plain.md", "content": "no frontmatter here", "updated_at": convDay},
			{"name": "travel.md", "content": "---\nname: travel\ndescription: \"Prefers window seats\"\nmetadata:\n  type: user\n---\n\nWindow seats.\n",
				"updated_at": convDay, "created_at": created},
			{"name": "work.md", "content": "---\ndescription: Short replies\ntype: feedback\n---\nBe brief.", "updated_at": convDay},
		}}, nil
	})
	var res assistantwire.AssistantMemoryResult
	if err := askConversations(t, s, assistantwire.MethodAssistantMemory, `{}`, &res); err != nil {
		t.Fatal(err)
	}
	if len(asked) != 1 || asked[0]["op"] != "list" || asked[0]["cwd"] != "/home/me/.persona/assistant" {
		t.Fatalf("the node was asked %v", asked)
	}
	var names []string
	for _, e := range res.Entries {
		names = append(names, e.Name)
		if e.Content != "" {
			t.Errorf("%s: content without include_content", e.Name)
		}
	}
	if strings.Join(names, ",") != "plain.md,travel.md,work.md" { // the index is not an entry
		t.Fatalf("entries %v", names)
	}
	plain, travel, work := res.Entries[0], res.Entries[1], res.Entries[2]
	if plain.Description != "" || plain.Type != "" || plain.Created != nil || !plain.Modified.Equal(convDay) || plain.Size != len("no frontmatter here") {
		t.Errorf("plain %+v", plain)
	}
	if travel.Description != "Prefers window seats" || travel.Type != "user" || travel.Created == nil || !travel.Created.Equal(created) {
		t.Errorf("travel %+v", travel)
	}
	if work.Description != "Short replies" || work.Type != "feedback" {
		t.Errorf("work %+v", work)
	}

	res = assistantwire.AssistantMemoryResult{}
	if err := askConversations(t, s, assistantwire.MethodAssistantMemory, `{"include_content":true}`, &res); err != nil {
		t.Fatal(err)
	}
	if res.Entries[2].Content != "---\ndescription: Short replies\ntype: feedback\n---\nBe brief." || res.Entries[2].Truncated {
		t.Errorf("content %+v", res.Entries[2])
	}
	if asked[1]["cwd"] != "/home/me/.persona/assistant" {
		t.Fatalf("the second list went to %v", asked[1]["cwd"])
	}
	// Strict params: no space to ask for (the health space is never an extension's).
	if err := askConversations(t, s, assistantwire.MethodAssistantMemory, `{"space":"health"}`, &res); convCode(err) != "invalid_params" {
		t.Fatalf("space: %v", err)
	}
}

// Long texts are cut per entry and over the page; no memory directory is no entries.
func TestExtensionMemoryBounds(t *testing.T) {
	s, f := conversationsService(t)
	long := strings.Repeat("é", assistantwire.AssistantMemoryEntryMax) // two bytes each: twice the bound
	var files []map[string]any
	for i := range 40 {
		files = append(files, map[string]any{"name": string(rune('a'+i%26)) + string(rune('a'+i/26)) + ".md", "content": long, "updated_at": convDay})
	}
	f.answerNode(DeviceAssistantMemory, func(string, json.RawMessage) (any, error) { return map[string]any{"files": files}, nil })
	var res assistantwire.AssistantMemoryResult
	if err := askConversations(t, s, assistantwire.MethodAssistantMemory, `{"include_content":true}`, &res); err != nil {
		t.Fatal(err)
	}
	total := 0
	for i, e := range res.Entries {
		if !e.Truncated || len(e.Content) > assistantwire.AssistantMemoryEntryMax || !strings.HasPrefix(long, e.Content) || e.Size != len(long) {
			t.Fatalf("entry %d: %d bytes, truncated %v", i, len(e.Content), e.Truncated)
		}
		total += len(e.Content)
	}
	if len(res.Entries) != 40 || total > assistantwire.AssistantMemoryTotalMax || res.Entries[39].Content != "" {
		t.Fatalf("%d entries, %d bytes, last %d", len(res.Entries), total, len(res.Entries[39].Content))
	}

	f.answerNode(DeviceAssistantMemory, func(string, json.RawMessage) (any, error) { return map[string]any{"files": []any{}}, nil })
	res = assistantwire.AssistantMemoryResult{}
	if err := askConversations(t, s, assistantwire.MethodAssistantMemory, `{}`, &res); err != nil || res.Entries == nil || len(res.Entries) != 0 {
		t.Fatalf("empty: %+v %v", res, err)
	}
}

func TestMemoryFrontmatter(t *testing.T) {
	for _, c := range []struct{ in, desc, typ string }{
		{"---\ndescription: a\ntype: b\n---\nx", "a", "b"},
		{"\xef\xbb\xbf---\r\ndescription: 'a: b'\r\n---\r\n", "a: b", ""}, // after a byte-order mark
		{"---\nmetadata:\n  type: project\ndescription: d\n---", "d", "project"},
		{"---\ndescription: never closed\n", "", ""},
		{"text\n---\ndescription: not at the top\n---", "", ""},
		{"", "", ""},
	} {
		if d, ty := memoryFrontmatter(c.in); d != c.desc || ty != c.typ {
			t.Errorf("%q: %q %q, want %q %q", c.in, d, ty, c.desc, c.typ)
		}
	}
}
