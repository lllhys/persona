package kb

import (
	"encoding/json"
	"fmt"
	"os"
	"slices"
	"time"
	"unicode/utf8"
)

// The hub's own transcript of a hub-mode meeting (docs/assistant/meeting-minutes.md "The transcript of record"):
// hub_segments.json beside meta.json, the sentences of the live pass or, once it ran, of the final pass, with a
// revision of their own. The upload still owns segments.json, complete and revision. Neither file is the transcript
// people read: that is the transcript of record (TranscriptOfRecord), the hub's sentences with the phone's segments
// where the hub has none, which transcript.md (rendered on every upload and every put of the hub's transcript),
// detail.segments, the minutes, the summary and the follow-up's seed take. hub_segments.json is never searched
// (transcript.md is the searched text), is in backups like segments.json, is not in an upload's undo journal
// (recordingFiles: a crash between it and transcript.md leaves transcript.md behind, which every retention pass mends,
// sweepTranscriptions), and goes with the trim.

const hubSegmentsFile = "hub_segments.json"

// HubTranscript is hub_segments.json: the hub's own sentences, with their own revision.
type HubTranscript struct {
	Rev      int64              `json:"rev"`
	Pass     string             `json:"pass"` // HubPassLive | HubPassFinal
	Segments []RecordingSegment `json:"segments"`
}

// readHubLocked is recording id's hub_segments.json (an empty one when there is none). The caller holds recFilesMu.
func (s *Store) readHubLocked(id string) (HubTranscript, error) {
	h := HubTranscript{Segments: []RecordingSegment{}}
	raw, err := os.ReadFile(s.path(KindRecording, id, hubSegmentsFile))
	if os.IsNotExist(err) {
		return h, nil
	}
	if err != nil {
		return h, err
	}
	if err := json.Unmarshal(raw, &h); err != nil {
		return HubTranscript{Segments: []RecordingSegment{}}, fmt.Errorf("hub transcript of %s: %w", id, err)
	}
	if h.Segments == nil {
		h.Segments = []RecordingSegment{}
	}
	return h, nil
}

// HubTranscript is recording rawID's hub transcript (rev 0 and no segments before the first put): ErrRecordingDeleted
// after a delete, ErrNotFound with no upload of it.
func (e *Engine) HubTranscript(rawID string) (HubTranscript, error) {
	id, ok := RecordingID(rawID)
	if !ok {
		return HubTranscript{}, ErrNotFound
	}
	st := e.Store
	if _, gone := st.tombstones()[id]; gone {
		return HubTranscript{}, ErrRecordingDeleted
	}
	if _, ok := st.Recording(id); !ok {
		return HubTranscript{}, ErrNotFound
	}
	st.recFilesMu.RLock()
	defer st.recFilesMu.RUnlock()
	return st.readHubLocked(id)
}

// cleanHubSegments are the hub's sentences as hub_segments.json keeps them: mended as the live log's are (cleanLive),
// empty ones left out, in time order, Source SourceHub, Persona's echo kept with its mark (Echo, echo.go); at most as
// many and as long as an upload's.
func cleanHubSegments(segs []RecordingSegment) []RecordingSegment {
	out := make([]RecordingSegment, 0, len(segs))
	total := 0
	for _, sg := range segs {
		l := cleanLive(LiveSegment{Start: sg.Start, End: sg.End, Text: sg.Text})
		if l.Text == "" {
			continue
		}
		if len(out) == maxRecordingSegments || total+len(l.Text) > maxRecordingText {
			break
		}
		total += len(l.Text)
		out = append(out, RecordingSegment{Start: l.Start, End: l.End, Text: l.Text, Source: SourceHub, Echo: sg.Echo})
	}
	slices.SortStableFunc(out, func(a, b RecordingSegment) int {
		switch {
		case a.Start < b.Start:
			return -1
		case a.Start > b.Start:
			return 1
		}
		return 0
	})
	return out
}

// HubTranscriptPut replaces recording rawID's hub transcript with segs (cleanHubSegments) from pass (HubPassLive or
// HubPassFinal), renders transcript.md again from the transcript of record, counts its characters in meta.json's
// chars (and plans the minutes again with them) and indexes the recording; rev is the transcript's revision after it.
// ErrNotFound when the recording was deleted or trimmed (also meanwhile), ErrReadOnly, ErrNotHub when it is not
// transcribed on the hub, ErrTranscriptionFinal for a live pass once a final one is stored.
func (e *Engine) HubTranscriptPut(rawID, pass string, segs []RecordingSegment) (rev int64, err error) {
	id, ok := RecordingID(rawID)
	if !ok {
		return 0, ErrNotFound
	}
	if pass != HubPassLive && pass != HubPassFinal {
		return 0, badRecording("a hub transcript's pass is live or final")
	}
	clean := cleanHubSegments(segs)
	st := e.Store
	st.recMu.Lock()
	defer st.recMu.Unlock()
	if st.ReadOnly() {
		return 0, ErrReadOnly
	}
	r, ok := st.Recording(id)
	if _, gone := st.tombstones()[id]; gone || !ok || r.Trimmed() {
		return 0, ErrNotFound
	}
	if !r.HubTranscribed() {
		return 0, ErrNotHub
	}
	h, record, err := st.putHubLocked(r, pass, clean)
	if err != nil {
		return 0, err
	}
	if chars := recordChars(record); chars != r.Chars {
		r.Chars = chars
		r.planMinutes()
		if err := st.putEntity(KindRecording, r.ID, r, &r.Tagging, false); err != nil {
			return h.Rev, err
		}
	}
	st.notify(KindRecording, id, "messages")
	e.index(KindRecording, id, r.Name, r.Tagging, r.OneLine, r.StartedAt)
	return h.Rev, nil
}

// putHubLocked writes recording r's hub_segments.json (segs, cleaned, from pass) and transcript.md from the transcript
// of record after it, not when the recording was deleted or trimmed meanwhile (ErrNotFound). The caller holds recMu.
func (s *Store) putHubLocked(r *Recording, pass string, segs []RecordingSegment) (HubTranscript, []RecordingSegment, error) {
	s.recFilesMu.Lock()
	defer s.recFilesMu.Unlock()
	if err := s.recoverRecordingLocked(r.ID); err != nil {
		return HubTranscript{}, nil, err
	}
	if !s.photosHereLocked(r.ID) || s.trimmedLocked(r.ID) {
		return HubTranscript{}, nil, ErrNotFound
	}
	h, err := s.readHubLocked(r.ID)
	if err != nil {
		// Unreadable: replaced, its revision counted on from the time (above any it can have had).
		h = HubTranscript{Rev: time.Now().Unix()}
	}
	if h.Pass == HubPassFinal && pass == HubPassLive {
		return HubTranscript{}, nil, ErrTranscriptionFinal // a late live flush never replaces the final pass's
	}
	h = HubTranscript{Rev: h.Rev + 1, Pass: pass, Segments: segs}
	raw, err := json.MarshalIndent(h, "", "  ")
	if err != nil {
		return HubTranscript{}, nil, err
	}
	if err := writeRecordingFile(s.path(KindRecording, r.ID, hubSegmentsFile), append(raw, '\n')); err != nil {
		return HubTranscript{}, nil, err
	}
	device, err := s.diskSegmentsLocked(r.ID)
	if err != nil {
		return h, nil, err
	}
	record := TranscriptOfRecord(true, device, segs)
	md := []byte(s.renderTranscript(r, record))
	return h, record, writeRecordingFile(s.path(KindRecording, r.ID, "transcript.md"), md)
}

// diskSegmentsLocked is recording id's segments.json as committed. The caller holds recFilesMu and recovered it.
func (s *Store) diskSegmentsLocked(id string) ([]RecordingSegment, error) {
	var segs []RecordingSegment
	raw, err := os.ReadFile(s.path(KindRecording, id, "segments.json"))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return segs, json.Unmarshal(raw, &segs)
}

// trimmedLocked: recording id's meta.json says it was trimmed. The caller holds recFilesMu.
func (s *Store) trimmedLocked(id string) bool {
	var trim struct {
		TrimmedAt *time.Time `json:"trimmed_at"`
	}
	raw, err := os.ReadFile(s.path(KindRecording, id, "meta.json"))
	return err == nil && json.Unmarshal(raw, &trim) == nil && trim.TrimmedAt != nil
}

// TranscriptOfRecord is a recording's text of record: in device mode the upload's segments; in hub mode the hub's
// sentences, and each upload segment the hub's sentences, taken together (their union), cover no more than half of,
// in time order (the hub's first at the same start), with Source set (SourceHub, SourceDevice). The phone's text is
// made on the device in the recording's language only, so in hub mode it fills only where the hub has none. Persona's
// echo stays in it, marked (Echo: the hub's by the echo rule, the phone's as uploaded); what is built from the meeting
// reads it through MeetingLines.
func TranscriptOfRecord(hubMode bool, device, hub []RecordingSegment) []RecordingSegment {
	if !hubMode {
		return slices.Clone(device)
	}
	out := make([]RecordingSegment, 0, len(hub)+len(device))
	spans := make([][2]float64, 0, len(hub))
	for _, sg := range hub {
		sg.Source = SourceHub
		out = append(out, sg)
		spans = append(spans, [2]float64{sg.Start, sg.End})
	}
	union := UnionSpans(spans)
	for _, sg := range device {
		if coveredShare(sg.Start, sg.End, union) <= 0.5 {
			sg.Source = SourceDevice
			out = append(out, sg)
		}
	}
	slices.SortStableFunc(out, func(a, b RecordingSegment) int {
		switch {
		case a.Start < b.Start:
			return -1
		case a.Start > b.Start:
			return 1
		}
		return 0
	})
	return out
}

// recordChars is how many characters segs have without Persona's echo, which is not the meeting's text (meta.json's
// chars: in hub mode the transcript of record's). A meeting of only Persona's echo has none, so it gets no minutes.
func recordChars(segs []RecordingSegment) int {
	n := 0
	for _, sg := range MeetingLines(segs) {
		n += utf8.RuneCountInString(sg.Text)
	}
	return n
}

// hubMode: m says the recording is transcribed on the hub.
func hubMode(m *Meeting) bool { return m != nil && m.Transcription.hub() }

// recordSnapshot is recording id's committed files (recordingSnapshot: an upload's undo journal while one is pending),
// its meeting.json (nil without one, or when it cannot be read: device mode) and its hub transcript, read together.
func (s *Store) recordSnapshot(id string) (map[string][]byte, *Meeting, HubTranscript, error) {
	if s.recordingReadWait != nil {
		s.recordingReadWait()
	}
	s.recFilesMu.RLock()
	defer s.recFilesMu.RUnlock()
	undo, err := s.readRecordingUndo(id)
	if err != nil {
		return nil, nil, HubTranscript{}, err
	}
	files := map[string][]byte(nil)
	if undo != nil {
		files = undo.Files
	} else if files, err = s.recordingDiskSnapshot(id); err != nil {
		return nil, nil, HubTranscript{}, err
	}
	m, err := s.readMeeting(id)
	if err != nil {
		m = nil
	}
	h, err := s.readHubLocked(id)
	if err != nil {
		h = HubTranscript{Segments: []RecordingSegment{}}
	}
	return files, m, h, nil
}

// recordOf is the transcript of record of a recordSnapshot.
func recordOf(files map[string][]byte, m *Meeting, h HubTranscript) []RecordingSegment {
	var segs []RecordingSegment
	_ = json.Unmarshal(files["segments.json"], &segs)
	return TranscriptOfRecord(hubMode(m), segs, h.Segments)
}

// recordLocked is the transcript of record of recording r (its Conv read) with device its upload's segments, for the
// commit (which holds recFilesMu).
func (s *Store) recordLocked(r *Recording, device []RecordingSegment) []RecordingSegment {
	if !r.HubTranscribed() {
		return device
	}
	h, _ := s.readHubLocked(r.ID)
	return TranscriptOfRecord(true, device, h.Segments)
}

// hubSegmentsOf is recording id's hub sentences (none when it has none or they cannot be read), for an upload.
func (s *Store) hubSegmentsOf(id string) []RecordingSegment {
	s.recFilesMu.RLock()
	defer s.recFilesMu.RUnlock()
	h, _ := s.readHubLocked(id)
	return h.Segments
}

// RecordOf is recording rawID's transcript of record (the final pass's reference frames): ErrRecordingDeleted after a
// delete, ErrNotFound with no upload of it.
func (e *Engine) RecordOf(rawID string) ([]RecordingSegment, error) {
	id, ok := RecordingID(rawID)
	if !ok {
		return nil, ErrNotFound
	}
	if _, gone := e.Store.tombstones()[id]; gone {
		return nil, ErrRecordingDeleted
	}
	files, m, h, err := e.Store.recordSnapshot(id)
	if err != nil {
		return nil, err
	}
	if files["meta.json"] == nil {
		return nil, ErrNotFound
	}
	return nonNilSegments(recordOf(files, m, h)), nil
}

// repairRecord mends hub-mode recording id after a crash between hub_segments.json and transcript.md (HubTranscriptPut)
// or meta.json: transcript.md and chars made what the transcript of record says, each only when it does not already.
// Whether anything changed. The caller holds recMu.
func (e *Engine) repairRecord(r *Recording) (bool, error) {
	st := e.Store
	if !r.HubTranscribed() || r.Trimmed() {
		return false, nil
	}
	st.recFilesMu.Lock()
	if err := st.recoverRecordingLocked(r.ID); err != nil {
		st.recFilesMu.Unlock()
		return false, err
	}
	if !st.photosHereLocked(r.ID) || st.trimmedLocked(r.ID) {
		st.recFilesMu.Unlock()
		return false, nil
	}
	device, err := st.diskSegmentsLocked(r.ID)
	if err != nil {
		st.recFilesMu.Unlock()
		return false, err
	}
	record := st.recordLocked(r, device)
	want := st.renderTranscript(r, record)
	path := st.path(KindRecording, r.ID, "transcript.md")
	changed := false
	if raw, err := os.ReadFile(path); err != nil || string(raw) != want {
		if err := writeRecordingFile(path, []byte(want)); err != nil {
			st.recFilesMu.Unlock()
			return false, err
		}
		changed = true
	}
	st.recFilesMu.Unlock()
	if chars := recordChars(record); chars != r.Chars {
		r.Chars = chars
		r.planMinutes()
		if err := st.putRecordingMeta(r); err != nil {
			return changed, err
		}
		changed = true
	}
	return changed, nil
}
