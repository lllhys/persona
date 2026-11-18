package extmgr

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/nwverse/persona/server/internal/extension"
	"github.com/nwverse/persona/server/internal/i18n"
)

func TestExtensionStepSurvivesFormCopiesAndDetailSerialization(t *testing.T) {
	s, err := extension.ParseSchema(json.RawMessage(`{"type":"object","properties":{
		"goal":{"type":"integer","x-step":250},
		"group":{"type":"object","properties":{"count":{"type":"integer","x-step":10}}}
	}}`))
	if err != nil {
		t.Fatal(err)
	}
	ts := extension.Translations{i18n.En: &extension.Translation{Configuration: map[string]*extension.FieldText{
		"goal":  {Title: "Daily goal"},
		"group": {Properties: map[string]*extension.FieldText{"count": {Title: "Count"}}},
	}}}
	localized := ts.Schema(i18n.En, s)
	form := nodeConfirmForm(i18n.En, localized, &extension.Manifest{Node: &extension.NodeRuntime{Machines: "machines"}}, "Example")
	form = withoutIPhonePhones(&extension.Extension{Record: extension.Record{ID: iphoneExtensionID}}, form)
	for _, path := range []string{"/api/v1/extensions/example/-/", "/api/v1/admin/extensions/example", "/api/v1/plugins/example/-/"} {
		raw, err := json.Marshal(detailOut(httptest.NewRequest("GET", path, nil), ExtensionDetail{Form: form}))
		if err != nil {
			t.Fatal(err)
		}
		var out struct {
			Form extension.Schema `json:"form"`
		}
		if err := json.Unmarshal(raw, &out); err != nil {
			t.Fatal(err)
		}
		goal, count := out.Form.Fields[0], out.Form.Fields[1].Fields[0]
		if goal.Step == nil || *goal.Step != 250 || goal.Title != "Daily goal" || count.Step == nil || *count.Step != 10 || count.Title != "Count" {
			t.Fatalf("%s lost its step or text: %s", path, raw)
		}
	}
	if s.Fields[0].Title != "" || s.Fields[1].Fields[0].Title != "" {
		t.Fatal("form copies changed the shared schema")
	}
}

func TestSelfSettingsStepDoesNotConstrainValues(t *testing.T) {
	h := extensionSettingsHub(t)
	m := &extension.Manifest{ManifestVersion: extension.ManifestVersion, ID: "example-goal", Name: "Goal", Version: "1.0.0",
		Engines:       extension.Engines{ExtensionProtocol: extension.Protocol},
		Configuration: json.RawMessage(`{"type":"object","properties":{"goal":{"type":"integer","minimum":1000,"maximum":3000,"x-step":250}}}`)}
	if err := h.extensions.reg.AddBuiltin(m, "/opt/example", true); err != nil {
		t.Fatal(err)
	}
	text, err := h.extensionSettingsText(m.ID)
	if err != nil || !strings.Contains(text, "a whole number from 1000 up to 3000") {
		t.Fatalf("assistant bounds: %s, %v", text, err)
	}
	change, err := h.setExtensionSettings(m.ID, "Goal", json.RawMessage(`{"goal":2100}`))
	if err != nil {
		t.Fatal(err)
	}
	if change.apply == nil {
		t.Fatal("off-grid configuration has no change to approve")
	}
	if _, err := change.apply(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	if got := h.extensions.reg.SavedConfig(h.extensions.reg.Get(m.ID)); string(got) != `{"goal":2100}` {
		t.Fatalf("assistant changed the typed value: %s", got)
	}
	if _, err := h.setExtensionSettings(m.ID, "Goal", json.RawMessage(`{"goal":3001}`)); err == nil {
		t.Fatal("assistant ignored the maximum")
	}
}
