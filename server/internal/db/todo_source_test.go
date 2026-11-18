package db

import (
	"context"
	"errors"
	"testing"
	"time"
)

// A todo remembers where it was asked; the report back is claimed once; the owner can stop it; the value goes with the todo.
func TestTodoSource(t *testing.T) {
	d := open(t)
	ctx := context.Background()
	base := time.Date(2026, 11, 7, 10, 0, 0, 0, time.UTC)
	for _, id := range []string{"todo_a", "todo_b", "todo_c"} {
		if err := d.CreateTodo(ctx, TodoRow{ID: id, Title: id, CreatedAt: base, UpdatedAt: base}); err != nil {
			t.Fatal(err)
		}
	}
	src := TodoSource{Channel: "feishu", ChatID: "oc_test_src", MessageID: "om_test_src"}
	if err := d.SetTodoSource(ctx, "todo_a", src); err != nil {
		t.Fatal(err)
	}
	if err := d.SetTodoSource(ctx, "todo_b", src); err != nil { // not unique: two todos from one message
		t.Fatalf("a second todo from the same message: %v", err)
	}
	if err := d.SetTodoSource(ctx, "todo_gone", src); !errors.Is(err, ErrTodoNotFound) {
		t.Fatalf("a todo that is gone: %v", err)
	}
	all, err := d.ListTodoSources(ctx)
	if err != nil || len(all) != 2 || all["todo_a"] != src {
		t.Fatalf("list = %+v, %v", all, err)
	}
	if got, _ := d.GetTodo(ctx, "todo_a"); !got.UpdatedAt.Equal(base) {
		t.Fatalf("setting a source moved updated_at to %v", got.UpdatedAt)
	}

	// Nothing is claimed before a report is planned; a plan is made once; one claim wins; the second, and one after a
	// restart (a fresh read), lose.
	at := base.Add(time.Hour)
	late := at.Add(11 * time.Minute) // after the plan below falls due
	if _, _, _, ok, err := d.ClaimTodoSourceReport(ctx, "todo_a", late, 2); err != nil || ok {
		t.Fatalf("a claim before the plan: %v %v", ok, err)
	}
	due := at.Add(10 * time.Minute)
	if ok, err := d.PlanTodoSourceReport(ctx, "todo_a", "做完了", due, false, nil); err != nil || !ok {
		t.Fatalf("plan: %v %v", ok, err)
	}
	if ok, _ := d.PlanTodoSourceReport(ctx, "todo_a", "另一句", due.Add(time.Hour), false, nil); ok {
		t.Fatal("planned twice")
	}
	if ok, _ := d.PlanTodoSourceReport(ctx, "todo_c", "x", due, false, nil); ok {
		t.Fatal("a todo with no source was planned")
	}
	all, _ = d.ListTodoSources(ctx)
	if all["todo_a"].ReportText != "做完了" || all["todo_a"].ReportDueAt != due.Format(time.RFC3339Nano) {
		t.Fatalf("after the plan: %+v", all["todo_a"])
	}
	// Not before the time the store has for it, whatever the caller thinks, and what is claimed is the text the store has.
	if _, _, _, ok, err := d.ClaimTodoSourceReport(ctx, "todo_a", due.Add(-time.Second), 2); err != nil || ok {
		t.Fatalf("a claim before the planned time: %v %v", ok, err)
	}
	if n, text, _, ok, err := d.ClaimTodoSourceReport(ctx, "todo_a", late, 2); err != nil || !ok || n != 1 || text != "做完了" {
		t.Fatalf("first claim: %d %q %v %v", n, text, ok, err)
	}
	if _, _, _, ok, err := d.ClaimTodoSourceReport(ctx, "todo_a", late.Add(time.Minute), 2); err != nil || ok {
		t.Fatalf("second claim: %v %v", ok, err)
	}
	all, _ = d.ListTodoSources(ctx)
	if all["todo_a"].ReportedAt != late.Format(time.RFC3339Nano) || all["todo_a"].ChatID != src.ChatID || all["todo_a"].Attempts != 1 {
		t.Fatalf("after the claim: %+v", all["todo_a"])
	}
	// A claim that is let go can be made again, as the second try; the tries are bounded; a todo with no source cannot be claimed.
	if err := d.ReleaseTodoSourceReport(ctx, "todo_a"); err != nil {
		t.Fatal(err)
	}
	if n, _, _, ok, _ := d.ClaimTodoSourceReport(ctx, "todo_a", late, 2); !ok || n != 2 {
		t.Fatalf("a released claim could not be made again: %d %v", n, ok)
	}
	_ = d.ReleaseTodoSourceReport(ctx, "todo_a")
	if _, _, _, ok, _ := d.ClaimTodoSourceReport(ctx, "todo_a", late, 2); ok {
		t.Fatal("a third try was allowed past the bound")
	}
	if _, _, _, ok, _ := d.ClaimTodoSourceReport(ctx, "todo_c", late, 2); ok {
		t.Fatal("a todo with no source was claimed")
	}

	// A plan is dropped (the todo was reopened) while it has not been told, and a new one can be made; not once told.
	if err := d.CreateTodo(ctx, TodoRow{ID: "todo_d", Title: "d", CreatedAt: base, UpdatedAt: base}); err != nil {
		t.Fatal(err)
	}
	if err := d.SetTodoSource(ctx, "todo_d", src); err != nil {
		t.Fatal(err)
	}
	if ok, _ := d.PlanTodoSourceReport(ctx, "todo_d", "第一句", due, false, nil); !ok {
		t.Fatal("not planned")
	}
	if changed, err := d.ClearTodoSourcePlan(ctx, "todo_d"); err != nil || !changed {
		t.Fatalf("clear: %v %v", changed, err)
	}
	if changed, _ := d.ClearTodoSourcePlan(ctx, "todo_d"); changed {
		t.Fatal("cleared a plan that was not there")
	}
	if ok, _ := d.PlanTodoSourceReport(ctx, "todo_d", "第二句", due, false, nil); !ok {
		t.Fatal("not planned again")
	}
	if _, _, _, ok, _ := d.ClaimTodoSourceReport(ctx, "todo_d", late, 2); !ok {
		t.Fatal("not claimed")
	}
	if changed, _ := d.ClearTodoSourcePlan(ctx, "todo_d"); changed {
		t.Fatal("dropped the plan of a report that was told")
	}
	if all, _ := d.ListTodoSources(ctx); all["todo_d"].ReportText != "第二句" {
		t.Fatalf("the told report lost its text: %+v", all["todo_d"])
	}

	// "Don't report back" stops the claim, and says whether it changed anything.
	if changed, err := d.StopTodoSourceReport(ctx, "todo_b", at); err != nil || !changed {
		t.Fatalf("stop: %v %v", changed, err)
	}
	if changed, _ := d.StopTodoSourceReport(ctx, "todo_b", at); changed {
		t.Fatal("stopping twice changed it")
	}
	if changed, err := d.StopTodoSourceReport(ctx, "todo_c", at); err != nil || changed {
		t.Fatalf("stop with no source: %v %v", changed, err)
	}
	if _, err := d.StopTodoSourceReport(ctx, "todo_gone", at); !errors.Is(err, ErrTodoNotFound) {
		t.Fatalf("stop on a todo that is gone: %v", err)
	}
	_, _ = d.PlanTodoSourceReport(ctx, "todo_b", "x", due, false, nil)
	if _, _, _, ok, _ := d.ClaimTodoSourceReport(ctx, "todo_b", late, 2); ok {
		t.Fatal("a stopped report was claimed")
	}
	if ok, _ := d.PlanTodoSourceReport(ctx, "todo_b", "x", due, false, nil); ok {
		t.Fatal("a stopped report was planned")
	}

	// Deleting the todo deletes the source with it.
	if err := d.DeleteTodo(ctx, "todo_a"); err != nil {
		t.Fatal(err)
	}
	if all, _ := d.ListTodoSources(ctx); len(all) != 2 { // todo_b and todo_d
		t.Fatalf("after delete: %+v", all)
	}
}
