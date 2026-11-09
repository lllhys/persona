package hub

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/nwverse/persona/server/internal/assistant"
	"github.com/nwverse/persona/server/internal/assistantwire"
	"github.com/nwverse/persona/server/internal/bus"
	"github.com/nwverse/persona/server/internal/db"
	"github.com/nwverse/persona/server/internal/devicewire"
	"github.com/nwverse/persona/server/internal/extension"
	"github.com/nwverse/persona/server/internal/extwire"
)

// relayingExt is the manager's extensions faked (fakeExt) that also serves what the hub relays to it, as the manager
// does: on extensions.accept it dials the relay stream and serves HTTP on it with serve.
type relayingExt struct {
	*fakeExt
	serve http.Handler
	dial  func(ctx context.Context, query string) (*websocket.Conn, error)
}

func (x *relayingExt) Request(ctx context.Context, method string, params json.RawMessage) (any, bool, error) {
	switch method {
	case extwire.MethodAccept:
		var a struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(params, &a); err != nil || a.ID == "" {
			return nil, true, errors.New("extensions.accept needs an id")
		}
		conn, err := x.dial(ctx, url.Values{"id": {a.ID}}.Encode())
		if err != nil {
			return nil, true, err
		}
		// As the manager serves the relay (extmgr Accept): over a pipe, whose deadlines the server may set.
		nc := websocket.NetConn(context.Background(), conn, websocket.MessageBinary)
		mine, theirs := net.Pipe()
		go func() {
			defer nc.Close()
			defer theirs.Close()
			done := make(chan struct{}, 2)
			go func() { _, _ = io.Copy(theirs, nc); done <- struct{}{} }()
			go func() { _, _ = io.Copy(nc, theirs); done <- struct{}{} }()
			<-done
		}()
		go func() { _ = (&http.Server{Handler: x.serve}).Serve(newOneConnListener(mine)) }()
		return map[string]any{}, true, nil
	case extwire.MethodAuthorize:
		// The manager judges /api/v1/extensions/<id>/… by its own route (extmgr authorize).
		var a extwire.Authorize
		if err := json.Unmarshal(params, &a); err != nil {
			return nil, true, err
		}
		if a.Pattern == "/api/v1/extensions/" {
			a.Pattern = "/api/v1/extensions/{id}/"
		}
		raw, _ := json.Marshal(a)
		return x.fakeExt.Request(ctx, method, raw)
	}
	return x.fakeExt.Request(ctx, method, params)
}

// oneConnListener gives one connection, then waits until that connection is closed.
type oneConnListener struct {
	mu   sync.Mutex
	c    net.Conn
	once sync.Once
	done chan struct{}
}

func newOneConnListener(c net.Conn) *oneConnListener {
	l := &oneConnListener{done: make(chan struct{})}
	l.c = &closingConn{Conn: c, closed: func() { _ = l.Close() }}
	return l
}

func (l *oneConnListener) Accept() (net.Conn, error) {
	l.mu.Lock()
	c := l.c
	l.c = nil
	l.mu.Unlock()
	if c != nil {
		return c, nil
	}
	<-l.done
	return nil, net.ErrClosed
}

func (l *oneConnListener) Close() error   { l.once.Do(func() { close(l.done) }); return nil }
func (l *oneConnListener) Addr() net.Addr { return &net.UnixAddr{Name: "relay", Net: "unix"} }

type closingConn struct {
	net.Conn
	closed func()
}

func (c *closingConn) Close() error { err := c.Conn.Close(); c.closed(); return err }

// relayedManager is what a test's fake manager saw relayed: the X-Persona-* headers of a request (its answer) and of
// a WebSocket (its first message, then it echoes); each WebSocket's end on its side is sent on ended, with its device.
type relayedManager struct {
	ended chan string
}

func personaHeaders(r *http.Request) map[string]string {
	out := map[string]string{}
	for k, v := range r.Header {
		if strings.HasPrefix(strings.ToLower(k), "x-persona-") {
			out[k] = strings.Join(v, ",")
		}
	}
	return out
}

func (m *relayedManager) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if !strings.HasSuffix(r.URL.Path, "/ws") {
		_ = json.NewEncoder(w).Encode(personaHeaders(r))
		return
	}
	c, err := websocket.Accept(w, r, nil)
	if err != nil {
		return
	}
	defer c.CloseNow()
	device := r.Header.Get(extwire.HeaderDevice)
	defer func() { m.ended <- device }()
	b, _ := json.Marshal(personaHeaders(r))
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

// relayTestHub is a hub with device tokens and pairing codes in memory, its manager connected over the bus and serving the relayed
// routes with a relayedManager; the apps reach it at the server returned.
func relayTestHub(t *testing.T) (*Hub, *relayedManager, *fakeExt, *httptest.Server) {
	t.Helper()
	h := newTestHub(t)
	h.store = &pairStore{tokenStore: tokenStore{memStore: *newMemStore(), rows: map[string]db.DeviceToken{}, rev: map[string]bool{}},
		pairs: map[string]db.PairToken{}}
	if h.busStore() == nil {
		h.bus.idents = &memIdents{}
	}
	busMux := http.NewServeMux()
	h.busRoutes(busMux)
	busSrv := httptest.NewServer(busMux)
	t.Cleanup(busSrv.Close)
	m := &relayedManager{ended: make(chan string, 8)}
	var token string
	x := &relayingExt{fakeExt: fakeExtOf(h), serve: m, dial: func(ctx context.Context, query string) (*websocket.Conn, error) {
		return bus.DialAt(ctx, busSrv.URL, token, busSrv.Client(), devicewire.RelayPath, query)
	}}
	a := dialAssistant(t, h, busSrv.URL, false, func(o *assistant.BusOptions) { o.Extensions = x })
	token = a.token
	srv := httptest.NewServer(h.routes())
	t.Cleanup(srv.Close)
	return h, m, x.fakeExt, srv
}

// relayDevice makes a device token of kind ("phone": full, "watch": answer-only) with by's credential.
func relayDevice(t *testing.T, srv *httptest.Server, by, name, kind string) (id, token string) {
	t.Helper()
	code, out := call(t, srv.Config.Handler, "POST", "/api/v1/auth/tokens", by, `{"name":"`+name+`","kind":"`+kind+`"}`)
	if code != 200 {
		t.Fatalf("token for %s: %d %v", name, code, out)
	}
	return out["id"].(string), out["token"].(string)
}

func wsURL(srv *httptest.Server, path string) string {
	return "ws" + strings.TrimPrefix(srv.URL, "http") + path
}

// What the hub relays to the manager for an extension's own HTTP says who asked, as the hub's auth found it, on the
// request and the WebSocket alike, whatever the client sent under X-Persona-*.
func TestRelayedCallerHeaders(t *testing.T) {
	h, _, fx, srv := relayTestHub(t)
	master := h.cfg.Token
	phoneID, phone := relayDevice(t, srv, master, "iPhone", "phone")
	ext := fx.issue("caller", "Caller", []string{extension.ScopeCallPrefix + "callee"})
	forged := http.Header{"X-Persona-Device": {"someone-else"}, "X-Persona-Role": {"hub"}, "X-Persona-Owner": {"1"},
		"X-Persona-Admin": {"1"}, "X-Persona-Scopes": {"kb.read"}, "X-Persona-Forged": {"1"}}
	get := func(token, lang string) map[string]string {
		t.Helper()
		req, _ := http.NewRequest("GET", srv.URL+"/api/v1/extensions/callee/echo", nil)
		for k, v := range forged {
			req.Header[k] = v
		}
		req.Header.Set("Authorization", "Bearer "+token)
		if lang != "" {
			req.Header.Set("Accept-Language", lang)
		}
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		out := map[string]string{}
		_ = json.NewDecoder(res.Body).Decode(&out)
		if res.StatusCode != 200 {
			t.Fatalf("%d %v", res.StatusCode, out)
		}
		return out
	}
	for name, c := range map[string]struct {
		got  map[string]string
		want map[string]string
	}{
		"a full device": {get(phone, "en-US"), map[string]string{"X-Persona-Device": phoneID, "X-Persona-Device-Name": "iPhone",
			"X-Persona-Role": roleFull, "X-Persona-Lang": "en", "X-Persona-Remote": "127.0.0.1"}},
		"another extension's token": {get(ext, "ja"), map[string]string{"X-Persona-Device": "extension:caller",
			"X-Persona-Device-Name": "Caller", "X-Persona-Role": roleExtension, "X-Persona-Extension": "caller",
			"X-Persona-Scopes": extension.ScopeCallPrefix + "callee", "X-Persona-Lang": "ja", "X-Persona-Remote": "127.0.0.1"}},
		// No device: the master token, a full caller (the manager passes Device / Role / Lang on to the extension as is).
		"the master token": {get(master, "zh-CN"), map[string]string{"X-Persona-Role": roleFull, "X-Persona-Lang": "zh-Hans",
			"X-Persona-Remote": "127.0.0.1", "X-Persona-Owner": "1"}},
	} {
		if len(c.got) != len(c.want) {
			t.Fatalf("%s: %v, want %v", name, c.got, c.want)
		}
		for k, v := range c.want {
			if c.got[k] != v {
				t.Fatalf("%s: %s = %q, want %q (%v)", name, k, c.got[k], v, c.got)
			}
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	hd := http.Header{"Authorization": {"Bearer " + phone}, "Accept-Language": {"ja"}}
	for k, v := range forged {
		hd[k] = v
	}
	c, _, err := websocket.Dial(ctx, wsURL(srv, "/api/v1/extensions/callee/ws"), &websocket.DialOptions{HTTPHeader: hd})
	if err != nil {
		t.Fatal(err)
	}
	defer c.CloseNow()
	_, b, err := c.Read(ctx)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	_ = json.Unmarshal(b, &got)
	if got["X-Persona-Device"] != phoneID || got["X-Persona-Role"] != roleFull || got["X-Persona-Lang"] != "ja" ||
		got["X-Persona-Owner"] != "" || got["X-Persona-Admin"] != "" || got["X-Persona-Forged"] != "" {
		t.Fatalf("WebSocket: %v", got)
	}
}

// Revoking a device ends the WebSockets it has open through the manager to the extensions, both sides, at once; another
// device's and the master token's stay.
func TestRevokingEndsRelayedSockets(t *testing.T) {
	h, m, _, srv := relayTestHub(t)
	master := h.cfg.Token
	phoneID, phone := relayDevice(t, srv, master, "iPhone", "phone")
	_, mac := relayDevice(t, srv, master, "Mac", "mac")
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	open := func(token string) *websocket.Conn {
		t.Helper()
		c, _, err := websocket.Dial(ctx, wsURL(srv, "/api/v1/extensions/callee/ws"),
			&websocket.DialOptions{HTTPHeader: http.Header{"Authorization": {"Bearer " + token}}})
		if err != nil {
			t.Fatal(err)
		}
		if _, _, err := c.Read(ctx); err != nil { // who asked
			t.Fatal(err)
		}
		return c
	}
	roundTrip := func(c *websocket.Conn) error {
		if err := c.Write(ctx, websocket.MessageText, []byte(`{"type":"ping"}`)); err != nil {
			return err
		}
		_, b, err := c.Read(ctx)
		if err == nil && string(b) != `{"type":"ping"}` {
			t.Fatalf("echo: %q", b)
		}
		return err
	}
	phoneWS, macWS, masterWS := open(phone), open(mac), open(master)
	defer phoneWS.CloseNow()
	defer macWS.CloseNow()
	defer masterWS.CloseNow()
	for _, c := range []*websocket.Conn{phoneWS, macWS, masterWS} {
		if err := roundTrip(c); err != nil {
			t.Fatal(err)
		}
	}

	if code, out := call(t, srv.Config.Handler, "DELETE", "/api/v1/auth/tokens/"+phoneID, master, ""); code != 200 {
		t.Fatalf("revoke: %d %v", code, out)
	}
	readCtx, readCancel := context.WithTimeout(ctx, 5*time.Second)
	defer readCancel()
	if _, _, err := phoneWS.Read(readCtx); err == nil || readCtx.Err() != nil {
		t.Fatalf("the revoked phone's WebSocket is still open: %v", err)
	}
	select {
	case device := <-m.ended:
		if device != phoneID {
			t.Fatalf("ended on the manager's side: %q, want the phone's", device)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the manager's side of the revoked phone's WebSocket is still open")
	}
	for name, c := range map[string]*websocket.Conn{"mac": macWS, "master": masterWS} {
		if err := roundTrip(c); err != nil {
			t.Fatalf("%s's WebSocket after the phone's revocation: %v", name, err)
		}
	}
	h.ext.calls.mu.Lock()
	kept := len(h.ext.calls.byID[phoneID])
	h.ext.calls.mu.Unlock()
	if kept != 0 {
		t.Fatalf("still kept for the revoked phone: %d", kept)
	}
}

// An extension's WebSocket to another extension's routes ends when its manager drops the token (the extension stops,
// is switched off, upgraded or uninstalled, or gets its next token) and tells the hub, as revoking a device ends the
// device's. Another extension's stays open, and so does the token's own when another manager names its hash.
func TestDroppedExtensionTokenEndsItsRelayedSockets(t *testing.T) {
	h, m, fx, srv := relayTestHub(t)
	caller := fx.issue("caller", "Caller", []string{extension.ScopeCallPrefix + "callee"})
	other := fx.issue("other", "Other", []string{extension.ScopeCallPrefix + "callee"})
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	open := func(token string) *websocket.Conn {
		t.Helper()
		c, _, err := websocket.Dial(ctx, wsURL(srv, "/api/v1/extensions/callee/ws"),
			&websocket.DialOptions{HTTPHeader: http.Header{"Authorization": {"Bearer " + token}}})
		if err != nil {
			t.Fatal(err)
		}
		if _, _, err := c.Read(ctx); err != nil { // who asked
			t.Fatal(err)
		}
		return c
	}
	roundTrip := func(c *websocket.Conn) error {
		if err := c.Write(ctx, websocket.MessageText, []byte(`{"type":"ping"}`)); err != nil {
			return err
		}
		_, b, err := c.Read(ctx)
		if err == nil && string(b) != `{"type":"ping"}` {
			t.Fatalf("echo: %q", b)
		}
		return err
	}
	dropped := func(from, tok string) {
		t.Helper()
		raw, _ := json.Marshal(map[string]string{"hash": hashToken(tok)})
		if _, ok, err := h.extAppCall(ctx, from, assistantwire.AppCall{AppID: extwire.AppHub, Method: extwire.HubTokenRevoked,
			Params: raw}); !ok || err != nil {
			t.Fatalf("%s: handled %v, %v", extwire.HubTokenRevoked, ok, err)
		}
	}
	callerWS, otherWS := open(caller), open(other)
	defer callerWS.CloseNow()
	defer otherWS.CloseNow()
	for _, c := range []*websocket.Conn{callerWS, otherWS} {
		if err := roundTrip(c); err != nil {
			t.Fatal(err)
		}
	}

	dropped("another-manager", caller)
	raw, _ := json.Marshal(map[string]string{"hash": "x:" + hashToken(caller)})
	if _, _, err := h.extAppCall(ctx, "another-manager", assistantwire.AppCall{AppID: extwire.AppHub,
		Method: extwire.HubTokenRevoked, Params: raw}); err == nil {
		t.Fatal("a notice naming no token's hash was taken")
	}
	if err := roundTrip(callerWS); err != nil {
		t.Fatalf("ended by another manager's notice: %v", err)
	}
	fx.mu.Lock()
	delete(fx.tokens, hashToken(caller))
	fx.mu.Unlock()
	dropped(assistantwire.AssistantID, caller)
	readCtx, readCancel := context.WithTimeout(ctx, 5*time.Second)
	defer readCancel()
	if _, _, err := callerWS.Read(readCtx); err == nil || readCtx.Err() != nil {
		t.Fatalf("the dropped token's WebSocket is still open: %v", err)
	}
	select {
	case device := <-m.ended:
		if device != "extension:caller" {
			t.Fatalf("ended on the manager's side: %q, want the caller's", device)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the manager's side of the dropped token's WebSocket is still open")
	}
	if err := roundTrip(otherWS); err != nil {
		t.Fatalf("the other extension's WebSocket after the caller's token was dropped: %v", err)
	}
	h.ext.calls.mu.Lock()
	kept := 0
	for id, m := range h.ext.calls.byID {
		if strings.HasSuffix(id, ":"+hashToken(caller)) {
			kept += len(m)
		}
	}
	h.ext.calls.mu.Unlock()
	if kept != 0 {
		t.Fatalf("still kept for the dropped token: %d", kept)
	}
}

// A call let in with an extension's token just before its manager dropped it, and kept only after the hub was told,
// ends by itself; the extension's next token's does not. One let in longer ago than a drop is remembered ends too.
func TestRelayedCallOfADroppedTokenBeforeTracked(t *testing.T) {
	h := newTestHub(t)
	asked := time.Now()
	asAt := func(hash string, asked time.Time) *http.Request {
		r := httptest.NewRequest("GET", "/api/v1/extensions/x/ws", nil)
		return r.WithContext(context.WithValue(r.Context(), principalKey{}, principal{ID: "extension:caller",
			Role: roleExtension, Extension: "caller", Manager: assistantwire.AssistantID, extHash: hash, extAt: asked}))
	}
	as := func(hash string) *http.Request { return asAt(hash, asked) }
	if n := h.dropRevokedToken(assistantwire.AssistantID, "hash-gone"); n != 0 {
		t.Fatalf("ended %d with nothing kept", n)
	}
	r, done := h.trackRelayed(as("hash-gone"))
	if r.Context().Err() == nil {
		t.Fatal("a dropped token's call goes on")
	}
	done()
	r, done = h.trackRelayed(as("hash-next"))
	if r.Context().Err() != nil {
		t.Fatal("the extension's next token's call was ended")
	}
	if n := h.dropRevokedToken("another-manager", "hash-next"); n != 0 || r.Context().Err() != nil {
		t.Fatalf("another manager's notice ended %d: %v", n, r.Context().Err())
	}
	if n := h.dropRevokedToken(assistantwire.AssistantID, "hash-next"); n != 1 || r.Context().Err() == nil {
		t.Fatalf("dropped %d, err %v", n, r.Context().Err())
	}
	done()
	r, done = h.trackRelayed(asAt("hash-old", time.Now().Add(-tokenRevokedFor)))
	if r.Context().Err() == nil {
		t.Fatal("a call let in longer ago than a drop is remembered goes on")
	}
	done()
}

// A token the manager drops while the hub is still asking it whose the token is: the call its answer lets in ends as it
// starts (the hub keeps when it asked, not when it was answered).
func TestTokenDroppedWhileAskedEndsItsCall(t *testing.T) {
	h := newTestHub(t)
	fx := extTokensOf(t, h)
	tok := fx.issue("caller", "Caller", []string{extension.ScopeCallPrefix + "callee"})
	fx.mu.Lock()
	fx.authorizing = func(hash string) { h.dropRevokedToken(assistantwire.AssistantID, hash) }
	fx.mu.Unlock()
	r := httptest.NewRequest("GET", "/api/v1/extensions/callee/ws", nil)
	p, ok, _, _, _ := h.authorizeExtension(context.Background(), tok, r)
	if !ok || p.Manager != assistantwire.AssistantID {
		t.Fatalf("authorized %v by %q", ok, p.Manager)
	}
	r, done := h.trackRelayed(r.WithContext(context.WithValue(r.Context(), principalKey{}, p)))
	defer done()
	if r.Context().Err() == nil {
		t.Fatal("the call of a token dropped while it was asked goes on")
	}
}

// A manager off the bus cannot tell the hub of a token it drops meanwhile: each of its connections ending, a replaced
// one too, ends what its extensions' tokens relay, and what one let in before would start relaying. Another manager's
// (one whose name begins with it too), a device's, and a call let in after it came back go on.
func TestManagerLeavingTheBusEndsItsTokensRelayedCalls(t *testing.T) {
	h := newTestHub(t)
	as := func(p principal) *http.Request {
		r := httptest.NewRequest("GET", "/api/v1/extensions/x/ws", nil)
		return r.WithContext(context.WithValue(r.Context(), principalKey{}, p))
	}
	ext := func(manager, hash string, asked time.Time) principal {
		return principal{ID: "extension:caller", Role: roleExtension, Extension: "caller", Manager: manager, extHash: hash,
			extAt: asked}
	}
	asked := time.Now()
	mine, done := h.trackRelayed(as(ext("m1", "aa", asked)))
	defer done()
	goesOn := map[string]*http.Request{}
	for name, p := range map[string]principal{"manager m1:x's": ext("m1:x", "bb", asked),
		"another manager's": ext("m2", "cc", asked), "a device's": {ID: "dev-1", Role: roleFull}} {
		r, done := h.trackRelayed(as(p))
		defer done()
		goesOn[name] = r
	}

	h.busEnded(&busConn{ident: db.BusIdentity{ID: "m1", Role: bus.RoleManager}, replaced: true})
	if mine.Context().Err() == nil {
		t.Fatal("the manager's token's call goes on after it left the bus")
	}
	for name, r := range goesOn {
		if r.Context().Err() != nil {
			t.Fatalf("%s call was ended", name)
		}
	}
	late, done := h.trackRelayed(as(ext("m1", "dd", asked)))
	defer done()
	if late.Context().Err() == nil {
		t.Fatal("a call let in before the manager left goes on once kept")
	}
	h.ext.calls.mu.Lock()
	left := h.ext.calls.left["m1"]
	h.ext.calls.mu.Unlock()
	next, done := h.trackRelayed(as(ext("m1", "dd", left.Add(time.Nanosecond))))
	defer done()
	if next.Context().Err() != nil {
		t.Fatal("a call let in after the manager came back was ended")
	}
}

// What the hub remembers of dropped tokens stays bounded however many notices a manager sends: its newest
// relayDroppedMost at most, without forgetting another manager's, none past tokenRevokedFor, and none once it left the
// bus (that fences them all). Of managers that left, none past tokenRevokedFor.
func TestRelayDroppedStaysBounded(t *testing.T) {
	h := newTestHub(t)
	calls := &h.ext.calls
	kept := func(manager, hash string) (n int, ok bool) {
		t.Helper()
		calls.mu.Lock()
		defer calls.mu.Unlock()
		d := calls.dropped[manager]
		if d == nil {
			return 0, false
		}
		if len(d.at) != len(d.order) {
			t.Fatalf("%s: %d remembered, %d in order", manager, len(d.at), len(d.order))
		}
		_, ok = d.at[hash]
		return len(d.order), ok
	}
	hash := func(i int) string { return fmt.Sprintf("%064x", i) }
	h.dropRevokedToken("m2", hash(0))
	for i := range relayDroppedMost + 100 {
		h.dropRevokedToken("m1", hash(i))
	}
	h.dropRevokedToken("m1", hash(relayDroppedMost+99)) // told again
	if n, ok := kept("m1", hash(relayDroppedMost+99)); n != relayDroppedMost || !ok {
		t.Fatalf("kept %d (the newest: %v), want %d", n, ok, relayDroppedMost)
	}
	if _, ok := kept("m1", hash(99)); ok {
		t.Fatal("the oldest is kept past the bound")
	}
	if _, ok := kept("m1", hash(100)); !ok {
		t.Fatal("more than the oldest forgotten")
	}
	if n, ok := kept("m2", hash(0)); n != 1 || !ok {
		t.Fatalf("another manager's notices forgot its own: %d kept", n)
	}

	calls.mu.Lock()
	for _, d := range calls.dropped {
		for k := range d.at {
			d.at[k] = time.Now().Add(-tokenRevokedFor)
		}
	}
	calls.mu.Unlock()
	h.dropRevokedToken("m2", hash(1))
	if n, ok := kept("m2", hash(1)); n != 1 || !ok {
		t.Fatalf("kept %d past tokenRevokedFor, want only the newest", n)
	}
	h.dropLeftManager("m1")
	if n, _ := kept("m1", ""); n != 0 {
		t.Fatalf("kept %d of a manager's dropped tokens once it left the bus", n)
	}

	calls.mu.Lock()
	calls.left["m1"] = time.Now().Add(-tokenRevokedFor)
	calls.mu.Unlock()
	h.dropLeftManager("m3")
	calls.mu.Lock()
	_, m1 := calls.left["m1"]
	n := len(calls.left)
	calls.mu.Unlock()
	if m1 || n != 1 {
		t.Fatalf("kept %d managers that left (the one past tokenRevokedFor: %v), want only the one that just did", n, m1)
	}
}

// A WebSocket opened with a pairing code through the manager ends when the code does (here its expiry; revoking the
// device that redeemed it expires it too), as the code's own /api/v1/ws does: it is not a device revoked() is told of.
func TestRelayedPairingCallEndsWithTheCode(t *testing.T) {
	h, m, _, srv := relayTestHub(t)
	tok := "jp_" + strings.Repeat("ab", 32)
	device := "pair_" + hashToken(tok)
	if err := h.store.CreatePairToken(context.Background(), hashToken(tok), time.Now().Add(1500*time.Millisecond), ""); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	c, _, err := websocket.Dial(ctx, wsURL(srv, "/api/v1/extensions/callee/ws"),
		&websocket.DialOptions{HTTPHeader: http.Header{"Authorization": {"Bearer " + tok}}})
	if err != nil {
		t.Fatal(err)
	}
	defer c.CloseNow()
	_, b, err := c.Read(ctx)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	_ = json.Unmarshal(b, &got)
	if got["X-Persona-Device"] != device || got["X-Persona-Role"] != roleFull || got["X-Persona-Paired"] != "1" {
		t.Fatalf("who asked: %v", got)
	}
	readCtx, readCancel := context.WithTimeout(ctx, 6*time.Second)
	defer readCancel()
	if _, _, err := c.Read(readCtx); err == nil || readCtx.Err() != nil {
		t.Fatalf("the WebSocket outlived its pairing code: %v", err)
	}
	select {
	case ended := <-m.ended:
		if ended != device {
			t.Fatalf("ended on the manager's side: %q, want the pairing code's", ended)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the manager's side outlived the pairing code")
	}
}

// A client cannot drop the hub's headers by naming them in Connection (a hop-by-hop header the proxy removes): a
// pairing code stays paired, an extension's token stays the extension's.
func TestRelayedCallerHeadersSurviveConnection(t *testing.T) {
	h, _, fx, srv := relayTestHub(t)
	tok := "jp_" + strings.Repeat("cd", 32)
	if err := h.store.CreatePairToken(context.Background(), hashToken(tok), time.Now().Add(time.Minute), ""); err != nil {
		t.Fatal(err)
	}
	ext := fx.issue("caller", "Caller", []string{extension.ScopeCallPrefix + "callee"})
	get := func(token string) map[string]string {
		t.Helper()
		req, _ := http.NewRequest("GET", srv.URL+"/api/v1/extensions/callee/echo", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Connection", "X-Persona-Paired, X-Persona-Role, X-Persona-Device, X-Persona-Extension")
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		out := map[string]string{}
		_ = json.NewDecoder(res.Body).Decode(&out)
		if res.StatusCode != 200 {
			t.Fatalf("%d %v", res.StatusCode, out)
		}
		return out
	}
	if got := get(tok); got["X-Persona-Paired"] != "1" || got["X-Persona-Role"] != roleFull || got["X-Persona-Device"] != "pair_"+hashToken(tok) {
		t.Fatalf("pairing code: %v", got)
	}
	if got := get(ext); got["X-Persona-Role"] != roleExtension || got["X-Persona-Extension"] != "caller" {
		t.Fatalf("extension token: %v", got)
	}
}

// A request that passed the auth just before its device was revoked ends as it starts; the master token's are not
// kept, a device's are until done.
func TestRelayedCallRevokedBeforeTracked(t *testing.T) {
	h := newTestHub(t)
	as := func(id string) *http.Request {
		r := httptest.NewRequest("GET", "/api/v1/extensions/x/ws", nil)
		return r.WithContext(context.WithValue(r.Context(), principalKey{}, principal{ID: id, Role: roleFull}))
	}
	h.revoked("dev_gone")
	r, done := h.trackRelayed(as("dev_gone"))
	if r.Context().Err() == nil {
		t.Fatal("a revoked device's call goes on")
	}
	done()
	r, done = h.trackRelayed(httptest.NewRequest("GET", "/api/v1/extensions/x/ws", nil))
	done()
	if r.Context().Err() != nil {
		t.Fatal("the master token's request was ended")
	}
	r, done = h.trackRelayed(as("dev_live"))
	if n := h.dropRelayed("dev_live"); n != 1 || r.Context().Err() == nil {
		t.Fatalf("dropped %d, err %v", n, r.Context().Err())
	}
	done()
	h.ext.calls.mu.Lock()
	left := len(h.ext.calls.byID)
	h.ext.calls.mu.Unlock()
	if left != 0 {
		t.Fatalf("left kept: %d", left)
	}
}
