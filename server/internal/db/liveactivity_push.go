package db

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

// ClaimLiveActivityStart spends the hub's rolling push-to-start budget before dispatch.
// The activity row lock also excludes a token registration that committed before the
// claim. No client timestamp or token renewal resets this independent delivery state.
// A claim is stamped at (in microseconds) and is always later than the device's last one,
// so that time names it to HoldLiveActivityStart.
// A denied claim returns its due time, or zero if the persisted token no longer needs
// a start. An error (including an ambiguous commit) must never authorize a push.
func (d *DB) ClaimLiveActivityStart(ctx context.Context, device, startToken string, at time.Time, cooldown, maxAge time.Duration) (due time.Time, claimed bool, err error) {
	tx, err := d.pool.Begin(ctx)
	if err != nil {
		return due, false, err
	}
	defer tx.Rollback(ctx)
	// Lock the parent before the activity: retirement/revocation locks devices
	// before deleting activity rows. A reversed order can deadlock at the FK check.
	var parent int
	err = tx.QueryRow(ctx, `SELECT 1 FROM devices WHERE device_id=$1 FOR KEY SHARE`, device).Scan(&parent)
	if errors.Is(err, pgx.ErrNoRows) {
		return due, false, nil
	}
	if err != nil {
		return due, false, err
	}
	var updateToken, currentStart string
	var started *time.Time
	err = tx.QueryRow(ctx, `SELECT push_token, push_to_start_token, started_at FROM live_activities WHERE device_id = $1 FOR UPDATE`, device).Scan(&updateToken, &currentStart, &started)
	if errors.Is(err, pgx.ErrNoRows) {
		return due, false, nil
	}
	if err != nil {
		return due, false, err
	}
	if startToken == "" || currentStart != startToken || (updateToken != "" && started != nil && at.Sub(*started) < maxAge) {
		return due, false, nil
	}
	at = at.UTC().Truncate(time.Microsecond)
	err = tx.QueryRow(ctx, `INSERT INTO live_activity_start_budget (device_id, claimed_at, next_start_at)
		VALUES ($1, $2, $3)
		ON CONFLICT (device_id) DO UPDATE SET claimed_at = EXCLUDED.claimed_at, next_start_at = EXCLUDED.next_start_at
		WHERE live_activity_start_budget.next_start_at <= EXCLUDED.claimed_at
			AND live_activity_start_budget.claimed_at < EXCLUDED.claimed_at
		RETURNING next_start_at`, device, at, at.Add(cooldown)).Scan(&due)
	if errors.Is(err, pgx.ErrNoRows) {
		err = tx.QueryRow(ctx, `SELECT next_start_at FROM live_activity_start_budget WHERE device_id = $1`, device).Scan(&due)
		return due, false, err
	}
	if err != nil {
		return time.Time{}, false, err
	}
	if err = tx.Commit(ctx); err != nil {
		return time.Time{}, false, err
	}
	return due, true, nil
}

// HoldLiveActivityStart sets how long after the device's last claimed start the next one may be claimed. The hub holds
// it for as long as an activity can run once it has sent a start, since that activity may be up where no update reaches
// it, and puts the cooldown back when the phone answers or the start is refused.
//
// asOf says which start the write is about: the time that start was claimed at (the at of its ClaimLiveActivityStart)
// for its hold and for its refusal, the time the phone answered for an answer. A start claimed after asOf is left as it
// is, so a write that arrives late (a slow store, a push answered after its retries, the other slot) cannot move the
// deadline of a newer start. claimed_at tells the claims of a device apart: each is later than the one before it
// (ClaimLiveActivityStart), in microseconds, which is what the column keeps. The row lock orders the write against a
// claim in flight: it sees that claim's row or runs before it.
//
// changed reports whether the deadline moved: false when it already was that, when the device has no claim, and when
// its last claim is after asOf.
func (d *DB) HoldLiveActivityStart(ctx context.Context, device string, asOf time.Time, hold time.Duration) (changed bool, err error) {
	tag, err := d.pool.Exec(ctx, `UPDATE live_activity_start_budget
		SET next_start_at = claimed_at + $3 * interval '1 microsecond'
		WHERE device_id = $1 AND claimed_at <= $2 AND next_start_at <> claimed_at + $3 * interval '1 microsecond'`,
		device, asOf.UTC().Truncate(time.Microsecond), hold.Microseconds())
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}
