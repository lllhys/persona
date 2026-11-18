package assistant

import (
	"context"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/assistantwire"
	"github.com/nwverse/persona/server/internal/proto"
)

// A leader's pieces of work (#5846): todo_create in a leader opens the to-do under its direction (origin and supervisor
// the leader, no agent, no done_when), task_plan's todo_id can run an agent on it later, the direction tree shows it as
// planned until an agent works on it, and it raises no stuck report while it has not started. Ids are placeholders.

func mustTodo(t *testing.T, s *Service, title string) Todo {
	t.Helper()
	td, ok := todoByTitle(s, title)
	if !ok {
		t.Fatalf("no to-do %q", title)
	}
	return td
}

func TestLeaderTodoCreateIsAPieceOfItsDirection(t *testing.T) {
	e := treeEnv(t)
	out, err := runJSON(t, e.s, "conv_lead_a", "todo_create", map[string]any{"title": "Tablet layout", "notes": "two columns", "priority": "high"})
	if err != nil {
		t.Fatal(err)
	}
	piece := mustTodo(t, e.s, "Tablet layout")
	if piece.Origin != "conv_lead_a" || piece.Supervision == nil || piece.Supervision.Supervisor != "conv_lead_a" ||
		len(piece.Supervision.DoneWhen) != 0 || piece.Supervision.ExpectMinutes != 0 || len(piece.Agents) != 0 ||
		piece.Notes != "two columns" || piece.Priority != "high" || piece.DoneAt != nil {
		t.Fatalf("the piece: %+v %+v", piece, piece.Supervision)
	}
	if !strings.Contains(out, "todo_id "+piece.ID) {
		t.Fatalf("the answer does not say how to run it: %s", out)
	}
	// The same title again is the same piece.
	if again, err := runJSON(t, e.s, "conv_lead_a", "todo_create", map[string]any{"title": "Tablet layout"}); err != nil ||
		!strings.HasPrefix(again, "Already there") {
		t.Fatalf("again: %q %v", again, err)
	}

	// The leader can run an agent on it with task_plan's todo_id; another conversation cannot.
	if _, err := e.s.reusableTodo(context.Background(), "conv_lead_a", piece.ID, false); err != nil {
		t.Fatalf("the leader's todo_id: %v", err)
	}
	if _, err := e.s.reusableTodo(context.Background(), "conv_lead_b", piece.ID, false); err == nil {
		t.Fatal("another leader's todo_id was taken")
	}

	// The main conversation and a side conversation still make the user's plain to-dos.
	if _, err := runJSON(t, e.s, treeMain, "todo_create", map[string]any{"title": "Buy milk"}); err != nil {
		t.Fatal(err)
	}
	if _, err := runJSON(t, e.s, "conv_side", "todo_create", map[string]any{"title": "Read the report"}); err != nil {
		t.Fatal(err)
	}
	for _, title := range []string{"Buy milk", "Read the report"} {
		if td := mustTodo(t, e.s, title); td.Origin != "" || td.Supervision != nil {
			t.Fatalf("%s: %+v %+v", title, td, td.Supervision)
		}
	}

	ds := e.tree().Directions
	a, sec := ds[0], ds[len(ds)-1]
	if !slices.Equal(taskIDs(a), []string{piece.ID}) || a.Tasks[0].State != treePlanned || a.Counts != (treeCounts{Planned: 1}) {
		t.Fatalf("A: %+v", a)
	}
	if len(sec.Tasks) != 2 || sec.Counts != (treeCounts{Planned: 2}) {
		t.Fatalf("the secretary: %+v", sec)
	}
}

func TestDirectionTreeShowsWhatNoAgentWorkedOnAsPlanned(t *testing.T) {
	e := treeEnv(t)
	// Past a day (stuck's limit), short of two (the To-dos SOP shelves a task with no activity for two days).
	old := time.Now().Add(-30 * time.Hour)
	e.f.setSessions("mac", proto.Session{MachineID: "mac", SessionID: "s-run", State: proto.StateWorking})
	stale := func(t *Todo) { t.CreatedAt, t.UpdatedAt = old, old }

	// Not started for over a day: planned, not stuck.
	piece := e.task("A piece nobody took up", func(t *Todo) { supervisedBy("conv_lead_a")(t); t.Origin = "conv_lead_a"; stale(t) })
	running := e.task("Running", func(t *Todo) {
		supervisedBy("conv_lead_a")(t)
		t.Agents = []TodoAgent{{MachineID: "mac", SessionID: "s-run"}}
	})
	// An agent worked on it and was let go: it has started.
	worked := e.task("Worked on, then let go", func(t *Todo) { supervisedBy("conv_lead_a")(t); stale(t) })
	e.worked(worked)
	// A concern waits for the main conversation's answer: it keeps its state.
	concern := e.task("A concern", func(t *Todo) { t.Kind = "concern" })

	state := map[string]string{}
	var order []int
	for _, d := range e.tree().Directions {
		for _, tk := range d.Tasks {
			state[tk.ID] = tk.State
			if d.ID == "conv_lead_a" {
				order = append(order, slices.Index(treeStates, tk.State))
			}
		}
	}
	if state[piece] != treePlanned || state[running] != treeWorking || state[worked] == treePlanned || state[concern] != treeWorking {
		t.Fatalf("states: piece %q, running %q, worked %q, concern %q", state[piece], state[running], state[worked], state[concern])
	}
	// Listed after the ones under way.
	if !slices.IsSorted(order) || slices.Index(treeStates, treePlanned) != len(treeStates)-1 {
		t.Fatalf("order: %v", order)
	}
}

// A leader's piece no agent has worked on is not stuck however long it waits: no report, no main-conversation entry.
func TestLeaderStuckSkipsPiecesNotStarted(t *testing.T) {
	st := &memStore{}
	e := directionEnv(t, st)
	setSettings(t, e.s, func(v *Settings) { v.Main = "conv_main" })
	e.title("conv_lead_a", "1110 | 探索 | x")
	e.leaders("conv_lead_a")
	e.syncLeaders()
	id := e.leaderTask("平板布局", "conv_lead_a")
	for _, r := range e.reports(time.Now().Add(3*24*time.Hour), true) {
		if r.Kind == assistantwire.LeaderStuck {
			t.Fatalf("a piece not started was reported stuck: %+v", r)
		}
	}
	tr := todoTriage{Section: triageOther, Last: time.Now().Add(-72 * time.Hour)}
	if td, _ := e.s.todoNow(id); taskStuck(td, tr, true, time.Now()) {
		t.Fatal("taskStuck on a piece not started")
	}
	// Waiting on the owner it is stuck as before.
	tr.Section = triageWaiting
	if td, _ := e.s.todoNow(id); !taskStuck(td, tr, true, time.Now()) {
		t.Fatal("not stuck while waiting on the owner for three days")
	}
}
