package assistant

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/assistantwire"
	"github.com/nwverse/persona/server/internal/extwire"
	"github.com/nwverse/persona/server/internal/kb"
	"github.com/nwverse/persona/server/internal/rpc"
)

// Transcription for extensions (#8485): an extension's audio, a piece at a time, on the chosen contribution's replay;
// the sentences of the windows it trusted, and the windows it did not as gaps.

// extTranscribeFixture is a manager with a fake transcription contribution.
func extTranscribeFixture(t *testing.T) (*Service, *fakeTranscriber) {
	t.Helper()
	shortHubTimes(t)
	m := newMeetingFixture(t)
	tr := newFakeTranscriber(t)
	m.s.trans = tr
	t.Cleanup(func() {
		m.s.extTr.mu.Lock()
		jobs := make([]*extJob, 0, len(m.s.extTr.jobs))
		for _, j := range m.s.extTr.jobs {
			jobs = append(jobs, j)
		}
		m.s.extTr.mu.Unlock()
		for _, j := range jobs {
			m.s.endExtJob(j, "test over")
		}
	})
	return m.s, tr
}

// extTr is one of extension ext's transcription calls as the manager hands it over, its answer decoded into out.
func extTr(t *testing.T, s *Service, ext, method string, params, out any) error {
	t.Helper()
	raw, _ := json.Marshal(params)
	call, _ := json.Marshal(assistantwire.ExtensionTranscription{Extension: ext, Method: method, Params: raw})
	res, ok, err := s.transcriptionRequest(context.Background(), assistantwire.MethodTranscriptionExtension, call)
	if !ok {
		t.Fatalf("%s: not answered", method)
	}
	if err == nil && out != nil {
		b, _ := json.Marshal(res)
		if err := json.Unmarshal(b, out); err != nil {
			t.Fatal(err)
		}
	}
	return err
}

// pcmB64 is seconds of silence as transcription.audio's data.
func pcmB64(seconds float64) string {
	return base64.StdEncoding.EncodeToString(make([]byte, int(seconds*kb.AudioRate)*2))
}

func startJob(t *testing.T, s *Service, ext string, p assistantwire.TranscriptionStartParams) string {
	t.Helper()
	var out assistantwire.TranscriptionStartResult
	if err := extTr(t, s, ext, assistantwire.MethodTranscriptionStart, p, &out); err != nil || out.Job == "" {
		t.Fatalf("start: %q %v", out.Job, err)
	}
	return out.Job
}

func TestExtensionTranscribesItsAudio(t *testing.T) {
	s, tr := extTranscribeFixture(t)
	tr.set(func(f *fakeTranscriber) {
		f.replay = jobScript{usage: 24, windows: []fakeWindow{
			{0, 3, false, []fakeLine{{0.5, 1.5, "Crack two eggs."}}},
			{3, 6, true, nil}, // the sessions did not agree: a gap
			{6, 8, false, []fakeLine{{6.2, 7, "Serve warm."}}},
		}}
	})
	job := startJob(t, s, "example-video", assistantwire.TranscriptionStartParams{Language: "zh-CN", Label: "clip n-1\nsecond line"})
	for _, sec := range []float64{3, 3, 2} {
		if err := extTr(t, s, "example-video", assistantwire.MethodTranscriptionAudio,
			assistantwire.TranscriptionAudioParams{Job: job, Data: pcmB64(sec)}, nil); err != nil {
			t.Fatalf("audio: %v", err)
		}
	}
	var out assistantwire.TranscriptionFinishResult
	if err := extTr(t, s, "example-video", assistantwire.MethodTranscriptionFinish, assistantwire.TranscriptionJobParams{Job: job}, &out); err != nil {
		t.Fatalf("finish: %v", err)
	}
	want := []assistantwire.TranscriptionSegment{{From: 0.5, To: 1.5, Text: "Crack two eggs."}, {From: 6.2, To: 7, Text: "Serve warm."}}
	if len(out.Segments) != 2 || out.Segments[0] != want[0] || out.Segments[1] != want[1] {
		t.Fatalf("segments %+v", out.Segments)
	}
	if len(out.Gaps) != 1 || out.Gaps[0] != (assistantwire.TranscriptionGap{From: 3, To: 6}) || out.Seconds != 8 {
		t.Fatalf("gaps %+v, seconds %v", out.Gaps, out.Seconds)
	}
	jobs := tr.jobsOf(extwire.TranscriptionReplay)
	if len(jobs) != 1 {
		t.Fatalf("%d replay jobs", len(jobs))
	}
	st := jobs[0].start
	if st.Speed != 4 || st.Language != "zh-CN" || st.At == nil || *st.At != 0 {
		t.Fatalf("start %+v", st)
	}
	if _, samples, _, ended := jobs[0].snapshot(); samples != 8*kb.AudioRate || !ended {
		t.Fatalf("the contribution got %d samples, ended %v", samples, ended)
	}
	waitFor(t, "the job's socket closed", func() bool { return jobs[0].over() })
	// Finished: the job is gone, and the extension can start another.
	if err := extTr(t, s, "example-video", assistantwire.MethodTranscriptionAudio,
		assistantwire.TranscriptionAudioParams{Job: job, Data: pcmB64(1)}, nil); codeOf(err) != "invalid_params" {
		t.Fatalf("audio after finish: %v", err)
	}
	startJob(t, s, "example-video", assistantwire.TranscriptionStartParams{})
	if transcriptionLabel("clip n-1\nsecond line") != "clip n-1" || transcriptionLabel(strings.Repeat("字", 50)) != strings.Repeat("字", 40)+"…" {
		t.Fatal("labels")
	}
}

func TestExtensionTranscriptionLimits(t *testing.T) {
	s, tr := extTranscribeFixture(t)
	tr.set(func(f *fakeTranscriber) {
		f.replay = jobScript{windows: []fakeWindow{{0, 1, false, []fakeLine{{0.1, 0.9, "One."}}}}}
	})
	a := startJob(t, s, "ext-a", assistantwire.TranscriptionStartParams{})
	if err := extTr(t, s, "ext-a", assistantwire.MethodTranscriptionStart, assistantwire.TranscriptionStartParams{}, nil); codeOf(err) != "busy" {
		t.Fatalf("a second job of one extension: %v", err)
	}
	startJob(t, s, "ext-b", assistantwire.TranscriptionStartParams{})
	if err := extTr(t, s, "ext-c", assistantwire.MethodTranscriptionStart, assistantwire.TranscriptionStartParams{}, nil); codeOf(err) != "busy" {
		t.Fatalf("a third extension's job: %v", err)
	}
	for name, p := range map[string]any{
		"over 256 KiB":     assistantwire.TranscriptionAudioParams{Job: a, Data: base64.StdEncoding.EncodeToString(make([]byte, 256<<10+2))},
		"odd bytes":        assistantwire.TranscriptionAudioParams{Job: a, Data: base64.StdEncoding.EncodeToString(make([]byte, 3))},
		"not base64":       assistantwire.TranscriptionAudioParams{Job: a, Data: "not base64!"},
		"an unknown field": map[string]any{"job": a, "data": pcmB64(1), "rate": 8000},
		"no such job":      assistantwire.TranscriptionAudioParams{Job: "nope", Data: pcmB64(1)},
	} {
		if err := extTr(t, s, "ext-a", assistantwire.MethodTranscriptionAudio, p, nil); codeOf(err) != "invalid_params" {
			t.Errorf("%s: %v", name, err)
		}
	}
	// Another extension's job is not there for it.
	for _, m := range []string{assistantwire.MethodTranscriptionAudio, assistantwire.MethodTranscriptionFinish, assistantwire.MethodTranscriptionCancel} {
		if err := extTr(t, s, "ext-b", m, assistantwire.TranscriptionAudioParams{Job: a, Data: pcmB64(1)}, nil); codeOf(err) != "invalid_params" {
			t.Errorf("%s of another extension's job: %v", m, err)
		}
	}
	// Cancel gives the place back.
	if err := extTr(t, s, "ext-a", assistantwire.MethodTranscriptionCancel, assistantwire.TranscriptionJobParams{Job: a}, nil); err != nil {
		t.Fatal(err)
	}
	c := startJob(t, s, "ext-c", assistantwire.TranscriptionStartParams{})

	// Too long ends the job.
	old := extTranscribeSamplesMost
	extTranscribeSamplesMost = 2 * kb.AudioRate
	t.Cleanup(func() { extTranscribeSamplesMost = old })
	if err := extTr(t, s, "ext-c", assistantwire.MethodTranscriptionAudio, assistantwire.TranscriptionAudioParams{Job: c, Data: pcmB64(2)}, nil); err != nil {
		t.Fatal(err)
	}
	if err := extTr(t, s, "ext-c", assistantwire.MethodTranscriptionAudio, assistantwire.TranscriptionAudioParams{Job: c, Data: pcmB64(0.5)}, nil); codeOf(err) != "too_long" {
		t.Fatalf("past the most audio: %v", err)
	}
	if err := extTr(t, s, "ext-c", assistantwire.MethodTranscriptionFinish, assistantwire.TranscriptionJobParams{Job: c}, nil); codeOf(err) != "invalid_params" {
		t.Fatalf("finish after too_long: %v", err)
	}
}

func TestExtensionTranscriptionUnavailable(t *testing.T) {
	s, tr := extTranscribeFixture(t)
	for name, set := range map[string]func(f *fakeTranscriber){
		"not set up":          func(f *fakeTranscriber) { f.info.Setup = "upstream" },
		"no replay":           func(f *fakeTranscriber) { f.info.Features = []string{extwire.TranscriptionLive} },
		"info fails":          func(f *fakeTranscriber) { f.infoErr = errors.New("not running") },
		"refuses the job":     func(f *fakeTranscriber) { f.replay = jobScript{refuse: extwire.TEUpstreamUnavailable} },
		"never ready":         func(f *fakeTranscriber) { f.replay = jobScript{noReady: true} },
		"the dial is refused": func(f *fakeTranscriber) { f.dialErr = errors.New("refused") },
	} {
		t.Run(name, func(t *testing.T) {
			tr.set(func(f *fakeTranscriber) {
				f.info = extwire.TranscriptionInfo{Protocol: extwire.TranscriptionProtocol,
					Features: []string{extwire.TranscriptionLive, extwire.TranscriptionReplay}}
				f.infoErr, f.dialErr, f.replay = nil, nil, jobScript{}
				set(f)
			})
			if err := extTr(t, s, "ext-a", assistantwire.MethodTranscriptionStart, assistantwire.TranscriptionStartParams{}, nil); codeOf(err) != "transcription_unavailable" {
				t.Fatalf("%v", err)
			}
			s.extTr.mu.Lock()
			n := len(s.extTr.jobs)
			s.extTr.mu.Unlock()
			if n != 0 {
				t.Fatalf("%d jobs held after an unavailable start", n)
			}
		})
	}
}

// A job the extension leaves alone is cancelled; one the contribution fails says so with its code.
func TestExtensionTranscriptionIdleAndFailure(t *testing.T) {
	s, tr := extTranscribeFixture(t)
	old := extTranscribeIdle
	extTranscribeIdle = 150 * time.Millisecond
	t.Cleanup(func() { extTranscribeIdle = old })
	idle := startJob(t, s, "ext-a", assistantwire.TranscriptionStartParams{})
	waitFor(t, "the idle job cancelled", func() bool {
		s.extTr.mu.Lock()
		defer s.extTr.mu.Unlock()
		return s.extTr.jobs[idle] == nil
	})
	if jobs := tr.jobsOf(extwire.TranscriptionReplay); len(jobs) != 1 {
		t.Fatalf("%d jobs", len(jobs))
	} else {
		waitFor(t, "its socket closed", func() bool { return jobs[0].over() })
	}

	extTranscribeIdle = time.Minute
	tr.set(func(f *fakeTranscriber) { f.replay = jobScript{failAt: 1} })
	job := startJob(t, s, "ext-a", assistantwire.TranscriptionStartParams{})
	err := extTr(t, s, "ext-a", assistantwire.MethodTranscriptionAudio, assistantwire.TranscriptionAudioParams{Job: job, Data: pcmB64(2)}, nil)
	if err == nil {
		err = extTr(t, s, "ext-a", assistantwire.MethodTranscriptionFinish, assistantwire.TranscriptionJobParams{Job: job}, nil)
	}
	var re *rpc.Error
	if !errors.As(err, &re) || re.Code != "transcription_failed" || !strings.Contains(string(re.Data), extwire.TEUpstreamFailed) {
		t.Fatalf("a failed job: %v", err)
	}
}

func TestExtensionTranscriptionActiveCallIsNotIdle(t *testing.T) {
	s, tr := extTranscribeFixture(t)
	oldIdle, oldWait, oldAhead := extTranscribeIdle, jobDoneWait, replayAhead
	extTranscribeIdle, jobDoneWait, replayAhead = 150*time.Millisecond, 750*time.Millisecond, time.Second
	t.Cleanup(func() { extTranscribeIdle, jobDoneWait, replayAhead = oldIdle, oldWait, oldAhead })
	tr.set(func(f *fakeTranscriber) { f.replay = jobScript{noAck: true} })
	job := startJob(t, s, "ext-a", assistantwire.TranscriptionStartParams{})
	began := time.Now()
	err := extTr(t, s, "ext-a", assistantwire.MethodTranscriptionAudio,
		assistantwire.TranscriptionAudioParams{Job: job, Data: pcmB64(3)}, nil)
	if codeOf(err) != assistantwire.CodeTranscriptionTimeout {
		t.Fatalf("active audio RPC ended after %v with %T %v (code %q), want provider timeout", time.Since(began), err, err, codeOf(err))
	}
	waitFor(t, "job socket closed", func() bool { return tr.jobsOf(extwire.TranscriptionReplay)[0].over() })
}

// A contribution that stops answering ends the job with timeout.
func TestExtensionTranscriptionTimeout(t *testing.T) {
	s, tr := extTranscribeFixture(t)
	jobDoneWait = 300 * time.Millisecond
	replayAhead = time.Second
	tr.set(func(f *fakeTranscriber) { f.replay = jobScript{noAck: true} })
	job := startJob(t, s, "ext-a", assistantwire.TranscriptionStartParams{})
	err := extTr(t, s, "ext-a", assistantwire.MethodTranscriptionAudio, assistantwire.TranscriptionAudioParams{Job: job, Data: pcmB64(3)}, nil)
	if codeOf(err) != "timeout" {
		t.Fatalf("a contribution that acknowledges nothing: %v", err)
	}
}
