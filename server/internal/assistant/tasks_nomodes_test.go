package assistant

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"testing/synctest"

	"github.com/nwverse/persona/server/internal/proto"
	"github.com/nwverse/persona/server/internal/rpc"
)

// #7559: the node starts traex and omp with no permission mode and no effort, and refuses a start that names one
// (node/control.go). The hub must not send plan for a read-only task to them, nor fill in a writable task's default
// mode or a tier's effort; one the caller names is refused before anything starts. pi, cursor and agy (#8151) the node
// starts with the two ignored: the hub drops them, the caller's own included, without refusing.

const readonlyConstraint = "本任务只读：不改项目文件、不提交、不对外写入"

// noModesService is readonlyTaskService with a store (model profiles live there), and launcher agent besides claude.
func noModesService(t *testing.T, agent string) (*Service, *fakeHub) {
	t.Helper()
	f := newFakeHub()
	f.sessions["box"] = nil
	f.launcherList = []proto.Launcher{{Name: "claude", Agent: "claude"}, {Name: agent, Agent: agent}}
	f.ownModels = true
	return newServiceOn(t, f, &memStore{}), f
}

// noModesStart fails t when start carries what the node refuses for an agent that takes no modes.
func noModesStart(t *testing.T, start map[string]any) {
	t.Helper()
	for _, k := range []string{"permission_mode", "plan_then", "effort"} {
		if v, ok := start[k]; ok && v != "" {
			t.Fatalf("start sends %s %v to an agent that takes none: %v", k, v, start)
		}
	}
}

// A read-only task for pi, cursor or agy is refused instead (#8316, TestReadonlyTaskPlanRefusesLaunchersWithoutReadonly).
func TestReadonlyNonClaudeTaskCarriesTheConstraintNotPlan(t *testing.T) {
	for _, agent := range []string{"traex", "omp"} {
		t.Run(agent, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				s, f := readonlyTaskService(t)
				f.launcherList = []proto.Launcher{{Name: "claude", Agent: "claude"}, {Name: agent, Agent: agent}}
				task := readonlyTestTask("")
				task.Launcher = agent
				if _, err := s.toolTaskPlan(context.Background(), "readonly-chat", readonlyTaskArgs(t, task)); err != nil {
					t.Fatal(err)
				}
				synctest.Wait()
				p := readonlyFeedPlan(t, s, "readonly-chat")
				if p.State != planStarted || !p.Tasks[0].Readonly || p.Tasks[0].Error != "" {
					t.Fatalf("saved read-only plan = %+v", p.Tasks[0])
				}
				f.mu.Lock()
				defer f.mu.Unlock()
				if len(f.starts) != 1 || f.starts[0]["launcher"] != agent {
					t.Fatalf("starts = %v", f.starts)
				}
				noModesStart(t, f.starts[0])
				if prompt, _ := f.starts[0]["prompt"].(string); strings.Count(prompt, readonlyConstraint) != 1 {
					t.Fatalf("read-only prompt = %q", prompt)
				}
				if todo := onlyTodo(s); len(todo.Agents) != 1 {
					t.Fatalf("task did not finish dispatch: %+v", todo)
				}
			})
		})
	}
}

// A writable task to traex gets neither the owner's default permission mode (on its card, or at the start) nor its
// tier's effort; Codex and Claude keep theirs.
func TestWritableNonClaudeTaskGetsNoDefaultModeOrEffort(t *testing.T) {
	for _, tc := range []struct{ agent, wantMode string }{
		{"traex", ""},
		{"omp", ""},
		{"pi", ""},
		{"cursor", ""},
		{"codex", "acceptEdits"},
		{"claude", "bypassPermissions"},
	} {
		t.Run(tc.agent, func(t *testing.T) {
			task := readonlyTestTask("")
			task.Readonly, task.Launcher = false, tc.agent
			task.DoneWhen, task.ExpectMinutes = []string{"the login flow is fixed"}, 30
			ctx := context.Background()

			// The plan: the card's permission is what will run.
			s, f := readonlyTaskService(t)
			f.launcherList = []proto.Launcher{{Name: "claude", Agent: "claude"}, {Name: tc.agent, Agent: tc.agent}}
			setSettings(t, s, func(v *Settings) { v.DefaultPermissionMode = "acceptEdits" })
			if _, err := s.toolTaskPlan(ctx, "writable-chat", readonlyTaskArgs(t, task)); err != nil {
				t.Fatal(err)
			}
			if saved := readonlyFeedPlan(t, s, "writable-chat").Tasks[0]; saved.Permission != tc.wantMode {
				t.Fatalf("the card's permission = %q, want %q: %+v", saved.Permission, tc.wantMode, saved)
			}

			// The start, of a task on a tier with an effort (model profiles need a store).
			s, f = noModesService(t, tc.agent)
			setSettings(t, s, func(v *Settings) { v.DefaultPermissionMode = "acceptEdits" })
			saveTier(t, s, "standard", ModelTier{Launcher: tc.agent, Effort: "high"})
			task.Launcher, task.Tier = "", "standard"
			if _, _, err := s.startTaskAgent(ctx, &task, false); err != nil {
				t.Fatal(err)
			}
			if len(f.starts) != 1 || f.starts[0]["launcher"] != tc.agent {
				t.Fatalf("starts = %v", f.starts)
			}
			start := f.starts[0]
			if tc.wantMode == "" {
				noModesStart(t, start)
				if task.Effort != "" {
					t.Fatalf("the task keeps the tier's effort %q", task.Effort)
				}
				return
			}
			if start["permission_mode"] != tc.wantMode || start["effort"] != "high" {
				t.Fatalf("%s lost its mode or effort: %v", tc.agent, start)
			}
		})
	}
}

// A mode or effort the call names for traex or omp is refused by task_plan, saying why, before a plan is saved or
// anything starts. A read-only task's permission is replaced by read-only, so it is not one the call asked for. For pi
// and cursor (work agents, #8151) neither is refused: the plan saves none and the task starts without them, as the node
// would start it anyway; a read-only task for them is refused as read-only (#8316), whatever it names.
func TestNonClaudeTaskRefusesAskedModeOrEffort(t *testing.T) {
	for _, agent := range []string{"traex", "omp", "pi", "cursor"} {
		refuses := !proto.IsWorkAgentType(agent)
		for _, tc := range []struct {
			name, permission, effort, want string
			readonly                       bool
		}{
			{"writable permission", "acceptEdits", "", "takes no permission mode", false},
			{"writable effort", "", "high", "takes no effort", false},
			{"read-only effort", "", "high", "takes no effort", true},
			{"read-only permission", "bypassPermissions", "", "", true},
		} {
			t.Run(agent+"/"+tc.name, func(t *testing.T) {
				synctest.Test(t, func(t *testing.T) {
					s, f := readonlyTaskService(t)
					f.launcherList = []proto.Launcher{{Name: "claude", Agent: "claude"}, {Name: agent, Agent: agent}}
					task := readonlyTestTask(tc.permission)
					task.Readonly, task.Launcher, task.Effort = tc.readonly, agent, tc.effort
					task.DoneWhen, task.ExpectMinutes = []string{"the login flow is fixed"}, 30
					_, err := s.toolTaskPlan(context.Background(), "chat", readonlyTaskArgs(t, task))
					synctest.Wait()
					if tc.readonly && !refuses {
						var re *rpc.Error
						if !errors.As(err, &re) || re.Code != "readonly_unsupported" {
							t.Fatalf("read-only %s task: error = %v, want readonly_unsupported", agent, err)
						}
						f.mu.Lock()
						defer f.mu.Unlock()
						if len(f.starts) != 0 {
							t.Fatalf("a refused task started: %v", f.starts)
						}
						return
					}
					if tc.want == "" || !refuses {
						if err != nil {
							t.Fatalf("task refused: %v", err)
						}
						saved := readonlyFeedPlan(t, s, "chat").Tasks[0]
						if saved.Effort != "" || (!tc.readonly && saved.Permission != "") {
							t.Fatalf("the card keeps what will not run: permission %q, effort %q", saved.Permission, saved.Effort)
						}
						if tc.readonly { // started at once; a writable one waits for Start
							f.mu.Lock()
							defer f.mu.Unlock()
							if len(f.starts) != 1 || f.starts[0]["launcher"] != agent {
								t.Fatalf("starts = %v", f.starts)
							}
							noModesStart(t, f.starts[0])
						}
						return
					}
					f.mu.Lock()
					defer f.mu.Unlock()
					if err == nil || !strings.Contains(err.Error(), tc.want) || !strings.Contains(err.Error(), agent) {
						t.Fatalf("error = %v, want one saying %q", err, tc.want)
					}
					if len(f.starts) != 0 {
						t.Fatalf("a refused task started: %v", f.starts)
					}
				})
			})
		}
	}
}

// agent_start to traex or omp: an effort the call names is refused before anything starts (and before a health
// conversation's card asks the user); a tier's is dropped; no mode. pi and cursor start with neither, the call's own
// effort dropped rather than refused (#8151). Claude, Codex and a Paseo provider keep the effort they are given.
func TestAgentStartNonClaudeEffort(t *testing.T) {
	run := func(t *testing.T, agent string, input map[string]any) (*fakeHub, error) {
		t.Helper()
		s, f := newQuietServiceOn(t, &memStore{})
		f.launcherList = []proto.Launcher{{Name: "claude", Agent: "claude"}, {Name: agent, Agent: agent}}
		saveTier(t, s, "standard", ModelTier{Launcher: agent, Effort: "high"})
		input["machine"], input["cwd"], input["prompt"] = "box", "/code/app", "Fix the failure"
		args, err := json.Marshal(input)
		if err != nil {
			t.Fatal(err)
		}
		_, err = s.runTool(context.Background(), "server", "conv1", "agent_start", args)
		return f, err
	}
	oneStart := func(t *testing.T, f *fakeHub, agent string) map[string]any {
		t.Helper()
		f.mu.Lock()
		defer f.mu.Unlock()
		if len(f.starts) != 1 || f.starts[0]["launcher"] != agent {
			t.Fatalf("starts = %v", f.starts)
		}
		return f.starts[0]
	}
	for _, agent := range []string{"traex", "omp", "pi", "cursor"} {
		t.Run(agent+"/asked", func(t *testing.T) {
			f, err := run(t, agent, map[string]any{"launcher": agent, "effort": "high"})
			if proto.IsWorkAgentType(agent) {
				if err != nil {
					t.Fatalf("work agent refused: %v", err)
				}
				noModesStart(t, oneStart(t, f, agent))
				return
			}
			if err == nil || !strings.Contains(err.Error(), "takes no effort") || !strings.Contains(err.Error(), "no agent was started") {
				t.Fatalf("error = %v", err)
			}
			if len(f.starts) != 0 {
				t.Fatalf("refused start went on: %v", f.starts)
			}
		})
		t.Run(agent+"/tier", func(t *testing.T) {
			f, err := run(t, agent, map[string]any{"tier": "standard"})
			if err != nil {
				t.Fatal(err)
			}
			noModesStart(t, oneStart(t, f, agent))
		})
	}
	// Codex, and an agent a runtime starts (a Paseo provider, #8141), keep the effort: the node or the runtime judges it.
	for _, agent := range []string{"codex", "paseo"} {
		t.Run(agent+"/asked", func(t *testing.T) {
			f, err := run(t, agent, map[string]any{"launcher": agent, "effort": "high"})
			if err != nil {
				t.Fatal(err)
			}
			if start := oneStart(t, f, agent); start["effort"] != "high" {
				t.Fatalf("%s lost its effort: %v", agent, start)
			}
		})
	}
	t.Run("claude/asked", func(t *testing.T) {
		f, err := run(t, "claude", map[string]any{"launcher": "claude", "effort": "high"})
		if err != nil {
			t.Fatal(err)
		}
		if start := oneStart(t, f, "claude"); start["effort"] != "high" || start["permission_mode"] != "bypassPermissions" {
			t.Fatalf("claude lost its effort or mode: %v", start)
		}
	})
}
