package kb

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"
)

const hubMinutesReply = `{"title": "设计评审", "points": [{"text": "上限保持在 4 MB", "line": 1}]}`

// hubMinutesEngine is a hub engine whose model answers minutes, on a clock the test moves, telling each push.
func hubMinutesEngine(t *testing.T, reply string) (e *Engine, m *captureModel, clock *time.Time, told *int) {
	t.Helper()
	m = &captureModel{reply: reply}
	e, _ = hubEngine(t, m)
	c := time.Date(2026, 11, 10, 14, 0, 0, 0, goldenZone)
	clock = &c
	e.clock = func() time.Time { return *clock }
	n := 0
	told = &n
	e.MinutesWritten = func(context.Context, Recording, Minutes) { n++ }
	return e, m, clock, told
}

// hubCompleteMeeting stores a complete hub-mode meeting of the phone's lines (uploaded at *clock), its live hub
// sentences and some audio.
func hubCompleteMeeting(t *testing.T, e *Engine, id string, at time.Time, phone []string, hub []RecordingSegment) {
	t.Helper()
	putRecording(t, e, id, hubUpload(1, at.Add(-time.Hour), true, nil, phone...), at)
	if len(hub) > 0 {
		if _, err := e.HubTranscriptPut(id, HubPassLive, hub); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := e.AudioPut(id, 0, pcmAt(0, secs(1))); err != nil {
		t.Fatal(err)
	}
}

// finishTranscription runs what the manager does at the end of the final pass: the final sentences, the state, and
// FinalTranscriptStored.
func finishTranscription(t *testing.T, e *Engine, id, state string, final []RecordingSegment) {
	t.Helper()
	if final != nil {
		if _, err := e.HubTranscriptPut(id, HubPassFinal, final); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := e.TranscriptionUpdate(id, func(t *MeetingTranscription, _ Recording) error { t.State = state; return nil }); err != nil {
		t.Fatal(err)
	}
	if err := e.FinalTranscriptStored(id); err != nil {
		t.Fatal(err)
	}
}

func audioThere(e *Engine, id string) bool {
	_, err := os.Stat(e.Store.audioPath(id))
	return err == nil
}

// In hub mode the minutes wait for the final transcript (at most 45 minutes after the complete upload), are made from
// the transcript of record (the hub's sentences as said, the phone's only where the hub has none), pushed once, and
// take the audio with them.
func TestHubMinutesWaitForTheFinalTranscript(t *testing.T) {
	e, m, clock, told := hubMinutesEngine(t, hubMinutesReply)
	hubCompleteMeeting(t, e, recID, *clock, []string{"我们先把 export cap 保持在四兆", "手机二"}, []RecordingSegment{
		hseg(0, 8, "我们先把 export cap 保持在 4 MB。")})
	if r := recordingNow(t, e, recID); r.MinutesState != MinutesPending {
		t.Fatalf("state %q", r.MinutesState)
	}
	*clock = clock.Add(10 * time.Minute)
	if n := e.WriteMinutes(context.Background(), 2); n != 0 || len(m.calls) != 0 {
		t.Fatalf("written while the final pass runs: %d, %d calls", n, len(m.calls))
	}
	finishTranscription(t, e, recID, TranscriptionFinal, []RecordingSegment{
		hseg(0, 8, "我们先把 export cap 保持在 4 MB。"), hseg(8.5, 9, "Bo owns the vendor follow-up.")})
	if !audioThere(e, recID) {
		t.Fatal("the audio went before the minutes")
	}
	if n := e.WriteMinutes(context.Background(), 2); n != 1 || len(m.calls) != 1 {
		t.Fatalf("written %d, %d calls", n, len(m.calls))
	}
	in := m.calls[0][1]
	for _, want := range []string{"我们先把 export cap 保持在 4 MB。", "Bo owns the vendor follow-up.", "手机二"} {
		if !strings.Contains(in, want) {
			t.Errorf("the minutes' input lacks %q:\n%s", want, in)
		}
	}
	if strings.Contains(in, "四兆") {
		t.Errorf("the phone's text where the hub has its own:\n%s", in)
	}
	if r := recordingNow(t, e, recID); r.MinutesState != MinutesReady || *told != 1 {
		t.Fatalf("state %q told %d", r.MinutesState, *told)
	}
	if audioThere(e, recID) {
		t.Fatal("the audio outlived the minutes from the final transcript")
	}
	if tr := transcriptionNow(t, e, recID); tr.AudioGoneAt.IsZero() {
		t.Fatalf("audio gone not noted: %+v", tr)
	}
	if ids, _ := e.HubRecordings(); len(ids) != 0 {
		t.Fatalf("still the manager's: %v", ids)
	}
}

// Past the wait the minutes are made from the live sentences and pushed; the final pass makes them again quietly (no
// second push) and the audio goes once those are written.
func TestHubMinutesMadeAgainQuietlyAfterTheFinalPass(t *testing.T) {
	e, m, clock, told := hubMinutesEngine(t, hubMinutesReply)
	hubCompleteMeeting(t, e, recID, *clock, []string{"手机一"}, []RecordingSegment{hseg(0, 8, "live words")})
	*clock = clock.Add(minutesHubWait - time.Second)
	if n := e.WriteMinutes(context.Background(), 2); n != 0 {
		t.Fatal("written before the wait ended")
	}
	*clock = clock.Add(2 * time.Second)
	if n := e.WriteMinutes(context.Background(), 2); n != 1 || *told != 1 || !strings.Contains(m.calls[0][1], "live words") {
		t.Fatalf("after the wait: %d, told %d", n, *told)
	}
	if !audioThere(e, recID) {
		t.Fatal("minutes of the live text took the audio")
	}
	*clock = clock.Add(time.Minute)
	finishTranscription(t, e, recID, TranscriptionFinal, []RecordingSegment{hseg(0, 8, "final words")})
	r := recordingNow(t, e, recID)
	if r.MinutesState != MinutesPending || !r.quietMinutes() {
		t.Fatalf("not made again quietly: %q quiet %d asks %d", r.MinutesState, r.MinutesQuiet, r.MinutesAsks)
	}
	if n := e.WriteMinutes(context.Background(), 2); n != 1 || !strings.Contains(m.calls[1][1], "final words") {
		t.Fatalf("again: %d", n)
	}
	if r := recordingNow(t, e, recID); r.MinutesState != MinutesReady || *told != 1 {
		t.Fatalf("state %q told %d", r.MinutesState, *told)
	}
	if audioThere(e, recID) {
		t.Fatal("the quiet remake kept the audio")
	}
	// The summary is made again from the final transcript too.
	if r := recordingNow(t, e, recID); !r.summaryDue() {
		t.Fatalf("summary not due: %d final %v", r.SummarizedRev, r.SummarizedFinal)
	}
}

// The short summary is made again from the final transcript, also when its call from the live text was still running
// as the final pass ended, and when a crash kept FinalTranscriptStored from being told.
func TestHubSummaryMadeAgainAfterTheFinalPass(t *testing.T) {
	var e *Engine
	race := true
	m := &funcModel{f: func(n int, _, user string) (string, error) {
		if race && n == 1 { // the final pass ends while the live text's summary is being made
			finishTranscription(t, e, recID, TranscriptionFinal, []RecordingSegment{hseg(0, 8, "final words")})
		}
		return `{"title": "Design review", "summary": "A summary.", "one_line": "One line."}`, nil
	}}
	e, _ = hubEngine(t, m)
	now := time.Date(2026, 11, 10, 14, 0, 0, 0, goldenZone)
	hubCompleteMeeting(t, e, recID, now, []string{"手机一"}, []RecordingSegment{hseg(0, 8, "live words")})
	ctx := context.Background()
	if n := e.SummarizeRecordings(ctx, 3); n != 1 || !strings.Contains(m.calls[0][1], "live words") {
		t.Fatalf("the live text's summary: %d", n)
	}
	if r := recordingNow(t, e, recID); !r.summaryDue() {
		t.Fatalf("the summary of the live text settled it: rev %d final %v", r.SummarizedRev, r.SummarizedFinal)
	}
	if n := e.SummarizeRecordings(ctx, 3); n != 1 || !strings.Contains(m.calls[1][1], "final words") {
		t.Fatalf("again from the final transcript: %d", n)
	}
	if n := e.SummarizeRecordings(ctx, 3); n != 0 || len(m.calls) != 2 {
		t.Fatalf("a third summary: %d, %d calls", n, len(m.calls))
	}

	// A crash between the final state and FinalTranscriptStored: the summary of the live text is due all the same.
	race = false
	const crashed = "5b6c3f4a-1d2e-4f80-9a1b-2c3d4e5f6a7b"
	hubCompleteMeeting(t, e, crashed, now, []string{"手机一"}, []RecordingSegment{hseg(0, 8, "live words")})
	if n := e.SummarizeRecordings(ctx, 3); n != 1 {
		t.Fatalf("the live text's summary: %d", n)
	}
	if _, err := e.HubTranscriptPut(crashed, HubPassFinal, []RecordingSegment{hseg(0, 8, "crashed final words")}); err != nil {
		t.Fatal(err)
	}
	if _, err := e.TranscriptionUpdate(crashed, func(t *MeetingTranscription, _ Recording) error { t.State = TranscriptionFinal; return nil }); err != nil {
		t.Fatal(err)
	}
	if n := e.SummarizeRecordings(ctx, 3); n != 1 || !strings.Contains(m.calls[len(m.calls)-1][1], "crashed final words") {
		t.Fatalf("after the crash: %d", n)
	}
}

// Minutes that end failed from the final transcript take the audio; so does a final pass that leaves a recording
// with no minutes coming: no text at all, or never complete.
func TestHubAudioGoesWithoutMinutes(t *testing.T) {
	e, _, clock, _ := hubMinutesEngine(t, `{"title": "x", "points": [{"text": "made up", "line": 99}]}`)
	hubCompleteMeeting(t, e, recID, *clock, []string{"手机一"}, nil)
	finishTranscription(t, e, recID, TranscriptionFailed, nil)
	for range minutesTries {
		e.WriteMinutes(context.Background(), 2)
	}
	if r := recordingNow(t, e, recID); r.MinutesState != MinutesFailed {
		t.Fatalf("state %q", r.MinutesState)
	}
	if audioThere(e, recID) {
		t.Fatal("failed minutes kept the audio")
	}

	// No text at all: no minutes, the audio goes once the final pass stored nothing.
	const silent, open = "5b6c3f4a-1d2e-4f80-9a1b-2c3d4e5f6a7b", "6c7d4e5b-2e3f-4a91-8b2c-3d4e5f6a7b8c"
	hubCompleteMeeting(t, e, silent, *clock, nil, nil)
	if r := recordingNow(t, e, silent); r.MinutesState != "" || r.Chars != 0 {
		t.Fatalf("silent %+v", r)
	}
	finishTranscription(t, e, silent, TranscriptionFailed, nil)
	if audioThere(e, silent) {
		t.Fatal("a meeting with no text kept its audio")
	}

	// Never complete: the final pass at the meeting's end, then the audio goes.
	putRecording(t, e, open, hubUpload(1, clock.Add(-13*time.Hour), false, nil, "手机一"), *clock)
	if _, err := e.AudioPut(open, 0, pcmAt(0, 100)); err != nil {
		t.Fatal(err)
	}
	finishTranscription(t, e, open, TranscriptionFinal, []RecordingSegment{hseg(0, 5, "final words")})
	if audioThere(e, open) {
		t.Fatal("a recording never complete kept its audio after the final pass")
	}
	if err := e.FinalTranscriptStored(open); err != nil {
		t.Fatalf("told again: %v", err)
	}
	if err := e.FinalTranscriptStored("7d8e5f6c-3f40-4b02-9c3d-4e5f6a7b8c9d"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("no upload: %v", err)
	}
}

// A meeting whose phone wrote nothing gets its minutes from the hub's sentences alone (has text counts the transcript
// of record): planned, asked for, written.
func TestHubOnlyMeetingGetsMinutes(t *testing.T) {
	e, m, clock, told := hubMinutesEngine(t, hubMinutesReply)
	hubCompleteMeeting(t, e, recID, *clock, nil, []RecordingSegment{hseg(0, 4, "下周三之前 Alice 要把 pricing 的 proposal 发出来。")})
	if r := recordingNow(t, e, recID); r.Chars == 0 || r.MinutesState != MinutesPending {
		t.Fatalf("no text? %+v", r)
	}
	if err := e.AskMinutes(recID); err != nil {
		t.Fatalf("ask: %v", err)
	}
	finishTranscription(t, e, recID, TranscriptionFinal, nil)
	if n := e.WriteMinutes(context.Background(), 2); n != 1 || *told != 1 || !strings.Contains(m.calls[0][1], "下周三之前 Alice 要把 pricing 的 proposal 发出来。") {
		t.Fatalf("written %d told %d", n, *told)
	}
}

// Every retention pass deletes what should be gone: the audio of a recording whose meeting.json no longer says hub (an
// older manager's after a rollback), and of one whose minutes from the final transcript were written when a crash
// came before the deletion; minutes of the live text whose final pass was never told are made again (quietly).
func TestHubAudioSweep(t *testing.T) {
	e, _, clock, told := hubMinutesEngine(t, hubMinutesReply)
	// The rollback: meeting.json written by an older manager, without the transcription.
	hubCompleteMeeting(t, e, recID, *clock, []string{"手机一"}, nil)
	if err := writeRecordingFile(e.Store.Path(KindRecording, recID, meetingFile), []byte(`{"private":true}`+"\n")); err != nil {
		t.Fatal(err)
	}
	// The crash after the minutes: written from the final transcript, the audio still there.
	const crashed, untold = "5b6c3f4a-1d2e-4f80-9a1b-2c3d4e5f6a7b", "6c7d4e5b-2e3f-4a91-8b2c-3d4e5f6a7b8c"
	hubCompleteMeeting(t, e, crashed, *clock, []string{"手机一"}, nil)
	if _, err := e.TranscriptionUpdate(crashed, func(t *MeetingTranscription, _ Recording) error { t.State = TranscriptionFinal; return nil }); err != nil {
		t.Fatal(err)
	}
	*clock = clock.Add(time.Minute)
	putMeetingMinutes(t, e, crashed, Minutes{Version: 1, Title: "Design review", When: "w", GeneratedAt: *clock})
	changeRecording(t, e, crashed, func(r *Recording) { r.MinutesRev, r.MinutesState = r.Revision, MinutesReady })
	// Minutes of the live text, the final pass stored and FinalTranscriptStored never told.
	hubCompleteMeeting(t, e, untold, *clock, []string{"手机一"}, nil)
	putMeetingMinutes(t, e, untold, Minutes{Version: 1, Title: "Design review", When: "w", GeneratedAt: *clock})
	changeRecording(t, e, untold, func(r *Recording) { r.MinutesRev, r.MinutesState = r.Revision, MinutesReady })
	*clock = clock.Add(time.Minute)
	if _, err := e.TranscriptionUpdate(untold, func(t *MeetingTranscription, _ Recording) error { t.State = TranscriptionFinal; return nil }); err != nil {
		t.Fatal(err)
	}

	e.PruneRecordings(*clock)
	if audioThere(e, recID) || audioThere(e, crashed) {
		t.Fatalf("left: rollback %v, crash %v", audioThere(e, recID), audioThere(e, crashed))
	}
	if !audioThere(e, untold) {
		t.Fatal("the audio went before the minutes from the final transcript")
	}
	if r := recordingNow(t, e, untold); r.MinutesState != MinutesPending || !r.quietMinutes() {
		t.Fatalf("not made again: %q", r.MinutesState)
	}
	*clock = clock.Add(time.Minute)
	// The rolled-back recording's first minutes (pushed) and the quiet remake (not).
	if n := e.WriteMinutes(context.Background(), 5); n != 2 || *told != 1 {
		t.Fatalf("written %d told %d", n, *told)
	}
	if audioThere(e, untold) {
		t.Fatal("the remade minutes kept the audio")
	}
	e.PruneRecordings(*clock) // nothing more to do
	if tr := transcriptionNow(t, e, crashed); tr.AudioGoneAt.IsZero() {
		t.Fatalf("not noted %+v", tr)
	}
}

// Persona's echo in hub mode (#5905): the hub's sentences keep their echo mark in hub_segments.json, the transcript of
// record keeps it (the hub's and the phone's), transcript.md marks it, chars and the minutes leave it out, and a
// meeting whose only words are Persona's gets no minutes.
func TestHubEchoIsKeptMarkedAndLeftOutOfTheMinutes(t *testing.T) {
	e, m, clock, _ := hubMinutesEngine(t, hubMinutesReply)
	u := hubUpload(1, clock.Add(-time.Hour), true, nil, "手机一", "手机二", "phone three")
	u.Segments[2].Echo = true // the phone's mark, where the hub has no sentence
	putRecording(t, e, recID, u, *clock)
	echo := RecordingSegment{Start: 10, End: 18, Text: "We keep the cap for this release.", Echo: true}
	if _, err := e.HubTranscriptPut(recID, HubPassLive, []RecordingSegment{hseg(0, 8, "我们先把 export cap 保持在 4 MB。"), echo}); err != nil {
		t.Fatal(err)
	}
	h, _ := e.HubTranscript(recID)
	if len(h.Segments) != 2 || h.Segments[0].Echo || !h.Segments[1].Echo {
		t.Fatalf("hub_segments.json %+v", h.Segments)
	}
	d, err := e.Get(KindRecording, recID, 0)
	if err != nil {
		t.Fatal(err)
	}
	var marks []string
	for _, sg := range d.Segments {
		if sg.Echo {
			marks = append(marks, sg.Source+": "+sg.Text)
		}
	}
	if strings.Join(marks, "|") != "hub: We keep the cap for this release.|device: phone three" {
		t.Fatalf("detail.segments' echo %q (%+v)", marks, d.Segments)
	}
	md, _ := os.ReadFile(e.Store.path(KindRecording, recID, "transcript.md"))
	if !strings.Contains(string(md), "[Persona 回声] We keep the cap for this release.") || !strings.Contains(string(md), "[Persona 回声] phone three") {
		t.Fatalf("transcript.md\n%s", md)
	}
	if r := recordingNow(t, e, recID); r.Chars != len([]rune("我们先把 export cap 保持在 4 MB。")) {
		t.Fatalf("chars %d: Persona's echo counted", r.Chars)
	}
	finishTranscription(t, e, recID, TranscriptionFinal, nil)
	if n := e.WriteMinutes(context.Background(), 2); n != 1 {
		t.Fatalf("written %d", n)
	}
	if in := m.calls[0][1]; strings.Contains(in, "We keep the cap") || strings.Contains(in, "phone three") || !strings.Contains(in, "4 MB") {
		t.Fatalf("the minutes' input:\n%s", in)
	}

	// Only Persona's words: no text, so no minutes.
	const other = "7d8e5f6c-3f40-4b02-9c3d-4e5f6a7b8c9d"
	hubCompleteMeeting(t, e, other, *clock, nil, []RecordingSegment{echo})
	if r := recordingNow(t, e, other); r.Chars != 0 || r.MinutesState == MinutesPending {
		t.Fatalf("an echo-only meeting: chars %d, minutes %q", r.Chars, r.MinutesState)
	}
}
