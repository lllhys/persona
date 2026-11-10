package kb

import (
	"maps"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"
)

// dayKeysRef is how mergeDayLines collected what a day file holds before continuation lines were joined once
// (verbatim): the reference dayKeys must match.
func dayKeysRef(rows []string) map[string]bool {
	have := map[string]bool{}
	for i := 0; i < len(rows); i++ {
		if !strings.HasPrefix(rows[i], "- ") {
			continue
		}
		msg := rows[i]
		for j := i + 1; j < len(rows) && strings.HasPrefix(rows[j], "  "); j++ {
			msg += "\n" + rows[j]
		}
		have[dupKey(msg)] = true
	}
	return have
}

// rezoneDayRef is how settleChatLocked read a day file before continuation lines were joined once (verbatim, with
// the entries collected in file order instead of by their new day): the reference rezoneDay must match.
func rezoneDayRef(raw, d string, from, to *time.Location, chatName *string) []rezoneEntry {
	var out []rezoneEntry
	var cur *rezoneEntry
	flush := func() {
		if cur != nil {
			out = append(out, *cur)
			cur = nil
		}
	}
	for _, l := range strings.Split(raw, "\n") {
		switch {
		case strings.HasPrefix(l, "# "):
			if *chatName == "" {
				*chatName = strings.TrimSuffix(l[2:], " · "+d)
			}
		case strings.HasPrefix(l, "- "):
			flush()
			at, err := time.ParseInLocation("2006-01-02 15:04", d+" "+safeSlice(l, 2, 7), from)
			if err != nil {
				// Not a message line as Line.Format writes them: kept on its day, as it is.
				at, _ = time.ParseInLocation("2006-01-02", d, to)
				cur = &rezoneEntry{at: at, text: l}
				continue
			}
			cur = &rezoneEntry{at: at, text: "- " + at.In(to).Format("15:04") + l[7:], oldDay: d, old: l}
		case strings.HasPrefix(l, "  ") && cur != nil:
			cur.text += "\n" + l
			if cur.old != "" {
				cur.old += "\n" + l
			}
		default:
			flush()
		}
	}
	flush()
	return out
}

// dayRows splits a day file into rows as mergeDayLines does.
func dayRows(text string) []string {
	var rows []string
	if text != "" {
		rows = strings.Split(text, "\n")
	}
	if strings.HasSuffix(text, "\n") {
		rows = rows[:len(rows)-1]
	}
	return rows
}

func checkDayKeys(t *testing.T, name, text string) {
	t.Helper()
	rows := dayRows(text)
	if got, want := dayKeys(rows), dayKeysRef(rows); !maps.Equal(got, want) {
		t.Errorf("%s: %d keys, want %d\ngot  %q\nwant %q", name, len(got), len(want),
			slices.Sorted(maps.Keys(got)), slices.Sorted(maps.Keys(want)))
	}
}

func checkRezoneDay(t *testing.T, name, raw string, from, to *time.Location, chat string) {
	t.Helper()
	gotName, wantName := chat, chat
	got := rezoneDay(raw, "2026-10-20", from, to, &gotName)
	want := rezoneDayRef(raw, "2026-10-20", from, to, &wantName)
	if gotName != wantName {
		t.Errorf("%s %v->%v: chat name %q, want %q", name, from, to, gotName, wantName)
	}
	if len(got) != len(want) {
		t.Errorf("%s %v->%v: %d entries, want %d", name, from, to, len(got), len(want))
		return
	}
	for i := range got {
		if !got[i].at.Equal(want[i].at) || got[i].oldDay != want[i].oldDay {
			t.Errorf("%s %v->%v entry %d: at %v day %q, want %v %q", name, from, to, i, got[i].at, got[i].oldDay,
				want[i].at, want[i].oldDay)
		}
		sameText(t, name+" text", got[i].text, want[i].text)
		sameText(t, name+" old", got[i].old, want[i].old)
	}
}

func rezoneZones(t testing.TB) [][2]*time.Location {
	t.Helper()
	sh, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Skip("no zone data")
	}
	return [][2]*time.Location{{time.UTC, sh}, {sh, time.UTC}, {time.Local, sh}, {time.UTC, time.Local}}
}

// Merging history, ingest's late lines and moving zones join a message's continuation lines once; what they read
// from a day file is the same as before, byte for byte.
func TestMergeDayKeysJoinContinuationsAsBefore(t *testing.T) {
	for name, raw := range continuationFixtures {
		checkDayKeys(t, name, raw)
	}
	if got := dayKeys(dayRows(continuationFixtures["long"])); len(got) != 3 {
		t.Fatalf("long: %d keys", len(got))
	}
}

func TestRezoneDayJoinsContinuationsAsBefore(t *testing.T) {
	for _, z := range rezoneZones(t) {
		for name, raw := range continuationFixtures {
			if name == "long" {
				continue // the reference is quadratic in it: checked once below
			}
			checkRezoneDay(t, name, raw, z[0], z[1], "")
			checkRezoneDay(t, name, raw, z[0], z[1], "named")
		}
	}
	sh := rezoneZones(t)[0][1]
	checkRezoneDay(t, "long", continuationFixtures["long"], time.UTC, sh, "")
	name := ""
	got := rezoneDay(continuationFixtures["long"], "2026-10-20", time.UTC, sh, &name)
	if len(got) != 3 || strings.Count(got[1].text, "\n") != 5000 || !strings.HasPrefix(got[1].text, "- 17:30 Bob: the log:\n") ||
		strings.TrimPrefix(got[1].old, "- 09:30") != strings.TrimPrefix(got[1].text, "- 17:30") || name != "Build chat" {
		t.Fatalf("long: %d entries, name %q", len(got), name)
	}
}

// allocated is how many bytes f allocates.
func allocated(f func()) uint64 {
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	f()
	runtime.ReadMemStats(&after)
	return after.TotalAlloc - before.TotalAlloc
}

// One message with 5,000 continuation lines (a pasted log): adding a line to its day used to copy the message once
// per line, about 820 MB for a 320 KB day.
func TestMergeDayLongContinuationAllocatesItsSize(t *testing.T) {
	day := longDay(5000)
	l := Line{At: time.Date(2026, 10, 20, 9, 45, 0, 0, time.Local), Sender: "C", Text: "late"}
	var out string
	n := allocated(func() { out, _ = mergeDayLines(day, []Line{l}, time.Local, true) })
	if n > uint64(40*len(day)) {
		t.Fatalf("merging one line into a %d-byte day allocated %d bytes", len(day), n)
	}
	if !strings.Contains(out, "finished in 52 ms\n- 09:45 C: late\n- 10:00 Alice: after the paste\n") {
		t.Fatalf("late line not in place: %q", out[len(out)-200:])
	}
}

// The same day moved from UTC into another zone used to cost about 1.6 GB.
func TestRezoneLongContinuationAllocatesItsSize(t *testing.T) {
	sh := rezoneZones(t)[0][1]
	st, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	dir := st.path(KindChat, "lark-long", "messages")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	day := longDay(5000)
	if err := os.WriteFile(filepath.Join(dir, "2026-10-20.md"), []byte(day), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, zoneFile), []byte("UTC\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	var moved int
	n := allocated(func() { moved, err = st.Rezone(sh) })
	if err != nil || moved != 1 {
		t.Fatalf("moved %d, %v", moved, err)
	}
	if n > uint64(40*len(day)) {
		t.Fatalf("moving a %d-byte day allocated %d bytes", len(day), n)
	}
	got := readDay(t, st, "lark-long", "2026-10-20")
	want := strings.NewReplacer("- 09:00 Alice", "- 17:00 Alice", "- 09:30 Bob", "- 17:30 Bob", "- 10:00 Alice", "- 18:00 Alice").Replace(day)
	sameText(t, "moved day", got, want)
}

func FuzzMergeDayKeysAndRezoneDay(f *testing.F) {
	for name, raw := range continuationFixtures {
		if name != "long" {
			f.Add(raw)
		}
	}
	f.Add(longDay(20))
	zones := rezoneZones(f)
	f.Fuzz(func(t *testing.T, raw string) {
		checkDayKeys(t, "fuzz", raw)
		for _, z := range zones {
			checkRezoneDay(t, "fuzz", raw, z[0], z[1], "")
		}
		if t.Failed() {
			t.Fatalf("input %q", raw)
		}
	})
}
