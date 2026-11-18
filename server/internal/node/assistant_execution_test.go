package node

import (
	"context"
	"encoding/json"
	"os"
	"testing"

	"github.com/nwverse/persona/server/internal/proto"
)

func TestAssistantExecutionReportsRegisteredNativeProcess(t *testing.T) {
	t.Setenv("PERSONA_EXECUTION_CONTAINER", "")
	n := &Node{procs: map[string]*assistantProc{
		"main": {id: "main", agent: "claude", ready: make(chan struct{})},
	}}
	ctx := context.Background()
	if got := n.assistantExecution(ctx, "main"); got.State != proto.ExecutionStarting {
		t.Fatalf("native process not ready: %+v", got)
	}
	n.procs["main"].started()
	host, err := os.Hostname()
	if err != nil || host == "" {
		t.Fatalf("execution hostname: %q %v", host, err)
	}
	if got := n.assistantExecution(ctx, "main"); got.State != proto.ExecutionRunning || got.Source != "native" ||
		got.Harness != "claude" || got.Location == nil || got.Location.Host != host || got.Location.Container != "" {
		t.Fatalf("native execution: %+v", got)
	}
	t.Setenv("PERSONA_EXECUTION_CONTAINER", "example-container")
	if got := n.assistantExecution(ctx, "main"); got.Location == nil || got.Location.Container != "example-container" {
		t.Fatalf("explicit container: %+v", got)
	}
}

func TestAssistantExecutionUsesOwningRuntimeOnly(t *testing.T) {
	t.Setenv("PERSONA_EXECUTION_CONTAINER", "node-container")
	for _, capable := range []bool{false, true} {
		name := "legacy"
		if capable {
			name = "reported"
		}
		t.Run(name, func(t *testing.T) {
			n, hub := runtimeNode(t)
			assistantHub(hub)
			hello := assistantRuntimeHello()
			hello["caps"].(map[string]any)["execution"] = capable
			host := dialRuntime(t, n, hello)
			id := startedAssistant(t, n, host)
			host.answer = func(method string, params json.RawMessage) (any, error) {
				var p map[string]string
				_ = json.Unmarshal(params, &p)
				if method != "session.execution" || p["session_id"] != id {
					t.Errorf("wrong runtime lookup: %s %s", method, params)
				}
				return proto.AssistantExecution{State: proto.ExecutionRunning, Location: &proto.ExecutionLocation{Container: "runtime-container"}}, nil
			}
			raw, err := nodeCall(t, n, "assistant.execution", map[string]string{"session_id": id})
			var got proto.AssistantExecution
			if err != nil || json.Unmarshal(raw, &got) != nil {
				t.Fatalf("node reply: %s %v", raw, err)
			}
			if got.Source != "runtime" || got.Harness != "dsh" {
				t.Fatalf("runtime harness: %+v", got)
			}
			if !capable && (got.Location != nil || len(host.got("session.execution")) != 0) {
				t.Fatalf("legacy runtime location or call: %+v", got)
			}
			if capable && (got.State != proto.ExecutionRunning || got.Location == nil || got.Location.Container != "runtime-container" || got.Location.Host != "") {
				t.Fatalf("runtime location: %+v", got)
			}
		})
	}
}
