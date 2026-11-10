package rpc

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"runtime"
	"runtime/debug"
	"testing"
)

// oldNotify is the line Notify wrote before it took encoded params as they are: params encoded, then the envelope,
// then the newline.
func oldNotify(event string, params any) ([]byte, error) {
	raw, err := json.Marshal(params)
	if err != nil {
		return nil, err
	}
	b, err := json.Marshal(message{Event: event, Params: raw})
	if err != nil {
		return nil, err
	}
	return append(b, '\n'), nil
}

// Encoded params go out exactly as when they were encoded once more: HTML characters and U+2028 escaped, whitespace
// dropped, and the same error for ones that are not JSON (nothing written).
func TestNotifyRawIsTheSameLine(t *testing.T) {
	for _, params := range []any{
		json.RawMessage(`{"html":"<a href='x'>&amp;</a>","sep":"\u2028\u2029   "}`),
		json.RawMessage(" {\n\t\"a\" : [ 1 , 2.50 , \"\\u003c\" ] ,\r\n \"b\":{ } }\n"),
		json.RawMessage(`null`), json.RawMessage(`"é\u00e9"`), json.RawMessage(`12e3`),
		json.RawMessage(`{"a":`), json.RawMessage(`[1,2,]`), json.RawMessage("{\"bad\":\"\xff\"}"), json.RawMessage("  "),
		json.RawMessage(``), json.RawMessage(nil), map[string]any{"html": "<&>"},
	} {
		var got bytes.Buffer
		err := New("t", &got).Notify("e", params)
		want, werr := oldNotify("e", params)
		if fmt.Sprint(err) != fmt.Sprint(werr) || !bytes.Equal(got.Bytes(), want) {
			t.Errorf("Notify(%q):\n%q, %v\nwas %q, %v", params, got.Bytes(), err, want, werr)
		}
	}
}

func sessionList(size int) json.RawMessage {
	b := []byte(`{"sessions":[`)
	for i := 0; len(b) < size; i++ {
		b = fmt.Appendf(b, `{"id":"s%06d","title":"a <b>session</b> & more","cwd":"/home/u/work"},`, i)
	}
	return append(b[:len(b)-1], "]}"...)
}

// BenchmarkNotifyRaw sends a computer's session list (50 and 200 sessions' worth) as encoded params.
func BenchmarkNotifyRaw(b *testing.B) {
	for _, size := range []int{39 << 10, 260 << 10} {
		b.Run(fmt.Sprintf("%dK", size>>10), func(b *testing.B) {
			p, raw := New("t", io.Discard), sessionList(size)
			b.SetBytes(int64(len(raw)))
			b.ReportAllocs()
			for range b.N {
				if err := p.Notify("sessions", raw); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

const notifyRawAllocationLimit = 371566

// Sending a 260 KiB session list allocates its line once; it was the list twice more (encoded again, and copied to
// add the newline).
func TestNotifyRawAllocates(t *testing.T) {
	p, raw := New("t", io.Discard), sessionList(260<<10)
	per := notifyAllocatedBytes(t, func() error { return p.Notify("sessions", raw) })
	// The line is 360,744 bytes (its HTML characters escaped); it was 721,168.
	if per > notifyRawAllocationLimit && !raceEnabled {
		t.Errorf("a Notify of %d encoded bytes allocated %d bytes, want at most %d", len(raw), per, notifyRawAllocationLimit)
	}
}

// The same measurement and limit must still reject the historical double-encoding implementation.
func TestNotifyRawAllocationLimitRejectsDoubleEncoding(t *testing.T) {
	raw := sessionList(260 << 10)
	per := notifyAllocatedBytes(t, func() error {
		_, err := oldNotify("sessions", raw)
		return err
	})
	if per <= notifyRawAllocationLimit {
		t.Errorf("double encoding allocated %d bytes, want more than %d", per, notifyRawAllocationLimit)
	}
}

// notifyAllocatedBytes measures the warmed steady state, like testing.AllocsPerRun: a single processor keeps
// json's warmed encoder in the same sync.Pool shard. Disabling GC alone does not prevent a switch to a cold shard.
func notifyAllocatedBytes(t *testing.T, send func() error) uint64 {
	t.Helper()
	const runs = 50
	defer runtime.GOMAXPROCS(runtime.GOMAXPROCS(1))
	defer debug.SetGCPercent(debug.SetGCPercent(-1)) // no GC empties json's pool of encoder buffers
	if err := send(); err != nil {                   // warm the encoder before measuring
		t.Fatal(err)
	}
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	for range runs {
		if err := send(); err != nil {
			t.Fatal(err)
		}
	}
	runtime.ReadMemStats(&after)
	return (after.TotalAlloc - before.TotalAlloc) / runs
}

// raceEnabled: built with -race, whose sync.Pool drops buffers at random (race_test.go).
var raceEnabled bool
