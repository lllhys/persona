package db

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/nwverse/persona/server/internal/proto"
)

// ---- token usage by machine (docs/server/machines.md「Token 用量」) ----

// UsageMinuteStep is the step of a row as a node reported it; UsageRolledStep of ten minutes rolled up.
const (
	UsageMinuteStep = 60
	UsageRolledStep = 600
)

const putUsageSQL = `INSERT INTO machine_usage (machine_id, ts, step, model, launcher, input, cache_write, cache_read,
		output, requests)
	VALUES ($1, to_timestamp($2), 60, $3, $4, $5, $6, $7, $8, $9)
	ON CONFLICT (machine_id, ts, step, model, launcher) DO UPDATE SET input = $5, cache_write = $6, cache_read = $7,
		output = $8, requests = $9`

// UsageWrite is one node's "usage" notification to store (PutUsage's arguments).
type UsageWrite struct {
	Machine     string
	Buckets     []proto.UsageBucket
	ReplaceFrom time.Time
}

func queueUsage(b *pgx.Batch, w UsageWrite) {
	if !w.ReplaceFrom.IsZero() {
		b.Queue(`DELETE FROM machine_usage WHERE machine_id = $1 AND step = 60 AND ts >= to_timestamp($2)`,
			w.Machine, w.ReplaceFrom.Unix())
	}
	for _, x := range w.Buckets {
		b.Queue(putUsageSQL, w.Machine, x.Minute, x.Model, x.Launcher, x.Input, x.CacheWrite, x.CacheRead, x.Output, x.Requests)
	}
}

// PutUsage stores a node's buckets: a bucket is its minute's whole total, so it replaces what was there. With
// replaceFrom set (the node's whole window, "full"), the machine's minute rows from then on that it did not send
// are deleted first: what the node counts now is the record (an older node's rows without a launcher, a reply it no
// longer counts twice).
func (d *DB) PutUsage(ctx context.Context, machine string, buckets []proto.UsageBucket, replaceFrom time.Time) error {
	_, err := d.PutUsages(ctx, []UsageWrite{{Machine: machine, Buckets: buckets, ReplaceFrom: replaceFrom}})
	return err
}

// PutUsages stores several writes as PutUsage does, in order, in one transaction: one commit, so one WAL flush,
// instead of one a write. A write Postgres rejects costs only itself: rejected[i] is the error writes[i] got
// (writeAll).
func (d *DB) PutUsages(ctx context.Context, writes []UsageWrite) (rejected []error, err error) {
	return d.writeAll(ctx, len(writes), func(b *pgx.Batch, i int) { queueUsage(b, writes[i]) })
}

// RollupUsage sums the minute rows before before (rounded down to ten minutes) into one row a machine, ten minutes,
// model and launcher. Moving and summing is one statement, so a minute is rolled up once; a ten-minute row already
// there (a node that sent an old minute again) is added to.
func (d *DB) RollupUsage(ctx context.Context, before time.Time) (int64, error) {
	tag, err := d.pool.Exec(ctx, `WITH moved AS (
			DELETE FROM machine_usage WHERE step = 60 AND ts < to_timestamp($1) RETURNING *
		)
		INSERT INTO machine_usage AS u (machine_id, ts, step, model, launcher, input, cache_write, cache_read, output, requests)
		SELECT machine_id, date_bin('10 minutes', ts, timestamptz 'epoch'), 600, model, launcher, sum(input), sum(cache_write),
			sum(cache_read), sum(output), sum(requests)
		FROM moved GROUP BY 1, 2, 4, 5
		ON CONFLICT (machine_id, ts, step, model, launcher) DO UPDATE SET input = u.input + excluded.input,
			cache_write = u.cache_write + excluded.cache_write, cache_read = u.cache_read + excluded.cache_read,
			output = u.output + excluded.output, requests = u.requests + excluded.requests`,
		before.Truncate(10*time.Minute).Unix())
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

// PruneUsage deletes every row older than before.
func (d *DB) PruneUsage(ctx context.Context, before time.Time) (int64, error) {
	tag, err := d.pool.Exec(ctx, `DELETE FROM machine_usage WHERE ts < to_timestamp($1)`, before.Unix())
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

// UsageRow is one model and launcher's usage over a step (Minute: the step's start).
type UsageRow = proto.UsageBucket

// UsageSeries sums a machine's rows from from (inclusive) to to over steps of step seconds (aligned to the Unix
// epoch; a multiple of ten minutes, or of a minute under 27 hours), one row a step, model and launcher, in time order.
func (d *DB) UsageSeries(ctx context.Context, machine string, from, to time.Time, step int) ([]UsageRow, error) {
	rows, err := d.pool.Query(ctx, `SELECT extract(epoch FROM date_bin(make_interval(secs => $4), ts, timestamptz 'epoch'))::bigint,
			model, launcher, sum(input)::bigint, sum(cache_write)::bigint, sum(cache_read)::bigint, sum(output)::bigint, sum(requests)::bigint
		FROM machine_usage WHERE machine_id = $1 AND ts >= to_timestamp($2) AND ts < to_timestamp($3)
		GROUP BY 1, 2, 3 ORDER BY 1, 2, 3`, machine, from.Unix(), to.Unix(), step)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []UsageRow
	for rows.Next() {
		var r UsageRow
		if err := rows.Scan(&r.Minute, &r.Model, &r.Launcher, &r.Input, &r.CacheWrite, &r.CacheRead, &r.Output, &r.Requests); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
