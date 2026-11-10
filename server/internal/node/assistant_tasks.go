package node

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"time"
)

// recordBackgroundTasks preserves the difference between an explicit empty
// runtime event and an absent, null or malformed list. Unknown evidence keeps
// existing task descriptions, so it cannot erase visible active work.
func (p *assistantProc) recordBackgroundTasks(raw json.RawMessage) ([]string, error) {
	var event *[]*struct {
		Description string `json:"description"`
	}
	if err := json.Unmarshal(raw, &event); err != nil || event == nil {
		p.invalidateUpdateTasks()
		return nil, errors.New("explicit background task list unavailable")
	}
	tasks := make([]string, 0, len(*event))
	for _, item := range *event {
		if item == nil {
			p.invalidateUpdateTasks()
			return nil, errors.New("background task entry is not an object")
		}
		tasks = append(tasks, item.Description)
	}
	p.mu.Lock()
	p.tasks, p.tasksObserved = tasks, true
	p.tasksRevision++
	p.lastActive = time.Now()
	p.mu.Unlock()
	return tasks, nil
}

func (p *assistantProc) invalidateUpdateTasks() {
	p.mu.Lock()
	p.tasksObserved = false
	p.tasksRevision++
	p.mu.Unlock()
}

func (p *assistantProc) observedUpdateTasks() ([]string, uint64, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.tasksObserved {
		return nil, p.tasksRevision, errors.New("runtime background inventory has not been explicitly observed")
	}
	return slices.Clone(p.tasks), p.tasksRevision, nil
}

// claudeTasksSnapshotWait bounds the wait for the task list that follows a repeated initialize's success.
const claudeTasksSnapshotWait = 2 * time.Second

// syncClaudeTasks asks a running Claude process for its background tasks when it has reported none (#3495). Claude
// Code sends background_tasks_changed only when the set changes, never at start, and answers a repeated initialize
// control request with the current set right behind its success, an empty one included (its schema; 2.1.311). The
// node sends no initialize of its own, so the first one here is not a repeat: it asks twice. A CLI that predates the
// snapshot sends nothing, which leaves the set unknown.
func (p *assistantProc) syncClaudeTasks(ctx context.Context) {
	if p.stdin == nil || !p.alive() {
		return // no process to ask
	}
	for range 2 {
		p.mu.Lock()
		observed, revision := p.tasksObserved, p.tasksRevision
		p.mu.Unlock()
		if observed {
			return
		}
		if err := p.control(ctx, map[string]any{"subtype": "initialize"}); err != nil {
			return
		}
		deadline := time.Now().Add(claudeTasksSnapshotWait)
		for time.Now().Before(deadline) && ctx.Err() == nil {
			p.mu.Lock()
			changed := p.tasksRevision != revision
			p.mu.Unlock()
			if changed {
				break
			}
			time.Sleep(10 * time.Millisecond)
		}
	}
}
