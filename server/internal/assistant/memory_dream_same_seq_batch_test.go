package assistant

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/proto"
	"github.com/nwverse/persona/server/internal/transcript"
)

func memoryDreamFiveTextBlocks(t *testing.T) ([]string, []proto.Item) {
	t.Helper()
	want := make([]string, 5)
	content := make([]any, len(want))
	for i := range want {
		want[i] = strings.Repeat(string(rune('a'+i)), 2800)
		content[i] = map[string]any{"type": "text", "text": want[i]}
	}
	line, err := json.Marshal(map[string]any{
		"type": "user",
		"uuid": "five-text-blocks",
		"message": map[string]any{
			"role":    "user",
			"content": content,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	items := transcript.ParseLine(7, line, &transcript.Meta{})
	if len(items) != len(want) {
		t.Fatalf("parser produced %d items, want %d", len(items), len(want))
	}
	for _, item := range items {
		if item.Seq != 7 {
			t.Fatalf("parser produced sequence %d, want 7", item.Seq)
		}
	}
	return want, items
}

func TestMemoryDreamKeepsSameSequenceGroupTogether(t *testing.T) {
	want, items := memoryDreamFiveTextBlocks(t)
	next := []byte(`{"type":"user","uuid":"next-user-line","message":{"role":"user","content":[{"type":"text","text":"later"}]}}`)
	items = append(items, transcript.ParseLine(8, next, &transcript.Meta{})...)

	batch, seq := memoryDreamMessages(items, 0)
	if !reflect.DeepEqual(batch, want) || seq != 7 {
		t.Fatalf("first pass: %d messages, checkpoint %d; want all five sequence-7 messages, checkpoint 7", len(batch), seq)
	}
	var bytes int
	for _, message := range batch {
		bytes += len(message)
	}
	if bytes != 14000 {
		t.Fatalf("first pass contains %d bytes, want 14000", bytes)
	}
	batch, seq = memoryDreamMessages(items, seq)
	if !reflect.DeepEqual(batch, []string{"later"}) || seq != 8 {
		t.Fatalf("second pass: messages = %q, checkpoint %d; want later, 8", batch, seq)
	}
	batch, seq = memoryDreamMessages(items, seq)
	if len(batch) != 0 || seq != 8 {
		t.Fatalf("third pass: messages = %q, checkpoint %d; want empty, 8", batch, seq)
	}
}

func TestMemoryDreamServiceProcessesWholeSameSequenceGroup(t *testing.T) {
	ctx := context.Background()
	want, items := memoryDreamFiveTextBlocks(t)
	f := newFakeHub().withMachine()
	f.devices = []Device{{ID: "server", Online: true, Home: "/home/example"}}
	f.sessions["server"] = []proto.Session{{SessionID: "main", Assistant: true, Status: proto.StatusDone}}
	f.items = items
	f.hello.Assistant.MemoryDreaming = true
	f.judge = `{"body":""}`
	st := &memStore{}
	s := withSettings(t, f, New(Options{Hub: &dreamTestHub{fakeHub: f}, Store: func() Store { return st }, Logf: testLogf(t)}))
	setSettings(t, s, func(v *Settings) {
		v.Main, v.MainMachineID, v.MachineID = "main", "server", "server"
	})

	s.memoryDream(ctx)
	if f.judged != 1 || len(f.judgeAsked) != 1 {
		t.Fatalf("model calls = %d, inputs = %d; want 1", f.judged, len(f.judgeAsked))
	}
	var input struct {
		Messages []string `json:"user_messages"`
	}
	if err := json.Unmarshal([]byte(f.judgeAsked[0]), &input); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(input.Messages, want) {
		t.Fatalf("model received %d messages, want all five sequence-7 messages", len(input.Messages))
	}
	var progress memoryDreamProgress
	if _, err := st.Setting(ctx, memoryDreamKey, &progress); err != nil {
		t.Fatal(err)
	}
	if progress.Seq != 7 {
		t.Fatalf("checkpoint = %d, want 7", progress.Seq)
	}

	progress.CheckedAt = time.Now().Add(-25 * time.Hour)
	if err := st.PutSetting(ctx, memoryDreamKey, progress); err != nil {
		t.Fatal(err)
	}
	s.memoryDream(ctx)
	if f.judged != 1 {
		t.Fatalf("aging the checkpoint replayed the sequence: model calls = %d, want 1", f.judged)
	}
}
