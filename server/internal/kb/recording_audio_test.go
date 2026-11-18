package kb

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"testing"
	"time"
)

// pcmAt is n samples of audio from sample from, each sample saying where it is (so a read shows what was stored where).
func pcmAt(from, n int64) []byte {
	out := make([]byte, 2*n)
	for k := range n {
		binary.LittleEndian.PutUint16(out[2*k:], uint16((from+k)*7+1))
	}
	return out
}

// secs is t seconds in samples.
func secs(t float64) int64 { return atSample(t) }

// hubMeeting stores recording id as a hub-mode meeting, not complete yet, started an hour before now.
func hubMeeting(t *testing.T, e *Engine, id string, now time.Time) {
	t.Helper()
	putRecording(t, e, id, hubUpload(1, now.Add(-time.Hour), false, nil), now)
}

func audioDirOf(e *Engine, id string) string { return e.Store.audioPath(id) }

// The socket's writes: stored from where they say, what is held already ignored, a seek forward leaving a gap that
// meeting.json's gaps (and PartTranscription) follow; ack is the end of the contiguous audio around the latest write,
// HeldTo the end of the latest held; reads cross holes only as far as the audio goes.
func TestAudioWriteSyncAndHeld(t *testing.T) {
	now := time.Date(2026, 11, 10, 14, 0, 0, 0, goldenZone)
	e, changed := hubEngine(t, nil)
	hubMeeting(t, e, recID, now)
	*changed = nil
	w, err := e.AudioOpen(recID)
	if err != nil {
		t.Fatal(err)
	}
	if w.HeldTo() != 0 {
		t.Fatalf("fresh %v", w.HeldTo())
	}
	if held, _ := e.AudioHeld(recID); len(held) != 0 {
		t.Fatalf("fresh held %v", held)
	}
	write := func(at float64, n int64) {
		t.Helper()
		if err := w.Write(at, pcmAt(secs(at), n)); err != nil {
			t.Fatal(err)
		}
	}
	write(0, secs(1))
	write(1, secs(1))
	// Held already: ignored (another body for the same seconds changes nothing).
	if err := w.Write(0.5, bytes.Repeat([]byte{0xff}, int(2*secs(1)))); err != nil {
		t.Fatal(err)
	}
	if ack, err := w.Sync(); err != nil || ack != 2 {
		t.Fatalf("ack %v %v", ack, err)
	}
	if len(*changed) != 0 {
		t.Fatalf("contiguous audio moved %v", *changed)
	}
	// A seek forward: the span between is a gap.
	write(5, secs(1))
	if ack, err := w.Sync(); err != nil || ack != 6 {
		t.Fatalf("ack after the seek %v %v", ack, err)
	}
	if held, _ := e.AudioHeld(recID); !sameSpans(held, [][2]float64{{0, 2}, {5, 6}}) || w.HeldTo() != 6 {
		t.Fatalf("held %v to %v", held, w.HeldTo())
	}
	if tr := transcriptionNow(t, e, recID); !sameSpans(tr.Gaps, [][2]float64{{2, 5}}) {
		t.Fatalf("gaps %v", tr.Gaps)
	}
	if len(*changed) != 1 || !slices.Equal((*changed)[0], []string{PartTranscription}) {
		t.Fatalf("told %v", *changed)
	}
	// Bad frames.
	if err := w.Write(7, []byte{1, 2, 3}); !errors.Is(err, ErrBadAudio) {
		t.Fatalf("odd: %v", err)
	}
	if err := w.Write(-1, pcmAt(0, 2)); !errors.Is(err, ErrBadAudio) {
		t.Fatalf("negative: %v", err)
	}
	// Filling the hole late: the gap closes; ack is the contiguous end around it.
	write(2, secs(3))
	if ack, err := w.Sync(); err != nil || ack != 6 {
		t.Fatalf("ack after the fill %v %v", ack, err)
	}
	if tr := transcriptionNow(t, e, recID); len(tr.Gaps) != 0 {
		t.Fatalf("gaps after the fill %v", tr.Gaps)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("closed twice: %v", err)
	}
	// What was stored where: the first body over 0–1, not the one sent again.
	p := make([]byte, 2*secs(1))
	if n, err := e.AudioRead(recID, 0.5, p); err != nil || n != len(p) || !bytes.Equal(p, pcmAt(secs(0.5), secs(1))) {
		t.Fatalf("read %d %v", n, err)
	}
	if n, err := e.AudioRead(recID, 6, p); !errors.Is(err, io.EOF) || n != 0 {
		t.Fatalf("past the end %d %v", n, err)
	}
	var idx audioIndex
	if raw, err := os.ReadFile(filepath.Join(audioDirOf(e, recID), audioIndexFile)); err != nil || json.Unmarshal(raw, &idx) != nil ||
		idx.Rate != AudioRate || !sameSpans(idx.Spans, [][2]float64{{0, 6}}) {
		t.Fatalf("index %+v %v", idx, err)
	}
	// The socket's end: the gaps run to it. The final pass's start freezes them.
	*changed = nil
	if tr, err := e.TranscriptionUpdate(recID, func(t *MeetingTranscription, _ Recording) error { t.End = 8; return nil }); err != nil ||
		!sameSpans(tr.Gaps, [][2]float64{{6, 8}}) || len(*changed) != 1 {
		t.Fatalf("end %+v %v, told %v", tr.Gaps, err, *changed)
	}
	if tr, err := e.TranscriptionUpdate(recID, func(t *MeetingTranscription, _ Recording) error {
		t.State, t.FinalFrom = TranscriptionFinishing, time.Now()
		return nil
	}); err != nil || !sameSpans(tr.Gaps, [][2]float64{{6, 8}}) {
		t.Fatalf("final pass %+v %v", tr.Gaps, err)
	}
	if _, err := e.AudioPut(recID, 6, pcmAt(secs(6), secs(2))); !errors.Is(err, ErrTranscriptionFinal) {
		t.Fatalf("gap upload after the start: %v", err)
	}
	if tr := transcriptionNow(t, e, recID); !sameSpans(tr.Gaps, [][2]float64{{6, 8}}) {
		t.Fatalf("frozen %v", tr.Gaps)
	}
}

// Chunk files roll over at their size and the reads go across them and stop at holes; a store opened again (a
// restart) finds what was written from the files alone, the tail of a write a crash cut short to whole samples.
func TestAudioChunksAndRestart(t *testing.T) {
	defer func(n int64) { audioChunkMost = n }(audioChunkMost)
	audioChunkMost = 1600 // 0.1 s
	now := time.Date(2026, 11, 10, 14, 0, 0, 0, goldenZone)
	e, _ := hubEngine(t, nil)
	hubMeeting(t, e, recID, now)
	w, err := e.AudioOpen(recID)
	if err != nil {
		t.Fatal(err)
	}
	if err := w.Write(0, pcmAt(0, 5000)); err != nil {
		t.Fatal(err)
	}
	if err := w.Write(1, pcmAt(16000, 800)); err != nil { // after a hole
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	var names []string
	entries, _ := os.ReadDir(audioDirOf(e, recID))
	for _, ent := range entries {
		names = append(names, ent.Name())
	}
	slices.Sort(names)
	if want := []string{"0.pcm", "1600.pcm", "16000.pcm", "3200.pcm", "4800.pcm", audioIndexFile}; !slices.Equal(names, want) {
		t.Fatalf("files %v", names)
	}
	p := make([]byte, 2*6000)
	if n, err := e.AudioRead(recID, 0.05, p); err != nil || n != 2*(5000-800) || !bytes.Equal(p[:n], pcmAt(800, 5000-800)) {
		t.Fatalf("across chunks %d %v", n, err)
	}
	if n, err := e.AudioRead(recID, 0.5, p); err != nil || n != 0 {
		t.Fatalf("in the hole %d %v", n, err)
	}
	if n, err := e.AudioRead(recID, 1.01, p); err != nil || n != 2*640 || !bytes.Equal(p[:n], pcmAt(16160, 640)) {
		t.Fatalf("after the hole %d %v", n, err)
	}

	// A write cut short by a crash: a byte past the last whole sample.
	f, err := os.OpenFile(filepath.Join(audioDirOf(e, recID), "16000.pcm"), os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	f.Write([]byte{9})
	f.Close()
	st, err := NewStore(e.Store.Dir)
	if err != nil {
		t.Fatal(err)
	}
	again := &Engine{Store: st, TranscriptionReady: e.TranscriptionReady}
	w2, err := again.AudioOpen(recID)
	if err != nil {
		t.Fatal(err)
	}
	if w2.HeldTo() != sampleAt(16800) {
		t.Fatalf("after a restart %v", w2.HeldTo())
	}
	if err := w2.Write(sampleAt(16800), pcmAt(16800, 10)); err != nil {
		t.Fatal(err)
	}
	if n, err := again.AudioRead(recID, sampleAt(16790), p[:40]); err != nil || n != 40 || !bytes.Equal(p[:40], pcmAt(16790, 20)) {
		t.Fatalf("appended after a restart %d %v", n, err)
	}
	if held, _ := again.AudioHeld(recID); !sameSpans(held, [][2]float64{{0, sampleAt(5000)}, {1, sampleAt(16810)}}) {
		t.Fatalf("held %v", held)
	}
	_ = w2.Close()
}

// At most MaxAudioSeconds of a recording: audio past it is refused (what lies before is stored), for the socket and the
// gap upload alike.
func TestAudioFull(t *testing.T) {
	now := time.Date(2026, 11, 10, 14, 0, 0, 0, goldenZone)
	e, _ := hubEngine(t, nil)
	hubMeeting(t, e, recID, now)
	w, err := e.AudioOpen(recID)
	if err != nil {
		t.Fatal(err)
	}
	last := float64(MaxAudioSeconds) - 0.5
	if err := w.Write(last, pcmAt(secs(last), secs(1))); !errors.Is(err, ErrAudioFull) {
		t.Fatalf("over: %v", err)
	}
	if err := w.Write(MaxAudioSeconds, pcmAt(secs(MaxAudioSeconds), 10)); !errors.Is(err, ErrAudioFull) {
		t.Fatalf("past: %v", err)
	}
	_ = w.Close()
	if held, _ := e.AudioHeld(recID); !sameSpans(held, [][2]float64{{last, MaxAudioSeconds}}) {
		t.Fatalf("held %v", held)
	}
	if gaps, err := e.AudioPut(recID, MaxAudioSeconds+1, pcmAt(0, 10)); !errors.Is(err, ErrAudioFull) || gaps == nil {
		t.Fatalf("put past: %v %v", gaps, err)
	}
}

// PUT …/audio stores what the hub lacks and answers the gaps left, up to the recording's end once known; its refusals
// come in the contract's order, the socket's (AudioOpen) likewise without the body's.
func TestAudioPutAndRefusals(t *testing.T) {
	now := time.Date(2026, 11, 10, 14, 0, 0, 0, goldenZone)
	e, changed := hubEngine(t, nil)
	u := hubUpload(1, now.Add(-time.Hour), true, nil, "一句")
	u.Duration = 10
	putRecording(t, e, recID, u, now)
	if tr := transcriptionNow(t, e, recID); !sameSpans(tr.Gaps, [][2]float64{{0, 10}}) {
		t.Fatalf("nothing held of a known end: %v", tr.Gaps)
	}
	*changed = nil
	gaps, err := e.AudioPut(recID, 2, pcmAt(secs(2), secs(3)))
	if err != nil || !sameSpans(gaps, [][2]float64{{0, 2}, {5, 10}}) {
		t.Fatalf("put %v %v", gaps, err)
	}
	if gaps, err = e.AudioPut(recID, 0, pcmAt(0, secs(10))); err != nil || len(gaps) != 0 || gaps == nil {
		t.Fatalf("filled %v %v", gaps, err)
	}
	p := make([]byte, 2*secs(10))
	if n, err := e.AudioRead(recID, 0, p); err != nil || n != len(p) || !bytes.Equal(p, pcmAt(0, secs(10))) {
		t.Fatalf("read %d %v", n, err)
	}
	if len(*changed) != 2 {
		t.Fatalf("told %v", *changed)
	}

	// The refusals.
	const dev, trimmed, final, deleted = "5b6c3f4a-1d2e-4f80-9a1b-2c3d4e5f6a7b", "6c7d4e5b-2e3f-4a91-8b2c-3d4e5f6a7b8c",
		"7d8e5f6c-3f40-4b02-9c3d-4e5f6a7b8c9d", "8e9f6a7d-4051-4c13-8d4e-5f6a7b8c9dae"
	du := meetingUpload(1, now.Add(-time.Hour), "一句")
	du.SetTranscription(TranscriptionDevice, nil)
	putRecording(t, e, dev, du, now)
	putRecording(t, e, trimmed, hubUpload(1, now.Add(-time.Hour), true, nil, "一句"), now)
	putMeetingMinutes(t, e, trimmed, Minutes{Version: 1, Title: "Design review", When: "w"})
	if _, _, err := e.Store.trimRecording(recordingNow(t, e, trimmed), now); err != nil {
		t.Fatal(err)
	}
	hubMeeting(t, e, final, now)
	if _, err := e.TranscriptionUpdate(final, func(t *MeetingTranscription, _ Recording) error { t.FinalFrom = now; return nil }); err != nil {
		t.Fatal(err)
	}
	hubMeeting(t, e, deleted, now)
	if _, err := e.DeleteRecording(deleted, now); err != nil {
		t.Fatal(err)
	}
	ro, err := OpenReadOnly(e.Store.Dir)
	if err != nil {
		t.Fatal(err)
	}
	roe := &Engine{Store: ro}
	const missing = "9fa07b8e-5162-4d24-9e5f-6a7b8c9daebf"
	odd := []byte{1, 2, 3}
	for _, c := range []struct {
		name      string
		e         *Engine
		id        string
		at        float64
		pcm       []byte
		put, open error
	}{
		{"deleted", e, deleted, -1, odd, ErrRecordingDeleted, ErrRecordingDeleted},
		{"no upload", e, missing, -1, odd, ErrNotFound, ErrNotFound},
		{"not an id", e, "nope", 0, nil, ErrNotFound, ErrNotFound},
		{"bad body before not_hub", e, dev, 0, odd, ErrBadAudio, ErrNotHub},
		{"negative at", e, dev, -1, nil, ErrBadAudio, ErrNotHub},
		{"bad body before read-only", roe, recID, 0, odd, ErrBadAudio, ErrReadOnly},
		{"read-only", roe, recID, 0, nil, ErrReadOnly, ErrReadOnly},
		{"not hub", e, dev, 0, nil, ErrNotHub, ErrNotHub},
		{"trimmed", e, trimmed, 0, nil, ErrTranscriptExpired, ErrTranscriptExpired},
		{"final pass", e, final, 0, nil, ErrTranscriptionFinal, ErrTranscriptionFinal},
	} {
		if _, err := c.e.AudioPut(c.id, c.at, c.pcm); !errors.Is(err, c.put) {
			t.Errorf("%s: put %v, want %v", c.name, err, c.put)
		}
		if _, err := c.e.AudioOpen(c.id); !errors.Is(err, c.open) {
			t.Errorf("%s: open %v, want %v", c.name, err, c.open)
		}
	}
	if _, err := os.Stat(e.Store.Path(KindRecording, deleted)); !os.IsNotExist(err) {
		t.Fatalf("a refused write made the deleted recording's directory: %v", err)
	}
	// The reads work on a read-only knowledge base.
	if held, err := roe.AudioHeld(recID); err != nil || !sameSpans(held, [][2]float64{{0, 10}}) {
		t.Fatalf("read-only held %v %v", held, err)
	}
}

// The final pass's start closes the audio to writes, an open socket's included; its reads go on.
func TestAudioFinalClosesWrites(t *testing.T) {
	now := time.Date(2026, 11, 10, 14, 0, 0, 0, goldenZone)
	e, _ := hubEngine(t, nil)
	hubMeeting(t, e, recID, now)
	w, err := e.AudioOpen(recID)
	if err != nil {
		t.Fatal(err)
	}
	if err := w.Write(0, pcmAt(0, 100)); err != nil {
		t.Fatal(err)
	}
	if _, err := e.TranscriptionUpdate(recID, func(t *MeetingTranscription, _ Recording) error {
		t.State, t.FinalFrom = TranscriptionFinishing, now
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := w.Write(sampleAt(100), pcmAt(100, 100)); !errors.Is(err, ErrTranscriptionFinal) {
		t.Fatalf("write after the start: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	p := make([]byte, 400)
	if n, err := e.AudioRead(recID, 0, p); err != nil || n != 200 {
		t.Fatalf("read %d %v", n, err)
	}
}

// The recording's delete, the trim (with and without photos the minutes cite) and AudioDelete take the audio, and no
// write after them, nor one racing them, brings it back.
func TestAudioGoesWithTheRecording(t *testing.T) {
	now := time.Date(2026, 11, 10, 14, 0, 0, 0, goldenZone)
	e, _ := hubEngine(t, nil)

	// The delete, with a socket open.
	hubMeeting(t, e, recID, now)
	w, err := e.AudioOpen(recID)
	if err != nil {
		t.Fatal(err)
	}
	if err := w.Write(0, pcmAt(0, 100)); err != nil {
		t.Fatal(err)
	}
	if _, err := e.DeleteRecording(recID, now); err != nil {
		t.Fatal(err)
	}
	if err := w.Write(1, pcmAt(secs(1), 100)); !errors.Is(err, ErrRecordingDeleted) {
		t.Fatalf("write after the delete: %v", err)
	}
	if _, err := w.Sync(); !errors.Is(err, ErrRecordingDeleted) {
		t.Fatalf("sync after the delete: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("close after the delete: %v", err)
	}
	if _, err := os.Stat(e.Store.Path(KindRecording, recID)); !os.IsNotExist(err) {
		t.Fatalf("the audio brought a deleted recording back: %v", err)
	}

	// The trim, without photos and with one the minutes cite.
	for i, id := range []string{"5b6c3f4a-1d2e-4f80-9a1b-2c3d4e5f6a7b", "6c7d4e5b-2e3f-4a91-8b2c-3d4e5f6a7b8c"} {
		putRecording(t, e, id, hubUpload(1, now.Add(-time.Hour), true, nil, "第一句", "第二句"), now)
		w, err := e.AudioOpen(id)
		if err != nil {
			t.Fatal(err)
		}
		if err := w.Write(0, pcmAt(0, 100)); err != nil {
			t.Fatal(err)
		}
		cites := Minutes{Version: 1, Title: "Design review", When: "w"}
		if i == 1 {
			addPhoto(t, e, id, photoUpload(1, 5, "白板", "Q3"), false)
			cites.Points = []MinutesPoint{{Text: "白板", Photo: 1}}
		}
		putMeetingMinutes(t, e, id, cites)
		if _, _, err := e.Store.trimRecording(recordingNow(t, e, id), now); err != nil {
			t.Fatal(err)
		}
		if err := w.Write(1, pcmAt(secs(1), 100)); !errors.Is(err, ErrTranscriptExpired) {
			t.Fatalf("write after the trim: %v", err)
		}
		_ = w.Close()
		if _, err := os.Stat(audioDirOf(e, id)); !os.IsNotExist(err) {
			t.Fatalf("the trim kept the audio: %v", err)
		}
		_, photoErr := os.Stat(e.Store.Path(KindRecording, id, photosDir, "p1.jpg"))
		if (i == 1) != (photoErr == nil) {
			t.Fatalf("cited photo: %v", photoErr)
		}
	}
	// Through the retention: the trimmed recording keeps its mode and state, no gaps, its audio noted gone.
	const kept = "7d8e5f6c-3f40-4b02-9c3d-4e5f6a7b8c9d"
	old := now.AddDate(0, 0, -20)
	u := hubUpload(1, old, true, &Timeline{Speaking: spk(1, 2)}, "一句")
	u.Duration = 10
	putRecording(t, e, kept, u, old)
	if _, err := e.AudioPut(kept, 0, pcmAt(0, secs(1))); err != nil {
		t.Fatal(err)
	}
	putMeetingMinutes(t, e, kept, Minutes{Version: 1, Title: "Design review", When: "w"})
	e.PruneRecordings(now)
	tr := transcriptionNow(t, e, kept)
	if tr.Mode != TranscriptionHub || tr.State != TranscriptionLive || tr.Gaps != nil || tr.Timeline != nil || tr.AudioGoneAt.IsZero() {
		t.Fatalf("trimmed %+v", tr)
	}
	m, _, _ := e.MeetingRead(kept)
	if v := m.TranscriptionView(nil); v == nil || v.State != TranscriptionLive || v.Gaps == nil || len(*v.Gaps) != 0 {
		t.Fatalf("trimmed view %+v", v)
	}
	if _, err := os.Stat(audioDirOf(e, kept)); !os.IsNotExist(err) {
		t.Fatalf("retention kept the audio: %v", err)
	}

	// AudioDelete: again is no error; nothing to read, nothing more taken.
	const gone = "8e9f6a7d-4051-4c13-8d4e-5f6a7b8c9dae"
	hubMeeting(t, e, gone, now)
	if _, err := e.AudioPut(gone, 0, pcmAt(0, 100)); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := e.AudioDelete(gone); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := os.Stat(audioDirOf(e, gone)); !os.IsNotExist(err) {
		t.Fatalf("audio left: %v", err)
	}
	if tr := transcriptionNow(t, e, gone); tr.AudioGoneAt.IsZero() {
		t.Fatalf("not noted %+v", tr)
	}
	if held, err := e.AudioHeld(gone); err != nil || held != nil {
		t.Fatalf("held %v %v", held, err)
	}
	if n, err := e.AudioRead(gone, 0, make([]byte, 10)); n != 0 || !errors.Is(err, io.EOF) {
		t.Fatalf("read %d %v", n, err)
	}
	if _, err := e.AudioOpen(gone); !errors.Is(err, ErrTranscriptionFinal) {
		t.Fatalf("open after the delete: %v", err)
	}
}

// Writes racing the recording's delete: whatever the order, the deleted recording's directory never comes back.
func TestAudioWritesRacingTheDelete(t *testing.T) {
	now := time.Date(2026, 11, 10, 14, 0, 0, 0, goldenZone)
	for round := range 5 {
		e, _ := hubEngine(t, nil)
		hubMeeting(t, e, recID, now)
		var wg sync.WaitGroup
		start := make(chan struct{})
		for k := range 4 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				w, err := e.AudioOpen(recID)
				for i := range 50 {
					at := float64(k*100+i) / 10
					if err == nil {
						err = w.Write(at, pcmAt(secs(at), 160))
					}
					_, _ = e.AudioPut(recID, at+0.05, pcmAt(secs(at+0.05), 160))
				}
				if w != nil {
					_ = w.Close()
				}
			}()
		}
		close(start)
		if _, err := e.DeleteRecording(recID, now); err != nil {
			t.Fatal(err)
		}
		wg.Wait()
		if _, err := os.Stat(e.Store.Path(KindRecording, recID)); !os.IsNotExist(err) {
			t.Fatalf("round %d: the audio brought a deleted recording back: %v", round, err)
		}
	}
}

// openChunk is the chunk file recording id's audio has open for writing (nil: none).
func openChunk(e *Engine, id string) *os.File {
	rec := e.Store.audioRec(id)
	rec.mu.Lock()
	defer rec.mu.Unlock()
	return rec.f
}

// A gap upload leaves no chunk file open, and neither does a delete, a prune or the trim of a recording whose socket
// left one open: an open file would keep the unlinked audio on disk (and its descriptor) past the delete.
func TestAudioLeavesNoChunkOpen(t *testing.T) {
	now := time.Date(2026, 11, 10, 14, 0, 0, 0, goldenZone)
	e, _ := hubEngine(t, nil)
	hubMeeting(t, e, recID, now)
	if _, err := e.AudioPut(recID, 0, pcmAt(0, secs(1))); err != nil {
		t.Fatal(err)
	}
	if f := openChunk(e, recID); f != nil {
		t.Fatalf("a gap upload left %s open", f.Name())
	}
	if _, err := e.DeleteRecording(recID, now); err != nil {
		t.Fatal(err)
	}
	if f := openChunk(e, recID); f != nil {
		t.Fatalf("the delete left %s open", f.Name())
	}

	// A socket's writer leaves its chunk open while it writes; the delete and the trim close it.
	const deleted, trimmed = "5b6c3f4a-1d2e-4f80-9a1b-2c3d4e5f6a7b", "6c7d4e5b-2e3f-4a91-8b2c-3d4e5f6a7b8c"
	for _, id := range []string{deleted, trimmed} {
		putRecording(t, e, id, hubUpload(1, now.Add(-time.Hour), true, nil, "一句"), now)
		w, err := e.AudioOpen(id)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = w.Close() })
		if err := w.Write(0, pcmAt(0, 100)); err != nil {
			t.Fatal(err)
		}
		if openChunk(e, id) == nil {
			t.Fatal("no chunk open while writing")
		}
	}
	if _, err := e.DeleteRecording(deleted, now); err != nil {
		t.Fatal(err)
	}
	putMeetingMinutes(t, e, trimmed, Minutes{Version: 1, Title: "Design review", When: "w"})
	if _, _, err := e.Store.trimRecording(recordingNow(t, e, trimmed), now); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{deleted, trimmed} {
		if f := openChunk(e, id); f != nil {
			t.Fatalf("%s: %s still open after its audio went", id, f.Name())
		}
	}

	// The prune of a recording past recording_days.
	const pruned = "7d8e5f6c-3f40-4b02-9c3d-4e5f6a7b8c9d"
	old := now.AddDate(0, 0, -20)
	putRecording(t, e, pruned, hubUpload(1, old, true, nil, "一句"), old)
	w, err := e.AudioOpen(pruned)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = w.Close() })
	if err := w.Write(0, pcmAt(0, 100)); err != nil {
		t.Fatal(err)
	}
	e.PruneRecordings(now)
	if _, ok := e.Store.Recording(pruned); ok {
		t.Fatal("not pruned")
	}
	if f := openChunk(e, pruned); f != nil {
		t.Fatalf("the prune left %s open", f.Name())
	}
}
