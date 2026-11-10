package link

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"runtime"
	"runtime/debug"
	"strings"
	"testing"

	"github.com/coder/websocket"
)

// streamKeptLimit is what one idle stream pair (both ends) may keep after carrying node traffic both ways. Each message
// compressed on its own, a pair keeps 36-42 KiB (64-76 KiB built with -race); with context takeover each end keeps its
// flate writer and window, 1,690 KiB a pair.
const streamKeptLimit = 128 << 10

// nodeLines is node traffic as a stream carries it: JSON lines, a session list longer than one 32 KiB write among
// short answers below the compression threshold.
func nodeLines() []byte {
	var b bytes.Buffer
	b.WriteString(`{"id":1,"result":{}}` + "\n")
	b.WriteString(`{"event":"sessions.changed","params":{"sessions":[`)
	for i := range 400 {
		fmt.Fprintf(&b, `{"id":"s-%d","title":"fix the build of module %d","cwd":"/home/u/src/p%d","busy":%v},`, i, i%7, i%5, i%2 == 0)
	}
	b.WriteString("{}]}}\n")
	for i := range 20 {
		fmt.Fprintf(&b, `{"id":%d,"result":{"ok":true}}`+"\n", i+2)
	}
	return b.Bytes()
}

// streamPair is one stream as the hub (accepting) and Persona Link (dialing) open it, under the modes each end asks for.
type streamPair struct {
	hub, link *websocket.Conn
	agreed    string // Sec-WebSocket-Extensions of the handshake
}

func openStreams(t *testing.T, accept, dial websocket.CompressionMode, n int) []streamPair {
	t.Helper()
	accepted := make(chan *websocket.Conn, n)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := websocket.Accept(w, r, &websocket.AcceptOptions{CompressionMode: accept})
		if err != nil {
			return
		}
		accepted <- c
	}))
	t.Cleanup(srv.Close)
	var pairs []streamPair
	for range n {
		c, resp, err := websocket.Dial(context.Background(), "ws"+strings.TrimPrefix(srv.URL, "http"),
			&websocket.DialOptions{CompressionMode: dial})
		if err != nil {
			t.Fatal(err)
		}
		pairs = append(pairs, streamPair{hub: <-accepted, link: c, agreed: resp.Header.Get("Sec-WebSocket-Extensions")})
	}
	t.Cleanup(func() {
		for _, p := range pairs {
			p.hub.CloseNow()
			p.link.CloseNow()
		}
	})
	return pairs
}

// carry sends data each way through the pair as the streams write it (io.Copy from a socket into websocket.NetConn: a
// binary message per 32 KiB read) and returns what each end read, every message read to its end as an idle stream has.
func carry(t *testing.T, p streamPair, data []byte) (atHub, atLink []byte) {
	t.Helper()
	ctx := context.Background()
	read := func(c *websocket.Conn, into *[]byte, done chan<- error) {
		for len(*into) < len(data) {
			_, b, err := c.Read(ctx)
			if err != nil {
				done <- err
				return
			}
			*into = append(*into, b...)
		}
		done <- nil
	}
	done := make(chan error, 2)
	go read(p.hub, &atHub, done)
	go read(p.link, &atLink, done)
	for _, c := range []*websocket.Conn{p.link, p.hub} {
		nc, from := websocket.NetConn(ctx, c, websocket.MessageBinary), struct{ io.Reader }{bytes.NewReader(data)}
		if _, err := io.CopyBuffer(nc, from, make([]byte, 32<<10)); err != nil {
			t.Fatal(err)
		}
	}
	for range 2 {
		if err := <-done; err != nil {
			t.Fatal(err)
		}
	}
	return atHub, atLink
}

// liveBytes is the heap still reachable (two GCs: a sync.Pool's victims go in the second).
func liveBytes() uint64 {
	var m runtime.MemStats
	runtime.GC()
	runtime.GC()
	runtime.ReadMemStats(&m)
	return m.HeapAlloc
}

// Old ends asked for context takeover. Every pairing of old and new ends connects and carries the node's bytes
// unchanged; once either end is new, both compress each message on its own and an idle stream keeps little.
func TestStreamCompressionInterop(t *testing.T) {
	const old, pairs = websocket.CompressionContextTakeover, 16
	data := nodeLines()
	defer debug.SetGCPercent(debug.SetGCPercent(-1)) // only the measurement's GCs
	for _, c := range []struct {
		name         string
		accept, dial websocket.CompressionMode
		agreed       string
	}{
		{"old hub, old link", old, old, "permessage-deflate"},
		{"new hub, old link", StreamCompression, old, "permessage-deflate; client_no_context_takeover; server_no_context_takeover"},
		{"old hub, new link", old, StreamCompression, "permessage-deflate; client_no_context_takeover; server_no_context_takeover"},
		{"new hub, new link", StreamCompression, StreamCompression, "permessage-deflate; client_no_context_takeover; server_no_context_takeover"},
	} {
		t.Run(c.name, func(t *testing.T) {
			before := liveBytes()
			ps := openStreams(t, c.accept, c.dial, pairs)
			for _, p := range ps {
				if p.agreed != c.agreed {
					t.Fatalf("agreed %q, want %q", p.agreed, c.agreed)
				}
				if atHub, atLink := carry(t, p, data); !bytes.Equal(atHub, data) || !bytes.Equal(atLink, data) {
					t.Fatalf("the stream changed the bytes: %d and %d of %d read back equal", len(atHub), len(atLink), len(data))
				}
			}
			kept := (int64(liveBytes()) - int64(before)) / pairs
			runtime.KeepAlive(ps)
			t.Logf("%s: %d KiB kept per idle pair", c.name, kept>>10)
			if takeover := c.agreed == "permessage-deflate"; takeover != (kept > streamKeptLimit) {
				t.Errorf("an idle pair kept %d KiB (context takeover %v), the limit is %d KiB", kept>>10, takeover, streamKeptLimit>>10)
			}
		})
	}
}
