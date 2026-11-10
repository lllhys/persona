package assistant

import (
	"context"
	"encoding/json"
	"net/http"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/nwverse/persona/server/internal/proto"
)

func assistantLauncherFixture(t *testing.T) *Service {
	t.Helper()
	s, f, _ := modelService(t)
	f.launcherList = []proto.Launcher{
		{Name: "claude", Source: "zshrc"},
		{Name: "fast", Source: "zshrc", Model: "fast-default"},
		{Name: "codex", Source: "path", Agent: "codex", Model: "codex-default"},
		{Name: "engine-claude", Source: "paseo", Model: "engine-default"},
		{Name: "engine-codex", Source: "paseo", Agent: "codex", Model: "engine-default"},
		{Name: "traex", Source: "path", Agent: "traex"},
	}
	f.hello.Models.Launchers = map[string][]ConfiguredLauncher{
		"server": {{Launcher: proto.Launcher{Name: "configured-only"}, AgentType: "claude"}},
	}
	putLaunchers(t, s,
		storedLauncher{ManagedLauncher: proto.ManagedLauncher{Name: "direct-messages", Kind: "api", API: "messages", BaseURL: "https://api.example.com", Model: "direct-default"}},
		storedLauncher{ManagedLauncher: proto.ManagedLauncher{Name: "direct-responses", Kind: "api", API: "responses", BaseURL: "https://api.example.com", Model: "responses-default"}},
	)
	return s
}

func TestAssistantLauncherCandidates(t *testing.T) {
	s := assistantLauncherFixture(t)
	p := purposeOf(t, s, "assistant")
	for _, name := range []string{"engine-claude", "engine-codex", "configured-only", "traex", "direct-responses"} {
		if slices.Contains(p.ProviderIDs, "launcher."+name) {
			t.Errorf("assistant purpose offers unsupported provider %q", name)
		}
	}
	read := func(purpose string) []string {
		t.Helper()
		r := request(s, http.MethodGet, "/api/v1/machines/server/launchers?purpose="+purpose, "")
		if r.Code != http.StatusOK {
			t.Fatalf("%s picker: %d %s", purpose, r.Code, r.Body.String())
		}
		var out struct {
			Launchers []ConfiguredLauncher `json:"launchers"`
		}
		if err := json.Unmarshal(r.Body.Bytes(), &out); err != nil {
			t.Fatal(err)
		}
		names := make([]string, 0, len(out.Launchers))
		for _, l := range out.Launchers {
			names = append(names, l.Name)
		}
		return names
	}
	got := read("assistant")
	for _, name := range got {
		if !slices.Contains(p.ProviderIDs, "launcher."+name) {
			t.Errorf("assistant picker offers %q without a compatible provider: %v", name, p.ProviderIDs)
		}
	}
	for _, name := range []string{"claude", "fast", "codex", "direct-messages"} {
		if !slices.Contains(got, name) {
			t.Errorf("supported launcher %q missing from %v", name, got)
		}
	}
	ordinary := read("agent.new")
	for _, name := range []string{"engine-claude", "engine-codex", "configured-only", "traex"} {
		if !slices.Contains(ordinary, name) {
			t.Errorf("ordinary Agent lost launcher %q: %v", name, ordinary)
		}
	}
	r := s.Tool(context.Background(), ToolCall{Name: "persona_settings", MachineID: "server", SessionID: "c1",
		Arguments: json.RawMessage(`{"action":"get","key":"assistant.launcher"}`)})
	if r.IsError {
		t.Fatalf("get launcher: %+v", r)
	}
	text := r.Content[0].Text
	for _, name := range []string{"engine-claude", "engine-codex", "configured-only", "direct-responses", "traex"} {
		if strings.Contains(text, "- "+name+" ") {
			t.Errorf("settings offers unsupported launcher %q: %s", name, text)
		}
	}
	for _, name := range got {
		if !strings.Contains(text, "- "+name+" ") {
			t.Errorf("settings and picker disagree about %q: %s", name, text)
		}
	}
}

func TestAssistantLauncherRejectsUnsupported(t *testing.T) {
	for _, name := range []string{"engine-claude", "engine-codex", "configured-only", "traex"} {
		t.Run(name, func(t *testing.T) {
			s := assistantLauncherFixture(t)
			setModel(t, s, "assistant", ModelBinding{Provider: "launcher.claude", Model: "old-model", Effort: "high"})
			before := modelRecordOf(t, s, "assistant")
			raw, _ := json.Marshal(name)
			// Check preparation first, so a regression never waits for a permission card.
			if change, err := s.setLauncher(context.Background(), raw); err == nil || change != nil {
				t.Fatalf("unsupported launcher prepared a change: change=%+v err=%v", change, err)
			}
			args, _ := json.Marshal(map[string]any{"action": "set", "key": "assistant.launcher", "value": name})
			r := s.Tool(context.Background(), ToolCall{Name: "persona_settings", MachineID: "server", SessionID: "c1", Arguments: args})
			if !r.IsError {
				t.Fatalf("unsupported launcher accepted by settings: %+v", r)
			}
			if qs := s.pendingQuestions(); len(qs) != 0 {
				t.Fatalf("unsupported launcher opened a permission card: %+v", qs)
			}
			if after := modelRecordOf(t, s, "assistant"); !reflect.DeepEqual(before, after) {
				t.Fatalf("rejected setting changed the model: before=%+v after=%+v", before, after)
			}
		})
	}
}

func TestAssistantLauncherSwitchUsesDefault(t *testing.T) {
	for _, tc := range []struct{ name, model string }{{"fast", "fast-default"}, {"direct-messages", "direct-default"}} {
		t.Run(tc.name, func(t *testing.T) {
			s := assistantLauncherFixture(t)
			setModel(t, s, "assistant", ModelBinding{Provider: "launcher.claude", Model: "old-model", Effort: "high", Runtime: proto.AssistantRuntimeAgentSDK})
			raw, _ := json.Marshal(tc.name)
			change, err := s.setLauncher(context.Background(), raw)
			if err != nil {
				t.Fatal(err)
			}
			if change.apply == nil {
				t.Fatal("launcher switch did not prepare a change")
			}
			if _, err := change.apply(context.Background()); err != nil {
				t.Fatal(err)
			}
			want := ModelBinding{Provider: "launcher." + tc.name, Runtime: proto.AssistantRuntimeAgentSDK}
			if got := modelRecordOf(t, s, "assistant").Binding; got != want {
				t.Errorf("launcher switch retained the old model or effort: got=%+v want=%+v", got, want)
			}
			p := purposeOf(t, s, "assistant")
			if p.Effective.Provider != "launcher."+tc.name || p.Effective.Model != tc.model || p.Effective.Effort != "" || p.Effective.Runtime != proto.AssistantRuntimeAgentSDK {
				t.Fatalf("purpose after launcher switch: %+v", p)
			}
			t.Logf("GET model-config/purposes/assistant: override=%+v effective=%+v", p.Override, p.Effective)
			params := map[string]any{}
			if err := s.applyModel(context.Background(), "assistant", params); err != nil {
				t.Fatal(err)
			}
			if params["launcher"] != tc.name || params["model"] != nil || params["effort"] != nil || params["runtime"] != proto.AssistantRuntimeAgentSDK {
				t.Fatalf("start parameters did not use the new launcher's defaults: %v", params)
			}
		})
	}
}

func TestAssistantLauncherUnchangedKeepsModel(t *testing.T) {
	s := assistantLauncherFixture(t)
	setModel(t, s, "assistant", ModelBinding{Provider: "launcher.fast", Model: "chosen", Effort: "high"})
	before := modelRecordOf(t, s, "assistant")
	change, err := s.setLauncher(context.Background(), json.RawMessage(`"fast"`))
	if err != nil || change == nil || change.apply != nil {
		t.Fatalf("unchanged launcher: change=%+v err=%v", change, err)
	}
	if after := modelRecordOf(t, s, "assistant"); after != before {
		t.Fatalf("unchanged launcher reset the model: before=%+v after=%+v", before, after)
	}
}

func TestAssistantLauncherPickerUsesTargetMachine(t *testing.T) {
	s, f, _ := modelService(t)
	f.sessions["other"] = nil
	f.zshrc = map[string][]proto.Launcher{
		"server": {{Name: "local-only", Source: "zshrc"}},
		"other":  {{Name: "remote-only", Source: "zshrc"}},
	}
	r := request(s, http.MethodGet, "/api/v1/machines/other/launchers?purpose=assistant", "")
	if r.Code != http.StatusOK {
		t.Fatalf("target machine picker: %d %s", r.Code, r.Body.String())
	}
	var out struct {
		Launchers []ConfiguredLauncher `json:"launchers"`
	}
	if err := json.Unmarshal(r.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, l := range out.Launchers {
		names = append(names, l.Name)
	}
	if !slices.Equal(names, []string{"remote-only"}) {
		t.Fatalf("picker used the current assistant's machine instead of the target: %v", names)
	}
}

func TestAssistantLauncherMessagesProviderScope(t *testing.T) {
	s, f, _ := modelService(t)
	f.sessions["other"] = nil
	f.launcherList = []proto.Launcher{{Name: "claude", Source: "zshrc"}}
	putLaunchers(t, s, storedLauncher{ManagedLauncher: proto.ManagedLauncher{
		Name: "restricted-messages", Kind: "api", API: "messages", BaseURL: "https://api.example.com", Model: "restricted-default",
	}, Share: shareSelected, ScopeMachines: []string{"other"}})
	if p := purposeOf(t, s, "assistant"); slices.Contains(p.ProviderIDs, "launcher.restricted-messages") {
		t.Fatal("assistant purpose accepted a provider outside its machine scope")
	}
	for _, machine := range []string{"server", "other"} {
		r := request(s, http.MethodGet, "/api/v1/machines/"+machine+"/launchers?purpose=assistant", "")
		if r.Code != http.StatusOK {
			t.Fatalf("%s picker: %d %s", machine, r.Code, r.Body.String())
		}
		var out struct {
			Launchers []ConfiguredLauncher `json:"launchers"`
		}
		if err := json.Unmarshal(r.Body.Bytes(), &out); err != nil {
			t.Fatal(err)
		}
		found := slices.ContainsFunc(out.Launchers, func(l ConfiguredLauncher) bool { return l.Name == "restricted-messages" })
		if found != (machine == "other") {
			t.Fatalf("%s picker ignored provider scope: %+v", machine, out.Launchers)
		}
	}
	if change, err := s.setLauncher(context.Background(), json.RawMessage(`"restricted-messages"`)); err == nil || change != nil {
		t.Fatalf("out-of-scope provider prepared a setting change: change=%+v err=%v", change, err)
	}
}
