package assistant

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/nwverse/persona/server/internal/apierr"
	"github.com/nwverse/persona/server/internal/db"
)

// To-dos imported from the iPhone's Reminders (#4009, docs/assistant/todos.md "Imported from Reminders"). One way only:
// the phone sends the reminders of the lists the user picked, and each becomes a to-do the first time it is seen; later
// changes of its title, notes or due date in Reminders change the to-do, each field only when it changed there. Nothing
// is written back, and completing or deleting a reminder leaves its to-do alone. A to-do deleted in Persona is not
// imported again.

const (
	importBatchMax      = 1000 // reminders in one request
	importExternalIDMax = 512  // characters of an item's id
)

// todoImportStore keeps the links between imported items and their to-dos (db/todo_imports.go).
type todoImportStore interface {
	GetTodoImport(ctx context.Context, source, externalID string) (db.TodoImport, bool, error)
	InsertTodoImport(ctx context.Context, in db.TodoImport, at time.Time) (bool, error)
	SetTodoImportFingerprint(ctx context.Context, source, externalID, fingerprint string, at time.Time) error
}

var _ todoImportStore = (*db.DB)(nil)

// todoImports are the links: in the store when there is one, otherwise in memory (tests, a store-less assistant).
// Both are used under s.todos.importing only.
type todoImports struct {
	st  todoImportStore
	mem map[[2]string]db.TodoImport
}

func (s *Service) todoImportLinks() todoImports {
	if st, ok := s.store().(todoImportStore); ok && s.todoDB() != nil {
		return todoImports{st: st}
	}
	if s.todos.imports == nil { // made and used under s.todos.importing
		s.todos.imports = map[[2]string]db.TodoImport{}
	}
	return todoImports{mem: s.todos.imports}
}

func (l todoImports) get(ctx context.Context, source, id string) (db.TodoImport, bool, error) {
	if l.st != nil {
		return l.st.GetTodoImport(ctx, source, id)
	}
	in, ok := l.mem[[2]string{source, id}]
	return in, ok, nil
}

func (l todoImports) insert(ctx context.Context, in db.TodoImport, at time.Time) (bool, error) {
	if l.st != nil {
		return l.st.InsertTodoImport(ctx, in, at)
	}
	k := [2]string{in.Source, in.ExternalID}
	if _, ok := l.mem[k]; ok {
		return false, nil
	}
	l.mem[k] = in
	return true, nil
}

func (l todoImports) setFingerprint(ctx context.Context, source, id, fp string, at time.Time) error {
	if l.st != nil {
		return l.st.SetTodoImportFingerprint(ctx, source, id, fp, at)
	}
	k := [2]string{source, id}
	in := l.mem[k]
	in.Fingerprint = fp
	l.mem[k] = in
	return nil
}

// importedReminder is one reminder as the phone sends it.
type importedReminder struct {
	ExternalID string          `json:"external_id"`
	Title      string          `json:"title"`
	Notes      string          `json:"notes"`
	DueAt      json.RawMessage `json:"due_at"`
	DueDate    json.RawMessage `json:"due_date"`
}

// ImportResult says what a batch did: to-dos made, to-dos changed, items that matched what was last taken, items left
// out (no title, or their to-do was deleted in Persona).
type ImportResult struct {
	Created   int `json:"created"`
	Updated   int `json:"updated"`
	Unchanged int `json:"unchanged"`
	Skipped   int `json:"skipped"`
}

// importFields is what a to-do takes from a reminder, and fingerprint the hashes it keeps of each.
type importFields struct {
	title, notes string
	due          db.TodoEdit // SetDue with DueAt or DueDate (or neither)
}

func fieldHash(v string) string {
	sum := sha256.Sum256([]byte(v))
	return hex.EncodeToString(sum[:8])
}

func (f importFields) dueKey() string {
	switch {
	case f.due.DueAt != nil:
		return "at:" + f.due.DueAt.UTC().Format(time.RFC3339)
	case f.due.DueDate != nil:
		return "day:" + f.due.DueDate.Format(dayLayout)
	}
	return ""
}

// fingerprint is "<title>.<notes>.<due>", a short hash of each, so a later import can tell which of them changed.
func (f importFields) fingerprint() string {
	return fieldHash(f.title) + "." + fieldHash(f.notes) + "." + fieldHash(f.dueKey())
}

// changedSince is the edit that brings a to-do from what was taken last (fingerprint old) to f: only the fields that
// changed in Reminders, so what the user changed in Persona stays unless Reminders changed it too. ok is false when
// nothing changed.
func (f importFields) changedSince(old string) (db.TodoEdit, bool) {
	was := strings.Split(old, ".")
	if len(was) != 3 {
		was = []string{"", "", ""} // unreadable: take all of it
	}
	now := strings.Split(f.fingerprint(), ".")
	var e db.TodoEdit
	ok := false
	if now[0] != was[0] {
		t := f.title
		e.Title, ok = &t, true
	}
	if now[1] != was[1] {
		n := f.notes
		e.Notes, ok = &n, true
	}
	if now[2] != was[2] {
		e.SetDue, e.DueAt, e.DueDate, ok = true, f.due.DueAt, f.due.DueDate, true
	}
	return e, ok
}

// readReminder checks an item: its id and its fields as a to-do takes them. skip is true for one without a title
// (Reminders allows them; a to-do needs one).
func readReminder(in importedReminder) (id string, f importFields, skip bool, err error) {
	id = strings.TrimSpace(in.ExternalID)
	if id == "" {
		return "", f, false, errors.New("external_id cannot be empty")
	}
	if utf8.RuneCountInString(id) > importExternalIDMax {
		return "", f, false, fmt.Errorf("external_id is longer than %d characters", importExternalIDMax)
	}
	plan := todoPlanIn{DueAt: in.DueAt, DueDate: in.DueDate}
	if in.DueAt == nil && in.DueDate == nil {
		plan.DueAt = json.RawMessage("null") // no due date in Reminders: none here either
	}
	if err := plan.edit(&f.due); err != nil {
		return "", f, false, fmt.Errorf("%s: %w", id, err)
	}
	f.title, f.notes = strings.TrimSpace(in.Title), strings.TrimSpace(in.Notes)
	return id, f, f.title == "", nil
}

// importReminders takes a batch, one item at a time; the first error stops it (what was done stays done, and the next
// import picks up the rest).
func (s *Service) importReminders(ctx context.Context, items []importedReminder) (ImportResult, error) {
	var res ImportResult
	type item struct {
		id     string
		f      importFields
		noName bool
	}
	read := make([]item, 0, len(items))
	for _, in := range items {
		id, f, skip, err := readReminder(in)
		if err != nil {
			return res, &badImport{err}
		}
		read = append(read, item{id, f, skip})
	}

	s.todos.importing.Lock() // one batch at a time: two at once would make two to-dos of a new reminder
	defer s.todos.importing.Unlock()
	links := s.todoImportLinks()
	const src = db.TodoImportSourceReminders
	for _, it := range read {
		now := time.Now().UTC()
		link, seen, err := links.get(ctx, src, it.id)
		if err != nil {
			return res, err
		}
		switch {
		case it.noName:
			res.Skipped++
		case !seen:
			t, err := s.createPlainTodo(ctx, it.f.title, it.f.notes, it.f.due)
			if err != nil {
				return res, err
			}
			ok, err := links.insert(ctx, db.TodoImport{Source: src, ExternalID: it.id, TodoID: t.ID, Fingerprint: it.f.fingerprint()}, now)
			if err != nil || !ok {
				_ = s.removeTodo(ctx, t.ID) // another manager imported it first: keep theirs
				if err != nil {
					return res, err
				}
				res.Unchanged++
				continue
			}
			res.Created++
		default:
			if _, alive := s.todoNow(link.TodoID); link.TodoID == "" || !alive {
				res.Skipped++ // deleted in Persona: not brought back
				continue
			}
			e, changed := it.f.changedSince(link.Fingerprint)
			if !changed {
				res.Unchanged++
				continue
			}
			if _, err := s.editTodo(ctx, link.TodoID, e); errors.Is(err, db.ErrTodoNotFound) {
				res.Skipped++
				continue
			} else if err != nil {
				return res, err
			}
			if err := links.setFingerprint(ctx, src, it.id, it.f.fingerprint(), now); err != nil {
				return res, err
			}
			res.Updated++
		}
	}
	return res, nil
}

// badImport is a batch the phone sent wrong (400), as against one the store could not take (500).
type badImport struct{ error }

func (e *badImport) Unwrap() error { return e.error }

// todoImportRoutes: POST /api/v1/todos/import/reminders {items: [{external_id, title, notes?, due_at? | due_date?}]}
// → ImportResult. A full device only: the phone that reads its Reminders.
func (s *Service) todoImportRoutes() {
	s.handle("POST /api/v1/todos/import/reminders", func(w http.ResponseWriter, r *http.Request) {
		if !fullRole(r) {
			apierr.Write(w, http.StatusForbidden, errors.New("read-only role cannot modify todos"))
			return
		}
		if err := s.ensureTodosLoaded(r.Context()); err != nil {
			apierr.Write(w, http.StatusServiceUnavailable, errTodosUnavailable)
			return
		}
		var in struct {
			Items []importedReminder `json:"items"`
		}
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			apierr.Write(w, http.StatusBadRequest, err)
			return
		}
		if len(in.Items) > importBatchMax {
			apierr.Write(w, http.StatusBadRequest, fmt.Errorf("at most %d items at a time", importBatchMax))
			return
		}
		res, err := s.importReminders(r.Context(), in.Items)
		var bad *badImport
		switch {
		case errors.As(err, &bad):
			apierr.Write(w, http.StatusBadRequest, bad.error)
		case err != nil:
			apierr.Write(w, http.StatusInternalServerError, err)
		default:
			apierr.WriteJSON(w, res)
		}
	})
}
