package assistant

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/nwverse/persona/server/internal/prompts"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/ai"
)

// logBuffer keeps what the inbox logged.
type logBuffer struct {
	mu sync.Mutex
	b  strings.Builder
}

func (l *logBuffer) logf(format string, args ...any) {
	l.mu.Lock()
	defer l.mu.Unlock()
	fmt.Fprintf(&l.b, format+"\n", args...)
}

func (l *logBuffer) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.String()
}

// An upstream out of quota (docs/assistant/model-config.md "Upstream rate limits"): the first triage pass sends a few requests at most and
// logs nothing; every item stays untriaged (sorted by rule meanwhile); later passes in the pause send nothing. Once
// the quota is back every item is triaged, each asked once.
func TestInboxTriageWaitsOutAnExhaustedQuota(t *testing.T) {
	ctx := context.Background()
	var sent atomic.Int32
	var back atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/messages" {
			// Something else on the machine probing local ports (a node finding an agent's ports): not a model call.
			http.NotFound(w, r)
			return
		}
		sent.Add(1)
		if !back.Load() {
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"error":{"type":"rate_limit_error","code":"account_quota_exhausted","message":"Account usage limit reached"}}`))
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"content": []any{map[string]any{"type": "text",
			"text": `{"tier":"fyi","summary":"周五灰度","reason":"群里同步"}`}}})
	}))
	t.Cleanup(srv.Close)

	h := newInboxHub(t)
	if err := h.inboxReady(ctx); err != nil {
		t.Fatal(err)
	}
	waitNeedsYouIdle(t, h.Service)
	var logs logBuffer
	h.logf = logs.logf
	h.in.model = ai.New(ai.Config{BaseURL: srv.URL, APIKey: "k", Model: "m"})
	const items = 20
	for i := range items {
		id := fmt.Sprintf("lark:g%d:1", i)
		h.inbox.items[id] = &InboxItem{ID: id, Kind: KindGroup, Last: "1", Context: "张三：周五灰度", UpdatedAt: time.Now()}
	}
	pass := func() {
		h.triageInbox(ctx)
		h.inbox.triaging.Lock() // the pass runs in the background: wait for it
		h.inbox.triaging.Unlock()
	}
	triaged := func() (n int) {
		h.inbox.mu.Lock()
		defer h.inbox.mu.Unlock()
		for _, it := range h.inbox.items {
			if it.TriagedAt == it.Last && it.Summary == "周五灰度" {
				n++
			}
		}
		return n
	}

	pass()
	if n := sent.Load(); n < 1 || n > triageConcurrency {
		t.Fatalf("sent %d request(s) in the first pass", n)
	}
	h.inbox.mu.Lock()
	for _, it := range h.inbox.items {
		if it.TriagedAt != "" || it.Tier != TierFYI || it.Reason != "按规则" {
			t.Errorf("item %s: tier %q reason %q triaged %q", it.ID, it.Tier, it.Reason, it.TriagedAt)
		}
	}
	h.inbox.mu.Unlock()
	if strings.Contains(logs.String(), "triage") {
		t.Fatalf("logged:\n%s", logs.String())
	}
	first := sent.Load()
	pass()
	if sent.Load() != first {
		t.Fatalf("a pass during the pause sent %d request(s)", sent.Load()-first)
	}

	back.Store(true)
	time.Sleep(1100 * time.Millisecond) // Retry-After: 1
	for range 3 {
		if pass(); triaged() == items {
			break
		}
	}
	if n := triaged(); n != items || sent.Load()-first != items {
		t.Fatalf("%d of %d triaged with %d request(s) after the pause", n, items, sent.Load()-first)
	}
}

// A judge call put off by a held connection leaves the item unlinked and says nothing in the log.
func TestInboxJudgePutOffQuietly(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`{"error":{"type":"service_unavailable","code":"account_concurrency_full","message":"account concurrency is full"}}`))
	}))
	t.Cleanup(srv.Close)
	h := agentHub(t)
	var logs logBuffer
	h.logf = logs.logf
	h.in.model = ai.New(ai.Config{BaseURL: srv.URL, APIKey: "k", Model: "m"})
	for _, id := range []string{"x", "y"} {
		it := actionItem(id, time.Now())
		h.inbox.mu.Lock()
		h.inbox.items[id] = it
		h.inbox.mu.Unlock()
		it.Agent = &InboxAgent{State: AgentQueued}
		if h.linkTask(context.Background(), it, []prompts.InboxTask{{ID: "todo_1", Title: "示例"}}) || it.Agent.TodoID != "" {
			t.Fatalf("linked on a put-off call: %+v", it.Agent)
		}
	}
	if s := logs.String(); s != "" {
		t.Fatalf("logged:\n%s", s)
	}
}
