package extension

import (
	"strings"
	"testing"
)

// The transcription point (docs/assistant/meeting-minutes.md "The `transcription` extension point"): an id and a
// name, served over the program's HTTP side as voice is; the same id may be a voice contribution's too.
func TestTranscriptionPoint(t *testing.T) {
	httpOnly := func(contributes map[string]any) []byte {
		return with(t, func(m map[string]any) {
			m["runtime"] = map[string]any{"command": "bin/x", "rpc": "none", "http": true}
			m["activation_events"] = []any{"on_startup"}
			m["contributes"] = contributes
		})
	}
	m, err := Parse(httpOnly(map[string]any{
		"voice":         []any{map[string]any{"id": "talk", "name": "Talk"}},
		"transcription": []any{map[string]any{"id": "gpt-live", "name": "GPT-Live"}, map[string]any{"id": "talk"}},
	}))
	if err != nil {
		t.Fatal(err)
	}
	if got := m.Items("transcription"); strings.Join(got, ",") != "gpt-live,talk" {
		t.Fatalf("items %v", got)
	}
	if m.Kind() != "voice" {
		t.Fatalf("a voice extension that also transcribes is still of kind voice: %q", m.Kind())
	}
	if p := Points["transcription"]; p.Version != 1 || !p.NeedsHTTP || p.NeedsRPC || p.Namespace != "transcription" {
		t.Fatalf("%+v", p)
	}
	if PointVersions()["transcription"] != 1 {
		t.Fatal("the point's version goes to the programs")
	}
	if only, err := Parse(httpOnly(map[string]any{"transcription": []any{map[string]any{"id": "tx"}}})); err != nil || only.Kind() != "transcription" {
		t.Fatalf("transcription alone: %v %v", only, err)
	}

	for name, contributes := range map[string]map[string]any{
		"an empty list":  {"transcription": []any{}},
		"not a list":     {"transcription": map[string]any{"id": "tx"}},
		"a bad id":       {"transcription": []any{map[string]any{"id": "Bad ID"}}},
		"no id":          {"transcription": []any{map[string]any{"name": "x"}}},
		"an id twice":    {"transcription": []any{map[string]any{"id": "tx"}, map[string]any{"id": "tx"}}},
		"another field":  {"transcription": []any{map[string]any{"id": "tx", "features": []any{"live"}}}},
		"a name not str": {"transcription": []any{map[string]any{"id": "tx", "name": 3}}},
	} {
		if _, err := Parse(httpOnly(contributes)); err == nil {
			t.Errorf("%s: taken", name)
		}
	}
	// It needs the program's HTTP side: the manager dials /transcription/<id>/… on its socket.
	if _, err := Parse(with(t, func(m map[string]any) {
		m["contributes"] = map[string]any{"transcription": []any{map[string]any{"id": "tx"}}}
	})); err == nil || !strings.Contains(err.Error(), "transcription") {
		t.Fatalf("transcription without http: %v", err)
	}
}
