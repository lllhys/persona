package assistant

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/nwverse/persona/server/internal/db"
)

// Reminders imported as to-dos (todo_imports.go, #4009): made once, changed only where Reminders changed, never
// brought back once deleted in Persona.

const importPath = "/api/v1/todos/import/reminders"

func importBatch(t *testing.T, e *todoEnv, body string) ImportResult {
	t.Helper()
	var res ImportResult
	if err := json.Unmarshal(e.must("POST", importPath, body).Body.Bytes(), &res); err != nil {
		t.Fatal(err)
	}
	return res
}

func todoTitled(e *todoEnv, title string) *Todo {
	for _, t := range e.s.todosOf(e.t.Context(), "") {
		if t.Title == title {
			return e.get(t.ID)
		}
	}
	return nil
}

func TestImportReminders(t *testing.T) {
	todoStores(t, func(t *testing.T, e *todoEnv, _ *db.DB) {
		first := `{"items":[
			{"external_id":"r1","title":" Buy milk ","notes":"2 L","due_date":"2026-11-12"},
			{"external_id":"r2","title":"Call the bank","due_at":"2026-11-11T09:30:00+08:00"},
			{"external_id":"r3","title":"  "}]}`
		if got := importBatch(t, e, first); got != (ImportResult{Created: 2, Skipped: 1}) {
			t.Fatalf("first import: %+v", got)
		}
		milk := todoTitled(e, "Buy milk")
		if milk == nil || milk.Notes != "2 L" || milk.DueDate != "2026-11-12" {
			t.Fatalf("imported: %+v", milk)
		}
		if bank := todoTitled(e, "Call the bank"); bank == nil || bank.DueAt == nil || bank.DueAt.UTC().Hour() != 1 {
			t.Fatalf("imported with a moment: %+v", bank)
		}

		// The same batch again changes nothing.
		if got := importBatch(t, e, first); got != (ImportResult{Unchanged: 2, Skipped: 1}) {
			t.Fatalf("again: %+v", got)
		}

		// Edited in Persona (title), then the due date changes in Reminders: only the due date is taken.
		e.must("PATCH", "/api/v1/todos/"+milk.ID, `{"title":"Buy oat milk"}`)
		moved := strings.Replace(first, `"due_date":"2026-11-12"`, `"due_date":"2026-11-13"`, 1)
		if got := importBatch(t, e, moved); got != (ImportResult{Updated: 1, Unchanged: 1, Skipped: 1}) {
			t.Fatalf("after a due change: %+v", got)
		}
		if got := e.get(milk.ID); got.Title != "Buy oat milk" || got.DueDate != "2026-11-13" || got.Notes != "2 L" {
			t.Fatalf("after a due change: %+v", got)
		}

		// The due date removed in Reminders goes here too.
		if got := importBatch(t, e, `{"items":[{"external_id":"r1","title":"Buy milk","notes":"2 L"}]}`); got.Updated != 1 {
			t.Fatalf("due removed: %+v", got)
		}
		if got := e.get(milk.ID); got.DueDate != "" || got.DueAt != nil {
			t.Fatalf("due removed: %+v", got)
		}

		// Deleted in Persona: not brought back, even when it changes in Reminders.
		e.must("DELETE", "/api/v1/todos/"+milk.ID, "")
		if got := importBatch(t, e, `{"items":[{"external_id":"r1","title":"Buy milk!"}]}`); got != (ImportResult{Skipped: 1}) {
			t.Fatalf("after delete: %+v", got)
		}
		if todoTitled(e, "Buy milk!") != nil {
			t.Fatal("a deleted to-do came back")
		}
	})
}

func TestImportRemindersRequests(t *testing.T) {
	e := newTodoEnv(t, nil)
	for _, c := range []struct{ name, body string }{
		{"no id", `{"items":[{"title":"x"}]}`},
		{"bad date", `{"items":[{"external_id":"a","title":"x","due_date":"tomorrow"}]}`},
		{"both dues", `{"items":[{"external_id":"a","title":"x","due_date":"2026-11-12","due_at":"2026-11-12T10:00:00Z"}]}`},
		{"too many", `{"items":[` + strings.TrimSuffix(strings.Repeat(`{"external_id":"a","title":"x"},`, importBatchMax+1), ",") + `]}`},
		{"not json", `items`},
	} {
		if w := e.do("POST", importPath, c.body); w.Code != http.StatusBadRequest {
			t.Errorf("%s: %d %s", c.name, w.Code, w.Body)
		}
	}
	if w := e.doAs("answer", "POST", importPath, `{"items":[]}`); w.Code != http.StatusForbidden {
		t.Fatalf("a read-only device: %d", w.Code)
	}
	if len(e.s.todosOf(t.Context(), "")) != 0 {
		t.Fatal("a refused batch made a to-do")
	}
}
