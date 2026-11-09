package assistant

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/nwverse/persona/server/internal/db"
	"github.com/nwverse/persona/server/internal/kb"
)

// projectEnv is todoEnv with a knowledge base that has two long-term projects, 社群运营 and 诊断能力, and a third one
// merged into the first.
func projectEnv(t *testing.T, database *db.DB) *todoEnv {
	t.Helper()
	f := newFakeHub().withMachine()
	var s *Service
	if database == nil {
		s = newKBService(t, f)
	} else {
		s = newKBServiceOn(t, f, database)
	}
	e := kbEng(t, s)
	for _, name := range []string{"社群运营", "诊断能力", "社区运营"} {
		if _, err := e.Apply(kb.KindProject, name, kb.Update{Name: name, Kind: "long"}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := e.Apply(kb.KindProject, "社区运营", kb.Update{MergeInto: "社群运营"}); err != nil {
		t.Fatal(err)
	}
	return &todoEnv{t: t, s: s, f: f}
}

// projectStores runs body on projectEnv in memory and, when there is a test database, on it.
func projectStores(t *testing.T, body func(t *testing.T, e *todoEnv)) {
	t.Run("memory", func(t *testing.T) { body(t, projectEnv(t, nil)) })
	t.Run("store", func(t *testing.T) { body(t, projectEnv(t, todoTestDB(t))) })
}

// The apps put a todo in a knowledge-base project and take it out (#2581): by slug or name (a merged project's name
// leads to the one it went into), kept by other edits, cleared by "", refused when no project has that name; a todo
// without one has no project field at all, so older apps read the same JSON.
func TestTodoProjectAPI(t *testing.T) {
	projectStores(t, func(t *testing.T, e *todoEnv) {
		var res struct{ Todo Todo }
		_ = json.Unmarshal(e.must("POST", "/api/v1/todos", `{"title":"Weekly digest","project":"社群运营"}`).Body.Bytes(), &res)
		id := res.Todo.ID
		if res.Todo.Project != "社群运营" {
			t.Fatalf("created: %+v", res.Todo)
		}
		if got, _ := e.todo(id); got.Project != "社群运营" {
			t.Fatalf("listed: %+v", got)
		}

		e.must("PATCH", "/api/v1/todos/"+id, `{"title":"Weekly digest, part two"}`)
		if got, _ := e.todo(id); got.Project != "社群运营" || got.Title != "Weekly digest, part two" {
			t.Fatalf("a title edit moved it: %+v", got)
		}
		e.must("PATCH", "/api/v1/todos/"+id, `{"project":"诊断能力"}`)
		if got, _ := e.todo(id); got.Project != "诊断能力" {
			t.Fatalf("moved: %+v", got)
		}
		e.must("PATCH", "/api/v1/todos/"+id, `{"project":"社区运营"}`)
		if got, _ := e.todo(id); got.Project != "社群运营" {
			t.Fatalf("by a merged project's name: %+v", got)
		}
		if w := e.do("PATCH", "/api/v1/todos/"+id, `{"project":"no such area"}`); w.Code != http.StatusBadRequest ||
			!strings.Contains(w.Body.String(), "no knowledge-base project") {
			t.Fatalf("unknown project: %d %s", w.Code, w.Body)
		}
		if got, _ := e.todo(id); got.Project != "社群运营" {
			t.Fatalf("a refused edit changed it: %+v", got)
		}
		if w := e.do("POST", "/api/v1/todos", `{"title":"Elsewhere","project":"no such area"}`); w.Code != http.StatusBadRequest {
			t.Fatalf("created in an unknown project: %d %s", w.Code, w.Body)
		}
		e.must("PATCH", "/api/v1/todos/"+id, `{"project":""}`)
		if got, _ := e.todo(id); got.Project != "" {
			t.Fatalf("cleared: %+v", got)
		}

		plain := e.create("Water the plants")
		body := e.must("GET", "/api/v1/todos", "").Body.String()
		var raw struct{ Todos []map[string]any }
		_ = json.Unmarshal([]byte(body), &raw)
		for _, td := range raw.Todos {
			if _, has := td["project"]; has {
				t.Fatalf("a todo without a project has the field: %v (%s)", td, plain.ID)
			}
		}
	})
}

// The assistant's tools set, show, filter by and clear a todo's project; an unknown one is refused with what to do.
func TestTodoProjectTools(t *testing.T) {
	projectStores(t, func(t *testing.T, e *todoEnv) {
		res, err := e.tool("todo_create", map[string]any{"title": "Reply to the group", "project": "社群运营"})
		if err != nil || !strings.Contains(res, "project 社群运营") {
			t.Fatalf("create: %s %v", res, err)
		}
		id := idIn(t, res)
		other, err := e.tool("todo_create", map[string]any{"title": "Read the crash report"})
		if err != nil || strings.Contains(other, "project") {
			t.Fatalf("create without: %s %v", other, err)
		}
		if _, err := e.tool("todo_create", map[string]any{"title": "Nowhere", "project": "社群运营二"}); err == nil ||
			!strings.Contains(err.Error(), "kb_list") {
			t.Fatalf("unknown project: %v", err)
		}

		list, err := e.tool("todo_list", map[string]any{"project": "社群运营"})
		if err != nil || !strings.Contains(list, id) || strings.Contains(list, idIn(t, other)) {
			t.Fatalf("filtered: %s %v", list, err)
		}
		if res, err := e.tool("todo_update", map[string]any{"id": idIn(t, other), "project": "诊断能力"}); err != nil ||
			!strings.Contains(res, "project 诊断能力") {
			t.Fatalf("update: %s %v", res, err)
		}
		if res, err := e.tool("todo_update", map[string]any{"id": id, "project": ""}); err != nil || strings.Contains(res, "project") {
			t.Fatalf("cleared: %s %v", res, err)
		}
		if got, _ := e.todo(id); got.Project != "" {
			t.Fatalf("cleared, read back: %+v", got)
		}
	})
}

// A task_plan task names its project; the todo the task becomes is in it.
func TestTaskPlanProject(t *testing.T) {
	synctest.Test(t, taskPlanProject)
}

func taskPlanProject(t *testing.T) {
	f := newFakeHub()
	f.sessions["box"] = nil
	f.ownModels = true
	f.hello.KB = &KBSettings{}
	// no database (plans live in memory), a knowledge base of its own
	s := withSettings(t, f, New(Options{Hub: f, Logf: testLogf(t), KB: &KBOptions{Dir: t.TempDir()}}))
	t.Cleanup(s.kbClose)
	if _, err := kbEng(t, s).Apply(kb.KindProject, "诊断能力", kb.Update{Name: "诊断能力", Kind: "long"}); err != nil {
		t.Fatal(err)
	}
	task := readonlyTestTask("")
	task.Project = "no such area"
	if _, err := s.toolTaskPlan(context.Background(), "conv1", readonlyTaskArgs(t, task)); err == nil ||
		!strings.Contains(err.Error(), "task 1") {
		t.Fatalf("unknown project: %v", err)
	}
	task.Project = "诊断能力"
	if _, err := s.toolTaskPlan(context.Background(), "conv1", readonlyTaskArgs(t, task)); err != nil {
		t.Fatal(err)
	}
	synctest.Wait()
	if todo := onlyTodo(s); todo.Project != "诊断能力" || todo.Origin != "conv1" || len(todo.Agents) != 1 {
		t.Fatalf("task's todo = %+v", todo)
	}
}

// A finished task in a project of its own writes its outcome to that project's timeline, not to the one its agent's
// directory belongs to; with its project gone, to that one as before.
func TestTaskOutcomeGoesToTheTodosProject(t *testing.T) {
	s, f, todo := outcomeEnv(t, todoTestDB(t), outcomeReply)
	f.outcome = `{"outcome":"修好登录页横屏崩溃","links":[]}`
	e := kbEng(t, s)
	if _, err := e.Apply(kb.KindProject, "诊断能力", kb.Update{Name: "诊断能力", Kind: "long"}); err != nil {
		t.Fatal(err)
	}
	slug := "诊断能力"
	moved, err := s.editTodo(context.Background(), todo.ID, db.TodoEdit{Project: &slug})
	if err != nil {
		t.Fatal(err)
	}
	s.recordTaskOutcome(moved, time.Now().UTC())
	if log := e.Store.Log("诊断能力", 10); len(log) != 1 || !strings.Contains(log[0], "结论：修好登录页横屏崩溃") {
		t.Fatalf("its project's log = %v", log)
	}
	if log := e.Store.Log("天气-app", 10); len(log) != 0 {
		t.Fatalf("the directory's project got it too: %v", log)
	}

	gone := moved
	gone.Project = "删掉的项目"
	if got := outcomeProjects(e, gone, []outcomeAgent{{MachineID: "box", Cwd: "/code/weather/ios"}}); len(got) != 1 || got[0].Slug != "天气-app" {
		t.Fatalf("with its project gone: %v", got)
	}
}
