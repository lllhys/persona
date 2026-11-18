package assistant

import (
	"net/http"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/db"
	"github.com/nwverse/persona/server/internal/proto"
)

// Seen to-dos (todo_seen.go, #5707): Got it marks a to-do seen without counting as activity, Undo takes the mark off,
// the apps read it in the list and are told it changed; a seen to-do leaves Just finished.

func TestTodoSeenRequests(t *testing.T) {
	todoStores(t, func(t *testing.T, e *todoEnv, _ *db.DB) {
		id := e.create("发版").ID
		e.must("PATCH", "/api/v1/todos/"+id, `{"done":true}`)
		before := e.get(id)
		if before == nil || before.SeenAt != nil {
			t.Fatalf("a new to-do reads seen: %+v", before)
		}
		// Only a full device marks it; an unknown to-do is 404.
		for _, method := range []string{"PUT", "DELETE"} {
			if w := e.doAs("answer", method, "/api/v1/todos/"+id+"/seen", ""); w.Code != http.StatusForbidden {
				t.Fatalf("%s as a read-only role: %d", method, w.Code)
			}
			if w := e.do(method, "/api/v1/todos/todo_gone/seen", ""); w.Code != http.StatusNotFound {
				t.Fatalf("%s an unknown to-do: %d", method, w.Code)
			}
		}

		events := todosEvents(e)
		e.must("PUT", "/api/v1/todos/"+id+"/seen", "")
		got := e.get(id)
		if got == nil || got.SeenAt == nil || got.SeenAt.Before(*before.DoneAt) {
			t.Fatalf("after Got it: %+v", got)
		}
		// the store keeps microseconds: the same moment, read back from it
		sameMoment := func(a, b time.Time) bool { return a.Truncate(time.Microsecond).Equal(b.Truncate(time.Microsecond)) }
		if !sameMoment(got.UpdatedAt, before.UpdatedAt) {
			t.Fatalf("Got it counted as activity: updated_at %v, was %v", got.UpdatedAt, before.UpdatedAt)
		}
		if todosEvents(e) == events {
			t.Fatal("the apps were not told the to-dos changed")
		}
		tr := triageTodo(listedTodo{Todo: *got}, nil, time.Now(), time.Time{}, time.UTC, false)
		if tr.Section != triageDone {
			t.Fatalf("a seen closed to-do is in %s", tr.Section)
		}

		e.must("DELETE", "/api/v1/todos/"+id+"/seen", "")
		if got := e.get(id); got == nil || got.SeenAt != nil || !sameMoment(got.UpdatedAt, before.UpdatedAt) {
			t.Fatalf("after Undo: %+v", got)
		}
	})
}

func TestTodoSeenSurvivesRestart(t *testing.T) {
	database := todoTestDB(t)
	e := newTodoEnv(t, database)
	id := e.create("发版").ID
	e.must("PUT", "/api/v1/todos/"+id+"/seen", "")
	again := &todoEnv{t: t, s: newServiceOn(t, e.f, database), f: e.f}
	if got := again.get(id); got == nil || got.SeenAt == nil {
		t.Fatalf("after a restart: %+v", got)
	}
}

// An open concern is never filed with the finished ones (#3582): seen, it waits in Other open for its report.
func TestTriageSeenConcernStaysOpen(t *testing.T) {
	now := time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC)
	moved, seen := now.Add(-time.Hour), now.Add(-30*time.Minute)
	sess := proto.Session{MachineID: "m", SessionID: "s", State: proto.StateUnread, UpdatedAt: moved}
	td := listedTodo{Todo: Todo{ID: "c", Title: "c", CreatedAt: moved, UpdatedAt: moved, Kind: db.KindConcern, SeenAt: &seen,
		Agents: []TodoAgent{{MachineID: "m", SessionID: "s", LinkedAt: moved}}}}
	if tr := triageTodo(td, lookupOf([]proto.Session{sess}), now, time.Time{}, time.UTC, false); tr.Section != triageOther || tr.Rule != ruleRest {
		t.Fatalf("a seen concern is %s / %s", tr.Section, tr.Rule)
	}
	td.Kind = ""
	if tr := triageTodo(td, lookupOf([]proto.Session{sess}), now, time.Time{}, time.UTC, false); tr.Section != triageDone || tr.Rule != ruleSeen {
		t.Fatalf("a seen to-do is %s / %s", tr.Section, tr.Rule)
	}
	r := triageTodos([]listedTodo{td}, lookupOf([]proto.Session{sess}), now, time.UTC, true)
	if r.Open != 0 || r.Finished != 1 {
		t.Fatalf("a seen finished turn counts %d open, %d finished", r.Open, r.Finished)
	}
}
