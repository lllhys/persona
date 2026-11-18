package transcript

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math/rand/v2"
	"strconv"
	"strings"
	"testing"
)

func shortValueInput() json.RawMessage {
	values := make([]any, 4_000)
	for i := range values {
		values[i] = []any{strconv.Itoa(i), strings.Repeat("a", 17), strings.Repeat("b", 18)}
	}
	raw, err := json.Marshal(map[string]any{"values": values})
	if err != nil {
		panic(err)
	}
	return raw
}

func TestClipJSONBoundsShortValues(t *testing.T) {
	raw := shortValueInput()
	if len(raw) != 198_902 {
		t.Fatalf("reproduction is %d bytes, want 198902", len(raw))
	}
	out := clipJSON(raw)
	if len(out) > maxInputLen {
		t.Fatalf("clipJSON left %d bytes from %d, want at most %d", len(out), len(raw), maxInputLen)
	}
}

func randomClipString(r *rand.Rand) string {
	parts := []string{"x", "<>&", "  ", "汉🙂", "\"\\\n"}
	lengths := []int{0, 1, 7, 31, 255, 256, 257, 1000, 5000}
	return strings.Repeat(parts[r.IntN(len(parts))], lengths[r.IntN(len(lengths))])
}

func randomClipValue(r *rand.Rand, depth int) any {
	if depth == 0 || r.IntN(4) == 0 {
		switch r.IntN(4) {
		case 0:
			return randomClipString(r)
		case 1:
			return r.Int()
		case 2:
			return r.IntN(2) == 0
		default:
			return nil
		}
	}
	if r.IntN(2) == 0 {
		out := make([]any, r.IntN(7))
		for i := range out {
			out[i] = randomClipValue(r, depth-1)
		}
		return out
	}
	out := make(map[string]any, r.IntN(7))
	for i := 0; i < r.IntN(7); i++ {
		out[fmt.Sprintf("key_%d_%s", i, strings.Repeat("k", r.IntN(20)))] = randomClipValue(r, depth-1)
	}
	return out
}

func TestClipJSONRandomObjectsBounded(t *testing.T) {
	r := rand.New(rand.NewPCG(2907, 3))
	for i := 0; i < 40; i++ {
		root := map[string]any{}
		for j := 0; j < 12; j++ {
			root[fmt.Sprintf("field_%02d_%s", j, strings.Repeat("k", r.IntN(20)))] = randomClipValue(r, 4)
		}
		values := make([]any, 600)
		for j := range values {
			values[j] = randomClipValue(r, 2)
		}
		root["values"] = values
		raw := marshalPlain(root)
		out := clipJSON(raw)
		if len(out) > maxInputLen || !json.Valid(out) || len(out) > len(raw) {
			t.Fatalf("case %d: %d bytes from %d (valid %v), want valid JSON at most %d", i, len(out), len(raw), json.Valid(out), maxInputLen)
		}
		if again := clipJSON(raw); !bytes.Equal(out, again) {
			t.Fatalf("case %d is not deterministic", i)
		}
		var decoded map[string]any
		if err := json.Unmarshal(out, &decoded); err != nil {
			t.Fatalf("case %d: %v", i, err)
		}
	}
}

func FuzzClipJSONBound(f *testing.F) {
	f.Add([]byte(shortValueInput()))
	f.Add([]byte(`{"outer":[["a","b"],["c","d"]],"wide":{"a":1,"b":2}}`))
	f.Fuzz(func(t *testing.T, raw []byte) {
		out := clipJSON(raw)
		if len(raw) <= maxInputLen {
			if !bytes.Equal(out, raw) {
				t.Fatalf("at/under-budget input changed: %d bytes from %d", len(out), len(raw))
			}
			return
		}
		var object map[string]any
		if json.Unmarshal(raw, &object) != nil {
			return // invalid and non-object inputs keep their existing pass-through behavior
		}
		if len(out) > maxInputLen || !json.Valid(out) {
			t.Fatalf("clipJSON returned %d valid-%v bytes from %d, want valid JSON at most %d", len(out), json.Valid(out), len(raw), maxInputLen)
		}
	})
}

func TestClipJSONBoundsLargeJSONNumbers(t *testing.T) {
	raw := []byte(`{"n":1e1000,` + strings.TrimPrefix(string(shortValueInput()), "{"))
	out := clipJSON(raw)
	if len(out) > maxInputLen || !json.Valid(out) {
		t.Fatalf("large-number object became %d valid-%v bytes, want valid JSON at most %d", len(out), json.Valid(out), maxInputLen)
	}
	dec := json.NewDecoder(bytes.NewReader(out))
	dec.UseNumber()
	var decoded map[string]any
	if err := dec.Decode(&decoded); err != nil {
		t.Fatal(err)
	}
	number, ok := decoded["n"].(json.Number)
	if !ok || number.String() != "1e1000" {
		t.Fatalf("large number changed: %#v", decoded["n"])
	}
	if note := inputClipMarkerNote(t, decoded[inputClipMarker]); !strings.HasSuffix(note, fmt.Sprintf(clipNoteFormat, len(raw))) {
		t.Fatalf("large-number object has the wrong structural clip marker: %q", note)
	}
}

func TestClipJSONTrailingJSONAfterLargeNumberPassesThrough(t *testing.T) {
	raw := []byte(`{"n":1e1000,` + strings.TrimPrefix(string(shortValueInput()), "{") + ` x`)
	if out := clipJSON(raw); !bytes.Equal(out, raw) {
		t.Fatalf("trailing JSON after a large number changed: %d bytes from %d", len(out), len(raw))
	}
}

func TestClipJSONReportsOriginalSizeWithWhitespace(t *testing.T) {
	raw := append([]byte(" \n\t"), shortValueInput()...)
	raw = append(raw, ' ', '\n')
	out := clipJSON(raw)
	decoded, err := decodeInputObject(out)
	if err != nil {
		t.Fatal(err)
	}
	note := inputClipMarkerNote(t, decoded[inputClipMarker])
	if !strings.HasSuffix(note, fmt.Sprintf(clipNoteFormat, len(raw))) {
		t.Fatalf("marker %q does not report the original %d bytes", note, len(raw))
	}
}

func TestClipInputWorkLinearInDeepNesting(t *testing.T) {
	const depth, items = 2000, 12_000
	candidate := []byte(`{"a":` + strings.Repeat("[", depth) + strings.TrimSuffix(strings.Repeat("1,", items), ",") + strings.Repeat("]", depth) + `}`)
	if len(candidate) <= maxInputLen {
		t.Fatalf("deep fixture is only %d bytes", len(candidate))
	}
	out, work := boundInputJSONBudgetWork(candidate, len(candidate), maxInputLen)
	if len(out) > maxInputLen || !json.Valid(out) {
		t.Fatalf("deep input returned %d valid-%v bytes", len(out), json.Valid(out))
	}
	nodes := depth + items + 2 // root, every array, and every scalar
	if work > 6*nodes {
		t.Fatalf("deep clipping visited %d nodes for %d indexed values, want linear work at most %d", work, nodes, 6*nodes)
	}
}

func legacyClipJSONStrings(raw json.RawMessage) json.RawMessage {
	if len(raw) <= maxInputLen {
		return raw
	}
	var m map[string]any
	if json.Unmarshal(raw, &m) != nil {
		return raw
	}
	b := marshalPlain(mapStrings(m, func(s string) string { return clipTo(s, maxInputLen/2) }))
	if len(b) <= maxInputLen {
		return b
	}
	long, kept := 0, 0
	rest := marshalPlain(mapStrings(m, func(s string) string {
		if len(s) <= minInputShare {
			return s
		}
		long, kept = long+1, kept+min(len(s), maxInputLen/2)
		return ""
	}))
	if long == 0 {
		return b
	}
	share := (maxInputLen - len(rest)) * kept / (len(b) - len(rest)) / long
	share = min(max(share-clipNoteRoom, minInputShare), maxInputLen/2)
	return marshalPlain(mapStrings(m, func(s string) string { return clipIfShorter(s, share) }))
}

func TestClipJSONLeavesFittingStringPassesUnchanged(t *testing.T) {
	blocks := make([]map[string]string, 40)
	for i := range blocks {
		blocks[i] = map[string]string{"type": "paragraph", "text": strings.Repeat("Some notes on the plan. ", 125)}
	}
	medium, _ := json.Marshal(map[string]any{"page_id": "p1", "children": blocks})
	for _, raw := range []json.RawMessage{json.RawMessage(pushFilesInput()), medium} {
		legacy := legacyClipJSONStrings(raw)
		if len(legacy) > maxInputLen {
			t.Fatalf("legacy fixture is %d bytes, want a fitting string-pass result", len(legacy))
		}
		if got := clipJSON(raw); !bytes.Equal(got, legacy) {
			t.Fatalf("fitting string-pass output changed: %d bytes from legacy %d", len(got), len(legacy))
		}
	}
}

func BenchmarkClipJSONShortValues(b *testing.B) {
	raw := shortValueInput()
	var out json.RawMessage
	b.ReportAllocs()
	for b.Loop() {
		out = clipJSON(raw)
	}
	b.ReportMetric(float64(len(raw)), "raw-bytes/item")
	b.ReportMetric(float64(len(out)), "bytes/item")
}
