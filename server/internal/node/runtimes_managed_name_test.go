package node

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/proto"
)

// A Persona-managed launcher made from a Paseo provider keeps the provider's name; the runtime offering that name
// must not take its starts or its assistant conversations: personad launch runs it, as the pickers list it.
func TestManagedLauncherNamedLikeARuntimeProviderIsNotTheRuntimes(t *testing.T) {
	n, _ := runtimeNode(t)
	hello := dshHello()
	hello["caps"].(map[string]any)["assistant"] = true
	host := dialRuntime(t, n, hello)
	started := 0
	host.answer = func(method string, _ json.RawMessage) (any, error) {
		if method == "session.start" {
			started++
			return map[string]any{"session": map[string]any{"session_id": "session-new", "cwd": "/w", "status": "idle"}}, nil
		}
		return map[string]any{"ok": true}, nil
	}
	launcherCache.Lock()
	launcherCache.at, launcherCache.list = time.Now(), []proto.Launcher{{Name: "claude"}}
	launcherCache.Unlock()
	t.Cleanup(func() {
		launcherCache.Lock()
		launcherCache.at, launcherCache.list = time.Time{}, nil
		launcherCache.Unlock()
	})
	if n.runtimeOf("dsh") == nil {
		t.Fatal("the runtime's own launcher: the runtime runs it")
	}
	n.setManaged([]proto.ManagedLauncher{{Name: "dsh", BaseURL: "https://gw.example", Model: "m"}})
	if n.runtimeOf("dsh") != nil || n.assistantRuntime("dsh") != nil {
		t.Fatal("a managed launcher of the runtime's name went to the runtime")
	}
	if _, ok := n.runtimeLauncherModels("dsh"); ok {
		t.Fatal("the managed launcher's models came from the runtime")
	}
	_, _, _, ok := n.runtimeStart(context.Background(), runtimeStartParams{Cwd: "/w", Launcher: "dsh", Prompt: "hi"})
	if ok || started != 0 {
		t.Fatalf("the start went to the runtime (ok %v, starts %d)", ok, started)
	}
}

// Paseo's providers start agents only where Paseo is an enabled host: elsewhere its connected runtime offers none in
// launchers.list, and a start naming one does not go to it.
func TestEngineRuntimeProvidersNeedTheHostEnabled(t *testing.T) {
	n, _ := runtimeNode(t)
	hello := dshHello()
	hello["runtime"], hello["launcher"] = proto.EnginePaseo, "cc-relay"
	dialRuntime(t, n, hello)
	launcherCache.Lock()
	launcherCache.at, launcherCache.list = time.Now(), []proto.Launcher{{Name: "claude"}}
	launcherCache.Unlock()
	t.Cleanup(func() {
		launcherCache.Lock()
		launcherCache.at, launcherCache.list = time.Time{}, nil
		launcherCache.Unlock()
	})
	names := func() []string {
		raw, err := nodeCall(t, n, "launchers.list", map[string]any{})
		if err != nil {
			t.Fatal(err)
		}
		var ls struct {
			Launchers []proto.Launcher `json:"launchers"`
		}
		_ = json.Unmarshal(raw, &ls)
		var out []string
		for _, l := range ls.Launchers {
			out = append(out, l.Name)
		}
		return out
	}
	if got := names(); len(got) != 1 || got[0] != "claude" || n.runtimeOf("cc-relay") != nil {
		t.Fatalf("Paseo not enabled: listed %v", got)
	}
	enablePaseo(n)
	if got := names(); len(got) != 2 || got[1] != "cc-relay" || n.runtimeOf("cc-relay") == nil {
		t.Fatalf("Paseo enabled: listed %v", got)
	}
}

// enablePaseo makes Paseo one of n's hosts: its runtime's providers are offered and started only then.
func enablePaseo(n *Node) {
	n.eng.mu.Lock()
	n.eng.set = &proto.EngineChoice{Hosts: []string{proto.EngineHerdr, proto.EnginePaseo}, Default: proto.EngineHerdr}
	n.eng.mu.Unlock()
}

// With Paseo as the start's host, a managed launcher named like one of Paseo's providers starts on that provider:
// Paseo runs only its own providers, and shell_launcher would mean its default one, another model altogether.
func TestManagedLauncherOnDefaultPaseoUsesPaseosProviderOfThatName(t *testing.T) {
	withShellLaunchers(t, "claude")
	n, _ := runtimeNode(t)
	if err := n.useEngine(context.Background(), proto.EnginePaseo); err != nil {
		t.Fatal(err)
	}
	host := dialRuntime(t, n, paseoEngineHello())
	n.setManaged([]proto.ManagedLauncher{{Name: "paseo-claude", BaseURL: "https://gw.example", Model: "managed-model"}})
	host.answer = func(method string, _ json.RawMessage) (any, error) {
		if method == "session.start" {
			return map[string]any{"session": map[string]any{"session_id": "paseo-managed", "cwd": "/w", "status": "idle"}}, nil
		}
		return map[string]any{"ok": true}, nil
	}
	if _, err := nodeCall(t, n, "session.start", map[string]any{"cwd": "/w", "launcher": "paseo-claude", "async": true}); err != nil {
		t.Fatal(err)
	}
	starts := host.got("session.start")
	if len(starts) != 1 {
		t.Fatalf("Paseo starts %d", len(starts))
	}
	var start map[string]any
	_ = json.Unmarshal(starts[0], &start)
	// Paseo takes shell_launcher as "use your default provider": its provider of the same name is the closer match.
	if start["launcher"] != "paseo-claude" || start["shell_launcher"] != nil {
		t.Fatalf("started on Paseo's default provider instead of its own of that name: %v", start)
	}

	// The start still runs in Paseo, so ultracode is refused before another provider start rather than being checked as
	// a terminal Claude launch.
	_, err := nodeCall(t, n, "session.start", map[string]any{"cwd": "/w", "launcher": "paseo-claude", "ultracode": true, "async": true})
	if rpcCode(err) != ultracodeUnsupportedCode || !strings.Contains(err.Error(), "只有 Claude Code Agent") {
		t.Fatalf("ultracode did not follow the runtime start: %v", err)
	}
	if got := len(host.got("session.start")); got != 1 {
		t.Fatalf("ultracode reached session.start; starts %d", got)
	}
}

// A managed Antigravity launcher is started by the runtime's underlying agy launcher. Its model picker uses that same
// runtime, despite the managed name taking precedence for ordinary launchers.
func TestManagedAgyLauncherUsesUnderlyingRuntimeModels(t *testing.T) {
	withShellLaunchers(t, "claude")
	n, _ := runtimeNode(t)
	n.setManaged([]proto.ManagedLauncher{{Name: "agy", Agent: "agy", BaseURL: "https://gw.example", Model: "managed-model"}})
	hello := paseoEngineHello()
	hello["runtime"], hello["agent"], hello["launcher"] = "agy", "agy", "agy"
	hello["caps"].(map[string]any)["models"] = true
	hello["launchers"] = []map[string]any{{"name": "agy", "agent": "agy", "models": []map[string]any{{"id": "advertised-model"}}}}
	host := dialRuntime(t, n, hello)
	host.answer = func(method string, _ json.RawMessage) (any, error) {
		if method == "launchers.models" {
			return proto.LauncherModels{Models: []proto.AgentModel{{ID: "native-model", Name: "Native Model"}}}, nil
		}
		return map[string]any{"ok": true}, nil
	}
	raw, err := nodeCall(t, n, "launchers.models", map[string]any{"name": "agy", "refresh": true})
	if err != nil {
		t.Fatal(err)
	}
	var got proto.LauncherModels
	_ = json.Unmarshal(raw, &got)
	if got.Agent != "agy" || len(got.Models) != 1 || got.Models[0].ID != "native-model" {
		t.Fatalf("managed agy models %s", raw)
	}
	calls := host.got("launchers.models")
	if len(calls) != 1 {
		t.Fatalf("native model discovery calls %d", len(calls))
	}
	var call map[string]any
	_ = json.Unmarshal(calls[0], &call)
	if call["launcher"] != "agy" || call["refresh"] != true {
		t.Fatalf("native model discovery params %v", call)
	}

	// The start goes to that runtime with the managed model, so ultracode is judged as a runtime start too.
	host.answer = func(method string, _ json.RawMessage) (any, error) {
		if method == "session.start" {
			return map[string]any{"session": map[string]any{"session_id": "agy-managed", "cwd": "/w", "status": "idle"}}, nil
		}
		return map[string]any{"ok": true}, nil
	}
	cwd := t.TempDir()
	if _, err := nodeCall(t, n, "session.start", map[string]any{"cwd": cwd, "launcher": "agy", "async": true}); err != nil {
		t.Fatal(err)
	}
	var start map[string]any
	_ = json.Unmarshal(host.got("session.start")[0], &start)
	if start["model"] != "managed-model" {
		t.Fatalf("managed agy start %v", start)
	}
	_, err = nodeCall(t, n, "session.start", map[string]any{"cwd": cwd, "launcher": "agy", "ultracode": true, "async": true})
	if rpcCode(err) != ultracodeUnsupportedCode || len(host.got("session.start")) != 1 {
		t.Fatalf("ultracode on a managed agy start: %v, starts %d", err, len(host.got("session.start")))
	}
}

// A disabled engine runtime is absent before launcher ownership is decided: it cannot claim a name and then hide an
// ordinary runtime offering the same launcher. Listing, native models and starts all select the remaining owner.
func TestDisabledEngineRuntimeDoesNotClaimLauncherName(t *testing.T) {
	withShellLaunchers(t, "claude")
	n, _ := runtimeNode(t)
	cwd := t.TempDir()
	hello := func(runtime, agent, advertised string) map[string]any {
		return map[string]any{
			"runtime": runtime, "launcher": "shared", "caps": map[string]any{"start": true, "models": true},
			"launchers": []map[string]any{{"name": "shared", "agent": agent, "models": []map[string]any{{"id": advertised}}}},
		}
	}
	paseo := dialRuntime(t, n, hello(proto.EnginePaseo, "paseo", "paseo-advertised"))
	dsh := dialRuntime(t, n, hello("dsh", "dsh", "dsh-advertised"))
	answer := func(id, model string) func(string, json.RawMessage) (any, error) {
		return func(method string, _ json.RawMessage) (any, error) {
			switch method {
			case "session.start":
				return map[string]any{"session": map[string]any{"session_id": id, "cwd": "/w", "status": "idle"}}, nil
			case "launchers.models":
				return proto.LauncherModels{Models: []proto.AgentModel{{ID: model}}}, nil
			default:
				return map[string]any{"ok": true}, nil
			}
		}
	}
	paseo.answer = answer("paseo-session", "paseo-native")
	dsh.answer = answer("dsh-session", "dsh-native")

	t.Run("list", func(t *testing.T) {
		raw, err := nodeCall(t, n, "launchers.list", map[string]any{})
		if err != nil {
			t.Fatal(err)
		}
		var res struct {
			Launchers []proto.Launcher `json:"launchers"`
		}
		_ = json.Unmarshal(raw, &res)
		shared := 0
		for _, l := range res.Launchers {
			if l.Name == "shared" {
				shared++
				if l.Source != "dsh" {
					t.Fatalf("shared launcher source %q", l.Source)
				}
			}
		}
		if shared != 1 {
			t.Fatalf("shared launcher count %d in %s", shared, raw)
		}
	})
	t.Run("models", func(t *testing.T) {
		raw, err := nodeCall(t, n, "launchers.models", map[string]any{"name": "shared", "refresh": true})
		if err != nil {
			t.Fatal(err)
		}
		var got proto.LauncherModels
		_ = json.Unmarshal(raw, &got)
		if got.Agent != "dsh" || len(got.Models) != 1 || got.Models[0].ID != "dsh-native" {
			t.Fatalf("shared launcher models %s", raw)
		}
		if len(paseo.got("launchers.models")) != 0 || len(dsh.got("launchers.models")) != 1 {
			t.Fatalf("model discovery went to Paseo=%d DSH=%d", len(paseo.got("launchers.models")), len(dsh.got("launchers.models")))
		}
	})
	t.Run("start", func(t *testing.T) {
		raw, err := nodeCall(t, n, "session.start", map[string]any{"cwd": cwd, "launcher": "shared", "async": true})
		if err != nil {
			t.Fatal(err)
		}
		var res struct {
			Session proto.Session `json:"session"`
		}
		_ = json.Unmarshal(raw, &res)
		if res.Session.SessionID != "dsh-session" || len(paseo.got("session.start")) != 0 || len(dsh.got("session.start")) != 1 {
			t.Fatalf("started %s; Paseo=%d DSH=%d", raw, len(paseo.got("session.start")), len(dsh.got("session.start")))
		}
	})
	t.Run("ultracode follows start", func(t *testing.T) {
		before := len(dsh.got("session.start"))
		_, err := nodeCall(t, n, "session.start", map[string]any{"cwd": cwd, "launcher": "shared", "ultracode": true, "async": true})
		if rpcCode(err) != ultracodeUnsupportedCode || !strings.Contains(err.Error(), "只有 Claude Code Agent") {
			t.Fatalf("ultracode did not follow the DSH start: %v", err)
		}
		if len(dsh.got("session.start")) != before {
			t.Fatal("ultracode reached DSH session.start")
		}
	})
}
