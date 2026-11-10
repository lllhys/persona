package kb

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

func TestSinceCutTail(t *testing.T) {
	for _, c := range []struct {
		s     string
		limit int
		want  string
	}{
		{"## d\n- a\n", 100, "## d\n- a\n"},
		{"## d\n- a\n", 9, "## d\n- a\n"}, // fits exactly
		{"## d\n- a\n", 8, "…\n- a\n"},
		{"## d\n- a\n", 4, "…\n- a\n"},        // the kept part starts on the message line
		{"## d\n- abc\n- d\n", 8, "…\n- d\n"}, // inside a message: forward to the next one
		{"## d\n- abc\n## e\n- d\n", 10, "…\n## e\n- d\n"},
		// The next message start would drop more than half of what fits: the next line instead.
		{"- a\n  1234567890\n  12\n- b\n", 14, "…\n  12\n- b\n"},
		// The next line start is the next message and too far as well: the next character, so the long message's tail
		// fills what fits instead of being dropped with it.
		{"- 09:00 A: " + strings.Repeat("x", 120) + "\n- 09:01 B: last\n", 64, "…\n" + strings.Repeat("x", 47) + "\n- 09:01 B: last\n"},
		{"- a\n  " + strings.Repeat("x", 40) + "\n  y\n- b\n", 20, "…\n" + strings.Repeat("x", 11) + "\n  y\n- b\n"}, // the next line too
		// No line starts inside: the next character, never half of one.
		{"- 我们今天\n", 7, "…\n今天\n"},
		{"- 我们今天\n", 6, "…\n天\n"},
		{"- 我们今天\n", 1, "…\n\n"},
		{"- a\n", 0, "…\n"},
		{"- a\n", -1, "…\n"},
		{"", 0, ""},
	} {
		if got := cutTail(c.s, c.limit); got != c.want {
			t.Errorf("cutTail(%q, %d) = %q, want %q", c.s, c.limit, got, c.want)
		}
	}
}

// A cut through Chinese text keeps whole characters, at every size.
func TestSinceKeepsWholeCharacters(t *testing.T) {
	st, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	c := &Chat{ID: "lark-cjk", Name: "群"}
	at := time.Date(2026, 10, 20, 9, 0, 0, 0, st.Zone())
	if err := st.AppendMessages(c, []Line{{At: at, Sender: "张三", Text: "我们今天讨论一下发布计划\n第二行也是中文"},
		{At: at.Add(time.Minute), Sender: "李四", Text: "好的"}}); err != nil {
		t.Fatal(err)
	}
	full := st.Since(c.ID, at.Add(-time.Hour), 1<<20)
	for max := 0; max <= len(full)+1; max++ {
		out := st.Since(c.ID, at.Add(-time.Hour), max)
		if !utf8.ValidString(out) {
			t.Fatalf("max %d: invalid UTF-8 %q", max, out)
		}
		if kept, cut := strings.CutPrefix(out, "…\n"); cut && (len(kept) > max || !strings.HasSuffix(full, kept)) {
			t.Fatalf("max %d: %q is not a tail of %q within the limit", max, out, full)
		} else if !cut && out != full {
			t.Fatalf("max %d: %q, want the whole %q", max, out, full)
		}
	}
}

// A long last-but-one message is cut inside, not dropped whole: through Since, which compression summarizes and then
// marks covered, a dropped tail would never reach a summary.
func TestSinceKeepsTheTailOfALongMessage(t *testing.T) {
	st, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	c := &Chat{ID: "lark-long", Name: "Long"}
	at := time.Date(2026, 10, 20, 9, 0, 0, 0, st.Zone())
	if err := st.AppendMessages(c, []Line{{At: at, Sender: "A", Text: strings.Repeat("x", 120)},
		{At: at.Add(time.Minute), Sender: "B", Text: "last"}}); err != nil {
		t.Fatal(err)
	}
	full := st.Since(c.ID, at.Add(-time.Hour), 1<<20)
	const max = 64
	out := st.Since(c.ID, at.Add(-time.Hour), max)
	kept, cut := strings.CutPrefix(out, "…\n")
	if !cut || len(kept) != max || !strings.HasSuffix(full, kept) || !strings.HasPrefix(kept, "xxx") {
		t.Fatalf("Since(%d) = %q, want the last %d bytes of %q", max, out, max, full)
	}
}

// A backlog of 200 days (10 MB) costs the newest days that fill the answer, not all of them (before: 92 MB allocated
// for a 24 KB answer).
func TestSinceReadsOnlyTheDaysItKeeps(t *testing.T) {
	st, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	dir := st.path(KindChat, "lark-backlog", "messages")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	start := time.Date(2025, 10, 20, 0, 0, 0, 0, st.Zone())
	body := strings.Repeat("x", 1000)
	var last string
	for d := range 200 {
		day := start.AddDate(0, 0, d).Format("2006-01-02")
		var b strings.Builder
		fmt.Fprintf(&b, "# g · %s\n\n", day)
		for i := range 50 {
			last = fmt.Sprintf("- 09:%02d a: %s %s", i, day, body)
			b.WriteString(last + "\n")
		}
		if err := os.WriteFile(filepath.Join(dir, day+".md"), []byte(b.String()), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	var out string
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	out = st.Since("lark-backlog", start.Add(-time.Hour), compressInput)
	runtime.ReadMemStats(&after)
	if n := after.TotalAlloc - before.TotalAlloc; n > 2<<20 {
		t.Errorf("Since allocated %d bytes for a %d-byte answer", n, compressInput)
	}
	kept, cut := strings.CutPrefix(out, "…\n")
	if !cut || len(kept) > compressInput || !strings.HasPrefix(kept, "- ") || !strings.HasSuffix(kept, last+"\n") {
		t.Fatalf("since: %d bytes, starting %q", len(out), out[:min(len(out), 40)])
	}
}
