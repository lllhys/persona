package node

import (
	"bytes"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/nwverse/persona/server/internal/transcript"
)

// bgMarkerSet is every marker bgMatch looks for, as scanBackground tested them one by one before.
func bgMarkerSet() [][]byte {
	return slices.Concat(bgMarkers, transcript.GoalMarkers, transcript.UltracodeMarkers, transcript.PlanMarkers)
}

// The one-pass matcher says what testing each marker in turn says: on every line of the transcript samples, on each
// marker alone, inside other text and cut short by a byte, and where markers overlap or share a start.
func TestBGMatchSameAsContainsAny(t *testing.T) {
	markers, m := bgMarkerSet(), bgMatch()
	check := func(what string, line []byte) bool {
		t.Helper()
		want := containsAny(line, markers)
		if got := m.match(line); got != want {
			t.Fatalf("%s: match %v, containsAny %v on %.200q", what, got, want, line)
		}
		return want
	}
	files, err := filepath.Glob(filepath.Join("..", "transcript", "testdata", "*.jsonl"))
	if err != nil || len(files) == 0 {
		t.Fatalf("no transcript samples: %v", err)
	}
	lines, hits := 0, 0
	for _, f := range files {
		src, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for _, l := range bytes.SplitAfter(src, []byte("\n")) {
			lines++
			if check(filepath.Base(f), l) {
				hits++
			}
		}
	}
	if hits == 0 || hits == lines {
		t.Fatalf("the samples should hold lines with markers and without: %d of %d", hits, lines)
	}
	for _, k := range markers {
		check("marker", k)
		check("inside", []byte(`{"x":"`+string(k)+`"}`))
		check("cut short", k[:len(k)-1])
		check("cut short twice", append(slices.Clone(k[:len(k)-1]), k[:len(k)-1]...))
	}
	for _, l := range []string{`"name":"Tas"name":"TaskCreat"name":"Task"`, `Goal setGoal clearGoal set: `, `CronCreatCronDelet`, `in background with I`,
		`"name":"Agen"name":"Agent`, `ultra_effort_exi`, "", "\n"} {
		check("overlap", []byte(l))
	}
}

// FuzzBGMatch: on any line the matcher agrees with containsAny over all the markers.
func FuzzBGMatch(f *testing.F) {
	markers := bgMarkerSet()
	for _, k := range markers {
		f.Add(k)
		f.Add(k[:len(k)-1])
	}
	f.Add([]byte(`{"type":"assistant","message":{"content":[{"type":"tool_use","name":"TaskCreate","input":{}}]}}`))
	f.Fuzz(func(t *testing.T, line []byte) {
		if got, want := bgMatch().match(line), containsAny(line, markers); got != want {
			t.Fatalf("match %v, containsAny %v on %q", got, want, line)
		}
	})
}

// The table stays small: byte classes instead of 256 columns per state (42 classes and 366 states, 61,488 bytes, for
// the 28 markers; 256 columns would be 374,784). The bound is that plus 3%: a new marker that needs more says so here.
func TestBGMatchTableIsCompact(t *testing.T) {
	const most = 63_333
	if b := len(bgMatch().next) * 4; b > most {
		t.Fatalf("the table takes %d bytes, want at most %d", b, most)
	}
}

// BenchmarkBGMatch is the per-line marker test of a node's first scan, over the transcript samples' lines.
func BenchmarkBGMatch(b *testing.B) {
	files, _ := filepath.Glob(filepath.Join("..", "transcript", "testdata", "*.jsonl"))
	var lines [][]byte
	size := 0
	for _, f := range files {
		src, _ := os.ReadFile(f)
		for _, l := range bytes.SplitAfter(src, []byte("\n")) {
			if len(l) <= 64<<10 {
				lines, size = append(lines, l), size+len(l)
			}
		}
	}
	markers := bgMarkerSet()
	for _, c := range []struct {
		name  string
		match func([]byte) bool
	}{{"containsAny", func(l []byte) bool { return containsAny(l, markers) }}, {"matcher", bgMatch().match}} {
		b.Run(c.name, func(b *testing.B) {
			b.SetBytes(int64(size))
			for b.Loop() {
				for _, l := range lines {
					c.match(l)
				}
			}
		})
	}
}
