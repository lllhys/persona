package hub

import (
	"bytes"
	"encoding/json"
	"reflect"

	"github.com/nwverse/persona/server/internal/i18n"
	"github.com/nwverse/persona/server/internal/proto"
)

// The manager's deployment reports (and saved errors) are canonical, not tied to the language at job start, and so
// are the notes nodes write on their engines (i18n.EngineText). Translate copies at the last boundary, including an
// old event replayed to a connection in a different language.
func localizedMachines(l i18n.Lang, machines []proto.Machine) []proto.Machine {
	out := append([]proto.Machine{}, machines...)
	for i := range out {
		out[i].DeployError = i18n.DeploymentText(l, out[i].DeployError)
		out[i].DeployReason = i18n.DeploymentText(l, out[i].DeployReason)
		out[i].Computer = i18n.ComputerStatus(l, out[i].Computer)
		out[i].Browser = i18n.BrowserStatus(l, out[i].Browser)
		out[i].IPhone = i18n.IPhoneStatus(l, out[i].IPhone)
		out[i].Clipboard = i18n.ClipboardStatus(l, out[i].Clipboard)
		out[i].Engines = i18n.EngineNotes(l, out[i].Engines)
	}
	return out
}

func localizedMachineEvent(l i18n.Lang, raw []byte) []byte {
	if !bytes.Contains(raw, []byte(`"machines"`)) {
		return raw
	}
	var body map[string]json.RawMessage
	if json.Unmarshal(raw, &body) != nil || string(body["type"]) != `"machines"` {
		return raw
	}
	var machines []proto.Machine
	if json.Unmarshal(body["machines"], &machines) != nil {
		return raw
	}
	translated := localizedMachines(l, machines)
	changed := false
	for i := range machines {
		changed = changed || machines[i].DeployError != translated[i].DeployError || machines[i].DeployReason != translated[i].DeployReason ||
			!reflect.DeepEqual(machines[i].Computer, translated[i].Computer) || !reflect.DeepEqual(machines[i].Browser, translated[i].Browser) ||
			!reflect.DeepEqual(machines[i].IPhone, translated[i].IPhone) ||
			!reflect.DeepEqual(machines[i].Clipboard, translated[i].Clipboard) ||
			!reflect.DeepEqual(machines[i].Engines, translated[i].Engines)
	}
	if !changed {
		return raw
	}
	body["machines"], _ = json.Marshal(translated)
	result, err := json.Marshal(body)
	if err != nil {
		return raw
	}
	return result
}

func localizedDeploymentError(l i18n.Lang, raw []byte) []byte {
	var body map[string]json.RawMessage
	if json.Unmarshal(raw, &body) != nil {
		return raw
	}
	var text string
	if json.Unmarshal(body["error"], &text) != nil {
		return raw
	}
	translated := i18n.DeploymentText(l, text)
	if translated == text {
		return raw
	}
	body["error"], _ = json.Marshal(translated)
	result, err := json.Marshal(body)
	if err != nil {
		return raw
	}
	return result
}
