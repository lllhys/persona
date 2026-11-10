package kb

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
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
