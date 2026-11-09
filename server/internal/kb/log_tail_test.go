package kb

import (
	"bytes"
	"errors"
	"fmt"
	"math"
	"math/rand/v2"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"
)

// oldTodoOutcomes is TodoOutcomes before it read the timeline from its end: every line of the timeline as a string,
// then the todo lines walked back from the last.
func oldTodoOutcomes(raw []byte, n int) []TodoOutcome {
	if n <= 0 {
		return nil
	}
	var out []TodoOutcome
	lines := oldLogLines(raw, 1<<30)
	for i := len(lines) - 1; i >= 0 && len(out) < n; i-- {
		if o, ok := parseTodoLine(lines[i]); ok {
			out = append(out, o)
		}
	}
	return out
}

// splitBack is what linesBack hands out, the plain way: every line of raw, last first.
func splitBack(raw []byte) []string {
	lines := strings.Split(string(raw), "\n")
	slices.Reverse(lines)
	return lines
}

// collectBack runs linesBack over raw with chunk, keeping at most most lines (all with most < 0).
func collectBack(t *testing.T, raw []byte, chunk, most int) []string {
	t.Helper()
	var got []string
	err := linesBack(bytes.NewReader(raw), int64(len(raw)), chunk, func(l []byte) bool {
		got = append(got, string(l))
		return most < 0 || len(got) < most
	})
	if err != nil {
		t.Fatal(err)
	}
	return got
}

// Read from the end a chunk at a time, a file comes apart into the same lines as split whole, for any chunk size:
// chunks that end inside a multibyte character or a CRLF, lines longer than a chunk, no trailing newline, no newline.
func TestLinesBackMatchesSplit(t *testing.T) {
	pieces := []string{"\n", "\n", "a", "- ", "-", " ", "\r\n", "ü", "€", "😀", "·", strings.Repeat("€", 40)}
	rng := rand.New(rand.NewPCG(1, 2))
	var texts [][]byte
	for _, s := range []string{"", "\n", "\n\n", "a", "a\n", "\na", "- a\n- b", strings.Repeat("😀", 100)} {
		texts = append(texts, []byte(s))
	}
	for range 200 {
		var b strings.Builder
		for range rng.IntN(120) {
			b.WriteString(pieces[rng.IntN(len(pieces))])
		}
		texts = append(texts, []byte(b.String()))
	}
	for _, raw := range texts {
		want := splitBack(raw)
		for _, chunk := range []int{1, 2, 3, 4, 5, 7, 16, 61, logChunk} {
			if got := collectBack(t, raw, chunk, -1); !reflect.DeepEqual(got, want) {
				t.Fatalf("chunk %d, %q: %q, want %q", chunk, raw, got, want)
			}
			if most := 1 + len(want)/2; !reflect.DeepEqual(collectBack(t, raw, chunk, most), want[:most]) {
				t.Fatalf("chunk %d, %q, stopped after %d: not the first lines from the end", chunk, raw, most)
			}
		}
	}
}

// A read error ends the scan with it.
func TestLinesBackReportsAReadError(t *testing.T) {
	if err := linesBack(failingReader{}, 10, 4, func([]byte) bool { return true }); !errors.Is(err, errRead) {
		t.Fatalf("err = %v", err)
	}
}

var errRead = errors.New("read failed")

type failingReader struct{}

func (failingReader) ReadAt([]byte, int64) (int, error) { return 0, errRead }

// randomTimeline is a timeline as AppendLog and the others write it, and some it does not: lines of every writer
// (todo lines among them, and lines that only look like them) with multibyte text, continuation and blank lines,
// sometimes a line longer than two chunks, with or without a header and a trailing newline.
func randomTimeline(rng *rand.Rand) []byte {
	var lines []string
	if rng.IntN(4) > 0 {
		lines = append(lines, logTitle, "")
	}
	words := []string{"moved to Thursday", "über", "€12", "😀", "·", "x"}
	text := func() string {
		var b strings.Builder
		for range 1 + rng.IntN(12) {
			b.WriteString(words[rng.IntN(len(words))] + " ")
		}
		return strings.TrimSpace(b.String())
	}
	at := time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)
	for i := range rng.IntN(4000) {
		when := at.Add(time.Duration(i) * time.Minute).Format("2006-01-02 15:04")
		switch r := rng.IntN(21); {
		case r < 10:
			lines = append(lines, fmt.Sprintf("- %s · lark · %s", when, text()))
		case r < 13:
			lines = append(lines, fmt.Sprintf("- %s · %s%s · %s%s", when, TodoLogSource, TodoLogTitle(text()), TodoOutcomePrefix, text()))
		case r < 14: // a chat whose name starts like a todo's source: no outcome
			lines = append(lines, fmt.Sprintf("- %s · %sweekly · %s", when, TodoLogSource, text()))
		case r < 15: // a todo line with nothing after its prefix: no outcome
			lines = append(lines, fmt.Sprintf("- %s · %st · %s ", when, TodoLogSource, TodoOutcomePrefix))
		case r < 16: // a todo line whose time is a byte short or long: no outcome
			shifted := []string{when[1:], when + "0"}[rng.IntN(2)]
			lines = append(lines, fmt.Sprintf("- %s · %st · %s%s", shifted, TodoLogSource, TodoOutcomePrefix, text()))
		case r < 18:
			lines = append(lines, "  continued "+text())
		case r < 19:
			lines = append(lines, "", "-x")
		default:
			if rng.IntN(100) == 0 {
				lines = append(lines, fmt.Sprintf("- %s · %slong · %s%s", when, TodoLogSource, TodoOutcomePrefix,
					strings.Repeat("€😀", 2*logChunk/7+rng.IntN(100))))
			} else {
				lines = append(lines, "- "+text())
			}
		}
	}
	raw := strings.Join(lines, "\n")
	if rng.IntN(3) > 0 {
		raw += "\n"
	}
	return []byte(raw)
}

// Log and TodoOutcomes read the timeline from its end but return exactly what they did when they read all of it: the
// same lines and outcomes, in the same order, nil in the same cases, for any n.
func TestTailReadsMatchTheWholeRead(t *testing.T) {
	st := logStore(t, time.Local, nil)
	p := st.Path(KindProject, "p", "log.md")
	rng := rand.New(rand.NewPCG(3, 4))
	texts := map[string][]byte{}
	for k, v := range logTexts {
		texts[k] = []byte(v)
	}
	for i := range 40 {
		texts[fmt.Sprintf("random %d", i)] = randomTimeline(rng)
	}
	long := append([]byte(logTitle+"\n\n- "+TodoLogSource+"x · "+TodoOutcomePrefix), bytes.Repeat([]byte("€"), logChunk)...)
	texts["one line of three chunks"] = long
	big := 0
	for name, raw := range texts {
		if len(raw) > logChunk {
			big++
		}
		if err := writeFile(p, raw); err != nil {
			t.Fatal(err)
		}
		all := len(oldLogLines(raw, math.MaxInt))
		for _, n := range []int{0, 1, 2, 5, 30, all - 1, all, all + 1, 1 << 30, math.MaxInt} {
			if n < 0 {
				continue
			}
			if got, want := st.Log("p", n), oldLogLines(raw, n); !reflect.DeepEqual(got, want) || (got == nil) != (want == nil) {
				t.Errorf("Log, %s, n=%d: %d lines (nil %v), want %d (nil %v)", name, n, len(got), got == nil, len(want), want == nil)
			}
			if got, want := st.TodoOutcomes("p", n), oldTodoOutcomes(raw, n); !reflect.DeepEqual(got, want) {
				t.Errorf("TodoOutcomes, %s, n=%d: %d outcomes, want %d", name, n, len(got), len(want))
			}
		}
	}
	if big < 5 {
		t.Fatalf("only %d timelines longer than a chunk", big)
	}
	if got := st.TodoOutcomes("nope", 5); got != nil {
		t.Errorf("no timeline: %+v", got)
	}
}
