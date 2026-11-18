package assistant

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/nwverse/persona/server/internal/apierr"
	"github.com/nwverse/persona/server/internal/db"
)

// Seen to-dos (#5707, docs/assistant/todo-status-sop.md "Seen"): the owner looked at a to-do in Just finished, by Got it
// on its row or by opening it, so it is filed under Done (todo_triage.go) instead of waiting out the 24 hours. The mark is
// the to-do's seen_at, kept here so every device agrees. Marking it is not activity: updated_at stays, and a to-do
// that moves after it comes back to Just finished.

// seeTodo marks todo id seen now, or with seen false takes the mark off (Undo); db.ErrTodoNotFound when it is not there.
func (s *Service) seeTodo(ctx context.Context, id string, seen bool) (Todo, error) {
	cur, ok := s.todoNow(id)
	if !ok {
		return Todo{}, db.ErrTodoNotFound
	}
	var at *time.Time
	if seen {
		now := time.Now().UTC()
		at = &now
	}
	s.todos.writing.Lock()
	var row *db.TodoRow
	if st := s.todoDB(); st != nil {
		var err error
		if row, err = st.SeeTodo(ctx, id, at); err != nil {
			s.todos.writing.Unlock()
			return Todo{}, err
		}
	} else {
		r := todoRowOf(cur)
		r.SeenAt = at
		row = &r
	}
	out, _, applied := s.setTodoRow(id, *row, nil)
	s.todos.writing.Unlock()
	if !applied {
		return Todo{}, db.ErrTodoNotFound // deleted meanwhile
	}
	s.todosChanged(ctx) // the other devices read the list again: it moves there too
	return out, nil
}

func (s *Service) todoSeenRoutes() {
	register := func(method string, seen bool) {
		// PUT /api/v1/todos/{id}/seen: the owner saw it (Got it, or its detail opened); DELETE: Undo.
		s.handle(method+" /api/v1/todos/{id}/seen", func(w http.ResponseWriter, r *http.Request) {
			if !fullRole(r) {
				apierr.Write(w, http.StatusForbidden, errors.New("read-only role cannot modify todos"))
				return
			}
			if err := s.ensureTodosLoaded(r.Context()); err != nil {
				apierr.Write(w, http.StatusServiceUnavailable, errTodosUnavailable)
				return
			}
			t, err := s.seeTodo(r.Context(), r.PathValue("id"), seen)
			if errors.Is(err, db.ErrTodoNotFound) {
				apierr.Write(w, http.StatusNotFound, errors.New("todo not found"))
				return
			}
			if err != nil {
				apierr.Write(w, http.StatusInternalServerError, err)
				return
			}
			apierr.WriteJSON(w, map[string]any{"todo": t})
		})
	}
	register("PUT", true)
	register("DELETE", false)
}
