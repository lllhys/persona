package extmgr

import (
	"context"
	"encoding/json"
	"testing"
)

// x264 for the live screen (#8859): per Mac and off by default, turned on from its page into the plugin's screen_x264,
// and reported as the Mac's screen_x264 only while the plugin and the Mac's switch are on.
func TestScreenX264Switch(t *testing.T) {
	h, _ := extensionTestHub(t)
	ctx := context.Background()
	mux := h.routes()
	mac, _ := computerMac(t, h, "mac")
	if err := h.extensions.reg.SetEnabled(ctx, cuaExtensionID, true, "test"); err != nil {
		t.Fatal(err)
	}
	if code, out := call(t, mux, "PUT", "/api/v1/machines/mac/computer", h.cfg.Token, `{"enabled": true}`); code != 200 || out["screen_x264"] == true {
		t.Fatalf("on for the Mac, x264 not: %d %v", code, out)
	}
	if code, out := call(t, mux, "PUT", "/api/v1/machines/mac/computer", h.cfg.Token, `{"screen_x264": true}`); code != 200 || out["screen_x264"] != true || out["enabled"] != true || out["no_auto_lock"] == true {
		t.Fatalf("turned on, the other switches left as they were: %d %v", code, out)
	}
	var cfg map[string]any
	_ = json.Unmarshal(h.extensions.reg.Config(h.extensions.reg.Get(cuaExtensionID)), &cfg)
	if l, _ := cfg["screen_x264"].([]any); len(l) != 1 || l[0] != "mac" {
		t.Fatalf("in the plugin's settings: %v", cfg)
	}
	if c := h.computerOf(mac); c == nil || !c.ScreenX264 {
		t.Fatalf("the Mac's status: %+v", c)
	}
	if code, out := call(t, mux, "PUT", "/api/v1/machines/mac/computer", h.cfg.Token, `{"screen_x264": false}`); code != 200 || out["screen_x264"] == true {
		t.Fatalf("turned off: %d %v", code, out)
	}
	call(t, mux, "PUT", "/api/v1/machines/mac/computer", h.cfg.Token, `{"screen_x264": true}`)
	call(t, mux, "PUT", "/api/v1/machines/mac/computer", h.cfg.Token, `{"enabled": false}`)
	if c := h.computerOf(mac); c != nil && c.ScreenX264 {
		t.Fatalf("the Mac's switch off: x264 must be too: %+v", c)
	}
}

// The plugin's settings form offers it per Mac, asking first, like no_auto_lock.
func TestScreenX264IsAPerMacListThatAsksFirst(t *testing.T) {
	var schema struct {
		Properties map[string]struct {
			Type    string `json:"type"`
			Confirm string `json:"x-confirm"`
			Items   struct {
				Format string   `json:"format"`
				OS     []string `json:"x-os"`
			} `json:"items"`
		} `json:"properties"`
	}
	if err := json.Unmarshal([]byte(cuaConfig), &schema); err != nil {
		t.Fatal(err)
	}
	p, ok := schema.Properties["screen_x264"]
	if !ok || p.Type != "array" || p.Confirm == "" || p.Items.Format != "machine" || len(p.Items.OS) != 1 || p.Items.OS[0] != "darwin" {
		t.Fatalf("screen_x264 = %+v", p)
	}
}
