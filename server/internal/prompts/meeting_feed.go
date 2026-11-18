package prompts

// MeetingFeed is one batch of a recorded meeting for the session that attends it (meeting/feed,
// docs/assistant/meeting-minutes.md "Feeding the attending session"): a short line saying what it is, then the live
// lines and the questions answered on the phone since the last batch in a context block. Every text goes in through
// ctx, so a line someone said cannot close the block; Title is the meeting's name on one line.
type MeetingFeed struct {
	Lang  string // the user's language: en, ja, else zh-Hans
	Title string
	Role  string // notes, listener or participant
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
	// Room: the batch goes to the meeting's own room conversation (docs/assistant/meeting-minutes.md "The room
	// conversation"), which writes nothing after it.
	Room bool
	// Topics: the room has meeting_topics_set (phase 2): the batch shows it TopicsSoFar (none yet when empty) and asks
	// it to update them, only when the meeting moved on, before it writes nothing.
	Topics      bool
	TopicsSoFar []MeetingTopic
	// Highlights: the room has meeting_highlights_set (phase 2b): the batch shows it HighlightsSoFar after the topics
	// (none yet when empty) and asks it to update them beside the topics, only when the batch changed what matters most.
	Highlights      bool
	HighlightsSoFar []MeetingHighlight
	// Lean: a room started with lean batches (docs/assistant/meeting-minutes.md "Long meetings: lean batches and
	// compaction (#8253)"), which has both tools and their rules in its CLAUDE.md: one short line asks it to update the
	// topics and highlights as its rules say instead of restating them, and the lists so far are shown only with Lists
	// (at the boundaries). Without Lean the text is as before it.
	Lean, Lists bool
}

// MeetingHighlight is one of a meeting's highlights as its room set them: when it was made (in a batch the clock time
// of its minute, HH:MM in the user's zone as the batch writes its lines; in the follow-up's seed the time from the
// start, m:ss, as its transcript's) and its text on one line.
type MeetingHighlight struct{ At, Text string }

// MeetingTopic is one of a meeting's topics as its room set them: the state (done, current, open), the title, when
// it began from the meeting's start ("" when not said) and the room's note ("" when none).
type MeetingTopic struct{ State, Title, At, Note string }

// MeetingFeedLine is one live sentence and when it was said (HH:MM).
type MeetingFeedLine struct{ At, Text string }

// MeetingFeedAsk is a question asked on the phone during the meeting and its answer.
type MeetingFeedAsk struct{ Question, Answer string }

func RenderMeetingFeed(d MeetingFeed) string { return render("meeting/feed", d) }
