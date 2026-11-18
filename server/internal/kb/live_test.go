package kb

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"slices"
	"strings"
	"testing"
	"time"
)

// attendedUpload is the upload an attending phone sends when the meeting starts: a meeting, no text yet.
func attendedUpload(rev int64, start time.Time, role string) RecordingUpload {
	u := recUpload(rev, start, false)
	u.Kind, u.Attendance = RecordingMeeting, role
	return u
}

// The attendance fields never fail an upload: a bad value is stored as off and the result says why; off is "" on
// disk, the main chat nil; like kind, every upload replaces them.
func TestRecordingAttendanceFields(t *testing.T) {
	now := time.Date(2026, 11, 10, 14, 0, 0, 0, goldenZone)
	long := strings.Repeat("é", maxSessionIDRunes+1)
	for _, c := range []struct {
		name        string
		kind, role  string
		session     *SessionRef
		want, prob  string
		wantSession *SessionRef
	}{
		{"listener", RecordingMeeting, "listener", nil, AttendanceListener, "", nil},
		{"participant, a session", RecordingMeeting, " Participant ", &SessionRef{MachineID: "mac-1", SessionID: "s1"}, AttendanceParticipant, "", &SessionRef{MachineID: "mac-1", SessionID: "s1"}},
		{"no ids: the main chat", RecordingMeeting, "listener", &SessionRef{}, AttendanceListener, "", nil},
		{"off", RecordingMeeting, "off", &SessionRef{MachineID: "mac-1", SessionID: "s1"}, AttendanceOff, "", nil},
		{"none", RecordingMeeting, "", nil, AttendanceOff, "", nil},
		{"unknown role", RecordingMeeting, "speaker", nil, AttendanceOff, AttendanceUnknownRole, nil},
		{"notes: a room's only (#8851)", RecordingMeeting, "notes", nil, AttendanceOff, AttendanceUnknownRole, nil},
		{"a note", RecordingNote, "listener", nil, AttendanceOff, AttendanceNotMeeting, nil},
		{"kind omitted", "", "participant", nil, AttendanceOff, AttendanceNotMeeting, nil},
		{"one id", RecordingMeeting, "listener", &SessionRef{MachineID: "mac-1"}, AttendanceOff, AttendanceBadSession, nil},
		{"the other id", RecordingMeeting, "listener", &SessionRef{SessionID: "s1"}, AttendanceOff, AttendanceBadSession, nil},
		{"an id too long", RecordingMeeting, "listener", &SessionRef{MachineID: "mac-1", SessionID: long}, AttendanceOff, AttendanceBadSession, nil},
		{"an id of 200", RecordingMeeting, "listener", &SessionRef{MachineID: "mac-1", SessionID: long[2:]}, AttendanceListener, "", &SessionRef{MachineID: "mac-1", SessionID: long[2:]}},
	} {
		t.Run(c.name, func(t *testing.T) {
			e := goldenEngine(t, nil)
			u := recUpload(1, now, false, "Hello everyone")
			u.Kind, u.Attendance, u.AttendingSession = c.kind, c.role, c.session
			put, err := e.PutRecording(recID, u, now)
			if err != nil || put.Status != RecordingStored || put.Attendance != c.want || put.AttendanceProblem != c.prob {
				t.Fatalf("put %+v %v, want %s / %q", put, err, c.want, c.prob)
			}
			r := recordingNow(t, e, recID)
			if r.AttendanceRole() != c.want || (r.Attendance == AttendanceOff) {
				t.Errorf("stored %q", r.Attendance)
			}
			if (r.AttendingSession == nil) != (c.wantSession == nil) || r.AttendingSession != nil && *r.AttendingSession != *c.wantSession {
				t.Errorf("session %+v, want %+v", r.AttendingSession, c.wantSession)
			}
			if segs := e.Store.RecordingSegments(recID); len(segs) != 1 {
				t.Errorf("the transcript was not stored: %+v", segs)
			}
			raw, _ := os.ReadFile(e.Store.Path(KindRecording, recID, "meta.json"))
			if c.want == AttendanceOff && strings.Contains(string(raw), `"attendance"`) {
				t.Errorf("off is written: %s", raw)
			}
			d, err := e.Get(KindRecording, recID, 0)
			if err != nil {
				t.Fatal(err)
			}
			var meta struct {
				Attendance       string      `json:"attendance"`
				AttendingSession *SessionRef `json:"attending_session"`
			}
			_ = json.Unmarshal(d.Meta, &meta)
			if meta.Attendance != r.Attendance || (meta.AttendingSession == nil) != (c.wantSession == nil) {
				t.Errorf("detail.meta %s", d.Meta)
			}
		})
	}
}

// Wrong JSON types for the two fields are a problem with them, not with the upload.
func TestRecordingAttendanceJSON(t *testing.T) {
	now := time.Date(2026, 11, 10, 14, 0, 0, 0, goldenZone)
	for _, c := range []struct {
		fields, want, prob string
		session            *SessionRef
	}{
		{`"attendance": "listener", "attending_session": null`, AttendanceListener, "", nil},
		{`"attendance": "listener", "attending_session": {"machine_id": "mac-1", "session_id": "s1", "label": "x"}`, AttendanceListener, "", &SessionRef{MachineID: "mac-1", SessionID: "s1"}},
		{`"attendance": "listener", "attending_session": {"machine_id": "", "session_id": null}`, AttendanceListener, "", nil},
		{`"attendance": 5`, AttendanceOff, AttendanceUnknownRole, nil},
		{`"attendance": {"role": "listener"}`, AttendanceOff, AttendanceUnknownRole, nil},
		{`"attendance": "listener", "attending_session": "s1"`, AttendanceOff, AttendanceBadSession, nil},
		{`"attendance": "listener", "attending_session": {"machine_id": 7, "session_id": "s1"}`, AttendanceOff, AttendanceBadSession, nil},
		{`"attendance": "off", "attending_session": "s1"`, AttendanceOff, "", nil},
	} {
		body := `{"revision": 1, "started_at": "2026-11-10T14:00:00+08:00", "device": "phone", "kind": "meeting",
			"segments": [{"start": 0, "end": 2, "text": "Hello"}], ` + c.fields + `}`
		var u RecordingUpload
		if err := json.Unmarshal([]byte(body), &u); err != nil {
			t.Fatalf("%s: %v", c.fields, err)
		}
		if u.Revision != 1 || u.Kind != RecordingMeeting || len(u.Segments) != 1 {
			t.Fatalf("%s: the rest was not read: %+v", c.fields, u)
		}
		e := goldenEngine(t, nil)
		put, err := e.PutRecording(recID, u, now)
		if err != nil || put.Attendance != c.want || put.AttendanceProblem != c.prob {
			t.Errorf("%s: %+v %v", c.fields, put, err)
			continue
		}
		r := recordingNow(t, e, recID)
		if (r.AttendingSession == nil) != (c.session == nil) || r.AttendingSession != nil && *r.AttendingSession != *c.session {
			t.Errorf("%s: session %+v", c.fields, r.AttendingSession)
		}
	}
	var u RecordingUpload
	if err := json.Unmarshal([]byte(`{"revision": "one"}`), &u); err == nil {
		t.Error("a bad revision decoded")
	}
}

// Every upload replaces the fields; a stale one leaves the stored role in effect; LiveChanged hears of uploads of
// recordings that attend or attended until then.
func TestRecordingAttendanceReplaced(t *testing.T) {
	e := goldenEngine(t, nil)
	var changed []string
	e.LiveChanged = func(id string) { changed = append(changed, id) }
	now := time.Date(2026, 11, 10, 14, 0, 0, 0, goldenZone)
	note := "11111111-1111-4111-8111-111111111111"
	putRecording(t, e, note, recUpload(1, now, false, "a note"), now)
	if len(changed) != 0 {
		t.Fatalf("a plain note told the feeder: %v", changed)
	}
	u := attendedUpload(1, now, AttendanceParticipant)
	u.AttendingSession = &SessionRef{MachineID: "mac-1", SessionID: "s1"}
	putRecording(t, e, recID, u, now)
	moved := attendedUpload(2, now, AttendanceParticipant)
	moved.AttendingSession = &SessionRef{MachineID: "mac-2", SessionID: "s2"}
	putRecording(t, e, recID, moved, now)
	if r := recordingNow(t, e, recID); r.AttendingSession == nil || r.AttendingSession.MachineID != "mac-2" {
		t.Fatalf("not moved: %+v", r.AttendingSession)
	}
	stale := attendedUpload(1, now, AttendanceListener)
	if put, err := e.PutRecording(recID, stale, now); err != nil || put.Status != RecordingStale || put.Attendance != AttendanceParticipant {
		t.Fatalf("stale: %+v %v", put, err)
	}
	off := attendedUpload(3, now, "")
	putRecording(t, e, recID, off, now)
	if r := recordingNow(t, e, recID); r.Attendance != "" || r.AttendingSession != nil {
		t.Fatalf("an upload without attendance kept it: %q %+v", r.Attendance, r.AttendingSession)
	}
	putRecording(t, e, recID, attendedUpload(4, now, ""), now)
	if strings.Join(changed, ",") != strings.Join([]string{recID, recID, recID}, ",") {
		t.Fatalf("changed %v: the first, the move and the one turning it off", changed)
	}
	old := attendedUpload(1, now.AddDate(0, 0, -30), AttendanceListener)
	if put, err := e.PutRecording("22222222-2222-4222-8222-222222222222", old, now); err != nil || put.Status != RecordingExpired || put.Attendance != AttendanceOff {
		t.Fatalf("expired: %+v %v", put, err)
	}
}

// A meeting that completes with no text gets no minutes: minutes_state absent, nothing for the model, and asking for
// minutes is refused as for an incomplete transcript.
func TestMeetingWithoutTextGetsNoMinutes(t *testing.T) {
	m := &funcModel{f: func(int, string, string) (string, error) { return minutesReply, nil }}
	e := goldenEngine(t, m)
	now := time.Date(2026, 11, 10, 15, 0, 0, 0, goldenZone)
	start := now.Add(-time.Hour)
	putRecording(t, e, recID, attendedUpload(1, start, AttendanceListener), now)
	if r := recordingNow(t, e, recID); r.MinutesState != MinutesPending {
		t.Fatalf("while recording: %q, want pending", r.MinutesState)
	}
	done := attendedUpload(2, start, AttendanceListener)
	done.Complete = true
	putRecording(t, e, recID, done, now)
	if r := recordingNow(t, e, recID); r.MinutesState != "" {
		t.Fatalf("complete with no text: %q, want none", r.MinutesState)
	}
	if states, _ := e.RecordingStates(); len(states) != 1 || states[0].MinutesState != "" {
		t.Fatalf("states %+v", states)
	}
	if n := e.WriteMinutes(context.Background(), 3); n != 0 || len(m.calls) != 0 {
		t.Fatalf("wrote %d with %d calls", n, len(m.calls))
	}
	if err := e.AskMinutes(recID); !errors.Is(err, ErrRecordingIncomplete) {
		t.Fatalf("asked: %v", err)
	}
	if r := recordingNow(t, e, recID); r.MinutesState != "" || r.MinutesAsked {
		t.Fatalf("after asking: %q asked %v", r.MinutesState, r.MinutesAsked)
	}
	// Text in a later revision: minutes again.
	putRecording(t, e, recID, meetingUpload(3, start, "Hello everyone"), now)
	if r := recordingNow(t, e, recID); r.MinutesState != MinutesPending {
		t.Fatalf("with text: %q", r.MinutesState)
	}
}

func TestLiveAppend(t *testing.T) {
	e := goldenEngine(t, nil)
	var changed int
	e.LiveChanged = func(string) { changed++ }
	now := time.Date(2026, 11, 10, 14, 0, 0, 0, goldenZone)
	segs := func(texts ...string) []LiveSegment {
		var out []LiveSegment
		for i, t := range texts {
			out = append(out, LiveSegment{Start: float64(i * 3), End: float64(i*3 + 2), Text: t})
		}
		return out
	}
	if _, _, err := e.LiveAppend(recID, 0, segs("early")); !errors.Is(err, ErrNotFound) {
		t.Fatalf("before any upload: %v", err)
	}
	if _, _, err := e.LiveAppend("not-a-uuid", 0, nil); !errors.Is(err, ErrNotFound) {
		t.Fatalf("not a UUID: %v", err)
	}
	putRecording(t, e, recID, meetingUpload(1, now, "said before"), now)
	changed = 0
	if _, _, err := e.LiveAppend(recID, 0, segs("off")); !errors.Is(err, ErrNotAttending) {
		t.Fatalf("attendance off: %v", err)
	}
	putRecording(t, e, recID, attendedUpload(2, now, AttendanceListener), now)
	changed = 0
	l, r, err := e.LiveAppend(strings.ToUpper(recID), 0, segs("Let's start", "Ada has the numbers"))
	if err != nil || len(l.Segments) != 2 || r.Attendance != AttendanceListener || changed != 1 {
		t.Fatalf("append: %+v %v changed %d", l, err, changed)
	}
	// A replay with one more: only the new one is stored.
	l, _, err = e.LiveAppend(recID, 0, segs("Let's start", "Ada has the numbers", "Bo agrees"))
	if err != nil || len(l.Segments) != 3 || l.Segments[2].Text != "Bo agrees" || l.Bytes != len("Let's start")+len("Ada has the numbers")+len("Bo agrees") {
		t.Fatalf("replay: %+v %v", l, err)
	}
	// Nothing new (an empty request, a pure replay): no write, nothing to tell.
	changed = 0
	for _, c := range []struct {
		from int
		segs []LiveSegment
	}{{3, nil}, {1, segs("Ada has the numbers")}} {
		if l, _, err := e.LiveAppend(recID, c.from, c.segs); err != nil || len(l.Segments) != 3 {
			t.Fatalf("from %d: %+v %v", c.from, l, err)
		}
	}
	if changed != 0 {
		t.Fatalf("told %d times of nothing", changed)
	}
	// A gap: the error says where to send from.
	_, _, err = e.LiveAppend(recID, 5, segs("later"))
	var gap LiveGapError
	var gapPtr *LiveGapError
	if !errors.Is(err, ErrLiveGap) || !errors.As(err, &gap) || gap.Next != 3 || !errors.As(err, &gapPtr) || gapPtr.Next != 3 {
		t.Fatalf("gap: %v", err)
	}
	for _, bad := range []struct {
		from int
		segs []LiveSegment
	}{{-1, nil}, {3, make([]LiveSegment, MaxLiveRequest+1)}} {
		if _, _, err := e.LiveAppend(recID, bad.from, bad.segs); !errors.Is(err, ErrBadLive) {
			t.Errorf("from %d, %d sentences: %v", bad.from, len(bad.segs), err)
		}
	}
	// Bad sentences are mended, never refused.
	long := strings.Repeat("中", 1334) // 4002 bytes
	l, _, err = e.LiveAppend(recID, 3, []LiveSegment{{Start: -4, End: -9, Text: long}, {Start: 10, End: 8, Text: "  "}, {Start: 12, End: 13, Text: "two\nlines"}})
	if err != nil || len(l.Segments) != 6 {
		t.Fatalf("mended: %+v %v", l.Segments, err)
	}
	if s := l.Segments[3]; s.Start != 0 || s.End != 0 || len(s.Text) != 3999 || !strings.HasPrefix(long, s.Text) {
		t.Errorf("a long one: start %v end %v %d bytes", s.Start, s.End, len(s.Text))
	}
	if s := l.Segments[4]; s.Text != "" || s.End != 10 {
		t.Errorf("an empty one holds its position: %+v", s)
	}
	if s := l.Segments[5]; s.Text != "two lines" {
		t.Errorf("one line: %q", s.Text)
	}
	if got, _, err := e.LiveRead(recID); err != nil || len(got.Segments) != 6 || got.Bytes != l.Bytes {
		t.Fatalf("read back: %+v %v", got, err)
	}
	// Turned off mid-meeting: the lines stay, nothing more is taken.
	putRecording(t, e, recID, attendedUpload(3, now, ""), now)
	if _, _, err := e.LiveAppend(recID, 6, segs("after")); !errors.Is(err, ErrNotAttending) {
		t.Fatalf("after off: %v", err)
	}
	if got, r, err := e.LiveRead(recID); err != nil || len(got.Segments) != 6 || r.AttendanceRole() != AttendanceOff {
		t.Fatalf("after off: %+v %v", got, err)
	}
	// Deleted: told apart from never uploaded, before anything else.
	if _, err := e.DeleteRecording(recID, now); err != nil {
		t.Fatal(err)
	}
	if _, _, err := e.LiveAppend(recID, -1, nil); !errors.Is(err, ErrRecordingDeleted) {
		t.Fatalf("deleted: %v", err)
	}
	if _, _, err := e.LiveRead(recID); !errors.Is(err, ErrRecordingDeleted) {
		t.Fatalf("read deleted: %v", err)
	}
	if _, err := os.Stat(e.Store.Path(KindRecording, recID)); !os.IsNotExist(err) {
		t.Fatalf("a delete left the live log: %v", err)
	}
}

// The limits: more than maxLivePositions positions or maxLiveBytes of text refuse the whole request and mark the log
// full; what still fits is taken.
func TestLiveFull(t *testing.T) {
	e := goldenEngine(t, nil)
	now := time.Date(2026, 11, 10, 14, 0, 0, 0, goldenZone)
	putRecording(t, e, recID, attendedUpload(1, now, AttendanceParticipant), now)
	seed := Live{Segments: make([]LiveSegment, maxLivePositions-1)}
	if ok, err := e.Store.putLive(recID, &seed); !ok || err != nil {
		t.Fatal(ok, err)
	}
	two := []LiveSegment{{Text: "one"}, {Text: "two"}}
	l, _, err := e.LiveAppend(recID, maxLivePositions-1, two)
	if !errors.Is(err, ErrLiveFull) || len(l.Segments) != maxLivePositions-1 || !l.Full {
		t.Fatalf("over the positions: %d %v full %v", len(l.Segments), err, l.Full)
	}
	if got, _, _ := e.LiveRead(recID); !got.Full || len(got.Segments) != maxLivePositions-1 {
		t.Fatalf("not marked full: %v %d", got.Full, len(got.Segments))
	}
	if l, _, err := e.LiveAppend(recID, maxLivePositions-1, two[:1]); err != nil || len(l.Segments) != maxLivePositions {
		t.Fatalf("the last position: %d %v", len(l.Segments), err)
	}
	if _, _, err := e.LiveAppend(recID, maxLivePositions, nil); err != nil {
		t.Fatalf("asking for next when full: %v", err)
	}
	other := "11111111-1111-4111-8111-111111111111"
	putRecording(t, e, other, attendedUpload(1, now, AttendanceListener), now)
	bytes := Live{Segments: []LiveSegment{{Text: "x"}}, Bytes: maxLiveBytes - 3}
	if ok, err := e.Store.putLive(other, &bytes); !ok || err != nil {
		t.Fatal(ok, err)
	}
	if _, _, err := e.LiveAppend(other, 1, []LiveSegment{{Text: "four"}}); !errors.Is(err, ErrLiveFull) {
		t.Fatalf("over the bytes: %v", err)
	}
	if l, _, err := e.LiveAppend(other, 1, []LiveSegment{{Text: "abc"}}); err != nil || l.Bytes != maxLiveBytes {
		t.Fatalf("up to the bytes: %d %v", l.Bytes, err)
	}
}

// A read-only knowledge base reads the live log and stores nothing; read-only comes after deleted and not found.
func TestLiveReadOnly(t *testing.T) {
	e := goldenEngine(t, nil)
	now := time.Date(2026, 11, 10, 14, 0, 0, 0, goldenZone)
	putRecording(t, e, recID, attendedUpload(1, now, AttendanceListener), now)
	if _, _, err := e.LiveAppend(recID, 0, []LiveSegment{{Text: "Hello"}}); err != nil {
		t.Fatal(err)
	}
	ro, err := OpenReadOnly(e.Store.Dir)
	if err != nil {
		t.Fatal(err)
	}
	re := &Engine{Store: ro}
	if l, r, err := re.LiveRead(recID); err != nil || len(l.Segments) != 1 || r.Attendance != AttendanceListener {
		t.Fatalf("read: %+v %v", l, err)
	}
	if _, _, err := re.LiveAppend(recID, 1, []LiveSegment{{Text: "more"}}); !errors.Is(err, ErrReadOnly) {
		t.Fatalf("append: %v", err)
	}
	if _, _, err := re.LiveAppend("11111111-1111-4111-8111-111111111111", 0, nil); !errors.Is(err, ErrNotFound) {
		t.Fatalf("append to none: %v", err)
	}
	if _, err := re.LiveUpdate(recID, func(l *Live, _ Recording) error { l.Fed = 1; return nil }); !errors.Is(err, ErrReadOnly) {
		t.Fatalf("update: %v", err)
	}
}

func TestLiveUpdate(t *testing.T) {
	e := goldenEngine(t, nil)
	now := time.Date(2026, 11, 10, 14, 0, 0, 0, goldenZone)
	if _, err := e.LiveUpdate(recID, func(*Live, Recording) error { return nil }); !errors.Is(err, ErrNotFound) {
		t.Fatalf("no recording: %v", err)
	}
	putRecording(t, e, recID, attendedUpload(1, now, AttendanceParticipant), now)
	path := e.Store.Path(KindRecording, recID, liveFile)
	// Nothing changed: nothing written.
	if _, err := e.LiveUpdate(recID, func(_ *Live, r Recording) error {
		if r.Attendance != AttendanceParticipant {
			t.Errorf("given %q", r.Attendance)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("an unchanged log was written: %v", err)
	}
	boom := errors.New("boom")
	if _, err := e.LiveUpdate(recID, func(l *Live, _ Recording) error { l.Fed = 9; return boom }); !errors.Is(err, boom) {
		t.Fatalf("fn failed: %v", err)
	}
	if l, _, _ := e.LiveRead(recID); l.Fed != 0 {
		t.Fatalf("a failed fn was written: %+v", l)
	}
	at := 12.5
	l, err := e.LiveUpdate(recID, func(l *Live, _ Recording) error {
		l.Asks = append(l.Asks, LiveAsk{ID: "6f1c0a58-0b0e-4c1e-9d55-2f0f6f0d9a11", Question: "What did we decide?", QuestionSource: "typed",
			At: &at, State: "pending", AskedAt: now})
		l.FedTo, l.Fed, l.Problem = &SessionRef{MachineID: "mac-1", SessionID: "s1"}, 0, "attendee_unavailable"
		return nil
	})
	if err != nil || len(l.Asks) != 1 {
		t.Fatalf("update: %+v %v", l, err)
	}
	got, _, err := e.LiveRead(recID)
	if err != nil || len(got.Asks) != 1 || *got.Asks[0].At != 12.5 || got.Problem != "attendee_unavailable" || got.FedTo.SessionID != "s1" {
		t.Fatalf("read back %+v %v", got, err)
	}
	if _, err := e.DeleteRecording(recID, now); err != nil {
		t.Fatal(err)
	}
	if _, err := e.LiveUpdate(recID, func(l *Live, _ Recording) error { l.Fed = 1; return nil }); !errors.Is(err, ErrNotFound) {
		t.Fatalf("after a delete: %v", err)
	}
	if _, err := os.Stat(e.Store.Path(KindRecording, recID)); !os.IsNotExist(err) {
		t.Fatalf("an update brought a deleted recording back: %v", err)
	}
}

// When the hub first saw the end is kept; only recordings that attend or have a live log get a live log for it.
func TestLiveEndedAt(t *testing.T) {
	e := goldenEngine(t, nil)
	start := time.Date(2026, 11, 10, 14, 0, 0, 0, goldenZone)
	end := start.Add(time.Hour)
	u := attendedUpload(1, start, AttendanceListener)
	putRecording(t, e, recID, u, start)
	if l, _, _ := e.LiveRead(recID); !l.EndedAt.IsZero() {
		t.Fatalf("ended before the end: %v", l.EndedAt)
	}
	u.Revision, u.EndedAt = 2, &end
	putRecording(t, e, recID, u, end.Add(time.Minute))
	u.Revision, u.Complete = 3, true
	putRecording(t, e, recID, u, end.Add(5*time.Minute))
	if l, _, _ := e.LiveRead(recID); !l.EndedAt.Equal(end.Add(time.Minute)) {
		t.Fatalf("ended at %v, want the first upload carrying ended_at", l.EndedAt)
	}
	note := recUpload(1, start, true, "a note")
	note.EndedAt = &end
	other := "11111111-1111-4111-8111-111111111111"
	putRecording(t, e, other, note, end)
	if _, err := os.Stat(e.Store.Path(KindRecording, other, liveFile)); !os.IsNotExist(err) {
		t.Fatalf("a plain recording got a live log: %v", err)
	}
}

func TestAttendingRecordings(t *testing.T) {
	e := goldenEngine(t, nil)
	now := time.Date(2026, 11, 10, 14, 0, 0, 0, goldenZone)
	a, b, c, d := "11111111-1111-4111-8111-111111111111", "22222222-2222-4222-8222-222222222222", "33333333-3333-4333-8333-333333333333", "44444444-4444-4444-8444-444444444444"
	putRecording(t, e, b, attendedUpload(1, now, AttendanceListener), now)
	putRecording(t, e, a, attendedUpload(1, now.Add(-time.Hour), AttendanceParticipant), now)
	putRecording(t, e, c, attendedUpload(1, now, AttendanceListener), now)
	putRecording(t, e, d, meetingUpload(1, now, "not attended"), now)
	if _, err := e.LiveUpdate(c, func(l *Live, _ Recording) error { l.EndFed = true; return nil }); err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, r := range e.AttendingRecordings() {
		ids = append(ids, r.ID)
	}
	if strings.Join(ids, ",") != a+","+b {
		t.Fatalf("attending %v", ids)
	}
}

// The live log is never searched (kb_search, the app's search, an entry's matches) nor shown by Get; later uploads
// leave it alone, and the retention takes it with the recording.
func TestLiveNeverSearchedOrShown(t *testing.T) {
	e := goldenEngine(t, nil)
	days := 14
	e.RecordingDaysFunc = func() int { return days }
	now := time.Date(2026, 11, 10, 14, 0, 0, 0, goldenZone)
	u := attendedUpload(1, now.AddDate(0, 0, -3), AttendanceParticipant)
	u.Segments = []RecordingSegment{{Start: 0, End: 2, Text: "uploaded orchard line"}}
	putRecording(t, e, recID, u, now)
	if _, _, err := e.LiveAppend(recID, 0, []LiveSegment{{Start: 1, End: 2, Text: "zephyrlive budget line"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := e.LiveUpdate(recID, func(l *Live, _ Recording) error {
		l.Asks = append(l.Asks, LiveAsk{ID: "6f1c0a58-0b0e-4c1e-9d55-2f0f6f0d9a11", Question: "quasarask question", QuestionSource: "typed",
			State: "answered", Answer: "nebulaanswer reply", Sources: []AskSource{{Kind: KindRecording, ID: recID, Name: "cometsource"}}, AskedAt: now})
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	// Uploads replace the transcript; the live log stays as it was.
	u.Revision = 2
	putRecording(t, e, recID, u, now)
	if l, _, _ := e.LiveRead(recID); len(l.Segments) != 1 || len(l.Asks) != 1 {
		t.Fatalf("an upload changed the live log: %+v", l)
	}
	for _, rel := range []string{"recordings/" + recID + "/live.json", "recordings/" + recID + "/live.json.tmp"} {
		if searchable(rel) {
			t.Errorf("%s is searchable", rel)
		}
	}
	if res := e.Find("orchard", 10); len(res) != 1 {
		t.Fatalf("the transcript is not found: %+v", res)
	}
	for _, word := range []string{"zephyrlive", "quasarask", "nebulaanswer", "cometsource"} {
		if hits := e.Search(word, true, 20); len(hits) != 0 {
			t.Errorf("Search %s: %+v", word, hits)
		}
		if res := e.Find(word, 20); len(res) != 0 {
			t.Errorf("Find %s: %+v", word, res)
		}
		if m := e.Matches(KindRecording, recID, word, 20); len(m) != 0 {
			t.Errorf("Matches %s: %+v", word, m)
		}
		d, err := e.Get(KindRecording, recID, 100000)
		if err != nil {
			t.Fatal(err)
		}
		raw, _ := json.Marshal(d)
		if strings.Contains(string(raw), word) {
			t.Errorf("Get shows %s: %s", word, raw)
		}
	}
	days = 1
	if n := e.PruneRecordings(now); n != 1 {
		t.Fatalf("pruned %d", n)
	}
	if _, err := os.Stat(e.Store.Path(KindRecording, recID)); !os.IsNotExist(err) {
		t.Fatalf("the retention left the live log: %v", err)
	}
	if _, _, err := e.LiveRead(recID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("read after the retention: %v", err)
	}
}

// Appends from several phones' retries, uploads noting the end and a delete at once: the positions stay contiguous
// with nothing stored twice, and the delete stays a delete.
func TestLiveConcurrent(t *testing.T) {
	e := goldenEngine(t, nil)
	now := time.Date(2026, 11, 10, 14, 0, 0, 0, goldenZone)
	putRecording(t, e, recID, attendedUpload(1, now, AttendanceListener), now)
	const total = 60
	done := make(chan error, 4)
	for w := range 3 {
		go func() {
			for {
				l, _, err := e.LiveRead(recID)
				if err != nil {
					done <- err
					return
				}
				from := len(l.Segments)
				if from >= total {
					done <- nil
					return
				}
				batch := []LiveSegment{{Start: float64(from), Text: fmt.Sprintf("line %d", from)}, {Start: float64(from + 1), Text: fmt.Sprintf("line %d", from+1)}}
				if _, _, err := e.LiveAppend(recID, from, batch[:1+w%2]); err != nil && !errors.Is(err, ErrLiveGap) {
					done <- err
					return
				}
			}
		}()
	}
	go func() {
		end := now.Add(time.Hour)
		for rev := int64(2); rev < 12; rev++ {
			u := attendedUpload(rev, now, AttendanceListener)
			u.EndedAt = &end
			if _, err := e.PutRecording(recID, u, end); err != nil {
				done <- err
				return
			}
		}
		done <- nil
	}()
	for range 4 {
		if err := <-done; err != nil {
			t.Fatal(err)
		}
	}
	l, _, err := e.LiveRead(recID)
	if err != nil {
		t.Fatal(err)
	}
	for i, sg := range l.Segments {
		if sg.Text != fmt.Sprintf("line %d", i) {
			t.Fatalf("position %d holds %q", i, sg.Text)
		}
	}
	if len(l.Segments) < total || l.EndedAt.IsZero() {
		t.Fatalf("%d positions, ended %v", len(l.Segments), l.EndedAt)
	}
	stop := make(chan struct{})
	go func() {
		defer close(stop)
		for range 20 {
			_, _ = e.LiveUpdate(recID, func(l *Live, _ Recording) error { l.Fed++; return nil })
			_, _, _ = e.LiveAppend(recID, len(l.Segments), []LiveSegment{{Text: "late"}})
		}
	}()
	if _, err := e.DeleteRecording(recID, now); err != nil {
		t.Fatal(err)
	}
	<-stop
	if _, err := os.Stat(e.Store.Path(KindRecording, recID)); !os.IsNotExist(err) {
		t.Fatalf("the live log brought a deleted recording back: %v", err)
	}
}

// The topics (#3862 phase 2): CleanTopics takes a list within the limits (titles and notes on one line, states in
// lower case) and refuses one over a limit saying which; TopicsSet replaces them, moves their revision and topics_at
// only when they change, and MeetingChanged hears of PartTopics, as it hears of PartLines on every stored append.
func TestTopicsSet(t *testing.T) {
	at := 61.0
	clean, err := CleanTopics([]LiveTopic{{Title: " Export\n cap ", State: " Done ", At: &at, Note: "kept\tat 4 MB"},
		{Title: strings.Repeat("字", MaxTopicTitle), State: TopicCurrent, Note: strings.Repeat("a", MaxTopicNote)}})
	if err != nil || clean[0].Title != "Export cap" || clean[0].State != TopicDone || clean[0].Note != "kept at 4 MB" || *clean[0].At != 61 {
		t.Fatalf("clean %+v %v", clean, err)
	}
	if none, err := CleanTopics([]LiveTopic{}); err != nil || none != nil {
		t.Fatalf("none %+v %v", none, err)
	}
	twenty := make([]LiveTopic, MaxTopics)
	for i := range twenty {
		twenty[i] = LiveTopic{Title: fmt.Sprint(i), State: TopicOpen}
	}
	if _, err := CleanTopics(twenty); err != nil {
		t.Fatalf("20 topics: %v", err)
	}
	neg, nan := -1.0, math.NaN()
	for name, c := range map[string]struct {
		topics []LiveTopic
		want   string
	}{
		"21":          {append(slices.Clone(twenty), LiveTopic{Title: "x", State: TopicOpen}), "21 topics, over the 20"},
		"title":       {[]LiveTopic{{Title: strings.Repeat("字", MaxTopicTitle+1), State: TopicOpen}}, "topic 1's title is 81 characters, over the 80"},
		"note":        {[]LiveTopic{{Title: "a", State: TopicOpen}, {Title: "b", State: TopicOpen, Note: strings.Repeat("a", MaxTopicNote+1)}}, "topic 2's note is 201 characters"},
		"empty title": {[]LiveTopic{{Title: " \n ", State: TopicOpen}}, "topic 1 has no title"},
		"state":       {[]LiveTopic{{Title: "a", State: "next"}}, `state is "next"`},
		"two current": {[]LiveTopic{{Title: "a", State: TopicCurrent}, {Title: "b", State: "Current"}}, "2 topics are current"},
		"negative at": {[]LiveTopic{{Title: "a", State: TopicOpen, At: &neg}}, "topic 1's at"},
		"NaN at":      {[]LiveTopic{{Title: "a", State: TopicOpen, At: &nan}}, "topic 1's at"},
	} {
		if _, err := CleanTopics(c.topics); !errors.Is(err, ErrBadTopics) || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: %v", name, err)
		}
	}

	e := goldenEngine(t, nil)
	var heard [][]string
	e.MeetingChanged = func(_ string, parts []string) { heard = append(heard, parts) }
	now := time.Date(2026, 11, 10, 14, 0, 0, 0, goldenZone)
	if _, err := e.TopicsSet(recID, clean, now); !errors.Is(err, ErrNotFound) {
		t.Fatalf("no recording: %v", err)
	}
	putRecording(t, e, recID, attendedUpload(1, now, AttendanceParticipant), now)
	if _, _, err := e.LiveAppend(recID, 0, []LiveSegment{{Start: 1, End: 2, Text: "a line"}}); err != nil {
		t.Fatal(err)
	}
	if changed, err := e.TopicsSet(recID, clean, now); err != nil || !changed {
		t.Fatalf("set: %v %v", changed, err)
	}
	if changed, err := e.TopicsSet(recID, clean, now.Add(time.Minute)); err != nil || changed {
		t.Fatalf("the same again: %v %v", changed, err)
	}
	if _, err := e.TopicsSet(recID, append(clean, clean[1]), now); !errors.Is(err, ErrBadTopics) {
		t.Fatalf("over a limit: %v", err)
	}
	l, _, _ := e.LiveRead(recID)
	if len(l.Topics) != 2 || l.TopicsRev != 1 || !l.TopicsAt.Equal(now) {
		t.Fatalf("after the same list again %+v", l)
	}
	if changed, err := e.TopicsSet(recID, nil, now.Add(time.Hour)); err != nil || !changed {
		t.Fatalf("cleared: %v %v", changed, err)
	}
	if l, _, _ = e.LiveRead(recID); l.Topics != nil || l.TopicsRev != 2 || !l.TopicsAt.Equal(now.Add(time.Hour)) {
		t.Fatalf("cleared %+v", l)
	}
	if want := [][]string{{PartLines}, {PartTopics}, {PartTopics}}; fmt.Sprint(heard) != fmt.Sprint(want) {
		t.Fatalf("heard %v, want %v", heard, want)
	}
}

// meeting_highlights_set (docs/assistant/meeting-minutes.md "Phase 2b: highlights"): CleanHighlights puts each text on
// one line and each said_at as HH:MM, and refuses what breaks a limit, naming it, in the contract's order;
// HighlightsSet resolves each minute against the live log in the batches' zone (the earliest start and the latest end
// of the minute's lines, placeholders never counted; a minute the clock shows twice is its first occurrence), sorts
// them by at (one minute's in the order given), changes nothing for the same resolved list and refuses a minute with no
// line; a kept minute that gained a line is a change; only [] clears them. Their revision goes up with each change.
func TestHighlightsSet(t *testing.T) {
	clean, err := CleanHighlights([]HighlightSaid{{Text: " Keep\n the\tcap ", SaidAt: " [9:05] "}, {Text: strings.Repeat("字", MaxHighlightText), SaidAt: "[ 23:59 ]"}})
	if err != nil || len(clean) != 2 || clean[0] != (HighlightSaid{Text: "Keep the cap", SaidAt: "09:05"}) || clean[1].SaidAt != "23:59" {
		t.Fatalf("clean %+v %v", clean, err)
	}
	if none, err := CleanHighlights([]HighlightSaid{}); err != nil || none != nil {
		t.Fatalf("none %+v %v", none, err)
	}
	four := make([]HighlightSaid, MaxHighlights)
	for i := range four {
		four[i] = HighlightSaid{Text: fmt.Sprint("point ", i), SaidAt: "0:00"}
	}
	if _, err := CleanHighlights(four); err != nil {
		t.Fatalf("4 highlights: %v", err)
	}
	for name, c := range map[string]struct {
		said []HighlightSaid
		want string
	}{
		"5":                    {append(slices.Clone(four), HighlightSaid{Text: "x", SaidAt: "0:00"}), "invalid highlights: 5 highlights, over the 4 the meeting may have"},
		"5, one malformed":     {append([]HighlightSaid{{Malformed: true}}, four...), "5 highlights, over the 4"},
		"malformed":            {[]HighlightSaid{{Text: "a", SaidAt: "0:00"}, {Malformed: true}}, "highlight 2 is not an object with a string text and a string said_at"},
		"no text":              {[]HighlightSaid{{Text: " \n ", SaidAt: "x"}}, "highlight 1 has no text"},
		"long":                 {[]HighlightSaid{{Text: "a", SaidAt: "0:00"}, {Text: strings.Repeat("字", MaxHighlightText+1), SaidAt: "0:00"}}, "highlight 2's text is 81 characters, over the 80 a highlight may have"},
		"an offset":            {[]HighlightSaid{{Text: "a", SaidAt: "1:02:03"}}, `highlight 1's said_at is "1:02:03", not the H:MM or HH:MM of a batch line`},
		"hour 24":              {[]HighlightSaid{{Text: "a", SaidAt: "24:00"}}, `said_at is "24:00"`},
		"minute 60":            {[]HighlightSaid{{Text: "a", SaidAt: "9:60"}}, `said_at is "9:60"`},
		"three digits":         {[]HighlightSaid{{Text: "a", SaidAt: "009:05"}}, `said_at is "009:05"`},
		"no minutes":           {[]HighlightSaid{{Text: "a", SaidAt: "9"}}, `said_at is "9"`},
		"empty":                {[]HighlightSaid{{Text: "a", SaidAt: ""}}, `said_at is ""`},
		"the text before time": {[]HighlightSaid{{Text: "", SaidAt: "25:99"}}, "highlight 1 has no text"},
	} {
		if _, err := CleanHighlights(c.said); !errors.Is(err, ErrBadHighlights) || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: %v", name, err)
		}
	}

	e := goldenEngine(t, nil)
	var heard [][]string
	e.MeetingChanged = func(_ string, parts []string) { heard = append(heard, parts) }
	now := time.Date(2026, 11, 10, 14, 0, 0, 0, goldenZone) // 06:00 in UTC
	said := func(pairs ...string) []HighlightSaid {
		var out []HighlightSaid
		for i := 0; i < len(pairs); i += 2 {
			out = append(out, HighlightSaid{Text: pairs[i], SaidAt: pairs[i+1]})
		}
		return out
	}
	if _, err := e.HighlightsSet(recID, said("a", "14:00"), goldenZone, now); !errors.Is(err, ErrNotFound) {
		t.Fatalf("no recording: %v", err)
	}
	putRecording(t, e, recID, attendedUpload(1, now, AttendanceParticipant), now)
	if _, _, err := e.LiveAppend(recID, 0, []LiveSegment{
		{Start: 1, End: 4, Text: "Ada: the cap stays."}, {Start: 0.5, End: 90, Text: ""}, {Start: 30, End: 50, Text: "Bo: for this release."},
		{Start: 59.9, End: 61, Text: "Ada: agreed."}, {Start: 60, End: 62, Text: "Bo: next, hiring."}, {Start: 125, End: 128, Text: "Ada: who owns it?"},
		{Start: 185, End: 186, Text: ""},
	}); err != nil {
		t.Fatal(err)
	}
	// In UTC, single-digit hours and brackets: sorted by at, the minute's first start and last end.
	if changed, err := e.HighlightsSet(recID, said("Who owns hiring is open.", "[6:02]", "The cap stays\nfor this release.", " 06:00 ", "Hiring is next.", "6:01"),
		time.UTC, now); err != nil || !changed {
		t.Fatalf("set: %v %v", changed, err)
	}
	want := []LiveHighlight{{Text: "The cap stays for this release.", At: 1, End: 61}, {Text: "Hiring is next.", At: 60, End: 62}, {Text: "Who owns hiring is open.", At: 125, End: 128}}
	l, _, _ := e.LiveRead(recID)
	if !slices.Equal(l.Highlights, want) || l.HighlightsRev != 1 || !l.HighlightsAt.Equal(now) {
		t.Fatalf("set %+v", l)
	}
	// The same list in the zone the recording's batches would be written in is the same resolved list: nothing changes.
	if changed, err := e.HighlightsSet(recID, said("Hiring is next.", "14:01", "The cap stays for this release.", "14:00", "Who owns hiring is open.", "14:02"),
		goldenZone, now.Add(time.Minute)); err != nil || changed {
		t.Fatalf("the same again: %v %v", changed, err)
	}
	// A minute with no line (a placeholder's, one not said), or an over-limit list: refused, nothing changes.
	for _, bad := range [][]HighlightSaid{said("a", "6:00", "b", "6:03"), said("a", "6:30"), append(said("a", "6:00"), four...)} {
		if _, err := e.HighlightsSet(recID, bad, time.UTC, now.Add(time.Hour)); !errors.Is(err, ErrBadHighlights) {
			t.Fatalf("%v: %v", bad, err)
		}
	}
	if _, err := e.HighlightsSet(recID, said("a", "6:00", "b", "6:03"), time.UTC, now); err == nil ||
		err.Error() != "invalid highlights: highlight 2's said_at 06:03 is not the minute of any line of the meeting so far" {
		t.Fatalf("a minute with no line: %v", err)
	}
	if l, _, _ = e.LiveRead(recID); !slices.Equal(l.Highlights, want) || l.HighlightsRev != 1 || !l.HighlightsAt.Equal(now) {
		t.Fatalf("after refused lists %+v", l)
	}
	// One minute's highlights keep the order given.
	if changed, err := e.HighlightsSet(recID, said("Bo asks who hires.", "6:01", "Hiring is next.", "6:01", "The cap stays.", "6:00"), time.UTC, now); err != nil || !changed {
		t.Fatalf("one minute twice: %v %v", changed, err)
	}
	if l, _, _ = e.LiveRead(recID); fmt.Sprint(l.Highlights) != "[{The cap stays. 1 61} {Bo asks who hires. 60 62} {Hiring is next. 60 62}]" || l.HighlightsRev != 2 {
		t.Fatalf("one minute twice %+v", l.Highlights)
	}
	// A kept minute that gained a line has a later end: the same list is a change.
	if _, _, err := e.LiveAppend(recID, 7, []LiveSegment{{Start: 58, End: 65, Text: "Bo: yes."}}); err != nil {
		t.Fatal(err)
	}
	if changed, err := e.HighlightsSet(recID, said("Bo asks who hires.", "6:01", "Hiring is next.", "6:01", "The cap stays.", "6:00"), time.UTC, now); err != nil || !changed {
		t.Fatalf("a grown minute: %v %v", changed, err)
	}
	if l, _, _ = e.LiveRead(recID); l.Highlights[0] != (LiveHighlight{Text: "The cap stays.", At: 1, End: 65}) || l.HighlightsRev != 3 {
		t.Fatalf("a grown minute %+v", l)
	}
	// Only [] clears them; [] again changes nothing.
	if changed, err := e.HighlightsSet(recID, []HighlightSaid{}, time.UTC, now.Add(time.Hour)); err != nil || !changed {
		t.Fatalf("cleared: %v %v", changed, err)
	}
	if l, _, _ = e.LiveRead(recID); l.Highlights != nil || l.HighlightsRev != 4 || !l.HighlightsAt.Equal(now.Add(time.Hour)) {
		t.Fatalf("cleared %+v", l)
	}
	if changed, err := e.HighlightsSet(recID, nil, time.UTC, now); err != nil || changed {
		t.Fatalf("cleared again: %v %v", changed, err)
	}
	if want := [][]string{{PartLines}, {PartHighlights}, {PartHighlights}, {PartLines}, {PartHighlights}, {PartHighlights}}; fmt.Sprint(heard) != fmt.Sprint(want) {
		t.Fatalf("heard %v, want %v", heard, want)
	}

	// When the clock goes back an hour, a minute it shows twice is its first occurrence.
	ny, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatal(err)
	}
	e = goldenEngine(t, nil)
	start := time.Date(2026, 11, 1, 5, 30, 0, 0, time.UTC) // 01:30 EDT; at 06:00 UTC the clock goes back to 01:00 EST
	putRecording(t, e, recID, attendedUpload(1, start, AttendanceParticipant), start)
	if _, _, err := e.LiveAppend(recID, 0, []LiveSegment{{Start: 10, End: 20, Text: "first"}, {Start: 3610, End: 3620, Text: "an hour later"}}); err != nil {
		t.Fatal(err)
	}
	if r, _ := e.Store.Recording(recID); LiveClock(*r, 3610, ny) != "01:30" {
		t.Fatalf("not a minute shown twice: %s", LiveClock(*r, 3610, ny))
	}
	if _, err := e.HighlightsSet(recID, said("The first.", "1:30"), ny, start); err != nil {
		t.Fatal(err)
	}
	if l, _, _ = e.LiveRead(recID); !slices.Equal(l.Highlights, []LiveHighlight{{Text: "The first.", At: 10, End: 20}}) {
		t.Fatalf("a minute shown twice %+v", l.Highlights)
	}
}
