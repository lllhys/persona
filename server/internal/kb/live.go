package kb

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/nwverse/persona/server/internal/lazyre"
)

// Attending a meeting (docs/assistant/meeting-minutes.md "Attending a meeting"): a meeting recording uploaded with a
// role (listener or participant) gets a live log, the finished sentences the phone appends by position while it
// records (POST /api/v1/recordings/{id}/live), which the assistant feeds to the attending session and answers the
// room's questions from. It is live.json in the recording's directory: the sentences by position, what was fed to
// which session, and the questions asked with their answers. The upload still owns the transcript; the live log is
// the hub's, so it is not in an upload's undo journal (recordingFiles). Never searched (find.go searchable) and never
// shown by Get; a delete or the retention takes it with the directory.

// Attendance roles. Off is stored as "" (omitted in meta.json). Notes is a room meeting's only (#8851): its room keeps
// the live log, the topics and the highlights while Persona answers nothing (Answering), so a meeting Persona was not
// invited to still has its live view; #2981's top-level attendance never takes it.
const (
	AttendanceOff         = "off"
	AttendanceNotes       = "notes"
	AttendanceListener    = "listener"
	AttendanceParticipant = "participant"
)

// What was wrong with an upload's attendance fields (RecordingPut.AttendanceProblem); attending is off then.
const (
	AttendanceUnknownRole = "unknown_role" // attendance is not off, listener or participant
	AttendanceNotMeeting  = "not_meeting"  // a role on a recording whose kind is not meeting
	AttendanceBadSession  = "bad_session"  // attending_session has exactly one id, or one over maxSessionIDRunes
)

// Limits of attending.
const (
	maxSessionIDRunes = 200
	// MaxLiveRequest is how many sentences one POST …/live carries at most (the HTTP layer answers 400 above it;
	// LiveAppend refuses them too).
	MaxLiveRequest   = 200
	maxLiveText      = 4000 // bytes of one sentence: longer ones are cut at a character boundary
	maxLivePositions = 20000
	maxLiveBytes     = 4 << 20 // of live text in all
	// What MeetingMaterial gives of the live sentences, of the photos' words, and of earlier meetings.
	materialLiveRunes  = 12000
	materialPhotoRunes = 8000
	materialMaxEarlier = 6
)

const liveFile = "live.json"

// SessionRef names one session the way the rest of the API does.
type SessionRef struct {
	MachineID string `json:"machine_id"`
	SessionID string `json:"session_id"`
}

// LiveSegment is one finished sentence of the live log: seconds from the recording's start, as the upload's
// segments. Text "" is a placeholder position (an empty sentence), never fed.
type LiveSegment struct {
	Start float64 `json:"start"`
	End   float64 `json:"end"`
	Text  string  `json:"text"`
	// Source is whose sentence it is in hub mode (SourceHub, SourceDevice: filled from the phone's uploads); absent in
	// device mode. Only the hub sets it.
	Source string `json:"source,omitempty"`
}

// AskSource is an earlier meeting (kind recordings) or a project (kind projects) an answer drew on; When, Start and End
// are the meeting's minutes' (#4713: clients format Start / End and show When only without them).
type AskSource struct {
	Kind  string    `json:"kind"`
	ID    string    `json:"id"`
	Name  string    `json:"name"`
	When  string    `json:"when,omitempty"`
	Start time.Time `json:"start,omitzero"`
	End   time.Time `json:"end,omitzero"`
}

// LiveAsk is a question asked during the meeting (POST /api/v1/recordings/{id}/ask) and its answer.
type LiveAsk struct {
	ID       string `json:"id"`
	Question string `json:"question"`
	// QuestionSent is the question as the request had it ("" without one), what a retry with the same id is matched on.
	QuestionSent   string      `json:"question_sent,omitempty"`
	QuestionSource string      `json:"question_source"`
	At             *float64    `json:"at,omitempty"`
	LiveNext       *int        `json:"live_next,omitempty"`
	State          string      `json:"state"` // pending | answered | failed
	Answer         string      `json:"answer"`
	Lang           string      `json:"lang"`
	Speak          bool        `json:"speak"`
	Sources        []AskSource `json:"sources"`
	Error          string      `json:"error,omitempty"`
	AskedAt        time.Time   `json:"asked_at"`
	AnsweredAt     *time.Time  `json:"answered_at,omitempty"`
	// Fed is the hub's: the answer already went to the attending session in a batch.
	Fed bool `json:"fed,omitempty"`
	// Room meetings (docs/assistant/meeting-minutes.md "Asking the room"). Via is the way it is answered (AskViaRoom or
	// AskViaMaterial), fixed when its first attempt goes out ("" before); Spoken the request's spoken (a Talk span);
	// Heard the lines a heard question was made of; Attempts the messages it sent the room, the latest last.
	Via      string        `json:"via,omitempty"`
	Spoken   bool          `json:"spoken,omitempty"`
	Heard    []LiveSegment `json:"heard,omitempty"`
	Attempts []AskAttempt  `json:"attempts,omitempty"`
}

// The ways an ask is answered (LiveAsk.Via).
const (
	AskViaRoom     = "room"
	AskViaMaterial = "material"
)

// AskAttempt is one message a room ask sent the room: its attempt number k (1 for the first), its client_msg_id
// (ask-<id>-<k>), the room's last line before it (where its turn is read from) and when it went out.
type AskAttempt struct {
	K      int       `json:"k"`
	MsgID  string    `json:"msg_id"`
	Base   int       `json:"base"`
	SentAt time.Time `json:"sent_at,omitzero"`
}

// Live is live.json: the live log of an attended meeting and what became of it.
type Live struct {
	Segments []LiveSegment `json:"segments"` // index = position
	Bytes    int           `json:"bytes"`    // the live text's bytes in all (maxLiveBytes)
	Fed      int           `json:"fed"`      // positions given to FedTo at least once
	// FedTo is the session last fed (resolved: the main chat by its real ids), FedAt when.
	FedTo *SessionRef `json:"fed_to,omitempty"`
	FedAt time.Time   `json:"fed_at,omitzero"`
	// EndedAt is when the hub first saw an upload carrying ended_at (PutRecording); EndFed that the end batch went out.
	EndedAt time.Time `json:"ended_at,omitzero"`
	EndFed  bool      `json:"end_fed,omitempty"`
	// Problem is the last feeding problem (attendee_unavailable, attendee_not_allowed), the feeder's.
	Problem string `json:"problem,omitempty"`
	// Full: sentences were refused for the limits (ErrLiveFull), so the phone stopped sending them (problem live_full).
	Full bool      `json:"full,omitempty"`
	Asks []LiveAsk `json:"asks,omitempty"`
	// AsksRev is the asks' revision (the meeting_live event's asks): it goes up whenever they change (LiveUpdate).
	AsksRev int64 `json:"asks_rev,omitempty"`
	// Topics are the meeting's topics as its room set them (meeting_topics_set, TopicsSet), TopicsAt their last change
	// and TopicsRev their revision (the meeting_live event's topics), which goes up whenever they change (LiveUpdate).
	// Kept here, they go with the transcript: the trim takes live.json.
	Topics    []LiveTopic `json:"topics,omitempty"`
	TopicsAt  time.Time   `json:"topics_at,omitzero"`
	TopicsRev int64       `json:"topics_rev,omitempty"`
	// Highlights are the meeting's highlights so far as its room set them (meeting_highlights_set, HighlightsSet),
	// the earliest first; HighlightsAt their last change and HighlightsRev their revision (the meeting_live event's
	// highlights), which goes up whenever they change (LiveUpdate). They go with the transcript as the topics do.
	Highlights    []LiveHighlight `json:"highlights,omitempty"`
	HighlightsAt  time.Time       `json:"highlights_at,omitzero"`
	HighlightsRev int64           `json:"highlights_rev,omitempty"`
}

// The topics of a room meeting (docs/assistant/meeting-minutes.md "Phase 2: live view"): each done, current (at most
// one) or open, set whole by the room.
const (
	TopicDone    = "done"
	TopicCurrent = "current"
	TopicOpen    = "open"
	// The limits of meeting_topics_set: a call over one is refused, saying which.
	MaxTopics     = 20
	MaxTopicTitle = 80  // characters
	MaxTopicNote  = 200 // characters
)

// LiveTopic is one topic of the meeting: its title, its state, the seconds from the start when it began (nil: not
// said) and the room's short note on it.
type LiveTopic struct {
	Title string   `json:"title"`
	State string   `json:"state"`
	At    *float64 `json:"at,omitempty"`
	Note  string   `json:"note,omitempty"`
}

// The limits of meeting_highlights_set (docs/assistant/meeting-minutes.md "Phase 2b: highlights"): a call over one is
// refused, saying which.
const (
	MaxHighlights    = 4
	MaxHighlightText = 80 // characters, once on one line
)

// LiveHighlight is one highlight of the meeting so far: one short sentence and the minute of the live log it was
// made in, At the start of that minute's first line and End the end of its last, in seconds from the recording's
// start.
type LiveHighlight struct {
	Text string  `json:"text"`
	At   float64 `json:"at"`
	End  float64 `json:"end"`
}

// HighlightSaid is one highlight as the room names it (meeting_highlights_set): its text and SaidAt, the clock time
// (H:MM or HH:MM) of a batch line it was made in. Malformed: the room's item was not an object with a string text and
// a string said_at, which CleanHighlights refuses in its place.
type HighlightSaid struct {
	Text, SaidAt string
	Malformed    bool
}

var (
	// ErrNotAttending: the stored upload has attendance off.
	ErrNotAttending = errors.New("Persona does not attend this recording")
	// ErrLiveGap: from is above the positions held; a LiveGapError says how many there are.
	ErrLiveGap = errors.New("live sentences are missing before from")
	// ErrLiveFull: the live log reached maxLivePositions or maxLiveBytes; nothing of the request was stored.
	ErrLiveFull = errors.New("the live log of this recording is full")
	// ErrBadLive wraps what is wrong with a live request (a negative from, too many sentences).
	ErrBadLive = errors.New("invalid live sentences")
	// ErrBadTopics wraps the limit a meeting_topics_set call broke (CleanTopics).
	ErrBadTopics = errors.New("invalid topics")
	// ErrBadHighlights wraps the limit a meeting_highlights_set call broke (CleanHighlights, HighlightsSet).
	ErrBadHighlights = errors.New("invalid highlights")
)

// LiveGapError is ErrLiveGap with how many positions the hub holds: the phone sends again from Next. errors.As finds
// it as a LiveGapError and as a *LiveGapError.
type LiveGapError struct{ Next int }

func (e LiveGapError) Error() string { return fmt.Sprintf("%v: send from %d", ErrLiveGap, e.Next) }
func (e LiveGapError) Unwrap() error { return ErrLiveGap }

func (e LiveGapError) As(target any) bool {
	if p, ok := target.(**LiveGapError); ok {
		c := e
		*p = &c
		return true
	}
	return false
}

// AttendanceRole is the role r is attended in: AttendanceOff, AttendanceNotes (a room meeting's only),
// AttendanceListener or AttendanceParticipant. A room
// meeting's is its room's (meeting.json), never meta.json's; a trimmed recording is attended no more.
func (r Recording) AttendanceRole() string {
	role := r.Attendance
	if r.RoomMeeting() {
		role = r.Conv.Room.Attendance
	}
	if role == "" || r.TrimmedAt != nil {
		return AttendanceOff
	}
	return role
}

// UnmarshalJSON reads an upload like the default does, except that a value of the wrong type for attendance or
// attending_session does not fail it (the same request carries the transcript): normalizeAttendance turns attending
// off for it. Nor does one for room, private, transcription or timeline (readRoomAndPrivate, readTranscription), nor
// a malformed spoken (readSpoken).
func (u *RecordingUpload) UnmarshalJSON(data []byte) error {
	type plain RecordingUpload
	aux := struct {
		*plain
		Attendance       json.RawMessage `json:"attendance"`
		AttendingSession json.RawMessage `json:"attending_session"`
		Room             json.RawMessage `json:"room"`
		Private          json.RawMessage `json:"private"`
		Transcription    json.RawMessage `json:"transcription"`
		Timeline         json.RawMessage `json:"timeline"`
		Spoken           json.RawMessage `json:"spoken"`
	}{plain: (*plain)(u)}
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	u.Spoken = readSpoken(aux.Spoken)
	u.readRoomAndPrivate(aux.Room, aux.Private)
	u.readTranscription(aux.Transcription, aux.Timeline)
	u.Attendance, u.AttendingSession, u.badRole, u.badSession = "", nil, false, false
	if raw := aux.Attendance; !jsonNull(raw) {
		u.badRole = json.Unmarshal(raw, &u.Attendance) != nil
	}
	if raw := aux.AttendingSession; !jsonNull(raw) {
		var keys map[string]json.RawMessage
		if json.Unmarshal(raw, &keys) != nil {
			u.badSession = true
			return nil
		}
		ref := &SessionRef{}
		for key, dst := range map[string]*string{"machine_id": &ref.MachineID, "session_id": &ref.SessionID} {
			if v := keys[key]; !jsonNull(v) && json.Unmarshal(v, dst) != nil {
				u.badSession = true
			}
		}
		if !u.badSession {
			u.AttendingSession = ref
		}
	}
	return nil
}

// jsonNull: a field absent or null.
func jsonNull(raw json.RawMessage) bool {
	raw = bytes.TrimSpace(raw)
	return len(raw) == 0 || string(raw) == "null"
}

// normalizeAttendance settles the attendance fields of an upload clean accepted (its Kind is set), never failing it:
// an unknown role, a role on a recording that is not a meeting, or a session with exactly one id or one over
// maxSessionIDRunes leaves attending off, and the problem says which. Off is "" with no session; a session with no
// ids is nil (the main chat). Watch recordings are not refused here: the phone never gives them a role.
func (u *RecordingUpload) normalizeAttendance() (problem string) {
	role := strings.ToLower(strings.TrimSpace(u.Attendance))
	session := u.AttendingSession
	u.Attendance, u.AttendingSession = "", nil
	switch {
	case u.badRole:
		return AttendanceUnknownRole
	case role == "" || role == AttendanceOff:
		return ""
	case role != AttendanceListener && role != AttendanceParticipant:
		return AttendanceUnknownRole
	case u.Kind != RecordingMeeting:
		return AttendanceNotMeeting
	case u.badSession:
		return AttendanceBadSession
	}
	if session != nil {
		ref := SessionRef{MachineID: strings.TrimSpace(session.MachineID), SessionID: strings.TrimSpace(session.SessionID)}
		switch {
		case (ref.MachineID == "") != (ref.SessionID == ""),
			utf8.RuneCountInString(ref.MachineID) > maxSessionIDRunes, utf8.RuneCountInString(ref.SessionID) > maxSessionIDRunes:
			return AttendanceBadSession
		case ref.MachineID != "":
			u.AttendingSession = &ref
		}
	}
	u.Attendance = role
	return ""
}

// readLive is recording id's live.json; an empty Live when there is none yet.
func (s *Store) readLive(id string) (Live, error) {
	var l Live
	raw, err := os.ReadFile(s.path(KindRecording, id, liveFile))
	if os.IsNotExist(err) {
		return l, nil
	}
	if err != nil {
		return l, err
	}
	if err := json.Unmarshal(raw, &l); err != nil {
		return Live{}, fmt.Errorf("live log of %s: %w", id, err)
	}
	return l, nil
}

// putLive writes recording id's live.json as putMinutes writes the minutes: not when the recording was deleted or
// trimmed meanwhile (false then), so a delete stays a delete and a trim keeps no live log. The trim (recording_trim.go)
// holds recFilesMu from its meta.json to its last removal, so a read-modify-write that read the live log before it
// cannot write it back after. The caller holds liveMu.
func (s *Store) putLive(id string, l *Live) (bool, error) {
	if s.readOnly {
		return false, ErrReadOnly
	}
	raw, err := json.Marshal(l)
	if err != nil {
		return false, err
	}
	s.recFilesMu.Lock()
	defer s.recFilesMu.Unlock()
	if err := s.recoverRecordingLocked(id); err != nil {
		return false, err
	}
	meta, err := os.ReadFile(s.path(KindRecording, id, "meta.json"))
	if err != nil {
		return false, nil
	}
	var trim struct {
		TrimmedAt *time.Time `json:"trimmed_at"`
	}
	if json.Unmarshal(meta, &trim) == nil && trim.TrimmedAt != nil {
		return false, nil
	}
	// Synced before it replaces the old one: the phone was told next, and does not send those positions again.
	return true, writeRecordingFile(s.path(KindRecording, id, liveFile), append(raw, '\n'))
}

// liveState is recording id (a stored id) with its live log: ErrRecordingDeleted after a delete, ErrNotFound with
// no upload of it.
func (s *Store) liveState(id string) (Live, Recording, error) {
	if _, gone := s.tombstones()[id]; gone {
		return Live{}, Recording{}, ErrRecordingDeleted
	}
	r, ok := s.Recording(id)
	if !ok {
		return Live{}, Recording{}, ErrNotFound
	}
	l, err := s.readLive(id)
	return l, *r, err
}

// liveEnded notes in recording id's live log when the hub first saw it ended, for a recording that attends
// (attending) or has a live log already. The caller holds recMu.
func (s *Store) liveEnded(id string, attending bool, now time.Time) error {
	s.liveMu.Lock()
	defer s.liveMu.Unlock()
	if !attending {
		if _, err := os.Stat(s.path(KindRecording, id, liveFile)); err != nil {
			return nil
		}
	}
	l, err := s.readLive(id)
	if err != nil || !l.EndedAt.IsZero() {
		return err
	}
	l.EndedAt = now.UTC()
	_, err = s.putLive(id, &l)
	return err
}

// cleanLive is a sentence as the live log keeps it: a bad one is mended, never refused, since positions stay
// contiguous. A negative start is 0, an end before the start the start; the text is one line, cut to maxLiveText bytes
// at a character boundary.
func cleanLive(sg LiveSegment) LiveSegment {
	if math.IsNaN(sg.Start) || math.IsInf(sg.Start, 0) || sg.Start < 0 {
		sg.Start = 0
	}
	if math.IsNaN(sg.End) || math.IsInf(sg.End, 0) || sg.End < sg.Start {
		sg.End = sg.Start
	}
	sg.Text = cutBytes(strings.Join(strings.Fields(strings.ToValidUTF8(sg.Text, "�")), " "), maxLiveText)
	return sg
}

// cutBytes is s within n bytes, cut at a character boundary.
func cutBytes(s string, n int) string {
	if len(s) <= n {
		return s
	}
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n]
}

// LiveAppend stores sentences of recording id's live log, segs[0] at position from: the positions held already are
// skipped (a replay or a retry stores nothing twice) and the rest appended. Errors in the contract's order:
// ErrRecordingDeleted, ErrNotFound (no upload yet), ErrReadOnly, ErrBadLive (a negative from, over MaxLiveRequest
// sentences), ErrNotAttending (the stored upload has attendance off), ErrHubTranscribes (the hub transcribes it: its
// live log is the hub's, LiveAppendHub), a LiveGapError (from above the positions held),
// ErrLiveFull (the request would pass maxLivePositions or maxLiveBytes; nothing of it is stored). The Live returned is
// the log after it, also with ErrLiveGap and ErrLiveFull. Engine.LiveChanged hears of every append.
func (e *Engine) LiveAppend(rawID string, from int, segs []LiveSegment) (Live, Recording, error) {
	id, ok := RecordingID(rawID)
	if !ok {
		return Live{}, Recording{}, ErrNotFound
	}
	l, r, stored, err := e.liveAppend(id, from, segs)
	if stored {
		if e.LiveChanged != nil {
			e.LiveChanged(id)
		}
		e.meetingChanged(id, []string{PartLines}) // its revision is the live log's length
	}
	return l, r, err
}

func (e *Engine) liveAppend(id string, from int, segs []LiveSegment) (Live, Recording, bool, error) {
	st := e.Store
	st.liveMu.Lock()
	defer st.liveMu.Unlock()
	l, r, err := st.liveState(id)
	switch {
	case err != nil:
		return l, r, false, err
	case st.ReadOnly():
		return l, r, false, ErrReadOnly
	case from < 0:
		return l, r, false, fmt.Errorf("%w: from must be 0 or more", ErrBadLive)
	case len(segs) > MaxLiveRequest:
		return l, r, false, fmt.Errorf("%w: at most %d sentences a request", ErrBadLive, MaxLiveRequest)
	case !r.Attending():
		return l, r, false, ErrNotAttending
	case r.HubTranscribed():
		return l, r, false, ErrHubTranscribes
	case from > len(l.Segments):
		return l, r, false, LiveGapError{Next: len(l.Segments)}
	}
	fresh := segs[min(len(l.Segments)-from, len(segs)):]
	if len(fresh) == 0 {
		return l, r, false, nil
	}
	add, size := make([]LiveSegment, len(fresh)), 0
	for i, sg := range fresh {
		add[i] = cleanLive(sg)
		size += len(add[i].Text)
	}
	if len(l.Segments)+len(add) > maxLivePositions || l.Bytes+size > maxLiveBytes {
		if !l.Full {
			l.Full = true
			if _, err := st.putLive(id, &l); err != nil {
				e.logf("recording %s: live full: %v", id, err)
			}
		}
		return l, r, false, ErrLiveFull
	}
	next := l
	next.Segments = append(append(make([]LiveSegment, 0, len(l.Segments)+len(add)), l.Segments...), add...)
	next.Bytes += size
	if ok, err := st.putLive(id, &next); err != nil {
		return l, r, false, err
	} else if !ok {
		return l, r, false, st.goneError(id)
	}
	return next, r, true, nil
}

// LiveAppendHub appends the hub's sentences at the end of recording rawID's live log: no from and no role check (the
// caller applied the timeline), each Source SourceHub unless it is SourceDevice (the phone's text filling in), mended
// as LiveAppend mends them. first is the position of segs[0] (the live log's length when segs is empty). Errors:
// ErrRecordingDeleted, ErrNotFound, ErrReadOnly, ErrTranscriptExpired (trimmed), ErrNotHub (not transcribed on the
// hub), ErrLiveFull (past maxLivePositions or maxLiveBytes; nothing of it is stored). LiveChanged and MeetingChanged
// (PartLines) hear of an append.
func (e *Engine) LiveAppendHub(rawID string, segs []LiveSegment) (first int, err error) {
	id, ok := RecordingID(rawID)
	if !ok {
		return 0, ErrNotFound
	}
	first, stored, err := e.liveAppendHub(id, segs)
	if stored {
		if e.LiveChanged != nil {
			e.LiveChanged(id)
		}
		e.meetingChanged(id, []string{PartLines})
	}
	return first, err
}

func (e *Engine) liveAppendHub(id string, segs []LiveSegment) (int, bool, error) {
	st := e.Store
	st.liveMu.Lock()
	defer st.liveMu.Unlock()
	l, r, err := st.liveState(id)
	switch {
	case err != nil:
		return 0, false, err
	case st.ReadOnly():
		return len(l.Segments), false, ErrReadOnly
	case r.Trimmed():
		return len(l.Segments), false, ErrTranscriptExpired
	case !r.HubTranscribed():
		return len(l.Segments), false, ErrNotHub
	}
	first := len(l.Segments)
	if len(segs) == 0 {
		return first, false, nil
	}
	add, size := make([]LiveSegment, len(segs)), 0
	for i, sg := range segs {
		if sg.Source != SourceDevice {
			sg.Source = SourceHub
		}
		add[i] = cleanLive(sg)
		size += len(add[i].Text)
	}
	if len(l.Segments)+len(add) > maxLivePositions || l.Bytes+size > maxLiveBytes {
		if !l.Full {
			l.Full = true
			if _, err := st.putLive(id, &l); err != nil {
				e.logf("recording %s: live full: %v", id, err)
			}
		}
		return first, false, ErrLiveFull
	}
	next := l
	next.Segments = append(append(make([]LiveSegment, 0, len(l.Segments)+len(add)), l.Segments...), add...)
	next.Bytes += size
	if ok, err := st.putLive(id, &next); err != nil {
		return first, false, err
	} else if !ok {
		if err := st.goneError(id); !errors.Is(err, ErrNotAttending) {
			return first, false, err
		}
		return first, false, ErrTranscriptExpired // trimmed meanwhile
	}
	return first, true, nil
}

// goneError is why recording id takes no live log any more: deleted, trimmed (attending it is over) or never uploaded.
func (s *Store) goneError(id string) error {
	if _, gone := s.tombstones()[id]; gone {
		return ErrRecordingDeleted
	}
	if r, ok := s.Recording(id); ok && r.Trimmed() {
		return ErrNotAttending
	}
	return ErrNotFound
}

// LiveRead is recording id's live log (empty before any) and its stored upload, also on a read-only knowledge base:
// ErrRecordingDeleted after a delete, ErrNotFound with no upload of it.
func (e *Engine) LiveRead(rawID string) (Live, Recording, error) {
	id, ok := RecordingID(rawID)
	if !ok {
		return Live{}, Recording{}, ErrNotFound
	}
	return e.Store.liveState(id)
}

// LiveUpdate changes recording id's live log with fn (given it and the stored upload) and writes the result unless fn
// fails or changed nothing; the live log after it. The asks', the topics' and the highlights' revisions go up when fn
// changed them, and MeetingChanged hears of them. ErrNotFound when the recording's meta.json is gone (also deleted
// meanwhile) or the recording was trimmed meanwhile, ErrReadOnly on a read-only knowledge base. fn runs with the live
// log locked: it must be quick and call no Engine method of the live log or the recordings.
func (e *Engine) LiveUpdate(rawID string, fn func(l *Live, r Recording) error) (Live, error) {
	id, ok := RecordingID(rawID)
	if !ok {
		return Live{}, ErrNotFound
	}
	l, parts, err := e.liveUpdate(id, fn)
	e.meetingChanged(id, parts)
	return l, err
}

func (e *Engine) liveUpdate(id string, fn func(l *Live, r Recording) error) (Live, []string, error) {
	st := e.Store
	st.liveMu.Lock()
	defer st.liveMu.Unlock()
	r, ok := st.Recording(id)
	if !ok {
		return Live{}, nil, ErrNotFound
	}
	if st.ReadOnly() {
		return Live{}, nil, ErrReadOnly
	}
	l, err := st.readLive(id)
	if err != nil {
		return Live{}, nil, err
	}
	before, err := json.Marshal(l)
	if err != nil {
		return Live{}, nil, err
	}
	asksBefore, _ := json.Marshal(l.Asks)
	topicsBefore, _ := json.Marshal(l.Topics)
	highlightsBefore, _ := json.Marshal(l.Highlights)
	if err := fn(&l, *r); err != nil {
		return Live{}, nil, err
	}
	after, err := json.Marshal(l)
	if err != nil || bytes.Equal(before, after) {
		return l, nil, err
	}
	var parts []string
	if asksAfter, _ := json.Marshal(l.Asks); !bytes.Equal(asksBefore, asksAfter) {
		l.AsksRev++
		parts = append(parts, PartAsks)
	}
	if topicsAfter, _ := json.Marshal(l.Topics); !bytes.Equal(topicsBefore, topicsAfter) {
		l.TopicsRev++
		parts = append(parts, PartTopics)
	}
	if highlightsAfter, _ := json.Marshal(l.Highlights); !bytes.Equal(highlightsBefore, highlightsAfter) {
		l.HighlightsRev++
		parts = append(parts, PartHighlights)
	}
	if ok, err := st.putLive(id, &l); err != nil {
		return Live{}, nil, err
	} else if !ok {
		return Live{}, nil, ErrNotFound
	}
	return l, parts, nil
}

// CleanTopics is topics as the live log keeps them, or ErrBadTopics saying which limit they break: at most MaxTopics,
// each with a title of at most MaxTopicTitle characters and a state done, current or open (at most one current), at
// (when given) 0 or more seconds, a note of at most MaxTopicNote characters. Titles and notes are put on one line
// first; none at all is nil.
func CleanTopics(topics []LiveTopic) ([]LiveTopic, error) {
	if len(topics) > MaxTopics {
		return nil, fmt.Errorf("%w: %d topics, over the %d the meeting may have", ErrBadTopics, len(topics), MaxTopics)
	}
	var out []LiveTopic
	current := 0
	for i, t := range topics {
		n := i + 1
		t.Title, t.Note = topicLine(t.Title), topicLine(t.Note)
		t.State = strings.ToLower(strings.TrimSpace(t.State))
		switch c := utf8.RuneCountInString(t.Title); {
		case c == 0:
			return nil, fmt.Errorf("%w: topic %d has no title", ErrBadTopics, n)
		case c > MaxTopicTitle:
			return nil, fmt.Errorf("%w: topic %d's title is %d characters, over the %d a title may have", ErrBadTopics, n, c, MaxTopicTitle)
		}
		switch t.State {
		case TopicDone, TopicOpen:
		case TopicCurrent:
			current++
		default:
			return nil, fmt.Errorf("%w: topic %d's state is %q, not done, current or open", ErrBadTopics, n, t.State)
		}
		if t.At != nil && (math.IsNaN(*t.At) || math.IsInf(*t.At, 0) || *t.At < 0) {
			return nil, fmt.Errorf("%w: topic %d's at is not 0 or more seconds from the meeting's start", ErrBadTopics, n)
		}
		if c := utf8.RuneCountInString(t.Note); c > MaxTopicNote {
			return nil, fmt.Errorf("%w: topic %d's note is %d characters, over the %d a note may have", ErrBadTopics, n, c, MaxTopicNote)
		}
		if t.At != nil {
			at := *t.At
			t.At = &at
		}
		out = append(out, t)
	}
	if current > 1 {
		return nil, fmt.Errorf("%w: %d topics are current, and at most one may be", ErrBadTopics, current)
	}
	return out, nil
}

// topicLine is a topic's title or note on one line, valid UTF-8.
func topicLine(s string) string {
	return strings.Join(strings.Fields(strings.ToValidUTF8(s, "\uFFFD")), " ")
}

// TopicsSet replaces recording rawID's topics with topics (CleanTopics: ErrBadTopics names the limit broken), at now;
// false when they were those already, which changes nothing. LiveUpdate's errors otherwise; MeetingChanged hears of
// PartTopics.
func (e *Engine) TopicsSet(rawID string, topics []LiveTopic, now time.Time) (bool, error) {
	clean, err := CleanTopics(topics)
	if err != nil {
		return false, err
	}
	want, _ := json.Marshal(clean)
	changed := false
	_, err = e.LiveUpdate(rawID, func(l *Live, _ Recording) error {
		if have, _ := json.Marshal(l.Topics); bytes.Equal(have, want) {
			return nil
		}
		l.Topics, l.TopicsAt, changed = clean, now.UTC(), true
		return nil
	})
	return changed && err == nil, err
}

// LiveClock is the clock time a batch gives the moment sec seconds from recording r's start, in loc ("15:04"): the
// [HH:MM] of a batch's lines (the assistant's feedText) and the minute a highlight's said_at names (HighlightsSet), one
// function so the two never differ.
func LiveClock(r Recording, sec float64, loc *time.Location) string {
	return r.StartedAt.Add(time.Duration(sec * float64(time.Second))).In(loc).Format("15:04")
}

// highlightSaidAt is H:MM or HH:MM, hours 0 to 23.
var highlightSaidAt = lazyre.New(`^([01]?[0-9]|2[0-3]):[0-5][0-9]$`)

// highlightMinute is the minute said (a said_at) names, as LiveClock writes it ("9:05" is "09:05"), brackets and
// spaces around it dropped; false when it is not H:MM or HH:MM.
func highlightMinute(said string) (string, bool) {
	said = strings.TrimFunc(said, func(r rune) bool { return unicode.IsSpace(r) || r == '[' || r == ']' })
	if !highlightSaidAt.MatchString(said) {
		return "", false
	}
	if len(said) == len("9:05") {
		said = "0" + said
	}
	return said, true
}

// CleanHighlights is said as HighlightsSet resolves it, or ErrBadHighlights saying which limit it breaks: at most
// MaxHighlights, each an object with a string text and a string said_at, a text of 1 to MaxHighlightText characters
// once on one line, a said_at H:MM or HH:MM (put as HH:MM). None at all is nil.
func CleanHighlights(said []HighlightSaid) ([]HighlightSaid, error) {
	if len(said) > MaxHighlights {
		return nil, fmt.Errorf("%w: %d highlights, over the %d the meeting may have", ErrBadHighlights, len(said), MaxHighlights)
	}
	var out []HighlightSaid
	for i, h := range said {
		n := i + 1
		if h.Malformed {
			return nil, fmt.Errorf("%w: highlight %d is not an object with a string text and a string said_at", ErrBadHighlights, n)
		}
		h.Text = topicLine(h.Text)
		switch c := utf8.RuneCountInString(h.Text); {
		case c == 0:
			return nil, fmt.Errorf("%w: highlight %d has no text", ErrBadHighlights, n)
		case c > MaxHighlightText:
			return nil, fmt.Errorf("%w: highlight %d's text is %d characters, over the %d a highlight may have", ErrBadHighlights, n, c, MaxHighlightText)
		}
		minute, ok := highlightMinute(h.SaidAt)
		if !ok {
			return nil, fmt.Errorf("%w: highlight %d's said_at is %q, not the H:MM or HH:MM of a batch line", ErrBadHighlights, n, h.SaidAt)
		}
		out = append(out, HighlightSaid{Text: h.Text, SaidAt: minute})
	}
	return out, nil
}

// HighlightsSet replaces recording rawID's highlights with said (CleanHighlights), each resolved against the live log
// so far in loc, the time zone the batches are written in (highlightsResolved), at now; false when the resolved list
// is the one there already (each text, at and end), which changes nothing. A list that breaks a limit (among them a
// said_at whose minute has no line) is refused with ErrBadHighlights naming it, and nothing changes; [] clears them.
// LiveUpdate's errors otherwise; MeetingChanged hears of PartHighlights.
func (e *Engine) HighlightsSet(rawID string, said []HighlightSaid, loc *time.Location, now time.Time) (bool, error) {
	clean, err := CleanHighlights(said)
	if err != nil {
		return false, err
	}
	if loc == nil { // the caller's zone is the batches' (s.location); without one, the archive's (never the machine's)
		loc = e.Store.Zone()
	}
	changed := false
	_, err = e.LiveUpdate(rawID, func(l *Live, r Recording) error {
		hs, err := highlightsResolved(clean, l.Segments, r, loc)
		if err != nil {
			return err
		}
		have, _ := json.Marshal(l.Highlights)
		if want, _ := json.Marshal(hs); bytes.Equal(have, want) {
			return nil
		}
		l.Highlights, l.HighlightsAt, changed = hs, now.UTC(), true
		return nil
	})
	return changed && err == nil, err
}

// highlightsResolved are clean's highlights with the moments of their minutes in recording r's live log segs
// (highlightLines), sorted by at (those of one minute in the order given); ErrBadHighlights for one whose minute has
// no line.
func highlightsResolved(clean []HighlightSaid, segs []LiveSegment, r Recording, loc *time.Location) ([]LiveHighlight, error) {
	if len(clean) == 0 {
		return nil, nil
	}
	clocks := make([]string, len(segs)) // each line's minute as a batch writes it; "" for a placeholder, which never counts
	for i, sg := range segs {
		if sg.Text != "" {
			clocks[i] = LiveClock(r, sg.Start, loc)
		}
	}
	var out []LiveHighlight
	for i, h := range clean {
		at, end, ok := highlightLines(segs, clocks, h.SaidAt)
		if !ok {
			return nil, fmt.Errorf("%w: highlight %d's said_at %s is not the minute of any line of the meeting so far", ErrBadHighlights, i+1, h.SaidAt)
		}
		out = append(out, LiveHighlight{Text: h.Text, At: at, End: end})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].At < out[j].At })
	return out, nil
}

// highlightLines is minute (HH:MM) in the live log segs, whose lines a batch writes at clocks: the earliest start and
// the latest end of the positions whose clock is minute. A minute the clock shows twice (it went back an hour) is its
// first occurrence: the lines that start within a minute of the earliest. False when no line is in it.
func highlightLines(segs []LiveSegment, clocks []string, minute string) (at, end float64, ok bool) {
	for i, sg := range segs {
		if clocks[i] == minute && (!ok || sg.Start < at) {
			at, ok = sg.Start, true
		}
	}
	if !ok {
		return 0, 0, false
	}
	end = at
	for i, sg := range segs {
		if clocks[i] == minute && sg.Start-at < 60 {
			end = max(end, sg.End)
		}
	}
	return at, end, true
}

// AttendingRecordings is every recording whose stored attendance is not off and whose end batch has not gone out,
// oldest first: what the feeder picks up again after a restart.
func (e *Engine) AttendingRecordings() []Recording {
	var out []Recording
	for _, r := range e.Store.Recordings() {
		if !r.Attending() {
			continue
		}
		if l, err := e.Store.readLive(r.ID); err == nil && l.EndFed {
			continue
		}
		out = append(out, *r)
	}
	sort.Slice(out, func(i, j int) bool {
		if !out[i].StartedAt.Equal(out[j].StartedAt) {
			return out[i].StartedAt.Before(out[j].StartedAt)
		}
		return out[i].ID < out[j].ID
	})
	return out
}
