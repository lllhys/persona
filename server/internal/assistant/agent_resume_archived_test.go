package assistant

import (
	"encoding/json"
	"errors"
	"maps"
	"strings"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/proto"
)

func TestArchivedAgentResumeUsesNodeStopCause(t *testing.T) {
	for _, tc := range []struct {
		name     string
		snapshot proto.StopCause
		info     map[string]any
		readErr  error
		plain    bool
		want     string
	}{
		{"stop after snapshot", proto.StopUnknown, map[string]any{"stop_cause": proto.StopTaskCompleted}, nil, false, ""},
		{"idle", proto.StopUnknown, map[string]any{"stop_cause": proto.StopIdle}, nil, false, ""},
		{"restart", proto.StopUnknown, map[string]any{"stop_cause": proto.StopRestart}, nil, false, ""},
		{"later user close", proto.StopTaskCompleted, map[string]any{"stop_cause": proto.StopUser}, nil, false, "stop cause is user"},
		{"unknown", proto.StopTaskCompleted, map[string]any{"stop_cause": proto.StopUnknown}, nil, false, "stop cause is unknown"},
		{"old node", proto.StopTaskCompleted, map[string]any{}, nil, false, "stop cause is unknown"},
		{"unsupported cause", proto.StopTaskCompleted, map[string]any{"stop_cause": "future-cause"}, nil, false, "stop cause is unknown"},
		{"null cause", proto.StopTaskCompleted, map[string]any{"stop_cause": nil}, nil, false, "stop cause is unknown"},
		{"invalid cause", proto.StopTaskCompleted, map[string]any{"stop_cause": 1}, nil, false, "could not verify"},
		{"node read failed", proto.StopTaskCompleted, nil, errors.New("node unavailable"), false, "could not verify"},
		{"plain message", proto.StopTaskCompleted, map[string]any{"stop_cause": proto.StopTaskCompleted}, nil, true, "has stopped"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, f := newQuietService(t)
			c := stoppedBoxAgent("stop0001")
			c.Capabilities.StopCause = tc.snapshot
			// The hub inventory contains only the retained archive snapshot, not a current node row.
			rec := agentArchive{Session: archiveSnapshot(c), Archived: true, ChangedAt: time.Now()}
			f.setSessions("box", archiveRow(rec))
			todo := onBoxTask(t, s, c.SessionID, "conv1", false, "finish the fix")
			resumingNode(f)
			f.answerNode("session.info", func(machine string, raw json.RawMessage) (any, error) {
				var p map[string]any
				_ = json.Unmarshal(raw, &p)
				if machine != "box" || !maps.Equal(p, map[string]any{"session_id": c.SessionID}) {
					t.Errorf("wrong provenance read: %s %v", machine, p)
				}
				return tc.info, tc.readErr
			})
			res, err := runJSON(t, s, "conv1", "agent_send", map[string]any{
				"machine": "box", "session_id": c.SessionID, "text": "finish the fix",
				"resume": !tc.plain, "resume_why": "reuse its investigation",
			})
			f.mu.Lock()
			reads := 0
			for _, call := range f.calls {
				if call.Method == "session.info" {
					reads++
				}
			}
			f.mu.Unlock()
			wantReads := 1
			if tc.plain {
				wantReads = 0
			}
			if reads != wantReads {
				t.Fatalf("%d provenance reads, want %d", reads, wantReads)
			}
			starts, sent := startCalls(f), sentToAgent(f)
			if len(sent) != 0 {
				t.Fatalf("resume used session.send: %q", sent)
			}
			if tc.want != "" {
				if err == nil || !strings.Contains(err.Error(), tc.want) || len(starts) != 0 {
					t.Fatalf("unsafe resume: starts=%v err=%v, want %q", starts, err, tc.want)
				}
				if got := routedTexts(s, "conv1", todo.ID); len(got) != 0 {
					t.Fatalf("refused resume changed the task feed: %q", got)
				}
			} else {
				if err != nil || !strings.Contains(res, todo.ID) {
					t.Fatalf("archived resume: %s %v", res, err)
				}
				if len(starts) != 1 || !maps.Equal(starts[0], map[string]any{
					"resume_session_id": c.SessionID, "prompt": "finish the fix", "resume_automatic": true,
				}) {
					t.Fatalf("resume changed launcher, permissions or path: %v", starts)
				}
			}
			s.todos.mu.RLock()
			count := len(s.todos.todos)
			s.todos.mu.RUnlock()
			if count != 1 {
				t.Fatalf("resume replaced the original task: %d todos", count)
			}
		})
	}
}

func TestArchivedStoppedAgentResumeWithExplicitTask(t *testing.T) {
	s, f := newQuietService(t)
	c := stoppedBoxAgent("stop0001")
	c.Agent, c.SourceKind = "codex", "codex_history"
	c.Capabilities.StopCause = proto.StopUnknown // The snapshot was taken before the system stop.
	f.setSessions("box", archiveRow(agentArchive{Session: archiveSnapshot(c), Archived: true}))
	resumingNode(f)
	f.answerNode("session.info", func(string, json.RawMessage) (any, error) {
		return map[string]any{"stop_cause": proto.StopTaskCompleted}, nil
	})
	res, err := runJSON(t, s, "conv1", "agent_send", map[string]any{
		"machine": "box", "session_id": c.SessionID, "text": "fix the acceptance blocker",
		"resume": true, "resume_why": "reuse the completed investigation",
		"task": map[string]any{"title": "Fix the acceptance blocker", "done_when": []string{"acceptance passes"}},
	})
	if err != nil {
		t.Fatalf("resume with explicit task: %s %v", res, err)
	}
	got := onlyTodo(s)
	if got.ID == "" || !strings.Contains(res, got.ID) || got.DoneAt != nil ||
		got.Supervision == nil || got.Supervision.Supervisor != "conv1" ||
		len(got.Agents) != 1 || got.Agents[0].SessionID != c.SessionID {
		t.Fatalf("resume did not establish supervision: %+v", got)
	}
	starts := startCalls(f)
	if len(starts) != 1 || !maps.Equal(starts[0], map[string]any{
		"resume_session_id": c.SessionID, "prompt": "fix the acceptance blocker", "resume_automatic": true,
	}) || len(sentToAgent(f)) != 0 || taskCardWake(s, "conv1", got.ID) != wakeResumed {
		t.Fatalf("resume changed the start path or task card: %v", starts)
	}
}
