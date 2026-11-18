package db

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

// TodoImport links an item of another app (Source, ExternalID) to the to-do it was imported as (#4009). TodoID is ""
// once that to-do was deleted: the item is then left alone. Fingerprint is what was last taken from the item.
type TodoImport struct {
	Source      string
	ExternalID  string
	TodoID      string
	Fingerprint string
}

// TodoImportSourceReminders is the iPhone's Reminders app.
const TodoImportSourceReminders = "reminders"

// GetTodoImport returns the link of item (source, externalID); ok is false when it was never imported.
func (d *DB) GetTodoImport(ctx context.Context, source, externalID string) (TodoImport, bool, error) {
	out := TodoImport{Source: source, ExternalID: externalID}
	err := d.pool.QueryRow(ctx, `SELECT COALESCE(todo_id, ''), fingerprint FROM todo_imports WHERE source = $1 AND external_id = $2`,
		source, externalID).Scan(&out.TodoID, &out.Fingerprint)
	if errors.Is(err, pgx.ErrNoRows) {
		return TodoImport{}, false, nil
	}
	if err != nil {
		return TodoImport{}, false, err
	}
	return out, true, nil
}

// InsertTodoImport records a new link; false when the item already has one (another import got there first).
func (d *DB) InsertTodoImport(ctx context.Context, in TodoImport, at time.Time) (bool, error) {
	tag, err := d.pool.Exec(ctx, `INSERT INTO todo_imports (source, external_id, todo_id, fingerprint, created_at, updated_at)
		VALUES ($1, $2, NULLIF($3, ''), $4, $5, $5) ON CONFLICT (source, external_id) DO NOTHING`,
		in.Source, in.ExternalID, in.TodoID, in.Fingerprint, at)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}

// SetTodoImportFingerprint records what was last taken from the item.
func (d *DB) SetTodoImportFingerprint(ctx context.Context, source, externalID, fingerprint string, at time.Time) error {
	_, err := d.pool.Exec(ctx, `UPDATE todo_imports SET fingerprint = $3, updated_at = $4 WHERE source = $1 AND external_id = $2`,
		source, externalID, fingerprint, at)
	return err
}
