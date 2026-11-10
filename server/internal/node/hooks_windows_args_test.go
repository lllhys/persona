package node

import (
	"encoding/json"
	"slices"
	"testing"

	"github.com/nwverse/persona/server/internal/nodeclient"
)

func TestHookSettingsWindowsNativeArgs(t *testing.T) {
	const bin = `C:\Program Files\café\personad.exe`
	for _, dir := range []string{"", `C:\Users\café user\.persona`} {
		for _, assistant := range []bool{false, true} {
			native := hookSettingsForOS("windows", bin, dir, assistant)
			legacy := hookSettingsForOS("linux", bin, dir, assistant)
			events := native["hooks"].(map[string]any)
			if len(events) != 8 {
				t.Fatalf("lost hook events: %v", events)
			}
			expectedArgs := []string{"hook"}
			if dir != "" {
				expectedArgs = append(expectedArgs, nodeclient.HomeFlag, dir)
			}
			for event, raw := range events {
				for _, block := range raw.([]any) {
					for _, rawHook := range block.(map[string]any)["hooks"].([]any) {
						h := rawHook.(map[string]any)
						if h["command"] != bin || !slices.Equal(h["args"].([]string), expectedArgs) {
							t.Fatalf("%s: not native argv: %v", event, h)
						}
					}
				}
			}
			before, _ := json.Marshal(legacy)
			darwin, _ := json.Marshal(hookSettingsForOS("darwin", bin, dir, assistant))
			if string(darwin) != string(before) {
				t.Fatal("non-Windows hook generation changed")
			}
			// Only command and args may differ: retain every event, matcher and timeout.
			for _, raw := range legacy["hooks"].(map[string]any) {
				for _, block := range raw.([]any) {
					for _, rawHook := range block.(map[string]any)["hooks"].([]any) {
						h := rawHook.(map[string]any)
						h["command"], h["args"] = bin, expectedArgs
					}
				}
			}
			got, _ := json.Marshal(native)
			want, _ := json.Marshal(legacy)
			if string(got) != string(want) {
				t.Fatalf("changed gate behavior:\n%s\n%s", got, want)
			}
		}
	}
}
