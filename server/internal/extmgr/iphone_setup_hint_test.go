package extmgr

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/nwverse/persona/server/internal/proto"
	"github.com/nwverse/persona/server/internal/rpc"
)

const setupLink = "persona://iphone/setup"

// The guided setup's link goes with an iPhone tool's result exactly when the reason is a setup the user hasn't finished
// (#8199): each such failure, iphone_status with an iPhone not set up, and persona_settings on extension.iphone while
// no iPhone is allowed. Never on success, never on another failure, never in a description.
func TestIPhoneSetupHint(t *testing.T) {
	h, _ := extensionTestHub(t)
	ctx := context.Background()
	fail := ""
	other := proto.IPhoneDevice{ID: "dev-b2", Name: "Other iPhone", ProductType: "iPhone16,2", IOS: "27.0", Paired: true, Code: "developer_mode_off"}
	iphoneMac(t, h, "mac", []proto.IPhoneDevice{testIPhone, other}, func(p proto.IPhoneCall) (proto.IPhoneResult, error) {
		if fail == "" {
			return proto.IPhoneResult{Output: json.RawMessage(`{"done":true}`)}, nil
		}
		data, _ := json.Marshal(proto.IPhoneErrorData{Sent: proto.IPhoneNotSent})
		return proto.IPhoneResult{}, &rpc.Error{Code: fail, Message: "the phone said " + fail, Data: data}
	})
	if err := h.extensions.reg.SetEnabled(ctx, iphoneExtensionID, true, "test"); err != nil {
		t.Fatal(err)
	}
	hinted := func(res proto.ToolResult) bool { return strings.Contains(res.Content[0].Text, setupLink) }
	settings := func(key string) string {
		out, err := h.toolPersonaSettings(ctx, "server", "s1", json.RawMessage(`{"action":"get","key":"`+key+`"}`))
		if err != nil {
			t.Fatal(err)
		}
		return out
	}

	// Nothing allowed yet: a call has no iPhone to go to, and the setting says where to finish.
	if res := h.callIPhoneTool(ctx, "c", "iphone_look", nil); !res.IsError || !hinted(res) {
		t.Fatalf("none allowed: %+v", res)
	}
	if !strings.Contains(settings("extension.iphone"), setupLink) {
		t.Fatal("persona_settings on the extension with none allowed")
	}

	if err := h.extensions.reg.SetConfig(ctx, iphoneExtensionID, json.RawMessage(`{"machines": ["mac"], "phones": ["dev-a1"]}`), "test"); err != nil {
		t.Fatal(err)
	}
	h.refreshIPhoneMacs(ctx)
	if strings.Contains(settings("extension.iphone"), setupLink) {
		t.Fatal("persona_settings with an iPhone allowed")
	}
	// A wrong name while one is allowed is the model's mistake, not the setup.
	if res := h.callIPhoneTool(ctx, "c", "iphone_look", json.RawMessage(`{"phone": "nope"}`)); !res.IsError || hinted(res) {
		t.Fatalf("a wrong name: %+v", res)
	}
	if res := h.callIPhoneTool(ctx, "c", "iphone_look", nil); res.IsError || hinted(res) {
		t.Fatalf("success: %+v", res)
	}
	for code := range iphoneSetupCodes {
		fail = code
		if res := h.callIPhoneTool(ctx, "c", "iphone_look", nil); !res.IsError || !hinted(res) {
			t.Fatalf("%s: %+v", code, res)
		}
	}
	for _, code := range []string{"timeout", "locked", "busy", "bad_args", "service_unavailable"} {
		fail = code
		if res := h.callIPhoneTool(ctx, "c", "iphone_look", nil); !res.IsError || hinted(res) {
			t.Fatalf("%s is not about the setup: %+v", code, res)
		}
	}

	// iphone_status: the other iPhone isn't allowed (and its Developer Mode is off), so the setup line is there.
	fail = ""
	if res := h.callIPhoneTool(ctx, "c", "iphone_status", nil); res.IsError || !hinted(res) {
		t.Fatalf("status with one not set up: %+v", res)
	}
	if err := h.extensions.reg.SetConfig(ctx, iphoneExtensionID, json.RawMessage(`{"machines": ["mac"], "phones": ["dev-a1", "dev-b2"]}`), "test"); err != nil {
		t.Fatal(err)
	}
	h.refreshIPhoneMacs(ctx)
	if res := h.callIPhoneTool(ctx, "c", "iphone_status", nil); !hinted(res) {
		t.Fatalf("allowed, Developer Mode still off: %+v", res)
	}

	// The descriptions and every other setting stay as they are.
	for _, tool := range h.iphoneTools() {
		if strings.Contains(tool.Description, setupLink) {
			t.Fatalf("%s's description", tool.Name)
		}
	}
	if strings.Contains(settings("inbox.rules"), setupLink) {
		t.Fatal("another setting")
	}
}

// With everything set up, iphone_status carries no setup line.
func TestIPhoneSetupHintGoneWhenSetUp(t *testing.T) {
	h, _ := extensionTestHub(t)
	ctx := context.Background()
	iphoneMac(t, h, "mac", []proto.IPhoneDevice{testIPhone}, nil)
	if err := h.extensions.reg.SetEnabled(ctx, iphoneExtensionID, true, "test"); err != nil {
		t.Fatal(err)
	}
	if err := h.extensions.reg.SetConfig(ctx, iphoneExtensionID, json.RawMessage(`{"machines": ["mac"], "phones": ["dev-a1"]}`), "test"); err != nil {
		t.Fatal(err)
	}
	h.refreshIPhoneMacs(ctx)
	if res := h.callIPhoneTool(ctx, "c", "iphone_status", nil); res.IsError || strings.Contains(res.Content[0].Text, setupLink) {
		t.Fatalf("%+v", res)
	}
}
