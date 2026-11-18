package db

import (
	"context"
	"slices"
	"testing"
	"time"
)

func TestTodoInboxAndPieceEditsKeepEachOther(t *testing.T) {
	d := open(t)
	ctx := context.Background()
	now := time.Now()
	row := TodoRow{
		ID: "inbox-piece", Title: "Investigate report", CreatedAt: now, UpdatedAt: now,
		InboxItem: "example:old", PartOf: "request", After: []string{"first"},
	}
	if err := d.CreateTodo(ctx, row); err != nil {
		t.Fatal(err)
	}

	source, parent, after := "example:new", "another-request", []string{"second"}
	for _, tc := range []struct {
		name   string
		edit   TodoEdit
		source string
		parent string
		after  []string
	}{
		{"source", TodoEdit{InboxItem: &source}, source, row.PartOf, row.After},
		{"pieces", TodoEdit{PartOf: &parent, After: &after}, source, parent, after},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := d.EditTodo(ctx, row.ID, tc.edit, now); err != nil {
				t.Fatal(err)
			}
			var gotSource, gotParent string
			var gotAfter []string
			if err := d.pool.QueryRow(ctx, `SELECT inbox_item, part_of, after_ids FROM todos WHERE id = $1`, row.ID).
				Scan(&gotSource, &gotParent, &gotAfter); err != nil {
				t.Fatal(err)
			}
			if gotSource != tc.source || gotParent != tc.parent || !slices.Equal(gotAfter, tc.after) {
				t.Fatalf("stored source/pieces: %q, %q, %v; want %q, %q, %v",
					gotSource, gotParent, gotAfter, tc.source, tc.parent, tc.after)
			}
		})
	}
}
