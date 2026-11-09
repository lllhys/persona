package db

import (
	"context"
	"testing"
	"time"
)

// A todo's knowledge-base project (#2581): stored on create, read back by every read, changed and cleared by an edit,
// and kept by an edit of anything else.
func TestTodoProject(t *testing.T) {
	d := open(t)
	ctx := context.Background()
	base := time.Date(2026, 11, 9, 10, 0, 0, 0, time.UTC)
	if err := d.CreateTodo(ctx, TodoRow{ID: "todo_area", Title: "Plan the next release", CreatedAt: base, UpdatedAt: base,
		Project: "community"}); err != nil {
		t.Fatal(err)
	}
	if err := d.CreateTodo(ctx, TodoRow{ID: "todo_none", Title: "Water the plants", CreatedAt: base, UpdatedAt: base}); err != nil {
		t.Fatal(err)
	}
	got, err := d.GetTodo(ctx, "todo_area")
	if err != nil || got.Project != "community" {
		t.Fatalf("stored: %+v %v", got, err)
	}
	list, err := d.ListTodos(ctx)
	if err != nil {
		t.Fatal(err)
	}
	projects := map[string]string{}
	for _, r := range list {
		projects[r.ID] = r.Project
	}
	if projects["todo_area"] != "community" || projects["todo_none"] != "" {
		t.Fatalf("listed: %v", projects)
	}

	notes := "draft the notes"
	row, err := d.EditTodo(ctx, "todo_area", TodoEdit{Notes: &notes}, base.Add(time.Minute))
	if err != nil || row.Project != "community" {
		t.Fatalf("an edit of the notes changed the project: %+v %v", row, err)
	}
	other := "diagnostics"
	if row, err = d.EditTodo(ctx, "todo_area", TodoEdit{Project: &other}, base.Add(2*time.Minute)); err != nil || row.Project != other {
		t.Fatalf("moved: %+v %v", row, err)
	}
	if got, err = d.GetTodo(ctx, "todo_area"); err != nil || got.Project != other || !got.UpdatedAt.Equal(base.Add(2*time.Minute)) {
		t.Fatalf("moved, read back: %+v %v", got, err)
	}
	none := ""
	if row, err = d.EditTodo(ctx, "todo_area", TodoEdit{Project: &none}, base.Add(3*time.Minute)); err != nil || row.Project != "" {
		t.Fatalf("cleared: %+v %v", row, err)
	}
	// The other writers return whole rows too.
	if r, err := d.FailTodo(ctx, "todo_none", "no launcher", base.Add(4*time.Minute)); err != nil || r.Project != "" {
		t.Fatalf("failed: %+v %v", r, err)
	}
}
