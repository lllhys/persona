package rpc

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"reflect"
	"runtime"
	"runtime/debug"
	"strings"
	"testing"
	"time"
)

// ownedIsUnmarshal checks unmarshalOwned against json.Unmarshal for one line: when it reads the line, json.Unmarshal
// reads it too, into the same message, and a value that is half the line or more is the line's own bytes, a smaller
// one a copy; when it does not, it leaves the message alone. It says whether it read the line.
func ownedIsUnmarshal(t *testing.T, line []byte) bool {
	t.Helper()
	kept := bytes.Clone(line)
	var want, got message
	werr := json.Unmarshal(line, &want)
	ok := unmarshalOwned(line, &got)
	if !bytes.Equal(line, kept) {
		t.Fatalf("%q: reading the line changed it", kept)
	}
	if !ok {
		if !reflect.DeepEqual(got, message{}) {
			t.Fatalf("%q: not read, but the message is %+v", line, got)
		}
		return false
	}
	if werr != nil {
		t.Fatalf("%q: read as %+v, but json.Unmarshal says %v", line, got, werr)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("%q:\nread as          %+v\njson.Unmarshal's %+v", line, got, want)
	}
	for _, value := range []json.RawMessage{got.Params, got.Result} {
		if value == nil {
			continue
		}
		at := partOf(line, value)
		if most := 2*len(value) >= len(line); most != (at >= 0) {
			t.Fatalf("%q: the value %q (most of the line: %v) is a part of the line: %v", line, value, most, at >= 0)
		}
		if at >= 0 && cap(value) != len(value) {
			t.Fatalf("%q: the value %q has %d bytes of the line behind it", line, value, cap(value)-len(value))
		}
	}
	return true
}

// partOf is where in line value's own bytes are (the same memory, not equal bytes), -1 when they are not line's.
func partOf(line, value []byte) int {
	for at := range line {
		if &line[at] == &value[0] {
			return at
		}
	}
	return -1
}

// Lines of every kind Serve takes read the same either way, and the ones the peers write are read without a copy:
// requests, replies, errors and events, null and missing values, a key twice or in other letters (json takes those
// for the same field), whitespace, escapes, bytes that are not UTF-8, and lines that are not a message at all.
func TestUnmarshalOwnedIsUnmarshal(t *testing.T) {
	long := `{"method":"POST","body":"` + strings.Repeat("QUJD", 100) + `","more":[1,{"a":null}]}` // most of its line
	for _, c := range []struct {
		line string
		read bool
	}{
		{`{"id":"h1","method":"http.request","params":` + long + `}`, true},
		{`{"id":"h1","result":` + long + `}`, true},
		{`{"event":"pushed","params":` + long + `}`, true},
		{`{"id":"h1","error":{"message":"no"},"result":` + long + `}`, true},
		{" {	\"id\":\"h1\" , \"method\" : \"m\" ,\r \"params\" :  " + long + "  }  ", true},
		{`{"id":"h1","method":"m","params":{"a":1},"params":` + long + `}`, true},
		{`{"id":"h1","method":"m","params":` + long + `,"params":null}`, true},
		{`{"id":"h1","method":"m","PARAMS":` + long + `}`, true},
		{`{"id":"h1","method":"m","params":"` + strings.Repeat(`a string \" \u00e9 `, 30) + `"}`, true},
		{`{"id":"h1","method":"m","params":[` + strings.Repeat(`12.5e3,`, 80) + `0]}`, true},
		{`{"id":"h1","method":"m","params":` + long + `} trailing`, false},
		{`{"id":"h1","method":"m","params":` + long[:len(long)-1] + `}`, false},
		{`{"id":5,"method":"m","params":` + long + `}`, false},
		{`{"id":"h1","method":"http.request","params":{"method":"POST","body":"aGVsbG8="}}`, true},
		{`{"id":"h1","result":{"status":200,"body":"aGVsbG8="}}`, true},
		{`{"id":"h1","error":{"message":"no","code":"refused","data":{"why":[1,2,3]}}}`, true},
		{`{"event":"sessions.changed","params":[{"id":"s1"},{"id":"s2"}]}`, true},
		{`{"event":"rpc.cancel","params":{"id":"h1"}}`, true},
		{`{"id":"h1","method":"m"}`, true},
		{`{"id":"h1","method":"m","params":null}`, true},
		{`{"id":"h1","result":null,"error":null}`, true},
		{`{"id":"h1","method":"m","params":"a string"}`, true},
		{`{"id":"h1","method":"m","params":12.5e3}`, true},
		{`{"id":"h1","method":"m","params":true}`, true},
		{`{"id":"h1","method":"m","params":{}}`, true},
		{`{"id":"h1","method":"m","params":[]}`, true},
		{`{"id":"h1","method":"m","params":""}`, true},
		{`{"id":"h1","params":{"a":1},"result":{"b":2},"method":"m"}`, true},
		{"  {\t\"id\" : \"h1\" , \"method\":\"m\",\r \"params\" :  { \"a\" : [ 1 , 2 ] }  }  ", true},
		{`{"id":"h1","method":"m","params":{"a":1},"params":{"a":2}}`, true},
		{`{"id":"h1","method":"m","params":{"a":1},"params":null}`, true},
		{`{"id":"h1","method":"m","Params":{"a":1}}`, true},
		{`{"ID":"h1","METHOD":"m","PARAMS":{"a":1},"params":{"a":2}}`, true},
		{`{"id":"h1","method":"m","param\u0073":{"a":1}}`, true},
		{`{"id":"h1","method":"m","paramſ":{"a":1}}`, true},
		{`{"id":"h1","method":"m","params":{"text":"<a href=\"x\">&amp;</a> \u2028 \ud83d\ude00 \\ \/"}}`, true},
		{"{\"id\":\"h1\",\"method\":\"m\",\"params\":{\"text\":\"\xff\xfe not utf-8 \xc3\"}}", true},
		{`{"id":"h1","method":"m","params":{"deep":[[[[{"a":[{"b":null}]}]]]]},"unknown":{"x":[1,{"y":2}]}}`, true},
		{`{"jsonrpc":"2.0","id":"h1","method":"m","params":{"a":1}}`, true},
		{`{}`, true},
		{`null`, true},
		{`{"id":"h1","method":"m","params":{"a":1}} trailing`, false},
		{`{"id":"h1","method":"m","params":{"a":1}}{"id":"h2"}`, false},
		{`{"id":"h1","method":"m","params":{"a":1}`, false},
		{`{"id":"h1","method":"m","params":{"a":1,}}`, false},
		{`{"id":"h1","method":"m","params":{"a":01}}`, false},
		{`{"id":"h1","method":"m","params":}`, false},
		{`{"id":"h1","method":"m","params"}`, false},
		{`{"id":"h1","method":"m","params":{"a":"unterminated}}`, false},
		{`{"id":"h1","method":"m","params":{"a":"bad \x escape"}}`, false},
		{"{\"id\":\"h1\",\"method\":\"m\",\"params\":{\"a\":\"a control \x01 character\"}}", false},
		{`{"id":5,"method":"m","params":{"a":1}}`, false},
		{`{"id":"h1","method":["m"],"params":{"a":1}}`, false},
		{`{"id":"h1","error":"a string","result":{"a":1}}`, false},
		{`{"id":"h1","error":{"message":5},"result":{"a":1}}`, false},
		{`["id","h1"]`, false},
		{`"a string"`, false},
		{`12`, false},
		{`true`, false},
		{``, false},
		{`   `, false},
		{"\x00", false},
		{`{"id":"h1","method":"m","params":{"a":1}}` + "\x00", false},
		{strings.Repeat("x", 100), false},
	} {
		if read := ownedIsUnmarshal(t, []byte(c.line)); read != c.read {
			t.Errorf("%q: read without a copy: %v, want %v", c.line, read, c.read)
		}
	}
}

// A value the decoder handed on from a buffer of its own, or one that is not where its offset says, is not taken for a
// part of the line: unmarshalOwned then leaves the line to json.Unmarshal.
func TestKeptValueMustBeTheLines(t *testing.T) {
	line := []byte(`{"params":{"a":1}}`)
	const from, end = 10, 17
	if value, ok := (kept{value: line[from:end], end: end}).in(line); !ok || string(value) != `{"a":1}` || partOf(line, value) >= 0 {
		t.Fatalf("a small part of the line: %q, %v, want a copy of it", value, ok)
	}
	short := []byte(`{"p":{"a":1}}`) // here the value is most of the line
	if value, ok := (kept{value: short[5:12], end: 12}).in(short); !ok || string(value) != `{"a":1}` || partOf(short, value) != 5 ||
		cap(value) != len(value) {
		t.Fatalf("most of the line: %q, %v, want the line's own bytes and nothing behind them", value, ok)
	}
	if value, ok := (kept{}).in(line); !ok || value != nil {
		t.Fatalf("a field the line does not have: %q, %v", value, ok)
	}
	for name, k := range map[string]kept{
		"a copy of the value":            {value: bytes.Clone(line[from:end]), end: end},
		"an offset past the line":        {value: line[from:end], end: int64(len(line)) + 1},
		"an offset before the value's":   {value: line[from:end], end: end - 1},
		"an offset shorter than a value": {value: line[from:end], end: 3},
		"an empty value":                 {value: line[from:from], end: from},
	} {
		if value, ok := k.in(line); ok {
			t.Errorf("%s was taken for a part of the line: %q", name, value)
		}
	}
}

func FuzzUnmarshalOwnedIsUnmarshal(f *testing.F) {
	for _, seed := range []string{
		`{"id":"h1","method":"http.request","params":{"method":"POST","body":"aGVsbG8=","path":"/api/v1/assistant/conversations"}}`,
		`{"id":"h1","result":{"status":200,"header":{"Content-Type":["application/json"]},"body":"aGVsbG8="}}`,
		`{"params":[1,2,3,4,5,6,7,8,9]}`,
		`{"id":"h1","method":"http.request","params":{"method":"POST","body":"aGVsbG8="}}`,
		`{"id":"h1","result":{"status":200},"error":{"message":"no","data":[1]}}`,
		`{"event":"e","params":null,"PARAMS":[1,{"a":"\u00e9"}],"result":"r"}`,
		` { "id" : 5 , "params" : { "a" : 1 } } x`,
		`{"params":{"a":1},"params":2,"Result":3,"result":[4]}`,
	} {
		f.Add([]byte(seed))
	}
	f.Fuzz(func(t *testing.T, line []byte) { ownedIsUnmarshal(t, line) })
}

// uploadParams is an http.request's params with a body of n bytes, different for each salt.
func uploadParams(t *testing.T, n int, salt byte) []byte {
	t.Helper()
	body := make([]byte, n)
	for i := range body {
		body[i] = byte(i*7+i>>8) + salt
	}
	params, err := Marshal(uploadRequest{Method: "POST", Path: "/api/v1/assistant/conversations", Body: body})
	if err != nil {
		t.Fatal(err)
	}
	return params
}

// Serve hands a long line's params to the handler, its result to the call and an event's params to OnEvent as they
// were sent, whatever the reads split, with short lines before, between and behind them; and what a handler keeps of
// any line, long (the line's own bytes) or not (a copy out of the reader's buffer, also when the params are nearly
// all of the line), is still that after everything behind it was read.
func TestServeHandsOnLongLines(t *testing.T) {
	first := uploadParams(t, 3<<20, 1)
	second := uploadParams(t, linePiece, 2)
	third := uploadParams(t, 2<<20+12345, 3)
	answer := uploadParams(t, 2<<20, 4)
	small := []byte(`{"a":"short"}`)
	brief := []byte(`{"a":"` + strings.Repeat("most of a short line ", 20) + `"}`)
	medium := uploadParams(t, 300<<10, 5) // grows the reader's buffer, not past a piece
	var stream bytes.Buffer
	add := func(m message) {
		line, err := encodeLine(m)
		if err != nil {
			t.Fatal(err)
		}
		stream.Write(line)
	}
	add(message{ID: "n1", Method: "n1", Params: small})
	add(message{ID: "n6", Method: "n6", Params: brief})
	add(message{ID: "n7", Method: "n7", Params: medium})
	add(message{ID: "n2", Method: "n2", Params: first})
	add(message{Event: "note", Params: small})
	add(message{ID: "n3", Method: "n3", Params: second})
	add(message{ID: "h7", Result: answer})
	add(message{Event: "pushed", Params: third})
	for range 200 {
		add(message{Event: "note", Params: small})
	}
	add(message{ID: "n4", Method: "n4", Params: small})
	stream.WriteString(`{"id":"n5","method":"n5","params":{"a":"the last line, without a newline"}}`)
	want := map[string][]byte{"n1": small, "n2": first, "n3": second, "n4": small, "n6": brief, "n7": medium, "pushed": third,
		"n5": []byte(`{"a":"the last line, without a newline"}`)}

	for _, reads := range [][]byte{nil, {32}, {1, 0, 250, 3}} {
		t.Run(fmt.Sprint(reads), func(t *testing.T) {
			p := New("h", io.Discard)
			type handed struct {
				method string
				params json.RawMessage
			}
			requests := make(chan handed, 8)
			p.OnRequest = func(_ context.Context, method string, params json.RawMessage) (any, error) {
				requests <- handed{method, params}
				return nil, nil
			}
			got := map[string]json.RawMessage{}
			notes := 0
			p.OnEvent = func(event string, params json.RawMessage) {
				if event == "note" {
					if notes++; !bytes.Equal(params, small) {
						t.Errorf("a note's params: %q", params)
					}
					return
				}
				got[event] = params
			}
			reply := make(chan message, 1)
			p.pending["h7"] = reply
			r := &chunks{data: bytes.Clone(stream.Bytes()), sizes: reads, scale: 1024, end: io.EOF}
			if err := p.Serve(context.Background(), r); err != io.EOF {
				t.Fatal(err)
			}
			for i := range 7 { // their handlers run on goroutines of their own
				select {
				case h := <-requests:
					got[h.method] = h.params
				case <-time.After(10 * time.Second):
					t.Fatalf("%d of 7 requests reached the handler", i)
				}
			}
			if notes != 201 {
				t.Errorf("%d notes, want 201", notes)
			}
			for id, params := range want {
				if !bytes.Equal(got[id], params) {
					t.Errorf("%s got %d bytes of params, want the %d sent", id, len(got[id]), len(params))
				}
			}
			select {
			case m := <-reply:
				if !bytes.Equal(m.Result, answer) || m.ID != "h7" {
					t.Errorf("the call got %d bytes of result, want the %d sent", len(m.Result), len(answer))
				}
			default:
				t.Error("the call got no reply")
			}
		})
	}
}

// Serve's limit on a line is what it was: a line one byte under Limits.FrameBytes is served, one of FrameBytes ends
// the connection with bufio.ErrTooLong before any handler sees it, also when the line is read in pieces.
func TestServeLongLineAtTheLimit(t *testing.T) {
	const limit = 2<<20 + 77
	line := func(length int) []byte {
		const head, tail = `{"event":"e","params":"`, `"}`
		return append(append([]byte(head), bytes.Repeat([]byte("x"), length-len(head)-len(tail))...), tail...)
	}
	for _, c := range []struct {
		length int
		err    error
		events int
	}{{limit - 1, io.EOF, 2}, {limit, bufio.ErrTooLong, 1}, {limit + 1, bufio.ErrTooLong, 1}} {
		p := New("h", io.Discard)
		p.Limits.FrameBytes = limit
		events := 0
		p.OnEvent = func(string, json.RawMessage) { events++ }
		stream := append(append([]byte(`{"event":"e","params":1}`+"\n"), line(c.length)...), '\n')
		err := p.Serve(context.Background(), &chunks{data: stream, sizes: []byte{60}, scale: 1024, end: io.EOF})
		if err != c.err || events != c.events {
			t.Errorf("a line of %d bytes under a limit of %d: %d events, %v; want %d, %v", c.length, limit, events, err,
				c.events, c.err)
		}
	}
}

// A request whose params are megabytes costs Serve its line twice, the pieces it was read in and the line they are
// put together in: the handler's params are that line's, not one more copy (and no buffers doubled on the way).
func TestServeCopiesALongLineOnce(t *testing.T) {
	if raceEnabled {
		t.Skip("the race detector's instrumentation allocates on its own")
	}
	line := uploadLine(t, false, 12<<20)
	serve := func() {
		p := New("h", io.Discard)
		got := make(chan int, 1)
		p.OnRequest = func(_ context.Context, _ string, params json.RawMessage) (any, error) {
			got <- len(params)
			return nil, nil
		}
		if err := p.Serve(context.Background(), &socketReads{data: line}); err != io.EOF {
			t.Fatal(err)
		}
		select {
		case n := <-got:
			if n < 16<<20 {
				t.Fatalf("the handler got %d bytes of params", n)
			}
		case <-time.After(10 * time.Second):
			t.Fatal("the request did not reach the handler")
		}
	}
	serve()
	var before, after runtime.MemStats
	least := ^uint64(0)
	func() {
		defer debug.SetGCPercent(debug.SetGCPercent(-1)) // no GC empties grownBufs in the middle of a run
		for range 3 {
			runtime.ReadMemStats(&before)
			serve()
			runtime.ReadMemStats(&after)
			least = min(least, after.TotalAlloc-before.TotalAlloc)
		}
	}()
	if limit := uint64(float64(2*len(line))*1.03) + 3*linePiece; least > limit {
		t.Errorf("serving a %d-byte request allocated %d bytes, over %d (its pieces and the line)", len(line), least, limit)
	}
}
