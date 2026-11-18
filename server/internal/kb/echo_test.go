package kb

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// meetingEchoFixtures are the cases the hub and the iOS app share (spec/fixtures/meeting-echo).
const meetingEchoFixtures = "../../../spec/fixtures/meeting-echo/cases.json"

// Every shared case: the hub's rule (isEcho) says what the phone's does (#5905).
func TestMeetingEchoFollowsTheSharedCases(t *testing.T) {
	raw, err := os.ReadFile(meetingEchoFixtures)
	if err != nil {
		t.Fatal(err)
	}
	var fixtures struct {
		Version int `json:"version"`
		Cases   []struct {
			Name  string `json:"name"`
			Input struct {
				Line  RecordingSegment `json:"line"`
				Spans []SpokenSpan     `json:"spans"`
			} `json:"input"`
			Expected struct {
				Echo *bool `json:"echo"`
			} `json:"expected"`
		} `json:"cases"`
	}
	if err := json.Unmarshal(raw, &fixtures); err != nil {
		t.Fatal(err)
	}
	if fixtures.Version != 1 || len(fixtures.Cases) < 20 {
		t.Fatalf("version %d, %d cases", fixtures.Version, len(fixtures.Cases))
	}
	for _, c := range fixtures.Cases {
		if c.Expected.Echo == nil || len(c.Input.Spans) == 0 {
			t.Fatalf("%s: no expected echo or no spans", c.Name)
		}
		l := c.Input.Line
		if got := isEcho(l.Start, l.End, l.Text, c.Input.Spans); got != *c.Expected.Echo {
			t.Errorf("%s: echo %v, want %v", c.Name, got, *c.Expected.Echo)
		}
		// The same through an upload: the phone marked nothing, the hub marks it by the spans.
		segs := []RecordingSegment{l}
		markEcho(segs, c.Input.Spans)
		if segs[0].Echo != *c.Expected.Echo {
			t.Errorf("%s: marked %v, want %v", c.Name, segs[0].Echo, *c.Expected.Echo)
		}
	}
}

// The normalization: NFKC, lower case, letters and decimal digits only.
func TestMeetingEchoNormal(t *testing.T) {
	for in, want := range map[string]string{
		"You kept the CAP — for this release!": "youkeptthecapforthisrelease",
		"这一版先保留４ＭＢ的上限。":                        "这一版先保留4mb的上限",
		"回帰ﾃｽﾄ、OK？":                            "回帰テストok",
		"Cafe\u0301\u0301 \u2460":              "café1", // composed, the extra mark goes; ① is 1 after NFKC
		"……！":                                  "",
	} {
		if got := string(echoNormal(in)); got != want {
			t.Errorf("%q: %q, want %q", in, got, want)
		}
	}
}

// spoken is read leniently: a malformed span or list is left out, never an error; text is cut at a character
// boundary.
func TestReadSpoken(t *testing.T) {
	long := strings.Repeat("答", maxSpokenText) // 3 bytes each
	for _, c := range []struct {
		name, raw string
		want      []SpokenSpan
	}{
		{"absent", ``, nil},
		{"null", `null`, nil},
		{"not a list", `"yes"`, nil},
		{"an object", `{"start":1,"end":2}`, nil},
		{"malformed spans left out", `[1, "x", {"start":"a","end":2}, {"start":1}, {"start":5,"end":4}, {"start":1,"end":2,"text":7},
			{"start":10,"end":17.5,"text":"You kept the cap."}, {"start":20,"end":21}]`,
			[]SpokenSpan{{Start: 10, End: 17.5, Text: "You kept the cap."}, {Start: 20, End: 21}}},
		{"text cut", `[{"start":0,"end":1,"text":"` + long + `"}]`, []SpokenSpan{{Start: 0, End: 1, Text: strings.Repeat("答", maxSpokenText/3)}}},
	} {
		got := readSpoken(json.RawMessage(c.raw))
		if len(got) != len(c.want) {
			t.Errorf("%s: %+v, want %+v", c.name, got, c.want)
			continue
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Errorf("%s: %+v, want %+v", c.name, got[i], c.want[i])
			}
		}
	}
	over := "[" + strings.TrimSuffix(strings.Repeat(`{"start":1,"end":2,"text":"a"},`, maxSpokenSpans+1), ",") + "]"
	if got := readSpoken(json.RawMessage(over)); got != nil {
		t.Errorf("a list over the limit: %d spans", len(got))
	}
}

// ---- hub transcription (#3862 phase 5) ----

// The hub's sentences are judged by the same rule against the timeline's speaking spans: a line that overlaps a span
// (widened by half a second) and says its answer; a span without text keeps the time rule; extra is a span assumed for
// the sentences held (never in the timeline).
func TestEchoOfTheTimeline(t *testing.T) {
	tl := Timeline{Speaking: []SpokenSpan{
		{Start: 10, End: 20, Text: "We keep the cap for this release."},
		{Start: 30, End: 31}, // saved without its answer: the time rule
	}}
	for _, c := range []struct {
		name       string
		start, end float64
		text       string
		want       bool
	}{
		{"the answer, inside", 12, 18, "We keep the cap for this release.", true},
		{"the answer, in the margin before", 9.6, 10.2, "the cap", true},
		{"the answer, after the margin", 20.6, 25, "We keep the cap for this release.", false},
		{"someone else inside the span", 12, 14, "Stop for a moment please.", false},
		{"the answer's words merged with someone's, under the threshold", 15, 21, "the cap, but who owns the regression test and the docs?", false},
		{"a span without text: half inside", 30, 31.8, "anything at all", true},
		{"a span without text: under half (no margin)", 30.6, 32, "anything at all", false},
		{"no span near", 50, 52, "We keep the cap for this release.", false},
	} {
		if got := EchoOf(tl, c.start, c.end, c.text); got != c.want {
			t.Errorf("%s: %v, want %v", c.name, got, c.want)
		}
	}
	extra := SpokenSpan{Start: 60, End: 66, Text: "Bo owns the regression test."}
	if !EchoOf(tl, 61, 64, "Bo owns the regression test", extra) || EchoOf(tl, 61, 64, "Bo owns the regression test") {
		t.Error("an assumed span marks by its answer only while it is given")
	}
	if EchoOf(Timeline{}, 12, 18, "We keep the cap for this release.") {
		t.Error("no spans: echo")
	}
	// Held back: a sentence that ends after an open span began, less the margin.
	for _, c := range []struct {
		end  float64
		want bool
	}{{99.4, false}, {99.5, false}, {99.6, true}, {120, true}} {
		if got := EchoHeld(c.end, 100); got != c.want {
			t.Errorf("held ending at %v: %v", c.end, got)
		}
	}
}

// The echo rule stays in one place (echo.go: EchoOf, EchoHeld, EchoSpanClosed over isEcho): no other file of the
// knowledge base or the assistant widens spans by the margin or reads the timeline's speaking spans itself, so a
// refinement of the rule changes echo.go and no caller.
func TestEchoRuleIsOneFunction(t *testing.T) {
	allowed := map[string]bool{"echo.go": true, "transcription.go": true} // transcription.go keeps the timeline's union
	speaking := regexp.MustCompile(`\.Speaking\b|\becho(Margin|Containment)\b|\bechoMatches\b`)
	for _, dir := range []string{".", filepath.Join("..", "assistant")} {
		files, err := filepath.Glob(filepath.Join(dir, "*.go"))
		if err != nil {
			t.Fatal(err)
		}
		if len(files) == 0 {
			t.Fatalf("no Go files in %s", dir)
		}
		for _, f := range files {
			if strings.HasSuffix(f, "_test.go") || dir == "." && allowed[filepath.Base(f)] {
				continue
			}
			raw, err := os.ReadFile(f)
			if err != nil {
				t.Fatal(err)
			}
			for i, line := range strings.Split(string(raw), "\n") {
				if code, _, _ := strings.Cut(line, "//"); speaking.MatchString(code) {
					t.Errorf("%s:%d judges sentences against the speaking spans itself (use kb.EchoOf, kb.EchoHeld, kb.EchoSpanClosed): %s",
						f, i+1, strings.TrimSpace(line))
				}
			}
		}
	}
}

// A timeline closes an open span with a span that begins at or before it and ends after it; one that only touches it
// (ends where it began) or begins after it does not.
func TestEchoSpanClosed(t *testing.T) {
	tl := Timeline{Speaking: []SpokenSpan{{Start: 1.0, End: 1.65}, {Start: 1.65, End: 1.7, Text: "x"}, {Start: 3, End: 4}}}
	for _, c := range []struct {
		from, to float64
		ok       bool
	}{{1.65, 1.7, true}, {1.2, 1.65, true}, {3, 4, true}, {1.7, 0, false}, {2, 0, false}, {5, 0, false}} {
		if to, ok := EchoSpanClosed(tl, c.from); ok != c.ok || to != c.to {
			t.Errorf("open from %v: %v %v", c.from, to, ok)
		}
	}
}

// The timeline's union: overlapping and touching spans made one, their texts joined in time order; a span received
// again (every start and upload sends the whole timeline) counts once; the joined text is cut like a span's.
func TestSpokenUnion(t *testing.T) {
	a := SpokenSpan{Start: 10, End: 17, Text: "We keep the cap."}
	b := SpokenSpan{Start: 17, End: 20, Text: "Bo owns the test."}
	c := SpokenSpan{Start: 30, End: 31}
	var tl Timeline
	if !tl.Merge(Timeline{Speaking: []SpokenSpan{b, c, a}}) {
		t.Fatal("no change")
	}
	want := []SpokenSpan{{Start: 10, End: 20, Text: "We keep the cap. Bo owns the test."}, {Start: 30, End: 31}}
	if len(tl.Speaking) != 2 || tl.Speaking[0] != want[0] || tl.Speaking[1] != want[1] {
		t.Fatalf("union %+v", tl.Speaking)
	}
	for _, again := range [][]SpokenSpan{{a, b, c}, {a}, {b}, {{Start: 10.5, End: 16, Text: "We keep the cap."}}, {{Start: 12, End: 13}}} {
		if tl.Merge(Timeline{Speaking: again}) {
			t.Errorf("received again %+v: changed to %+v", again, tl.Speaking)
		}
	}
	// A span with text over a span without: the text is the span's now; one that overlaps grows it.
	if !tl.Merge(Timeline{Speaking: []SpokenSpan{{Start: 30.5, End: 32, Text: "Later."}}}) ||
		tl.Speaking[1] != (SpokenSpan{Start: 30, End: 32, Text: "Later."}) {
		t.Fatalf("union %+v", tl.Speaking)
	}
	long := strings.Repeat("答", maxSpokenText/3) + "a" // maxSpokenText bytes
	var big Timeline
	big.Merge(Timeline{Speaking: []SpokenSpan{{Start: 0, End: 5, Text: long}, {Start: 4, End: 9, Text: "and then some"}}})
	if len(big.Speaking) != 1 || big.Speaking[0].Text != long || big.Speaking[0].End != 9 {
		t.Fatalf("a joined text past the limit: %d bytes, %+v", len(big.Speaking[0].Text), big.Speaking[0].End)
	}
	if big.Merge(Timeline{Speaking: []SpokenSpan{{Start: 4, End: 9, Text: "and then some"}}}) {
		t.Error("a span cut away and received again changed the union")
	}
}
