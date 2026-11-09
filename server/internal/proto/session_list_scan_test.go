package proto

import (
	"bytes"
	"encoding/json"
	"encoding/json/jsontext"
	"io"
	"math/bits"
	"math/rand/v2"
	"reflect"
	"slices"
	"strings"
	"testing"
)

// jsontextScan is ScanSessionList as it was with jsontext's decoder reading data: what listScan reads byte by byte
// has to take, refuse and split exactly as it did.
func jsontextScan(data []byte) (rest []byte, rows []jsontext.Value, ok bool) {
	dec := jsontext.NewDecoder(bytes.NewBuffer(data), json.DefaultOptionsV1())
	if tok, err := dec.ReadToken(); err != nil || tok.Kind() != '{' {
		return nil, nil, false
	}
	read := func() (jsontext.Value, error) {
		v, err := dec.ReadValue()
		if err != nil {
			return nil, err
		}
		end := int(dec.InputOffset())
		return jsontext.Value(data[end-len(v) : end : end]), nil
	}
	var names [maxListMembers + 1][]byte
	n, listed := 0, false
	for dec.PeekKind() != '}' {
		name, err := read()
		if err != nil || n == len(names) || bytes.IndexByte(name, '\\') >= 0 {
			return nil, nil, false
		}
		bare := name[1 : len(name)-1]
		for _, seen := range names[:n] {
			if bytes.Equal(seen[1:len(seen)-1], bare) {
				return nil, nil, false
			}
		}
		names[n] = name
		n++
		if string(bare) == "sessions" {
			if dec.PeekKind() != '[' {
				return nil, nil, false
			}
			if _, err := dec.ReadToken(); err != nil {
				return nil, nil, false
			}
			rows = make([]jsontext.Value, 0, min(len(data)/512, 4096)+8)
			for dec.PeekKind() != ']' {
				v, err := read()
				if err != nil {
					return nil, nil, false
				}
				rows = append(rows, v)
			}
			if _, err := dec.ReadToken(); err != nil {
				return nil, nil, false
			}
			listed = true
			continue
		}
		if foldsToSessions(bare) {
			return nil, nil, false
		}
		val, err := read()
		if err != nil {
			return nil, nil, false
		}
		if rest == nil {
			rest = []byte{'{'}
		} else {
			rest = append(rest, ',')
		}
		rest = append(append(append(rest, name...), ':'), val...)
	}
	if _, err := dec.ReadToken(); err != nil {
		return nil, nil, false
	}
	if _, err := dec.ReadToken(); err != io.EOF || !listed {
		return nil, nil, false
	}
	if rest == nil {
		return emptyObject, rows, true
	}
	return append(rest, '}'), rows, true
}

// scanMatches: ScanSessionList splits data as jsontextScan does, and the event decodes as json.Unmarshal decodes it,
// from a fresh decoder and from its memo.
func scanMatches(t *testing.T, data []byte) {
	t.Helper()
	rest, rows, ok := ScanSessionList(data)
	wantRest, wantRows, wantOK := jsontextScan(data)
	if ok != wantOK || !bytes.Equal(rest, wantRest) || (rows == nil) != (wantRows == nil) ||
		!slices.EqualFunc(rows, wantRows, func(a, b jsontext.Value) bool { return bytes.Equal(a, b) }) {
		t.Fatalf("%q:\n got %v %q %q\nwant %v %q %q", data, ok, rest, rows, wantOK, wantRest, wantRows)
	}
	var want sessionsEvent
	wantErr := json.Unmarshal(data, &want)
	var d SessionListDecoder
	for round := range 2 {
		got, _, err := decodeEvent(&d, data)
		if (err != nil) != (wantErr != nil) || err == nil && !reflect.DeepEqual(got, want) {
			t.Fatalf("%q (round %d):\n got %+v, %v\nwant %+v, %v", data, round, got, err, want, wantErr)
		}
	}
}

// FuzzScanSessionList: whatever the bytes, the scan splits them as jsontext's decoder did and the event decodes as
// json.Unmarshal decodes it. Seeded with scanCases and a busy machine's list.
func FuzzScanSessionList(f *testing.F) {
	for _, tc := range scanCases {
		f.Add([]byte(tc.data))
	}
	f.Add(sessionListJSON(f))
	f.Fuzz(scanMatches)
}

// The scan reads as many arrays and objects one in another as jsontext does, and not one more: in a session, beside
// the list, and as the session itself (a session alone would decode at any of these depths).
func TestScanSessionListDepth(t *testing.T) {
	for _, depth := range []int{maxNestingDepth - 1, maxNestingDepth, maxNestingDepth + 1} {
		for _, at := range []struct {
			in   int // the arrays and objects v is in
			wrap func(v string) string
		}{
			{3, func(v string) string { return `{"sessions":[{"session_id":"s1","x":` + v + `}]}` }},
			{1, func(v string) string { return `{"machine_id":"m","x":` + v + `,"sessions":[]}` }},
			{2, func(v string) string { return `{"sessions":[` + v + `]}` }},
		} {
			for _, open := range []string{"[", `{"a":`} {
				close := map[string]string{"[": "]", `{"a":`: "}"}[open]
				k := depth - at.in
				data := []byte(at.wrap(strings.Repeat(open, k) + "1" + strings.Repeat(close, k)))
				if _, _, ok := ScanSessionList(data); ok != (depth <= maxNestingDepth) {
					t.Fatalf("%d deep (%s in %d): ok %v", depth, open, at.in, ok)
				}
				scanMatches(t, data)
			}
		}
	}
}

// specials marks the first quote, backslash or control character of eight bytes, whatever the bytes around it.
func TestSpecialsFindsTheFirst(t *testing.T) {
	picks := []byte{0x00, 0x01, 0x1f, ' ', '!', '"', '#', '\\', ']', 'a', 0x7f, 0x80, 0xa2, 0xdc, 0xe0, 0xff}
	rng := rand.New(rand.NewPCG(3, 4))
	for range 200000 {
		var w uint64
		first := 8
		for i := range 8 {
			c := picks[rng.IntN(len(picks))]
			if rng.IntN(3) == 0 {
				c = byte(rng.Uint32())
			}
			if first == 8 && (c == '"' || c == '\\' || c < ' ') {
				first = i
			}
			w |= uint64(c) << (8 * i)
		}
		if got := bits.TrailingZeros64(specials(w)) / 8; got != first {
			t.Fatalf("%016x: first special at %d, specials says %d", w, first, got)
		}
	}
}

// BenchmarkScanSessionList splits a busy machine's list (sessionListJSON), as the hub and the manager do on each of
// its changes before decoding what changed; jsontext is how it was split with jsontext's decoder (jsontextScan).
func BenchmarkScanSessionList(b *testing.B) {
	data := sessionListJSON(b)
	for _, scan := range []struct {
		name string
		scan func([]byte) ([]byte, []jsontext.Value, bool)
	}{{"bytes", ScanSessionList}, {"jsontext", jsontextScan}} {
		b.Run(scan.name, func(b *testing.B) {
			b.SetBytes(int64(len(data)))
			b.ReportAllocs()
			for b.Loop() {
				if _, rows, ok := scan.scan(data); !ok || len(rows) != 150 {
					b.Fatal(ok, len(rows))
				}
			}
		})
	}
}
