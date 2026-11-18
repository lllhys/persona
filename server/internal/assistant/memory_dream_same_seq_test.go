package assistant

import (
	"reflect"
	"testing"

	"github.com/nwverse/persona/server/internal/transcript"
)

func TestMemoryDreamIncludesEveryTextBlockFromOneLine(t *testing.T) {
	line := []byte(`{"type":"user","uuid":"one-user-line","message":{"role":"user","content":[{"type":"text","text":"I always prefer tea."},{"type":"text","text":"Always answer in English."}]}}`)
	items := transcript.ParseLine(7, line, &transcript.Meta{})
	if len(items) != 2 || items[0].Seq != 7 || items[1].Seq != 7 {
		t.Fatalf("parser did not produce two same-line text items: %+v", items)
	}
	messages, seq := memoryDreamMessages(items, 0)
	want := []string{"I always prefer tea.", "Always answer in English."}
	if !reflect.DeepEqual(messages, want) || seq != 7 {
		t.Fatalf("messages = %q, checkpoint = %d; want %q, 7", messages, seq, want)
	}
	if messages, seq := memoryDreamMessages(items, 8); len(messages) != 0 || seq != 8 {
		t.Fatalf("an earlier checkpoint moved backward: messages=%q checkpoint=%d", messages, seq)
	}
}
