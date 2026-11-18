package ext

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nwverse/persona/sdk/go/wire"
)

type rpcWriter func([]byte) (int, error)

func (w rpcWriter) Write(b []byte) (int, error) { return w(b) }

type rpcJSON string

func (s rpcJSON) MarshalJSON() ([]byte, error) { return []byte(s), nil }

type rpcBadJSON struct{}

func (rpcBadJSON) MarshalJSON() ([]byte, error) { return nil, errors.New("cannot encode") }

// This is the original payload and envelope encoding, including the newline.
func rpcOriginalLine(f frame) ([]byte, error) {
	b, err := json.Marshal(f)
	return append(b, '\n'), err
}

func TestRPCEncodedPayloadMatchesOriginal(t *testing.T) {
	view := rpcBenchmarkView()
	for _, payload := range []struct {
		name  string
		value any
	}{
		{"nil", nil}, {"empty_object", struct{}{}}, {"nil_slice", []string(nil)},
		{"empty_slice", []string{}}, {"empty_string", ""}, {"zero", 0},
		{"text", "<>&\u2028\u2029\x00\n\"\\\xff"},
		{"raw", json.RawMessage(" { \"text\": \"<>&\u2028\u2029\xff\" } ")},
		{"custom", rpcJSON(` { "id":"payload", "id":"last", "params":null, "result":{}, "text":"<>&" } `)},
		{"large_view", view},
	} {
		for _, name := range []string{"plain", "", "<>&\u2028\u2029\x00\n\"\\\xff"} {
			t.Run(payload.name+"/"+fmt.Sprintf("%q", name), func(t *testing.T) {
				raw, err := json.Marshal(payload.value)
				if err != nil {
					t.Fatal(err)
				}
				for _, kind := range []string{"call", "notify", "handle"} {
					t.Run(kind, func(t *testing.T) {
						var got bytes.Buffer
						p := newPeer(&got)
						want := frame{}
						switch kind {
						case "call":
							// A synchronous answer also exercises pending-call cleanup.
							p.w = rpcWriter(func(b []byte) (int, error) {
								p.pending["p1"] <- frame{}
								return got.Write(b)
							})
							err = (&Hub{p: p}).Call(context.Background(), name, payload.value, nil)
							want = frame{ID: "p1", Method: name, Params: raw}
							if len(p.pending) != 0 {
								t.Fatal("call left a pending answer")
							}
						case "notify":
							err = (&Hub{p: p}).Notify(name, payload.value)
							want = frame{Event: name, Params: raw}
						case "handle":
							p.onRequest = func(context.Context, string, json.RawMessage) (any, error) { return payload.value, nil }
							p.handle(context.Background(), frame{ID: name, Method: "ui.render"})
							want = frame{ID: name, Result: raw}
						}
						if err != nil {
							t.Fatal(err)
						}
						encoded, err := rpcOriginalLine(want)
						if err != nil || !bytes.Equal(got.Bytes(), encoded) {
							t.Fatalf("wire differs: error %v\ngot  %s\nwant %s", err, got.Bytes(), encoded)
						}
					})
				}
			})
		}
	}
}

func TestRPCEncodingFailuresAndRawFrames(t *testing.T) {
	for _, value := range []any{rpcBadJSON{}, rpcJSON(`{"unfinished":`), json.RawMessage{}, make(chan int)} {
		var got bytes.Buffer
		p := newPeer(&got)
		_, wantErr := json.Marshal(value)
		if wantErr == nil {
			t.Fatal("fixture must fail to marshal")
		}
		for _, err := range []error{p.call(context.Background(), "test", value, nil), p.notify("test", value)} {
			if err == nil || err.Error() != wantErr.Error() {
				t.Fatalf("encoding error: got %v, want %v", err, wantErr)
			}
		}
		if got.Len() != 0 || len(p.pending) != 0 {
			t.Fatal("failed payload wrote a frame or left a pending answer")
		}
		p.onRequest = func(context.Context, string, json.RawMessage) (any, error) { return value, nil }
		p.handle(context.Background(), frame{ID: "request"})
		want, _ := rpcOriginalLine(frame{ID: "request", Error: &Error{Message: wantErr.Error()}})
		if !bytes.Equal(got.Bytes(), want) {
			t.Fatalf("failed response: got %s, want %s", got.Bytes(), want)
		}
	}
	// Arbitrary raw bytes and errors retain encoding/json's validation and field order.
	for _, f := range []frame{
		{}, {Params: json.RawMessage{}}, {Result: json.RawMessage{}},
		{ID: "id", Method: "method", Event: "event", Params: json.RawMessage(` { "x":"<>&" } `), Result: json.RawMessage(`null`), Error: &Error{Message: "<>&", Data: json.RawMessage(` { "x":1 } `)}},
		{Error: &Error{Message: "error", Data: json.RawMessage(`invalid`)}},
		{Params: json.RawMessage(`invalid`)}, {Result: json.RawMessage(`invalid`)},
	} {
		var got bytes.Buffer
		want, wantErr := rpcOriginalLine(f)
		err := newPeer(&got).write(f)
		if wantErr != nil {
			if err == nil || err.Error() != wantErr.Error() || got.Len() != 0 {
				t.Fatalf("raw frame error: got %v and %q, want %v", err, got.Bytes(), wantErr)
			}
		} else if err != nil || !bytes.Equal(got.Bytes(), want) {
			t.Fatalf("raw frame: got %s (%v), want %s", got.Bytes(), err, want)
		}
	}
}

func TestRPCDepthMatchesOriginal(t *testing.T) {
	for _, depth := range []int{9999, 10000, 10001} {
		var value any = 0
		for i := 0; i < depth; i++ {
			value = []any{value}
		}
		t.Run(fmt.Sprintf("depth_%d", depth), func(t *testing.T) { testRPCDepth(t, value) })
	}
	// A large shallow array has many containers but remains within the nesting limit.
	value := make([]any, 10001)
	for i := range value {
		value[i] = struct{}{}
	}
	t.Run("shallow", func(t *testing.T) { testRPCDepth(t, value) })
}

func testRPCDepth(t *testing.T, value any) {
	t.Helper()
	raw, payloadErr := json.Marshal(value)
	for _, kind := range []string{"call", "notify", "handle"} {
		t.Run(kind, func(t *testing.T) {
			var got bytes.Buffer
			p := newPeer(&got)
			want := frame{Params: raw}
			var err error
			switch kind {
			case "call":
				want.ID, want.Method = "p1", "test"
				p.w = rpcWriter(func(b []byte) (int, error) {
					p.pending["p1"] <- frame{}
					return got.Write(b)
				})
				err = p.call(context.Background(), "test", value, nil)
			case "notify":
				want.Event = "test"
				err = p.notify("test", value)
			case "handle":
				want = frame{ID: "request", Result: raw}
				p.onRequest = func(context.Context, string, json.RawMessage) (any, error) { return value, nil }
				p.handle(context.Background(), frame{ID: "request"})
			}
			var line []byte
			wantErr := payloadErr
			if payloadErr != nil && kind == "handle" {
				line, wantErr = rpcOriginalLine(frame{ID: "request", Error: &Error{Message: payloadErr.Error()}})
			} else if payloadErr == nil {
				line, wantErr = rpcOriginalLine(want)
			}
			if wantErr != nil {
				if kind != "handle" && (err == nil || err.Error() != wantErr.Error()) {
					t.Fatalf("depth error: got %v, want %v", err, wantErr)
				}
				if got.Len() != 0 || len(p.pending) != 0 {
					t.Fatal("rejected payload wrote a frame or left a pending answer")
				}
			} else if err != nil || !bytes.Equal(got.Bytes(), line) {
				t.Fatalf("depth encoding: error %v, got %d bytes, want %d", err, got.Len(), len(line))
			}
		})
	}
}

func TestRPCConcurrentWrites(t *testing.T) {
	var got bytes.Buffer
	p := newPeer(nil)
	p.w = rpcWriter(func(b []byte) (int, error) {
		var f frame
		if err := json.Unmarshal(bytes.TrimSuffix(b, []byte{'\n'}), &f); err != nil {
			return 0, err
		}
		if f.Method != "" {
			p.mu.Lock()
			ch := p.pending[f.ID]
			p.mu.Unlock()
			ch <- frame{}
		}
		return got.Write(b)
	})
	p.onRequest = func(context.Context, string, json.RawMessage) (any, error) { return "result", nil }
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var wg sync.WaitGroup
	for i := 0; i < 30; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := p.call(ctx, "test", "params", nil); err != nil {
				t.Error(err)
			}
			if err := p.notify("test", "params"); err != nil {
				t.Error(err)
			}
			p.handle(ctx, frame{ID: "incoming"})
		}()
	}
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-ctx.Done():
		t.Fatal("concurrent writes did not finish")
	}
	if n := bytes.Count(got.Bytes(), []byte{'\n'}); n != 90 {
		t.Fatalf("got %d complete lines, want 90", n)
	}
}

func FuzzRPCNotifyMatchesOriginal(f *testing.F) {
	for _, raw := range []string{`null`, `{}`, `{"id":1,"id":2}`, ` { "text":"<>&\u2028" } `, "\"\xff\"", `invalid`} {
		f.Add("connector.changed", []byte(raw))
	}
	f.Add("<>&\u2028\u2029\xff", []byte(`{"value":true}`))
	f.Fuzz(func(t *testing.T, event string, input []byte) {
		var got bytes.Buffer
		err := newPeer(&got).notify(event, rpcJSON(input))
		raw, wantErr := json.Marshal(rpcJSON(input))
		var want []byte
		if wantErr == nil {
			want, wantErr = rpcOriginalLine(frame{Event: event, Params: raw})
		}
		if wantErr != nil {
			if err == nil || err.Error() != wantErr.Error() || got.Len() != 0 {
				t.Fatalf("error: got %v and %q, want %v", err, got.Bytes(), wantErr)
			}
		} else if err != nil || !bytes.Equal(got.Bytes(), want) {
			t.Fatalf("wire: got %q (%v), want %q", got.Bytes(), err, want)
		}
	})
}

func rpcBenchmarkView() wire.View {
	items := make([]wire.Component, 256)
	for i := range items {
		items[i] = wire.Component{Type: wire.ComponentRow, Title: fmt.Sprintf("Item %d", i), Text: strings.Repeat("An example description. ", 12)}
	}
	return wire.View{Title: "Example page", Sections: []wire.Section{{Items: items}}}
}

func BenchmarkRPCWire(b *testing.B) {
	for _, payload := range []struct {
		name  string
		value any
	}{
		{"small_request", struct {
			Contribution string `json:"contribution"`
		}{"home"}},
		{"large_view", rpcBenchmarkView()},
	} {
		b.Run(payload.name, func(b *testing.B) {
			for _, kind := range []string{"call", "notify", "handle"} {
				b.Run(kind, func(b *testing.B) {
					p := newPeer(io.Discard)
					if kind == "call" {
						p.w = rpcWriter(func(line []byte) (int, error) {
							for _, ch := range p.pending {
								ch <- frame{}
							}
							return len(line), nil
						})
					}
					p.onRequest = func(context.Context, string, json.RawMessage) (any, error) { return payload.value, nil }
					hub := &Hub{p: p}
					b.ReportAllocs()
					b.ResetTimer()
					for i := 0; i < b.N; i++ {
						var err error
						switch kind {
						case "call":
							err = hub.Call(context.Background(), "ui.render", payload.value, nil)
						case "notify":
							err = hub.Notify("connector.changed", payload.value)
						case "handle":
							p.handle(context.Background(), frame{ID: "request", Method: "ui.render"})
						}
						if err != nil {
							b.Fatal(err)
						}
					}
				})
			}
		})
	}
}
