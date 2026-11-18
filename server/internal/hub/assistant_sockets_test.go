package hub

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/nwverse/persona/server/internal/assistantwire"
	"github.com/nwverse/persona/server/internal/extension"
	"github.com/nwverse/persona/server/internal/extwire"
)

// audioManager is the manager's side of a meeting's audio socket faked: it upgrades GET …/audio, says what it was
// relayed with (the X-Persona-* headers, the path, the query and any credential) in its first message, then echoes;
// each socket's end on its side is sent on ended, with its device.
type audioManager struct {
	ended chan string
}

func (m *audioManager) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet || !strings.HasSuffix(r.URL.Path, "/audio") {
		http.NotFound(w, r)
		return
	}
	c, err := websocket.Accept(w, r, nil)
	if err != nil {
		return
	}
	defer c.CloseNow()
	device := r.Header.Get(extwire.HeaderDevice)
	defer func() { m.ended <- device }()
	seen := personaHeaders(r)
	seen["path"], seen["query"], seen["authorization"], seen["cookie"] = r.URL.Path, r.URL.RawQuery, r.Header.Get("Authorization"), r.Header.Get("Cookie")
	b, _ := json.Marshal(seen)
	if c.Write(r.Context(), websocket.MessageText, b) != nil {
		return
	}
	for {
		typ, b, err := c.Read(r.Context())
		if err != nil {
			return
		}
		if c.Write(r.Context(), typ, b) != nil {
			return
		}
	}
}

// A meeting's audio socket (assistantwire.SocketRoutes) goes to the manager over a relay stream, as an extension's
// socket does: upgraded end to end, frames both ways untouched, who asked in the hub's headers whatever the client
// forged, and the credential (Bearer or ?token=) never past the hub. Full credentials only: the watch's answer-only
// token and an extension's are refused before the upgrade, and nothing reaches the manager.
func TestAssistantSocketRelayed(t *testing.T) {
	m := &audioManager{ended: make(chan string, 8)}
	h, fx, srv := relayTestHubServing(t, m)
	master := h.cfg.Token
	phoneID, phone := relayDevice(t, srv, master, "iPhone", "phone")
	_, watch := relayDevice(t, srv, master, "Watch", "watch")
	ext := fx.issue("caller", "Caller", []string{extension.ScopeCallPrefix + "voice", extwire.ScopeKBRead, extwire.ScopeKBWrite})
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	open := func(path string, header http.Header) (map[string]string, *websocket.Conn) {
		t.Helper()
		c, _, err := websocket.Dial(ctx, wsURL(srv, path), &websocket.DialOptions{HTTPHeader: header})
		if err != nil {
			t.Fatal(err)
		}
		_, b, err := c.Read(ctx)
		if err != nil {
			t.Fatal(err)
		}
		seen := map[string]string{}
		_ = json.Unmarshal(b, &seen)
		return seen, c
	}
	forged := http.Header{"Authorization": {"Bearer " + phone}, "Cookie": {"persona_admin=x"}, "Accept-Language": {"ja"},
		"X-Persona-Device": {"someone-else"}, "X-Persona-Role": {"hub"}, "X-Persona-Owner": {"1"}, "X-Persona-Admin": {"1"}}
	seen, c := open("/api/v1/recordings/r1/audio", forged)
	defer c.CloseNow()
	if seen["path"] != "/api/v1/recordings/r1/audio" || seen["query"] != "" || seen["authorization"] != "" || seen["cookie"] != "" ||
		seen["X-Persona-Device"] != phoneID || seen["X-Persona-Device-Name"] != "iPhone" || seen["X-Persona-Role"] != roleFull ||
		seen["X-Persona-Lang"] != "ja" || seen["X-Persona-Owner"] != "" || seen["X-Persona-Admin"] != "" {
		t.Fatalf("relayed with %v", seen)
	}
	pcm := make([]byte, 3200) // 100 ms of 16 kHz PCM16
	for i := range pcm {
		pcm[i] = byte(i * 7)
	}
	for _, f := range []struct {
		typ  websocket.MessageType
		data []byte
	}{{websocket.MessageText, []byte(`{"type":"start","at":0,"timeline":{"roles":[{"at":0,"attendance":"listener"}]}}`)},
		{websocket.MessageBinary, pcm}, {websocket.MessageText, []byte(`{"type":"end","at":0.1}`)}} {
		if err := c.Write(ctx, f.typ, f.data); err != nil {
			t.Fatal(err)
		}
		if typ, b, err := c.Read(ctx); err != nil || typ != f.typ || string(b) != string(f.data) {
			t.Fatalf("echo %v %d bytes %v", typ, len(b), err)
		}
	}
	c.Close(websocket.StatusNormalClosure, "")

	// The token in the query, as a WebSocket client without headers sends it: taken, and kept from the manager; the
	// rest of the query goes on.
	seen, c = open("/api/v1/recordings/r2/audio?token="+phone+"&x=1", nil)
	defer c.CloseNow()
	if seen["path"] != "/api/v1/recordings/r2/audio" || seen["query"] != "x=1" || seen["X-Persona-Device"] != phoneID || seen["X-Persona-Role"] != roleFull {
		t.Fatalf("?token=: relayed with %v", seen)
	}
	c.Close(websocket.StatusNormalClosure, "")
	for range 2 {
		select {
		case <-m.ended:
		case <-time.After(5 * time.Second):
			t.Fatal("the manager's side did not end")
		}
	}

	for name, tok := range map[string]string{"the watch": watch, "an extension": ext} {
		_, res, err := websocket.Dial(ctx, wsURL(srv, "/api/v1/recordings/r1/audio"),
			&websocket.DialOptions{HTTPHeader: http.Header{"Authorization": {"Bearer " + tok}}})
		if err == nil || res == nil || res.StatusCode != http.StatusForbidden {
			t.Fatalf("%s upgraded: %v %v", name, res, err)
		}
		if code, out := call(t, srv.Config.Handler, "GET", "/api/v1/recordings/r1/audio", tok, ""); code != http.StatusForbidden {
			t.Fatalf("%s: %d %v", name, code, out)
		}
	}
	if code, out := call(t, srv.Config.Handler, "GET", "/api/v1/recordings/r1/audio", watch, ""); out["code"] != "forbidden" {
		t.Fatalf("the watch: %d %v", code, out)
	}
	// The gap upload beside it (an ordinary relayed route) is full credentials only too.
	for name, tok := range map[string]string{"the watch": watch, "an extension": ext} {
		if code, out := call(t, srv.Config.Handler, "PUT", "/api/v1/recordings/r1/audio?at=0", tok, "\x00\x00"); code != http.StatusForbidden {
			t.Fatalf("%s: PUT …/audio: %d %v", name, code, out)
		}
	}
	select {
	case device := <-m.ended:
		t.Fatalf("a refused caller reached the manager (%q)", device)
	default:
	}
}

// Revoking the phone ends its audio socket on both sides, at once.
func TestRevokingEndsTheAudioSocket(t *testing.T) {
	m := &audioManager{ended: make(chan string, 8)}
	h, _, srv := relayTestHubServing(t, m)
	master := h.cfg.Token
	phoneID, phone := relayDevice(t, srv, master, "iPhone", "phone")
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	c, _, err := websocket.Dial(ctx, wsURL(srv, "/api/v1/recordings/r1/audio"),
		&websocket.DialOptions{HTTPHeader: http.Header{"Authorization": {"Bearer " + phone}}})
	if err != nil {
		t.Fatal(err)
	}
	defer c.CloseNow()
	if _, _, err := c.Read(ctx); err != nil {
		t.Fatal(err)
	}
	if code, out := call(t, srv.Config.Handler, "DELETE", "/api/v1/auth/tokens/"+phoneID, master, ""); code != 200 {
		t.Fatalf("revoke: %d %v", code, out)
	}
	readCtx, readCancel := context.WithTimeout(ctx, 5*time.Second)
	defer readCancel()
	if _, _, err := c.Read(readCtx); err == nil || readCtx.Err() != nil {
		t.Fatalf("the revoked phone's audio socket is still open: %v", err)
	}
	select {
	case device := <-m.ended:
		if device != phoneID {
			t.Fatalf("ended on the manager's side: %q", device)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the manager's side of the revoked phone's audio socket is still open")
	}
}

// While the manager is away the audio socket is refused before the upgrade: 503 assistant_offline.
func TestAudioSocketWithoutTheManager(t *testing.T) {
	h := newTestHub(t)
	mux := h.routes()
	if code, out := call(t, mux, "GET", "/api/v1/recordings/r1/audio", h.cfg.Token, ""); code != http.StatusServiceUnavailable || out["code"] != assistantwire.CodeAssistantOffline {
		t.Fatalf("%d %v", code, out)
	}
}
