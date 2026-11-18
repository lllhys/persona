package assistant

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/nwverse/persona/server/internal/assistantwire"
	"github.com/nwverse/persona/server/internal/prompts"
	"github.com/nwverse/persona/server/internal/proto"
	"github.com/nwverse/persona/server/internal/transcript"
)

// A scheduled task's "Push when done" (#2396, docs/assistant/assistant.md "Push after running"): each run is a turn in a conversation, and the
// hub pushes every finished turn (hub/machine.go notifyFinished). Right for a morning briefing, wrong for a task that watches
// something ("push me when there is new mail"): every run pushed "task name · no new mail". A task's push is then
//
//   - pushEvery (""): every run's answer is pushed, as before;
//   - pushAsked: only what the run pushes itself (notify_user); a run with nothing new is not pushed at all.
//
// Such a task runs in its own conversation unless it goes on in one it came from (SessionID), and never in the main
// one (isolateScheduledConversation). In its own conversation, that conversation's finished turns are not pushed when they
// end normally: the hub's finished push (quietTaskFinish in fleet_push.go, by the tasks of the last snapshot) and the
// channels' mirror (quietTaskTurn) leave them out. A turn the user starts in that conversation is not pushed either:
// the price of judging by the conversation, not by the turn — which, across queued messages, background turns and
// compactions, the node can not tell for sure (three rounds of review on #2396). In a conversation it came from (a
// leader's patrol, #3673), the turn's own items decide (quietScheduledInput): the input's persisted MessageSource.Push,
// and nothing of the user's absorbed after it. Errors, cut-offs and a stuck turn are told as ever, and so is notify_user.
const (
	pushEvery = ""
	pushAsked = assistantwire.PushAsked
)

// pushesOnlyAsked: the task's runs push only what they ask for (notify_user).
func (t Task) pushesOnlyAsked() bool { return t.Push == pushAsked }

// checkPush says whether p is a push the app may save.
func checkPush(p string) error {
	switch p {
	case pushEvery, pushAsked:
		return nil
	}
	return fmt.Errorf("跑完推送 %q 不认识（\"\" 每次 / \"asked\" 只在它自己提醒时）", p)
}

// pushArg is a scheduling tool's push argument: "every" (or empty) or "asked".
func pushArg(a string) (string, error) {
	switch a {
	case "", "every":
		return pushEvery, nil
	case pushAsked:
		return pushAsked, nil
	}
	return "", fmt.Errorf("push %q: use \"every\" (each run's answer is pushed) or \"asked\" (only what the run pushes with notify_user)", a)
}

// pushText is how list_followups and the tools' answers say a task's push.
func pushText(t Task) string {
	if t.pushesOnlyAsked() && t.SessionID != "" {
		return "pushes only what a run sends with notify_user (nothing new: no push); it runs in the conversation it came from"
	}
	if t.pushesOnlyAsked() {
		return "pushes only what a run sends with notify_user (nothing new: no push); it runs in its own conversation"
	}
	return "each run's answer is pushed to the user"
}

// pushIn reads push off a request body: has says it was there. null or anything but a string is refused (400), not
// taken for "every".
func pushIn(raw map[string]json.RawMessage) (push string, has bool, err error) {
	v, ok := raw["push"]
	if !ok {
		return "", false, nil
	}
	if bytes.Equal(bytes.TrimSpace(v), []byte("null")) {
		return "", true, errors.New(`跑完推送不能是 null（"" 每次 / "asked" 只在它自己提醒时）`)
	}
	if err := json.Unmarshal(v, &push); err != nil {
		return "", true, fmt.Errorf("跑完推送要是字符串：%w", err)
	}
	return push, true, checkPush(push)
}

// isolateScheduledConversation keeps quiet runs and recurring runs out of the main chat.
// A quiet run needs its own delivery policy, and a recurring run keeps its own history.
func isolateScheduledConversation(t *Task, mayBeMain func(string) bool) {
	if t.SessionID != "" && (t.pushesOnlyAsked() || t.Repeat != repeatOnce) && mayBeMain(t.SessionID) {
		t.SessionID = ""
	}
}

// quietConversation: session is the own conversation of a task that pushes only what its runs send. Another task
// does not run into it (runTask, toolScheduleFollowup): its turns would go unpushed there.
func (s *Service) quietConversation(ctx context.Context, session string) bool {
	if session == "" {
		return false
	}
	sc, err := s.lockedSchedule(ctx)
	if err != nil {
		return false // nothing known
	}
	defer sc.mu.Unlock()
	for _, t := range sc.tasks {
		if t.pushesOnlyAsked() && t.RunSessionID == session {
			return true
		}
	}
	return false
}

// quietTaskTurn: session is a quiet task's own conversation whose turn may go unpushed now
// (assistantwire.QuietTaskFinish): the channels' mirror leaves it out, as the hub's finished push does.
func (s *Service) quietTaskTurn(ctx context.Context, machine, session string) bool {
	if s.meetingTurnNow(machine, session) { // a meeting's batch started it: a side exchange, mirrored nowhere (meeting_turns.go)
		return true
	}
	if s.supervising(session) { // a supervision message started its turn (supervise_digest.go): as the hub's finished push leaves it out
		sess, ok := s.session(ctx, machine, session)
		return ok && assistantwire.SupervisedFinish(sess, true)
	}
	sess, ok := s.session(ctx, machine, session)
	if ok && s.quietScheduledInput(ctx, machine, sess) {
		return true
	}
	if !s.quietConversation(ctx, session) {
		return false
	}
	main, known := s.knownMain(ctx)
	if !known {
		return false // which conversation is the main one is not known: mirrored as ever
	}
	return ok && assistantwire.QuietTaskFinish(sess, main)
}

// quietScheduledInput: sess's turn that just ended normally was started by an asked scheduled input and carries
// nothing of the user's (quietTurnItems). The mode lives in the native transcript, so manager or node restarts need no
// in-memory conversation hold. Only conversations an asked task posts into are read.
func (s *Service) quietScheduledInput(ctx context.Context, machine string, sess proto.Session) bool {
	if !sess.Assistant || !s.askedTaskPostsInto(ctx, sess.SessionID) {
		return false
	}
	main, known := s.knownMain(ctx)
	if !known || !assistantwire.QuietTaskFinish(sess, main) {
		return false
	}
	items, read := s.lastTurnItems(ctx, machine, sess.SessionID)
	if !read || !quietTurnItems(items) {
		return false // an old or unavailable node cannot prove this answer belongs to a quiet input
	}
	// A completion callback may run after another turn started: the items read must be of the session the caller
	// judges. Compared by revision, not by LastText, which the node collapses and cuts to a snippet.
	current, ok := s.session(ctx, machine, sess.SessionID)
	return ok && current.Status == sess.Status && current.Rev == sess.Rev && current.UpdatedAt.Equal(sess.UpdatedAt)
}

// askedTaskPostsInto: an asked task runs in session, as the conversation it came from or its own one.
func (s *Service) askedTaskPostsInto(ctx context.Context, session string) bool {
	if session == "" {
		return false
	}
	sc, err := s.lockedSchedule(ctx)
	if err != nil {
		return false
	}
	defer sc.mu.Unlock()
	for _, t := range sc.tasks {
		if t.pushesOnlyAsked() && (t.SessionID == session || t.RunSessionID == session) {
			return true
		}
	}
	return false
}

// quietTurnItems: a turn's items (the node's last turn, from the input that started it) say its answer is only the
// asked scheduled run's. The turn's input is its last user text that is neither queued nor a wake, and the run has
// answered after it (a patrol input written but not yet answered leaves the answer above it, the previous turn's).
// Answered is text after the input, or tool calls after it followed by the turn's end (turn_duration): a patrol that
// only calls tools, notify_user among them, ends on its own work (#3934). A turn with no output at all is still
// pushed, as an empty turn may be a failure. After the input:
//   - user input absorbed mid-turn (queued text or image without a scheduled source) makes the answer the user's too;
//   - a wake by a background task this turn did not start (one the user started earlier, a loop, a peer) is
//     the user's result, not the patrol's. A wake by a task the turn launched stays the patrol's.
func quietTurnItems(items []proto.Item) bool {
	start := -1
	for i := len(items) - 1; i >= 0; i-- {
		if it := items[i]; it.Role == "user" && it.Kind == proto.KindText && !it.Queued && !it.Wake && strings.TrimSpace(it.Text) != "" {
			start = i
			break
		}
	}
	if start < 0 || !askedScheduledSource(prompts.ItemSource(items[start])) {
		return false
	}
	answered, worked := false, false
	for i := start + 1; i < len(items); i++ {
		it := items[i]
		switch {
		case it.Wake:
			if !launchedIn(items[start+1:i], it.Task) {
				return false
			}
		case it.Role == "user" && it.Queued && (it.Kind == proto.KindImage || it.Kind == proto.KindText && strings.TrimSpace(it.Text) != ""):
			if !askedScheduledSource(prompts.ItemSource(it)) {
				return false
			}
		case it.Role == "assistant" && it.Kind == proto.KindText && strings.TrimSpace(it.Text) != "":
			answered = true
		case it.Role == "assistant" && it.Kind == proto.KindToolUse:
			worked = true
		case it.Kind == turnDurationKind && worked:
			answered = true
		}
	}
	return answered
}

// turnDurationKind is the item kind of a turn's end as the transcript readers write it (transcript.go and codex.go:
// Claude Code's system/turn_duration line, Codex's completed turn).
const turnDurationKind = "turn_duration"

// askedScheduledSource: source is a scheduled run's that pushes only what it sends.
func askedScheduledSource(source *proto.MessageSource) bool {
	return source != nil && (source.Kind == proto.SourcePatrol || source.Kind == proto.SourceFollowup) && source.Push == pushAsked
}

// launchedIn: the background task note reports on was launched by a call among items. A note with its call's id is
// matched by that id alone: another call's result may mention the task (a status check of the user's task). A note
// without one falls back to its task id, matched whole against the id a call's result says that call started
// (transcript.LaunchedBackgroundID). No note (a loop firing, a peer's message) was launched by none of them.
func launchedIn(items []proto.Item, note *proto.TaskNote) bool {
	if note == nil {
		return false
	}
	for _, it := range items {
		switch {
		case note.ToolUseID != "":
			if (it.Kind == proto.KindToolUse || it.Kind == proto.KindToolResult) && it.ToolUseID == note.ToolUseID {
				return true
			}
		case note.TaskID != "":
			if it.Kind == proto.KindToolResult && transcript.LaunchedBackgroundID(it.Text) == note.TaskID {
				return true
			}
		}
	}
	return false
}

// knownMain is the main conversation's id, and whether the settings could be read to say: a read that failed is not
// "no main conversation" (savedSettings' zero value is that).
func (s *Service) knownMain(ctx context.Context) (string, bool) {
	v, ok := s.savedSettings(ctx)
	if !ok {
		return "", false
	}
	return s.effective(ctx, v).Main, true
}

// mayBeMain: session is the main conversation, or the settings could not be read to tell. A quiet task does not go on
// in such a conversation: the main one is never quiet.
func (s *Service) mayBeMain(ctx context.Context, session string) bool {
	return s.mayBeMainNow(ctx)(session)
}

// mayBeMainNow is mayBeMain with the settings read once, for a caller about to hold the schedule's lock.
func (s *Service) mayBeMainNow(ctx context.Context) func(string) bool {
	main, known := s.knownMain(ctx)
	return func(session string) bool { return !known || session == main }
}
