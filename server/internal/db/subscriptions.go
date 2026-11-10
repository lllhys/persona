package db

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

// ---- source subscriptions (#2038, internal/subscriptions) ----

// SubscriptionRow is one subscriptions row: data is the subscription as internal/subscriptions keeps it.
type SubscriptionRow struct {
	ID, Connector, Source string
	Revision              int64
	Data                  []byte
	LeaseUntil            *time.Time
	CreatedAt, UpdatedAt  time.Time
}

// SubscriptionSeenRow is a dedup key a run adds.
type SubscriptionSeenRow struct {
	UID, ContentHash string
}

// SubscriptionItemRow is an item a run holds for the destinations.
type SubscriptionItemRow struct {
	UID  string
	Data []byte
}

// Retention of subscription_seen, a subscription's.
const (
	subscriptionSeenKeep   = 5000
	subscriptionSeenMaxAge = 400 * 24 * time.Hour
)

const subscriptionCols = `id, connector, source, revision, data, lease_until, created_at, updated_at`

func scanSubscription(row pgx.Row) (SubscriptionRow, error) {
	var r SubscriptionRow
	err := row.Scan(&r.ID, &r.Connector, &r.Source, &r.Revision, &r.Data, &r.LeaseUntil, &r.CreatedAt, &r.UpdatedAt)
	return r, err
}

// Subscriptions lists every subscription, oldest first.
func (d *DB) Subscriptions(ctx context.Context) ([]SubscriptionRow, error) {
	rows, err := d.pool.Query(ctx, `SELECT `+subscriptionCols+` FROM subscriptions ORDER BY created_at, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SubscriptionRow
	for rows.Next() {
		r, err := scanSubscription(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// Subscription is one subscription, nil when there is none with id.
func (d *DB) Subscription(ctx context.Context, id string) (*SubscriptionRow, error) {
	r, err := scanSubscription(d.pool.QueryRow(ctx, `SELECT `+subscriptionCols+` FROM subscriptions WHERE id = $1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &r, nil
}

// CreateSubscription adds a subscription.
func (d *DB) CreateSubscription(ctx context.Context, r SubscriptionRow) error {
	_, err := d.pool.Exec(ctx, `INSERT INTO subscriptions (`+subscriptionCols+`) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		r.ID, r.Connector, r.Source, r.Revision, r.Data, r.LeaseUntil, r.CreatedAt, r.UpdatedAt)
	return err
}

// UpdateSubscription writes r over the subscription if it is still at revision prev; false when it is not (or gone).
// The lease is left as it is.
func (d *DB) UpdateSubscription(ctx context.Context, r SubscriptionRow, prev int64) (bool, error) {
	tag, err := d.pool.Exec(ctx, `UPDATE subscriptions SET connector = $2, source = $3, revision = $4, data = $5, updated_at = $6
		WHERE id = $1 AND revision = $7`, r.ID, r.Connector, r.Source, r.Revision, r.Data, r.UpdatedAt, prev)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() == 1, nil
}

// DeleteSubscription removes a subscription with its dedup keys and pending items.
func (d *DB) DeleteSubscription(ctx context.Context, id string) error {
	_, err := d.pool.Exec(ctx, `DELETE FROM subscriptions WHERE id = $1`, id)
	return err
}

// LeaseSubscription holds id for a run until until, unless a run holds it past now; false when one does (or it is gone).
func (d *DB) LeaseSubscription(ctx context.Context, id string, now, until time.Time) (bool, error) {
	tag, err := d.pool.Exec(ctx, `UPDATE subscriptions SET lease_until = $2 WHERE id = $1 AND (lease_until IS NULL OR lease_until <= $3)`,
		id, until, now)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() == 1, nil
}

// SubscriptionSeen says which of uids subscription id has seen.
func (d *DB) SubscriptionSeen(ctx context.Context, id string, uids []string) (map[string]bool, error) {
	out := map[string]bool{}
	if len(uids) == 0 {
		return out, nil
	}
	rows, err := d.pool.Query(ctx, `SELECT uid FROM subscription_seen WHERE subscription_id = $1 AND uid = ANY($2)`, id, uids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var u string
		if err := rows.Scan(&u); err != nil {
			return nil, err
		}
		out[u] = true
	}
	return out, rows.Err()
}

// CommitSubscriptionRun writes a run of subscription id at once: merge turns the stored row's data and revision into
// the data to keep, the new dedup keys and items are added, the old keys pruned, and the lease released. A subscription
// deleted meanwhile keeps nothing.
func (d *DB) CommitSubscriptionRun(ctx context.Context, id string, merge func(data []byte, revision int64) ([]byte, error),
	seen []SubscriptionSeenRow, items []SubscriptionItemRow, now time.Time) error {
	tx, err := d.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	var data []byte
	var revision int64
	err = tx.QueryRow(ctx, `SELECT data, revision FROM subscriptions WHERE id = $1 FOR UPDATE`, id).Scan(&data, &revision)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	kept, err := merge(data, revision)
	if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE subscriptions SET data = $2, lease_until = NULL WHERE id = $1`, id, kept); err != nil {
		return err
	}
	if len(seen) > 0 {
		uids, hashes := make([]string, len(seen)), make([]string, len(seen))
		for i, s := range seen {
			uids[i], hashes[i] = s.UID, s.ContentHash
		}
		if _, err := tx.Exec(ctx, `INSERT INTO subscription_seen (subscription_id, uid, content_hash, first_seen_at)
			SELECT $1, u, h, $4 FROM unnest($2::text[], $3::text[]) AS t(u, h)
			ON CONFLICT (subscription_id, uid) DO NOTHING`, id, uids, hashes, now); err != nil {
			return err
		}
	}
	for _, it := range items {
		if _, err := tx.Exec(ctx, `INSERT INTO subscription_items (subscription_id, uid, data, created_at) VALUES ($1, $2, $3, $4)`,
			id, it.UID, it.Data, now); err != nil {
			return err
		}
	}
	if len(seen) > 0 {
		if _, err := tx.Exec(ctx, `DELETE FROM subscription_seen WHERE subscription_id = $1 AND (first_seen_at < $2 OR uid IN (
			SELECT uid FROM subscription_seen WHERE subscription_id = $1 ORDER BY first_seen_at DESC, uid OFFSET $3))`,
			id, now.Add(-subscriptionSeenMaxAge), subscriptionSeenKeep); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

// SubscriptionItems are the items held for subscription id after seq, oldest first, at most limit (#2039 reads them).
func (d *DB) SubscriptionItems(ctx context.Context, id string, after int64, limit int) ([]int64, []SubscriptionItemRow, error) {
	rows, err := d.pool.Query(ctx, `SELECT seq, uid, data FROM subscription_items WHERE subscription_id = $1 AND seq > $2
		ORDER BY seq LIMIT $3`, id, after, limit)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	var seqs []int64
	var out []SubscriptionItemRow
	for rows.Next() {
		var seq int64
		var r SubscriptionItemRow
		if err := rows.Scan(&seq, &r.UID, &r.Data); err != nil {
			return nil, nil, err
		}
		seqs, out = append(seqs, seq), append(out, r)
	}
	return seqs, out, rows.Err()
}
