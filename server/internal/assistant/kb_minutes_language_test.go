package assistant

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/kb"
)

// promptModel answers every knowledge-base call with reply and keeps the system prompts it was asked with.
type promptModel struct {
	reply   string
	mu      sync.Mutex
	systems []string
}

func (m *promptModel) JSONMax(_ context.Context, system, _ string, _ int, out any) error {
	m.mu.Lock()
	m.systems = append(m.systems, system)
	m.mu.Unlock()
	return json.Unmarshal([]byte(m.reply), out)
}

// The owner's language on the hub reaches the knowledge base: a meeting recording that says no language of its own
// gets English minutes for an English-speaking owner, while one that says Chinese keeps Chinese.
func TestMinutesFollowTheOwnersLanguage(t *testing.T) {
	f := newFakeHub()
	f.mu.Lock()
	f.hello.Locale = "en"
	f.mu.Unlock()
	s := kbServiceOn(t, f, newKBStore(), t.TempDir(), func(string, ...any) {}) // the push's goroutines log late
	e := kbEng(t, s)
	model := &promptModel{reply: `{"title":"Export review","points":[{"text":"The export drops big entries","line":1}],` +
		`"decisions":[{"text":"Keep the cap","line":2}]}`}
	e.Model, e.ModelFor = model, func(string) kb.Model { return model }
	start := time.Now().Add(-time.Hour).Truncate(time.Second)
	for _, tc := range []struct {
		id, language string
		ask          string
		want, never  string
	}{
		{id: "44444444-4444-4444-8444-444444444444", ask: "都用English写", want: "## Key points\n", never: "讨论要点"},
		{id: "55555555-5555-4555-8555-555555555555", language: "zh-CN", ask: "用中文写", want: "## 讨论要点\n", never: "Key points"},
	} {
		u := kb.RecordingUpload{Revision: 1, StartedAt: start, Duration: 30, Device: "phone", Complete: true, Kind: kb.RecordingMeeting,
			Language: tc.language, Segments: []kb.RecordingSegment{{Start: 0, End: 9, Text: "我们先看 export 的 bug"},
				{Start: 10, End: 19, Text: "Keep the cap for now"}, {Start: 20, End: 29, Text: "上限はまだ決まっていません"}}}
		if st, err := e.PutRecording(tc.id, u, time.Now()); err != nil || st.Status != kb.RecordingStored {
			t.Fatalf("put %s: %v %v", tc.id, st, err)
		}
		model.mu.Lock()
		model.systems = nil
		model.mu.Unlock()
		if n := e.WriteMinutes(context.Background(), 2); n != 1 {
			t.Fatalf("%s: wrote %d", tc.id, n)
		}
		model.mu.Lock()
		systems := model.systems
		model.mu.Unlock()
		if len(systems) != 1 || !strings.Contains(systems[0], tc.ask) {
			t.Errorf("%s: the minutes' prompt does not ask %q: %q", tc.id, tc.ask, systems)
		}
		m, err := e.Store.ReadMinutes(tc.id)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(m.Markdown, tc.want) || strings.Contains(m.Markdown, tc.never) {
			t.Errorf("%s: markdown:\n%s", tc.id, m.Markdown)
		}
	}
}
