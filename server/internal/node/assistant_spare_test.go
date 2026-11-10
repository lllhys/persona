package node

import (
	"testing"
	"time"
)

func TestAdoptSpareMCPPolicy(t *testing.T) {
	for _, tc := range []struct {
		name, agent      string
		started, current bool
		want             bool
	}{
		{"legacy unchanged", "claude", false, false, true},
		{"canonical unchanged", "claude", true, true, true},
		{"hub upgraded", "claude", false, true, false},
		{"hub downgraded", "claude", true, false, false},
		{"codex unchanged", "codex", false, true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			n := assistantNode(t)
			n.hubPersonaContracts.Store(tc.current)
			p := &assistantProc{id: "spare", agent: tc.agent, launcher: tc.agent, cwd: n.home, born: time.Now(), personaMCPOnly: tc.started,
				fingerprint: n.executionFingerprint(tc.agent, modelExecution{})}
			n.spare = p
			got := n.adoptSpare(n.home, tc.agent)
			if (got == p) != tc.want {
				t.Fatalf("adopted=%v, want %v", got == p, tc.want)
			}
			if tc.want {
				if n.spare != nil || n.procs[p.id] != p {
					t.Fatal("matching spare did not become the conversation")
				}
			} else if n.spare != p || len(n.procs) != 0 {
				t.Fatal("mismatched spare became a conversation")
			}
		})
	}
}

// A manager can retain its node connection while reconnecting to a different hub.
// An unused process must then be rebuilt with the new hub's registration policy.
func TestAssistantWarmReplacesMismatchedMCPPolicy(t *testing.T) {
	for _, initial := range []bool{false, true} {
		name := "hub upgraded"
		if initial {
			name = "hub downgraded"
		}
		t.Run(name, func(t *testing.T) {
			fakeClaudeHome(t, "if [ \"$1\" = \"--version\" ]; then echo '2.1.320'; exit 0; fi\necho '{\"type\":\"system\",\"subtype\":\"init\"}'\nwhile read line; do :; done\n")
			n := codexTestNode(t)
			withFakeHub(t, n)
			cwd := t.TempDir()
			n.hubPersonaContracts.Store(initial)
			if err := n.assistantWarm(cwd, "claude"); err != nil {
				t.Fatal(err)
			}
			n.mu.Lock()
			old := n.spare
			n.mu.Unlock()
			if old == nil {
				t.Fatal("no initial spare")
			}
			t.Cleanup(old.stop)
			if old.personaMCPOnly != initial {
				t.Fatalf("initial policy=%v, want %v", old.personaMCPOnly, initial)
			}
			n.hubPersonaContracts.Store(!initial)
			if err := n.assistantWarm(cwd, "claude"); err != nil {
				t.Fatal(err)
			}
			n.mu.Lock()
			replacement := n.spare
			n.mu.Unlock()
			if replacement == nil || replacement == old {
				t.Fatal("reused the spare from the previous hub capability")
			}
			t.Cleanup(replacement.stop)
			if replacement.personaMCPOnly == initial {
				t.Fatal("replacement kept the previous registration policy")
			}
			select {
			case <-old.exited:
			case <-time.After(5 * time.Second):
				t.Fatal("old spare did not exit")
			}
			if err := n.assistantWarm(cwd, "claude"); err != nil {
				t.Fatal(err)
			}
			n.mu.Lock()
			kept := n.spare
			n.mu.Unlock()
			if kept != replacement {
				t.Fatal("matching registration policy needlessly replaced the spare")
			}
		})
	}
}
