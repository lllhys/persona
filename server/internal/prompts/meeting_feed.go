package prompts

// MeetingFeed is one batch of a recorded meeting for the session that attends it (meeting/feed,
// docs/assistant/meeting-minutes.md "Feeding the attending session"): a short line saying what it is, then the live
// lines and the questions answered on the phone since the last batch in a context block. Every text goes in through
// ctx, so a line someone said cannot close the block; Title is the meeting's name on one line.
type MeetingFeed struct {
	Lang  string // the user's language: en, ja, else zh-Hans
	Title string
	Role  string // listener or participant
	// From and To are the wall-clock times (HH:MM, the user's zone) of the first and the last line: From "" when the
	// batch has no lines (only answers, or only the end), To "" when it is From.
	From, To string
	// SoFar: the meeting so far, for a session that was not the one fed before; LeftOut is how many earlier lines did
	// not fit the batch.
	SoFar   bool
	LeftOut int
	Lines   []MeetingFeedLine
	Asks    []MeetingFeedAsk
	Ended   bool // the end batch: the meeting is over
}

// MeetingFeedLine is one live sentence and when it was said (HH:MM).
type MeetingFeedLine struct{ At, Text string }

// MeetingFeedAsk is a question asked on the phone during the meeting and its answer.
type MeetingFeedAsk struct{ Question, Answer string }

func RenderMeetingFeed(d MeetingFeed) string { return render("meeting/feed", d) }
