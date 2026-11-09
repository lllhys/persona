package assistant

import (
	"context"
	"errors"
	"slices"
	"strings"
	"time"

	"github.com/nwverse/persona/server/internal/ai"
	"github.com/nwverse/persona/server/internal/db"
	"github.com/nwverse/persona/server/internal/i18n"
	"github.com/nwverse/persona/server/internal/kb"
	"github.com/nwverse/persona/server/internal/prompts"
)

// What a finished task came to (#2820, docs/assistant/todos.md "Record the outcome when done"). When a task the main conversation opened (its todo
// has an origin) is done, closed by the judge (done.go) or by the user, its agents' last replies are read and a model
// pulls out a one-line outcome, its links, the decisions made and the pitfalls hit. They go in the todo's decisions
// (source "outcome", once per completion) and in the timelines of the projects the agents worked in («to-do · <title>», in Chinese),
// where the next agent started for the project finds them (projectContext). Without a model the outcome is the
// receipt's text, without decisions or pitfalls.

const (
	outcomeReadTimeout  = time.Minute      // reading the agents' last replies
	outcomeModelTimeout = 90 * time.Second // the model's answer
	outcomeWriteTimeout = 45 * time.Second // the decisions and the timelines, on a context of their own
	outcomeKeep         = 6 * time.Hour    // how long a completion is remembered as recorded (the store has it after)
	outcomeReplyMax     = 3000             // runes of each agent's last reply the model reads
	outcomeTextMax      = 120              // runes of the outcome line
	outcomeItemsMost    = 5                // decisions, and pitfalls, kept of one completion
	outcomeLinksMost    = 3
	outcomeLogLinks     = 2   // links on the timeline line
	outcomeLogMax       = 200 // runes of a timeline line (kb's oneLine cuts there)
)

// taskOutcome is what the model pulls out of a finished task's last replies (todo/outcome).
type taskOutcome struct {
	Outcome   string   `json:"outcome"`
	Links     []string `json:"links"`
	Decisions []struct {
		Question string `json:"question"`
		Choice   string `json:"choice"`
		Reason   string `json:"reason"`
	} `json:"decisions"`
	Pitfalls []string `json:"pitfalls"`
}

// outcomeAgent is one of the task's agents as its outcome is written: where it worked and what it said last.
type outcomeAgent struct {
	MachineID, Machine, SessionID, Cwd, Text string
}

// outcomeKey names one completion of a todo: the decisions it writes carry it (their source_message), so a
// completion is recorded once, and a later one (reopened, done again) gets its own.
func outcomeKey(todoID string, doneAt time.Time) string {
	return "outcome:" + todoID + ":" + doneAt.UTC().Format(time.RFC3339Nano)
}

// recordTaskOutcome writes what task t came to; doneAt is when it was marked done. t is the todo as it was then, with
// its agents. Nothing for a todo that is not a task (no origin), or one already recorded for this completion.
func (s *Service) recordTaskOutcome(t Todo, doneAt time.Time) {
	if t.Origin == "" {
		return
	}
	key := outcomeKey(t.ID, doneAt)
	now := time.Now()
	s.tasks.mu.Lock()
	if s.tasks.outcomes == nil {
		s.tasks.outcomes = map[string]time.Time{}
	}
	for k, at := range s.tasks.outcomes {
		if now.Sub(at) > outcomeKeep {
			delete(s.tasks.outcomes, k)
		}
	}
	if _, ok := s.tasks.outcomes[key]; ok {
		s.tasks.mu.Unlock()
		return
	}
	s.tasks.outcomes[key] = now
	s.tasks.mu.Unlock()
	rctx, cancel := context.WithTimeout(context.Background(), outcomeReadTimeout)
	defer cancel()
	if s.outcomeRecorded(rctx, t.ID, key) {
		return // another assistant on the same store got there first
	}
	agents := s.outcomeAgents(rctx, t)
	if len(agents) == 0 {
		s.logf("task %s: outcome: no agent reply to read", t.ID)
		return
	}
	mctx, mcancel := context.WithTimeout(context.Background(), outcomeModelTimeout)
	defer mcancel()
	out, err := s.pullOutcome(mctx, t, agents)
	if err != nil {
		if !ai.IsPaused(err) {
			s.logf("task %s: outcome: %v (the receipt's text instead)", t.ID, err)
		}
		out = taskOutcome{Outcome: receiptOutcome(agents), Links: replyLinks(agents)}
	}
	out = tidyOutcome(out)
	if out.Outcome == "" {
		out.Outcome = receiptOutcome(agents)
	}
	if out.Outcome == "" {
		return
	}
	// The writes get a context of their own: a slow model must not leave even the fallback unwritten.
	ctx, wcancel := context.WithTimeout(context.Background(), outcomeWriteTimeout)
	defer wcancel()
	if !s.outcomeDecisions(ctx, t, key, agents[0], out) {
		return // recorded by another assistant on the same store, or the todo is gone
	}
	s.outcomeLog(ctx, t, agents, out)
}

// goRecordTaskOutcome records task t's outcome in the background (recordTaskOutcome), counted in tasks.outcomeRuns.
func (s *Service) goRecordTaskOutcome(t Todo, doneAt time.Time) {
	s.tasks.outcomeRuns.Add(1)
	go func() {
		defer s.tasks.outcomeRuns.Done()
		s.recordTaskOutcome(t, doneAt)
		s.sourceReportNow(t.ID) // the outcome is as written as it will be: tell where the request came from (todo_source.go)
	}()
}

// waitTaskOutcomes returns once the outcome recordings started so far have finished: what checks that a completion
// recorded no outcome needs not wait out a deadline. For tests only: the caller must know nothing else is finishing
// a task meanwhile, as a WaitGroup's Add from zero must not race its Wait.
func (s *Service) waitTaskOutcomes() { s.tasks.outcomeRuns.Wait() }

// outcomeRecorded: the store already has decisions of this completion.
func (s *Service) outcomeRecorded(ctx context.Context, todoID, key string) bool {
	st := s.todoDB()
	if st == nil {
		return false
	}
	list, err := st.ListTodoDecisions(ctx, todoID, true)
	if err != nil {
		return false
	}
	return slices.ContainsFunc(list, func(d db.TodoDecisionRow) bool {
		return d.Source == db.DecisionOutcome && strings.HasPrefix(d.SourceMessage, key)
	})
}

// outcomeAgents are t's agents that said something last, the most recently linked first: their whole last reply
// from the transcript, else the session list's clipped last_text.
func (s *Service) outcomeAgents(ctx context.Context, t Todo) []outcomeAgent {
	var out []outcomeAgent
	for i := len(t.Agents) - 1; i >= 0; i-- {
		a := t.Agents[i]
		if a.ReleasedAt != nil || a.DismissedAt != nil {
			continue
		}
		name := a.MachineID
		if m, ok := s.device(ctx, a.MachineID); ok {
			name = m.Name
		}
		_, text := s.lastTurn(ctx, a.MachineID, a.SessionID)
		cwd := a.Cwd
		if c, ok := s.session(ctx, a.MachineID, a.SessionID); ok {
			if text == "" {
				text = strings.TrimSpace(c.LastText)
			}
			if cwd == "" {
				cwd = c.Cwd
			}
		}
		if text = strings.TrimSpace(text); text == "" || cutOff(text) {
			continue
		}
		out = append(out, outcomeAgent{MachineID: a.MachineID, Machine: name, SessionID: a.SessionID, Cwd: cwd, Text: text})
	}
	return out
}

// pullOutcome asks the model what the task came to (the kb.project purpose: it writes the projects' timelines).
func (s *Service) pullOutcome(ctx context.Context, t Todo, agents []outcomeAgent) (taskOutcome, error) {
	in := prompts.TodoOutcomeInput{Title: t.Title, Prompt: clipMiddle(strings.TrimSpace(t.Notes), 1500)}
	for _, a := range agents {
		in.Replies = append(in.Replies, prompts.TodoOutcomeReply{Machine: a.Machine, Cwd: a.Cwd, Text: clipMiddle(a.Text, outcomeReplyMax)})
	}
	var out taskOutcome
	err := s.purposeModel("kb.project").JSONMax(ctx, prompts.RenderTodoOutcome(prompts.TodoOutcome{Owner: s.owner(ctx)}),
		prompts.RenderTodoOutcomeInput(in), 2048, &out)
	return out, err
}

// receiptOutcome is the outcome without a model: the newest reply's text as the receipt shows it, on one line.
func receiptOutcome(agents []outcomeAgent) string {
	if len(agents) == 0 {
		return ""
	}
	return clip(oneLine(clipMarkdown(agents[0].Text, 600)), outcomeTextMax)
}

// replyLinks are the pull requests the replies name, the newest last (as the receipt picks its PR).
func replyLinks(agents []outcomeAgent) []string {
	var out []string
	for i := len(agents) - 1; i >= 0; i-- {
		out = append(out, prLink.FindAllString(agents[i].Text, -1)...)
	}
	return out
}

// tidyOutcome trims what the model said: one-line texts within bounds, links that are links, no empty items, no
// repeats, at most outcomeItemsMost decisions and pitfalls.
func tidyOutcome(o taskOutcome) taskOutcome {
	out := taskOutcome{Outcome: clip(oneLine(o.Outcome), outcomeTextMax)}
	for _, l := range o.Links {
		l = strings.TrimSpace(l)
		if (strings.HasPrefix(l, "https://") || strings.HasPrefix(l, "http://")) && !strings.ContainsAny(l, " \t\n") &&
			!slices.Contains(out.Links, l) && len(out.Links) < outcomeLinksMost {
			out.Links = append(out.Links, clip(l, 300))
		}
	}
	for _, d := range o.Decisions {
		q, c := decisionText(oneLine(d.Question)), decisionText(oneLine(d.Choice))
		if q == "" || c == "" || len(out.Decisions) >= outcomeItemsMost {
			continue
		}
		d.Question, d.Choice, d.Reason = clip(q, 200), clip(c, 300), clip(decisionText(oneLine(d.Reason)), 300)
		out.Decisions = append(out.Decisions, d)
	}
	for _, p := range o.Pitfalls {
		if p = clip(oneLine(p), 300); p != "" && !slices.Contains(out.Pitfalls, p) && len(out.Pitfalls) < outcomeItemsMost {
			out.Pitfalls = append(out.Pitfalls, p)
		}
	}
	return out
}

// outcomeDecisions writes the outcome, each decision and each pitfall as a decision of t (source "outcome"); a
// completion's rows share its key, each with its own suffix, so the store keeps them once. The outcome's row goes
// first and claims the completion: false when another assistant on the same store wrote it first, or the todo is gone
// (nothing else is written then). Without a store there is nothing to claim: true.
func (s *Service) outcomeDecisions(ctx context.Context, t Todo, key string, by outcomeAgent, o taskOutcome) bool {
	st := s.todoDB()
	if st == nil {
		return true
	}
	l := s.lang(ctx)
	type row struct{ q, c, r, id string }
	rows := []row{{q: i18n.T(l, "assistant-tasks.outcome_question"), c: strings.Join(append([]string{o.Outcome}, o.Links...), "\n"), id: "o"}}
	for i, d := range o.Decisions {
		rows = append(rows, row{q: d.Question, c: d.Choice, r: d.Reason, id: "d" + string(rune('1'+i))})
	}
	for i, p := range o.Pitfalls {
		rows = append(rows, row{q: i18n.T(l, "assistant-tasks.outcome_pitfall"), c: p, id: "p" + string(rune('1'+i))})
	}
	at := decisionNow()
	wrote, last := false, at
	for i, r := range rows {
		at := at.Add(time.Duration(i) * time.Microsecond) // listed in this order
		ok, err := s.insertDecision(ctx, st, db.TodoDecisionRow{ID: "dec_" + randomHex(8), TodoID: t.ID, Question: decisionText(r.q),
			Choice: decisionText(r.c), Reason: decisionText(r.r), Source: db.DecisionOutcome, SourceMachine: by.MachineID,
			SourceSession: by.SessionID, SourceMessage: key + "#" + r.id, CreatedAt: at})
		if err != nil {
			s.logf("task %s: outcome decisions: %v", t.ID, err)
			if i == 0 {
				return false
			}
			break
		}
		if i == 0 && !ok {
			return false // the completion is another's
		}
		if ok {
			wrote, last = true, at
		}
	}
	if wrote {
		s.decisionsChanged(ctx, t.ID, last)
	}
	return true
}

// outcomeLog appends "Outcome: …" (in Chinese) to the timeline of the project t was put in (todo_project.go), or, when it has
// none (or it is gone), of each project an agent of t worked in (kb.ProjectsForWork), once a project: «to-do · <title>», not
// bounded like an agent's turns (kb_bus.go).
func (s *Service) outcomeLog(ctx context.Context, t Todo, agents []outcomeAgent, o taskOutcome) {
	if !s.kbWritable(ctx) {
		return
	}
	e, err := s.kbGet(ctx)
	if errors.Is(err, errKBNotOpen) {
		select { // it opens once the hub's hello came and the archive's zone is read: one more try
		case <-time.After(30 * time.Second):
		case <-ctx.Done():
		}
		e, err = s.kbGet(ctx)
	}
	if err != nil || e == nil {
		return
	}
	line := outcomeLogLine(o)
	source := kb.TodoLogSource + kb.TodoLogTitle(t.Title)
	for _, p := range outcomeProjects(e, t, agents) {
		if _, err := e.Apply(kb.KindProject, p.Slug, kb.Update{Log: line, Source: source}); err != nil {
			s.logf("task %s: outcome log %s: %v", t.ID, p.Slug, err)
		}
	}
}

// outcomeProjects are the projects whose timelines get t's outcome: the one it was put in, else those its agents worked
// in, each once.
func outcomeProjects(e *kb.Engine, t Todo, agents []outcomeAgent) []*kb.Project {
	if p := ownProject(e, t); p != nil {
		return []*kb.Project{p}
	}
	var out []*kb.Project
	done := map[string]bool{}
	for _, a := range agents {
		for _, p := range e.ProjectsForWork(a.MachineID, a.Cwd) {
			if !done[p.Slug] {
				done[p.Slug] = true
				out = append(out, p)
			}
		}
	}
	return out
}

// outcomeLogLine is "Outcome: <outcome> <links>" (in Chinese) within a timeline line: the links whole (those that fit), the outcome
// clipped to the room they leave, so kb's cut at outcomeLogMax never breaks a link.
func outcomeLogLine(o taskOutcome) string {
	const prefix = kb.TodoOutcomePrefix
	var links []string
	room := outcomeLogMax - len([]rune(prefix)) - 20 // at least 20 runes of the outcome
	for _, l := range o.Links[:min(len(o.Links), outcomeLogLinks)] {
		if n := len([]rune(l)) + 1; n <= room {
			links, room = append(links, l), room-n
		}
	}
	text := o.Outcome
	if max := room + 20 - 1; len([]rune(text)) > max {
		text = clip(text, max-1) // clip adds "…"
	}
	if len(links) == 0 {
		return prefix + text
	}
	return prefix + text + " " + strings.Join(links, " ")
}

// projectOutcomesMost is how many finished todos' outcomes a new agent for a project is handed.
const projectOutcomesMost = 5

// projectContext is the <persona-context> a new agent working in dir on machine gets before its first prompt: each of
// its projects' one line and latest todo outcomes (todo/project); empty when the directory is no project's or the
// knowledge base is not open (the agent starts without it).
func (s *Service) projectContext(ctx context.Context, machine, dir string) string {
	if strings.TrimSpace(dir) == "" || s.isWorkMachine(machine) {
		return ""
	}
	e, err := s.kbGet(ctx)
	if err != nil || e == nil {
		return ""
	}
	var blocks []string
	for _, p := range e.ProjectsForWork(machine, dir) {
		d := prompts.TodoProject{Name: contextText(oneLine(p.Name)), OneLine: contextText(oneLine(p.OneLine))}
		for _, o := range e.Store.TodoOutcomes(p.Slug, projectOutcomesMost) {
			d.Outcomes = append(d.Outcomes, prompts.TodoProjectOutcome{When: o.When, Title: contextText(o.Title), Text: contextText(o.Text)})
		}
		if b := prompts.RenderTodoProject(d); b != "" {
			blocks = append(blocks, b)
		}
		if len(blocks) == 2 { // a directory in more projects than that: the first two (slug order) are enough
			break
		}
	}
	return strings.Join(blocks, "\n\n")
}

// withProjectContext puts the project background of dir on machine before a new agent's first prompt; the prompt as
// it is without any (or when it is empty: nothing to put it before).
func (s *Service) withProjectContext(ctx context.Context, machine, dir, prompt string) string {
	if strings.TrimSpace(prompt) == "" {
		return prompt
	}
	if c := s.projectContext(ctx, machine, dir); c != "" {
		return c + "\n\n" + prompt
	}
	return prompt
}
