package hub

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/nwverse/persona/server/internal/db"
	"github.com/nwverse/persona/server/internal/extwire"
)

// The live screen's video dial carries x264 (#8859) only while the manager reports it on for that Mac (the plugin's
// screen_x264); the node then hands the helper the user's ffmpeg, if it has one.
func TestTheVideoDialAsksForX264OnlyWhenOnForTheMac(t *testing.T) {
	for _, on := range []bool{false, true} {
		t.Run(fmt.Sprint(on), func(t *testing.T) {
			h := newTestHub(t)
			h.store = &tokenStore{memStore: *newMemStore(), rows: map[string]db.DeviceToken{}, rev: map[string]bool{}}
			tok := "jd_" + randomHex(24)
			_ = h.store.CreateDeviceToken(context.Background(), db.DeviceToken{ID: "dev", Name: "iPhone", Kind: "phone", Role: roleFull}, hashToken(tok))
			var mu sync.Mutex
			var dials []map[string]any
			screenNode(t, h, echoVNC(t), false, "", func(method string, p map[string]any) {
				if method == "screen.dial" {
					mu.Lock()
					dials = append(dials, p)
					mu.Unlock()
				}
			})
			computer, _ := json.Marshal(map[string]any{"enabled": true, "screen_x264": on})
			h.ext.report.Store(&extwire.Report{Machines: map[string]extwire.MachineExtras{"m1": {ScreenPort: true, Computer: computer}}})
			srv := httptest.NewServer(h.routes())
			t.Cleanup(srv.Close)
			u := fmt.Sprintf("ws%s/api/v1/machines/m1/screen/video/connect?token=%s&pair=K", strings.TrimPrefix(srv.URL, "http"), url.QueryEscape(tok))
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			c, _, err := websocket.Dial(ctx, u, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer c.CloseNow()
			mu.Lock()
			defer mu.Unlock()
			if len(dials) != 1 {
				t.Fatalf("screen.dial calls = %d", len(dials))
			}
			if got, _ := dials[0]["x264"].(bool); got != on {
				t.Fatalf("x264 in screen.dial = %v (%v), want %v", dials[0]["x264"], dials[0], on)
			}
		})
	}
}
