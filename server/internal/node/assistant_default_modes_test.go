package node

import (
	"context"
	"strings"
	"testing"

	"github.com/nwverse/persona/server/internal/proto"
)

// Exercise the real process startup, mode persistence, and acknowledged Claude control protocol.
func TestClaudeNewConversationPermissionModes(t *testing.T) {
	for _, mode := range proto.PermissionModes {
		t.Run(mode, func(t *testing.T) {
			f := cliTest(t)
			id, err := f.n.assistantSend(context.Background(), "", t.TempDir(), "claude", "hello", nil, mode, "", cliExec())
			if err != nil {
				t.Fatal(err)
			}
			f.idle(t, id)
			st, ok := f.n.state.get(id)
			if !ok || st.AppMode != mode {
				t.Fatalf("persisted mode: %+v", st)
			}
			p := f.proc(id)
			p.mu.Lock()
			applied := p.mode
			p.mu.Unlock()
			if applied != mode {
				t.Fatalf("applied mode: %q, want %q", applied, mode)
			}
			if mode == proto.ModeBypass {
				if argv := f.file("cli-argv"); !strings.Contains(argv, "--permission-mode "+mode) {
					t.Fatalf("start args: %q", argv)
				}
			} else if !strings.Contains(f.file("stdin.jsonl"), `"mode":"`+mode+`"`) {
				t.Fatalf("Claude did not receive mode %s: %s", mode, f.file("stdin.jsonl"))
			}
		})
	}
}
