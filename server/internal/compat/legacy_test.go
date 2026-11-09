package compat

import (
	"bytes"
	"encoding/json"
	"net/http"
	"reflect"
	"testing"
)

func TestInstalledServiceAndProbeBytesStayStable(t *testing.T) {
	for _, test := range []struct{ name, got, want string }{
		{"node label", NodeLabel, "com.jarvis.node"},
		{"port probe", PortProbeAgent, "jarvis-ports"},
		{"resident node matcher", ResidentNodePattern, "([j]arvisd|[p]ersonad|[p]ersona-node) node --attach"},
	} {
		if test.got != test.want {
			t.Fatalf("%s changed: got %q, want %q", test.name, test.got, test.want)
		}
	}
}

func TestExactRuntimeAliasesPreserveCustomNames(t *testing.T) {
	for _, pair := range [][2]string{{"jarvis-dsh-runtime", "persona-dsh-runtime"}, {"jarvis-pi-runtime", "persona-pi-runtime"}, {"jarvis-paseo-runtime", "persona-paseo-runtime"}, {"jarvis-agent-sdk-runtime", "persona-agent-sdk-runtime"}} {
		if RuntimePackage(pair[0]) != pair[1] || LegacyRuntimePackage(pair[1]) != pair[0] {
			t.Fatal(pair)
		}
	}
	for _, name := range []string{"jarvis-user-package", "persona-custom-package", "custom"} {
		if RuntimePackage(name) != name || LegacyRuntimePackage(name) != name {
			t.Fatal("custom package was rewritten", name)
		}
	}
}
func TestHeaderAliasesAndConflicts(t *testing.T) {
	h := http.Header{"X-Jarvis-Op-Id": {"11111111-2222-4333-8444-555555555555"}}
	if Header(h, "X-Persona-Op-Id") == "" {
		t.Fatal("baseline header ignored")
	}
	SetHeader(h, "X-Persona-Op-Id", "same")
	if h.Get("X-Jarvis-Op-Id") != "same" || HeaderConflict(h, "X-Persona-Op-Id") {
		t.Fatal(h)
	}
	h.Set("X-Jarvis-Op-Id", "different")
	if !HeaderConflict(h, "X-Persona-Op-Id") {
		t.Fatal("conflicting receipt accepted")
	}
	DelHeader(h, "X-Persona-Op-Id")
	if h.Get("X-Jarvis-Op-Id") != "" || h.Get("X-Persona-Op-Id") != "" {
		t.Fatal("alias survived sanitization")
	}
}
func TestOnlyExactContextTagsAreProjected(t *testing.T) {
	in := `<persona-context kind="context">Persona project</persona-context><persona-file path="x"/><persona-contextual>Keep</persona-contextual>`
	expected := `<jarvis-context kind="context">Persona project</jarvis-context><jarvis-file path="x"/><persona-contextual>Keep</persona-contextual>`
	if out := LegacyContextTags(in); out != expected {
		t.Fatal(out)
	}
}
func TestProjectionIsScopedAndPreservesOpaqueValues(t *testing.T) {
	focus := []byte(`{"type":"herdr_focus","source":"persona","id":"persona-circuit","mine":true}`)
	if string(ProjectJSON(focus, false)) != string(focus) {
		t.Fatal("stable focus identity was rewritten")
	}
	raw := []byte(`{"type":"items","large":9007199254740993,"items":[{"text":"<persona-context>fixture</persona-context>","title":"Jarvis project","id":"persona-visor"}]}`)
	var out map[string]any
	d := json.NewDecoder(bytes.NewReader(ProjectJSON(raw, false)))
	d.UseNumber()
	if err := d.Decode(&out); err != nil {
		t.Fatal(err)
	}
	if out["large"] != json.Number("9007199254740993") {
		t.Fatal("opaque number was rounded", out["large"])
	}
	item := out["items"].([]any)[0].(map[string]any)
	if item["text"] != `<jarvis-context>fixture</jarvis-context>` || item["title"] != "Jarvis project" || item["id"] != "persona-visor" {
		t.Fatal(item)
	}
	if string(ProjectJSON(raw, true)) != string(raw) {
		t.Fatal("opt-in peer lost canonical bytes")
	}
}
func TestMCPNamespacesShareOnlyTheBuiltinHandler(t *testing.T) {
	env := map[string]string{"PERSONA_SESSION_ID": "same", "JARVIS_SESSION_ID": "stale", "PERSONA_GUEST": "", "JARVIS_GUEST": "1"}
	shared := map[string]any{"command": "fixture", "env": env}
	servers := AddMCPAliases(map[string]any{"persona": shared, "extension": map[string]any{"command": "other"}})
	if len(servers) != 3 || !reflect.DeepEqual(servers["jarvis"], servers["persona"]) {
		t.Fatal(servers)
	}
	mirrored := shared["env"].(map[string]string)
	if mirrored["JARVIS_SESSION_ID"] != "same" || mirrored["JARVIS_GUEST"] != "" {
		t.Fatal("identity or empty-value precedence was lost")
	}
	expected := []string{"mcp__persona__lark_agent_reply", "mcp__jarvis__lark_agent_reply"}
	if !reflect.DeepEqual(MCPToolNames([]string{"lark_agent_reply"}), expected) {
		t.Fatal("guest permission scope expanded")
	}
}
