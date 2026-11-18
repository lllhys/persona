package hub

import (
	"encoding/json"
	"testing"
)

// An app that follows "messaging" for its Feishu screens says so (lark_via_messaging) and is not sent the legacy "lark"
// notice of the same change; every other app still is, and both get "messaging" (#9591).
func TestLegacyLarkGoesOnlyToAppsThatStillReadIt(t *testing.T) {
	h := newTestHub(t)
	legacy := &client{out: make(chan []byte, 32)}
	addTestClient(h, legacy)
	follows := &client{out: make(chan []byte, 32)}
	addTestClient(h, follows)
	h.onClientHello(follows, []byte(`{"platform":"ios-app","version":"test","features":["lark_via_messaging"]}`))
	<-follows.out // ack

	h.broadcastRaw("lark", []byte(`{"feed_ids":["oc_example"],"type":"lark"}`))
	h.broadcastRaw("messaging", []byte(`{"conversations":["oc_example"],"source":"lark","type":"messaging"}`))

	types := func(c *client) []string {
		var out []string
		for len(c.out) > 0 {
			var event struct {
				Type string `json:"type"`
			}
			if err := json.Unmarshal(<-c.out, &event); err != nil {
				t.Fatal(err)
			}
			out = append(out, event.Type)
		}
		return out
	}
	if got := types(legacy); len(got) != 2 || got[0] != "lark" || got[1] != "messaging" {
		t.Fatalf("an app that reads the old API: %v, want [lark messaging]", got)
	}
	if got := types(follows); len(got) != 1 || got[0] != "messaging" {
		t.Fatalf("an app that follows messaging: %v, want [messaging]", got)
	}

	// It stops following (its source went): a new hello without the feature, and the notice comes again.
	h.onClientHello(follows, []byte(`{"platform":"ios-app","version":"test","features":[]}`))
	<-follows.out // ack
	h.broadcastRaw("lark", []byte(`{"feed_ids":["oc_example"],"type":"lark"}`))
	if got := types(follows); len(got) != 1 || got[0] != "lark" {
		t.Fatalf("after a hello without the feature: %v, want [lark]", got)
	}
}
