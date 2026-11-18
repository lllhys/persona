package assistant

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/nwverse/persona/server/internal/proto"
)

// Every assistant conversation runs on Claude Code's CLI (#9799). The main chat's "runtime" an older hub stored or an
// older app sends ("cli", "agentsdk") is read and accepted as the CLI: it reaches neither the apps, the node, nor the
// next write.

// rawSetting is key's value as the store holds it.
func rawSetting(t *testing.T, s *Service, key string) string {
	t.Helper()
	var raw json.RawMessage
	if _, err := s.store().Setting(context.Background(), key, &raw); err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

// putRawSetting stores raw under key, as an older hub wrote it.
func putRawSetting(t *testing.T, s *Service, key, raw string) {
	t.Helper()
	if err := s.store().PutSetting(context.Background(), key, json.RawMessage(raw)); err != nil {
		t.Fatal(err)
	}
}

// GET offers no runtime: no purpose has runtimes, and none lists runtime among its fields.
func TestModelConfigOffersNoRuntime(t *testing.T) {
	s, _, _ := modelService(t)
	var list struct {
		Purposes []map[string]json.RawMessage `json:"purposes"`
	}
	var one struct {
		Purpose map[string]json.RawMessage `json:"purpose"`
	}
	if r := modelRequest(s, http.MethodGet, "", ""); r.Code != http.StatusOK || json.Unmarshal(r.Body.Bytes(), &list) != nil {
		t.Fatalf("catalog: %d %s", r.Code, r.Body.String())
	}
	if r := modelRequest(s, http.MethodGet, "assistant", ""); r.Code != http.StatusOK || json.Unmarshal(r.Body.Bytes(), &one) != nil {
		t.Fatalf("assistant: %d %s", r.Code, r.Body.String())
	}
	if len(list.Purposes) == 0 || one.Purpose == nil {
		t.Fatal("no purposes")
	}
	for _, p := range append(list.Purposes, one.Purpose) {
		var fields []string
		if err := json.Unmarshal(p["fields"], &fields); err != nil {
			t.Fatal(err)
		}
		if _, ok := p["runtimes"]; ok || slices.Contains(fields, "runtime") {
			t.Fatalf("%s offers a runtime: runtimes %s, fields %v", p["id"], p["runtimes"], fields)
		}
	}
}

// An older app's PATCH or PUT may carry the main chat's runtime. Any value, on any purpose, is accepted and ignored:
// the save succeeds, the rest of it applies, and no runtime is stored.
func TestModelPatchIgnoresARuntime(t *testing.T) {
	s, _, _ := modelService(t)
	for _, id := range []string{"assistant", purposeMeetingRoom, "agent.new", "kb"} {
		for _, method := range []string{http.MethodPatch, http.MethodPut} {
			for _, runtime := range []string{`"agentsdk"`, `"cli"`, `null`, `"something-else"`} {
				body := fmt.Sprintf(`{"revision":%d,"runtime":%s}`, modelRecordOf(t, s, id).Revision, runtime)
				if w := modelRequest(s, method, id, body); w.Code != http.StatusOK {
					t.Fatalf("%s %s %s: %d %s", method, id, body, w.Code, w.Body.String())
				}
				if raw := rawSetting(t, s, modelRecordKey(id)); strings.Contains(raw, "runtime") {
					t.Fatalf("%s %s %s stored %s", method, id, body, raw)
				}
			}
		}
	}
	body := fmt.Sprintf(`{"revision":%d,"runtime":"agentsdk","effort":"high"}`, modelRecordOf(t, s, "assistant").Revision)
	if w := modelRequest(s, http.MethodPatch, "assistant", body); w.Code != http.StatusOK {
		t.Fatalf("%s: %d %s", body, w.Code, w.Body.String())
	}
	if r := modelRecordOf(t, s, "assistant"); r.Binding != (ModelBinding{Effort: "high"}) {
		t.Fatalf("the rest of the patch: %+v", r)
	}
}

// A binding an older hub stored with the main chat's runtime reads as the CLI: no error, no runtime in what GET shows
// or what the node is asked to run, and none in the record after its next write. A runtime a caller left in a node
// call's params is dropped too (an older node would start "agentsdk" on the Agent SDK).
func TestStoredRuntimeReadsAsTheCLI(t *testing.T) {
	for _, runtime := range []string{"agentsdk", "cli"} {
		t.Run(runtime, func(t *testing.T) {
			s, _, _ := modelService(t)
			ctx := context.Background()
			putRawSetting(t, s, modelRecordKey("assistant"),
				`{"revision":3,"binding":{"provider":"launcher.claude","model":"test-model","effort":"high","runtime":"`+runtime+`"}}`)
			want := ModelBinding{Provider: "launcher.claude", Model: "test-model", Effort: "high"}
			if r := modelRecordOf(t, s, "assistant"); r.Revision != 3 || r.Binding != want {
				t.Fatalf("record: %+v", r)
			}
			if b, err := s.resolveModel(ctx, "assistant"); err != nil || b != want {
				t.Fatalf("resolved: %+v %v", b, err)
			}
			if p := purposeOf(t, s, "assistant"); p.Override != want || p.Effective != want || slices.Contains(p.Fields, "runtime") {
				t.Fatalf("purpose: %+v", p)
			}
			for name, apply := range map[string]func(map[string]any) error{
				"assistant":    func(p map[string]any) error { return s.applyModel(ctx, "assistant", p) },
				"schedule":     func(p map[string]any) error { return s.applyModel(ctx, "schedule", p) },
				"meeting room": func(p map[string]any) error { return s.applyModel(ctx, purposeMeetingRoom, p) },
				"other":        func(p map[string]any) error { p["launcher"] = "other"; return s.applyModel(ctx, "assistant", p) },
				"picked": func(p map[string]any) error {
					p["launcher"], p["model"] = "claude", "test-picked"
					return s.applyPickedModel(ctx, p)
				},
			} {
				params := map[string]any{"runtime": "agentsdk"}
				if err := apply(params); err != nil {
					t.Fatalf("%s: %v", name, err)
				}
				if _, ok := params["runtime"]; ok || params["launcher"] == nil || params["model_fingerprint"] == nil {
					t.Fatalf("%s: %v", name, params)
				}
			}
			if w := modelRequest(s, http.MethodPatch, "assistant", `{"revision":3,"effort":"low"}`); w.Code != http.StatusOK {
				t.Fatalf("next write: %d %s", w.Code, w.Body.String())
			}
			if raw := rawSetting(t, s, modelRecordKey("assistant")); strings.Contains(raw, "runtime") || !strings.Contains(raw, `"effort":"low"`) {
				t.Fatalf("after the next write: %s", raw)
			}
		})
	}
}

// The execution fingerprint of a binding without a runtime is the one a hub sent before the runtime went (#9799), so
// no conversation restarts for it. One stored with "agentsdk" reads as that same CLI binding, whose fingerprint
// differs from what an older hub sent for the Agent SDK: the node restarts that conversation on the CLI.
func TestModelFingerprintOfACLIBindingIsUnchanged(t *testing.T) {
	s, _, _ := modelService(t)
	ctx := context.Background()
	fingerprint := func() string {
		t.Helper()
		params := map[string]any{}
		if err := s.applyModel(ctx, "assistant", params); err != nil {
			t.Fatal(err)
		}
		fp, _ := params["model_fingerprint"].(string)
		return fp
	}
	// What a hub before #9799 sent for these two bindings (no launcher of that name stored).
	const (
		beforeCLI = "85a3f5f048305a1040f1f0d5d967fcf9f508be8430a298174a069abfaa57f593"
		beforeSDK = "5a64cedc6e07fe22ad8a3eab9e62af3e8673b59eb0229d2586facf7630c96fb1"
	)
	putRawSetting(t, s, modelRecordKey("assistant"), `{"revision":1,"binding":{"provider":"launcher.claude","model":"test-model","effort":"high"}}`)
	if got := fingerprint(); got != beforeCLI {
		t.Fatalf("the CLI binding's fingerprint changed: %s, before %s", got, beforeCLI)
	}
	putRawSetting(t, s, modelRecordKey("assistant"),
		`{"revision":2,"binding":{"provider":"launcher.claude","model":"test-model","effort":"high","runtime":"agentsdk"}}`)
	if got := fingerprint(); got != beforeCLI || got == beforeSDK {
		t.Fatalf("a stored agentsdk binding: %s; the CLI's %s, the Agent SDK's %s", got, beforeCLI, beforeSDK)
	}
}

// Model profiles an older hub stored with a runtime in a category read without it: the view, a switch and a rollback
// work and write no runtime. A PUT that still sends one with a category is accepted and stores none.
func TestModelProfilesIgnoreARuntime(t *testing.T) {
	s, _, _ := modelService(t)
	ctx := context.Background()
	putRawSetting(t, s, modelRecordKey("assistant"),
		`{"revision":2,"binding":{"provider":"launcher.claude","model":"test-chat","runtime":"agentsdk"}}`)
	putRawSetting(t, s, modelProfilesKey, `{"revision":1,"profiles":[{"id":"selected","name":"Selected",`+
		`"purposes":{"assistant":{"model":"test-other","runtime":"agentsdk"}},"updated_at":"2026-01-01T00:00:00Z"}]}`)
	v := profilesOf(t, s)
	if len(v.Profiles) != 1 || v.Profiles[0].Purposes["assistant"] != (ModelBinding{Model: "test-other"}) ||
		!slices.Equal(v.Profiles[0].Changes, []string{"assistant"}) {
		t.Fatalf("view: %+v", v)
	}
	if sw, err := s.applyModelProfile(ctx, "selected", ProfileActor{Kind: "hub"}); err != nil || !slices.Equal(sw.Changed, []string{"assistant"}) {
		t.Fatalf("switch: %+v %v", sw, err)
	}
	for _, key := range []string{modelRecordKey("assistant"), modelProfileStateKey} {
		if raw := rawSetting(t, s, key); strings.Contains(raw, "runtime") {
			t.Fatalf("%s after the switch: %s", key, raw)
		}
	}
	if r := modelRecordOf(t, s, "assistant"); r.Binding != (ModelBinding{Model: "test-other"}) {
		t.Fatalf("switched to %+v", r)
	}
	if _, err := s.rollbackModelProfile(ctx, ProfileActor{Kind: "hub"}); err != nil {
		t.Fatal(err)
	}
	if r := modelRecordOf(t, s, "assistant"); r.Binding != (ModelBinding{Provider: "launcher.claude", Model: "test-chat"}) {
		t.Fatalf("rolled back to %+v", r)
	}
	body := fmt.Sprintf(`{"revision":%d,"name":"Custom","purposes":{"assistant":{"model":"test-custom","runtime":"agentsdk"}}}`, profilesOf(t, s).Revision)
	if w := request(s, http.MethodPut, "/api/v1/model-profiles/custom", body); w.Code != http.StatusOK {
		t.Fatalf("PUT: %d %s", w.Code, w.Body.String())
	}
	if raw := rawSetting(t, s, modelProfilesKey); strings.Contains(raw, "runtime") || !strings.Contains(raw, "test-custom") {
		t.Fatalf("stored profiles: %s", raw)
	}
}

func TestModelMessagesAPIProviderRunsAsLauncher(t *testing.T) {
	s, _, _ := modelService(t)
	ctx := context.Background()
	stored := []storedLauncher{
		{ManagedLauncher: proto.ManagedLauncher{Kind: "api", Name: "gw", BaseURL: "https://gw.example.com", Model: "m1"}},
		{ManagedLauncher: proto.ManagedLauncher{Kind: "api", API: "messages", Name: "gw2", BaseURL: "https://gw2.example.com", Model: "m2"}},
		{ManagedLauncher: proto.ManagedLauncher{Kind: "api", API: "responses", Name: "resp", BaseURL: "https://r.example.com", Model: "gpt"}},
		{ManagedLauncher: proto.ManagedLauncher{Kind: "api", API: "chat", Name: "chat", BaseURL: "https://chat.example.com", Model: "m"}},
	}
	if err := s.putSetting(ctx, launchersManagedKey, stored); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"gw", "gw2"} {
		p := map[string]any{"launcher": name}
		if err := s.applyModel(ctx, "assistant", p); err != nil || p["launcher"] != name {
			t.Fatalf("%s: %v %+v", name, err, p)
		}
	}
	if err := s.applyModel(ctx, "assistant", map[string]any{"launcher": "resp"}); err == nil {
		t.Fatal("a Responses provider ran as a launcher")
	}
	if err := s.applyModel(ctx, "assistant", map[string]any{"launcher": "chat"}); err == nil {
		t.Fatal("a Chat Completions provider ran as a launcher")
	}
	nodes, err := s.nodeLaunchers(stored)
	if err != nil || len(nodes) != 2 {
		t.Fatalf("node providers: %+v, %v", nodes, err)
	}
	if c, err := s.apiConfig(ctx, ModelBinding{Provider: "launcher.chat"}); err != nil || c.API != "chat" || c.Configured() {
		t.Fatalf("chat config or keyless behavior: %+v, %v", c, err)
	}
	r := modelRequest(s, "GET", "assistant", "")
	var detail struct {
		Purpose ModelPurpose `json:"purpose"`
	}
	_ = json.Unmarshal(r.Body.Bytes(), &detail)
	ids := detail.Purpose.ProviderIDs
	if !slices.Contains(ids, "launcher.gw") || !slices.Contains(ids, "launcher.gw2") || slices.Contains(ids, "launcher.resp") || slices.Contains(ids, "launcher.chat") {
		t.Fatalf("assistant providers: %v", ids)
	}
}
