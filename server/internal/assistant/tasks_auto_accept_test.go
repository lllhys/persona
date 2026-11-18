package assistant

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"testing/synctest"

	"github.com/nwverse/persona/server/internal/proto"
)

// The Agents Persona itself starts run with Allow all on Claude Code and on a provider whose approval is an Auto Accept
// switch (#8141). On a computer whose engine is Paseo the launchers are its providers (node/runtimes_registry.go):
// one running Claude Code (no agent), one running Codex, Grok over ACP with Auto Accept, and pi with neither modes nor
// the switch.
func paseoLaunchers() []proto.Launcher {
	return []proto.Launcher{
		{Name: "my-claude", Source: "paseo"},
		{Name: "my-codex", Agent: "codex", Source: "paseo"},
		{Name: "grok", Agent: "paseo", Source: "paseo", AutoAccept: true},
		{Name: "pi", Agent: "paseo", Source: "paseo"},
	}
}

// agent_start: Allow all where it applies; Codex keeps its sandbox and pi its own way, as before. Only a Claude start
// names its session (a Claude Code id), whatever its mode.
func TestAgentStartAllowsAllOnAnAutoAcceptProvider(t *testing.T) {
	for _, tc := range []struct {
		launcher, want string
		names          bool
	}{
		{"my-claude", "bypassPermissions", true},
		{"grok", "bypassPermissions", false},
		{"my-codex", "", false},
		{"pi", "", false},
	} {
		t.Run(tc.launcher, func(t *testing.T) {
			s, f := newQuietService(t)
			f.launcherList = paseoLaunchers()
			args, err := json.Marshal(map[string]any{"machine": "box", "cwd": "/code/app", "prompt": "Inspect the failure", "launcher": tc.launcher})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := s.runTool(context.Background(), "server", "conv1", "agent_start", args); err != nil {
				t.Fatal(err)
			}
			if len(f.starts) != 1 || f.starts[0]["launcher"] != tc.launcher {
				t.Fatalf("starts = %v", f.starts)
			}
			if got, _ := f.starts[0]["permission_mode"].(string); got != tc.want {
				t.Fatalf("permission_mode = %q, want %q; start = %v", got, tc.want, f.starts[0])
			}
			if _, named := f.starts[0]["session_id"]; named != tc.names {
				t.Fatalf("session_id named = %v, want %v; start = %v", named, tc.names, f.starts[0])
			}
		})
	}
}

// A writable planned task: its card shows the mode that runs, and the start runs in it. With the manager's default at
// Auto, Grok's card used to say Auto, a mode it never applies, and its Agent asked for every tool; now it is Allow all,
// as Claude Code's. Codex and pi take the settled mode, as they did.
func TestPlannedTaskOnAnAutoAcceptProviderAllowsAll(t *testing.T) {
	want := map[string]string{"my-claude": "bypassPermissions", "grok": "bypassPermissions", "my-codex": "auto", "pi": "auto"}
	for _, launcher := range []string{"my-claude", "grok", "my-codex", "pi"} {
		t.Run(launcher, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				s, f := readonlyTaskService(t)
				f.launcherList = paseoLaunchers()
				setSettings(t, s, func(v *Settings) { v.DefaultPermissionMode = "auto" })
				task := PlanTask{Title: "Fix login", Prompt: "Fix the login flow and add tests.", Machine: "box", Cwd: "/code/app", Launcher: launcher}
				if _, err := s.toolTaskPlan(context.Background(), "writable-chat", readonlyTaskArgs(t, task)); err != nil {
					t.Fatal(err)
				}
				synctest.Wait()
				p := readonlyFeedPlan(t, s, "writable-chat")
				if p.State != planOpen || p.Tasks[0].Permission != want[launcher] || len(f.starts) != 0 {
					t.Fatalf("waiting card = %+v, starts = %v; want permission %s", p, f.starts, want[launcher])
				}
				if _, err := s.startPlan(context.Background(), p.ID, nil, false); err != nil {
					t.Fatal(err)
				}
				synctest.Wait()
				f.mu.Lock()
				defer f.mu.Unlock()
				if len(f.starts) != 1 || f.starts[0]["launcher"] != launcher || f.starts[0]["permission_mode"] != want[launcher] {
					t.Fatalf("start = %v, want permission_mode %s", f.starts, want[launcher])
				}
			})
		})
	}
}

// A read-only task on an Auto Accept provider starts as a Claude one does (docs/assistant/todos.md "Read-only planned
// tasks"): Allow all, held to reading by its first message. It used to stay in plan, which only Claude Code holds on
// Paseo, so the bridge refused it. Codex and pi keep plan.
func TestReadonlyTaskOnAnAutoAcceptProviderAllowsAll(t *testing.T) {
	want := map[string]string{"grok": "bypassPermissions", "my-codex": "plan", "pi": "plan"}
	for _, launcher := range []string{"grok", "my-codex", "pi"} {
		t.Run(launcher, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				s, f := readonlyTaskService(t)
				f.launcherList = paseoLaunchers()
				task := readonlyTestTask("")
				task.Launcher = launcher
				if _, err := s.toolTaskPlan(context.Background(), "readonly-chat", readonlyTaskArgs(t, task)); err != nil {
					t.Fatal(err)
				}
				synctest.Wait()
				if p := readonlyFeedPlan(t, s, "readonly-chat"); p.Tasks[0].Permission != want[launcher] || !p.Tasks[0].Readonly {
					t.Fatalf("saved read-only plan = %+v, want permission %s", p, want[launcher])
				}
				f.mu.Lock()
				defer f.mu.Unlock()
				if len(f.starts) != 1 || f.starts[0]["permission_mode"] != want[launcher] {
					t.Fatalf("read-only start = %v, want permission_mode %s", f.starts, want[launcher])
				}
				if prompt, _ := f.starts[0]["prompt"].(string); strings.Count(prompt, "本任务只读：不改项目文件、不提交、不对外写入") != 1 {
					t.Fatalf("read-only prompt = %q", prompt)
				}
			})
		})
	}
}

// A computer too busy to list its launchers at the start: the last list the hub saw there says the provider has Auto
// Accept, so the task still runs with Allow all (launcher_fit.go lastSeenAgent).
func TestTaskOnSlowListKeepsAutoAccept(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s, f := readonlyTaskService(t)
		f.launcherList = nil // the live list can't be read
		s.models.zshrcMu.Lock()
		s.models.zshrc = map[string][]proto.Launcher{"box": paseoLaunchers()}
		s.models.zshrcMu.Unlock()
		task := readonlyTestTask("plan")
		task.Launcher = "grok"
		if _, err := s.toolTaskPlan(context.Background(), "slow-chat", readonlyTaskArgs(t, task)); err != nil {
			t.Fatal(err)
		}
		synctest.Wait()
		if len(f.starts) != 1 || f.starts[0]["permission_mode"] != "bypassPermissions" {
			t.Fatalf("start = %v, want bypassPermissions", f.starts)
		}
	})
}

// An Agent a person starts from New Agent keeps the mode they chose, on an Auto Accept provider too: only the Agents
// Persona starts itself run with Allow all (agents.start_params leaves the mode alone).
func TestPersonsStartOnAnAutoAcceptProviderKeepsItsMode(t *testing.T) {
	f := newFakeHub()
	f.launcherList = paseoLaunchers()
	s := newTestService(t, f)
	for _, mode := range []string{"default", "acceptEdits", ""} {
		params := map[string]any{"cwd": "/code", "launcher": "grok", "launch": "engine"}
		if mode != "" {
			params["permission_mode"] = mode
		}
		p, err := s.AgentStartParams(context.Background(), AgentStartParams{MachineID: "box", Params: params})
		if err != nil {
			t.Fatal(err)
		}
		if got, _ := p["permission_mode"].(string); got != mode || p["launcher"] != "grok" {
			t.Fatalf("mode %q: start params %v", mode, p)
		}
	}
}

// On a computer whose engine is Paseo, every start's launcher is one of Paseo's providers (launcher_fit_engine.go,
// #8140): Grok's Agents that Persona starts run with Allow all there too, its plan card says so, and a launcher Paseo
// lacks that stands in on Paseo's Codex keeps the settled mode as Codex does in a terminal.
func TestAutoAcceptOnAPaseoEngineComputer(t *testing.T) {
	s, f := paseoFitHub(t)
	f.hostLaunchers["pbox"] = append(f.hostLaunchers["pbox"], proto.Launcher{Name: "grok", Source: "paseo", Agent: "paseo", AutoAccept: true})
	ctx := context.Background()
	if _, err := s.runTool(ctx, "server", "conv9", "agent_start", json.RawMessage(`{"machine":"pbox","cwd":"/home/me/code","prompt":"Reply with pong","launcher":"grok"}`)); err != nil {
		t.Fatal(err)
	}
	if _, err := s.runTool(ctx, "server", "conv9", "agent_start", json.RawMessage(`{"machine":"pbox","cwd":"/home/me/code","prompt":"Reply with pong","launcher":"codex-cpa"}`)); err != nil {
		t.Fatal(err)
	}
	starts := startsOf(f)
	if len(starts) != 2 || starts[0]["launcher"] != "grok" || starts[0]["permission_mode"] != "bypassPermissions" || starts[0]["session_id"] != nil {
		t.Fatalf("grok's start: %v", starts)
	}
	if starts[1]["launcher"] != "codex-cpa" || starts[1]["permission_mode"] != nil {
		t.Fatalf("codex-cpa's start: %v", starts[1])
	}
	res, err := s.toolTaskPlan(ctx, "conv9", json.RawMessage(`{"tasks":[{"title":"Ping","prompt":"Reply with pong","machine":"pbox","cwd":"~/code/app","launcher":"grok","why":"A quick check"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	p, err := s.loadPlan(ctx, planID(t, res))
	if err != nil {
		t.Fatal(err)
	}
	if p.Tasks[0].Launcher != "grok" || p.Tasks[0].Permission != "bypassPermissions" {
		t.Fatalf("grok's card: %+v", p.Tasks[0])
	}
}
