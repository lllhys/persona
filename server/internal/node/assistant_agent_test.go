package node

import (
	"context"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/proto"
)

func cachedAssistantProbe(t *testing.T, launcher string, detail proto.LauncherDetail) {
	t.Helper()
	probeCache.Lock()
	at, hadAt := probeCache.at[launcher]
	got, hadGot := probeCache.got[launcher]
	probeCache.at[launcher], probeCache.got[launcher] = time.Now(), detail
	probeCache.Unlock()
	t.Cleanup(func() {
		probeCache.Lock()
		defer probeCache.Unlock()
		if hadAt {
			probeCache.at[launcher] = at
		} else {
			delete(probeCache.at, launcher)
		}
		if hadGot {
			probeCache.got[launcher] = got
		} else {
			delete(probeCache.got, launcher)
		}
	})
}

func TestAssistantAgentBuiltinCodexSurvivesFailedProbe(t *testing.T) {
	cachedAssistantProbe(t, "codex", proto.LauncherDetail{Error: "codex did not start an agent when tried"})
	n := &Node{runtimes: newRuntimeRegistry()}
	if agent, err := n.assistantAgent(context.Background(), "codex"); err != nil || agent != "codex" {
		t.Fatalf("assistantAgent(codex) = %q, %v; want codex despite a failed probe", agent, err)
	}
}

func TestAssistantAgentDiscoveredCodexAliasSurvivesFailedProbe(t *testing.T) {
	const launcher = "codex-assistant"
	withCodexLaunchers(t, map[string][]string{launcher: {"codex", "-c", "model_reasoning_effort=high"}})
	cachedAssistantProbe(t, launcher, proto.LauncherDetail{Error: "launcher did not start an agent when tried"})
	n := &Node{runtimes: newRuntimeRegistry()}
	if agent, err := n.assistantAgent(context.Background(), launcher); err != nil || agent != "codex" {
		t.Fatalf("assistantAgent(%s) = %q, %v; want codex despite a failed probe", launcher, agent, err)
	}
}

func TestAssistantAgentManagedCodexSurvivesFailedProbe(t *testing.T) {
	const launcher = "managed-codex"
	cachedAssistantProbe(t, launcher, proto.LauncherDetail{Error: "probe failed"})
	n := &Node{runtimes: newRuntimeRegistry()}
	n.setManaged([]proto.ManagedLauncher{{Name: launcher, Agent: "codex"}})
	if agent, err := n.assistantAgent(context.Background(), launcher); err != nil || agent != "codex" {
		t.Fatalf("assistantAgent(%s) = %q, %v; want codex despite a failed probe", launcher, agent, err)
	}
}

func TestAssistantAgentCustomLaunchersStillUseProbe(t *testing.T) {
	for _, tc := range []struct {
		name   string
		detail proto.LauncherDetail
		want   string
	}{
		{"custom-codex", proto.LauncherDetail{Agent: "codex"}, "codex"},
		{"custom-claude", proto.LauncherDetail{Agent: "claude"}, "claude"},
		{"custom-unavailable", proto.LauncherDetail{Error: "probe failed"}, "claude"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cachedAssistantProbe(t, tc.name, tc.detail)
			n := &Node{runtimes: newRuntimeRegistry()}
			if agent, err := n.assistantAgent(context.Background(), tc.name); err != nil || agent != tc.want {
				t.Fatalf("assistantAgent(%s) = %q, %v; want %s", tc.name, agent, err, tc.want)
			}
		})
	}
}

func TestAssistantAgentRuntimeCanOfferCodexLauncher(t *testing.T) {
	// A shell launcher of the same name wins over the runtime (assistantRuntime), and the shell's list is a package
	// cache: an earlier test that listed this computer's real shell (one with codex installed) left "codex" in it, so
	// the test failed from its second run in a process on. It is about a shell without one.
	shellLaunchers(t)
	n, _ := runtimeNode(t)
	hello := assistantRuntimeHello()
	hello["launcher"] = "codex"
	_ = dialRuntime(t, n, hello)
	if agent, err := n.assistantAgent(context.Background(), "codex"); err != nil || agent != assistantOnRuntime {
		t.Fatalf("assistantAgent(codex) = %q, %v; want runtime", agent, err)
	}
}

func TestAssistantAgentRuntimeTakesPriorityOverDiscoveredCodexAlias(t *testing.T) {
	const launcher = "codex-assistant"
	withCodexLaunchers(t, map[string][]string{launcher: {"codex"}})
	n, _ := runtimeNode(t)
	hello := assistantRuntimeHello()
	hello["launcher"] = launcher
	_ = dialRuntime(t, n, hello)
	if agent, err := n.assistantAgent(context.Background(), launcher); err != nil || agent != assistantOnRuntime {
		t.Fatalf("assistantAgent(%s) = %q, %v; want runtime", launcher, agent, err)
	}
}
