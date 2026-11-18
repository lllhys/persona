package kb

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/nwverse/persona/server/internal/prompts"
)

// A recording's title written from what was said (#8873, docs/assistant/meeting-minutes.md "Titles"): one nobody named
// (no title of the user's, no calendar) is named by the model while it records (the live title), and once it is complete
// by its summary's title, which a meeting's minutes then settle. The owner's own title and the calendar's always come
// first (NameSource), so a renamed recording is never renamed again.

// PurposeMeetingTitle is the model use of the live title: Information processing's, as chat titles are.
const PurposeMeetingTitle = "meeting.title"

// What wrote a recording's AITitle (Recording.AITitleFrom); "" is the summary's, written before it was said.
const (
	AITitleLive    = "live"
	AITitleSummary = "summary"
	AITitleMinutes = "minutes"
)

const (
	// liveTitleFirstChars is the meeting text (Chars: the transcript of record, Persona's echo left out) the first
	// live title waits for: about two minutes of talk.
	liveTitleFirstChars = 400
	// Another live title is asked for only once the text has doubled since the last and liveTitleEvery has passed,
	// at most liveTitleMax calls a recording (failed ones included), and only while it is uploaded to: one with no
	// upload for liveTitleStale (a lost phone) is left as it is.
	liveTitleEvery = 10 * time.Minute
	liveTitleMax   = 4
	liveTitleStale = 12 * time.Hour
	// What a call reads of a long transcript: its start and its latest part.
	liveTitleHead = 2000
	liveTitleTail = 4000
	// liveTitleCallTime bounds one call: the answer is a few words.
	liveTitleCallTime = time.Minute
	// titleRunes is the most of a model's title kept (a cut ends on "…"): a list row shows one line of it.
	titleRunes = 60
)

// liveTitleDue: r, still recording and named by nobody, has enough new text for its next live title.
func (r *Recording) liveTitleDue(now time.Time) bool {
	switch {
	case r.Complete, r.Trimmed(), r.Kind == RecordingDay:
		return false
	case r.Renamed != "" || r.Title != "" || calendarTitle(r) != "": // the owner's or the calendar's name comes first
		return false
	case r.AITitleFrom == AITitleSummary || r.AITitleFrom == AITitleMinutes: // settled
		return false
	case r.Chars < liveTitleFirstChars, r.LiveTitles >= liveTitleMax, now.Sub(r.Updated) > liveTitleStale:
		return false
	case r.LiveTitles > 0 && (now.Sub(r.LiveTitleAt) < liveTitleEvery || r.Chars < 2*r.LiveTitleChars):
		return false
	}
	return true
}

// ErrBadTitle: a name given on the web is not one line of at most renameRunes characters.
var ErrBadTitle = errors.New("a title is one line of at most 200 characters")

// renameRunes is the most of a name given on the web, as the phone's calendar titles.
const renameRunes = 200

// RenameRecording gives recording id the owner's name from the web (#8877): it names the recording before the upload's
// title until an upload changes that title (the phone renamed it since), and "" takes it back. Not for a deleted
// recording (ErrRecordingDeleted) or one there is none of (ErrNotFound); a trimmed one keeps its meta.json, so it can.
func (e *Engine) RenameRecording(rawID, title string, now time.Time) (*Recording, error) {
	if e.Store.ReadOnly() {
		return nil, ErrReadOnly
	}
	id, ok := RecordingID(rawID)
	if !ok {
		return nil, badRecording("id must be a UUID")
	}
	title = strings.TrimSpace(title)
	if strings.ContainsAny(title, "\r\n") || utf8.RuneCountInString(title) > renameRunes {
		return nil, ErrBadTitle
	}
	st := e.Store
	st.recMu.Lock()
	defer st.recMu.Unlock()
	if _, gone := st.tombstones()[id]; gone {
		return nil, ErrRecordingDeleted
	}
	r, ok := st.Recording(id)
	if !ok {
		return nil, ErrNotFound
	}
	r.Renamed, r.RenamedAt = title, nil
	if title != "" {
		at := now.UTC()
		r.RenamedAt = &at
	}
	r.Name = e.recordingName(r)
	if err := st.putEntity(KindRecording, r.ID, r, &r.Tagging, false); err != nil {
		return nil, err
	}
	st.notify(KindRecording, r.ID, "metadata")
	e.index(KindRecording, r.ID, r.Name, r.Tagging, r.OneLine, r.StartedAt)
	return r, nil
}

// named puts r's name in the answer, with where it came from; neither while it is named only by when it started.
func (p *RecordingPut) named(r *Recording) {
	if p.NameSource = r.NameSource(); p.NameSource != "" {
		p.Name = r.Name
	}
}

// metaWithNameSource is a recording's meta.json (detail.meta) with name_source, where its name comes from (#8873);
// as it is when it is named only by when it started.
func metaWithNameSource(raw []byte) []byte {
	var r Recording
	if json.Unmarshal(raw, &r) != nil {
		return raw
	}
	src := r.NameSource()
	var meta map[string]json.RawMessage
	if src == "" || json.Unmarshal(raw, &meta) != nil || meta == nil {
		return raw
	}
	meta["name_source"], _ = json.Marshal(src)
	out, err := json.Marshal(meta)
	if err != nil {
		return raw
	}
	return out
}

// titleLine is a title the model wrote, on one line, without marks or the quotes around it, at most titleRunes.
func titleLine(s string) string {
	s = strings.Join(strings.Fields(stripMarks(s)), " ")
	s = strings.TrimSpace(strings.Trim(s, "\"'“”‘’「」『』《》"))
	return clipAtMost(s, titleRunes)
}

// liveTitle is the model's answer for a live title (kb/recording_title).
type liveTitle struct {
	Title string `json:"title"`
	Keep  bool   `json:"keep"`
}

// TitleRecordings writes the live titles that are due (liveTitleDue), the most recently uploaded recording first, at
// most max calls a run; how many calls it made. A paused connection or no model set up stops the run without counting:
// they stay due. Any other failure counts as a call, so the next waits liveTitleEvery.
func (e *Engine) TitleRecordings(ctx context.Context, max int) int {
	if e.Model == nil && e.ModelFor == nil {
		return 0
	}
	now := e.now()
	var todo []*Recording
	for _, r := range e.Store.Recordings() {
		if r.liveTitleDue(now) {
			todo = append(todo, r)
		}
	}
	// The newest upload first: the meeting being recorded now.
	slices.SortFunc(todo, func(a, b *Recording) int { return b.Updated.Compare(a.Updated) })
	calls := 0
	for _, r := range todo {
		if calls >= max || ctx.Err() != nil {
			break
		}
		called, err := e.titleRecording(ctx, r.ID, now)
		if called {
			calls++
		}
		if err != nil {
			if deferred(ctx, err) {
				break
			}
			e.logf("recording %s: live title: %v", r.ID, err)
		}
	}
	return calls
}

// titleRecording asks for recording id's live title, read as it is now; called: the model was asked (not when it is no
// longer due). The answer is stored unless the recording was deleted, trimmed, or settled by its summary meanwhile.
func (e *Engine) titleRecording(ctx context.Context, id string, now time.Time) (called bool, err error) {
	files, meeting, hub, err := e.Store.recordSnapshot(id)
	if err != nil || files["meta.json"] == nil {
		return false, err
	}
	var r Recording
	if err := json.Unmarshal(files["meta.json"], &r); err != nil {
		return false, err
	}
	r.Conv = meeting
	if !r.liveTitleDue(now) {
		return false, nil
	}
	var device []RecordingSegment
	if err := json.Unmarshal(files["segments.json"], &device); err != nil {
		return false, err
	}
	var text []string
	for _, sg := range MeetingLines(TranscriptOfRecord(hubMode(meeting), device, hub.Segments)) {
		if t := strings.Join(strings.Fields(sg.Text), " "); t != "" {
			text = append(text, t)
		}
	}
	in := prompts.KBRecordingTitleInput{Meeting: r.Kind == RecordingMeeting || r.MinutesAsked, Start: r.StartedAt.In(e.Store.Zone()),
		Minutes: int(r.Duration / 60), Language: r.Language, Transcript: headAndTail(strings.Join(text, "\n"), liveTitleHead, liveTitleTail)}
	if r.AITitleFrom == AITitleLive {
		in.Current = r.AITitle
	}
	var out liveTitle
	cctx, cancel := context.WithTimeout(ctx, liveTitleCallTime)
	err = e.modelFor(PurposeMeetingTitle).JSONMax(cctx, prompts.RenderKBRecordingTitle(e.writingOwner(e.outputLang(&r))),
		prompts.RenderKBRecordingTitleInput(in), 300, &out)
	cancel()
	if err != nil && deferred(ctx, err) {
		return true, err // not counted: asked again once the connection is back
	}
	st := e.Store
	st.recMu.Lock()
	defer st.recMu.Unlock()
	cur, ok := st.Recording(id)
	if !ok || cur.Trimmed() {
		return true, err
	}
	cur.LiveTitles++
	cur.LiveTitleAt = now.UTC()
	if err == nil {
		cur.LiveTitleChars = r.Chars
		settled := cur.AITitleFrom == AITitleSummary || cur.AITitleFrom == AITitleMinutes
		if t := titleLine(out.Title); !settled && t != "" && !(out.Keep && cur.AITitleFrom == AITitleLive && cur.AITitle != "") {
			cur.AITitle, cur.AITitleFrom = t, AITitleLive
		}
	}
	cur.Name = e.recordingName(cur)
	if perr := st.putEntity(KindRecording, cur.ID, cur, &cur.Tagging, false); perr != nil {
		return true, perr
	}
	st.notify(KindRecording, cur.ID, "metadata")
	e.index(KindRecording, cur.ID, cur.Name, cur.Tagging, cur.OneLine, cur.StartedAt)
	return true, err
}

// headAndTail is s when it has at most head+tail runes, else its first head and last tail runes around a mark.
func headAndTail(s string, head, tail int) string {
	if utf8.RuneCountInString(s) <= head+tail {
		return s
	}
	r := []rune(s)
	return string(r[:head]) + "\n…（中间省略）…\n" + string(r[len(r)-tail:])
}
