package transcript

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"testing"
)

type inputClipFixtureCase struct {
	Name     string          `json:"name"`
	Input    json.RawMessage `json:"input"`
	Expected struct {
		Check           string   `json:"check"`
		InputBytes      int      `json:"input_bytes"`
		GoInputBytes    int      `json:"go_input_bytes"`
		SDKInputBytes   int      `json:"sdk_input_bytes"`
		OutputBytes     int      `json:"output_bytes"`
		GoOutputBytes   int      `json:"go_output_bytes"`
		SDKOutputBytes  int      `json:"sdk_output_bytes"`
		Remaining       *int     `json:"remaining"`
		GoRemaining     *int     `json:"go_remaining"`
		SDKRemaining    *int     `json:"sdk_remaining"`
		ARemaining      int      `json:"a_remaining"`
		BRemaining      int      `json:"b_remaining"`
		Total           int      `json:"total"`
		Inner           []int    `json:"inner"`
		LastKey         string   `json:"last_key"`
		Present         []string `json:"present"`
		Absent          []string `json:"absent"`
		ArrayItems      *int     `json:"array_items"`
		ObjectFields    *int     `json:"object_fields"`
		GoObjectFields  *int     `json:"go_object_fields"`
		SDKObjectFields *int     `json:"sdk_object_fields"`
		Marker          bool     `json:"marker"`
		MarkerKey       string   `json:"marker_key"`
		KeyPrefix       string   `json:"key_prefix"`
		Value           *string  `json:"value"`
	} `json:"expected"`
}

func inputClipFixtureInt(t *testing.T, v any) int {
	t.Helper()
	n, ok := v.(float64)
	if !ok || n != float64(int(n)) {
		t.Fatalf("fixture count %v is not an integer", v)
	}
	return int(n)
}

func expandInputClipFixture(t *testing.T, v any, index int) any {
	t.Helper()
	switch v := v.(type) {
	case []any:
		out := make([]any, len(v))
		for i, e := range v {
			out[i] = expandInputClipFixture(t, e, index)
		}
		return out
	case map[string]any:
		if target, ok := v["$pad_object_to"]; ok {
			value, ok := expandInputClipFixture(t, v["value"], index).(map[string]any)
			if !ok {
				t.Fatal("$pad_object_to value is not an object")
			}
			if _, exists := value["pad"]; exists {
				t.Fatal("$pad_object_to value already has pad")
			}
			value["pad"] = ""
			base := len(marshalPlain(value))
			padding := inputClipFixtureInt(t, target) - base
			if padding < 0 {
				t.Fatalf("padded object is already %d bytes", base)
			}
			value["pad"] = strings.Repeat("x", padding)
			if got := len(marshalPlain(value)); got != base+padding {
				t.Fatalf("padded object is %d bytes, want %d", got, base+padding)
			}
			return value
		}
		if keys, ok := v["$keyed_fields"].([]any); ok {
			out := make(map[string]any, len(keys))
			suffix, _ := v["suffix"].(string)
			count := inputClipFixtureInt(t, v["count"])
			for i, key := range keys {
				name := key.(string) + strings.Repeat(suffix, count)
				out[name] = expandInputClipFixture(t, v["value"], i)
			}
			return out
		}
		if count, ok := v["$repeat"]; ok {
			out := make([]any, inputClipFixtureInt(t, count))
			for i := range out {
				out[i] = expandInputClipFixture(t, v["value"], i)
			}
			return out
		}
		if key, ok := v["$single_field"]; ok {
			name, ok := expandInputClipFixture(t, key, index).(string)
			if !ok {
				t.Fatal("$single_field key is not a string")
			}
			return map[string]any{name: expandInputClipFixture(t, v["value"], index)}
		}
		if count, ok := v["$fields"]; ok {
			out := make(map[string]any, inputClipFixtureInt(t, count))
			if base, ok := v["base"].(map[string]any); ok {
				for k, e := range base {
					out[k] = expandInputClipFixture(t, e, index)
				}
			}
			prefix, _ := v["key"].(string)
			pad := inputClipFixtureInt(t, v["pad"])
			for i := 0; i < inputClipFixtureInt(t, count); i++ {
				key := fmt.Sprintf("%s%0*d", prefix, pad, i)
				out[key] = expandInputClipFixture(t, v["value"], i)
			}
			return out
		}
		if width, ok := v["$index"]; ok {
			w := inputClipFixtureInt(t, width)
			if w == 0 {
				return strconv.Itoa(index)
			}
			return fmt.Sprintf("%0*d", w, index)
		}
		if text, ok := v["$string"]; ok {
			return strings.Repeat(text.(string), inputClipFixtureInt(t, v["count"]))
		}
		out := make(map[string]any, len(v))
		for k, e := range v {
			out[k] = expandInputClipFixture(t, e, index)
		}
		return out
	default:
		return v
	}
}

func inputClipTestObject(t *testing.T, v any) map[string]any {
	t.Helper()
	m, ok := v.(map[string]any)
	if !ok {
		t.Fatalf("got %T, want an object", v)
	}
	return m
}

func inputClipTestArray(t *testing.T, v any) []any {
	t.Helper()
	a, ok := v.([]any)
	if !ok {
		t.Fatalf("got %T, want an array", v)
	}
	return a
}

func inputClipMarkerNote(t *testing.T, v any) string {
	t.Helper()
	marker := inputClipTestObject(t, v)
	if len(marker) != 1 {
		t.Fatalf("marker has %d fields, want only note", len(marker))
	}
	note, ok := marker["note"].(string)
	if !ok {
		t.Fatalf("marker note is %T, want a string", marker["note"])
	}
	return note
}

func inputClipMarkerCounts(t *testing.T, note string) (arrayItems, objectFields int) {
	t.Helper()
	first, _, _ := strings.Cut(note, "\n")
	if _, err := fmt.Sscanf(first, "Omitted %d array items and %d object fields.", &arrayItems, &objectFields); err != nil {
		t.Fatalf("bad marker %q: %v", first, err)
	}
	return arrayItems, objectFields
}

func TestClipInputSharedCases(t *testing.T) {
	raw, err := os.ReadFile("../../../spec/fixtures/input-clip/cases.json")
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Cases []inputClipFixtureCase `json:"cases"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	if len(doc.Cases) == 0 {
		t.Fatal("no cases")
	}
	for _, c := range doc.Cases {
		t.Run(c.Name, func(t *testing.T) {
			var directive any
			if err := json.Unmarshal(c.Input, &directive); err != nil {
				t.Fatal(err)
			}
			expanded := expandInputClipFixture(t, directive, 0)
			input := marshalPlain(expanded)
			wantInputBytes := c.Expected.InputBytes
			if c.Expected.GoInputBytes > 0 {
				wantInputBytes = c.Expected.GoInputBytes
			}
			if wantInputBytes > 0 && len(input) != wantInputBytes {
				t.Fatalf("input is %d bytes, want %d", len(input), wantInputBytes)
			}
			before := append([]byte(nil), input...)
			out := clipJSON(input)
			if !bytes.Equal(input, before) {
				t.Fatal("clipJSON changed its source bytes")
			}
			if len(out) > maxInputLen {
				t.Fatalf("output is %d bytes, want at most %d", len(out), maxInputLen)
			}
			wantOutputBytes := c.Expected.OutputBytes
			if c.Expected.GoOutputBytes > 0 {
				wantOutputBytes = c.Expected.GoOutputBytes
			}
			if wantOutputBytes > 0 && len(out) != wantOutputBytes {
				t.Fatalf("output is %d bytes, want %d", len(out), wantOutputBytes)
			}
			if again := clipJSON(input); !bytes.Equal(out, again) {
				t.Fatal("clipJSON is not deterministic")
			}
			if again := clipJSON(out); !bytes.Equal(out, again) {
				t.Fatal("a bounded result changed when clipped again")
			}
			if !c.Expected.Marker {
				if !bytes.Equal(out, input) {
					t.Fatalf("unmarked output changed: %d bytes from %d", len(out), len(input))
				}
				return
			}
			var decoded map[string]any
			if err := json.Unmarshal(out, &decoded); err != nil {
				t.Fatalf("output is not valid JSON: %v", err)
			}
			markerKey := c.Expected.MarkerKey
			if markerKey == "" {
				markerKey = inputClipMarker
			}
			note := inputClipMarkerNote(t, decoded[markerKey])
			if !strings.HasSuffix(note, fmt.Sprintf(clipNoteFormat, len(input))) {
				t.Fatalf("marker %q does not end with the original-size cut note", note)
			}
			if !clipped(decoded) {
				t.Fatal("the hub's recursive cut detector did not recognize the marker")
			}
			arrayItems, objectFields := inputClipMarkerCounts(t, note)
			wantObjectFields := c.Expected.ObjectFields
			if c.Expected.GoObjectFields != nil {
				wantObjectFields = c.Expected.GoObjectFields
			}
			remaining := c.Expected.Remaining
			if c.Expected.GoRemaining != nil {
				remaining = c.Expected.GoRemaining
			}
			if c.Expected.ArrayItems != nil && arrayItems != *c.Expected.ArrayItems {
				t.Fatalf("marker says %d array items, want %d", arrayItems, *c.Expected.ArrayItems)
			}
			if wantObjectFields != nil && objectFields != *wantObjectFields {
				t.Fatalf("marker says %d object fields, want %d", objectFields, *wantObjectFields)
			}

			switch c.Expected.Check {
			case "short_values":
				values := inputClipTestArray(t, decoded["values"])
				if remaining != nil && len(values) != *remaining {
					t.Fatalf("kept %d rows, want %d", len(values), *remaining)
				}
				for i, row := range values {
					fields := inputClipTestArray(t, row)
					if len(fields) != 3 || fields[0] != strconv.Itoa(i) || fields[1] != strings.Repeat("a", 17) || fields[2] != strings.Repeat("b", 18) {
						t.Fatalf("row %d changed: %#v", i, fields)
					}
				}
				if arrayItems != 4_000-len(values) || objectFields != 0 {
					t.Fatalf("marker counts (%d, %d) do not match %d retained rows", arrayItems, objectFields, len(values))
				}
			case "nested_arrays":
				outer := inputClipTestArray(t, decoded["outer"])
				if remaining == nil || len(outer) != *remaining || len(c.Expected.Inner) != 1 {
					t.Fatalf("kept %d outer rows, want exactly %v with one shared inner length", len(outer), remaining)
				}
				for i, row := range outer {
					inner := inputClipTestArray(t, row)
					if len(inner) != c.Expected.Inner[0] {
						t.Fatalf("inner row %d has %d items, want %d", i, len(inner), c.Expected.Inner[0])
					}
					for _, value := range inner {
						if value != "x" {
							t.Fatalf("inner row %d changed a value: %#v", i, value)
						}
					}
				}
				if objectFields != 0 {
					t.Fatalf("nested arrays omitted %d object fields", objectFields)
				}
			case "object_array":
				if decoded["file_path"] != "/home/user/project/a.go" {
					t.Fatalf("file_path changed: %#v", decoded["file_path"])
				}
				edits := inputClipTestArray(t, decoded["edits"])
				if remaining == nil || len(edits) != *remaining {
					t.Fatalf("kept %d edits, want exactly %v", len(edits), remaining)
				}
				for i, edit := range edits {
					fields := inputClipTestObject(t, edit)
					if len(fields) != 2 || fields["old_string"] != "a" || fields["new_string"] != "b" {
						t.Fatalf("edit %d changed: %#v", i, fields)
					}
				}
			case "todo_array":
				todos := inputClipTestArray(t, decoded["todos"])
				if remaining == nil || len(todos) != *remaining {
					t.Fatalf("kept %d to-dos, want exactly %v", len(todos), remaining)
				}
				for i, todo := range todos {
					fields := inputClipTestObject(t, todo)
					if len(fields) != 3 || fields["content"] != "Do the work" || fields["activeForm"] != "Doing the work" || fields["status"] != "pending" {
						t.Fatalf("to-do %d changed: %#v", i, fields)
					}
				}
			case "question_array":
				questions := inputClipTestArray(t, decoded["questions"])
				if remaining == nil || len(questions) != *remaining {
					t.Fatalf("kept %d questions, want exactly %v", len(questions), remaining)
				}
				for i, question := range questions {
					fields := inputClipTestObject(t, question)
					options := inputClipTestArray(t, fields["options"])
					if fields["question"] != "Choose one?" || fields["header"] != "Choice" || fields["multi_select"] != false || len(options) != 1 {
						t.Fatalf("question %d changed: %#v", i, fields)
					}
					option := inputClipTestObject(t, options[0])
					if option["label"] != "A" || option["description"] != "First" {
						t.Fatalf("question %d option changed: %#v", i, option)
					}
				}
			case "wide_object":
				if values := inputClipTestArray(t, decoded["values"]); len(values) != 0 || arrayItems != 100 {
					t.Fatalf("array has %d items and marker says %d, want 0 and 100", len(values), arrayItems)
				}
				wide := inputClipTestObject(t, decoded["wide"])
				if remaining == nil || len(wide) != *remaining || objectFields != 2000-len(wide) {
					t.Fatalf("wide object has %d fields and marker says %d omitted, want exactly %v retained", len(wide), objectFields, remaining)
				}
				for i := 0; i < len(wide); i++ {
					key := fmt.Sprintf("a_long_field_name_with_many_bytes_%04d", i)
					if wide[key] != strconv.Itoa(i) {
						t.Fatalf("field %q changed or is out of order: %#v", key, wide[key])
					}
				}
			case "root_wide":
				if arrayItems != 0 {
					t.Fatalf("root-wide object omitted %d array items, want 0", arrayItems)
				}
				retained := len(decoded) - 1 // the synthetic marker is the only non-fixture field
				if remaining == nil || retained != *remaining || objectFields != 2000-retained {
					t.Fatalf("root kept %d fields and marker says %d omitted, want exactly %v retained", retained, objectFields, remaining)
				}
				for i := 0; i < retained; i++ {
					key := fmt.Sprintf("%s%04d", c.Expected.KeyPrefix, i)
					want := strconv.Itoa(i)
					if c.Expected.Value != nil {
						want = *c.Expected.Value
					}
					if decoded[key] != want {
						t.Fatalf("root field %q changed or is out of order: %#v, want %q", key, decoded[key], want)
					}
				}
			case "single_long_key":
				if len(decoded) != 1 || arrayItems != 0 || objectFields != 1 {
					t.Fatalf("single long key left %d fields and counts (%d, %d)", len(decoded), arrayItems, objectFields)
				}
			case "mixed_strings":
				long, ok := decoded["long"].(string)
				if !ok || !strings.HasPrefix(long, "abcdef") || !strings.HasSuffix(long, fmt.Sprintf(clipNoteFormat, 6000)) {
					t.Fatalf("long string did not go through the string passes: %q", long)
				}
				values := inputClipTestArray(t, decoded["values"])
				if remaining == nil || len(values) != *remaining {
					t.Fatalf("kept %d mixed rows, want exactly %v", len(values), remaining)
				}
				for i, row := range values {
					fields := inputClipTestArray(t, row)
					if len(fields) != 4 || fields[0] != "short" || fields[1] != strconv.Itoa(i) || fields[2] != true || fields[3] != nil {
						t.Fatalf("mixed row %d changed: %#v", i, fields)
					}
				}
			case "one_byte_over":
				values := inputClipTestArray(t, decoded["values"])
				if remaining == nil || len(values) != *remaining || arrayItems != 2430-len(values) || objectFields != 0 {
					t.Fatalf("boundary kept %d items with counts (%d, %d)", len(values), arrayItems, objectFields)
				}
				for i, value := range values {
					if value != "1234567" {
						t.Fatalf("boundary item %d changed: %#v", i, value)
					}
				}
				if decoded["pad"] != inputClipTestObject(t, expanded)["pad"] {
					t.Fatal("boundary padding changed")
				}
				// One more retained item, with the correspondingly shorter count in the marker, must not fit.
				decoded["values"] = append(values, "1234567")
				decoded[markerKey] = inputClipMarkerValue(len(input), arrayItems-1, objectFields)
				if restored := marshalPlain(decoded); len(restored) <= maxInputLen {
					t.Fatalf("one more item would still fit in %d bytes; the cut is not minimal", len(restored))
				}
			case "nested_empty":
				a := inputClipTestObject(t, decoded["a"])
				if small := inputClipTestArray(t, a["small"]); len(small) != 0 {
					t.Fatalf("small array kept %d items, want none", len(small))
				}
				wide := inputClipTestObject(t, a["wide"])
				if remaining == nil || len(wide) != *remaining {
					t.Fatalf("nested wide object kept %d fields, want exactly %v", len(wide), remaining)
				}
				for i := 0; i < len(wide); i++ {
					key := fmt.Sprintf("k%04d", i)
					if wide[key] != float64(1) {
						t.Fatalf("nested field %q changed or is absent: %#v", key, wide[key])
					}
				}
				wide[fmt.Sprintf("k%04d", len(wide))] = float64(1)
				decoded[markerKey] = inputClipMarkerValue(len(input), arrayItems, objectFields-1)
				if restored := marshalPlain(decoded); len(restored) <= maxInputLen {
					t.Fatalf("one more nested field would still fit in %d bytes", len(restored))
				}
			case "unsorted_root":
				retained := len(decoded) - 1
				if remaining == nil || retained != *remaining || arrayItems != 0 {
					t.Fatalf("unsorted root kept %d fields and %d array items, want %v and zero", retained, arrayItems, remaining)
				}
				for _, key := range c.Expected.Absent {
					if _, exists := decoded[key]; exists {
						t.Fatalf("unsorted root retained field %q that sorts last", key)
					}
				}
				generated := c.Expected.Total - (objectFields - len(c.Expected.Absent))
				if generated != retained {
					t.Fatalf("unsorted root retained %d generated fields, want %d", retained, generated)
				}
				for i := 0; i < generated; i++ {
					key := fmt.Sprintf("%s%04d", c.Expected.KeyPrefix, i)
					if decoded[key] != strconv.Itoa(i) {
						t.Fatalf("unsorted root field %q changed or is absent: %#v", key, decoded[key])
					}
				}
			case "two_arrays":
				for _, tc := range []struct {
					name string
					want int
				}{{"a", c.Expected.ARemaining}, {"b", c.Expected.BRemaining}} {
					rows := inputClipTestArray(t, decoded[tc.name])
					if len(rows) != tc.want {
						t.Fatalf("array %q kept %d rows, want exactly %d", tc.name, len(rows), tc.want)
					}
					for i, row := range rows {
						fields := inputClipTestArray(t, row)
						if len(fields) != 3 || fields[0] != strconv.Itoa(i) || fields[1] != strings.Repeat("a", 17) || fields[2] != strings.Repeat("b", 18) {
							t.Fatalf("array %q row %d changed: %#v", tc.name, i, fields)
						}
					}
				}
			case "integer_keys":
				rows := inputClipTestObject(t, decoded["rows"])
				if remaining == nil || len(rows) != *remaining || arrayItems != 0 {
					t.Fatalf("integer-key object kept %d fields, want exactly %v", len(rows), remaining)
				}
				keys := make([]string, 4000)
				for i := range keys {
					keys[i] = strconv.Itoa(i)
				}
				sort.Strings(keys)
				for _, key := range keys[:len(rows)] {
					if rows[key] != key {
						t.Fatalf("integer-like field %q changed or is absent: %#v", key, rows[key])
					}
				}
				if keys[len(rows)-1] != c.Expected.LastKey {
					t.Fatalf("last retained integer-like key is %q, want %q", keys[len(rows)-1], c.Expected.LastKey)
				}
			case "unicode_keys":
				rows := inputClipTestObject(t, decoded["rows"])
				originalRows := inputClipTestObject(t, inputClipTestObject(t, expanded)["rows"])
				if remaining == nil || len(rows) != *remaining || arrayItems != 0 {
					t.Fatalf("Unicode-key object kept %d fields, want exactly %v", len(rows), remaining)
				}
				for _, tc := range []struct {
					prefixes []string
					present  bool
				}{{c.Expected.Present, true}, {c.Expected.Absent, false}} {
					for _, prefix := range tc.prefixes {
						key := prefix + strings.Repeat("k", 8000)
						if _, exists := originalRows[key]; !exists {
							t.Fatalf("original Unicode key with prefix %q is absent", prefix)
						}
						if _, exists := rows[key]; exists != tc.present {
							t.Fatalf("Unicode key with prefix %q has presence %v, want %v", prefix, exists, tc.present)
						}
					}
				}
			case "marker_collision_base":
				if decoded[inputClipMarker] != "keep this user value" || markerKey != inputClipMarker+"_2" {
					t.Fatalf("base marker collision changed the user field or suffix: %#v, %q", decoded[inputClipMarker], markerKey)
				}
				values := inputClipTestArray(t, decoded["values"])
				if remaining == nil || len(values) != *remaining || arrayItems != 4000-len(values) {
					t.Fatalf("base collision kept %d rows with %d omissions, want exactly %v", len(values), arrayItems, remaining)
				}
			case "marker_collision":
				if decoded[inputClipMarker] != "keep this user value" || decoded[inputClipMarker+"_2"] != "keep this second user value" {
					t.Fatalf("user marker-like fields changed: %#v / %#v", decoded[inputClipMarker], decoded[inputClipMarker+"_2"])
				}
				values := inputClipTestArray(t, decoded["values"])
				if remaining == nil || len(values) != *remaining || arrayItems != 4000-len(values) {
					t.Fatalf("collision kept %d rows with %d omissions, want exactly %v", len(values), arrayItems, remaining)
				}
			default:
				t.Fatalf("unknown shared check %q", c.Expected.Check)
			}
		})
	}
}
