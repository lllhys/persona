package assistant

import (
	"cmp"
	"context"
	"slices"
	"strings"
	"time"

	"github.com/nwverse/persona/server/internal/proto"
)

// Independent of the task's estimate: no observed activity or scheduled resumption.
const superviseAllStoppedAfter = 30 * time.Minute

type superviseGroup struct {
	tasks    []Todo
	sessions []proto.Session
	unknown  bool // a failed read, not evidence that the signal no longer holds
}

// allStoppedTasks follows persisted worker links in both directions, including
// siblings. A supervisor reference also covers assistant conversations linked to
// a task. Plans only supply source/via when linking; no plan-history read is needed.
func allStoppedTasks(t Todo, tasks []Todo) []Todo {
	if !slices.ContainsFunc(tasks, func(task Todo) bool { return task.ID == t.ID }) {
		tasks = append(slices.Clone(tasks), t)
	}
	return allStoppedTaskGroups(tasks)[t.ID]
}

// Build the topology once per watch, not once per To-do. No machine calls here.
func allStoppedTaskGroups(tasks []Todo) map[string][]Todo {
	byID := map[string]Todo{}
	owners := map[string]string{}
	owner := func(ref, id string) {
		if previous, exists := owners[ref]; exists && previous != id {
			owners[ref] = "" // an ambiguous unqualified session ID cannot prove a relationship
		} else {
			owners[ref] = id
		}
	}
	for _, task := range tasks {
		if task.DoneAt == nil {
			byID[task.ID] = task
		}
	}
	for id, task := range byID {
		for _, a := range task.Agents {
			if a.active() {
				owner(leaderRef(a.MachineID, a.SessionID), id)
				owner(a.SessionID, id) // assistant conversation supervisors have no machine prefix
			}
		}
	}
	edges := map[string][]string{}
	link := func(child, parent string) {
		if _, ok := byID[parent]; !ok || child == parent {
			return
		}
		edges[child] = append(edges[child], parent)
		edges[parent] = append(edges[parent], child)
	}
	for id, task := range byID {
		for _, a := range task.Agents {
			if a.active() && a.Source == sourceWorker && a.Via != "" {
				link(id, a.Via)
			}
		}
		if task.Supervision != nil {
			link(id, owners[task.Supervision.Supervisor])
		}
	}
	groups := map[string][]Todo{}
	seen := map[string]bool{}
	for root := range byID {
		if seen[root] {
			continue
		}
		seen[root] = true
		queue := []string{root}
		var group []Todo
		for len(queue) > 0 {
			id := queue[0]
			queue = queue[1:]
			group = append(group, byID[id])
			for _, next := range edges[id] {
				if !seen[next] {
					seen[next] = true
					queue = append(queue, next)
				}
			}
		}
		for _, task := range group {
			groups[task.ID] = group
		}
	}
	return groups
}

// allStoppedSignal is pure so historical snapshots exercise the production rule.
// Missing/offline current members are not evidence of a stop (orphanSignal owns
// those). A completed or intentionally parked task never raises this signal.
func allStoppedSignal(t Todo, tasks []Todo, sessions []proto.Session, now time.Time) (superviseSignal, bool) {
	if t.Supervision == nil || t.DoneAt != nil {
		return superviseSignal{}, false
	}
	listed := make(map[[2]string]proto.Session, len(sessions))
	for _, c := range sessions {
		listed[[2]string{c.MachineID, c.SessionID}] = c
	}
	members := map[[2]string]proto.Session{}
	for _, task := range allStoppedTasks(t, tasks) {
		if task.DoneAt != nil || task.Cancelled() {
			return superviseSignal{}, false
		}
		if v := task.Supervision; v != nil {
			switch v.state(task) {
			case reviewParked, reviewPending:
				return superviseSignal{}, false
			}
		}
		gone := supersededAgents(task, sessions)
		n := 0
		for _, a := range task.Agents {
			key := [2]string{a.MachineID, a.SessionID}
			if !a.active() || gone[key] {
				continue
			}
			c, ok := listed[key]
			if !ok {
				return superviseSignal{}, false
			}
			members[key] = c
			n++
		}
		if n == 0 { // a linked task that has not started is not a stopped group
			return superviseSignal{}, false
		}
	}
	var keys, lines []string
	var latest time.Time
	for key, c := range members {
		since, ok := allStoppedSince(c, now)
		if !ok {
			return superviseSignal{}, false
		}
		// Some runtimes report children as separate sessions, not Subagents.
		for _, child := range sessions {
			if child.MachineID != key[0] || child.ParentSessionID != key[1] && child.RootSessionID != key[1] {
				continue
			}
			if child.Starting || len(child.Queued) > 0 ||
				child.PendingInteractions > 0 || child.State == proto.StateNeedsYou || child.AwaitsReply ||
				child.Loop != nil || child.TurnErrorResumes || child.Work != nil &&
				(child.Work.InteractionID != "" || child.Work.WakeAt != nil && child.Work.WakeAt.After(now)) {
				return superviseSignal{}, false
			}
			if child.State == proto.StateWorking || child.Status == proto.StatusWorking ||
				child.BackgroundWorking || child.BackgroundWaiting || backgroundRunning(child) {
				at, waiting := allStoppedBackground(child, now)
				if waiting || at.IsZero() || now.Sub(at) < superviseAllStoppedAfter {
					return superviseSignal{}, false
				}
				if at.After(since) {
					since = at
				}
			}
		}
		if since.After(latest) {
			latest = since
		}
		ref := leaderRef(key[0], key[1])
		keys = append(keys, ref+"@"+since.UTC().Format(time.RFC3339Nano))
		last := cmp.Or(strings.TrimSpace(c.Activity), strings.TrimSpace(c.LastText), "No last statement recorded")
		if w := c.Work; w != nil && (w.Wait != "" || w.Line != "") {
			last = cmp.Or(strings.TrimSpace(w.Wait), strings.TrimSpace(w.Line))
			if w.On != "" {
				last = w.On + ": " + last
			}
		}
		lines = append(lines, ref+": "+clip(oneLine(last), 600))
	}
	if len(keys) == 0 || now.Sub(latest) < superviseAllStoppedAfter {
		return superviseSignal{}, false
	}
	slices.Sort(keys)
	slices.Sort(lines)
	return superviseSignal{Todo: t.ID, Kind: sigAllStopped, Key: strings.Join(keys, "|"),
		Detail: strings.Join(lines, "\n"), Minutes: int(now.Sub(latest).Minutes())}, true
}

func allStoppedSince(c proto.Session, now time.Time) (time.Time, bool) {
	background := c.BackgroundWorking || c.BackgroundWaiting || backgroundRunning(c)
	if c.State != proto.StateIdle && c.State != proto.StateUnread && !(c.State == proto.StateWorking && background) ||
		c.Status != proto.StatusIdle && c.Status != proto.StatusDone || c.Starting ||
		c.PendingInteractions > 0 || c.AwaitsReply || len(c.Queued) > 0 || c.Loop != nil ||
		c.TurnErrorResumes {
		return time.Time{}, false
	}
	since, waiting := allStoppedBackground(c, now)
	if waiting {
		return time.Time{}, false
	}
	advance := func(at time.Time) {
		if at.After(since) {
			since = at
		}
	}
	advance(c.StateSince)
	if c.ActivityAt != nil {
		advance(*c.ActivityAt)
	}
	if w := c.Work; w != nil {
		if w.InteractionID != "" || w.WakeAt != nil && w.WakeAt.After(now) {
			return time.Time{}, false
		}
		advance(w.TurnAt)
		if w.WakeAt != nil {
			advance(*w.WakeAt) // a missed wake must also be overdue for the full interval
		}
	}
	return since, !since.IsZero() && !since.After(now)
}

// Background labels alone do not prove progress. Use observed activity where it
// exists, otherwise the start time. Without either, the parent's known stop bounds
// the age: its background work started before it stopped. A monitor with a future
// expiry is a timed wake. Activity timestamps also rearm deduplication.
func allStoppedBackground(c proto.Session, now time.Time) (latest time.Time, waiting bool) {
	latest = c.UpdatedAt
	advance := func(at time.Time) {
		if at.After(latest) {
			latest = at
		}
	}
	advance(c.StateSince)
	if c.Work != nil {
		advance(c.Work.TurnAt)
	}
	if c.ActivityAt != nil {
		advance(*c.ActivityAt)
	}
	for _, b := range c.Background {
		if b.Kind == "monitor" && b.ExpiresAt != nil && b.ExpiresAt.After(now) {
			return latest, true
		}
		advance(b.StartedAt)
	}
	for _, a := range c.Subagents {
		if a.Running {
			advance(a.StartedAt)
			advance(a.UpdatedAt)
		}
	}
	for _, w := range c.Workflows {
		if w.Running || w.Status == proto.WorkflowRunning {
			at := w.StartedAt
			if w.UpdatedAt.After(at) {
				at = w.UpdatedAt
			}
			for _, a := range w.Agents {
				if a.StartedAt.After(at) {
					at = a.StartedAt
				}
				if a.UpdatedAt.After(at) {
					at = a.UpdatedAt
				}
			}
			advance(at)
		}
	}
	if w := c.Work; w != nil && (w.InteractionID != "" || w.WakeAt != nil && w.WakeAt.After(now)) {
		return latest, true
	}
	return latest, false
}

type allStoppedBatchKey struct{}
type allStoppedChecks map[string]superviseSignal

// Validate a digest batch before its first delivery. Waking the leader with one
// copy must not invalidate the copy its own supervisor is about to receive.
// The context lasts only for this dispatch; retries and queue drains check again.
func (s *Service) allStoppedBatch(ctx context.Context, signals []superviseSignal) (context.Context, []superviseSignal, []superviseSignal) {
	checks := allStoppedChecks{}
	if !slices.ContainsFunc(signals, func(g superviseSignal) bool { return g.Kind == sigAllStopped }) {
		return ctx, signals, nil
	}
	known := s.ensureTodosLoaded(ctx) == nil
	var ready, retry []superviseSignal
	tasks := s.todosWhere(ctx, "*", func(t *Todo) bool { return t.DoneAt == nil })
	groups := allStoppedTaskGroups(tasks)
	lists := machineSessions{}
	now := time.Now()
	for _, g := range signals {
		if g.Kind != sigAllStopped {
			ready = append(ready, g)
			continue
		}
		if !known {
			retry = append(retry, g)
			continue
		}
		checks[g.id()] = superviseSignal{}
		if t, ok := s.todoNow(g.Todo); ok {
			group := s.allStoppedGroup(ctx, t, groups[t.ID], lists)
			if group.unknown {
				retry = append(retry, g)
				continue
			}
			if fresh, holds := allStoppedSignal(t, group.tasks, group.sessions, now); holds && fresh.Key == g.Key {
				checks[g.id()] = fresh
			}
		}
		ready = append(ready, g)
	}
	return context.WithValue(ctx, allStoppedBatchKey{}, checks), ready, retry
}

func (s *Service) checkedAllStopped(ctx context.Context, t Todo, g superviseSignal) (superviseSignal, bool) {
	if checks, ok := ctx.Value(allStoppedBatchKey{}).(allStoppedChecks); ok {
		if fresh, found := checks[g.id()]; found {
			return fresh, fresh.Kind == sigAllStopped && fresh.Key == g.Key
		}
	}
	tasks := s.todosWhere(ctx, "*", func(t *Todo) bool { return t.DoneAt == nil })
	group := s.allStoppedGroup(ctx, t, tasks, machineSessions{})
	fresh, holds := allStoppedSignal(t, group.tasks, group.sessions, time.Now())
	return fresh, !group.unknown && holds && fresh.Key == g.Key
}

// allStoppedGroup shares the watch's machine-list cache. Overlay the manager's
// ledger: a node list can predate a newly scheduled wake or a decision card.
func (s *Service) allStoppedGroup(ctx context.Context, t Todo, tasks []Todo, lists machineSessions) superviseGroup {
	group := superviseGroup{tasks: allStoppedTasks(t, tasks)}
	for _, task := range group.tasks {
		if task.Supervision != nil && (task.Supervision.state(task) == reviewParked || task.Supervision.state(task) == reviewPending) {
			return group // already known to be ineligible; no read is needed
		}
	}
	if !s.loadWork(ctx) {
		group.unknown = true
		return group // no evidence, not "no wakes"
	}
	s.accept.mu.Lock()
	cardsKnown := s.loadAcceptLocked(ctx)
	s.accept.mu.Unlock()
	if !cardsKnown {
		group.unknown = true
		return group // a failed card-ledger read is not "no pending answers"
	}
	machines := map[string]bool{}
	members := map[[2]string]bool{}
	for _, task := range group.tasks {
		if a, ok := s.askCopy(task.ID); ok && decideAsk(&a) && !acceptAskStale(&a, task) &&
			a.AcceptedAt.IsZero() && a.Recommend != "" {
			return group // a current, answerable task-decision card still waits for the owner
		}
		for _, a := range task.Agents {
			if a.active() {
				machines[a.MachineID] = true
				members[[2]string{a.MachineID, a.SessionID}] = true
				if _, seen := lists[a.MachineID]; !seen {
					list, err := s.sessions(ctx, a.MachineID)
					lists[a.MachineID] = machineList{list: list, err: err}
				}
			}
		}
	}
	for machine := range machines {
		list := lists[machine]
		if list.err != nil {
			group.unknown = true
			continue
		}
		for i := range list.list {
			row := &list.list[i]
			if !members[[2]string{machine, row.SessionID}] && !members[[2]string{machine, row.ParentSessionID}] &&
				!members[[2]string{machine, row.RootSessionID}] {
				continue // no full-machine row copies for every supervised task
			}
			c := *row
			if e, ok := s.workEntry([2]string{c.MachineID, c.SessionID}); ok {
				w := e.Work
				w.WakeAt, w.InteractionID, w.TurnAt = e.WakeAt, e.InteractionID, e.TurnAt
				c.Work = &w
			}
			group.sessions = append(group.sessions, c)
		}
	}
	return group
}
