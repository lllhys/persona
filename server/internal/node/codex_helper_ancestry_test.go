package node

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/nwverse/persona/server/internal/proto"
)

func TestCodexHelperParentSurvivesAppServerAndHistory(t *testing.T) {
	for _, legacy := range []bool{true, false} {
		name := "secondary-home"
		if legacy {
			name = "legacy-home"
		}
		t.Run(name, func(t *testing.T) {
			n := remoteTestNode(t)
			home := n.codexHome()
			if !legacy {
				home = t.TempDir()
			}
			if err := os.MkdirAll(home, 0o700); err != nil {
				t.Fatal(err)
			}
			for _, id := range []string{remoteRaw, remoteOther} {
				meta := map[string]any{"id": id, "originator": "codex-tui", "cwd": home, "source": "cli"}
				if id == remoteOther {
					meta["source"] = map[string]any{"subagent": map[string]any{"thread_spawn": map[string]any{"parent_thread_id": remoteRaw}}}
				}
				line, err := json.Marshal(map[string]any{"type": "session_meta", "payload": meta})
				if err != nil {
					t.Fatal(err)
				}
				path := filepath.Join(home, id+".jsonl")
				if err := os.WriteFile(path, append(line, '\n'), 0o600); err != nil {
					t.Fatal(err)
				}
				n.registerCodex(home, id, path)
			}
			parentID := n.registerCodex(home, remoteRaw, "")
			childID := n.registerCodex(home, remoteOther, "")
			f, c := connectRemoteFixture(t, n, home, "helpers", remoteRaw, remoteOther)
			f.mu.Lock()
			f.response = func(method string, params map[string]any) (any, bool) {
				if method != "thread/read" {
					return nil, false
				}
				id := params["threadId"].(string)
				thread := map[string]any{"id": id, "cwd": home, "status": map[string]any{"type": "active"}}
				if id == remoteOther {
					thread["parentThreadId"] = remoteRaw
					thread["name"] = "Bounded read-only helper"
				}
				return map[string]any{"thread": thread}, true
			}
			f.mu.Unlock()
			if err := c.refresh(); err != nil {
				t.Fatal(err)
			}

			for _, source := range []string{"codex_app_server", "codex_history"} {
				t.Run(source, func(t *testing.T) {
					if source == "codex_history" {
						f.mu.Lock()
						f.threads = nil
						f.mu.Unlock()
						if err := c.refresh(); err != nil {
							t.Fatal(err)
						}
					}
					rows := n.codexSessions(map[string]bool{})
					byID := make(map[string]proto.Session, len(rows))
					for _, row := range rows {
						byID[row.SessionID] = row
					}
					if len(rows) != 2 || byID[parentID].IsSubagent {
						t.Fatalf("parent and helper must both remain inspectable: %+v", rows)
					}
					child := byID[childID]
					if child.SourceKind != source || !child.IsSubagent ||
						child.ParentThreadID != remoteRaw || child.ParentSessionID != parentID || child.RootSessionID != parentID {
						t.Fatalf("helper lost its listed parent: source=%s raw=%s public=%s child=%+v", source, remoteRaw, parentID, child)
					}
					if child.Capabilities == nil || !child.Capabilities.Read || child.Capabilities.Send ||
						child.Capabilities.Attach || child.Capabilities.Interrupt {
						t.Fatalf("helper must remain read-only: %+v", child.Capabilities)
					}
				})
			}
			if f.count("thread/resume") != 0 || f.count("turn/start") != 0 {
				t.Fatal("listing a helper must not resume it or start a turn")
			}
		})
	}
}
