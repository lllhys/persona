package node

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nwverse/persona/server/internal/proto"
)

// Feed the real discovery parser into the node's normal launcher and assistant paths: installing the plain CLI
// must not turn it into an explicit shell override of its assistant-capable companion (#9181).
func TestAgyCommandOnPathUsesConnectedAssistantRuntime(t *testing.T) {
	for _, override := range []string{"", "function", "alias", "helper"} {
		t.Run("override="+override, func(t *testing.T) {
			parts := []string{"", "", "", "", "", "", "", "/opt/agent/bin/agy", "", ""}
			switch override {
			case "function":
				parts[8] = "agy\n"
			case "alias":
				parts[1] = "agy\x1f/opt/custom/bin/agy\x1f\x1e"
			case "helper":
				parts[0] = "agy https://gateway.example.com custom-model\n"
			}
			ls, _, _, _ := mergeLaunchers(launcherBegin + strings.Join(parts, "\x1d") + launcherEnd)
			withShellLaunchers(t) // owns and restores the global discovery cache
			launcherCache.Lock()
			launcherCache.list = ls
			launcherCache.Unlock()
			n, hub := runtimeNode(t)
			assistantHub(hub)
			if n.runtimeOf("agy") != nil {
				t.Fatal("no connected runtime: keep the terminal fallback")
			}
			hello := assistantRuntimeHello()
			hello["runtime"], hello["agent"], hello["launcher"] = "agy", "agy", "agy"
			hello["launchers"] = []map[string]any{{"name": "agy", "agent": "agy", "models": []map[string]any{{"id": "runtime-model"}}}}
			host := dialRuntime(t, n, hello)
			host.answer = func(method string, _ json.RawMessage) (any, error) {
				if method == "session.start" {
					return map[string]any{"session": map[string]any{"session_id": "agy-assistant-test", "cwd": n.home, "status": "idle"}}, nil
				}
				return map[string]any{"ok": true}, nil
			}
			raw, err := nodeCall(t, n, "launchers.list", map[string]any{})
			if err != nil {
				t.Fatal(err)
			}
			var listed struct{ Launchers []proto.Launcher }
			if err := json.Unmarshal(raw, &listed); err != nil {
				t.Fatal(err)
			}
			var picks []proto.Launcher
			for _, l := range listed.Launchers {
				if l.Name == "agy" {
					picks = append(picks, l)
				}
			}
			if len(picks) != 1 {
				t.Fatalf("ambiguous AGY launcher: %+v", picks)
			}
			if override != "" {
				if picks[0].Assistant || n.runtimeOf("agy") != nil || n.assistantRuntime("agy") != nil {
					t.Fatal("the runtime took an explicit shell override")
				}
				return
			}
			if !picks[0].Assistant || picks[0].Source != "agy" {
				t.Fatalf("plain CLI shadowed the assistant runtime: %+v", picks[0])
			}
			if agent, err := n.assistantAgent(context.Background(), "agy"); err != nil || agent != assistantOnRuntime {
				t.Fatalf("assistant agent: %s %v", agent, err)
			}
			models, err := nodeCall(t, n, "launchers.models", map[string]any{"name": "agy"})
			if err != nil || !strings.Contains(string(models), `"id":"runtime-model"`) {
				t.Fatalf("runtime model catalog: %s %v", models, err)
			}
			if _, err := nodeCall(t, n, "assistant.send", map[string]any{"launcher": "agy",
				"cwd": filepath.Join(n.home, ".persona", "assistant"), "text": "test task", "model": "runtime-model"}); err != nil {
				t.Fatal(err)
			}
			if len(host.got("session.start")) != 1 || len(host.got("session.send")) != 1 {
				t.Fatal("assistant task did not use the runtime protocol")
			}
			var start map[string]any
			_ = json.Unmarshal(host.got("session.start")[0], &start)
			if start["assistant"] != true || start["model"] != "runtime-model" {
				t.Fatalf("assistant settings changed: %+v", start)
			}
			_ = host.conn.Close()
			waitFor(t, "terminal fallback after disconnect", func() bool { return n.runtimeOf("agy") == nil })
			if n.assistantRuntime("agy") != nil {
				t.Fatal("disconnected runtime remained an assistant launcher")
			}
			raw, err = nodeCall(t, n, "launchers.list", map[string]any{})
			listed.Launchers = nil
			if err != nil || json.Unmarshal(raw, &listed) != nil {
				t.Fatalf("disconnected launcher list: %s %v", raw, err)
			}
			if l := byName(listed.Launchers)["agy"]; l.Name == "" || l.Assistant || l.Source != "zshrc" {
				t.Fatalf("terminal fallback was lost: %+v", l)
			}
		})
	}
}
