package assistant

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/i18n"
	"github.com/nwverse/persona/server/internal/proto"
)

// discovered is a model as discovery answers it, its pointers read.
type discovered struct {
	id, name           string
	window, output     int
	efforts, all       []string
	def                string
	none               bool
	images, tools      string // "true", "false" or "" for unknown
	inheritedFromEmpty bool
}

func discoveredOf(m proto.ProviderModel) discovered {
	b := func(p *bool) string {
		if p == nil {
			return ""
		}
		if *p {
			return "true"
		}
		return "false"
	}
	return discovered{id: m.ID, name: m.Name, window: m.ContextWindow, output: m.MaxOutputTokens, efforts: m.Efforts, all: m.AllEfforts,
		def: m.DefaultEffort, none: m.NoEffort, images: b(m.ImageInput), tools: b(m.ToolCalls), inheritedFromEmpty: m.InheritedFrom == ""}
}

func sameDiscovered(a, b discovered) bool {
	return a.id == b.id && a.name == b.name && a.window == b.window && a.output == b.output && slices.Equal(a.efforts, b.efforts) &&
		slices.Equal(a.all, b.all) && a.def == b.def && a.none == b.none && a.images == b.images && a.tools == b.tools &&
		a.inheritedFromEmpty == b.inheritedFromEmpty
}

// discoverFrom answers discovery from a gateway whose /v1/models pages are pages (keyed by the raw query, "" the first)
// and whose /v1/model-capabilities answers report (404 when empty). It returns the models, the error and what the
// capability report was asked with.
func discoverFrom(t *testing.T, in probeInput, pages map[string]string, report string) ([]proto.ProviderModel, any, string) {
	t.Helper()
	var asked string
	gw := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/models":
			body, ok := pages[r.URL.RawQuery]
			if !ok {
				t.Errorf("unexpected page %q", r.URL.RawQuery)
			}
			_, _ = w.Write([]byte(body))
		case "/v1/model-capabilities":
			asked = r.URL.RawQuery + "|" + r.Header.Get("Authorization")
			if report == "" {
				http.NotFound(w, r)
				return
			}
			_, _ = w.Write([]byte(report))
		default:
			http.NotFound(w, r)
		}
	}))
	defer gw.Close()
	s, _ := launcherTestService(t)
	in.BaseURL, in.APIKey = gw.URL, fakeKey
	out := s.providerDiscover(i18n.With(context.Background(), i18n.En), in)
	models, _ := out["models"].([]proto.ProviderModel)
	return models, out["error"], asked
}

// Every list shape a gateway answers in is read for what it reports, by its fields alone: the levels in the
// gateway's own words (projected for Claude Code into efforts), an explicit none, the output limit, image input and
// tool calls; entries marked as no chat model are left out. A list that reports none of it answers as before.
func TestDiscoveryReadsGatewayMetadataByShape(t *testing.T) {
	for _, tc := range []struct {
		name   string
		pages  map[string]string
		report string
		in     probeInput
		want   []discovered
		asked  string
	}{
		{
			// one page with everything per model, as magpie lists them; its image models only come with ?kind=
			name: "single shape",
			pages: map[string]string{"": `{"object":"list","data":[
				{"id":"example-thinker","object":"model","type":"model","display_name":"Example Thinker","reasoning":true,
				 "supported_reasoning_levels":[{"effort":"minimal"},{"effort":"low"},{"effort":"high"},{"effort":"max"}],
				 "reasoning_levels":["minimal","low","high","max"],"context_window":256000,"context_length":256000,
				 "max_input_tokens":256000,"max_output_tokens":32000,"modalities":{"input":["text","image"],"output":["text"]},
				 "tool_call":true,"supported_parameters":["tools","tool_choice","reasoning"]},
				{"id":"example-plain","object":"model","reasoning":false,"supported_reasoning_levels":[],"reasoning_levels":[],
				 "context_window":64000,"modalities":{"input":["text"],"output":["text"]},"tool_call":true,
				 "supported_parameters":["tools","tool_choice"]},
				{"id":"example-quiet","object":"model","reasoning":true,"reasoning_levels":["none","low","ultra"]},
				{"id":"gw/example-painter","object":"model","type":"model","kind":"image",
				 "modalities":{"input":["text"],"output":["image"]},"supported_endpoint_types":["image-generation"]},
				{"id":"gw/example-video","object":"model","kind":"video","modalities":{"input":["text","image"],"output":["video"]}}]}`},
			want: []discovered{
				{id: "example-thinker", name: "Example Thinker", window: 256000, output: 32000, efforts: []string{"low", "high", "max"},
					all: []string{"minimal", "low", "high", "max"}, images: "true", tools: "true", inheritedFromEmpty: true},
				{id: "example-plain", window: 64000, none: true, images: "false", tools: "true", inheritedFromEmpty: true},
				{id: "example-quiet", efforts: []string{"low", "max"}, all: []string{"none", "low", "ultra"}, inheritedFromEmpty: true},
			},
			asked: "protocol=messages|Bearer " + fakeKey,
		},
		{
			// Anthropic's envelope, 20 a page with has_more / last_id: max_tokens is not read (lists disagree on what it
			// is, and one can't tell a gateway's stand-in for a model it doesn't know); levels come from the report
			name: "anthropic envelope with a capability report",
			pages: map[string]string{
				"": `{"data":[{"type":"model","id":"example-a","display_name":"Example A","created_at":"2026-01-01T00:00:00Z",
				      "max_input_tokens":400000,"max_tokens":128000},
				     {"type":"model","id":"example-b","display_name":"Example B","max_input_tokens":200000,"max_tokens":64000}],
				     "first_id":"example-a","has_more":true,"last_id":"example-b"}`,
				"after_id=example-b": `{"data":[{"type":"model","id":"example-c","max_input_tokens":200000,"max_tokens":64000},
				     {"type":"model","id":"example-image","max_input_tokens":200000,"max_tokens":64000},
				     {"type":"model","id":"example-d","default_effort":"high"}],
				     "first_id":"example-c","has_more":false,"last_id":"example-d"}`,
			},
			report: `{"object":"list","schema_version":1,"data":[
				{"model_id":"example-a","protocols":["messages"],"reasoning":{"status":"supported","allowed":["low","high","xhigh"],"default":"high","source":"synthetic"}},
				{"model_id":"example-b","protocols":["messages"],"reasoning":{"status":"unsupported","allowed":[],"default":null,"source":"synthetic"}},
				{"model_id":"example-c","protocols":["messages"],"reasoning":{"status":"unknown","allowed":["low"],"default":null,"source":"synthetic"}},
				{"model_id":"example-image","protocols":[],"reasoning":{"status":"unsupported","allowed":[],"default":null,"source":"synthetic"}},
				{"model_id":"example-unlisted","protocols":["messages"],"reasoning":{"status":"supported","allowed":["low"],"default":"low"}},
				{"model_id":"example-d","protocols":["messages"],"reasoning":{"status":"supported","allowed":["low","medium"],"default":"medium"}},
				{"model_id":"example-a","protocols":[],"reasoning":{"status":"unsupported"}}]}`,
			want: []discovered{
				{id: "example-a", name: "Example A", window: 400000, efforts: []string{"low", "high", "xhigh"}, def: "high", inheritedFromEmpty: true},
				{id: "example-b", name: "Example B", window: 200000, none: true, inheritedFromEmpty: true},
				{id: "example-c", window: 200000, inheritedFromEmpty: true},
				// the report's levels bring its default: the list's is none of them
				{id: "example-d", efforts: []string{"low", "medium"}, def: "medium", inheritedFromEmpty: true},
			},
			asked: "protocol=messages|Bearer " + fakeKey,
		},
		{
			// a Codex model manifest; asked about Responses for a direct Responses connection
			name: "codex manifest",
			in:   probeInput{API: "responses"},
			pages: map[string]string{"": `{"models":[{"slug":"example-codex-1","display_name":"Example Codex","visibility":"list",
				"supported_reasoning_levels":[{"effort":"low","description":"Fast"},{"effort":"medium","description":"Balanced"},
				 {"effort":"high","description":"Deep"},{"effort":"ultra","description":"Deepest"}],
				"default_reasoning_level":"medium","context_window":272000,"input_modalities":["text","image"]}]}`},
			want: []discovered{{id: "example-codex-1", name: "Example Codex", window: 272000, efforts: []string{"low", "medium", "high", "max"},
				all: []string{"low", "medium", "high", "ultra"}, def: "medium", images: "true", inheritedFromEmpty: true}},
			asked: "protocol=responses|Bearer " + fakeKey,
		},
		{
			// ids alone, and objects that report nothing new: the answer is what it was before (the report is a 404)
			name:  "plain list",
			pages: map[string]string{"": `["example-x",{"id":"example-y","context_length":64000},{"id":"example-z","owned_by":"someone"}]`},
			want: []discovered{{id: "example-x", inheritedFromEmpty: true}, {id: "example-y", window: 64000, inheritedFromEmpty: true},
				{id: "example-z", inheritedFromEmpty: true}},
			asked: "protocol=messages|Bearer " + fakeKey,
		},
		{
			// other spellings: OpenRouter's architecture / top_provider / supported_parameters, LiteLLM's mode, a
			// models.dev style limit, new-api's endpoint types, and words no level list knows
			name: "other spellings",
			pages: map[string]string{"": `{"data":[
				{"id":"example-router","context_length":131072,"architecture":{"input_modalities":["text","image"],"output_modalities":["text"]},
				 "top_provider":{"max_completion_tokens":16384},"supported_parameters":["max_tokens","temperature"]},
				{"id":"example-limits","limit":{"output":8192},"supports_vision":false,"supports_function_calling":true,
				 "reasoning":{"supported_efforts":["low","high"],"default_effort":"low"}},
				{"id":"example-vendor-words","reasoning_levels":["light","extra_high"],"efforts":["medium","high"]},
				{"id":"example-embed","mode":"embedding"},
				{"id":"example-rerank","supported_endpoint_types":["jina-rerank"]},
				{"id":"example-chat-endpoints","supported_endpoint_types":["openai","image-generation"]},
				{"id":"example-says-thinks","reasoning":true,"supported_parameters":["tools"]},
				{"id":"example-mixed","supported_reasoning_levels":["low","high"],"reasoning":{"status":"unsupported"}}]}`},
			want: []discovered{
				{id: "example-router", window: 131072, output: 16384, none: true, images: "true", tools: "false", inheritedFromEmpty: true},
				{id: "example-limits", output: 8192, efforts: []string{"low", "high"}, def: "low", images: "false", tools: "true", inheritedFromEmpty: true},
				{id: "example-vendor-words", efforts: []string{"medium", "high"}, inheritedFromEmpty: true},
				{id: "example-chat-endpoints", inheritedFromEmpty: true},
				// said to reason: supported_parameters without a reasoning entry says nothing of its levels
				{id: "example-says-thinks", tools: "true", inheritedFromEmpty: true},
				// a list wins over a status of unsupported
				{id: "example-mixed", efforts: []string{"low", "high"}, inheritedFromEmpty: true},
			},
			asked: "protocol=messages|Bearer " + fakeKey,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			models, failure, asked := discoverFrom(t, tc.in, tc.pages, tc.report)
			if failure != nil || len(models) != len(tc.want) {
				t.Fatalf("error %v, models %+v", failure, models)
			}
			for i, m := range models {
				if got := discoveredOf(m); !sameDiscovered(got, tc.want[i]) {
					t.Errorf("model %d:\n got  %+v\n want %+v", i, got, tc.want[i])
				}
			}
			if asked != tc.asked {
				t.Errorf("capability report asked as %q, want %q", asked, tc.asked)
			}
		})
	}
}

// A list that reports nothing new answers byte for byte as before: no new field appears.
func TestDiscoveryOfAPlainListIsUnchanged(t *testing.T) {
	models, failure, _ := discoverFrom(t, probeInput{}, map[string]string{"": `{"data":[{"id":"own-model","context_length":64000},"other-model"]}`}, "")
	raw, _ := json.Marshal(models)
	if failure != nil || string(raw) != `[{"id":"own-model","context_window":64000},{"id":"other-model"}]` {
		t.Fatalf("%v %s", failure, raw)
	}
}

// The capability report only adds: a 404, an error status, a body it can't read, one past its timeout and a gateway
// that never answers it leave the list as it was, and discovery does not wait for the report past its own bound.
func TestDiscoveryCapabilityReportFailuresAreIgnored(t *testing.T) {
	old := capabilitiesTimeout
	capabilitiesTimeout = 200 * time.Millisecond
	t.Cleanup(func() { capabilitiesTimeout = old })
	for name, answer := range map[string]func(http.ResponseWriter, *http.Request){
		"404":        http.NotFound,
		"500":        func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(500) },
		"401":        func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(401) },
		"not json":   func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("<html>")) },
		"other json": func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(`{"data":"nothing"}`)) },
		"hangs": func(w http.ResponseWriter, r *http.Request) {
			select {
			case <-r.Context().Done():
			case <-time.After(5 * time.Second):
			}
		},
	} {
		t.Run(name, func(t *testing.T) {
			gw := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/v1/model-capabilities" {
					answer(w, r)
					return
				}
				_, _ = w.Write([]byte(`{"data":[{"id":"example-a","context_window":64000,"reasoning_levels":["low"]},{"id":"example-b"}]}`))
			}))
			defer gw.Close()
			s, _ := launcherTestService(t)
			started := time.Now()
			out := s.providerDiscover(i18n.With(context.Background(), i18n.En), probeInput{BaseURL: gw.URL, APIKey: fakeKey})
			took := time.Since(started)
			models, _ := out["models"].([]proto.ProviderModel)
			if out["error"] != nil || out["code"] != nil || len(models) != 2 || models[0].ContextWindow != 64000 ||
				!slices.Equal(models[0].Efforts, []string{"low"}) || len(models[1].Efforts) != 0 || models[1].NoEffort {
				t.Fatalf("%v", out)
			}
			if took > 2*time.Second {
				t.Fatalf("discovery waited %v for the report", took)
			}
		})
	}
}

// The levels and limits a gateway reports win over the catalog's, exact or inherited, and an explicit none keeps the
// catalog's levels out; an exact catalog entry's window still wins over a reported one.
func TestDiscoveryPrefersReportedLevelsOverTheCatalog(t *testing.T) {
	exact, ok := catalogModel("glm-5.3")
	if !ok || len(exact.Efforts) == 0 || exact.DefaultEffort == "" || exact.InheritedFrom != "" {
		t.Fatalf("catalog: %+v", exact)
	}
	newer, ok := catalogModel("glm-5.9")
	if !ok || newer.InheritedFrom == "" {
		t.Fatalf("catalog generation: %+v", newer)
	}
	models, failure, _ := discoverFrom(t, probeInput{}, map[string]string{"": `{"data":[
		{"id":"glm-5.3","context_window":12345,"reasoning_levels":["low","medium"]},
		{"id":"vendor/glm-5.3","reasoning":false},
		{"id":"glm-5.9","context_window":96000,"reasoning_levels":["high","max"],"default_reasoning_level":"max","max_output_tokens":8000},
		{"id":"GLM-5.3-20260101"}]}`}, "")
	if failure != nil || len(models) != 4 {
		t.Fatalf("%v %+v", failure, models)
	}
	// the catalog's default is not one of the reported levels, so none is given
	if m := models[0]; m.ContextWindow != exact.ContextWindow || !slices.Equal(m.Efforts, []string{"low", "medium"}) || m.DefaultEffort != "" {
		t.Errorf("exact entry: %+v", m)
	}
	if m := models[1]; !m.NoEffort || len(m.Efforts) != 0 || m.DefaultEffort != "" || m.Name == "" {
		t.Errorf("explicit none: %+v", m)
	}
	if m := models[2]; m.ContextWindow != 96000 || !slices.Equal(m.Efforts, []string{"high", "max"}) || m.DefaultEffort != "max" ||
		m.MaxOutputTokens != 8000 || m.InheritedFrom != "" {
		t.Errorf("inherited generation: %+v", m)
	}
	if m := models[3]; !slices.Equal(m.Efforts, exact.Efforts) || m.DefaultEffort != exact.DefaultEffort || m.NoEffort {
		t.Errorf("nothing reported: %+v", m)
	}
}

// The DeepSeek list outside the base has no capability report next to it, and an empty list asks for none.
func TestDiscoveryCapabilityReportNotAskedWithoutAList(t *testing.T) {
	var paths []string
	gw := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		_, _ = w.Write([]byte(`{"data":[]}`))
	}))
	defer gw.Close()
	s, _ := launcherTestService(t)
	if out := s.providerDiscover(context.Background(), probeInput{BaseURL: gw.URL, APIKey: fakeKey}); out["error"] != nil ||
		!slices.Equal(paths, []string{"/v1/models"}) {
		t.Fatalf("%v %v", out, paths)
	}
	var urls []string
	interceptProbe(t, func(r *http.Request) (*http.Response, error) {
		urls = append(urls, r.URL.String())
		return probeResponse(r, 200, `{"data":[{"id":"deepseek-v4-pro"}]}`), nil
	})
	if out := s.providerDiscover(context.Background(), probeInput{BaseURL: "https://api.deepseek.com/anthropic", APIKey: fakeKey}); out["error"] != nil ||
		!slices.Equal(urls, []string{"https://api.deepseek.com/models"}) {
		t.Fatalf("%v %v", out, urls)
	}
}

// A stored Codex launcher's report is asked about Responses, whatever its stored api.
func TestCapabilityProtocol(t *testing.T) {
	for want, target := range map[string]probeTarget{
		"messages":         {},
		"responses":        {api: "responses"},
		"chat_completions": {api: "chat", kind: "api"},
	} {
		if got := target.capabilityProtocol(); got != want {
			t.Errorf("%+v: %s, want %s", target, got, want)
		}
	}
	if got := (probeTarget{agent: "codex"}).capabilityProtocol(); got != "responses" {
		t.Errorf("codex: %s", got)
	}
}

// Saving keeps the levels in the words the provider's agent or API takes: Codex's minimal and ultra, a direct API's
// none, Claude Code's low…max alone (efforts says all of it then). Levels changed by hand, or a client that knows
// nothing of all_efforts, leave efforts the truth; levels chosen undo an explicit none.
func TestSavedEffortsFollowTheProvidersVocabulary(t *testing.T) {
	m := func(efforts, all []string, none bool, def string) proto.ProviderModel {
		return proto.ProviderModel{ID: "m", Efforts: efforts, AllEfforts: all, NoEffort: none, DefaultEffort: def}
	}
	claude, codex, direct := providerEffortVocab("launcher", "claude"), providerEffortVocab("", "codex"), providerEffortVocab("api", "")
	for _, tc := range []struct {
		name  string
		in    proto.ProviderModel
		vocab []string
		want  proto.ProviderModel
	}{
		{"claude drops minimal", m([]string{"low", "high"}, []string{"minimal", "low", "high"}, false, "high"), claude,
			m([]string{"low", "high"}, nil, false, "high")},
		{"codex keeps minimal and ultra", m([]string{"low", "high", "max"}, []string{"minimal", "high", "ultra"}, false, ""), codex,
			m([]string{"low", "high", "max"}, []string{"minimal", "high", "ultra"}, false, "")},
		{"codex drops none", m([]string{"low"}, []string{"none", "low"}, false, ""), codex, m([]string{"low"}, nil, false, "")},
		{"direct keeps none", m([]string{"low", "high"}, []string{"none", "low", "high"}, false, ""), direct,
			m([]string{"low", "high"}, []string{"none", "low", "high"}, false, "")},
		{"direct maps minimal and ultra", m([]string{"low", "max"}, []string{"minimal", "ultra"}, false, ""), direct,
			m([]string{"low", "max"}, nil, false, "")},
		{"levels changed by hand", m([]string{"high"}, []string{"minimal", "high", "ultra"}, false, ""), codex,
			m([]string{"high"}, nil, false, "")},
		{"words outside every list", m([]string{"low"}, []string{"light", "low", "extreme"}, false, ""), codex,
			m([]string{"low"}, nil, false, "")},
		{"chosen levels undo none", m([]string{"low"}, nil, true, ""), claude, m([]string{"low"}, nil, false, "")},
		{"a chosen default undoes none", m(nil, []string{"minimal"}, true, "low"), codex, m(nil, nil, false, "low")},
		{"none keeps no levels", m(nil, []string{"minimal"}, true, ""), codex, m(nil, nil, true, "")},
	} {
		got := settleSavedEfforts(tc.in, tc.vocab)
		if !slices.Equal(got.Efforts, tc.want.Efforts) || !slices.Equal(got.AllEfforts, tc.want.AllEfforts) ||
			got.NoEffort != tc.want.NoEffort || got.DefaultEffort != tc.want.DefaultEffort {
			t.Errorf("%s: %+v, want %+v", tc.name, got, tc.want)
		}
	}
}

// Through the API: a provider saved with what discovery answered keeps the new fields in its agent's words, GET
// gives them back, an app that sends no models keeps them, and a negative output limit is refused.
func TestProviderSaveKeepsDiscoveredMetadata(t *testing.T) {
	s, mux := launcherTestService(t)
	ctx := context.Background()
	models := `[{"id":"example-codex-1","efforts":["low","high","max"],"all_efforts":["minimal","low","high","ultra"],"default_effort":"high",
		"max_output_tokens":64000,"image_input":true,"tool_calls":false},{"id":"example-plain","no_effort":true}]`
	for name, agent := range map[string]string{"cx": "codex", "cc": "claude"} {
		if code, raw := launcherCall(t, mux, "POST", "/api/v1/launchers", `{"name":"`+name+`","agent":"`+agent+`","base_url":"https://gw.example.com/v1",
			"model":"example-codex-1","api_key":"`+fakeKey+`","models":`+models+`}`); code != 200 {
			t.Fatal(name, code, raw)
		}
	}
	byName := map[string]storedLauncher{}
	for _, l := range s.storedLaunchers(ctx) {
		byName[l.Name] = l
	}
	cx, cc := byName["cx"].Models, byName["cc"].Models
	if len(cx) != 2 || !slices.Equal(cx[0].AllEfforts, []string{"minimal", "low", "high", "ultra"}) || cx[0].MaxOutputTokens != 64000 ||
		cx[0].ImageInput == nil || !*cx[0].ImageInput || cx[0].ToolCalls == nil || *cx[0].ToolCalls || !cx[1].NoEffort {
		t.Fatalf("codex: %+v", cx)
	}
	if len(cc) != 2 || cc[0].AllEfforts != nil || !slices.Equal(cc[0].Efforts, []string{"low", "high", "max"}) || cc[0].MaxOutputTokens != 64000 || !cc[1].NoEffort {
		t.Fatalf("claude: %+v", cc)
	}
	_, list := launcherCall(t, mux, "GET", "/api/v1/launchers", "")
	if !strings.Contains(list, `"all_efforts":["minimal","low","high","ultra"]`) || !strings.Contains(list, `"no_effort":true`) ||
		!strings.Contains(list, `"max_output_tokens":64000`) || !strings.Contains(list, `"image_input":true`) || !strings.Contains(list, `"tool_calls":false`) {
		t.Fatalf("GET: %s", list)
	}
	cxModels := func() []proto.ProviderModel {
		for _, l := range s.storedLaunchers(ctx) {
			if l.Name == "cx" {
				return l.Models
			}
		}
		return nil
	}
	if code, raw := launcherCall(t, mux, "PUT", "/api/v1/launchers/cx", `{"base_url":"https://gw.example.com/v1","model":"example-codex-1"}`); code != 200 {
		t.Fatal(raw)
	}
	if m := cxModels(); len(m) != 2 || len(m[0].AllEfforts) != 4 || !m[1].NoEffort {
		t.Fatalf("kept without models: %+v", m)
	}
	// an app that knows only the old fields resends the models: what the gateway reported stays, as far as it still
	// agrees with the levels sent (levels chosen for the second model undo its none)
	if code, raw := launcherCall(t, mux, "PUT", "/api/v1/launchers/cx", `{"base_url":"https://gw.example.com/v1","model":"example-codex-1",
		"models":[{"id":"example-codex-1","efforts":["low","high","max"],"default_effort":"high"},{"id":"example-plain","efforts":["high"]}]}`); code != 200 {
		t.Fatal(raw)
	}
	if m := cxModels(); len(m) != 2 || len(m[0].AllEfforts) != 4 || m[0].MaxOutputTokens != 64000 || m[0].ImageInput == nil ||
		m[1].NoEffort || !slices.Equal(m[1].Efforts, []string{"high"}) {
		t.Fatalf("kept with old models: %+v", m)
	}
	// levels changed by hand leave all_efforts behind
	if code, raw := launcherCall(t, mux, "PUT", "/api/v1/launchers/cx", `{"base_url":"https://gw.example.com/v1","model":"example-codex-1",
		"models":[{"id":"example-codex-1","efforts":["high"]}]}`); code != 200 {
		t.Fatal(raw)
	}
	if m := cxModels(); len(m) != 1 || m[0].AllEfforts != nil || m[0].MaxOutputTokens != 64000 {
		t.Fatalf("levels by hand: %+v", m)
	}
	if code, raw := launcherCall(t, mux, "POST", "/api/v1/launchers", `{"name":"neg","base_url":"https://gw.example.com","model":"m",
		"api_key":"`+fakeKey+`","models":[{"id":"m","max_output_tokens":-1}]}`); code != 400 || !strings.Contains(raw, "输出上限") {
		t.Fatalf("negative output limit: %d %s", code, raw)
	}
}

// A cindy model's output limit, image input, tool calls and reasoning false come in as discovery reads a gateway's;
// its levels as before (the ones Claude Code takes, the default only among them).
func TestCindyModelMetadata(t *testing.T) {
	var m cindyModel
	if err := json.Unmarshal([]byte(`{"id":"example","maxOutputTokens":32000,"supportsImageInput":true,"supportsToolCalls":false,
		"reasoning":true,"reasoningEfforts":["minimal","low","high"],"reasoningDefaultEffort":"high"}`), &m); err != nil {
		t.Fatal(err)
	}
	pm := cindyProviderModel(m)
	if pm.MaxOutputTokens != 32000 || pm.ImageInput == nil || !*pm.ImageInput || pm.ToolCalls == nil || *pm.ToolCalls || pm.NoEffort ||
		!slices.Equal(pm.Efforts, []string{"low", "high"}) || pm.AllEfforts != nil || pm.DefaultEffort != "high" {
		t.Fatalf("%+v", pm)
	}
	if pm := cindyProviderModel(cindyModel{ID: "plain", Reasoning: ptr(false)}); !pm.NoEffort || pm.Efforts != nil {
		t.Fatalf("reasoning false: %+v", pm)
	}
	if pm := cindyProviderModel(cindyModel{ID: "unsaid", ReasoningEfforts: []string{"high"}}); pm.NoEffort || pm.MaxOutputTokens != 0 ||
		pm.ImageInput != nil || pm.ToolCalls != nil {
		t.Fatalf("nothing said: %+v", pm)
	}
}

// What says nothing is not read as a statement (#8796): null, an empty parameter or input list, a list with an entry
// of a shape it can't read, a partial parameter list; a none guessed from a whole parameter list gives way to the
// catalog's levels. Only an empty level list and the report's protocols: [] say "none".
func TestDiscoveryReadsOnlyWhatIsSaid(t *testing.T) {
	exact, ok := catalogModel("glm-5.3")
	if !ok || len(exact.Efforts) == 0 {
		t.Fatalf("catalog: %+v", exact)
	}
	models, failure, _ := discoverFrom(t, probeInput{}, map[string]string{"": `{"data":[
		{"id":"example-nulls","reasoning":null,"supports_reasoning":null,"supports_vision":null,"tool_call":null,
		 "supported_parameters":null,"reasoning_levels":null,"modalities":{"input":null,"output":null}},
		{"id":"example-empty-lists","supported_parameters":[],"modalities":{"input":[],"output":["text"]}},
		{"id":"example-odd-levels","reasoning_levels":[{"level":"low"}],"supported_reasoning_levels":[{"effort":"high"},{"rank":2}]},
		{"id":"example-partial-params","supported_parameters":["tools","tool_choice"]},
		{"id":"example-whole-params","supported_parameters":["tools","temperature","max_tokens"]},
		{"id":"glm-5.3","supported_parameters":["temperature","max_tokens"]},
		{"id":"example-said-none","reasoning_levels":[]},
		{"id":"example-odd-protocols"},
		{"id":"example-guess-then-report","supported_parameters":["temperature","max_tokens"]},
		{"id":"example-guess-then-entry","supported_parameters":["temperature"]},
		{"id":"example-guess-then-entry","reasoning_levels":["high"]},
		{"id":"example-empty-then-other-input","modalities":{"input":[]},"architecture":{"input_modalities":["text","image"]}},
		{"id":"example-blanks","supported_parameters":["temperature","tools",""],"reasoning_levels":["",""]}]}`},
		`{"data":[{"model_id":"example-odd-protocols","protocols":[{"protocol":"messages"}],"reasoning":{"status":"supported","allowed":["low"]}},
		  {"model_id":"example-guess-then-report","protocols":["messages"],"reasoning":{"status":"supported","allowed":["low","high"]}}]}`)
	if failure != nil || len(models) != 12 { // the repeated entry is one model
		t.Fatalf("%v %+v", failure, models)
	}
	want := []discovered{
		{id: "example-nulls", inheritedFromEmpty: true},
		{id: "example-empty-lists", inheritedFromEmpty: true},
		{id: "example-odd-levels", inheritedFromEmpty: true},
		{id: "example-partial-params", tools: "true", inheritedFromEmpty: true},
		{id: "example-whole-params", none: true, tools: "true", inheritedFromEmpty: true},
		{id: "glm-5.3", name: exact.Name, window: exact.ContextWindow, efforts: exact.Efforts, def: exact.DefaultEffort, tools: "false", inheritedFromEmpty: true},
		{id: "example-said-none", none: true, inheritedFromEmpty: true},
		{id: "example-odd-protocols", efforts: []string{"low"}, inheritedFromEmpty: true},
		// a guessed none loses to levels the report or a repeated entry gives
		{id: "example-guess-then-report", efforts: []string{"low", "high"}, tools: "false", inheritedFromEmpty: true},
		{id: "example-guess-then-entry", efforts: []string{"high"}, tools: "false", inheritedFromEmpty: true},
		{id: "example-empty-then-other-input", images: "true", inheritedFromEmpty: true},
		// blank entries are skipped; a list of only blanks says nothing
		{id: "example-blanks", none: true, tools: "true", inheritedFromEmpty: true},
	}
	for i, m := range models {
		if got := discoveredOf(m); !sameDiscovered(got, want[i]) {
			t.Errorf("model %d:\n got  %+v\n want %+v", i, got, want[i])
		}
	}
}

// A Codex launcher not saved yet is asked about Responses: the web names its agent, while its api is still messages.
func TestDiscoveryAsksAboutTheNamedAgentsProtocol(t *testing.T) {
	_, _, asked := discoverFrom(t, probeInput{API: "messages", Agent: ptr("codex")}, map[string]string{"": `{"data":[{"id":"example-codex-1"}]}`}, "")
	if asked != "protocol=responses|Bearer "+fakeKey {
		t.Fatalf("asked as %q", asked)
	}
	// a stored Codex launcher the form turns into a Claude Code one is asked about Messages
	s, _ := launcherTestService(t)
	putLaunchers(t, s, storedLauncher{ManagedLauncher: proto.ManagedLauncher{Name: "cx", Agent: "codex", BaseURL: "https://gw.example.com/v1", Model: "m"}})
	for want, agent := range map[string]*string{"responses": nil, "messages": ptr("claude")} {
		tg, err := s.probeTarget(context.Background(), probeInput{Name: "cx", Agent: agent})
		if err != nil || tg.capabilityProtocol() != want {
			t.Errorf("agent %v: %s, %v; want %s", agent, tg.capabilityProtocol(), err, want)
		}
	}
}

// A default level chosen for a model that takes none undoes the none, as levels chosen do; the web, which names the
// reported fields for every model, clears what it leaves out, while an app that names none of them keeps them.
func TestSavingClearsWhatTheWebLeavesOut(t *testing.T) {
	if got := settleSavedEfforts(proto.ProviderModel{ID: "m", NoEffort: true, DefaultEffort: "high"}, proto.EffortLevels); got.NoEffort ||
		got.DefaultEffort != "high" {
		t.Fatalf("chosen default: %+v", got)
	}
	s, mux := launcherTestService(t)
	ctx := context.Background()
	put := func(models string) proto.ProviderModel {
		t.Helper()
		if code, raw := launcherCall(t, mux, "PUT", "/api/v1/launchers/gw", `{"base_url":"https://gw.example.com","model":"m","models":`+models+`}`); code != 200 {
			t.Fatal(code, raw)
		}
		return s.storedLaunchers(ctx)[0].Models[0]
	}
	if code, raw := launcherCall(t, mux, "POST", "/api/v1/launchers", `{"name":"gw","base_url":"https://gw.example.com","model":"m","api_key":"`+fakeKey+`",
		"models":[{"id":"m","no_effort":true,"max_output_tokens":8000,"image_input":false,"tool_calls":true}]}`); code != 200 {
		t.Fatal(code, raw)
	}
	if m := put(`[{"id":"m","context_window":64000}]`); !m.NoEffort || m.MaxOutputTokens != 8000 || m.ImageInput == nil || m.ToolCalls == nil {
		t.Fatalf("an older app's models keep the stored report: %+v", m)
	}
	if m := put(`[{"id":"m","default_effort":"high"}]`); m.NoEffort || m.DefaultEffort != "high" || m.MaxOutputTokens != 8000 {
		t.Fatalf("a chosen default undoes none: %+v", m)
	}
	if m := put(`[{"id":"m","no_effort":false}]`); m.NoEffort || m.MaxOutputTokens != 0 || m.ImageInput != nil || m.ToolCalls != nil {
		t.Fatalf("the web's models clear what they leave out: %+v", m)
	}
}
