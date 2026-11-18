package assistant

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/nwverse/persona/server/internal/extwire"
	"github.com/nwverse/persona/server/internal/kb"
)

// The final pass (docs/assistant/meeting-minutes.md "Gaps and the final pass"). Names and texts are placeholders.

// liveTwo are the live job's sentences of the final pass tests.
var liveTwo = []fakeLine{{0.5, 1.5, "Live one."}, {4.2, 5.0, "Live two."}}

// meetingAudio records recording id in hub mode: audio from 0 to 2 and from 4 to 6 (a gap between), liveTwo as its
// live sentences, the socket's end at 6, then the complete upload.
func (h *hubFixture) meetingAudio(id string) {
	h.t.Helper()
	h.tr.set(func(f *fakeTranscriber) { f.live = jobScript{lines: liveTwo} })
	h.hubUpload(id, 1, nil)
	p, _ := h.open(id, 0, nil)
	p.audio(2)
	p.send(map[string]any{"type": "seek", "at": 4.0})
	p.audio(2)
	p.send(map[string]any{"type": "end", "at": 6.0})
	p.waitClosed()
	waitFor(h.t, "the live sentences", func() bool { return len(h.hubTexts(id)) == 2 })
	h.hubUpload(id, 2, map[string]any{"complete": true, "duration": 6.0, "ended_at": time.Now().UTC().Format(time.RFC3339),
		"segments": phoneSegs(fakeLine{0.4, 1.6, "Phone one."}, fakeLine{2.1, 2.9, "Phone in the gap."})})
}

// finalState waits for recording id's state to be want.
func (h *hubFixture) finalState(id, want string) {
	h.t.Helper()
	waitFor(h.t, "state "+want, func() bool {
		h.s.finalRound(context.Background())
		return h.transcription(id)["state"] == want
	})
}

// The pass waits for the gap uploads; from its start the hub takes no more audio; its windows replace the live
// sentences where it did not keep them; the transcript of record and detail.segments are the hub's; the minutes are
// made from it and the audio goes once they are written.
func TestFinalPass(t *testing.T) {
	h := newHubFixture(t)
	h.meetingAudio(meetingRec)
	h.tr.set(func(f *fakeTranscriber) {
		f.replay = jobScript{windows: []fakeWindow{{0, 3, false, []fakeLine{{0.5, 1.5, "Replay one."}, {2.2, 2.8, "Replay in the gap."}}},
			{3, 6, true, nil}}, usage: 7}
	})
	h.s.finalRound(context.Background())
	if tr := h.transcription(meetingRec); tr["state"] != "finishing" || len(tr["gaps"].([]any)) != 1 {
		t.Fatalf("waiting for the gap: %v", tr)
	}
	if len(h.tr.jobsOf(extwire.TranscriptionReplay)) != 0 {
		t.Fatal("the pass started with a gap left")
	}
	path := h.srv.URL + "/api/v1/recordings/" + meetingRec + "/audio?at=2"
	req, _ := http.NewRequest("PUT", path, strings.NewReader(string(make([]byte, 2*2*kb.AudioRate))))
	resp, err := h.srv.Client().Do(req)
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("gap upload: %v %v", err, resp)
	}
	resp.Body.Close()
	h.finalState(meetingRec, "final")
	jobs := h.tr.jobsOf(extwire.TranscriptionReplay)
	if len(jobs) != 1 || jobs[0].start.Speed != 4 || *jobs[0].start.At != 0 || jobs[0].start.Language != "en-US" {
		t.Fatalf("replay jobs: %+v", jobs)
	}
	frames, audio, _, _ := jobs[0].snapshot()
	var refs []string
	for _, f := range frames {
		if f.Type == extwire.TFReference {
			refs = append(refs, *f.Text)
		}
	}
	if !slices.Equal(refs, []string{"Live one.", "Phone in the gap.", "Live two."}) || audio != 6*kb.AudioRate {
		t.Fatalf("references %q, %d samples", refs, audio)
	}
	if got := h.hubTexts(meetingRec); !slices.Equal(got, []string{"Replay one.", "Replay in the gap.", "Live two."}) {
		t.Fatalf("the final transcript %q", got)
	}
	d, err := h.engine().Get(kb.KindRecording, meetingRec, 0)
	if err != nil {
		t.Fatal(err)
	}
	var texts []string
	for _, sg := range d.Segments {
		texts = append(texts, sg.Text+"/"+sg.Source)
	}
	if !slices.Equal(texts, []string{"Replay one./hub", "Replay in the gap./hub", "Live two./hub"}) {
		t.Fatalf("detail.segments %q", texts)
	}
	m, _, _ := h.engine().MeetingRead(meetingRec)
	if tr := m.Transcription; tr.UsageSeconds < 7 || tr.FinalAt.IsZero() || len(tr.Gaps) != 0 {
		t.Fatalf("transcription %+v", tr)
	}
	put, _ := http.NewRequest("PUT", path, strings.NewReader("ab"))
	if resp, err := h.srv.Client().Do(put); err != nil || resp.StatusCode != http.StatusConflict {
		t.Fatalf("audio after the pass: %v %v", err, resp)
	}
	audioDir := filepath.Join(h.s.kb.dir, kb.KindRecording, meetingRec, "attachments", "audio")
	if _, err := os.Stat(audioDir); err != nil {
		t.Fatalf("the audio went before the minutes: %v", err)
	}
	h.minutesFor(meetingRec, `{"title":"Design review","decisions":[{"text":"Keep the cap","line":1}]}`)
	if _, err := os.Stat(audioDir); !os.IsNotExist(err) {
		t.Fatalf("the audio after the final minutes: %v", err)
	}
}

// From the pass's start the phone's socket and gap uploads are refused transcription_final; a pass that waited
// finalGapWait starts with gaps left.
func TestFinalPassAfterTheWait(t *testing.T) {
	h := newHubFixture(t)
	h.meetingAudio(meetingRec)
	h.tr.set(func(f *fakeTranscriber) {
		f.replay = jobScript{windows: []fakeWindow{{0, 6, false, []fakeLine{{0.5, 1.5, "Replay one."}}}}}
	})
	e := h.engine()
	h.s.finalLook(context.Background(), e, meetingRec, time.Now().Add(2*finalGapWait))
	waitFor(t, "final", func() bool { return h.transcription(meetingRec)["state"] == "final" })
	tr := h.transcription(meetingRec)
	if gaps := tr["gaps"].([]any); len(gaps) != 1 {
		t.Fatalf("the gaps the pass had no audio for: %v", tr)
	}
	p := dialPhone(t, h.sockets, meetingRec)
	if f := p.wait("error", 1); f["code"] != codeTranscriptionFinal {
		t.Fatalf("socket after the pass: %v", f)
	}
}

// A try that never reached the upstream (upstream_unavailable, nothing billed) is tried again after each of
// finalUnreachableRetries before the live sentences are final; one that billed first counts as failed, two retries
// (#9535).
func TestFinalPassRetriesWhileUnreachable(t *testing.T) {
	h := newHubFixture(t)
	h.meetingAudio(meetingRec)
	h.tr.set(func(f *fakeTranscriber) { f.replay = jobScript{refuse: extwire.TEUpstreamUnavailable} })
	finalGapWait = 0
	h.finalState(meetingRec, "final")
	if n := len(h.tr.jobsOf(extwire.TranscriptionReplay)); n != 1+len(finalUnreachableRetries) {
		t.Fatalf("%d tries", n)
	}
	m, _, _ := h.engine().MeetingRead(meetingRec)
	if tr := m.Transcription; tr.FinalTries != 6 || tr.FinalUnreachable != 6 {
		t.Fatalf("tries %d, unreachable %d", tr.FinalTries, tr.FinalUnreachable)
	}
	if got := h.hubTexts(meetingRec); !slices.Equal(got, []string{"Live one.", "Live two."}) {
		t.Fatalf("the final transcript %q", got)
	}

	h.meetingAudio(meetingOther)
	h.tr.set(func(f *fakeTranscriber) {
		f.replay = jobScript{failAt: 1, failCode: extwire.TEUpstreamUnavailable, usage: 1}
	})
	h.finalState(meetingOther, "final")
	if n := len(h.tr.jobsOf(extwire.TranscriptionReplay)) - 6; n != 1+len(finalRetries) {
		t.Fatalf("billed, then unavailable: %d tries", n)
	}
	m, _, _ = h.engine().MeetingRead(meetingOther)
	if tr := m.Transcription; tr.FinalTries != 3 || tr.FinalUnreachable != 0 || tr.UsageSeconds < 3 {
		t.Fatalf("billed, then unavailable: %+v", tr)
	}
}

// finalRetryAfter keeps the two budgets apart: unreachable tries use theirs whatever failed before, and a failed try
// counts only the tries that were not unreachable.
func TestFinalRetryAfter(t *testing.T) {
	for _, c := range []struct {
		tries, unreachable int
		now                bool
		wait               time.Duration
		ok                 bool
	}{
		{1, 0, false, time.Minute, true},
		{2, 0, false, 5 * time.Minute, true},
		{3, 0, false, 0, false},
		{1, 1, true, time.Minute, true},
		{5, 5, true, time.Hour, true},
		{6, 6, true, 0, false},
		{4, 2, true, 5 * time.Minute, true},  // two failed tries before: the unreachable budget is untouched
		{4, 2, false, 5 * time.Minute, true}, // the second failed try, after two unreachable ones
		{5, 2, false, 0, false},              // the third: unreachable tries do not lengthen the failed budget
		{7, 5, false, 5 * time.Minute, true}, // a failed try after a long outage: its second
		{0, 0, false, 0, false},
	} {
		wait, ok := finalRetryAfter(kb.MeetingTranscription{FinalTries: c.tries, FinalUnreachable: c.unreachable}, c.now)
		if wait != c.wait || ok != c.ok {
			t.Errorf("tries %d, unreachable %d, this one unreachable %v: %v %v, want %v %v", c.tries, c.unreachable, c.now, wait, ok, c.wait, c.ok)
		}
	}
}

// A pass that fails is tried again twice, then the live sentences are final; a contribution without replay makes
// them final at once; with no audio at all the state is failed.
func TestFinalPassRetriesAndFallbacks(t *testing.T) {
	h := newHubFixture(t)
	h.meetingAudio(meetingRec)
	h.tr.set(func(f *fakeTranscriber) { f.replay = jobScript{refuse: extwire.TEUpstreamFailed} })
	finalGapWait = 0
	h.finalState(meetingRec, "final")
	if n := len(h.tr.jobsOf(extwire.TranscriptionReplay)); n != 3 {
		t.Fatalf("%d tries", n)
	}
	if got := h.hubTexts(meetingRec); !slices.Equal(got, []string{"Live one.", "Live two."}) {
		t.Fatalf("the final transcript %q", got)
	}
	m, _, _ := h.engine().MeetingRead(meetingRec)
	if m.Transcription.FinalTries != 3 {
		t.Fatalf("tries %d", m.Transcription.FinalTries)
	}

	h.meetingAudio(meetingOther)
	h.tr.set(func(f *fakeTranscriber) { f.info.Features = []string{extwire.TranscriptionLive} })
	h.finalState(meetingOther, "final")
	if got := h.hubTexts(meetingOther); !slices.Equal(got, []string{"Live one.", "Live two."}) || len(h.tr.jobsOf(extwire.TranscriptionReplay)) != 3 {
		t.Fatalf("no replay: %q", got)
	}

	silent := "13131313-1313-4313-8313-131313131313"
	h.hubUpload(silent, 1, map[string]any{"complete": true, "duration": 60.0, "segments": phoneSegs(fakeLine{0, 5, "Phone only."})})
	h.finalState(silent, "failed")
	d, err := h.engine().Get(kb.KindRecording, silent, 0)
	if err != nil || len(d.Segments) != 1 || d.Segments[0].Source != kb.SourceDevice {
		t.Fatalf("a failed recording's segments: %+v %v", d, err)
	}
}

// A recording no complete upload ended is complete for the pass feedStale after its last upload.
func TestFinalPassNeverComplete(t *testing.T) {
	h := newHubFixture(t)
	h.tr.set(func(f *fakeTranscriber) {
		f.live = jobScript{lines: liveTwo[:1]}
		f.replay = jobScript{windows: []fakeWindow{{0, 2, false, []fakeLine{{0.5, 1.5, "Replay one."}}}}}
	})
	h.hubUpload(meetingRec, 1, nil)
	p, _ := h.open(meetingRec, 0, nil)
	p.audio(2)
	p.wait("line", 1)
	_ = p.c.CloseNow()
	e := h.engine()
	h.s.finalLook(context.Background(), e, meetingRec, time.Now())
	if tr := h.transcription(meetingRec); tr["state"] != "live" {
		t.Fatalf("before its end: %v", tr)
	}
	h.s.finalLook(context.Background(), e, meetingRec, time.Now().Add(feedStale+time.Minute))
	waitFor(t, "final", func() bool { return h.transcription(meetingRec)["state"] == "final" })
	if got := h.hubTexts(meetingRec); !slices.Equal(got, []string{"Replay one."}) {
		t.Fatalf("the final transcript %q", got)
	}
}

// A recording deleted, or gone with the retention, ends its socket and job.
func TestHubRunStoppedByDeleteAndRetention(t *testing.T) {
	h := newHubFixture(t)
	h.hubUpload(meetingRec, 1, nil)
	p, _ := h.open(meetingRec, 0, nil)
	p.audio(1)
	h.do("DELETE", "/api/v1/recordings/"+meetingRec, "", nil, http.StatusOK)
	if f := p.wait("error", 1); f["code"] != codeRecordingDeleted {
		t.Fatalf("deleted: %v", f)
	}
	if h.s.hubRunOf(meetingRec) != nil {
		t.Fatal("the run outlived the recording")
	}

	h.hubUpload(meetingOther, 1, nil)
	other, _ := h.open(meetingOther, 0, nil)
	other.audio(1)
	e := h.engine()
	e.PruneRecordings(time.Now().AddDate(0, 0, 40))
	h.s.hubSweep(e)
	if f := other.wait("error", 1); f["code"] != codeRecordingDeleted {
		t.Fatalf("gone with the retention: %v", f)
	}
}

// A replay job that keeps its socket open but stops answering is given up after jobDoneWait without a frame, and
// tried again: after the retries the live sentences are final, the audio no longer held by a pass that never ends.
func TestFinalPassReplayThatStopsAnswering(t *testing.T) {
	h := newHubFixture(t)
	h.meetingAudio(meetingRec)
	h.tr.set(func(f *fakeTranscriber) { f.replay = jobScript{noAck: true} })
	finalGapWait, replayAhead, jobDoneWait = 0, time.Second, 300*time.Millisecond
	h.finalState(meetingRec, "final")
	jobs := h.tr.jobsOf(extwire.TranscriptionReplay)
	if len(jobs) != 3 {
		t.Fatalf("%d tries", len(jobs))
	}
	for _, j := range jobs {
		if !j.over() {
			t.Fatal("a replay job given up is still open")
		}
	}
	if got := h.hubTexts(meetingRec); !slices.Equal(got, []string{"Live one.", "Live two."}) {
		t.Fatalf("the final transcript %q", got)
	}
}

// The recording's delete ends its final pass at once: the replay job is closed, not left transcribing a meeting that
// is gone; the trim (through the sweep) does too.
func TestFinalPassEndsWithTheRecording(t *testing.T) {
	h := newHubFixture(t)
	h.meetingAudio(meetingRec)
	h.tr.set(func(f *fakeTranscriber) { f.replay = jobScript{noAck: true} })
	finalGapWait, replayAhead, jobDoneWait = 0, time.Second, time.Minute
	h.s.finalRound(context.Background())
	waitFor(t, "the replay waiting for its acks", func() bool {
		jobs := h.tr.jobsOf(extwire.TranscriptionReplay)
		if len(jobs) != 1 {
			return false
		}
		_, audio, _, _ := jobs[0].snapshot()
		return audio > kb.AudioRate
	})
	h.do("DELETE", "/api/v1/recordings/"+meetingRec, "", nil, http.StatusOK)
	waitFor(t, "the replay job closed", func() bool { return h.tr.jobsOf(extwire.TranscriptionReplay)[0].over() })

	h.meetingAudio(meetingOther)
	h.s.finalRound(context.Background())
	waitFor(t, "the replay waiting for its acks", func() bool { return len(h.tr.jobsOf(extwire.TranscriptionReplay)) == 2 })
	e := h.engine()
	e.PruneRecordings(time.Now().AddDate(0, 0, 40))
	h.s.hubSweep(e)
	waitFor(t, "the replay job closed", func() bool { return h.tr.jobsOf(extwire.TranscriptionReplay)[1].over() })
}

// The final pass waits for the live job's last sentences after the phone's end: a complete upload right after the
// end starts no pass before the socket got its last lines and was closed 1000.
func TestFinalPassWaitsForTheLastLines(t *testing.T) {
	h := newHubFixture(t)
	gate := make(chan struct{})
	h.tr.set(func(f *fakeTranscriber) {
		f.live = jobScript{lines: liveTwo[:1], held: true, endGate: gate}
		f.replay = jobScript{windows: []fakeWindow{{0, 2, false, []fakeLine{{0.5, 1.5, "Replay one."}}}}}
	})
	h.hubUpload(meetingRec, 1, nil)
	p, _ := h.open(meetingRec, 0, nil)
	p.audio(2)
	p.send(map[string]any{"type": "end", "at": 2.0})
	waitFor(t, "the job told of the end", func() bool {
		jobs := h.tr.jobsOf(extwire.TranscriptionLive)
		if len(jobs) != 1 {
			return false
		}
		_, _, _, ended := jobs[0].snapshot()
		return ended
	})
	h.hubUpload(meetingRec, 2, map[string]any{"complete": true, "duration": 2.0})
	h.s.finalRound(context.Background())
	h.s.hubTr.mu.Lock()
	st := h.s.hubTr.passes[meetingRec]
	running := st != nil && st.running
	h.s.hubTr.mu.Unlock()
	m, _, _ := h.engine().MeetingRead(meetingRec)
	if running || !m.Transcription.FinalFrom.IsZero() {
		t.Fatal("the pass started before the last lines")
	}
	close(gate)
	if f := p.wait("line", 1); f["text"] != liveTwo[0].text {
		t.Fatalf("line %v", f)
	}
	if status := p.waitClosed(); status != websocket.StatusNormalClosure || len(p.of("error")) != 0 {
		t.Fatalf("closed %d, errors %v", status, p.of("error"))
	}
	h.finalState(meetingRec, "final")
}

// The final pass marks Persona's echo among its sentences by the timeline's spans and their answers (kb/echo.go),
// never drops it: the final transcript and detail.segments keep it marked, transcript.md marks it, and chars leaves it
// out.
func TestFinalPassMarksEcho(t *testing.T) {
	h := newHubFixture(t)
	h.meetingAudio(meetingRec)
	h.hubUpload(meetingRec, 3, map[string]any{"complete": true, "duration": 6.0, "ended_at": time.Now().UTC().Format(time.RFC3339),
		"segments": phoneSegs(fakeLine{0.4, 1.6, "Phone one."}, fakeLine{2.1, 2.9, "Phone in the gap."}),
		"timeline": map[string]any{"speaking": []map[string]any{{"start": 4.0, "end": 5.2, "text": "Bo owns the regression test."}}}})
	h.tr.set(func(f *fakeTranscriber) {
		f.replay = jobScript{windows: []fakeWindow{{0, 6, false, []fakeLine{{0.5, 1.5, "Replay one."}, {4.2, 5.0, "Bo owns the regression test."},
			{4.4, 4.9, "Wait, who owns the docs?"}}}}}
	})
	h.s.finalLook(context.Background(), h.engine(), meetingRec, time.Now().Add(2*finalGapWait))
	waitFor(t, "final", func() bool { return h.transcription(meetingRec)["state"] == "final" })
	if got, echo := h.hubTexts(meetingRec), h.hubEcho(meetingRec); len(got) != 3 || !slices.Equal(echo, []bool{false, true, false}) {
		t.Fatalf("the final transcript %q, echo %v", got, echo)
	}
	d, err := h.engine().Get(kb.KindRecording, meetingRec, 0)
	if err != nil {
		t.Fatal(err)
	}
	var marked []string
	for _, sg := range d.Segments {
		if sg.Echo {
			marked = append(marked, sg.Text)
		}
	}
	if !slices.Equal(marked, []string{"Bo owns the regression test."}) {
		t.Fatalf("detail.segments %+v", d.Segments)
	}
	md, err := os.ReadFile(filepath.Join(h.s.kb.dir, kb.KindRecording, meetingRec, "transcript.md"))
	if err != nil || !strings.Contains(string(md), "[Persona 回声] Bo owns the regression test.") {
		t.Fatalf("transcript.md %s %v", md, err)
	}
	r, ok := h.engine().Store.Recording(meetingRec)
	if want := len("Replay one.") + len("Wait, who owns the docs?") + len("Phone in the gap."); !ok || r.Chars != want {
		t.Fatalf("chars %d, want %d (Persona's echo left out)", r.Chars, want)
	}
}

// Without a replay the live sentences are final, and the complete upload's timeline marks Persona's echo among them
// too (a span the live pass never saw closed, repaired late), never dropping it.
func TestFinalLiveSentencesMarkedByTheLateTimeline(t *testing.T) {
	h := newHubFixture(t)
	h.meetingAudio(meetingRec)
	h.hubUpload(meetingRec, 3, map[string]any{"complete": true, "duration": 6.0, "ended_at": time.Now().UTC().Format(time.RFC3339),
		"timeline": map[string]any{"speaking": []map[string]any{{"start": 4.0, "end": 5.2, "text": "Live two."}}}})
	h.tr.set(func(f *fakeTranscriber) { f.info.Features = []string{extwire.TranscriptionLive} })
	finalGapWait = 0
	h.finalState(meetingRec, "final")
	if got, echo := h.hubTexts(meetingRec), h.hubEcho(meetingRec); !slices.Equal(got, []string{"Live one.", "Live two."}) ||
		!slices.Equal(echo, []bool{false, true}) {
		t.Fatalf("the final transcript %q, echo %v", got, echo)
	}
}
