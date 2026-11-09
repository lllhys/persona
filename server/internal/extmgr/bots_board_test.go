package extmgr

import (
	"testing"

	"github.com/nwverse/persona/server/internal/assistantwire"
)

// The Lark bot's off-by-default switches, `board` (#6052) and `only_conclusions` (#1226): each is off by default and for
// settings saved before it; an app that does not know it leaves it as it was; the assistant's feishu.status says it and
// feishu.configure sets it. The Lark bot's channel page draws its switches from the list the manager gives: each is one
// of them, off.
func TestBotOffByDefaultFeatureSwitches(t *testing.T) {
	for _, feature := range []string{"board", "only_conclusions"} {
		t.Run(feature, func(t *testing.T) {
			t.Run("bot settings", func(t *testing.T) {
				w := newBotWorld(t)
				w.pair(t)
				feats := func() map[string]any {
					f, _ := w.feishu(t, assistantwire.FeishuStatusGet, nil)["features"].(map[string]any)
					return f
				}
				if f := feats(); f[feature] != false {
					t.Fatalf("default features %v: %s must be off", f, feature)
				}
				if code, _ := w.do(t, "PUT", "/api/v1/bots/feishu", `{"features":{"main":true,"cards":true,"`+feature+`":true}}`); code != 200 {
					t.Fatal(code)
				}
				if f := feats(); f[feature] != true || f["reports"] != true {
					t.Fatalf("%s on: %v", feature, f)
				}
				// An app from before the switch sends the others only, and leaves it as it was.
				if code, _ := w.do(t, "PUT", "/api/v1/bots/feishu", `{"features":{"main":true,"cards":true,"agents":true}}`); code != 200 {
					t.Fatal(code)
				}
				if f := feats(); f[feature] != true || f["agents"] != true {
					t.Fatalf("an older app's PUT: %v", f)
				}
				w.feishu(t, assistantwire.FeishuConfigure, map[string]any{"features": map[string]bool{feature: false}})
				if f := feats(); f[feature] != false || f["agents"] != true {
					t.Fatalf("%s off by the assistant: %v", feature, f)
				}
			})
			t.Run("channel list", func(t *testing.T) {
				w := newChannelWorld(t)
				out, err := w.channels(t, assistantwire.ChannelsList, nil)
				if err != nil {
					t.Fatal(err)
				}
				for _, c := range out["channels"].([]any) {
					m := c.(map[string]any)
					if m["id"] != "feishu" {
						continue
					}
					for _, f := range m["features"].([]any) {
						if fm := f.(map[string]any); fm["id"] == feature {
							if fm["on"] == true || fm["title"] == "" || fm["description"] == "" {
								t.Fatalf("the %s switch %v", feature, fm)
							}
							return
						}
					}
					t.Fatalf("no %s switch in %v", feature, m["features"])
				}
				t.Fatal("no Lark bot channel")
			})
		})
	}
}
