package assistant

import (
	"slices"
	"sync"
	"time"

	"github.com/nwverse/persona/server/internal/assistantwire"
	"github.com/nwverse/persona/server/internal/db"
)

// What the extensions are told of the todos (#5279, docs/extensions/extensions.md "Events"): todo.changed when a todo is created,
// deleted, or its title, whether it is done, its supervisor (#3484) or its chat thread changes (each carries its kind:
// concern for one of the owner's concerns, #3561), and todo.decision for every decision written on
// one. The hub hands them only to the extensions that subscribed and were granted todos.read (extmgr/todo_events.go);
// this side only says what happened, and nothing is sent while the hub carries no extensions (todoEventPublisher).

// todoChangedSettle is how long a change waits for the rest: what happens to todos within it is told once, as the
// todos are at its end.
const todoChangedSettle = time.Second

// todoEventLimit bounds what waits to be handed to the hub: past it the oldest is dropped.
const todoEventLimit = 512

// todoEventPublisher is optional for in-process embedders. Production's BusHub publishes these two typed events; they
// never enter the public ws.broadcast path.
type todoEventPublisher interface {
	TodoEvent(event string, params any) error
}

func (b *BusHub) TodoEvent(event string, params any) error { return b.notifyHub(event, params) }

// todoMark is what a change of a todo is told by: what todo.changed carries and whether it is done.
type todoMark struct {
	title      string
	done       bool
	failed     bool
	bound      bool
	thread     TodoThread
	supervisor string
	kind       string
}

func markOf(t *Todo) todoMark {
	m := todoMark{title: t.Title, done: t.DoneAt != nil, failed: t.StartError != "", kind: t.Kind}
	if t.Supervision != nil {
		m.supervisor = t.Supervision.Supervisor
	}
	if t.Thread != nil {
		m.bound, m.thread = true, *t.Thread
	}
	return m
}

func (m todoMark) eventThread() *assistantwire.TodoEventThread {
	if !m.bound {
		return nil
	}
	return &assistantwire.TodoEventThread{Channel: m.thread.Channel, ChatID: m.thread.ChatID, ThreadID: m.thread.ThreadID, Title: m.thread.Title}
}

type todoNotice struct {
	event  string
	params any
}

// todoEvents is what the todos have waiting to be told to the hub. marks is the todos as the extensions were last told
// of them (nil until the todos are loaded: nothing is told of the todos as they were found).
type todoEvents struct {
	mu       sync.Mutex // marks, armed
	marks    map[string]todoMark
	armed    bool // a flush is scheduled
	flushing sync.Mutex
	after    func(d time.Duration, f func()) // tests replace the timer

	qmu     sync.Mutex // pending, running, dropped
	pending []todoNotice
	running bool
	dropped bool // the log has said events are dropped, and the queue has not been empty since
}

func (e *todoEvents) later(d time.Duration, f func()) {
	if e.after != nil {
		e.after(d, f)
		return
	}
	time.AfterFunc(d, f)
}

func (s *Service) todoEventsOn() bool {
	_, ok := s.hub.(todoEventPublisher)
	return ok
}

// todoMarksBaseline takes the todos as they were loaded for what the extensions are told of them; the loader holds the
// todos' lock.
func (s *Service) todoMarksBaseline(todos map[string]*Todo) {
	marks := make(map[string]todoMark, len(todos))
	for id, t := range todos {
		marks[id] = markOf(t)
	}
	e := &s.tevents
	e.mu.Lock()
	e.marks = marks
	e.mu.Unlock()
}

// todoChangedSoon is called whenever the todos changed ({"type":"todos"}): it schedules telling the extensions what
// differs from what they were told, once the changes of the next todoChangedSettle are in. Reading the differences from
// the todos themselves means no writer of a todo has to remember it.
func (s *Service) todoChangedSoon() {
	if !s.todoEventsOn() {
		return
	}
	e := &s.tevents
	e.mu.Lock()
	if e.armed || e.marks == nil {
		e.mu.Unlock()
		return
	}
	e.armed = true
	e.mu.Unlock()
	e.later(todoChangedSettle, s.todoChangedFlush)
}

func (s *Service) todoChangedFlush() {
	e := &s.tevents
	e.flushing.Lock()
	defer e.flushing.Unlock()
	e.mu.Lock()
	e.armed = false // before the todos are read: a change from here on schedules the next flush
	e.mu.Unlock()
	s.todos.mu.RLock()
	cur := make(map[string]todoMark, len(s.todos.todos))
	for id, t := range s.todos.todos {
		cur[id] = markOf(t)
	}
	s.todos.mu.RUnlock()
	e.mu.Lock()
	prev := e.marks
	e.marks = cur
	e.mu.Unlock()
	if prev == nil {
		return
	}
	ids := make([]string, 0, len(cur)+len(prev))
	for id := range cur {
		ids = append(ids, id)
	}
	for id := range prev {
		if _, ok := cur[id]; !ok {
			ids = append(ids, id)
		}
	}
	slices.Sort(ids)
	for _, id := range ids {
		now, there := cur[id]
		was, known := prev[id]
		switch {
		case there && !known:
			s.todoQueue(todoNotice{assistantwire.EventTodoChanged, assistantwire.TodoChangedEvent{
				ID: id, Title: now.title, Thread: now.eventThread(), Done: now.done, Supervisor: now.supervisor, Kind: now.kind, What: assistantwire.TodoCreated}})
		case there && now != was:
			s.todoQueue(todoNotice{assistantwire.EventTodoChanged, assistantwire.TodoChangedEvent{
				ID: id, Title: now.title, Thread: now.eventThread(), Done: now.done, Supervisor: now.supervisor, Kind: now.kind, What: assistantwire.TodoUpdated}})
		case !there:
			s.todoQueue(todoNotice{assistantwire.EventTodoChanged, assistantwire.TodoChangedEvent{
				ID: id, Title: was.title, Thread: was.eventThread(), Done: was.done, Supervisor: was.supervisor, Kind: was.kind, What: assistantwire.TodoDeleted}})
		}
	}
}

// todoDecisionEvent tells the extensions of a decision just written (every source: one that came from the todo's own
// thread carries source "thread", for an extension that would otherwise post it back to where it was said).
func (s *Service) todoDecisionEvent(row db.TodoDecisionRow) {
	if !s.todoEventsOn() {
		return
	}
	s.todos.mu.RLock()
	t, ok := s.todos.todos[row.TodoID]
	var m todoMark
	if ok {
		m = markOf(t)
	}
	s.todos.mu.RUnlock()
	if !ok {
		return
	}
	s.todoQueue(todoNotice{assistantwire.EventTodoDecision, assistantwire.TodoDecisionEvent{
		ID: row.TodoID, Title: m.title, Thread: m.eventThread(),
		Decision: assistantwire.TodoEventDecision{ID: row.ID, Question: row.Question, Choice: row.Choice, Reason: row.Reason,
			Source: row.Source, SupersedesID: row.SupersedesID}}})
}

// todoQueue keeps the todos' writes independent of a slow or disconnected bus: one worker hands the hub what waits, in
// order. A todo.changed for a todo that has one waiting is merged into it (assistantwire.MergeTodoChanged), so a backlog
// is at most one a todo; past todoEventLimit the oldest is dropped, and the log says so (once, until the queue is empty
// again). Like the other extension events these are best effort.
func (s *Service) todoQueue(n todoNotice) {
	e := &s.tevents
	e.qmu.Lock()
	defer e.qmu.Unlock()
	if c, ok := n.params.(assistantwire.TodoChangedEvent); ok {
		for i, old := range e.pending {
			o, isChange := old.params.(assistantwire.TodoChangedEvent)
			if !isChange || o.ID != c.ID {
				continue
			}
			if merged, keep := assistantwire.MergeTodoChanged(o, c); keep {
				e.pending[i].params = merged
			} else {
				e.pending = slices.Delete(e.pending, i, i+1)
			}
			return
		}
	}
	if len(e.pending) >= todoEventLimit {
		e.pending = e.pending[1:]
		if !e.dropped {
			e.dropped = true
			s.logf("todos: the extensions are behind: the oldest of %d waiting todo events is dropped", todoEventLimit)
		}
	}
	e.pending = append(e.pending, n)
	if !e.running {
		e.running = true
		go s.todoSendEvents()
	}
}

func (s *Service) todoSendEvents() {
	publisher := s.hub.(todoEventPublisher)
	e := &s.tevents
	for {
		e.qmu.Lock()
		if len(e.pending) == 0 {
			e.running = false
			e.dropped = false
			e.qmu.Unlock()
			return
		}
		n := e.pending[0]
		e.pending = e.pending[1:]
		e.qmu.Unlock()
		if publisher.TodoEvent(n.event, n.params) != nil {
			s.logf("todos: %s could not be told to the extensions", n.event)
		}
	}
}
