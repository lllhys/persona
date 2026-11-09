package kb

import (
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

// benchLogLines is the size of the timeline the project-log benchmarks read and append to (~1.3 MB).
const benchLogLines = 20000

// bigLog is a timeline of n lines in zone, as AppendLog writes them.
func bigLog(zone *time.Location, n int) []byte {
	var b strings.Builder
	b.WriteString(logHeader(zone))
	at := time.Date(2024, 1, 1, 9, 0, 0, 0, zone)
	for i := range n {
		fmt.Fprintf(&b, "- %s · lark · line %d: moved to Thursday, 发布改到周四\n",
			at.Add(time.Duration(i)*time.Hour).Format("2006-01-02 15:04"), i)
	}
	return []byte(b.String())
}

// todoTail is n todo lines (TodoOutcomes) as they follow a timeline written by bigLog.
func todoTail(n int) []byte {
	var b strings.Builder
	at := time.Date(2026, 11, 1, 9, 0, 0, 0, time.UTC)
	for i := range n {
		fmt.Fprintf(&b, "- %s · %s%s · %sdone %d\n", at.Add(time.Duration(i)*time.Minute).Format("2006-01-02 15:04"),
			TodoLogSource, TodoLogTitle(fmt.Sprintf("todo %d", i)), TodoOutcomePrefix, i)
	}
	return []byte(b.String())
}

// BenchmarkProjectLog: appending a line to, and reading the latest lines or todo outcomes of, a project's 20k-line
// timeline (with 5 todo lines at its end, or none: TodoOutcomes then walks all of it).
func BenchmarkProjectLog(b *testing.B) {
	setupWith := func(b *testing.B, raw []byte) (*Store, func()) {
		b.Helper()
		st, err := NewStore(b.TempDir())
		if err != nil {
			b.Fatal(err)
		}
		if _, err := st.Rezone(time.UTC); err != nil {
			b.Fatal(err)
		}
		reset := func() {
			if err := os.WriteFile(st.Path(KindProject, "p", "log.md"), raw, 0o600); err != nil {
				b.Fatal(err)
			}
		}
		if err := os.MkdirAll(st.Path(KindProject, "p"), 0o700); err != nil {
			b.Fatal(err)
		}
		reset()
		return st, reset
	}
	setup := func(b *testing.B) (*Store, func()) { return setupWith(b, bigLog(time.UTC, benchLogLines)) }
	b.Run("Append", func(b *testing.B) {
		st, reset := setup(b)
		at := time.Date(2026, 11, 1, 9, 0, 0, 0, time.UTC)
		b.ReportAllocs()
		b.ResetTimer()
		for i := range b.N {
			if i%1000 == 999 { // keep the timeline near its size: the old cost grew with it
				b.StopTimer()
				reset()
				b.StartTimer()
			}
			if err := st.AppendLog("p", at, "lark", "a new line"); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("Log30", func(b *testing.B) {
		st, _ := setup(b)
		b.ReportAllocs()
		b.ResetTimer()
		for range b.N {
			if got := st.Log("p", 30); len(got) != 30 {
				b.Fatalf("%d lines", len(got))
			}
		}
	})
	b.Run("TodoOutcomes5", func(b *testing.B) {
		st, _ := setupWith(b, append(bigLog(time.UTC, benchLogLines), todoTail(5)...))
		b.ReportAllocs()
		b.ResetTimer()
		for range b.N {
			if got := st.TodoOutcomes("p", 5); len(got) != 5 {
				b.Fatalf("%d outcomes", len(got))
			}
		}
	})
	b.Run("TodoOutcomesNone", func(b *testing.B) {
		st, _ := setup(b)
		b.ReportAllocs()
		b.ResetTimer()
		for range b.N {
			if got := st.TodoOutcomes("p", 5); len(got) != 0 {
				b.Fatalf("%d outcomes", len(got))
			}
		}
	})
}
