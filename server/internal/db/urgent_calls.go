package db

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

// ---- urgent calls (urgent_calls, docs/assistant/urgent-call.md) ----

// UrgentCall is one matter the assistant rang the owner's phone about.
type UrgentCall struct {
	ID            string
	MachineID     string // the assistant's machine: a push about the call opens Conversation there
	Conversation  string
	Matter        string // what makes two calls the same matter: the card, the related key, else the title
	Title         string
	Body          string
	Why           string // owner_asked | rule | test
	RuleID        string
	RuleText      string // the rule's words when it rang
	Happening     string
	OwnerQuote    string
	InteractionID string
	State         string // ringing | answered | declined | missed | ended | failed | cancelled
	Attempt       int
	CreatedAt     time.Time
	RangAt        *time.Time
	NextAt        *time.Time
	Step          string // what NextAt is for: ring_timeout | retry | remind
	Reminders     int
	AnsweredAt    *time.Time
	EndedAt       *time.Time
	Seconds       int
	Outcome       string
	Transcript    string
	Version       int
}

const urgentCallColumns = `id, machine_id, conversation, matter, title, body, why, rule_id, rule_text, happening, owner_quote,
	interaction_id, state, attempt, created_at, rang_at, next_at, step, reminders, answered_at, ended_at, seconds, outcome,
	transcript, version`

func scanUrgentCall(r pgx.Row) (UrgentCall, error) {
	var c UrgentCall
	err := r.Scan(&c.ID, &c.MachineID, &c.Conversation, &c.Matter, &c.Title, &c.Body, &c.Why, &c.RuleID, &c.RuleText, &c.Happening,
		&c.OwnerQuote, &c.InteractionID, &c.State, &c.Attempt, &c.CreatedAt, &c.RangAt, &c.NextAt, &c.Step, &c.Reminders,
		&c.AnsweredAt, &c.EndedAt, &c.Seconds, &c.Outcome, &c.Transcript, &c.Version)
	return c, err
}

// InsertUrgentCall keeps a new call (its version starts at 0).
func (d *DB) InsertUrgentCall(ctx context.Context, c UrgentCall) error {
	_, err := d.pool.Exec(ctx, `INSERT INTO urgent_calls (id, machine_id, conversation, matter, title, body, why, rule_id, rule_text,
		happening, owner_quote, interaction_id, state, attempt, created_at, rang_at, next_at, step)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18)`,
		c.ID, c.MachineID, c.Conversation, c.Matter, c.Title, c.Body, c.Why, c.RuleID, c.RuleText, c.Happening, c.OwnerQuote,
		c.InteractionID, c.State, c.Attempt, c.CreatedAt.UTC(), c.RangAt, c.NextAt, c.Step)
	return err
}

// UrgentCall is the call with this id; false when there is none.
func (d *DB) UrgentCall(ctx context.Context, id string) (UrgentCall, bool, error) {
	c, err := scanUrgentCall(d.pool.QueryRow(ctx, `SELECT `+urgentCallColumns+` FROM urgent_calls WHERE id = $1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return UrgentCall{}, false, nil
	}
	return c, err == nil, err
}

// UrgentCallsSince are the calls created at or after since, oldest first (the day's count, the gap between matters,
// whether a matter rang before).
func (d *DB) UrgentCallsSince(ctx context.Context, since time.Time) ([]UrgentCall, error) {
	return d.urgentCalls(ctx, `SELECT `+urgentCallColumns+` FROM urgent_calls WHERE created_at >= $1 ORDER BY created_at, id`, since.UTC())
}

// UrgentCallsDue are the calls whose next step is due by now, soonest first, and when the next one after them is
// (zero: none).
func (d *DB) UrgentCallsDue(ctx context.Context, now time.Time) ([]UrgentCall, time.Time, error) {
	due, err := d.urgentCalls(ctx, `SELECT `+urgentCallColumns+` FROM urgent_calls WHERE next_at IS NOT NULL AND next_at <= $1
		ORDER BY next_at, id`, now.UTC())
	if err != nil {
		return nil, time.Time{}, err
	}
	var next *time.Time
	err = d.pool.QueryRow(ctx, `SELECT min(next_at) FROM urgent_calls WHERE next_at > $1`, now.UTC()).Scan(&next)
	if err != nil || next == nil {
		return due, time.Time{}, err
	}
	return due, *next, nil
}

// UpdateUrgentCall writes c's changing fields when the stored row is still at c.Version, and moves the version on.
// false: someone changed it first (another report from the phone, the loop, a second manager); read it again.
func (d *DB) UpdateUrgentCall(ctx context.Context, c UrgentCall) (bool, error) {
	tag, err := d.pool.Exec(ctx, `UPDATE urgent_calls SET state = $3, attempt = $4, rang_at = $5, next_at = $6, step = $7,
		reminders = $8, answered_at = $9, ended_at = $10, seconds = $11, outcome = $12, transcript = $13, version = version + 1
		WHERE id = $1 AND version = $2`,
		c.ID, c.Version, c.State, c.Attempt, c.RangAt, c.NextAt, c.Step, c.Reminders, c.AnsweredAt, c.EndedAt, c.Seconds,
		c.Outcome, c.Transcript)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() == 1, nil
}

func (d *DB) urgentCalls(ctx context.Context, q string, args ...any) ([]UrgentCall, error) {
	rows, err := d.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []UrgentCall
	for rows.Next() {
		c, err := scanUrgentCall(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}
