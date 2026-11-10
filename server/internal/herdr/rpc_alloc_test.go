package herdr

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"strings"
	"testing"
)

// paneReadAnswer is what herdr answers a resting Claude's 40-line visible read with: about 3.5 KB of screen.
var paneReadAnswer = func() []byte {
	var text strings.Builder
	for i := range 40 {
		if i%4 == 0 {
			text.WriteString(`❯ ` + strings.Repeat("word ", 15) + `\n`)
		} else {
			text.WriteString(strings.Repeat("- ", 38) + `\n`)
		}
	}
	return []byte(`{"id":"persona:1","result":{"type":"pane_read","read":{"pane_id":"w1:p2","source":"visible","text":"` +
		text.String() + `"}}}` + "\n")
}()

// answerServer answers every connection's request line with answer, one connection at a time and reading into one
// buffer, so that what a measurement sees allocated is the client's, apart from accepting the connection. first, when
// not nil, gets a copy of the first request line.
func answerServer(tb testing.TB, answer []byte, first chan<- []byte) string {
	tb.Helper()
	dir, err := os.MkdirTemp("", "herdr") // short: unix socket paths are limited to ~100 bytes
	if err != nil {
		tb.Fatal(err)
	}
	tb.Cleanup(func() { os.RemoveAll(dir) })
	sock := filepath.Join(dir, "h.sock")
	ln, err := listenSocket(sock)
	if err != nil {
		tb.Fatal(err)
	}
	tb.Cleanup(func() { ln.Close() })
	go func() {
		buf := make([]byte, 64<<10)
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			n := 0
			for n < len(buf) && bytes.IndexByte(buf[:n], '\n') < 0 {
				m, err := conn.Read(buf[n:])
				n += m
				if err != nil {
					break
				}
			}
			if first != nil {
				first <- bytes.Clone(buf[:n])
				first = nil
			}
			_, _ = conn.Write(answer)
			conn.Close()
		}
	}()
	return sock
}

// BenchmarkRPCPaneRead is one pane.read over the socket, as a node makes for every resting Claude every 5 s.
func BenchmarkRPCPaneRead(b *testing.B) {
	c := &Client{Bin: "/nonexistent/herdr", Socket: answerServer(b, paneReadAnswer, nil)}
	ctx := context.Background()
	b.ReportAllocs()
	for range b.N {
		if _, err := c.PaneRead(ctx, "w1:p2", "visible", 40); err != nil {
			b.Fatal(err)
		}
	}
}

// rpcAllocLimit is what one pane.read call with a 3.5 KB answer may allocate, the stand-in server's accept included:
// 16,128 bytes measured on Linux (go1.27), 82,056 with a new 64 KiB reader and a request map per call.
const rpcAllocLimit = 16_600

// A call read herdr's answer through a new 64 KiB bufio.Reader and built its request from a map: at about 15 calls a
// minute for each resting Claude, the readers alone were about 1 MB a minute. The reader is pooled (and smaller); the
// request is the same bytes as before.
func TestRPCAllocatesLittlePerCall(t *testing.T) {
	first := make(chan []byte, 1)
	c := &Client{Bin: "/nonexistent/herdr", Socket: answerServer(t, paneReadAnswer, first)}
	ctx := context.Background()
	read := func() {
		if text, err := c.PaneRead(ctx, "w1:p2", "visible", 40); err != nil || !strings.HasPrefix(text, "❯ word") {
			t.Fatalf("pane.read: %.20q %v", text, err)
		}
	}
	read()
	want, _ := json.Marshal(map[string]any{"id": "persona:1", "method": "pane.read",
		"params": map[string]any{"pane_id": "w1:p2", "source": "visible", "lines": 40}})
	if got := <-first; !bytes.Equal(got, append(want, '\n')) {
		t.Errorf("request %s, want %s", got, want)
	}
	if runtime.GOOS != "linux" {
		t.Skip("the limit is measured on Linux's sockets")
	}
	const runs = 100
	defer runtime.GOMAXPROCS(runtime.GOMAXPROCS(1))
	defer debug.SetGCPercent(debug.SetGCPercent(-1)) // no GC empties the pool
	read()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	for range runs {
		read()
	}
	runtime.ReadMemStats(&after)
	if per := (after.TotalAlloc - before.TotalAlloc) / runs; per > rpcAllocLimit && !raceEnabled {
		t.Errorf("a pane.read allocated %d bytes, want at most %d", per, rpcAllocLimit)
	}
}

// raceEnabled: built with -race, whose instrumentation allocates and whose sync.Pool drops entries at random
// (race_test.go).
var raceEnabled bool
