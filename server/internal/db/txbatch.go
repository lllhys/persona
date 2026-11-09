package db

import (
	"context"
	"slices"

	"github.com/jackc/pgx/v5"
)

// writeAll runs n writes in one transaction, in order (queue adds write i's statements to a batch): one commit, so
// one WAL flush, for all of them instead of one each. When that fails, they run again in one transaction, each under
// a savepoint of its own, so a write Postgres rejects costs only itself: rejected[i] is write i's error, and rejected
// is nil when every write was stored. err is a failure of the whole (the connection, the commit, ctx, or the only
// write); then nothing was stored.
func (d *DB) writeAll(ctx context.Context, n int, queue func(b *pgx.Batch, i int)) (rejected []error, err error) {
	if n == 0 {
		return nil, nil
	}
	err = pgx.BeginFunc(ctx, d.pool, func(tx pgx.Tx) error {
		b := &pgx.Batch{}
		for i := range n {
			queue(b, i)
		}
		return tx.SendBatch(ctx, b).Close()
	})
	if err == nil || n == 1 || ctx.Err() != nil {
		return nil, err
	}
	// A batch stops at its first error, and the transaction with it: find which writes Postgres takes.
	rejected = make([]error, n)
	err = pgx.BeginFunc(ctx, d.pool, func(tx pgx.Tx) error {
		for i := range n {
			rejected[i] = pgx.BeginFunc(ctx, tx, func(sp pgx.Tx) error { // a savepoint
				b := &pgx.Batch{}
				queue(b, i)
				return sp.SendBatch(ctx, b).Close()
			})
		}
		return nil
	})
	if err != nil || !slices.ContainsFunc(rejected, func(err error) bool { return err != nil }) {
		return nil, err
	}
	return rejected, nil
}
