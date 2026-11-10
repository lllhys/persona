package extmgr

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/assistantwire"
	"github.com/nwverse/persona/server/internal/extension"
)

// The leaders' events (#3484) reach a running extension only when it subscribed to the topic and was granted
// leaders.read (or the legacy full grant): todos.read is not enough. All ids are placeholders.
func TestLeaderEventsReachOnlyAuthorizedExtensions(t *testing.T) {
	for _, tc := range []struct {
		name, permissions string
		allowed           bool
	}{
		{"reader", `{"hub":["leaders.read"]}`, true},
		{"todos-reader", `{"hub":["todos.read"]}`, false},
		{"legacy", `{"hub_api":true}`, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h, _ := extensionTestHub(t)
			mux := h.routes()
			token := h.cfg.Token
			dir := uiPackage(t)
			path := filepath.Join(dir, extension.ManifestFile)
			raw, _ := os.ReadFile(path)
			var manifest map[string]any
			_ = json.Unmarshal(raw, &manifest)
			var permissions map[string]any
			_ = json.Unmarshal([]byte(tc.permissions), &permissions)
			permissions["launcher_keys"] = true
			manifest["permissions"] = permissions
			manifest["contributes"].(map[string]any)["events"] = []any{map[string]any{"id": "leaders",
				"subscribe": []string{"leader.changed", "leader.report", "agent.finished"}}}
			raw, _ = json.Marshal(manifest)
			if err := os.WriteFile(path, raw, 0600); err != nil {
				t.Fatal(err)
			}
			code, pv := call(t, mux, "POST", "/api/v1/extensions/-/preview", token, `{"source":"dir:`+dir+`"}`)
			if code != 200 {
				t.Fatalf("preview %d %v", code, pv)
			}
			code, out := call(t, mux, "POST", "/api/v1/extensions/-/install", token, fmt.Sprintf(`{"preview_id":%q,"permissions_digest":%q}`, pv["preview_id"], pv["permissions_digest"]))
			if code != 200 {
				t.Fatalf("install %d %v", code, out)
			}
			waitLong(t, "leader subscriber running", func() bool { p := h.extensions.get("example-ui"); return p != nil && p.proc != nil && p.proc.Ready() })
			changed, _ := json.Marshal(assistantwire.LeaderChangedEvent{ID: "conv_leader_1", Title: "1110 | 功能 | Android parity", Project: "android-parity"})
			report, _ := json.Marshal(assistantwire.LeaderReportEvent{ID: "rep_example_1", Leader: "conv_leader_1", Kind: assistantwire.LeaderReached,
				Todo: "todo_example_1", Text: "Accepted: Android sidebar groups", Link: "https://example.com/pull/1",
				At: time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC)})
			progress, _ := json.Marshal(assistantwire.LeaderReportEvent{ID: "rep_example_2", Leader: "conv_leader_1", Kind: assistantwire.LeaderProgress,
				Text: "Reached: sidebar groups", Reached: "sidebar groups", Doing: "tablet layout", Links: []string{"https://example.com/pull/2"},
				Link: "https://example.com/pull/2", At: time.Date(2026, 11, 10, 13, 0, 0, 0, time.UTC)})
			if !h.AssistantSaid(assistantwire.EventLeaderChanged, changed) || !h.AssistantSaid(assistantwire.EventLeaderReport, report) ||
				!h.AssistantSaid(assistantwire.EventLeaderReport, progress) {
				t.Fatal("the leader events are not the extensions'")
			}
			view := func() string {
				_, out := call(t, mux, "POST", "/api/v1/extensions/example-ui/-/ui/home", token, `{}`)
				return fmt.Sprint(out["view"])
			}
			waitEventQueuesEmpty(t, h)
			if !tc.allowed {
				h.extensions.emit(assistantwire.EventLeaderChanged, map[string]any{"id": "conv_leader_1", "title": "x"})
				h.extensions.emit(assistantwire.EventLeaderReport, map[string]any{"id": "rep_example_1"})
				h.extensions.emit("agent.finished", map[string]any{})
				waitLong(t, "sentinel event delivered", func() bool { return strings.Contains(view(), "value:1") })
				if v := view(); strings.Contains(v, "leader.") {
					t.Fatalf("a leader event reached an extension that is not allowed it: %s", v)
				}
				return
			}
			h.extensions.emit("agent.finished", map[string]any{})
			waitLong(t, "leader events delivered", func() bool { return strings.Contains(view(), "value:4") })
			v := view()
			for _, w := range []string{`<leader.changed `, `"id":"conv_leader_1"`, `"project":"android-parity"`, `<leader.report `,
				`"kind":"reached"`, `"todo":"todo_example_1"`, `"leader":"conv_leader_1"`, `"link":"https://example.com/pull/1"`,
				`"kind":"progress"`, `"doing":"tablet layout"`, `"links":["https://example.com/pull/2"]`} {
				if !strings.Contains(v, w) {
					t.Fatalf("the leader events lack %s: %s", w, v)
				}
			}
		})
	}
}

// What the assistant says must have the fields the schema requires: anything else never reaches the extensions.
func TestLeaderEventsRejectInvalidPayloads(t *testing.T) {
	h := newTestManager(t)
	h.extensions = &extensions{}
	for _, raw := range []string{`{}`, `[]`, `null`, `{"title":"x"}`} {
		h.AssistantSaid(assistantwire.EventLeaderChanged, json.RawMessage(raw))
	}
	for _, raw := range []string{`{}`, `{"id":"r","leader":"l","todo":"t","kind":"bogus"}`, `{"id":"r","leader":"l","kind":"decision"}`, `{"id":"r","kind":"stuck","todo":"t"}`,
		`{"leader":"l","kind":"stuck","todo":"t"}`, `{"id":"r","leader":"l","kind":"stuck"}`} {
		h.AssistantSaid(assistantwire.EventLeaderReport, json.RawMessage(raw))
	}
	q := &h.extensions.todoEvents
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.running || len(q.pending) > 0 {
		t.Fatalf("an invalid event was queued: %+v", q.pending)
	}
}

// A decision about a plan and the leader's own progress have no to-do and still go to the extensions (#3608, #3679).
func TestLeaderReportsWithoutATodo(t *testing.T) {
	for _, c := range []struct {
		p  assistantwire.LeaderReportEvent
		ok bool
	}{
		{assistantwire.LeaderReportEvent{ID: "r", Leader: "l", Kind: assistantwire.LeaderDecision, Plan: "plan_1"}, true},
		{assistantwire.LeaderReportEvent{ID: "r", Leader: "l", Kind: assistantwire.LeaderProgress, Reached: "x"}, true},
		{assistantwire.LeaderReportEvent{ID: "r", Leader: "l", Kind: assistantwire.LeaderDecision, Todo: "t"}, true},
		{assistantwire.LeaderReportEvent{ID: "r", Leader: "l", Kind: assistantwire.LeaderDecision}, false},
		{assistantwire.LeaderReportEvent{ID: "r", Leader: "l", Kind: assistantwire.LeaderReached}, false},
		{assistantwire.LeaderReportEvent{ID: "r", Leader: "l", Kind: assistantwire.LeaderStuck, Plan: "plan_1"}, false},
		{assistantwire.LeaderReportEvent{ID: "r", Kind: assistantwire.LeaderProgress}, false},
	} {
		if got := validLeaderReport(c.p); got != c.ok {
			t.Errorf("%+v: %v, want %v", c.p, got, c.ok)
		}
	}
}
