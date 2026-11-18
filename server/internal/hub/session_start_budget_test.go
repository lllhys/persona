package hub

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/assistantwire"
	"github.com/nwverse/persona/server/internal/proto"
)

// Codex and compatible variants return their session only after startup even when a client asks for async: the node gets the agent
// type the launcher resolves to, and the start's budget (startBudget) follows it. What the start runs on is the
// manager's (agents.start_params), whose calls carry no deadline over the bus: the budget is checked on the decision.
func TestSessionStartBudgetMatchesNodeStartup(t *testing.T) {
	for _, tc := range []struct {
		name       string
		agentType  string
		configured string
		async      bool
		short      bool
	}{
		{name: "default Claude async", async: true, short: true},
		{name: "explicit Claude async", agentType: "claude", async: true, short: true},
		{name: "explicit Codex async", agentType: "codex", async: true},
		{name: "explicit TRAEX async", agentType: "traex", async: true},
		{name: "configured Codex async", configured: "codex", async: true},
		{name: "configured TRAEX async", configured: "traex", async: true},
		{name: "Claude sync"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newTestHub(t)
			connectAssistant(t, h) // it says what a start runs on; without it nothing starts
			startReceiptNode(t, h, func(_ context.Context, params json.RawMessage) (any, error) {
				var in struct {
					AgentType string `json:"agent_type"`
				}
				if err := json.Unmarshal(params, &in); err != nil {
					return nil, err
				}
				want := tc.agentType
				if want == "" {
					want = tc.configured
				}
				if in.AgentType != want {
					t.Errorf("node agent_type = %q, want %q", in.AgentType, want)
				}
				return map[string]any{"session": proto.Session{SessionID: "created"}}, nil
			})
			h.byID["machine"].cfg.Launchers = []assistantwire.ConfiguredLauncher{{Launcher: proto.Launcher{Name: "wrapper"}, AgentType: tc.configured}}
			body, err := json.Marshal(map[string]any{
				"machine_id": "machine", "cwd": "/code", "launcher": "wrapper", "prompt": "hello", "tab_name": "test",
				"agent_type": tc.agentType, "async": tc.async,
			})
			if err != nil {
				t.Fatal(err)
			}
			r := httptest.NewRequest("POST", "/api/v1/sessions", strings.NewReader(string(body)))
			w := httptest.NewRecorder()
			h.startSession(w, r)
			if w.Code != http.StatusOK {
				t.Fatalf("start = %d %s", w.Code, w.Body)
			}
			want := tc.agentType
			if want == "" {
				want = tc.configured
			}
			budget := startBudget(startsAsync(tc.async, "", want))
			min, max := 60*time.Second, 101*time.Second
			if tc.short {
				min, max = 10*time.Second, 16*time.Second
			}
			if budget < min || budget > max {
				t.Fatalf("operation budget = %s, want between %s and %s", budget, min, max)
			}
		})
	}
}

// A start's goal (POST /api/v1/sessions) reaches the node's session.start, where the node sets it after the first
// message: the route does not drop the documented field.
func TestSessionStartForwardsTheGoal(t *testing.T) {
	h := newTestHub(t)
	connectAssistant(t, h)
	var gotGoal string
	startReceiptNode(t, h, func(_ context.Context, params json.RawMessage) (any, error) {
		var in struct {
			Goal string `json:"goal"`
		}
		if err := json.Unmarshal(params, &in); err != nil {
			return nil, err
		}
		gotGoal = in.Goal
		return map[string]any{"session": proto.Session{SessionID: "created"}}, nil
	})
	body, err := json.Marshal(map[string]any{
		"machine_id": "machine", "cwd": "/code", "launcher": "claude", "prompt": "fix the build", "tab_name": "t",
		"goal": "CI is green",
	})
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest("POST", "/api/v1/sessions", strings.NewReader(string(body)))
	w := httptest.NewRecorder()
	h.startSession(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("start = %d %s", w.Code, w.Body)
	}
	if gotGoal != "CI is green" {
		t.Fatalf("the node got goal %q", gotGoal)
	}
}
