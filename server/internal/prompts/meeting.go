package prompts

// The answers Persona gives during a meeting it attends (meeting/, docs/assistant/meeting-minutes.md "What the hub
// gives the answering model"): one model call with no tools. The system prompt is fixed; the input holds only the
// meeting material the caller built (kb.MeetingMaterial), every text of it other people's words.

// MeetingAnswer is the system prompt of an answer (meeting/answer): it takes only the owner's name, today's date in
// the user's zone and the answer's language (a BCP 47 tag), never the assistant's notes, memory or snapshot.
type MeetingAnswer struct {
	Owner, Today, Language string
}

func RenderMeetingAnswer(d MeetingAnswer) string { return render("meeting/answer", d) }

// MeetingAnswerInput is the material of one answer (meeting/answer_input). Lines are this meeting's live sentences so
// far, Asks the questions already answered in it, Earlier the earlier meetings' minutes. Question is the typed one;
// without it the question is in Heard, the lines of the 45 seconds before the tap at HeardAt. Times are clock strings
// from the recording's start; the caller puts every text on one line.
type MeetingAnswerInput struct {
	Title, Language   string
	Calendar          *MeetingCalendar
	Lines             []MeetingLine
	Asks              []MeetingQA
	Earlier           []MeetingEarlier
	Question, HeardAt string
	Heard             []MeetingLine
}

// MeetingCalendar is the phone's calendar hint of this meeting.
type MeetingCalendar struct {
	Title, Location string
	Attendees       []string
}

// MeetingLine is a live sentence and when it was said.
type MeetingLine struct{ At, Text string }

// MeetingQA is a question already answered in this meeting.
type MeetingQA struct{ Question, Answer string }

// MeetingEarlier is an earlier meeting's minutes (its id is what the answer's sources name).
type MeetingEarlier struct {
	ID, Title, When string
	Attendees       []string
	Points          []MeetingPoint
	Decisions, Open []string
	Actions         []MeetingAction
	Projects        []MeetingProject
}

// MeetingPoint is a point of earlier minutes and who made it.
type MeetingPoint struct{ Text, Who string }

// MeetingAction is an action item of earlier minutes as said; Todo is its to-do's state: open, done, suggestion (never
// made a to-do) or "" (not known).
type MeetingAction struct{ Who, What, By, DueDate, Todo string }

// MeetingProject is a project an earlier meeting belongs to: its slug (what the answer's sources name), name and status.
type MeetingProject struct{ Slug, Name, Status string }

func RenderMeetingAnswerInput(in MeetingAnswerInput) string {
	return render("meeting/answer_input", in)
}
