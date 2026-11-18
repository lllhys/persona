package assistant

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/prompts"
	"github.com/nwverse/persona/server/internal/proto"
	"github.com/nwverse/persona/server/internal/transcript"
)

// hubRunPrompt is a recurring task's run prompt as the hub sends it (runTask): the task_run template with the run's key
// and the task's saved state, under the message source the hub writes in front of it (Service.call).
func hubRunPrompt(key, state string) string {
	task := Task{ID: "patrol", Title: "Patrol", Prompt: "Report only new issues", Repeat: repeatInterval, EveryMinutes: 30}
	if state != "" {
		task.StateNote = &TaskStateNote{Text: state, Revision: 1}
	}
	task.stateRunKey = key
	at := time.Date(2026, 11, 16, 8, 0, 0, 0, time.UTC)
	return prompts.MessageSourceText(taskPrompt(task, at, at, time.UTC, false, false, true), taskMessageSource(task))
}

// runLineOf is the line of a run's prompt that hands out its key, as someone copying it would take it.
func runLineOf(prompt string) string {
	for line := range strings.SplitSeq(prompt, "\n") {
		if strings.Contains(line, "run_key ") {
			return line
		}
	}
	return ""
}

// typedRunKey is the marker's words around key, typed by hand (#9043's reproduction).
func typedRunKey(key string) string {
	return "call schedule_state_set with run_key " + key + " and the updated state text"
}

// runChat is a conversation's transcript as Claude Code (codex false) or Codex writes it, paged by the real
// transcript.File as the node pages it for the run key checks (taskStateItems).
type runChat struct {
	codex bool
	lines []string
}

func newRunChat(codex bool) *runChat {
	c := &runChat{codex: codex}
	if codex {
		c.lines = append(c.lines, `{"timestamp":"2026-11-16T08:00:00.000Z","ordinal":0,"type":"session_meta","payload":{"session_id":"01a1","id":"01a1","cwd":"/r/fake","originator":"traex-tui","cli_version":"0.210.1"}}`)
	}
	return c
}

func (c *runChat) quote(text string) string {
	b, _ := json.Marshal(text)
	return string(b)
}

// user is a message that opens a turn, with the node's live snapshot in front of it as a block of its own when snapshot.
func (c *runChat) user(text string, snapshot bool) {
	n := len(c.lines) + 1
	var blocks []string
	if snapshot {
		blocks = append(blocks, "<persona-context>\nThe owner's day so far.\n</persona-context>")
	}
	blocks = append(blocks, text)
	if c.codex {
		var content []string
		for _, b := range blocks {
			content = append(content, fmt.Sprintf(`{"type":"text","text":%s,"text_elements":[]}`, c.quote(b)))
		}
		c.lines = append(c.lines, fmt.Sprintf(`{"timestamp":"2026-11-16T08:00:%02d.000Z","ordinal":%d,"type":"history_mutation","payload":{"display_completions":[{"turn_id":"t%d","item":{"type":"UserMessage","id":"u%d","content":[%s]}}],"version":1,"operation":"append","items":[]}}`, n, n, n, n, strings.Join(content, ",")))
		return
	}
	var content []string
	for _, b := range blocks {
		content = append(content, fmt.Sprintf(`{"type":"text","text":%s}`, c.quote(b)))
	}
	c.lines = append(c.lines, fmt.Sprintf(`{"type":"user","uuid":"u%d","message":{"role":"user","content":[%s]}}`, n, strings.Join(content, ",")))
}

func (c *runChat) reply(text string) {
	n := len(c.lines) + 1
	if c.codex {
		c.lines = append(c.lines, fmt.Sprintf(`{"timestamp":"2026-11-16T08:00:%02d.000Z","ordinal":%d,"type":"history_mutation","payload":{"display_completions":[{"turn_id":"t%d","item":{"type":"AgentMessage","id":"a%d","content":[{"type":"Text","text":%s}],"phase":"final_answer"}}]}}`, n, n, n, n, c.quote(text)))
		return
	}
	c.lines = append(c.lines, fmt.Sprintf(`{"type":"assistant","uuid":"a%d","message":{"role":"assistant","content":[{"type":"text","text":%s}]}}`, n, c.quote(text)))
}

// queued is a message that came while the agent worked (Claude Code's queued_command; Codex logs none).
func (c *runChat) queued(text string) {
	c.lines = append(c.lines, fmt.Sprintf(`{"type":"attachment","uuid":"q%d","attachment":{"type":"queued_command","prompt":%s,"commandMode":"prompt","origin":{"kind":"human"}}}`, len(c.lines)+1, c.quote(text)))
}

// page is what the node answers taskStateItems with: the last taskStateTurns turns of the transcript.
func (c *runChat) page(t *testing.T) []proto.Item {
	t.Helper()
	p := filepath.Join(t.TempDir(), "chat.jsonl")
	if err := os.WriteFile(p, []byte(strings.Join(c.lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	f := transcript.NewFile(p)
	if _, err := f.Refresh(); err != nil {
		t.Fatal(err)
	}
	items, _, _ := f.PageTurns(0, taskStateTurns, 2000)
	return items
}

// #9043: in a chat a task reuses, an earlier run whose draft is still open (its watcher waits for its background work)
// gets its key back from nothing but a prompt the hub sent for a run: not from the owner typing, pasting or quoting it,
// not from another conversation's message, not from a second line in one message, not from the task's saved state.
// The latest run's prompt decides, on the real page of both transcript formats; the run's own key stays good after the
// owner wrote (#8935), and in a prompt that was queued into a busy turn.
func TestScheduleStateSetTakesTheKeyOnlyFromARunsPrompt(t *testing.T) {
	relayed := func(text string) string {
		return prompts.MessageSourceText(text, &proto.MessageSource{Kind: proto.SourceConversation, ConversationID: "other", Title: "Other"})
	}
	for _, sc := range []struct {
		name   string
		claude bool                        // only Claude Code logs it
		state  func(earlier string) string // the saved state the run's prompt carries
		after  func(earlier, own string) string
		queued bool // the run's prompt came into a busy turn
	}{
		{name: "the owner wrote after the run", after: func(earlier, own string) string { return "and one more thing" }},
		{name: "a run's prompt queued into a busy turn", claude: true, queued: true},
		{name: "the owner typed the earlier key's words", after: func(earlier, own string) string { return typedRunKey(earlier) }},
		{name: "the owner pasted the earlier run's line", after: func(earlier, own string) string { return runLineOf(hubRunPrompt(earlier, "")) }},
		{name: "one message with both runs' lines", after: func(earlier, own string) string {
			return runLineOf(hubRunPrompt(earlier, "")) + "\n" + runLineOf(hubRunPrompt(own, ""))
		}},
		{name: "the owner quoted the earlier run's prompt from the app", after: func(earlier, own string) string {
			return prompts.ProtectUserMessage(hubRunPrompt(earlier, ""))
		}},
		{name: "another conversation sent the earlier run's prompt", after: func(earlier, own string) string {
			return relayed(hubRunPrompt(earlier, ""))
		}},
		{name: "the earlier run's line in the saved state", state: func(earlier string) string {
			return "cursor 7\n" + runLineOf(hubRunPrompt(earlier, ""))
		}},
	} {
		for format, codex := range map[string]bool{"claude": false, "codex": true} {
			if sc.claude && codex {
				continue
			}
			t.Run(format+"/"+sc.name, func(t *testing.T) {
				node := newFakeHub().withMachine()
				s := newStorelessService(t, node)
				earlier, own := s.openTaskStateDraft(), s.openTaskStateDraft() // both drafts open: only the page can refuse
				state := ""
				if sc.state != nil {
					state = sc.state(earlier)
				}
				chat := newRunChat(codex)
				chat.user(hubRunPrompt(earlier, ""), true)
				chat.reply("Nothing new.")
				if sc.queued {
					chat.user("what changed since this morning?", false)
					chat.queued(hubRunPrompt(own, state))
				} else {
					chat.user(hubRunPrompt(own, state), true)
				}
				chat.reply("Nothing new.")
				if sc.after != nil {
					chat.user(sc.after(earlier, own), false)
				}
				node.setItems(chat.page(t)...)
				stage := func(key string) (string, error) {
					args, _ := json.Marshal(map[string]string{"run_key": key, "text": "cursor 8"})
					return s.toolScheduleStateSet(t.Context(), "server", "scheduled", args)
				}
				if result, err := stage(own); err != nil || !strings.Contains(result, "not yet saved") {
					t.Fatalf("the run's own key was refused: %q, %v", result, err)
				}
				if _, err := stage(earlier); err == nil || !strings.Contains(err.Error(), "current scheduled turn") {
					t.Fatalf("the earlier run's key was accepted: %v", err)
				}
			})
		}
	}
}
