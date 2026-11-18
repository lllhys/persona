package node

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/nwverse/persona/server/internal/nodeclient"
	"github.com/nwverse/persona/server/internal/proto"
	"github.com/nwverse/persona/server/internal/rpc"
)

const piTestKey = "sk-test-pi-provider-secret"

func TestPiModelMetadata(t *testing.T) {
	image, noImage := true, false
	for _, tc := range []struct {
		name, api, id string
		known         []proto.ProviderModel
		want          piModel
	}{
		{"unknown Claude", "anthropic-messages", "claude-x", nil,
			piModel{ID: "claude-x", Name: "claude-x", ContextWindow: 200000, MaxTokens: 32000, Reasoning: true, Input: []string{"text", "image"}}},
		{"unknown other", "openai-responses", "other", nil,
			piModel{ID: "other", Name: "other", ContextWindow: 128000, MaxTokens: 32000, Input: []string{"text"}}},
		{"reported capabilities", "openai-responses", "other", []proto.ProviderModel{{ID: "other", Name: "Other", ContextWindow: 64000, MaxOutputTokens: 24000, ImageInput: &image, AllEfforts: []string{"minimal"}}},
			piModel{ID: "other", Name: "Other", ContextWindow: 64000, MaxTokens: 24000, Reasoning: true, Input: []string{"text", "image"}}},
		{"explicit no image or thinking", "anthropic-messages", "claude-x", []proto.ProviderModel{{ID: "claude-x", ImageInput: &noImage, NoEffort: true, Efforts: []string{"high"}, AllEfforts: []string{"max"}}},
			piModel{ID: "claude-x", Name: "claude-x", ContextWindow: 200000, MaxTokens: 32000, Input: []string{"text"}}},
		{"output capped at window", "openai-responses", "other", []proto.ProviderModel{{ID: "other", ContextWindow: 10000, MaxOutputTokens: 20000}},
			piModel{ID: "other", Name: "other", ContextWindow: 10000, MaxTokens: 10000, Input: []string{"text"}}},
		{"unknown fields retain old estimates", "anthropic-messages", "other", []proto.ProviderModel{{ID: "other", ContextWindow: 40000, Efforts: []string{"low"}}},
			piModel{ID: "other", Name: "other", ContextWindow: 40000, MaxTokens: 10000, Reasoning: true, Input: []string{"text"}}},
		{"unmatched metadata", "openai-responses", "gpt-5-x", []proto.ProviderModel{{ID: "another", MaxOutputTokens: 3, ImageInput: &image, NoEffort: true}},
			piModel{ID: "gpt-5-x", Name: "gpt-5-x", ContextWindow: 128000, MaxTokens: 32000, Reasoning: true, Input: []string{"text"}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := piModelOf(tc.api, tc.id, tc.known); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("piModelOf() = %+v, want %+v", got, tc.want)
			}
		})
	}
}

// piTestHub answers launchers.pull with a key for every provider but "own" (the manager's own-service refusal).
func piTestHub(t *testing.T, n *Node, agentsDefault map[string]any) *pullHub {
	t.Helper()
	h := &pullHub{agentsDefault: agentsDefault, answer: func(name string) (any, error) {
		if name == "own" {
			return nil, &rpc.Error{Code: proto.CodeLauncherOwnService, Message: "own is not used on this computer"}
		}
		l := proto.ManagedLauncher{Name: name, BaseURL: "https://gw.example.com/v1", Model: "claude-x[1m]", APIKey: piTestKey}
		switch name {
		case "cx":
			l.Agent, l.BaseURL, l.Model = "codex", "https://oa.example.com/v1/", "gpt-5-x"
		case "bear":
			l.Auth, l.Headers = "bearer", "X-Team: blue\n"
		}
		return map[string]any{"launcher": l}, nil
	}}
	withPullHub(t, n, h)
	return h
}

// What pi gets: the providers in this computer's catalog that speak a protocol pi has, each with its key (pulled on
// demand), in pi's terms; Antigravity and a provider with a proxy of its own are not offered; a refused one is
// skipped with the manager's words.
func TestPiProvidersFromTheCatalog(t *testing.T) {
	n := &Node{}
	h := piTestHub(t, n, map[string]any{"launcher": "gw", "model": "claude-x"})
	var catalog []proto.ManagedLauncher
	for _, l := range []proto.ManagedLauncher{
		{Name: "gw", Model: "claude-x[1m]"}, {Name: "cx", Agent: "codex"}, {Name: "bear"}, {Name: "own", OwnService: proto.OwnServiceSkip},
		{Name: "gem", Agent: "agy"}, {Name: "prox", Proxy: "http://127.0.0.1:9"},
	} {
		l.Delivery, l.Rev = proto.DeliveryOnDemand, "r1"
		catalog = append(catalog, l.Public())
	}
	n.setCatalog(catalog)
	got := n.piProviders(context.Background())
	byLauncher := map[string]piProvider{}
	for _, p := range got.Providers {
		byLauncher[p.Launcher] = p
	}
	if len(byLauncher) != 3 || byLauncher["gem"].ID != "" || byLauncher["prox"].ID != "" {
		t.Fatalf("providers: %+v", got.Providers)
	}
	gw := byLauncher["gw"]
	if gw.ID != "persona-gw" || gw.API != "anthropic-messages" || gw.BaseURL != "https://gw.example.com" || gw.APIKey != piTestKey ||
		gw.AuthHeader || gw.Model != "claude-x" || len(gw.Models) != 1 || !gw.Models[0].Reasoning {
		t.Errorf("gw: %+v", gw)
	}
	if cx := byLauncher["cx"]; cx.API != "openai-responses" || cx.BaseURL != "https://oa.example.com/v1" || cx.Model != "gpt-5-x" {
		t.Errorf("cx: %+v", cx)
	}
	if b := byLauncher["bear"]; !b.AuthHeader || b.Headers["X-Team"] != "blue" {
		t.Errorf("bear: %+v", b)
	}
	if len(got.Skipped) != 1 || got.Skipped[0].Launcher != "own" || !strings.Contains(got.Skipped[0].Error, "not used on this computer") {
		t.Errorf("skipped: %+v", got.Skipped)
	}
	if got.Default == nil || got.Default.Provider != "persona-gw" || got.Default.Model != "claude-x" {
		t.Errorf("default: %+v", got.Default)
	}
	// Asked again (each pi session asks), the secrets come from memory: one pull per provider; only the refused one is
	// asked again, as personad launch would.
	pulls := h.count()
	n.piProviders(context.Background())
	if h.count() != pulls+1 {
		t.Errorf("pulled again: %d → %d", pulls, h.count())
	}
}

// A computer out of a provider's scope is never named it (the manager leaves it out of the catalog): pi gets nothing
// of it, and nothing is pulled.
func TestPiProvidersOutOfScopeGetNothing(t *testing.T) {
	n := &Node{}
	h := piTestHub(t, n, map[string]any{"launcher": "gw"})
	n.setCatalog(nil)
	got := n.piProviders(context.Background())
	if len(got.Providers) != 0 || got.Default != nil || len(got.Skipped) != 0 || h.count() != 0 {
		t.Fatalf("out of scope: %+v (pulled %d)", got, h.count())
	}
	b, _ := json.Marshal(got)
	if strings.Contains(string(b), piTestKey) {
		t.Fatal("a key reached pi")
	}
}

// Model Settings › Agents counts only when it names a provider pi has here, and a model that provider lists; an older
// manager (no launchers.agents_default) leaves pi's own choice.
func TestPiAgentsDefault(t *testing.T) {
	providers := []piProvider{{ID: "persona-gw", Launcher: "gw", Model: "m1", Models: []piModel{{ID: "m1"}, {ID: "m2"}}}}
	for _, c := range []struct {
		launcher, model string
		want            *piDefault
	}{
		{"gw", "", &piDefault{Provider: "persona-gw", Model: "m1"}},
		{"gw", "m2[1m]", &piDefault{Provider: "persona-gw", Model: "m2"}},
		{"gw", "elsewhere", nil},
		{"claude", "", nil},
	} {
		got := piDefaultOf(providers, c.launcher, c.model, "")
		if (got == nil) != (c.want == nil) || got != nil && *got != *c.want {
			t.Errorf("%s/%s: %+v, want %+v", c.launcher, c.model, got, c.want)
		}
	}
	n := &Node{}
	piTestHub(t, n, nil)
	n.setCatalog([]proto.ManagedLauncher{proto.ManagedLauncher{Name: "gw", Model: "claude-x", Delivery: proto.DeliveryOnDemand, Rev: "r1"}.Public()})
	if got := n.piProviders(context.Background()); len(got.Providers) != 1 || got.Default != nil {
		t.Fatalf("an older manager: %+v", got)
	}
}

// launcher.pi answers only a request with the launch key, the same refusal as launcher.env's otherwise.
func TestPiProvidersNeedTheLaunchKey(t *testing.T) {
	launchKeyHome(t)
	n := &Node{}
	piTestHub(t, n, nil)
	n.setCatalog([]proto.ManagedLauncher{proto.ManagedLauncher{Name: "gw", Model: "claude-x", Delivery: proto.DeliveryOnDemand, Rev: "r1"}.Public()})
	key, err := ensureLaunchKey(nodeclient.LaunchKeyPath())
	if err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"", strings.Repeat("0", launchKeyLen)} {
		resp, _ := n.piProvidersFor(context.Background(), sockRequest{Kind: "launcher.pi", LaunchKey: k}).(map[string]any)
		b, _ := json.Marshal(resp)
		if resp["error"] == nil || strings.Contains(string(b), piTestKey) || strings.Contains(string(b), "gw.example.com") {
			t.Errorf("without the key: %s", b)
		}
	}
	got, ok := n.piProvidersFor(context.Background(), sockRequest{Kind: "launcher.pi", LaunchKey: key}).(piProvidersAnswer)
	if !ok || len(got.Providers) != 1 || got.Providers[0].APIKey != piTestKey {
		t.Fatalf("with the key: %+v", got)
	}
}
