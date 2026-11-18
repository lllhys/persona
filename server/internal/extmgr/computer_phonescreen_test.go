package extmgr

import (
	"context"
	"testing"
)

// Phone Screen's requests go to the Mac's node for a Mac whose screen the app may show, not before; the node's
// refusals keep their codes; an op the node doesn't have never leaves the hub.
func TestComputerPhoneScreen(t *testing.T) {
	h, _ := extensionTestHub(t)
	ctx := context.Background()
	mux := h.routes()
	_, seen := computerMac(t, h, "mac")
	const url = "/api/v1/machines/mac/computer/phone-screen"
	start := `{"pair": "P", "op": "start", "width": 402, "height": 760}`
	if code, _ := call(t, mux, "POST", url, h.cfg.Token, start); code != 404 {
		t.Fatalf("the plugin off: %d", code)
	}
	if err := h.extensions.reg.SetEnabled(ctx, cuaExtensionID, true, "test"); err != nil {
		t.Fatal(err)
	}
	if code, _ := call(t, mux, "PUT", "/api/v1/machines/mac/computer", h.cfg.Token, `{"enabled": true}`); code != 200 {
		t.Fatal(code)
	}
	code, out := call(t, mux, "POST", url, h.cfg.Token, start)
	if d, _ := out["display"].(map[string]any); code != 200 || d["width"] != float64(540) {
		t.Fatalf("start: %d %v", code, out)
	}
	if code, out := call(t, mux, "POST", url, h.cfg.Token, `{"pair": "Q", "op": "start", "width": 402, "height": 760}`); code != 403 || out["code"] != "not_signed_in" {
		t.Fatalf("another pair: %d %v", code, out)
	}
	if code, out := call(t, mux, "POST", url, h.cfg.Token, `{"pair": "P", "op": "place", "pid": 99, "window": 1}`); code != 404 || out["code"] != "window_gone" {
		t.Fatalf("a gone window: %d %v", code, out)
	}
	if code, _ := call(t, mux, "POST", url, h.cfg.Token, `{"pair": "P", "op": "stop"}`); code != 200 {
		t.Fatalf("stop: %d", code)
	}
	before := 0
	for _, c := range phoneCalls(seen) {
		if c.Tool != "" {
			before++
		}
	}
	for _, bad := range []string{`{"pair": "P", "op": "resize"}`, `{"op": "start", "width": 1, "height": 1}`, `not json`} {
		if code, out := call(t, mux, "POST", url, h.cfg.Token, bad); code != 400 {
			t.Fatalf("%s: %d %v", bad, code, out)
		}
	}
	if after := len(phoneCalls(seen)); after != before {
		t.Fatalf("a refused request reached the node (%d calls, was %d)", after, before)
	}
}

func phoneCalls(seen func() (int, []computerCallSeen)) []computerCallSeen {
	_, c := seen()
	return c
}
