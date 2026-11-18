package node

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nwverse/persona/server/internal/host"
	"github.com/nwverse/persona/server/internal/proto"
)

// The client's Close has passed its last eligibility check, but discovery has
// already removed the Agent by the time the node receives the request.
func TestExplicitCloseAfterAgentExit(t *testing.T) {
	n, f, cwd := keptAgent(t)
	n.state.put(sidA, sessionState{Launcher: "claude", Cwd: cwd, Pane: "w1:p1"})
	if _, err := request(t, n, "session.close", map[string]any{"session_id": sidA}); err != nil {
		t.Fatal(err)
	}
	if calls := f.trace("pane.close", "tab.close"); len(calls) != 1 {
		t.Fatalf("explicit Close succeeded without closing its retained terminal: %v", calls)
	}
}

func closeAfterExitFixture(t *testing.T) (*Node, *fakeHerdr, string) {
	t.Helper()
	n, f, cwd := keptAgent(t)
	n.state.put(sidA, sessionState{Launcher: "claude", Cwd: cwd, Pane: "w1:p1"})
	return n, f, cwd
}

func TestExplicitCloseAfterExitPreservesOtherPanesAndFiles(t *testing.T) {
	n, f, cwd := closeAfterExitFixture(t)
	runPullGit(t, cwd, "init", "--quiet")
	file := filepath.Join(cwd, "uncommitted.txt")
	const contents = "unfinished fixture work\n"
	if err := os.WriteFile(file, []byte(contents), 0600); err != nil {
		t.Fatal(err)
	}
	dirty := runPullGit(t, cwd, "status", "--porcelain")
	f.reply("session.snapshot", snapshot(nil, nil, nil, []any{
		shellPane("w1:p1", "w1:t1", "w1", cwd, sidA),
		shellPane("w1:p2", "w1:t1", "w1", cwd, ""),
		shellPane("w1:p3", "w1:t2", "w1", cwd, sidB),
	}))
	if _, err := request(t, n, "session.close", map[string]any{"session_id": sidA}); err != nil {
		t.Fatal(err)
	}
	if calls := f.trace("pane.close", "tab.close", "workspace.close", "pane.send_keys", "pane.send_input"); len(calls) != 1 || calls[0] != `pane.close {"pane_id":"w1:p1"}` {
		t.Fatalf("Close affected more than its own pane: %v", calls)
	}
	b, err := os.ReadFile(file)
	if err != nil || string(b) != contents {
		t.Fatalf("dirty fixture file changed: %q, %v", b, err)
	}
	if got := runPullGit(t, cwd, "status", "--porcelain"); got != dirty || dirty == "" {
		t.Fatalf("fixture Git state changed: before=%q after=%q", dirty, got)
	}
}

func TestExplicitCloseAfterExitRefusesUnsafeTarget(t *testing.T) {
	for _, scenario := range []string{"missing token", "changed token", "detected agent", "agent inventory", "bound session", "node agent", "moved directory", "unknown directory", "missing tab", "missing workspace", "program foreground", "unknown foreground", "foreground error", "snapshot error"} {
		t.Run(scenario, func(t *testing.T) {
			n, f, cwd := closeAfterExitFixture(t)
			p := shellPane("w1:p1", "w1:t1", "w1", cwd, sidA)
			var agents []any
			switch scenario {
			case "missing token":
				delete(p, "tokens")
			case "changed token":
				p["tokens"] = map[string]any{personaTabToken: sidB}
			case "detected agent":
				p["agent"] = "claude"
			case "agent inventory":
				agents = []any{claudeAgent(sidB, proto.StatusWorking, "w1:p1", "w1:t1", "w1", cwd, "")}
			case "bound session":
				n.bindPane("w1:p1", sidB)
			case "node agent":
				n.agents[sidB] = host.Agent{PaneID: "w1:p1", Agent: "claude"}
			case "moved directory":
				p["cwd"] = filepath.Dir(cwd)
			case "unknown directory":
				delete(p, "cwd")
			case "missing tab":
				delete(p, "tab_id")
			case "missing workspace":
				delete(p, "workspace_id")
			case "program foreground":
				f.reply("pane.process_info", map[string]any{"process_info": map[string]any{"foreground_processes": []any{map[string]any{"name": "vim"}}}})
			case "unknown foreground":
				f.reply("pane.process_info", map[string]any{})
			case "foreground error":
				f.reply("pane.process_info", herdrFail{Code: "unavailable", Message: "fixture foreground unavailable"})
			}
			f.reply("session.snapshot", snapshot(nil, nil, agents, []any{p}))
			if scenario == "snapshot error" {
				f.reply("session.snapshot", herdrFail{Code: "unavailable", Message: "fixture snapshot unavailable"})
			}
			res, err := request(t, n, "session.close", map[string]any{"session_id": sidA})
			if err == nil || res["ok"] == true {
				t.Fatalf("unsafe target acknowledged: result=%v error=%v", res, err)
			}
			if !strings.HasSuffix(scenario, "error") {
				if code, _ := refusalOf(t, err); code != "agent_busy" {
					t.Fatalf("wrong refusal: %v", err)
				}
			}
			closesNothing(t, f)
		})
	}
}

func TestExplicitCloseAfterExitRechecksTarget(t *testing.T) {
	for _, scenario := range []string{"token removed", "rebound", "session moved", "session restarted", "agent appeared", "moved tab", "moved workspace", "moved directory", "snapshot error", "incomplete snapshot", "gone"} {
		t.Run(scenario, func(t *testing.T) {
			n, f, cwd := closeAfterExitFixture(t)
			f.after(func(c herdrCall) {
				if c.Method != "pane.process_info" {
					return
				}
				p := shellPane("w1:p1", "w1:t1", "w1", cwd, sidA)
				switch scenario {
				case "token removed":
					delete(p, "tokens")
				case "rebound":
					n.bindPane("w1:p1", sidB)
				case "session moved":
					n.bindPane("w1:p2", sidA)
				case "session restarted":
					n.mu.Lock()
					n.agents[sidA] = host.Agent{PaneID: "w1:p2", Agent: "claude"}
					n.mu.Unlock()
				case "agent appeared":
					p["agent"] = "claude"
				case "moved tab":
					p["tab_id"] = "w1:t2"
				case "moved workspace":
					p["workspace_id"] = "w2"
				case "moved directory":
					p["cwd"] = filepath.Dir(cwd)
				}
				f.reply("session.snapshot", snapshot(nil, nil, nil, []any{p}))
				if scenario == "snapshot error" {
					f.reply("session.snapshot", herdrFail{Code: "unavailable"})
				}
				if scenario == "incomplete snapshot" {
					f.reply("session.snapshot", map[string]any{})
				}
				if scenario == "gone" {
					f.reply("session.snapshot", snapshot(nil, nil, nil, nil))
				}
			})
			_, err := request(t, n, "session.close", map[string]any{"session_id": sidA})
			if scenario == "gone" {
				if err != nil {
					t.Fatal(err)
				}
			} else if err == nil {
				t.Fatal("changed or uncertain target was acknowledged")
			}
			closesNothing(t, f)
		})
	}
}

func TestExplicitCloseAfterExitAlreadyGoneAndCloseFailure(t *testing.T) {
	t.Run("fresh absence overrides stale display", func(t *testing.T) {
		n, f, _ := closeAfterExitFixture(t)
		f.reply("session.snapshot", snapshot(nil, nil, nil, nil))
		if _, err := request(t, n, "session.close", map[string]any{"session_id": sidA}); err != nil {
			t.Fatal(err)
		}
		closesNothing(t, f)
		if got := f.trace("session.snapshot"); len(got) != 1 {
			t.Fatalf("absence was not checked directly: %v", got)
		}
	})
	t.Run("explicit null inventories confirm absence", func(t *testing.T) {
		n, f, _ := closeAfterExitFixture(t)
		f.reply("session.snapshot", map[string]any{"snapshot": map[string]any{"panes": nil, "agents": nil}})
		if _, err := request(t, n, "session.close", map[string]any{"session_id": sidA}); err != nil {
			t.Fatal(err)
		}
		closesNothing(t, f)
	})
	t.Run("close failure propagates", func(t *testing.T) {
		n, f, _ := closeAfterExitFixture(t)
		f.reply("pane.close", herdrFail{Code: "unavailable", Message: "fixture close failed"})
		if _, err := request(t, n, "session.close", map[string]any{"session_id": sidA}); err == nil || !strings.Contains(err.Error(), "fixture close failed") {
			t.Fatalf("lost close error: %v", err)
		}
		if calls := f.trace("pane.close", "tab.close"); len(calls) != 1 {
			t.Fatalf("unexpected retry or tab close: %v", calls)
		}
	})
	t.Run("cached mark supplies missing saved pane", func(t *testing.T) {
		n, f, cwd := closeAfterExitFixture(t)
		n.state.put(sidA, sessionState{Launcher: "claude", Cwd: cwd})
		if _, err := request(t, n, "session.close", map[string]any{"session_id": sidA}); err != nil {
			t.Fatal(err)
		}
		if calls := f.trace("pane.close", "tab.close"); len(calls) != 1 {
			t.Fatalf("marked pane not closed: %v", calls)
		}
	})
	t.Run("legacy state without a terminal stays idempotent", func(t *testing.T) {
		n, f, _ := closeAfterExitFixture(t)
		n.state.put(sidA, sessionState{Launcher: "claude"})
		n.layouts = nil
		if _, err := request(t, n, "session.close", map[string]any{"session_id": sidA}); err != nil {
			t.Fatal(err)
		}
		if calls := f.calls(); len(calls) != 0 {
			t.Fatalf("legacy Close looked up an unrelated terminal: %v", calls)
		}
	})
}

func TestExplicitCloseAfterExitUsesNamedSessionDirectly(t *testing.T) {
	for _, scenario := range []string{"owned", "repurposed", "unavailable"} {
		t.Run(scenario, func(t *testing.T) {
			n, own, cwd := closeAfterExitFixture(t)
			h, fs := namedHerdr(t, own)
			n.hosts = host.NewRegistry(h)
			n.state.put(sidA, sessionState{Launcher: "claude", Cwd: cwd, Pane: "@build:w1:p1"})
			good := snapshot(nil, nil, nil, []any{shellPane("w1:p1", "w1:t1", "w1", cwd, sidA)})
			fs.build.reply("session.snapshot", good)
			// Prime the named session's fallback display cache with the old owner.
			if _, err := h.Snapshot(t.Context()); err != nil {
				t.Fatal(err)
			}
			fs.build.reply("pane.process_info", map[string]any{"process_info": map[string]any{"foreground_processes": []any{map[string]any{"name": "zsh"}}}})
			if scenario == "repurposed" {
				fs.build.reply("session.snapshot", snapshot(nil, nil, nil, []any{shellPane("w1:p1", "w1:t1", "w1", cwd, sidB)}))
			}
			if scenario == "unavailable" {
				fs.build.reply("session.snapshot", herdrFail{Code: "unavailable", Message: "named target unavailable"})
			}
			own.reset()
			fs.build.reset()
			fs.review.reset()
			_, err := request(t, n, "session.close", map[string]any{"session_id": sidA})
			if scenario == "owned" {
				if err != nil {
					t.Fatal(err)
				}
				if calls := fs.build.trace("pane.close", "tab.close"); len(calls) != 1 || calls[0] != `pane.close {"pane_id":"w1:p1"}` {
					t.Fatalf("wrong named target close: %v", calls)
				}
			} else {
				if err == nil {
					t.Fatal("stale named-session cache authorized Close")
				}
				closesNothing(t, fs.build)
			}
			if len(own.calls()) != 0 || len(fs.review.calls()) != 0 {
				t.Fatalf("called another session: own=%v review=%v", own.calls(), fs.review.calls())
			}
			fs.assertUntouched(t)
		})
	}
}

func TestExplicitCloseAfterExitRejectsIncompleteInventory(t *testing.T) {
	for _, scenario := range []struct {
		name  string
		reply any
	}{
		{"missing snapshot", map[string]any{}},
		{"null snapshot", map[string]any{"snapshot": nil}},
		{"missing panes", map[string]any{"snapshot": map[string]any{"agents": []any{}}}},
		{"missing agents", map[string]any{"snapshot": map[string]any{"panes": []any{}}}},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			n, f, _ := closeAfterExitFixture(t)
			f.reply("session.snapshot", scenario.reply)
			res, err := request(t, n, "session.close", map[string]any{"session_id": sidA})
			if err == nil {
				t.Fatalf("incomplete inventory acknowledged Close: %v", res)
			}
			closesNothing(t, f)
		})
	}
}

func TestExplicitCloseAfterExitRefusesFreshSessionElsewhere(t *testing.T) {
	for _, phase := range []string{"first snapshot", "after foreground"} {
		t.Run(phase, func(t *testing.T) {
			n, f, cwd := closeAfterExitFixture(t)
			restarted := snapshot(nil, nil,
				[]any{claudeAgent(sidA, proto.StatusWorking, "w1:p2", "w1:t1", "w1", cwd, "")},
				[]any{shellPane("w1:p1", "w1:t1", "w1", cwd, sidA)})
			if phase == "first snapshot" {
				f.reply("session.snapshot", restarted)
			} else {
				f.after(func(c herdrCall) {
					if c.Method == "pane.process_info" {
						f.reply("session.snapshot", restarted)
					}
				})
			}
			// Discovery and saved state are intentionally stale: only the direct inventory knows.
			if _, ok := n.agent(sidA); ok {
				t.Fatal("fixture cached a running session")
			}
			_, err := request(t, n, "session.close", map[string]any{"session_id": sidA})
			if err == nil {
				t.Fatal("fresh same-session restart was ignored")
			}
			if code, _ := refusalOf(t, err); code != "agent_busy" {
				t.Fatalf("wrong refusal: %v", err)
			}
			closesNothing(t, f)
		})
	}
}
