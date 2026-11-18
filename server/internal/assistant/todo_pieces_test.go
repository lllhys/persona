package assistant

import (
	"context"
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"testing"
	"testing/synctest"

	"github.com/nwverse/persona/server/internal/db"
)

// A request in pieces (todo_pieces.go): part_of and after are written by todo_create, todo_update, task_plan and the
// apps' PATCH, checked by the manager, kept in the store, and a piece the conversation recorded is the one its task_plan
// later runs an Agent on. Ids and titles are placeholders.

func pieceTool(t *testing.T, s *Service, self, name string, args map[string]any) string {
	t.Helper()
	out, err := runJSON(t, s, self, name, args)
	if err != nil {
		t.Fatalf("%s %v: %v", name, args, err)
	}
	return out
}

func pieceRefused(t *testing.T, s *Service, self, name string, args map[string]any, want string) {
	t.Helper()
	out, err := runJSON(t, s, self, name, args)
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("%s %v: got %q, %v; want an error with %q", name, args, out, err, want)
	}
}

func TestTodoPiecesTools(t *testing.T) {
	todoStores(t, func(t *testing.T, e *todoEnv, _ *db.DB) {
		s := e.s
		pieceTool(t, s, "conv1", "todo_create", map[string]any{"title": "Search page"})
		root := mustTodo(t, s, "Search page")
		if root.PartOf != "" || len(root.After) != 0 || root.Origin != "" {
			t.Fatalf("a plain to-do: %+v", root)
		}

		// A piece the conversation will hand out is its own (task_plan's todo_id can run an Agent on it).
		out := pieceTool(t, s, "conv1", "todo_create", map[string]any{"title": "API fields", "part_of": " " + root.ID + " "})
		api := mustTodo(t, s, "API fields")
		if api.PartOf != root.ID || api.Origin != "conv1" || api.Supervision != nil || len(api.Agents) != 0 {
			t.Fatalf("the piece: %+v", api)
		}
		if !strings.Contains(out, "todo_id "+api.ID) || !strings.Contains(out, "(piece of "+root.ID+")") {
			t.Fatalf("the answer: %s", out)
		}
		if _, err := s.reusableTodo(context.Background(), "conv1", api.ID, false); err != nil {
			t.Fatalf("its own conversation can't run an Agent on it: %v", err)
		}
		out = pieceTool(t, s, "conv1", "todo_create", map[string]any{"title": "Front end", "part_of": root.ID, "after": []string{api.ID, " ", api.ID}})
		front := mustTodo(t, s, "Front end")
		if front.PartOf != root.ID || !slices.Equal(front.After, []string{api.ID}) || !strings.Contains(out, "after "+api.ID+")") {
			t.Fatalf("after, cleaned: %+v %s", front, out)
		}
		pieceTool(t, s, "conv1", "todo_create", map[string]any{"title": "Tests", "part_of": root.ID, "after": []string{front.ID}})
		tests := mustTodo(t, s, "Tests")

		// What the manager refuses.
		pieceRefused(t, s, "conv1", "todo_create", map[string]any{"title": "x", "part_of": "todo_nope"}, `no to-do "todo_nope"`)
		pieceRefused(t, s, "conv1", "todo_create", map[string]any{"title": "x", "part_of": api.ID}, "is itself a piece of")
		pieceRefused(t, s, "conv1", "todo_create", map[string]any{"title": "x", "after": []string{api.ID}}, "give part_of too")
		pieceRefused(t, s, "conv1", "todo_create", map[string]any{"title": "x", "part_of": root.ID, "after": []string{root.ID}}, "not a piece of the same request")
		pieceRefused(t, s, "conv1", "todo_create", map[string]any{"title": "x", "part_of": root.ID, "after": "todo_a"}, "a list of to-do ids")
		pieceRefused(t, s, "conv1", "todo_update", map[string]any{"id": api.ID, "part_of": api.ID}, "the to-do itself")
		pieceRefused(t, s, "conv1", "todo_update", map[string]any{"id": api.ID, "after": []string{api.ID}}, "the to-do itself")
		pieceRefused(t, s, "conv1", "todo_update", map[string]any{"id": api.ID, "after": []string{tests.ID}}, "already waits for this to-do")
		pieceTool(t, s, "conv1", "todo_create", map[string]any{"title": "Another request"})
		other := mustTodo(t, s, "Another request")
		pieceRefused(t, s, "conv1", "todo_update", map[string]any{"id": root.ID, "part_of": other.ID}, "is a request with pieces")

		// Changing the request clears what it waited for; [] and "" clear them.
		pieceTool(t, s, "conv1", "todo_update", map[string]any{"id": tests.ID, "after": []string{front.ID, api.ID}})
		if got := mustTodo(t, s, "Tests"); !slices.Equal(got.After, []string{front.ID, api.ID}) {
			t.Fatalf("after replaced: %+v", got.After)
		}
		pieceTool(t, s, "conv1", "todo_update", map[string]any{"id": tests.ID, "after": []string{}})
		if got := mustTodo(t, s, "Tests"); got.PartOf != root.ID || len(got.After) != 0 {
			t.Fatalf("after cleared: %+v", got)
		}
		pieceTool(t, s, "conv1", "todo_update", map[string]any{"id": front.ID, "part_of": other.ID})
		if got := mustTodo(t, s, "Front end"); got.PartOf != other.ID || len(got.After) != 0 {
			t.Fatalf("moved to another request: %+v", got)
		}
		pieceTool(t, s, "conv1", "todo_update", map[string]any{"id": front.ID, "part_of": ""})
		if got := mustTodo(t, s, "Front end"); got.PartOf != "" || len(got.After) != 0 {
			t.Fatalf("no longer a piece: %+v", got)
		}

		// Read back as the store keeps them (a restart reads them again).
		pieceTool(t, s, "conv1", "todo_update", map[string]any{"id": tests.ID, "after": []string{api.ID}})
		reloadTodos(t, s)
		if got := mustTodo(t, s, "Tests"); got.PartOf != root.ID || !slices.Equal(got.After, []string{api.ID}) {
			t.Fatalf("after a reload: %+v", got)
		}
	})
}

func TestTodoPiecesAPI(t *testing.T) {
	todoStores(t, func(t *testing.T, e *todoEnv, _ *db.DB) {
		send := func(method, path, body string) Todo {
			var res struct{ Todo Todo }
			_ = json.Unmarshal(e.must(method, path, body).Body.Bytes(), &res)
			return res.Todo
		}
		root := send("POST", "/api/v1/todos", `{"title":"Move house"}`)
		a := send("POST", "/api/v1/todos", `{"title":"Book the van"}`)
		b := send("POST", "/api/v1/todos", `{"title":"Pack the kitchen"}`)
		if got := send("PATCH", "/api/v1/todos/"+a.ID, `{"part_of":"`+root.ID+`"}`); got.PartOf != root.ID {
			t.Fatalf("part_of: %+v", got)
		}
		got := send("PATCH", "/api/v1/todos/"+b.ID, `{"part_of":"`+root.ID+`","after":["`+a.ID+`"]}`)
		if got.PartOf != root.ID || !slices.Equal(got.After, []string{a.ID}) {
			t.Fatalf("after: %+v", got)
		}
		// The list carries them; a to-do that is no piece carries neither.
		var list struct{ Todos []map[string]any }
		_ = json.Unmarshal(e.must("GET", "/api/v1/todos", "").Body.Bytes(), &list)
		for _, td := range list.Todos {
			_, hasPart := td["part_of"]
			_, hasAfter := td["after"]
			switch td["id"] {
			case root.ID:
				if hasPart || hasAfter {
					t.Fatalf("the root: %v", td)
				}
			case b.ID:
				if td["part_of"] != root.ID || !hasAfter {
					t.Fatalf("the piece: %v", td)
				}
			}
		}
		for _, body := range []string{`{"part_of":"todo_nope"}`, `{"after":["` + a.ID + `"]}`, `{"part_of":7}`, `{"part_of":"` + b.ID + `"}`} {
			if w := e.do("PATCH", "/api/v1/todos/"+root.ID, body); w.Code != http.StatusBadRequest {
				t.Errorf("%s: %d %s", body, w.Code, w.Body)
			}
		}
		if w := e.do("PATCH", "/api/v1/todos/"+a.ID, `{"after":["`+b.ID+`"]}`); w.Code != http.StatusBadRequest {
			t.Errorf("a cycle: %d %s", w.Code, w.Body)
		}
		if got := send("PATCH", "/api/v1/todos/"+b.ID, `{"part_of":null}`); got.PartOf != "" || len(got.After) != 0 {
			t.Fatalf("cleared: %+v", got)
		}
	})
}

// A task_plan task names its place in a request; the to-do it starts on carries it, whether new or a piece recorded
// before (todo_id), and the recorded piece keeps its id.
func TestTaskPlanPieces(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s, _ := readonlyTaskService(t)
		ctx := context.Background()
		pieceTool(t, s, "conv1", "todo_create", map[string]any{"title": "Search page"})
		root := mustTodo(t, s, "Search page")
		pieceTool(t, s, "conv1", "todo_create", map[string]any{"title": "API fields", "part_of": root.ID})
		api := mustTodo(t, s, "API fields")

		bad := readonlyTestTask("")
		bad.PartOf = api.ID
		if _, err := s.toolTaskPlan(ctx, "conv1", readonlyTaskArgs(t, bad)); err == nil || !strings.Contains(err.Error(), "task 1: part_of") {
			t.Fatalf("a piece of a piece: %v", err)
		}

		task := readonlyTestTask("")
		task.Title, task.PartOf, task.After = "Back end", root.ID, []string{api.ID}
		if _, err := s.toolTaskPlan(ctx, "conv1", readonlyTaskArgs(t, task)); err != nil {
			t.Fatal(err)
		}
		synctest.Wait()
		back := mustTodo(t, s, "Back end")
		if back.PartOf != root.ID || !slices.Equal(back.After, []string{api.ID}) || len(back.Agents) != 1 {
			t.Fatalf("the new task's to-do: %+v", back)
		}

		// The recorded piece's turn: its task runs on it, keeping its id and its links.
		run := readonlyTestTask("")
		run.Title = "API fields"
		type withTodo struct {
			PlanTask
			TodoID string `json:"todo_id"`
		}
		args, _ := json.Marshal(map[string]any{"tasks": []withTodo{{PlanTask: run, TodoID: api.ID}}})
		if _, err := s.toolTaskPlan(ctx, "conv1", args); err != nil {
			t.Fatal(err)
		}
		synctest.Wait()
		got, _ := s.todoNow(api.ID)
		if got.PartOf != root.ID || len(got.Agents) != 1 {
			t.Fatalf("the recorded piece: %+v", got)
		}
		s.todos.mu.RLock()
		n := len(s.todos.todos)
		s.todos.mu.RUnlock()
		if n != 3 {
			t.Fatalf("%d to-dos, want the request, the API fields and the back end (no second to-do for the API fields)", n)
		}
	})
}
