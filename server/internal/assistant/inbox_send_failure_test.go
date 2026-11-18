package assistant

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/assistantwire"
	"github.com/nwverse/persona/server/internal/connector"
	"github.com/nwverse/persona/server/internal/i18n"
	"github.com/nwverse/persona/server/internal/proto"
	"github.com/nwverse/persona/server/internal/rpc"
)

// What a source's failure may say: a CLI's stderr with a token, a path, the reply itself (#9125). The manager's own
// answer never carries them (internal/extmgr's TestInboxReplyFailuresAreCoded); here a message that did must still
// not reach the response or the log.
const sendFailureLeak = "stderr: t-EXAMPLE-SECRET-TOKEN at /home/example/.lark-cli: 好的"

var sendFailureSecrets = []string{"t-EXAMPLE-SECRET-TOKEN", "/home/example", "stderr"}

// codedReplier is the Feishu plugin's connector whose Reply fails with err, as the extension manager answers it.
type codedReplier struct {
	extensionLark
	err error
}

func (c *codedReplier) Reply(context.Context, connector.InboxReply) error { return c.err }

// sendFailureCall posts body to the item's route and returns the status, the raw body and the Retry-After header.
func sendFailureCall(t *testing.T, h *inboxHub, path, body string) (int, string, string) {
	t.Helper()
	req := httptest.NewRequest("POST", path, strings.NewReader(body))
	req.Header.Set("X-Client-Request-Id", "0123abcd")
	w := httptest.NewRecorder()
	h.routes().ServeHTTP(w, req)
	raw, _ := io.ReadAll(w.Body)
	return w.Code, string(raw), w.Header().Get("Retry-After")
}

// keepLogs records the service's log lines.
func keepLogs(h *inboxHub) func() string {
	var mu sync.Mutex
	var lines []string
	prev := h.logf
	h.logf = func(format string, args ...any) {
		mu.Lock()
		lines = append(lines, fmt.Sprintf(format, args...))
		mu.Unlock()
		prev(format, args...)
	}
	return func() string {
		mu.Lock()
		defer mu.Unlock()
		return strings.Join(lines, "\n")
	}
}

func noSendSecrets(t *testing.T, what, text string) {
	t.Helper()
	for _, s := range sendFailureSecrets {
		if strings.Contains(text, s) {
			t.Fatalf("%s carries %q: %s", what, s, text)
		}
	}
}

// Confirming a reply suggestion whose send failed says why, with a status and a code per reason, delivered none and
// whether sending again can work; the sentence is ours, never the source's words, and the log names the item, the
// code and the request (#9125).
func TestInboxSuggestionSendFailureReasons(t *testing.T) {
	cases := []struct {
		code      string
		status    int
		retryable bool
		data      string
	}{
		{assistantwire.CodeInboxCannotReply, http.StatusConflict, false, ""},
		{assistantwire.CodeInboxSourceUnavailable, http.StatusServiceUnavailable, false, ""},
		{assistantwire.CodeInboxSourceSignedOut, http.StatusServiceUnavailable, false, ""},
		{assistantwire.CodeInboxSendDenied, http.StatusBadGateway, false, ""},
		{assistantwire.CodeInboxSendRateLimited, http.StatusTooManyRequests, true, `{"retry_after":30}`},
		{assistantwire.CodeInboxSendFailed, http.StatusBadGateway, true, ""},
		{assistantwire.CodeInboxSendUnrecorded, http.StatusServiceUnavailable, true, ""},
	}
	for _, c := range cases {
		t.Run(c.code, func(t *testing.T) {
			h, _, it := handledHub(t)
			logs := keepLogs(h)
			h.in.mu.Lock()
			h.in.conns = []connector.Connector{&codedReplier{err: &rpc.Error{Code: c.code, Message: sendFailureLeak, Data: json.RawMessage(c.data)}}}
			h.in.mu.Unlock()
			if _, err := suggestTool(t, h, `{"kind":"reply","text":"好的"}`); err != nil {
				t.Fatal(err)
			}
			status, raw, retryAfter := sendFailureCall(t, h, "/api/v1/inbox/"+it.ID+"/suggestion", `{"version":1,"action":"confirm"}`)
			var out map[string]any
			if err := json.Unmarshal([]byte(raw), &out); err != nil {
				t.Fatal(err)
			}
			if status != c.status || out["code"] != c.code || out["delivered"] != "none" || out["retryable"] != c.retryable ||
				out["error"] != i18n.T("", inboxSendFailures[c.code].key) || out["detail"] != nil {
				t.Fatalf("%d %s", status, raw)
			}
			if c.data != "" && (out["retry_after"] != float64(30) || retryAfter != "30") {
				t.Fatalf("rate limit: %s, Retry-After %q", raw, retryAfter)
			}
			noSendSecrets(t, "the response", raw)
			l := logs()
			if !strings.Contains(l, "inbox: suggestion of "+it.ID+" not sent: "+c.code+" (request 0123abcd)") {
				t.Fatalf("log: %s", l)
			}
			noSendSecrets(t, "the log", l)
			// The event keeps its failed send (Needs you), as before.
			h.inbox.mu.Lock()
			state := it.Handling.State
			h.inbox.mu.Unlock()
			if state != HandlingFailed {
				t.Fatalf("handling %q", state)
			}
		})
	}
}

// A send that may have gone out stays in doubt on confirmation, as before: 502 assistant_in_doubt, delivered unknown,
// never retryable, with our sentence and not the error's words; the event's own conversation path (a hand-off) too.
func TestInboxSuggestionInDoubtIsNotRetryable(t *testing.T) {
	check := func(t *testing.T, h *inboxHub, it *InboxItem) {
		t.Helper()
		status, raw, _ := sendFailureCall(t, h, "/api/v1/inbox/"+it.ID+"/suggestion", `{"version":1,"action":"confirm"}`)
		var out map[string]any
		_ = json.Unmarshal([]byte(raw), &out)
		if status != http.StatusBadGateway || out["code"] != assistantwire.CodeAssistantInDoubt || out["delivered"] != "unknown" ||
			out["retryable"] != false || out["error"] != i18n.T("", "rpc-errors.in_doubt") || out["detail"] != nil {
			t.Fatalf("%d %s", status, raw)
		}
		noSendSecrets(t, "the response", raw)
	}
	t.Run("cut off", func(t *testing.T) {
		h, lark, it := handledHub(t)
		if _, err := suggestTool(t, h, `{"kind":"reply","text":"好的"}`); err != nil {
			t.Fatal(err)
		}
		h.in.inDoubt = true // the answer is lost after it went out
		check(t, h, it)
		if len(lark.got) != 1 {
			t.Fatalf("sent %d", len(lark.got))
		}
	})
	t.Run("in doubt with the source's words", func(t *testing.T) {
		h, _, it := handledHub(t)
		h.in.mu.Lock()
		h.in.conns = []connector.Connector{&codedReplier{err: &rpc.Error{Code: assistantwire.CodeAssistantInDoubt, Message: sendFailureLeak}}}
		h.in.mu.Unlock()
		if _, err := suggestTool(t, h, `{"kind":"reply","text":"好的"}`); err != nil {
			t.Fatal(err)
		}
		check(t, h, it)
	})
	t.Run("handed off", func(t *testing.T) {
		h, _, it := handledHub(t)
		setSettings(t, h.Service, func(v *Settings) { v.Main = "m1" })
		h.f.setSessions("server", proto.Session{SessionID: "s1", Status: proto.StatusWorking}, proto.Session{SessionID: "m1", Assistant: true})
		handOff(t, h, it)
		if _, err := mainSuggest(h, "m1", `{"item":"lark:f1:100","kind":"reply","text":"可以"}`); err != nil {
			t.Fatal(err)
		}
		h.in.mu.Lock()
		h.in.conns = []connector.Connector{&codedReplier{err: &rpc.Error{Code: assistantwire.CodeAssistantInDoubt, Message: sendFailureLeak}}}
		h.in.mu.Unlock()
		check(t, h, it)
	})
}

// A hand-off conversation's confirmed reply that the source refused gets the same coded answer (#9125's path).
func TestInboxHandedSuggestionSendFailure(t *testing.T) {
	h, _, it := handledHub(t)
	setSettings(t, h.Service, func(v *Settings) { v.Main = "m1" })
	h.f.setSessions("server", proto.Session{SessionID: "s1", Status: proto.StatusWorking}, proto.Session{SessionID: "m1", Assistant: true})
	handOff(t, h, it)
	if _, err := mainSuggest(h, "m1", `{"item":"lark:f1:100","kind":"reply","text":"可以"}`); err != nil {
		t.Fatal(err)
	}
	h.in.mu.Lock()
	h.in.conns = []connector.Connector{&codedReplier{err: &rpc.Error{Code: assistantwire.CodeInboxSourceSignedOut, Message: sendFailureLeak}}}
	h.in.mu.Unlock()
	status, raw, _ := sendFailureCall(t, h, "/api/v1/inbox/"+it.ID+"/suggestion", `{"version":1,"action":"confirm"}`)
	if status != http.StatusServiceUnavailable || !strings.Contains(raw, `"code":"`+assistantwire.CodeInboxSourceSignedOut+`"`) ||
		!strings.Contains(raw, `"delivered":"none"`) {
		t.Fatalf("%d %s", status, raw)
	}
	noSendSecrets(t, "the response", raw)
}

// The reply box's Send shares the reasons; an item with no way to answer it is inbox_cannot_reply (409) here.
func TestInboxReplySendFailureReasons(t *testing.T) {
	h := newInboxHub(t)
	if err := h.inboxReady(context.Background()); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	it := &InboxItem{ID: "lark:example:1", Source: "lark", Kind: "review_requested", Last: "1", StartedAt: now, UpdatedAt: now,
		Connector: &InboxConnector{Thread: "example-org/example-repo#12", CanReply: false}}
	h.inbox.mu.Lock()
	h.inbox.items[it.ID] = it
	h.inbox.mu.Unlock()
	status, raw, _ := sendFailureCall(t, h, "/api/v1/inbox/"+it.ID+"/reply", `{"text":"好的"}`)
	if status != http.StatusConflict || !strings.Contains(raw, `"code":"`+assistantwire.CodeInboxCannotReply+`"`) || !strings.Contains(raw, `"retryable":false`) {
		t.Fatalf("an item that cannot be replied to: %d %s", status, raw)
	}
	h.inbox.mu.Lock()
	it.Connector.CanReply = true
	h.inbox.mu.Unlock()
	h.in.mu.Lock()
	h.in.conns = []connector.Connector{&codedReplier{err: &rpc.Error{Code: assistantwire.CodeInboxSendDenied, Message: sendFailureLeak}}}
	h.in.mu.Unlock()
	status, raw, _ = sendFailureCall(t, h, "/api/v1/inbox/"+it.ID+"/reply", `{"text":"好的"}`)
	if status != http.StatusBadGateway || !strings.Contains(raw, `"code":"`+assistantwire.CodeInboxSendDenied+`"`) {
		t.Fatalf("a refused reply: %d %s", status, raw)
	}
	noSendSecrets(t, "the response", raw)
}
