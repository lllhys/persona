package node

import (
	"context"
	"encoding/json"
	"slices"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/proto"
)

// Where a start goes to an engine runtime that runs only its own launchers (Paseo with caps.same_kind, #8140).

// sameKindPaseoHello is Paseo's bridge on a machine shaped like the report's: Claude Code's and Codex's built-in
// providers off, a Claude Code provider, a Codex one, and one named like a launcher of the login shell's (krcc).
func sameKindPaseoHello() map[string]any {
	h := paseoHello()
	h["caps"] = map[string]any{"start": true, "send": true, "interrupt": true, "rename": true, "close": true, "modes": true,
		"resume": true, "readonly": true, "same_kind": true}
	efforts := []string{"low", "medium", "high", "max"}
	h["launchers"] = []map[string]any{
		{"name": "claude-sub", "agent": "claude", "model": "Fable", "models": []map[string]any{{"id": "fable[1m]", "default": true, "efforts": efforts}}},
		{"name": "codex-cpa", "agent": "codex", "model": "GPT", "prompt_optional": true, "models": []map[string]any{{"id": "gpt-x", "default": true, "efforts": efforts}}},
		{"name": "krcc", "agent": "claude", "model": "Opus", "models": []map[string]any{{"id": "opus-paseo", "default": true, "efforts": efforts}}},
	}
	return h
}

// shellLaunchersOf puts launchers in the login shell's list (as withShellLaunchers does with names alone).
func shellLaunchersOf(t *testing.T, list ...proto.Launcher) {
	t.Helper()
	launcherCache.Lock()
	oldAt, oldList := launcherCache.at, launcherCache.list
	launcherCache.at, launcherCache.list = time.Now(), list
	launcherCache.Unlock()
	t.Cleanup(func() {
		launcherCache.Lock()
		launcherCache.at, launcherCache.list = oldAt, oldList
		launcherCache.Unlock()
	})
}

type listedLaunchers struct {
	Launchers []struct {
		proto.Launcher
		AgentType string `json:"agent_type"`
	} `json:"launchers"`
	Host        string `json:"host"`
	HostRuntime bool   `json:"host_runtime"`
}

func launchersFor(t *testing.T, n *Node, host string) listedLaunchers {
	t.Helper()
	params := map[string]any{}
	if host != "" {
		params["host"] = host
	}
	raw, err := nodeCall(t, n, "launchers.list", params)
	if err != nil {
		t.Fatal(err)
	}
	var ls listedLaunchers
	if err := json.Unmarshal(raw, &ls); err != nil {
		t.Fatal(err)
	}
	return ls
}

func (ls listedLaunchers) names() []string {
	var out []string
	for _, l := range ls.Launchers {
		out = append(out, l.Name+"/"+l.Source)
	}
	return out
}

// startAnswer is the node's session.start answer as the hub reads it.
type startAnswer struct {
	Session proto.Session       `json:"session"`
	Instead *proto.StartInstead `json:"instead"`
}

// On a machine whose only engine is Paseo, a start can run only Paseo's providers: the list for that host is theirs, in
// Paseo's order, a name shared with the login shell means Paseo's provider (its models, its start options), and a
// launcher Paseo does not have goes to it with the kind of agent it starts, never as "your default" (shell_launcher).
// The list without a host, which Settings and the main chat read (the assistant runs on the command line), is as before.
func TestPaseoHostRunsOnlyItsOwnLaunchers(t *testing.T) {
	shellLaunchersOf(t, proto.Launcher{Name: "krcc", Source: "zshrc"}, proto.Launcher{Name: "geminicc", Source: "zshrc"},
		proto.Launcher{Name: "codex", Agent: "codex", Source: "zshrc", PromptOptional: true}, proto.Launcher{Name: "traex", Agent: "traex", Source: "zshrc"})
	n, _ := runtimeNode(t)
	if err := n.useEngine(context.Background(), proto.EnginePaseo); err != nil {
		t.Fatal(err)
	}
	host := dialRuntime(t, n, sameKindPaseoHello())
	host.answer = func(method string, params json.RawMessage) (any, error) {
		if method != "session.start" {
			return map[string]any{"ok": true}, nil
		}
		var p struct {
			Launcher  string `json:"launcher"`
			AgentType string `json:"agent_type"`
			Effort    string `json:"effort"`
		}
		_ = json.Unmarshal(params, &p)
		res := map[string]any{"session": map[string]any{"session_id": "paseo-" + p.Launcher, "status": "idle", "cwd": "/w", "launcher": p.Launcher}}
		switch {
		case p.Launcher == "codex": // Paseo has none by that name: its Codex provider stands in
			res["session"].(map[string]any)["launcher"] = "codex-cpa"
			res["instead"] = map[string]any{"launcher": "codex", "model": "gpt-6", "effort": "bogus"}
		case p.Effort == "xhigh": // its model offers no xhigh: it runs at high
			res["session"].(map[string]any)["effort"] = "high"
			res["instead"] = map[string]any{"effort": "xhigh"}
		}
		return res, nil
	}
	waitFor(t, "Paseo's launchers", func() bool { return n.runtimes.launcherRuntime("codex-cpa") != nil })

	ls := launchersFor(t, n, proto.EnginePaseo)
	if !ls.HostRuntime || ls.Host != proto.EnginePaseo || !slices.Equal(ls.names(), []string{"claude-sub/paseo", "codex-cpa/paseo", "krcc/paseo"}) {
		t.Fatalf("the list for Paseo: %v (host %q, runtime %v)", ls.names(), ls.Host, ls.HostRuntime)
	}
	if l := ls.Launchers[1]; l.AgentType != "codex" || !l.PromptOptional {
		t.Fatalf("Paseo's Codex provider %+v: a first message is not needed", l)
	}
	if l := ls.Launchers[2]; l.AgentType != "claude" || l.Model != "Opus" {
		t.Fatalf("krcc is Paseo's provider: %+v", l)
	}
	if all := launchersFor(t, n, ""); all.HostRuntime || !slices.Contains(all.names(), "krcc/zshrc") || !slices.Contains(all.names(), "traex/zshrc") {
		t.Fatalf("the list without a host is the machine's own, as before: %v", all.names())
	}

	// A name both have: Paseo's models and start options, a start on Paseo's provider.
	raw, err := nodeCall(t, n, "launchers.models", map[string]any{"name": "krcc"})
	if err != nil {
		t.Fatal(err)
	}
	var models proto.LauncherModels
	_ = json.Unmarshal(raw, &models)
	if len(models.Models) != 1 || models.Models[0].ID != "opus-paseo" {
		t.Fatalf("krcc's models are Paseo's provider's: %s", raw)
	}
	for _, c := range []struct {
		name     string
		prompted bool
	}{{"krcc", false}, {"codex-cpa", false}} {
		raw, err := nodeCall(t, n, "session.start.options", map[string]any{"name": c.name})
		if err != nil {
			t.Fatalf("%s: start options refused a launcher a start accepts: %v", c.name, err)
		}
		var opts workStartOptions
		_ = json.Unmarshal(raw, &opts)
		if opts.PromptRequired != c.prompted {
			t.Fatalf("%s: %s", c.name, raw)
		}
	}
	if _, err := nodeCall(t, n, "session.start", map[string]any{"cwd": "/w", "launcher": "krcc", "prompt": "hi", "async": true}); err != nil {
		t.Fatal(err)
	}

	// Launchers Paseo does not have: the kind of agent each starts goes with them, the form's word first.
	for _, c := range []struct{ launcher, agentType, want string }{
		{"codex", "codex", "codex"},
		{"traex", "", "traex"},     // the login shell says so
		{"geminicc", "", "claude"}, // a Claude Code wrapper
	} {
		if _, err := nodeCall(t, n, "session.start", map[string]any{"cwd": "/w", "launcher": c.launcher, "agent_type": c.agentType,
			"model": "gpt-6", "effort": "high", "prompt": "hi", "async": true}); err != nil {
			t.Fatalf("%s: %v", c.launcher, err)
		}
		starts := host.got("session.start")
		var got map[string]any
		_ = json.Unmarshal(starts[len(starts)-1], &got)
		if got["launcher"] != c.launcher || got["agent_type"] != c.want || got["shell_launcher"] != nil {
			t.Fatalf("%s went to Paseo as %v", c.launcher, got)
		}
	}
	var first map[string]any
	_ = json.Unmarshal(host.got("session.start")[0], &first)
	if first["launcher"] != "krcc" || first["shell_launcher"] != nil {
		t.Fatalf("krcc: %v", first)
	}

	// What Paseo did not run as asked comes back with the session, a malformed part left out.
	raw, err = nodeCall(t, n, "session.start", map[string]any{"cwd": "/w", "launcher": "codex", "agent_type": "codex", "prompt": "hi", "async": true})
	if err != nil {
		t.Fatal(err)
	}
	var res startAnswer
	_ = json.Unmarshal(raw, &res)
	if res.Session.Launcher != "codex-cpa" || res.Instead == nil || *res.Instead != (proto.StartInstead{Launcher: "codex", Model: "gpt-6"}) {
		t.Fatalf("the answer: %s", raw)
	}
	raw, err = nodeCall(t, n, "session.start", map[string]any{"cwd": "/w", "launcher": "claude-sub", "effort": "xhigh", "prompt": "hi", "async": true})
	if err != nil {
		t.Fatal(err)
	}
	res = startAnswer{}
	_ = json.Unmarshal(raw, &res)
	if res.Session.Effort != "high" || res.Instead == nil || res.Instead.Effort != "xhigh" {
		t.Fatalf("a lowered effort: %s", raw)
	}
}

// A provider added in Persona (its own gateway, key and model) never runs inside Paseo: on a machine whose engine is
// Paseo a start with it goes there with the kind of agent it starts, so Paseo runs one of its own of that kind and says
// so, or refuses; the list for that host leaves it out. Paseo's provider of the same name is Paseo's.
func TestManagedLauncherOnPaseoHostGoesByItsKind(t *testing.T) {
	withShellLaunchers(t, "claude")
	n, _ := runtimeNode(t)
	if err := n.useEngine(context.Background(), proto.EnginePaseo); err != nil {
		t.Fatal(err)
	}
	host := dialRuntime(t, n, sameKindPaseoHello())
	host.answer = func(method string, _ json.RawMessage) (any, error) {
		if method == "session.start" {
			return map[string]any{"session": map[string]any{"session_id": "paseo-m", "cwd": "/w", "status": "idle"}}, nil
		}
		return map[string]any{"ok": true}, nil
	}
	waitFor(t, "Paseo's launchers", func() bool { return n.runtimes.launcherRuntime("krcc") != nil })
	n.setManaged([]proto.ManagedLauncher{{Name: "deepseek-gw", BaseURL: "https://gw.example", Model: "gw-model"},
		{Name: "codex-gw", Agent: "codex", BaseURL: "https://gw.example", Model: "gw-codex"}, {Name: "krcc", BaseURL: "https://gw.example", Model: "m"}})
	for _, c := range []struct{ launcher, agentType string }{{"deepseek-gw", "claude"}, {"codex-gw", "codex"}, {"krcc", ""}} {
		before := len(host.got("session.start"))
		if _, err := nodeCall(t, n, "session.start", map[string]any{"cwd": "/w", "launcher": c.launcher, "model": "gw-model", "effort": "high", "async": true}); err != nil {
			t.Fatal(err)
		}
		starts := host.got("session.start")
		if len(starts) != before+1 {
			t.Fatalf("%s did not go to Paseo", c.launcher)
		}
		var got map[string]any
		_ = json.Unmarshal(starts[len(starts)-1], &got)
		agentType, _ := got["agent_type"].(string)
		if got["launcher"] != c.launcher || agentType != c.agentType || got["shell_launcher"] != nil {
			t.Fatalf("%s went to Paseo as %v", c.launcher, got)
		}
	}
	for _, l := range launchersFor(t, n, proto.EnginePaseo).Launchers {
		if l.Source != proto.EnginePaseo {
			t.Fatalf("listed for Paseo: %+v", l)
		}
	}
	if m, ok := n.runtimeLauncherModelsIn(context.Background(), "krcc"); !ok || len(m.Models) != 1 || m.Models[0].ID != "opus-paseo" {
		t.Fatalf("krcc on Paseo is Paseo's provider: %+v %v", m, ok)
	}
}

// A computer with herdr and Paseo, herdr its default: a start in herdr keeps the shell's launchers (a name both have is
// the shell's there), one in Paseo runs Paseo's providers (the same name is Paseo's there), each list saying so.
func TestTerminalHostBesidePaseoKeepsTheShellsLaunchers(t *testing.T) {
	shellLaunchersOf(t, proto.Launcher{Name: "claude", Source: "zshrc"}, proto.Launcher{Name: "krcc", Source: "zshrc"})
	n, _ := runtimeNode(t)
	enablePaseo(n)
	dialRuntime(t, n, sameKindPaseoHello())
	waitFor(t, "Paseo's launchers", func() bool { return n.runtimes.launcherRuntime("claude-sub") != nil })
	herdr := launchersFor(t, n, proto.EngineHerdr)
	if herdr.HostRuntime || herdr.Host != proto.EngineHerdr || !slices.Contains(herdr.names(), "krcc/zshrc") || slices.Contains(herdr.names(), "krcc/paseo") {
		t.Fatalf("herdr's list: %v", herdr.names())
	}
	if paseo := launchersFor(t, n, proto.EnginePaseo); !paseo.HostRuntime || !slices.Contains(paseo.names(), "krcc/paseo") || slices.Contains(paseo.names(), "claude/zshrc") {
		t.Fatalf("Paseo's list: %v", paseo.names())
	}
	if h := n.runtimeOfIn(withStartHost(context.Background(), proto.EngineHerdr), "krcc"); h != nil {
		t.Fatal("a start in herdr runs the shell's krcc, not Paseo's")
	}
	if h := n.runtimeOfIn(context.Background(), "krcc"); h != nil {
		t.Fatal("the default host is herdr: the shell's krcc")
	}
	if h := n.runtimeOfIn(withStartHost(context.Background(), proto.EnginePaseo), "krcc"); h == nil || h.id() != proto.EnginePaseo {
		t.Fatal("a start in Paseo runs Paseo's krcc")
	}
}

// An engine runtime that does not say it runs only its own launchers keeps the old way: a launcher it lacks goes to it
// as shell_launcher (its default), and its host lists what it always did.
func TestEngineRuntimeWithoutSameKindKeepsShellLauncher(t *testing.T) {
	shellLaunchersOf(t, proto.Launcher{Name: "claude", Source: "zshrc"}, proto.Launcher{Name: "krcc", Source: "zshrc"})
	n, _ := runtimeNode(t)
	if err := n.useEngine(context.Background(), proto.EnginePaseo); err != nil {
		t.Fatal(err)
	}
	host := dialRuntime(t, n, paseoEngineHello()) // no same_kind
	host.answer = func(method string, _ json.RawMessage) (any, error) {
		if method == "session.start" {
			return map[string]any{"session": map[string]any{"session_id": "paseo-o", "cwd": "/w", "status": "idle"}}, nil
		}
		return map[string]any{"ok": true}, nil
	}
	waitFor(t, "its launchers", func() bool { return n.runtimes.launcherRuntime("paseo-claude") != nil })
	if ls := launchersFor(t, n, proto.EnginePaseo); ls.HostRuntime || !slices.Contains(ls.names(), "krcc/zshrc") {
		t.Fatalf("listed %v", ls.names())
	}
	if _, err := nodeCall(t, n, "session.start", map[string]any{"cwd": "/w", "launcher": "krcc", "agent_type": "claude", "async": true}); err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	_ = json.Unmarshal(host.got("session.start")[0], &got)
	if got["shell_launcher"] != true || got["agent_type"] != nil {
		t.Fatalf("started with %v", got)
	}
}
