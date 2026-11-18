package rpc

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"testing"
)

// socketReads hands data on in reads of at most 32 KiB, as a socket or the bus's WebSocket does: Serve never sees a
// long line in one read.
type socketReads struct{ data []byte }

func (s *socketReads) Read(p []byte) (int, error) {
	if len(s.data) == 0 {
		return 0, io.EOF
	}
	n := copy(p[:min(len(p), 32<<10)], s.data)
	s.data = s.data[n:]
	return n, nil
}

// uploadLine is the line of a request or of a reply whose params or result carry a body of size bytes in base64 (an
// http.request's, an HTTPResponse's): {"id":"h1","method":"http.request","params":{…,"body":"…"}} and its newline.
func uploadLine(tb testing.TB, reply bool, size int) []byte {
	body := make([]byte, size)
	for i := range body {
		body[i] = byte(i*7 + i>>8)
	}
	value, err := Marshal(uploadRequest{Method: "POST", Path: "/api/v1/assistant/conversations", Body: body})
	if err != nil {
		tb.Fatal(err)
	}
	m := message{ID: "h1", Method: "http.request", Params: value}
	if reply {
		m = message{ID: "h1", Result: value}
	}
	line, err := encodeLine(m)
	if err != nil {
		tb.Fatal(err)
	}
	return line
}

// BenchmarkServeLarge is what Serve does with one line that carries a 22.4 MB body (29.8 MB of base64): a request
// handed to its handler (an upload relayed inline, to a manager that takes no body stream) and a reply handed to its
// call (a large answer). B/op is every copy of the line on the way.
func BenchmarkServeLarge(b *testing.B) {
	const size = 22_369_743
	for _, kind := range []string{"request", "reply"} {
		b.Run("kind="+kind, func(b *testing.B) {
			line := uploadLine(b, kind == "reply", size)
			want := base64.StdEncoding.EncodedLen(size)
			b.SetBytes(int64(len(line)))
			b.ReportAllocs()
			for b.Loop() {
				p := New("h", io.Discard)
				got := make(chan int, 1)
				p.OnRequest = func(_ context.Context, _ string, params json.RawMessage) (any, error) {
					got <- len(params)
					return nil, nil
				}
				reply := make(chan message, 1)
				p.pending["h1"] = reply
				if err := p.Serve(context.Background(), &socketReads{data: line}); err != io.EOF {
					b.Fatal(err)
				}
				n := 0
				if kind == "reply" {
					n = len((<-reply).Result)
				} else {
					n = <-got
				}
				if n < want {
					b.Fatalf("the %s carried %d bytes, want at least %d", kind, n, want)
				}
			}
		})
	}
}

// BenchmarkServeLines is Serve reading a stream of events of one size each: the lines a connection carries all day
// (answers and events of a few hundred bytes, a computer's session list of a few hundred KiB) and the sizes around the
// one from which a line is read in pieces.
func BenchmarkServeLines(b *testing.B) {
	for _, c := range []struct {
		name        string
		size, lines int
	}{{"200B", 200, 10000}, {"8KiB", 8 << 10, 500}, {"300KiB", 300 << 10, 16}, {"900KiB", 900 << 10, 6}, {"1500KiB", 1500 << 10, 4},
		{"4MiB", 4 << 20, 2}} {
		b.Run("size="+c.name, func(b *testing.B) {
			params, err := json.Marshal(map[string]string{"text": string(bytes.Repeat([]byte("x"), c.size))})
			if err != nil {
				b.Fatal(err)
			}
			line, err := encodeLine(message{Event: "session.items", Params: params})
			if err != nil {
				b.Fatal(err)
			}
			stream := bytes.Repeat(line, c.lines)
			b.SetBytes(int64(len(stream)))
			b.ReportAllocs()
			for b.Loop() {
				p := New("h", io.Discard)
				events, bytesSeen := 0, 0
				p.OnEvent = func(_ string, params json.RawMessage) {
					events++
					bytesSeen += len(params)
				}
				if err := p.Serve(context.Background(), &socketReads{data: stream}); err != io.EOF {
					b.Fatal(err)
				}
				if events != c.lines || bytesSeen != c.lines*len(params) {
					b.Fatal(fmt.Sprintf("%d events with %d bytes, want %d with %d", events, bytesSeen, c.lines, c.lines*len(params)))
				}
			}
		})
	}
}
