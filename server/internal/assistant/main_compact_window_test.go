package assistant

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/i18n"
	"github.com/nwverse/persona/server/internal/proto"
)

// The main conversation's starts and messages carry the window it is held to (#7314): 400000 while the setting is
// unset, the saved value otherwise (0 too: the node then follows the launcher); no other conversation carries one.
func TestMainCompactWindowGoesWithTheMainConversation(t *testing.T) {
	ctx := context.Background()
	f := newFakeHub().withMachine()
	s := newTestService(t, f)
	for _, tt := range []struct {
		name string
		set  *int
		want int
	}{
		{"unset", nil, 400_000},
		{"off", ptr(0), 0},
		{"saved", ptr(250_000), 250_000},
	} {
		t.Run(tt.name, func(t *testing.T) {
			setSettings(t, s, func(v *Settings) { v.Main, v.MainCompactWindow = "", tt.set })
			sess, err := s.start(ctx, StartParams{Main: true, Text: "hello"})
			if err != nil {
				t.Fatal(err)
			}
			if got := f.lastSend(t)["main_compact_window"]; got != float64(tt.want) {
				t.Fatalf("main start: main_compact_window %v, want %d", got, tt.want)
			}
			f.setSessions("server", proto.Session{SessionID: sess.SessionID, Assistant: true})
			setSettings(t, s, func(v *Settings) { v.Main = sess.SessionID })
			params, err := s.SendParams(ctx, SendParams{MachineID: "server", SessionID: sess.SessionID, Params: map[string]any{"text": "next"}})
			if err != nil || params["main_compact_window"] != tt.want {
				t.Fatalf("main message: %v %v", params, err)
			}
			// What the node asks for a message it sends by itself (assistant.model) carries it too.
			raw, _ := json.Marshal(map[string]string{"session_id": sess.SessionID})
			out, handled, err := s.NodeAnswer(ctx, "server", "assistant.model", raw)
			if m, _ := out.(map[string]any); err != nil || !handled || m["main_compact_window"] != tt.want {
				t.Fatalf("assistant.model: %v %v %v", out, handled, err)
			}
			// A relay into the main conversation (Service.call fills in is_main and the window).
			if err := s.deliverRelay(ctx, "server", sess.SessionID, relayMsg{Tool: noticeTool, Text: "A task finished.", At: time.Now().UTC()}); err != nil {
				t.Fatal(err)
			}
			if got := f.lastSend(t)["main_compact_window"]; got != float64(tt.want) {
				t.Fatalf("relay into the main conversation: %v", got)
			}
		})
	}
	setSettings(t, s, func(v *Settings) { v.MainCompactWindow = nil })
	side, err := s.start(ctx, StartParams{Text: "side"})
	if err != nil {
		t.Fatal(err)
	}
	if _, set := f.lastSend(t)["main_compact_window"]; set {
		t.Fatalf("another conversation's start carries a window: %v", f.lastSend(t))
	}
	f.setSessions("server", proto.Session{SessionID: side.SessionID, Assistant: true})
	params, err := s.SendParams(ctx, SendParams{MachineID: "server", SessionID: side.SessionID, Params: map[string]any{"text": "next", "main_compact_window": 1}})
	if _, set := params["main_compact_window"]; err != nil || set || params["is_main"] != false {
		t.Fatalf("another conversation's message: %v %v", params, err)
	}
}

// The manager's own messages into the main conversation (session.send carries is_main since #8365) and the spare warmed
// for its next start carry the window as its starts do: without it the node would start the main conversation again on
// its launcher's window, and never adopt the spare. Another conversation's session.send carries none.
func TestMainCompactWindowGoesWithSessionSendAndWarm(t *testing.T) {
	ctx := context.Background()
	f := newFakeHub().withMachine()
	s := newTestService(t, f)
	lastParams := func(method string) map[string]any {
		t.Helper()
		f.mu.Lock()
		defer f.mu.Unlock()
		for i := len(f.calls) - 1; i >= 0; i-- {
			if f.calls[i].Method == method {
				var p map[string]any
				if err := json.Unmarshal(f.calls[i].Params, &p); err != nil {
					t.Fatal(err)
				}
				return p
			}
		}
		t.Fatalf("no %s", method)
		return nil
	}
	main, err := s.start(ctx, StartParams{Main: true, Text: "hello"})
	if err != nil {
		t.Fatal(err)
	}
	side, err := s.start(ctx, StartParams{Text: "side"})
	if err != nil {
		t.Fatal(err)
	}
	f.setSessions("server", proto.Session{SessionID: main.SessionID, Assistant: true}, proto.Session{SessionID: side.SessionID, Assistant: true})
	setSettings(t, s, func(v *Settings) { v.Main, v.MainCompactWindow = main.SessionID, ptr(250_000) })
	if err := s.call(ctx, "server", DeviceSessionSend, map[string]any{"session_id": main.SessionID, "text": "A task finished."}, nil); err != nil {
		t.Fatal(err)
	}
	if p := lastParams(DeviceSessionSend); p["is_main"] != true || p["main_compact_window"] != float64(250_000) {
		t.Fatalf("session.send into the main conversation: %v", p)
	}
	if err := s.call(ctx, "server", DeviceSessionSend, map[string]any{"session_id": side.SessionID, "text": "A task finished."}, nil); err != nil {
		t.Fatal(err)
	}
	if p := lastParams(DeviceSessionSend); p["is_main"] == true || p["main_compact_window"] != nil {
		t.Fatalf("session.send into another conversation: %v", p)
	}
	s.warm("")
	if p := lastParams(DeviceAssistantWarm); p["is_main"] != true || p["main_compact_window"] != float64(250_000) {
		t.Fatalf("the main chat's warm spare: %v", p)
	}
}

// persona_settings and PUT /api/v1/assistant take 0 or 100000–10000000 tokens; the card names the window before and
// after in the user's language.
func TestMainCompactWindowSetting(t *testing.T) {
	ctx := context.Background()
	s := newTestService(t, newFakeHub().withMachine())
	var key string
	for _, o := range s.ownSettings() {
		if o.Key == "assistant.main_compact_window" {
			key = o.Key
			if got, _ := o.get(ctx); got != "400000" {
				t.Fatalf("get while unset: %s", got)
			}
		}
	}
	if key == "" || settingTitles[key] != "title_main_compact_window" {
		t.Fatal("assistant.main_compact_window is not offered with its title")
	}
	for _, bad := range []string{`99999`, `10000001`, `-1`, `"400000"`, `1.5`, `null`} {
		if _, err := s.setMainCompactWindow(ctx, json.RawMessage(bad)); err == nil {
			t.Fatalf("%s accepted", bad)
		}
	}
	if c, err := s.setMainCompactWindow(ctx, json.RawMessage(`400000`)); err != nil || c.apply != nil {
		t.Fatalf("the default again: %+v %v", c, err)
	}
	c, err := s.setMainCompactWindow(ctx, json.RawMessage(`0`))
	if err != nil || c.apply == nil {
		t.Fatalf("0: %+v %v", c, err)
	}
	for _, l := range []i18n.Lang{i18n.En, i18n.ZhHans, i18n.Ja} {
		want := i18n.T(l, "assistant-settings_tool.change_main_compact_window", i18n.Args{
			"before": i18n.T(l, "assistant-settings_tool.main_compact_window_tokens", i18n.Args{"tokens": 400_000}),
			"after":  i18n.T(l, "assistant-settings_tool.main_compact_window_launcher")})
		if got := c.card(l); got != want || strings.Contains(got, "{") || !strings.Contains(got, "400000") {
			t.Fatalf("%s card: %q", l, got)
		}
	}
	if _, err := c.apply(ctx); err != nil {
		t.Fatal(err)
	}
	if got := mainCompactWindowOf(s.settings(ctx)); got != 0 {
		t.Fatalf("after 0: %d", got)
	}
	if status, _ := serveJSON(t, s, "PUT", "/api/v1/assistant", "en", `{"main_compact_window":50000}`); status != http.StatusBadRequest {
		t.Fatalf("PUT 50000: %d", status)
	}
	if status, body := serveJSON(t, s, "PUT", "/api/v1/assistant", "en", `{"main_compact_window":300000}`); status != http.StatusOK {
		t.Fatalf("PUT 300000: %d %v", status, body)
	}
	if got := mainCompactWindowOf(s.settings(ctx)); got != 300_000 {
		t.Fatalf("after PUT: %d", got)
	}
	// A saved value out of range (written by hand) is not sent on: the default is.
	if got := mainCompactWindowOf(Settings{MainCompactWindow: ptr(5)}); got != defaultMainCompactWindow {
		t.Fatalf("an out-of-range saved value: %d", got)
	}
}
