package assistant

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/nwverse/persona/server/internal/proto"
	"github.com/nwverse/persona/server/internal/transcript"
)

// transcriptChat is a conversation's transcript file in one of the two formats the nodes read, paged by the real
// transcript.File.PageTurns (so also by the real injected-prompt recognition) instead of the fake hub's own paging.
// The run key tests (schedule_state_set, the watcher's commit) look for the latest scheduled prompt in the last
// taskStateTurns turns of that page, which start at prompts the node counts as a user's: a scheduled prompt is not
// one, so the page reaches back past it (#8874, #8935).
type transcriptChat struct {
	t     *testing.T
	codex bool
	file  *transcript.File
	path  string
	mu    sync.Mutex
	n     int
}

func newTranscriptChat(t *testing.T, codex bool) *transcriptChat {
	t.Helper()
	c := &transcriptChat{t: t, codex: codex, path: filepath.Join(t.TempDir(), "chat.jsonl")}
	c.file = transcript.NewFile(c.path)
	if codex {
		c.write(`{"timestamp":"2026-10-28T08:42:16.027Z","ordinal":0,"type":"session_meta","payload":{"session_id":"01a1","id":"01a1","cwd":"/r/fake","originator":"traex-tui","cli_version":"0.210.1"}}`)
	}
	return c
}

func (c *transcriptChat) write(line string) {
	c.t.Helper()
	c.mu.Lock()
	defer c.mu.Unlock()
	f, err := os.OpenFile(c.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		c.t.Fatal(err)
	}
	if _, err := f.WriteString(line + "\n"); err != nil {
		c.t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		c.t.Fatal(err)
	}
	if _, err := c.file.Refresh(); err != nil {
		c.t.Fatal(err)
	}
}

func (c *transcriptChat) id(prefix string) string {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.n++
	return fmt.Sprintf("%s%d", prefix, c.n)
}

func quoteJSON(text string) string {
	b, _ := json.Marshal(text)
	return string(b)
}

// user is a message that opens a turn: the scheduled run's prompt (an injected one) or the user's own.
func (c *transcriptChat) user(text string) {
	c.t.Helper()
	id := c.id("u")
	if c.codex {
		c.write(fmt.Sprintf(`{"timestamp":"2026-10-28T08:42:48.954Z","ordinal":%s,"type":"history_mutation","payload":{"display_completions":[{"turn_id":"t","item":{"type":"UserMessage","id":%q,"content":[{"type":"text","text":%s,"text_elements":[]}]}}],"version":1,"operation":"append","items":[]}}`, id[1:], id, quoteJSON(text)))
		return
	}
	c.write(fmt.Sprintf(`{"type":"user","uuid":%q,"message":{"role":"user","content":%s}}`, id, quoteJSON(text)))
}

func (c *transcriptChat) assistant(text string) {
	c.t.Helper()
	id := c.id("a")
	if c.codex {
		c.write(fmt.Sprintf(`{"timestamp":"2026-10-28T08:43:50.000Z","ordinal":%s,"type":"history_mutation","payload":{"display_completions":[{"turn_id":"t","item":{"type":"AgentMessage","id":%q,"content":[{"type":"Text","text":%s}],"phase":"final_answer"}}]}}`, id[1:], id, quoteJSON(text)))
		return
	}
	c.write(fmt.Sprintf(`{"type":"assistant","uuid":%q,"message":{"role":"assistant","content":[{"type":"text","text":%s}]}}`, id, quoteJSON(text)))
}

// toolCall is a tool call and its result (Claude's transcript carries the result as a user line).
func (c *transcriptChat) toolCall(result string) {
	c.t.Helper()
	call, line := c.id("tu"), c.id("r")
	c.write(fmt.Sprintf(`{"type":"assistant","uuid":%q,"message":{"role":"assistant","content":[{"type":"tool_use","id":%q,"name":"Bash","input":{"command":"ls"}}]}}`, c.id("a"), call))
	c.write(fmt.Sprintf(`{"type":"user","uuid":%q,"message":{"role":"user","content":[{"type":"tool_result","tool_use_id":%q,"content":%s}]}}`, line, call, quoteJSON(result)))
}

// queued is a message the user sent while the agent worked (Claude's queued_command; Codex does not log one).
func (c *transcriptChat) queued(text string) {
	c.t.Helper()
	c.write(fmt.Sprintf(`{"type":"attachment","uuid":%q,"attachment":{"type":"queued_command","prompt":%s,"commandMode":"prompt","origin":{"kind":"human"}}}`, c.id("q"), quoteJSON(text)))
}

func (c *transcriptChat) page(turns int) []proto.Item {
	c.mu.Lock()
	defer c.mu.Unlock()
	items, _, _ := c.file.PageTurns(0, turns, 2000)
	return items
}

// scheduledPromptText is a run's prompt as the node sees it: the header prompts.ParseInjected takes for Persona's own, then the
// task's text with its run key.
func scheduledPromptText(task Task) string {
	return "⏰ patrol（定时任务，每 30 分钟）\n\nReport only new issues" + taskStatePrompt(task)
}

// endWatchers lets the run watchers a failed test left behind time out, so the bubble ends and the failure is reported
// instead of "deadlock: main bubble goroutine has exited".
func endWatchers(t *testing.T) {
	t.Cleanup(func() {
		<-time.After(runWatchTimeout + time.Hour)
		synctest.Wait()
	})
}

func chatFormats() map[string]bool { return map[string]bool{"claude": false, "codex": true} }

func stateSetArgs(key, text string) json.RawMessage {
	b, _ := json.Marshal(map[string]string{"run_key": key, "text": text})
	return b
}

// runKeyOf is the run key in a scheduled run's prompt, as the model reads it.
func runKeyOf(t *testing.T, prompt string) string {
	t.Helper()
	_, rest, ok := strings.Cut(prompt, "call schedule_state_set with run_key ")
	if !ok {
		t.Fatalf("the run's prompt has no state key: %q", prompt)
	}
	key, _, _ := strings.Cut(rest, " ")
	return key
}

// A task that keeps its conversation, four runs, and the user writing to it between runs 2 and 3, on the real page of a
// Claude and of a Codex transcript, with the real run and the real watcher: each run's own key is taken and saved (by
// the watcher, after its turn ended), and every earlier run's key is refused. The first run (a fresh chat) works too.
func TestReusedChatEveryRunSavesItsOwnState(t *testing.T) {
	for name, codex := range chatFormats() {
		t.Run(name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				chat := newTranscriptChat(t, codex)
				node := newFakeHub().withMachine()
				node.page = chat.page
				s := New(Options{Hub: node, Logf: testLogf(t)})
				s.turnPoll, s.runRetry = time.Second, time.Minute
				endWatchers(t)
				setTasks(s, Task{ID: "patrol", Title: "Patrol", Prompt: "Report only new issues", Repeat: repeatInterval,
					EveryMinutes: 30, Enabled: true})
				var keys []string
				for run := 1; run <= 4; run++ {
					if run == 3 {
						chat.user("how is it going?") // the user's own message, with the run before it as history
						chat.assistant("fine")
					}
					session, err := runNow(t, s, "patrol")
					if err != nil {
						t.Fatalf("run %d: %v", run, err)
					}
					prompt := node.lastSend(t)["text"].(string)
					key := runKeyOf(t, prompt)
					chat.user(prompt)
					node.setSessions("server", proto.Session{SessionID: session.SessionID, Status: proto.StatusWorking, State: proto.StateWorking})
					synctest.Wait()
					<-time.After(2 * s.turnPoll)
					synctest.Wait()
					chat.toolCall("a tool result in the turn")
					note := fmt.Sprintf("cursor %d", run)
					if result, err := s.runTool(t.Context(), "server", session.SessionID, "schedule_state_set", stateSetArgs(key, note)); err != nil || !strings.Contains(result, "not yet saved") {
						t.Fatalf("run %d: its own key was refused: %q, %v", run, result, err)
					}
					for i, earlier := range keys {
						if _, err := s.runTool(t.Context(), "server", session.SessionID, "schedule_state_set", stateSetArgs(earlier, "stale")); err == nil || !strings.Contains(err.Error(), "current scheduled turn") {
							t.Fatalf("run %d: the key of run %d was not refused as another turn's: %v", run, i+1, err)
						}
					}
					keys = append(keys, key)
					chat.assistant(fmt.Sprintf("New issue %d", run))
					node.setSessions("server", proto.Session{SessionID: session.SessionID, Status: proto.StatusDone, State: proto.StateIdle})
					<-time.After(3 * s.turnPoll)
					synctest.Wait()
					task, _ := taskByID(s, "patrol")
					if task.StateNote == nil || task.StateNote.Text != note || task.StateNote.Revision != uint64(run) {
						t.Fatalf("run %d: the watcher did not save its cursor: %+v", run, task.StateNote)
					}
				}
			})
		})
	}
}

// The watcher saves a staged draft only when the latest scheduled prompt in the conversation's last turns is the run's
// own (currentTaskStateRun, #8913, #8935): what the user or anything else wrote after it, before the next scheduled
// prompt, goes on with the run, so the draft is saved. Here the draft is staged (the key was right when the model
// called the tool) and is not saved when another run's scheduled prompt came after the run's, or when what carries
// the run's key is not a prompt (a tool result, the assistant's words).
func TestWatcherSavesStateOnlyWhenTheRunsPromptIsTheLatestScheduledOne(t *testing.T) {
	scheduled := scheduledPromptText
	type scenario struct {
		name  string
		build func(c *transcriptChat, own, other string) // what the chat holds when the watcher reads the ended turn
		saved bool
	}
	for _, sc := range []scenario{
		{"own prompt, the turn's tools and reply", func(c *transcriptChat, own, other string) {
			c.user(scheduled(Task{stateRunKey: own}))
			c.toolCall("tool output")
			c.assistant("done")
		}, true},
		{"fresh chat, the run's prompt and reply", func(c *transcriptChat, own, other string) {
			c.user(scheduled(Task{stateRunKey: own}))
			c.assistant("done")
		}, true},
		{"a queued message in the run's turn", func(c *transcriptChat, own, other string) {
			c.user(scheduled(Task{stateRunKey: other}))
			c.assistant("done")
			c.user(scheduled(Task{stateRunKey: own}))
			c.queued("one more thing while you work")
			c.assistant("done again")
		}, true},
		{"another run's prompt is the latest scheduled prompt", func(c *transcriptChat, own, other string) {
			c.user(scheduled(Task{stateRunKey: own}))
			c.assistant("done")
			c.user(scheduled(Task{stateRunKey: other}))
			c.assistant("done again")
		}, false},
		{"the user wrote after the run, before the next scheduled prompt", func(c *transcriptChat, own, other string) {
			c.user(scheduled(Task{stateRunKey: own}))
			c.assistant("done")
			c.user("and another thing")
			c.assistant("sure")
		}, true}, // #8913: the run goes on in a later turn until the next scheduled prompt
		{"the next scheduled prompt came after the user's message", func(c *transcriptChat, own, other string) {
			c.user(scheduled(Task{stateRunKey: own}))
			c.assistant("done")
			c.user("and another thing")
			c.assistant("sure")
			c.user(scheduled(Task{stateRunKey: other}))
			c.assistant("done again")
		}, false},
		{"a tool result carrying the key is not the run's prompt", func(c *transcriptChat, own, other string) {
			c.user(scheduled(Task{stateRunKey: other}))
			c.toolCall(scheduled(Task{stateRunKey: own}))
			c.assistant("done")
		}, false},
		{"the assistant echoing the key is not the run's prompt", func(c *transcriptChat, own, other string) {
			c.user(scheduled(Task{stateRunKey: other}))
			c.assistant(scheduled(Task{stateRunKey: own}))
		}, false},
	} {
		for format, codex := range chatFormats() {
			if codex && (strings.Contains(sc.name, "tool result") || strings.Contains(sc.name, "queued")) {
				continue // Codex logs neither a tool result nor a queued message as a user line
			}
			t.Run(format+"/"+sc.name, func(t *testing.T) {
				synctest.Test(t, func(t *testing.T) {
					chat := newTranscriptChat(t, codex)
					node := newFakeHub().withMachine()
					node.page = chat.page
					s := New(Options{Hub: node, Logf: testLogf(t)})
					s.turnPoll, s.runRetry = time.Second, time.Minute
					endWatchers(t)
					task := Task{ID: "patrol", Title: "Patrol", Prompt: "Report only new issues", Repeat: repeatInterval, EveryMinutes: 30,
						Enabled: true, MachineID: "server", LastSessionID: "s1"}
					setTasks(s, task)
					task.stateRunKey = s.openTaskStateDraft()
					other := s.openTaskStateDraft() // another run's key
					s.sched.mu.Lock()
					s.sched.stateDrafts[task.stateRunKey] = taskStateDraft{machine: "server", session: "s1", text: "staged cursor"}
					s.sched.mu.Unlock()
					sc.build(chat, task.stateRunKey, other)
					node.setSessions("server", proto.Session{SessionID: "s1", Status: proto.StatusDone, State: proto.StateIdle, Assistant: true})
					run := ScheduleRun{RefID: task.ID, Status: runOK, SessionID: "s1", MachineID: "server"}
					run.ID = s.recordRun(run)
					done := make(chan struct{})
					go func() {
						s.watchScheduledRun(run, task, Device{ID: "server", Name: "Example computer"})
						close(done)
					}()
					// The conversation was working (so the turn is the watched one), then idle.
					node.setSessions("server", proto.Session{SessionID: "s1", Status: proto.StatusWorking, State: proto.StateWorking, Assistant: true})
					<-time.After(2 * s.turnPoll)
					node.setSessions("server", proto.Session{SessionID: "s1", Status: proto.StatusDone, State: proto.StateIdle, Assistant: true})
					<-time.After(5 * s.turnPoll)
					synctest.Wait()
					select {
					case <-done:
					default:
						t.Fatal("the watcher did not end")
					}
					got, _ := taskByID(s, "patrol")
					if saved := got.StateNote != nil && got.StateNote.Text == "staged cursor"; saved != sc.saved {
						t.Fatalf("draft saved = %v, want %v (note %+v)", saved, sc.saved, got.StateNote)
					}
				})
			})
		}
	}
}

// What schedule_state_set accepts, on the real page (#8913's rule): the key of the latest scheduled prompt in the last
// turns, which what the user wrote after it, in its turn or a later one, does not end; the next scheduled prompt does.
// Text the run read or wrote itself (a tool result, the assistant's own words) is no prompt.
func TestScheduleStateSetOnTheRealPage(t *testing.T) {
	scheduled := scheduledPromptText
	for _, sc := range []struct {
		name   string
		claude bool // only Claude's transcript has it
		build  func(c *transcriptChat, own, other string)
		own    bool // the run's own key is accepted
		next   bool // the other key is the current run's (a later scheduled prompt): accepted, and the run's own is not
	}{
		{"fresh chat, the first run", false, func(c *transcriptChat, own, other string) {
			c.user(scheduled(Task{stateRunKey: own}))
		}, true, false},
		{"a queued message in the run's turn", true, func(c *transcriptChat, own, other string) {
			c.user(scheduled(Task{stateRunKey: own}))
			c.assistant("working")
			c.queued("one more thing while you work")
		}, true, false},
		{"a queued message after an earlier run", true, func(c *transcriptChat, own, other string) {
			c.user(scheduled(Task{stateRunKey: other}))
			c.assistant("done")
			c.user(scheduled(Task{stateRunKey: own}))
			c.queued("one more thing while you work")
		}, true, false},
		{"tool results in the run's turn", true, func(c *transcriptChat, own, other string) {
			c.user(scheduled(Task{stateRunKey: other}))
			c.assistant("done")
			c.user(scheduled(Task{stateRunKey: own}))
			c.toolCall("output")
			c.toolCall("more output")
		}, true, false},
		{"a manual message between the runs", false, func(c *transcriptChat, own, other string) {
			c.user(scheduled(Task{stateRunKey: other}))
			c.assistant("done")
			c.user("how is it going?")
			c.assistant("fine")
			c.user(scheduled(Task{stateRunKey: own}))
		}, true, false},
		{"the user wrote in the run's turn", false, func(c *transcriptChat, own, other string) {
			c.user(scheduled(Task{stateRunKey: own}))
			c.assistant("working")
			c.user("stop, do this instead")
		}, true, false},
		{"the user wrote in a later turn, before the next scheduled prompt", false, func(c *transcriptChat, own, other string) {
			c.user(scheduled(Task{stateRunKey: other}))
			c.assistant("done")
			c.user(scheduled(Task{stateRunKey: own}))
			c.assistant("done")
			c.user("and another thing")
			c.assistant("sure")
			c.user("one more")
		}, true, false},
		{"the next scheduled prompt came after the user's message", false, func(c *transcriptChat, own, other string) {
			c.user(scheduled(Task{stateRunKey: own}))
			c.assistant("done")
			c.user("and another thing")
			c.assistant("sure")
			c.user(scheduled(Task{stateRunKey: other}))
		}, false, true},
		{"the key only in a tool result", true, func(c *transcriptChat, own, other string) {
			c.user("what was the key?")
			c.toolCall(scheduled(Task{stateRunKey: own}))
		}, false, false},
		{"the key only in the assistant's words", false, func(c *transcriptChat, own, other string) {
			c.user("what was the key?")
			c.assistant(scheduled(Task{stateRunKey: own}))
		}, false, false},
	} {
		for format, codex := range chatFormats() {
			if sc.claude && codex {
				continue
			}
			t.Run(format+"/"+sc.name, func(t *testing.T) {
				chat := newTranscriptChat(t, codex)
				node := newFakeHub().withMachine()
				node.page = chat.page
				s := newStorelessService(t, node)
				task := Task{ID: "patrol", Repeat: repeatInterval, EveryMinutes: 30}
				task.stateRunKey = s.openTaskStateDraft()
				other := s.openTaskStateDraft() // another run's key, its draft still open: only the page can refuse it
				sc.build(chat, task.stateRunKey, other)
				_, err := s.toolScheduleStateSet(t.Context(), "server", "scheduled", stateSetArgs(task.stateRunKey, "cursor"))
				if sc.own != (err == nil) {
					t.Fatalf("the run's own key: accepted = %v, want %v (%v)", err == nil, sc.own, err)
				}
				if _, err := s.toolScheduleStateSet(t.Context(), "server", "scheduled", stateSetArgs(task.stateRunKey[:8], "prefix")); err == nil || !strings.Contains(err.Error(), "current scheduled turn") {
					t.Fatalf("a prefix of the run's key was not refused as no run's: %v", err)
				}
				_, err = s.toolScheduleStateSet(t.Context(), "server", "scheduled", stateSetArgs(other, "other"))
				if sc.next != (err == nil) {
					t.Fatalf("the other run's key: accepted = %v, want %v (%v)", err == nil, sc.next, err)
				}
				if err != nil && !strings.Contains(err.Error(), "current scheduled turn") {
					t.Fatalf("the other run's key was not refused as another turn's: %v", err)
				}
			})
		}
	}
}

// lastTurns, which the fake hub pages with, stops where the real PageTurns does: at the last prompt the node counts, so a
// reused scheduled chat's one-turn page holds the earlier runs too.
func TestLastTurnsPagesLikeTheNode(t *testing.T) {
	for format, codex := range chatFormats() {
		t.Run(format, func(t *testing.T) {
			chat := newTranscriptChat(t, codex)
			chat.user("question one")
			chat.assistant("a1")
			for run := 1; run <= 3; run++ {
				chat.user(fmt.Sprintf("⏰ patrol（定时任务，每 30 分钟）\n\nrun %d", run))
				chat.assistant("report")
			}
			chat.user("question two")
			chat.assistant("a2")
			chat.user("⏰ patrol（定时任务，每 30 分钟）\n\nrun 4")
			chat.assistant("report")
			all := chat.page(100)
			for turns := 1; turns <= 3; turns++ {
				real, fake := chat.page(turns), lastTurns(all, turns)
				if len(real) != len(fake) || real[0].ID != fake[0].ID {
					t.Fatalf("%d turns: the node's page has %d items from %q, the fake's %d from %q", turns, len(real), real[0].Text, len(fake), fake[0].Text)
				}
			}
			if page := chat.page(1); page[0].Text != "question two" || len(page) != 4 {
				t.Fatalf("the one-turn page starts at the last prompt the user wrote: %d items from %q", len(page), page[0].Text)
			}
		})
	}
}

// The page the run key is looked for in holds taskStateTurns user turns, counted from the end, and its start is the
// fourth counted prompt (a scheduled prompt is not one). So the run's own prompt with 3 user messages after it is
// on the page, and with 4 it is not: its own key is refused, however current it is. This documents the known
// fail-closed gap of #8935 on the real PageTurns, and it pins today's boundary, not a contract: update it when the
// window (taskStateTurns) changes.
func TestScheduleStateSetWindowBoundaryDocumentsTheKnownGap(t *testing.T) {
	for format, codex := range chatFormats() {
		for _, after := range []int{3, 4} {
			t.Run(fmt.Sprintf("%s/%d user messages after the run's prompt", format, after), func(t *testing.T) {
				chat := newTranscriptChat(t, codex)
				node := newFakeHub().withMachine()
				node.page = chat.page
				s := newStorelessService(t, node)
				task := Task{ID: "patrol", Repeat: repeatInterval, EveryMinutes: 30}
				task.stateRunKey = s.openTaskStateDraft()
				chat.user(scheduledPromptText(task))
				chat.assistant("done")
				for i := 1; i <= after; i++ {
					chat.user(fmt.Sprintf("message %d", i))
					chat.assistant("reply")
				}
				_, err := s.toolScheduleStateSet(t.Context(), "server", "scheduled", stateSetArgs(task.stateRunKey, "cursor"))
				if accepted := err == nil; accepted != (after < 4) {
					t.Fatalf("the run's own key with %d user messages after its prompt: accepted = %v, want %v (%v); the window changed, update this test", after, accepted, after < 4, err)
				}
				if err != nil && !strings.Contains(err.Error(), "current scheduled turn") {
					t.Fatalf("refused for another reason: %v", err)
				}
			})
		}
	}
}
