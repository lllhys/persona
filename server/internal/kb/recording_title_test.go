package kb

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/ai"
)

// titleModel answers each call with the next reply and keeps what it was asked; an error reply is returned instead.
type titleModel struct {
	replies []string
	errs    []error
	calls   [][2]string
}

func (m *titleModel) JSONMax(_ context.Context, system, user string, _ int, out any) error {
	m.calls = append(m.calls, [2]string{system, user})
	i := len(m.calls) - 1
	if i < len(m.errs) && m.errs[i] != nil {
		return m.errs[i]
	}
	reply := m.replies[min(i, len(m.replies)-1)]
	return json.Unmarshal([]byte(reply), out)
}

// talk is n lines of about 50 characters each, numbered so each upload's text is new.
func talk(from, n int) []string {
	var out []string
	for i := from; i < from+n; i++ {
		out = append(out, strings.Repeat("导出上限按条还是按录音算要先定下来，", 2)+"第"+string(rune('A'+i%26))+"段")
	}
	return out
}

// A recording nobody named gets a live title once it has enough text, another only when the text has doubled and ten
// minutes have passed (the model may keep the one it has), at most four; the summary then settles it, and a meeting's
// minutes over that, which a later summary does not undo.
func TestLiveTitleWrittenRefreshedAndSettled(t *testing.T) {
	m := &titleModel{replies: []string{`{"title":"「导出上限」","keep":false}`}}
	e := goldenEngine(t, m)
	start := time.Date(2026, 11, 10, 14, 0, 0, 0, goldenZone)
	now := start.Add(time.Minute)
	e.clock = func() time.Time { return now }
	ctx := context.Background()

	// Too little said: nothing due, the name is the start time, and the hub says no name.
	u := recUpload(1, start, false, talk(0, 3)...)
	u.Kind = RecordingMeeting
	put, err := e.PutRecording(recID, u, now)
	if err != nil || put.TitleDue || put.Name != "" || put.NameSource != "" {
		t.Fatalf("a short start: %+v %v", put, err)
	}
	if n := e.TitleRecordings(ctx, 2); n != 0 || len(m.calls) != 0 {
		t.Fatalf("titled a short start: %d", n)
	}
	// Enough: due, one call, its title (quotes gone) names it in lists and in the answers.
	u = recUpload(2, start, false, talk(0, 12)...)
	u.Kind = RecordingMeeting
	if put, _ = e.PutRecording(recID, u, now); !put.TitleDue {
		t.Fatalf("not due at %d chars", recordingNow(t, e, recID).Chars)
	}
	if n := e.TitleRecordings(ctx, 2); n != 1 {
		t.Fatalf("calls %d, want 1", n)
	}
	r := recordingNow(t, e, recID)
	if r.Name != "导出上限" || r.AITitleFrom != AITitleLive || r.NameSource() != NamedByModel || r.LiveTitles != 1 {
		t.Fatalf("after the first title: %q from %q (%s), %d calls", r.Name, r.AITitleFrom, r.NameSource(), r.LiveTitles)
	}
	if !strings.Contains(m.calls[0][1], "一场会议") || strings.Contains(m.calls[0][1], "现在的标题") {
		t.Errorf("the first call's input:\n%s", m.calls[0][1])
	}
	states, _ := e.RecordingStates()
	if len(states) != 1 || states[0].Name != "导出上限" || states[0].NameSource != NamedByModel {
		t.Fatalf("states %+v", states)
	}
	d, err := e.Get(KindRecording, recID, 100)
	if err != nil || !strings.Contains(string(d.Meta), `"name_source":"model"`) {
		t.Fatalf("detail.meta: %s %v", d.Meta, err)
	}
	if put, _ = e.PutRecording(recID, u, now); put.Name != "导出上限" || put.NameSource != NamedByModel || put.TitleDue {
		t.Fatalf("the answer after: %+v", put)
	}

	// Doubled but too soon: not asked. Ten minutes on: asked with the title it has, which it keeps.
	u = recUpload(3, start, false, talk(0, 25)...)
	u.Kind = RecordingMeeting
	now = start.Add(5 * time.Minute)
	putRecording(t, e, recID, u, now)
	if n := e.TitleRecordings(ctx, 2); n != 0 {
		t.Fatalf("asked again after 4 minutes: %d", n)
	}
	now = start.Add(12 * time.Minute)
	putRecording(t, e, recID, u, now)
	m.replies = []string{`{"title":"导出上限","keep":true}`}
	if n := e.TitleRecordings(ctx, 2); n != 1 || !strings.Contains(m.calls[1][1], "现在的标题：「导出上限」") {
		t.Fatalf("second call %d:\n%s", n, m.calls[len(m.calls)-1][1])
	}
	// Ten more minutes but the same text: not doubled, not asked.
	now = start.Add(25 * time.Minute)
	putRecording(t, e, recID, u, now)
	if n := e.TitleRecordings(ctx, 2); n != 0 {
		t.Fatalf("asked without new text: %d", n)
	}
	// The talk moves on: a new title.
	m.replies = []string{`{"title":"发布日期","keep":false}`}
	u = recUpload(4, start, false, talk(0, 60)...)
	u.Kind = RecordingMeeting
	putRecording(t, e, recID, u, now)
	if n := e.TitleRecordings(ctx, 2); n != 1 || recordingNow(t, e, recID).Name != "发布日期" {
		t.Fatalf("third call %d, name %q", n, recordingNow(t, e, recID).Name)
	}
	// A failure counts: the fourth is the last.
	m.errs = []error{nil, nil, nil, errors.New("unusable")}
	u = recUpload(5, start, false, talk(0, 130)...)
	u.Kind = RecordingMeeting
	now = start.Add(40 * time.Minute)
	putRecording(t, e, recID, u, now)
	if n := e.TitleRecordings(ctx, 2); n != 1 {
		t.Fatalf("fourth call %d", n)
	}
	if r := recordingNow(t, e, recID); r.Name != "发布日期" || r.LiveTitles != liveTitleMax {
		t.Fatalf("after a failure: %q, %d calls", r.Name, r.LiveTitles)
	}
	u = recUpload(6, start, false, talk(0, 300)...)
	u.Kind = RecordingMeeting
	now = start.Add(60 * time.Minute)
	if put, _ = e.PutRecording(recID, u, now); put.TitleDue {
		t.Fatal("due after four calls")
	}

	// Complete: the summary's title settles it, and the minutes' over that.
	sum := &captureModel{reply: `{"title":"导出评审","summary":"## 这是什么\n评审","one_line":"导出","importance":"P2","relevance":50,"tags":["导出"]}`}
	e.Model = sum
	u = recUpload(7, start, true, talk(0, 20)...)
	u.Kind = RecordingMeeting
	putRecording(t, e, recID, u, now)
	if n := e.SummarizeRecordings(ctx, 3); n != 1 {
		t.Fatalf("summarized %d", n)
	}
	if r := recordingNow(t, e, recID); r.Name != "导出评审" || r.AITitleFrom != AITitleSummary {
		t.Fatalf("after the summary: %q from %q", r.Name, r.AITitleFrom)
	}
	e.Model = &captureModel{reply: minutesReply}
	if n := e.WriteMinutes(ctx, 2); n != 1 {
		t.Fatalf("minutes %d", n)
	}
	if r := recordingNow(t, e, recID); r.Name != "发布评审" || r.AITitleFrom != AITitleMinutes || r.NameSource() != NamedByModel {
		t.Fatalf("after the minutes: %q from %q", r.Name, r.AITitleFrom)
	}
	e.Model = sum
	u.Revision = 8
	putRecording(t, e, recID, u, now)
	if n := e.SummarizeRecordings(ctx, 3); n != 1 || recordingNow(t, e, recID).Name != "发布评审" {
		t.Fatalf("a later summary renamed it: %d %q", n, recordingNow(t, e, recID).Name)
	}
}

// The owner's title and the calendar's come first: no live title is asked for, the hub says where the name comes
// from, and a title the phone sends later wins over one the model wrote.
func TestLiveTitleNeverOverTheOwnersOrTheCalendars(t *testing.T) {
	m := &titleModel{replies: []string{`{"title":"导出上限","keep":false}`}}
	e := goldenEngine(t, m)
	start := time.Date(2026, 11, 10, 14, 0, 0, 0, goldenZone)
	now := start.Add(5 * time.Minute)
	e.clock = func() time.Time { return now }
	ctx := context.Background()

	u := recUpload(1, start, false, talk(0, 20)...)
	u.Calendar = &RecordingCalendar{Title: "周会"}
	u.Title = "周会"
	if put, _ := e.PutRecording(recID, u, now); put.TitleDue || put.Name != "周会" || put.NameSource != NamedByCalendar {
		t.Fatalf("calendar: %+v", put)
	}
	u.Revision, u.Title = 2, "我的标题"
	if put, _ := e.PutRecording(recID, u, now); put.TitleDue || put.Name != "我的标题" || put.NameSource != NamedByTitle {
		t.Fatalf("the owner's: %+v", put)
	}
	if n := e.TitleRecordings(ctx, 2); n != 0 {
		t.Fatalf("titled a named recording: %d", n)
	}
	// Named by the model first, then by the owner: the owner's.
	const other = "1f8fad5b-d9cb-469f-a165-70867728950e"
	putRecording(t, e, other, recUpload(1, start, false, talk(0, 20)...), now)
	if n := e.TitleRecordings(ctx, 2); n != 1 || recordingNow(t, e, other).Name != "导出上限" {
		t.Fatalf("live title %d %q", n, recordingNow(t, e, other).Name)
	}
	u = recUpload(2, start, false, talk(0, 20)...)
	u.Title = "改过的名字"
	if put, _ := e.PutRecording(other, u, now); put.Name != "改过的名字" || put.NameSource != NamedByTitle || put.TitleDue {
		t.Fatalf("renamed: %+v", put)
	}
	// An all-day recording keeps its day's name.
	if (&Recording{Kind: RecordingDay, AITitle: "x", Chars: 5000}).liveTitleDue(now) {
		t.Error("a day recording is due a live title")
	}
	// A lost phone's recording is left as it is.
	if (&Recording{Chars: 5000, Updated: now.Add(-13 * time.Hour)}).liveTitleDue(now) {
		t.Error("a recording with no upload for 13 hours is due a live title")
	}
}

// What the model is asked, in Chinese and for a recording made in English; a long transcript sends its start and its
// latest part, and a paused connection is not counted as a call.
func TestGoldenLiveTitle(t *testing.T) {
	m := &captureModel{reply: `{"title":"导出上限","keep":false}`}
	e := goldenEngine(t, m)
	start := time.Date(2026, 11, 10, 14, 0, 0, 0, goldenZone)
	now := start.Add(3 * time.Minute)
	e.clock = func() time.Time { return now }
	u := recUpload(1, start, false, "大家好，今天评审导出", "Ada 说导出超过 4 MB 的条目会被丢掉", strings.Repeat("上限这一版先保留，", 50))
	u.Duration = 180
	putRecording(t, e, recID, u, now)
	if n := e.TitleRecordings(context.Background(), 2); n != 1 {
		t.Fatalf("calls %d", n)
	}
	m.golden(t, "recording_title")

	const en = "1f8fad5b-d9cb-469f-a165-70867728950e"
	u = recUpload(1, start, false, "Hi all, the export review", strings.Repeat("The cap stays for this release. ", 20))
	u.Language, u.Kind = "en-US", RecordingMeeting
	putRecording(t, e, en, u, now)
	if n := e.TitleRecordings(context.Background(), 2); n != 1 {
		t.Fatalf("calls %d", n)
	}
	golden(t, "recording_title.system.en", m.calls[0][0])
	if !strings.Contains(m.calls[0][0], "都用English写") {
		t.Errorf("not asked in English:\n%s", m.calls[0][0])
	}
	m.calls = nil

	// A paused connection puts it off without counting a call.
	const paused = "2f8fad5b-d9cb-469f-a165-70867728950e"
	putRecording(t, e, paused, recUpload(1, start, false, talk(0, 20)...), now)
	e.Model = &titleModel{errs: []error{&ai.PausedError{Pause: ai.Pause{Reason: ai.ReasonBusy, Until: now.Add(time.Minute)}}}}
	e.TitleRecordings(context.Background(), 2)
	if r := recordingNow(t, e, paused); r.LiveTitles != 0 || !r.liveTitleDue(now) {
		t.Errorf("after a pause: %d calls, due %v", r.LiveTitles, r.liveTitleDue(now))
	}

	if got := headAndTail(strings.Repeat("a", 10)+strings.Repeat("b", 10), 3, 4); got != "aaa\n…（中间省略）…\nbbbb" {
		t.Errorf("headAndTail %q", got)
	}
	if got := titleLine("  “**导出**  上限” "); got != "导出 上限" {
		t.Errorf("titleLine %q", got)
	}
	if got := titleLine(strings.Repeat("长", 80)); len([]rune(got)) != titleRunes || !strings.HasSuffix(got, "…") {
		t.Errorf("a long title: %q", got)
	}
}

// A name given on the web (#8877) names the recording before the phone's title and the generated one, and stops a live
// title; an upload with the phone's title unchanged keeps it, one that changes it (renamed on the phone since) takes it
// back; "" takes it back too. Errors: not one line or too long, no recording, a deleted one.
func TestWebRename(t *testing.T) {
	e := goldenEngine(t, &titleModel{replies: []string{`{"title":"导出上限","keep":false}`}})
	start := time.Date(2026, 11, 10, 14, 0, 0, 0, goldenZone)
	now := start.Add(5 * time.Minute)
	e.clock = func() time.Time { return now }
	if _, err := e.RenameRecording(recID, "网页名", now); !errors.Is(err, ErrNotFound) {
		t.Fatalf("no recording: %v", err)
	}
	if _, err := e.RenameRecording("nope", "网页名", now); !errors.Is(err, ErrBadRecording) {
		t.Fatalf("bad id: %v", err)
	}
	putRecording(t, e, recID, recUpload(1, start, false, talk(0, 20)...), now)
	for _, bad := range []string{"两\n行", strings.Repeat("长", 201)} {
		if _, err := e.RenameRecording(recID, bad, now); !errors.Is(err, ErrBadTitle) {
			t.Errorf("%q: %v", bad, err)
		}
	}
	r, err := e.RenameRecording(recID, " 网页名 ", now)
	if err != nil || r.Name != "网页名" || r.NameSource() != NamedByRename || r.RenamedAt == nil {
		t.Fatalf("renamed: %+v %v", r, err)
	}
	if recordingNow(t, e, recID).liveTitleDue(now) {
		t.Error("a renamed recording is due a live title")
	}
	if states, _ := e.RecordingStates(); states[0].Name != "网页名" || states[0].NameSource != NamedByRename {
		t.Errorf("states %+v", states)
	}
	// The phone's title as it was: the web's name stays.
	if put, _ := e.PutRecording(recID, recUpload(2, start, false, talk(0, 20)...), now); put.Name != "网页名" || put.NameSource != NamedByRename {
		t.Fatalf("an upload with the title as it was: %+v", put)
	}
	// The phone renamed it since: its name wins and the web's goes.
	u := recUpload(3, start, false, talk(0, 20)...)
	u.Title = "手机名"
	if put, _ := e.PutRecording(recID, u, now); put.Name != "手机名" || put.NameSource != NamedByTitle {
		t.Fatalf("the phone's newer name: %+v", put)
	}
	if r := recordingNow(t, e, recID); r.Renamed != "" || r.RenamedAt != nil {
		t.Fatalf("the web's name stayed: %+v", r)
	}
	// Cleared on the web.
	if _, err := e.RenameRecording(recID, "网页名", now); err != nil {
		t.Fatal(err)
	}
	if r, _ := e.RenameRecording(recID, "", now); r.Name != "手机名" || r.Renamed != "" {
		t.Fatalf("cleared: %q %q", r.Name, r.Renamed)
	}
	if _, err := e.DeleteRecording(recID, now); err != nil {
		t.Fatal(err)
	}
	if _, err := e.RenameRecording(recID, "网页名", now); !errors.Is(err, ErrRecordingDeleted) {
		t.Fatalf("deleted: %v", err)
	}
}
