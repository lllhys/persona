package db

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

// TodoSource is where a todo was asked for (#6146): the channel and the chat and message the request was made in, so that
// the todo's outcome can be told there, once. None of the first three is empty in a stored value.
type TodoSource struct {
	Channel   string `json:"channel"`
	ChatID    string `json:"chat_id"`
	MessageID string `json:"message_id"`
	// Anchor: for a todo bound to no topic (a request taken in the owner's private chat with the bot, for want of a home
	// topic group), the id of the bot's first message there. The report's notice is shown under it, and a "/noreply" said as
	// a reply to anything under it stops the report. "" for a todo that has a topic.
	Anchor string `json:"anchor,omitempty"`
	// Handoff is the bot's message that started the run in the main conversation (whether in a topic or in its DM).
	Handoff string `json:"handoff,omitempty"`
	// NoReport: the owner said not to report back. ReportedAt: when the outcome was claimed to be told (RFC 3339), "" until
	// then. ReportText is what will be told and ReportDueAt when (RFC 3339): the topic is shown the text and the time first,
	// so the owner can stop it; both "" until planned. Attempts counts the claims made, a retry being another claim.
	NoReport        bool   `json:"no_report,omitempty"`
	ReportedAt      string `json:"reported_at,omitempty"`
	ReportText      string `json:"report_text,omitempty"`
	ReportFullReply bool   `json:"report_full_reply,omitempty"`
	ReportDueAt     string `json:"report_due_at,omitempty"`
	Attempts        int    `json:"attempts,omitempty"`
}

// ListTodoSources returns the sources of all todos that have one, by todo id.
func (d *DB) ListTodoSources(ctx context.Context) (map[string]TodoSource, error) {
	rows, err := d.pool.Query(ctx, `SELECT id, source FROM todos WHERE source IS NOT NULL`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]TodoSource{}
	for rows.Next() {
		var id string
		var raw []byte
		if err := rows.Scan(&id, &raw); err != nil {
			return nil, err
		}
		var src TodoSource
		if err := json.Unmarshal(raw, &src); err != nil {
			return nil, err
		}
		out[id] = src
	}
	return out, rows.Err()
}

// SetTodoSource records where todo todoID came from (replacing what it had). updated_at is left alone: this is made
// together with the todo. ErrTodoNotFound when the todo is gone.
func (d *DB) SetTodoSource(ctx context.Context, todoID string, src TodoSource) error {
	raw, err := json.Marshal(src)
	if err != nil {
		return err
	}
	tag, err := d.pool.Exec(ctx, `UPDATE todos SET source = $2 WHERE id = $1`, todoID, raw)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrTodoNotFound
	}
	return nil
}

// StopTodoSourceReport records the owner's "don't report back" on todo todoID; changed is false when it has no source or
// already had that. ErrTodoNotFound when the todo is gone.
func (d *DB) StopTodoSourceReport(ctx context.Context, todoID string, at time.Time) (changed bool, err error) {
	tag, err := d.pool.Exec(ctx, `UPDATE todos SET source = source || '{"no_report": true}'::jsonb, updated_at = $2
		WHERE id = $1 AND source IS NOT NULL AND NOT COALESCE((source ->> 'no_report')::boolean, false)`, todoID, at)
	if err != nil {
		return false, err
	}
	if tag.RowsAffected() > 0 {
		return true, nil
	}
	var exists bool
	if err := d.pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM todos WHERE id = $1)`, todoID).Scan(&exists); err != nil {
		return false, err
	}
	if !exists {
		return false, ErrTodoNotFound
	}
	return false, nil
}

// PlanTodoSourceReport records what will be told for todo todoID and when: false for a todo with no source, one the owner
// stopped, one already told, and one already planned (the caller that wins is the one that shows the topic the plan).
func (d *DB) PlanTodoSourceReport(ctx context.Context, todoID, text string, due time.Time, fullReply bool, doneAt *time.Time) (bool, error) {
	tag, err := d.pool.Exec(ctx, `UPDATE todos SET source = source || jsonb_build_object('report_text', $2::text, 'report_due_at', $3::text, 'report_full_reply', $4::boolean)
		WHERE id = $1 AND source IS NOT NULL AND done_at IS NOT DISTINCT FROM $5::timestamptz
		  AND NOT COALESCE((source ->> 'no_report')::boolean, false) AND COALESCE(source ->> 'reported_at', '') = ''
		  AND COALESCE(source ->> 'report_due_at', '') = ''`,
		todoID, text, due.UTC().Format(time.RFC3339Nano), fullReply, doneAt)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}

// ClaimTodoSourceReport takes one try at the report back of todo todoID and says which try it is, and what is to be told
// (the text kept in the store, which another assistant may have planned afresh since): ok is false for a todo with no
// source, one the owner stopped, one already told (by this assistant before a restart, or another on the same store), one
// not planned, one whose planned time (in the store) is after now, and one that has used its maxAttempts. The claim is the
// guard against telling twice and too early, so it is made before the report is sent; now is also when the claim is written.
func (d *DB) ClaimTodoSourceReport(ctx context.Context, todoID string, now time.Time, maxAttempts int) (attempts int, text string, fullReply, ok bool, err error) {
	err = d.pool.QueryRow(ctx, `UPDATE todos SET source = source || jsonb_build_object('reported_at', $2::text,
			'attempts', COALESCE((source ->> 'attempts')::int, 0) + 1)
		WHERE id = $1 AND source IS NOT NULL
		  AND NOT COALESCE((source ->> 'no_report')::boolean, false) AND COALESCE(source ->> 'reported_at', '') = ''
		  AND COALESCE(source ->> 'report_due_at', '') <> '' AND (source ->> 'report_due_at')::timestamptz <= $4::timestamptz
		  AND COALESCE((source ->> 'attempts')::int, 0) < $3
		RETURNING (source ->> 'attempts')::int, COALESCE(source ->> 'report_text', ''), COALESCE((source ->> 'report_full_reply')::boolean, false)`,
		todoID, now.UTC().Format(time.RFC3339Nano), maxAttempts, now.UTC()).Scan(&attempts, &text, &fullReply)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, "", false, false, nil
	}
	return attempts, text, fullReply, err == nil, err
}

// ClearTodoSourcePlan drops the plan of todo todoID (its text and time) when it has not been told yet: it was reopened, so what
// was planned may no longer be what it came to. changed is false when there was no plan, or it was told already.
func (d *DB) ClearTodoSourcePlan(ctx context.Context, todoID string) (changed bool, err error) {
	tag, err := d.pool.Exec(ctx, `UPDATE todos SET source = source - 'report_text' - 'report_due_at' - 'report_full_reply'
		WHERE id = $1 AND source IS NOT NULL AND COALESCE(source ->> 'reported_at', '') = ''
		  AND COALESCE(source ->> 'report_due_at', '') <> ''`, todoID)
	return tag.RowsAffected() > 0, err
}

// ReleaseTodoSourceReport takes a claim back, for a report the hub refused outright (nothing was sent).
func (d *DB) ReleaseTodoSourceReport(ctx context.Context, todoID string) error {
	_, err := d.pool.Exec(ctx, `UPDATE todos SET source = source - 'reported_at' WHERE id = $1 AND source IS NOT NULL`, todoID)
	return err
}
