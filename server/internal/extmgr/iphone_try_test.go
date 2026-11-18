package extmgr

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/nwverse/persona/server/internal/proto"
)

// Try It (#8199): from the owner's own app, a look then Weather opened without the model. A real iPhone asks before
// the launch (unchecked); that one step is approved at once, anything else asked is not, and only the user's own
// signed-in app may ask at all.
func TestIPhoneTry(t *testing.T) {
	h, _ := extensionTestHub(t)
	ctx := context.Background()
	mux := h.routes()
	launched := `{"steps":[{"step":1,"action":"launch","delivery":"sent"}]}`
	asks := proto.IPhoneApproval{Step: 1, Action: "launch", Needs: []string{"unchecked"}, LaunchApp: iphoneTryApp}
	var calls []proto.IPhoneCall
	_, seen := iphoneMac(t, h, "mac", []proto.IPhoneDevice{testIPhone}, func(c proto.IPhoneCall) (proto.IPhoneResult, error) {
		calls = append(calls, c)
		switch {
		case c.Op == "look":
			return proto.IPhoneResult{Output: json.RawMessage(`{}`), Image: &proto.Image{Mime: "image/jpeg", Data: "AAAA"}}, nil
		case c.Approved == "ap1":
			return proto.IPhoneResult{Output: json.RawMessage(launched)}, nil
		default:
			return proto.IPhoneResult{NeedsApproval: true, Approval: &asks, ApprovalID: "ap1"}, nil
		}
	})
	phone := deviceToken("dev_phone", roleFull)
	if code, _ := call(t, mux, "POST", "/api/v1/iphone/try", phone, `{"device": "dev-a1"}`); code != 404 {
		t.Fatalf("the extension off: %d", code)
	}
	if err := h.extensions.reg.SetEnabled(ctx, iphoneExtensionID, true, "test"); err != nil {
		t.Fatal(err)
	}
	if code, _ := call(t, mux, "POST", "/api/v1/iphone/try", h.cfg.Token, `{"device": "dev-a1"}`); code != 403 {
		t.Fatalf("not the user's own app: %d", code)
	}
	if code, _ := call(t, mux, "POST", "/api/v1/iphone/try", phone, `{"device": "dev-a1"}`); code != 409 {
		t.Fatalf("not allowed yet: %d", code)
	}
	call(t, mux, "PUT", "/api/v1/machines/mac/iphone", h.cfg.Token, `{"enabled": true}`)
	call(t, mux, "PUT", "/api/v1/iphone/consent", phone, `{"device": "dev-a1", "allowed": true}`)
	if code, _ := call(t, mux, "POST", "/api/v1/iphone/try", phone, `{}`); code != 400 {
		t.Fatalf("no device: %d", code)
	}
	code, out := call(t, mux, "POST", "/api/v1/iphone/try", phone, `{"device": "dev-a1"}`)
	shot, _ := out["screenshot"].(map[string]any)
	if code != 200 || out["opened"] != true || out["app"] != iphoneTryApp || shot["data"] != "AAAA" {
		t.Fatalf("%d %v", code, out)
	}
	if len(calls) != 3 || calls[0].Op != "look" || calls[1].Op != "do" || !strings.Contains(string(calls[1].Args), iphoneTryApp) ||
		calls[2].Approved != "ap1" || len(calls[2].Args) != 0 {
		t.Fatalf("calls %+v", calls)
	}

	// Something else asked for: not approved, nothing resumed.
	asks = proto.IPhoneApproval{Step: 1, Action: "tap", Needs: []string{"unchecked"}, At: &proto.IPhonePoint{X: 1, Y: 2}}
	calls = nil
	code, out = call(t, mux, "POST", "/api/v1/iphone/try", phone, `{"device": "dev-a1"}`)
	if code != 200 || out["opened"] != false || out["error"] == "" || len(calls) != 2 {
		t.Fatalf("another ask: %d %v %+v", code, out, calls)
	}
	_ = seen
}

func TestIPhoneLaunchSent(t *testing.T) {
	for out, want := range map[string]bool{
		`{"steps":[{"action":"launch","delivery":"sent"}]}`:     true,
		`{"steps":[{"action":"launch","delivery":"not_sent"}]}`: false,
		`{"steps":[{"action":"launch","delivery":"unknown"}]}`:  false,
		`{}`:     false,
		`broken`: false,
	} {
		if got := iphoneLaunchSent(json.RawMessage(out)); got != want {
			t.Errorf("%s: %v", out, got)
		}
	}
}
