package hub

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/nwverse/persona/server/internal/assistantwire"
	"github.com/nwverse/persona/server/internal/db"
)

// relayStreams is a fake manager's extension that says which relay stream each request came on (X-Test-Relay):
//   - .../echo answers who asked;
//   - .../wait says on waiting that it is there, and answers who asked once the test closes release;
//   - .../slow answers its head (not with ?quiet=1), then holds until its request ends (on ended);
//   - .../events is an SSE stream: one event, then two more once the test closes more;
//   - .../ws echoes a WebSocket after a first message saying who asked and on which stream.
type relayStreams struct {
	started chan int64 // the stream a .../slow request came on, once its head was sent
	ended   chan int64 // the stream of a .../slow request whose context ended
	waiting chan int64 // the stream a .../wait request came on
	release chan struct{}
	more    chan struct{}
}

func newRelayStreams() *relayStreams {
	return &relayStreams{started: make(chan int64, 4), ended: make(chan int64, 4), waiting: make(chan int64, 4),
		release: make(chan struct{}), more: make(chan struct{})}
}

func streamOf(r *http.Request) int64 { n, _ := r.Context().Value(relayNumber{}).(int64); return n }

func (m *relayStreams) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	n := streamOf(r)
	w.Header().Set("X-Test-Relay", fmt.Sprint(n))
	switch {
	case strings.HasSuffix(r.URL.Path, "/slow"):
		if r.URL.Query().Get("quiet") == "" {
			w.WriteHeader(http.StatusOK)
			_ = http.NewResponseController(w).Flush()
		}
		m.started <- n
		<-r.Context().Done()
		m.ended <- n
	case strings.HasSuffix(r.URL.Path, "/wait"):
		m.waiting <- n
		select {
		case <-m.release:
		case <-r.Context().Done():
			return
		}
		_ = json.NewEncoder(w).Encode(personaHeaders(r))
	case strings.HasSuffix(r.URL.Path, "/events"):
		w.Header().Set("Content-Type", "text/event-stream")
		for i := range 3 {
			if i == 1 {
				select {
				case <-m.more:
				case <-r.Context().Done():
					return
				}
			}
			fmt.Fprintf(w, "data: %d\n\n", i)
			_ = http.NewResponseController(w).Flush()
		}
	case strings.HasSuffix(r.URL.Path, "/ws"):
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer c.CloseNow()
		who := personaHeaders(r)
		who["X-Test-Relay"] = fmt.Sprint(n)
		b, _ := json.Marshal(who)
		if c.Write(r.Context(), websocket.MessageText, b) != nil {
			return
		}
		for {
			typ, b, err := c.Read(r.Context())
			if err != nil || c.Write(r.Context(), typ, b) != nil {
				return
			}
		}
	default:
		_ = json.NewEncoder(w).Encode(personaHeaders(r))
	}
}

// relayGet is GET path relayed with token: its status, the stream it came on and what the manager answered.
func relayGet(t *testing.T, srv *httptest.Server, token, path string) (int, int64, map[string]string) {
	t.Helper()
	code, n, out, err := tryRelayGet(srv, token, path)
	if err != nil {
		t.Fatal(err)
	}
	return code, n, out
}

// tryRelayGet is relayGet for a goroutine of the test's own: the error instead of failing the test.
func tryRelayGet(srv *httptest.Server, token, path string) (int, int64, map[string]string, error) {
	req, _ := http.NewRequest("GET", srv.URL+path, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, 0, nil, err
	}
	defer res.Body.Close()
	out := map[string]string{}
	_ = json.NewDecoder(res.Body).Decode(&out)
	var n int64
	fmt.Sscan(res.Header.Get("X-Test-Relay"), &n)
	return res.StatusCode, n, out, nil
}

// pingRound is one round of pingRelay over one of the hub's relay streams.
type pingRound struct {
	idle, pinged bool
	err          error
}

// pingRounds has h's relay streams ping every 50 ms with a 200 ms timeout, and sends each round on the channel it
// returns. Set before any stream is dialled.
func pingRounds(h *Hub) <-chan pingRound {
	ch := make(chan pingRound, 1024)
	h.ext.relays.ping = [2]time.Duration{50 * time.Millisecond, 200 * time.Millisecond}
	h.ext.relays.rounds = func(idle, pinged bool, err error) {
		select {
		case ch <- pingRound{idle, pinged, err}:
		default: // a test that stopped reading
		}
	}
	return ch
}

// Requests relayed one after another go out on one relay stream: the manager is asked to take one once, and each
// request still says who asked, whoever asked before it on that stream.
func TestRelayedRequestsShareARelayStream(t *testing.T) {
	h, x, srv := relayTestHubVia(t, newRelayStreams(), nil)
	master := h.cfg.Token
	phoneID, phone := relayDevice(t, srv, master, "iPhone", "phone")
	for i := range 6 {
		token, device := master, ""
		if i%2 == 1 {
			token, device = phone, phoneID
		}
		code, n, who := relayGet(t, srv, token, "/api/v1/extensions/callee/echo")
		if code != http.StatusOK || n != 1 {
			t.Fatalf("request %d: %d on stream %d (%v)", i, code, n, who)
		}
		if who["X-Persona-Device"] != device || (device == "") != (who["X-Persona-Owner"] == "1") {
			t.Fatalf("request %d, device %q: %v", i, device, who)
		}
	}
	if got := x.accepted.Load(); got != 1 {
		t.Fatalf("the manager was asked to take %d relay streams, want 1", got)
	}
}

// Revoking a device ends its relayed request on a stream shared with others' requests, before its answer and in the
// middle of it: the manager's side of it ends, the stream is closed, and the next request goes out on another one.
func TestRevokingEndsARequestOnAPooledRelayStream(t *testing.T) {
	m := newRelayStreams()
	h, x, srv := relayTestHubVia(t, m, nil)
	x.ended = make(chan int64, 8)
	master := h.cfg.Token
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	for _, mid := range []bool{false, true} {
		name := map[bool]string{false: "before its answer", true: "in the middle of its answer"}[mid]
		phoneID, phone := relayDevice(t, srv, master, "iPhone", "phone")
		_, warm, _ := relayGet(t, srv, master, "/api/v1/extensions/callee/echo")
		path := "/api/v1/extensions/callee/slow"
		if !mid {
			path = "/api/v1/extensions/callee/slow?quiet=1"
		}
		ended := make(chan error, 1)
		go func() {
			req, _ := http.NewRequestWithContext(ctx, "GET", srv.URL+path, nil)
			req.Header.Set("Authorization", "Bearer "+phone)
			res, err := http.DefaultClient.Do(req)
			if err == nil {
				_, err = io.Copy(io.Discard, res.Body)
				res.Body.Close()
			}
			ended <- err
		}()
		var on int64
		select {
		case on = <-m.started:
		case <-ctx.Done():
			t.Fatalf("%s: the slow request never reached the manager", name)
		}
		if on != warm {
			t.Fatalf("%s: the phone's request came on stream %d, not on the warm %d", name, on, warm)
		}
		if code, out := call(t, srv.Config.Handler, "DELETE", "/api/v1/auth/tokens/"+phoneID, master, ""); code != 200 {
			t.Fatalf("%s: revoke: %d %v", name, code, out)
		}
		select {
		case <-ended:
		case <-time.After(5 * time.Second):
			t.Fatalf("%s: the revoked phone's request goes on", name)
		}
		for _, ch := range []chan int64{m.ended, x.ended} {
			select {
			case n := <-ch:
				if n != on {
					t.Fatalf("%s: stream %d ended, want %d", name, n, on)
				}
			case <-time.After(5 * time.Second):
				t.Fatalf("%s: the manager's side of the revoked request (stream %d) goes on", name, on)
			}
		}
		before := x.accepted.Load()
		if code, n, who := relayGet(t, srv, master, "/api/v1/extensions/callee/echo"); code != http.StatusOK || n == on ||
			who["X-Persona-Device"] != "" {
			t.Fatalf("%s: next request %d on stream %d (the revoked one's: %d): %v", name, code, n, on, who)
		}
		if x.accepted.Load() != before+1 {
			t.Fatalf("%s: the manager was asked for %d streams after the revocation, want 1", name, x.accepted.Load()-before)
		}
		h.ext.calls.mu.Lock()
		kept := len(h.ext.calls.byID[phoneID])
		h.ext.calls.mu.Unlock()
		if kept != 0 {
			t.Fatalf("%s: still kept for the revoked phone: %d", name, kept)
		}
	}
}

// The relay streams belong to the manager's bus connection: when it ends, the idle ones are closed, and the
// manager's next connection gets streams of its own.
func TestRelayStreamsEndWithTheManagersConnection(t *testing.T) {
	h, x, srv := relayTestHubVia(t, newRelayStreams(), nil)
	x.ended = make(chan int64, 8)
	master := h.cfg.Token
	if code, n, _ := relayGet(t, srv, master, "/api/v1/extensions/callee/echo"); code != http.StatusOK || n != 1 {
		t.Fatalf("%d on stream %d", code, n)
	}
	old := h.busConnOf(assistantwire.AssistantID)
	_ = old.ws.CloseNow()
	select {
	case n := <-x.ended:
		if n != 1 {
			t.Fatalf("stream %d ended, want 1", n)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the idle relay stream of the manager's ended connection is still open")
	}
	eventually(t, 10*time.Second, "the manager connects again", func() bool {
		c := h.busConnOf(assistantwire.AssistantID)
		return c != nil && c != old
	})
	for range 2 {
		if code, n, _ := relayGet(t, srv, master, "/api/v1/extensions/callee/echo"); code != http.StatusOK || n != 2 {
			t.Fatalf("after the manager reconnected: %d on stream %d, want 2", code, n)
		}
	}
}

// A manager's pool is its current bus connection's: another connection (one that replaced it, or came after it)
// gets a new one, and the old one keeps nothing idle. One connection's end retires its own pool only.
func TestRelayPoolFollowsTheManagersConnection(t *testing.T) {
	h := newTestHub(t)
	const id = "m1"
	c1, c2 := &busConn{ident: db.BusIdentity{ID: id}}, &busConn{ident: db.BusIdentity{ID: id}}
	setCurrent := func(c *busConn) {
		h.bus.mu.Lock()
		defer h.bus.mu.Unlock()
		if h.bus.current == nil {
			h.bus.current = map[string]*busConn{}
		}
		if c == nil {
			delete(h.bus.current, id)
		} else {
			h.bus.current[id] = c
		}
	}
	setCurrent(c1)
	p1 := h.relayPool(id)
	if p1.bus != c1 || p1.retired.Load() || h.relayPool(id) != p1 {
		t.Fatal("the current connection's pool is not kept")
	}
	setCurrent(c2) // replaced: before c1's end is seen
	p2 := h.relayPool(id)
	if p2 == p1 || p2.bus != c2 || !p1.retired.Load() || p2.retired.Load() {
		t.Fatalf("after the manager's connection was replaced: a new pool %v, the old one retired %v", p2 != p1, p1.retired.Load())
	}
	h.retireRelays(id, c1) // the replaced one's end
	if p2.retired.Load() || h.relayPool(id) != p2 {
		t.Fatal("the replaced connection's end retired its successor's pool")
	}
	h.retireRelays(id, c2)
	if !p2.retired.Load() {
		t.Fatal("the connection's end did not retire its pool")
	}
	setCurrent(nil)
	if p := h.relayPool(id); p.bus != nil || !p.retired.Load() || len(h.ext.relays.by) != 0 {
		t.Fatal("a pool is kept for a manager off the bus")
	}
}

// A relayed WebSocket and an SSE stream each keep a relay stream to themselves: requests while they are open go out
// on others. A WebSocket always gets a new stream (net/http keeps upgrades apart), closed with it and never pooled;
// the SSE stream's is reused once its answer ended.
func TestRelayedSocketsAndStreamsKeepTheirOwnStream(t *testing.T) {
	m := newRelayStreams()
	h, x, srv := relayTestHubVia(t, m, nil)
	x.ended = make(chan int64, 8)
	master := h.cfg.Token
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	get := func() int64 {
		t.Helper()
		code, n, _ := relayGet(t, srv, master, "/api/v1/extensions/callee/echo")
		if code != http.StatusOK {
			t.Fatalf("GET: %d", code)
		}
		return n
	}
	warm := get()
	c, _, err := websocket.Dial(ctx, wsURL(srv, "/api/v1/extensions/callee/ws"),
		&websocket.DialOptions{HTTPHeader: http.Header{"Authorization": {"Bearer " + master}}})
	if err != nil {
		t.Fatal(err)
	}
	defer c.CloseNow()
	_, b, err := c.Read(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var who map[string]string
	_ = json.Unmarshal(b, &who)
	var ws int64
	fmt.Sscan(who["X-Test-Relay"], &ws)
	if ws == warm || ws == 0 {
		t.Fatalf("the WebSocket came on stream %d, the idle one's %d", ws, warm)
	}
	for range 2 {
		if n := get(); n != warm {
			t.Fatalf("a request while the WebSocket is open went out on stream %d, not on the idle %d (the WebSocket's %d)", n, warm, ws)
		}
	}
	if err := c.Write(ctx, websocket.MessageText, []byte("hi")); err != nil {
		t.Fatal(err)
	}
	if _, b, err := c.Read(ctx); err != nil || string(b) != "hi" {
		t.Fatalf("echo: %q %v", b, err)
	}
	_ = c.Close(websocket.StatusNormalClosure, "")
	select {
	case n := <-x.ended:
		if n != ws {
			t.Fatalf("stream %d ended, want the WebSocket's %d", n, ws)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the WebSocket's relay stream is still open after it closed")
	}

	req, _ := http.NewRequestWithContext(ctx, "GET", srv.URL+"/api/v1/extensions/callee/events", nil)
	req.Header.Set("Authorization", "Bearer "+master)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var sse int64
	fmt.Sscan(res.Header.Get("X-Test-Relay"), &sse)
	first := make([]byte, len("data: 0\n\n"))
	if _, err := io.ReadFull(res.Body, first); err != nil || string(first) != "data: 0\n\n" {
		t.Fatalf("SSE's first event: %q %v", first, err)
	}
	during := get() // the SSE stream is open, its answer not over
	close(m.more)
	if rest, err := io.ReadAll(res.Body); err != nil || string(rest) != "data: 1\n\ndata: 2\n\n" {
		t.Fatalf("SSE's other events: %q %v", rest, err)
	}
	if during == sse {
		t.Fatalf("a request went out on the SSE stream's relay stream %d while it streamed", sse)
	}
	before := x.accepted.Load()
	streams := []int64{get(), get()}
	if x.accepted.Load() != before || !slices.Contains([]int64{sse, during}, streams[0]) {
		t.Fatalf("after the SSE stream ended: streams %v, %d new (the SSE's %d, the other %d)", streams,
			x.accepted.Load()-before, sse, during)
	}
}

// The hub's shutdown does not wait for the idle relay streams: they are closed as the managers' connections are.
func TestShutdownClosesIdleRelayStreams(t *testing.T) {
	h, _, srv := relayTestHubVia(t, newRelayStreams(), nil)
	for range 2 {
		if code, _, _ := relayGet(t, srv, h.cfg.Token, "/api/v1/extensions/callee/echo"); code != http.StatusOK {
			t.Fatalf("GET: %d", code)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	start := time.Now()
	if err := h.stopWebSockets(ctx); err != nil {
		t.Fatalf("stopping the WebSockets: %v after %s", err, time.Since(start).Round(time.Millisecond))
	}
}

// slowLink passes TCP connections to target with a delay each way, as a long link does, and can hold everything
// that is sent (a stalled tunnel) until let go.
type slowLink struct {
	ln     net.Listener
	target string
	delay  time.Duration
	mu     sync.Mutex
	open   chan struct{} // closed while the link passes what is sent; a new one while it holds it
}

// hold stops the link passing anything on, until release.
func (l *slowLink) hold() {
	l.mu.Lock()
	defer l.mu.Unlock()
	select {
	case <-l.open:
		l.open = make(chan struct{})
	default:
	}
}

func (l *slowLink) release() {
	l.mu.Lock()
	defer l.mu.Unlock()
	select {
	case <-l.open:
	default:
		close(l.open)
	}
}

func (l *slowLink) passing() <-chan struct{} {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.open
}

func newSlowLink(t *testing.T, target string, oneWay time.Duration) *slowLink {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	l := &slowLink{ln: ln, target: target, delay: oneWay, open: make(chan struct{})}
	close(l.open)
	var conns sync.WaitGroup
	t.Cleanup(func() { l.release(); _ = ln.Close(); conns.Wait() })
	go func() {
		for {
			a, err := ln.Accept()
			if err != nil {
				return
			}
			b, err := net.Dial("tcp", target)
			if err != nil {
				_ = a.Close()
				continue
			}
			conns.Add(2)
			go func() { defer conns.Done(); l.pass(b, a) }()
			go func() { defer conns.Done(); l.pass(a, b) }()
		}
	}()
	return l
}

func (l *slowLink) url() string { return "http://" + l.ln.Addr().String() }

// pass copies src to dst, each piece delay after it was read.
func (l *slowLink) pass(dst, src net.Conn) {
	type piece struct {
		at time.Time
		b  []byte
	}
	q := make(chan piece, 4096)
	go func() {
		defer close(q)
		buf := make([]byte, 32<<10)
		for {
			n, err := src.Read(buf)
			if n > 0 {
				q <- piece{time.Now().Add(l.delay), append([]byte(nil), buf[:n]...)}
			}
			if err != nil {
				return
			}
		}
	}()
	defer func() { _ = dst.Close(); _ = src.Close() }()
	for p := range q {
		if d := time.Until(p.at); d > 0 {
			time.Sleep(d) // sleep-ok: the link's one-way delay, what TestRelayedRequestsOverALongLink measures against
		}
		<-l.passing()
		if _, err := dst.Write(p.b); err != nil {
			return
		}
	}
}

// Over a link with a 140 ms round trip, a request relayed on a warm stream takes one round trip: before, every one
// took extensions.accept, the manager's WebSocket and the request, three round trips and more.
func TestRelayedRequestsOverALongLink(t *testing.T) {
	const oneWay = 70 * time.Millisecond
	var link *slowLink
	h, x, srv := relayTestHubVia(t, newRelayStreams(), func(hub string) string {
		link = newSlowLink(t, strings.TrimPrefix(hub, "http://"), oneWay)
		return link.url()
	})
	var took []time.Duration
	for range 6 {
		start := time.Now()
		if code, _, _ := relayGet(t, srv, h.cfg.Token, "/api/v1/extensions/callee/echo"); code != http.StatusOK {
			t.Fatalf("GET: %d", code)
		}
		took = append(took, time.Since(start).Round(time.Millisecond))
	}
	t.Logf("6 sequential relayed requests, %s round trip: %v; %d relay streams", 2*oneWay, took, x.accepted.Load())
	warm := slices.Clone(took[1:])
	slices.Sort(warm)
	if median := warm[len(warm)/2]; median >= 4*oneWay {
		t.Fatalf("a request on a warm relay stream took %s (median), two round trips or more: %v", median, took)
	}
}

// An idle relay stream that stops answering (a tunnel that stalled, a NAT that forgot it) is closed when its ping gets
// no pong, and the next request goes out on a new one, not on it.
func TestAQuietlyDeadRelayStreamIsNotReused(t *testing.T) {
	var link *slowLink
	h, x, srv := relayTestHubVia(t, newRelayStreams(), func(hub string) string {
		link = newSlowLink(t, strings.TrimPrefix(hub, "http://"), 0)
		return link.url()
	})
	rounds := pingRounds(h)
	if code, n, _ := relayGet(t, srv, h.cfg.Token, "/api/v1/extensions/callee/echo"); code != http.StatusOK || n != 1 {
		t.Fatalf("%d on stream %d", code, n)
	}
	link.hold()
	for failed := false; !failed; {
		select {
		case r := <-rounds:
			failed = r.pinged && r.err != nil
		case <-time.After(10 * time.Second):
			link.release()
			t.Fatal("the idle stream was not closed while its pings went unanswered")
		}
	}
	link.release()
	if code, n, _ := relayGet(t, srv, h.cfg.Token, "/api/v1/extensions/callee/echo"); code != http.StatusOK || n != 2 {
		t.Fatalf("after the link stalled: %d on stream %d (%d asked), want a new stream", code, n, x.accepted.Load())
	}
}

// A relay stream with a request on it is not pinged: a link that stalls while the manager works on the request does
// not cut it, as it did not before the streams were kept (only an idle stream is closed for a missing pong).
func TestARelayStreamWithARequestOnItIsNotPinged(t *testing.T) {
	var link *slowLink
	m := newRelayStreams()
	h, _, srv := relayTestHubVia(t, m, func(hub string) string {
		link = newSlowLink(t, strings.TrimPrefix(hub, "http://"), 0)
		return link.url()
	})
	rounds := pingRounds(h)
	if code, n, _ := relayGet(t, srv, h.cfg.Token, "/api/v1/extensions/callee/echo"); code != http.StatusOK || n != 1 {
		t.Fatalf("%d on stream %d", code, n)
	}
	type answer struct {
		code   int
		stream int64
		err    error
	}
	answered := make(chan answer, 1)
	go func() {
		code, n, _, err := tryRelayGet(srv, h.cfg.Token, "/api/v1/extensions/callee/wait")
		answered <- answer{code, n, err}
	}()
	select {
	case <-m.waiting: // the request is on the manager, its stream taken
	case <-time.After(10 * time.Second):
		t.Fatal("the request never reached the manager")
	}
	link.hold()
	// Rounds while the request is on the stream: more of them than a ping's timeout takes, with the link held.
	for busy := 0; busy < 6; {
		select {
		case r := <-rounds:
			if r.idle {
				continue // decided before the request took the stream
			}
			if r.pinged {
				link.release()
				close(m.release)
				t.Fatalf("a stream with a request on it was pinged (%v)", r.err)
			}
			busy++
		case <-time.After(10 * time.Second):
			link.release()
			close(m.release)
			t.Fatal("no ping rounds while the request was on its stream")
		}
	}
	link.release()
	close(m.release)
	if a := <-answered; a.err != nil || a.code != http.StatusOK || a.stream != 1 {
		t.Fatalf("a request through a stall: %d on stream %d, %v", a.code, a.stream, a.err)
	}
}
