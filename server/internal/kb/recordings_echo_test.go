package kb

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

// Persona's echo on a meeting's upload (#5905). The bodies are JSON, as the phone sends them: segments may carry
// echo, and spoken says where Persona spoke and what it said.

// echoUpload is a complete meeting upload of body's fields (segments, spoken) over the defaults, decoded as the route
// decodes it.
func echoUpload(t *testing.T, start time.Time, fields string) RecordingUpload {
	t.Helper()
	ended := start.Add(time.Minute)
	body := fmt.Sprintf(`{"revision":1,"started_at":%q,"ended_at":%q,"duration":60,"language":"en-US","device":"phone",`+
		`"kind":"meeting","complete":true,%s}`, start.Format(time.RFC3339), ended.Format(time.RFC3339), fields)
	var u RecordingUpload
	if err := json.Unmarshal([]byte(body), &u); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return u
}

// storedSegments is segments.json as written, each segment as a JSON object.
func storedSegments(t *testing.T, e *Engine, id string) []map[string]any {
	t.Helper()
	raw, err := os.ReadFile(e.Store.Path(KindRecording, id, "segments.json"))
	if err != nil {
		t.Fatal(err)
	}
	var segs []map[string]any
	if err := json.Unmarshal(raw, &segs); err != nil {
		t.Fatal(err)
	}
	return segs
}

// A participant who speaks while Persona answers stays the meeting's: kept unmarked, read by the minutes and the
// summary. Persona's own answer, by the hub's rule against the spans or by the phone's mark, is kept in segments.json
// marked echo, shown marked in transcript.md, and read by neither; it is no text of the meeting (Chars), and the
// minutes' line numbers still index segments.json.
func TestMeetingEchoIsKeptMarkedAndLeftOutOfTheMinutes(t *testing.T) {
	m := &captureModel{reply: `{"title":"Release review","summary":"## 这是什么\n评审","one_line":"评审"}`}
	e := goldenEngine(t, m)
	now := time.Date(2026, 11, 10, 16, 0, 0, 0, goldenZone)
	start := time.Date(2026, 11, 10, 14, 0, 0, 0, goldenZone)
	u := echoUpload(t, start, `"segments":[
		{"start":2,"end":9,"text":"Who owns the regression test?"},
		{"start":10.2,"end":17.8,"text":"Bo owns the regression test."},
		{"start":18,"end":24,"text":"You kept the cap for this release."},
		{"start":25,"end":30,"text":"The phone heard this one as Persona.","echo":true},
		{"start":31,"end":38,"text":"Thanks, next item."}],
		"spoken":[{"start":10,"end":17.5,"text":"You kept the cap."},{"start":18,"end":24.5,"text":"You kept the cap for this release."}]`)
	putRecording(t, e, recID, u, now)

	segs := storedSegments(t, e, recID)
	if len(segs) != 5 {
		t.Fatalf("segments.json keeps every line: %v", segs)
	}
	for i, want := range []bool{false, false, true, true, false} {
		if echo, _ := segs[i]["echo"].(bool); echo != want {
			t.Errorf("segment %d (%v): echo %v, want %v", i, segs[i]["text"], echo, want)
		}
		if _, ok := segs[i]["echo"]; !want && ok {
			t.Errorf("segment %d: echo written on a line of the meeting: %v", i, segs[i])
		}
	}
	r := recordingNow(t, e, recID)
	if want := len("Who owns the regression test?") + len("Bo owns the regression test.") + len("Thanks, next item."); r.Chars != want {
		t.Errorf("chars %d, want %d (the meeting's lines only)", r.Chars, want)
	}
	md, err := os.ReadFile(e.Store.Path(KindRecording, recID, "transcript.md"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{" Bo owns the regression test.\n", "[Persona 回声] You kept the cap for this release.\n",
		"[Persona 回声] The phone heard this one as Persona.\n"} {
		if !strings.Contains(string(md), want) {
			t.Errorf("transcript.md lacks %q:\n%s", want, md)
		}
	}

	if n := e.SummarizeRecordings(context.Background(), 2); n != 1 || len(m.calls) != 1 {
		t.Fatalf("summarized %d with %d calls", n, len(m.calls))
	}
	summary := m.calls[0][1]
	m.calls, m.reply = nil, minutesReply
	if n := e.WriteMinutes(context.Background(), 2); n != 1 || len(m.calls) != 1 {
		t.Fatalf("wrote %d minutes with %d calls", n, len(m.calls))
	}
	minutes := m.calls[0][1]
	for name, in := range map[string]string{"summary": summary, "minutes": minutes} {
		if !strings.Contains(in, "Bo owns the regression test.") || !strings.Contains(in, "Thanks, next item.") {
			t.Errorf("%s input lacks the meeting's lines:\n%s", name, in)
		}
		if strings.Contains(in, "You kept the cap") || strings.Contains(in, "The phone heard this one") {
			t.Errorf("%s input has Persona's echo:\n%s", name, in)
		}
	}
	if !strings.Contains(minutes, "[2] 14:00:10 Bo owns the regression test.") || !strings.Contains(minutes, "[5] 14:00:31 Thanks, next item.") {
		t.Errorf("the minutes' lines are numbered by segments.json:\n%s", minutes)
	}
}

// A meeting whose only words are Persona's echo has no text: no minutes wait for it, none are written.
func TestMeetingOfOnlyEchoGetsNoMinutes(t *testing.T) {
	m := &captureModel{reply: minutesReply}
	e := goldenEngine(t, m)
	now := time.Date(2026, 11, 10, 16, 0, 0, 0, goldenZone)
	u := echoUpload(t, now.Add(-time.Hour), `"segments":[
		{"start":1,"end":5,"text":"You kept the cap for this release."},
		{"start":6,"end":9,"text":"Bo owns it.","echo":true}],
		"spoken":[{"start":0.5,"end":5.5,"text":"You kept the cap for this release."}]`)
	putRecording(t, e, recID, u, now)
	if r := recordingNow(t, e, recID); r.Chars != 0 || r.MinutesState != "" {
		t.Fatalf("chars %d, minutes %q: an echo-only meeting waits for minutes forever", r.Chars, r.MinutesState)
	}
	if n := e.WriteMinutes(context.Background(), 2); n != 0 || len(m.calls) != 0 {
		t.Fatalf("wrote %d minutes, %d model calls", n, len(m.calls))
	}
	if n := e.SummarizeRecordings(context.Background(), 2); n != 0 || len(m.calls) != 0 {
		t.Fatalf("summarized %d, %d model calls", n, len(m.calls))
	}
}

// A spoken field of any shape never fails the upload; its well-formed spans still mark the echo.
func TestMalformedSpokenNeverFailsTheUpload(t *testing.T) {
	e := goldenEngine(t, nil)
	now := time.Date(2026, 11, 10, 16, 0, 0, 0, goldenZone)
	lines := `"segments":[{"start":10.2,"end":17.8,"text":"You kept the cap."},{"start":20,"end":24,"text":"Bo owns the regression test."}]`
	for i, spoken := range []string{`"yes"`, `7`, `{"start":10,"end":17.5}`, `[1,"x",null,{"start":"a"},{"start":10}]`,
		`[{"start":10,"end":17.5,"text":["You kept the cap."]}]`,
		"[" + strings.TrimSuffix(strings.Repeat(`{"start":10,"end":17.5,"text":"You kept the cap."},`, 1001), ",") + "]"} {
		u := echoUpload(t, now.Add(-time.Hour), lines+`,"spoken":`+spoken)
		u.Revision = int64(i + 1)
		putRecording(t, e, recID, u, now)
		for j, sg := range storedSegments(t, e, recID) {
			if _, marked := sg["echo"]; marked {
				t.Errorf("spoken %.40s: segment %d marked by a malformed span", spoken, j)
			}
		}
	}
	u := echoUpload(t, now.Add(-time.Hour), lines+`,"spoken":[1,{"start":"a","end":2},{"start":10,"end":17.5,"text":"You kept the cap."}]`)
	u.Revision = 10
	putRecording(t, e, recID, u, now)
	if segs := storedSegments(t, e, recID); segs[0]["echo"] != true || segs[1]["echo"] != nil {
		t.Errorf("the well-formed span beside malformed ones: %v", segs)
	}
}

// An all-day recording has no echo: Persona never speaks in one. A segment marked echo is stored unmarked by the
// append (POST …/segments) as by the PUT, so transcript.md shows it as the day's line, as the day's text counts it.
func TestDayRecordingDropsEchoMarks(t *testing.T) {
	now := dayClock.Add(2 * time.Hour)
	e := dayEngine(t, nil, &now)
	u := dayUpload(1000, "八点")
	if err := json.Unmarshal([]byte(`[{"start":0,"end":5,"text":"八点","echo":true}]`), &u.Segments); err != nil {
		t.Fatal(err)
	}
	mustPut(t, e, dayID, u, now)
	var a RecordingAppend
	if err := json.Unmarshal([]byte(`{"base":1000,"revision":1001,"from":1,`+
		`"segments":[{"start":120,"end":125,"text":"八点零二","echo":true}]}`), &a); err != nil {
		t.Fatal(err)
	}
	mustAppend(t, e, dayID, a)
	segs := storedSegments(t, e, dayID)
	if len(segs) != 2 {
		t.Fatalf("segments.json: %v", segs)
	}
	for i, sg := range segs {
		if _, marked := sg["echo"]; marked {
			t.Errorf("segment %d (%v) of an all-day recording stored as Persona's echo", i, sg["text"])
		}
	}
	md, err := os.ReadFile(e.Store.Path(KindRecording, dayID, "transcript.md"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(md), "Persona 回声") || !strings.Contains(string(md), "08:02:00 八点零二\n") {
		t.Errorf("transcript.md marks an all-day line as Persona's echo:\n%s", md)
	}
	if r := recordingNow(t, e, dayID); r.Chars != len([]rune("八点"))+len([]rune("八点零二")) {
		t.Errorf("chars %d", r.Chars)
	}
}
