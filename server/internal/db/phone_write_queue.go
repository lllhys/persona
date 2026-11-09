package db

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

const (
	PhoneWritePendingLimit = 100
	PhoneWriteHistoryLimit = 1000
	PhoneWriteArgsLimit    = 64 << 10
	PhoneWriteResultLimit  = 64 << 10
	PhoneWriteRetention    = 30 * 24 * time.Hour
)

var ErrPhoneWriteQueueFull = errors.New("phone write queue is full (100 pending writes)")

// PhoneWrite is a durable delivery receipt, not proof that a phone has performed the write.
type PhoneWrite struct {
	ID        string          `json:"id"`
	Tool      string          `json:"tool"`
	Args      json.RawMessage `json:"args"`
	State     string          `json:"state"`
	Result    json.RawMessage `json:"result,omitempty"`
	CreatedAt time.Time       `json:"created_at"`
	UpdatedAt time.Time       `json:"updated_at"`
}

const phoneWriteColumns = `id, tool, args, state, result, created_at, updated_at`

const findQueuedPhoneWriteSQL = `SELECT ` + phoneWriteColumns + ` FROM phone_write_queue
	WHERE state = 'queued' AND tool = $1 AND args = $2::jsonb ORDER BY sequence LIMIT 1`

const queuedPhoneWritesSQL = `SELECT ` + phoneWriteColumns + ` FROM phone_write_queue
	WHERE state = 'queued' ORDER BY sequence LIMIT $1`

func scanPhoneWrite(row pgx.Row) (*PhoneWrite, error) {
	var write PhoneWrite
	err := row.Scan(&write.ID, &write.Tool, &write.Args, &write.State, &write.Result, &write.CreatedAt, &write.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &write, nil
}

// The transaction lock makes admission, deduplication and pruning atomic across hub database pools.
func (d *DB) phoneWriteTransaction(ctx context.Context, f func(pgx.Tx) error) error {
	return pgx.BeginFunc(ctx, d.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('phone_write_queue', 0))`); err != nil {
			return err
		}
		return f(tx)
	})
}

func prunePhoneWrites(ctx context.Context, tx pgx.Tx) error {
	_, err := tx.Exec(ctx, `DELETE FROM phone_write_queue WHERE state NOT IN ('queued', 'delivering') AND
		(updated_at < now() - $1 * interval '1 second' OR sequence IN (
			SELECT sequence FROM phone_write_queue WHERE state NOT IN ('queued', 'delivering')
			ORDER BY updated_at DESC, sequence DESC OFFSET $2))`, int64(PhoneWriteRetention/time.Second), PhoneWriteHistoryLimit)
	return err
}

func validatePhoneWriteArgs(args json.RawMessage) error {
	var object map[string]json.RawMessage
	if len(args) > PhoneWriteArgsLimit || json.Unmarshal(args, &object) != nil || object == nil {
		return errors.New("phone write arguments must be a JSON object of at most 64 KiB")
	}
	return nil
}

// FindQueuedPhoneWrite prevents an opt-in retry from bypassing a queued receipt when the phone reconnects.
func (d *DB) FindQueuedPhoneWrite(ctx context.Context, tool string, args json.RawMessage) (*PhoneWrite, error) {
	if err := validatePhoneWriteArgs(args); err != nil {
		return nil, err
	}
	return scanPhoneWrite(d.pool.QueryRow(ctx, findQueuedPhoneWriteSQL, tool, args))
}

// EnqueuePhoneWrite returns an existing queued receipt for equal JSON, ignoring object key order and numeric spelling.
func (d *DB) EnqueuePhoneWrite(ctx context.Context, id, tool string, args json.RawMessage) (*PhoneWrite, error) {
	if err := validatePhoneWriteArgs(args); err != nil {
		return nil, err
	}
	if id == "" || tool == "" || len(id) > 128 || len(tool) > 256 {
		return nil, errors.New("invalid phone write ID or tool name")
	}
	var write *PhoneWrite
	err := d.phoneWriteTransaction(ctx, func(tx pgx.Tx) error {
		if err := prunePhoneWrites(ctx, tx); err != nil {
			return err
		}
		var err error
		write, err = scanPhoneWrite(tx.QueryRow(ctx, findQueuedPhoneWriteSQL, tool, args))
		if err != nil || write != nil {
			return err
		}
		var count int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM phone_write_queue WHERE state IN ('queued', 'delivering')`).Scan(&count); err != nil {
			return err
		}
		if count >= PhoneWritePendingLimit {
			return ErrPhoneWriteQueueFull
		}
		write, err = scanPhoneWrite(tx.QueryRow(ctx, `INSERT INTO phone_write_queue (id, tool, args)
			VALUES ($1, $2, $3) RETURNING `+phoneWriteColumns, id, tool, args))
		return err
	})
	return write, err
}

// PhoneWrites lists receipts without changing them. Admission and completion prune terminal history.
func (d *DB) PhoneWrites(ctx context.Context) ([]PhoneWrite, error) {
	return d.phoneWriteRows(ctx, `SELECT `+phoneWriteColumns+` FROM phone_write_queue ORDER BY sequence`)
}

// QueuedPhoneWrites reads only unsent writes through the pending index; idle delivery polls never read history.
func (d *DB) QueuedPhoneWrites(ctx context.Context) ([]PhoneWrite, error) {
	return d.phoneWriteRows(ctx, queuedPhoneWritesSQL, PhoneWritePendingLimit)
}

func (d *DB) phoneWriteRows(ctx context.Context, query string, args ...any) ([]PhoneWrite, error) {
	rows, err := d.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	writes := []PhoneWrite{}
	for rows.Next() {
		write, err := scanPhoneWrite(rows)
		if err != nil {
			return nil, err
		}
		writes = append(writes, *write)
	}
	return writes, rows.Err()
}

// ClaimPhoneWrite records the delivery attempt before any bytes can be sent to the phone.
func (d *DB) ClaimPhoneWrite(ctx context.Context, id string) (bool, error) {
	claimed := false
	err := d.phoneWriteTransaction(ctx, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `UPDATE phone_write_queue SET state = 'delivering', updated_at = now()
			WHERE id = $1 AND state = 'queued'`, id)
		claimed = tag.RowsAffected() == 1
		return err
	})
	return claimed, err
}

// FinishPhoneWrite cannot change a terminal receipt, including one recovered as uncertain after a restart.
func (d *DB) FinishPhoneWrite(ctx context.Context, id, state string, result json.RawMessage) error {
	if len(result) > PhoneWriteResultLimit {
		return errors.New("phone write result exceeds 64 KiB")
	}
	switch state {
	case "succeeded", "declined", "uncertain", "failed":
	default:
		return errors.New("invalid phone write result state")
	}
	return d.phoneWriteTransaction(ctx, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `UPDATE phone_write_queue SET state = $2, result = $3, updated_at = now()
			WHERE id = $1 AND state = 'delivering'`, id, state, result); err != nil {
			return err
		}
		return prunePhoneWrites(ctx, tx)
	})
}

// CancelPhoneWrite returns the current receipt; only a queued write can become cancelled.
func (d *DB) CancelPhoneWrite(ctx context.Context, id string) (*PhoneWrite, error) {
	var write *PhoneWrite
	err := d.phoneWriteTransaction(ctx, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `UPDATE phone_write_queue SET state = 'cancelled', updated_at = now()
			WHERE id = $1 AND state = 'queued'`, id); err != nil {
			return err
		}
		var err error
		write, err = scanPhoneWrite(tx.QueryRow(ctx, `SELECT `+phoneWriteColumns+` FROM phone_write_queue WHERE id = $1`, id))
		return err
	})
	return write, err
}

// RecoverPhoneWrites never retries an attempt whose phone may have completed the write before the hub stopped.
func (d *DB) RecoverPhoneWrites(ctx context.Context) error {
	return d.phoneWriteTransaction(ctx, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE phone_write_queue SET state = 'uncertain', updated_at = now()
			WHERE state = 'delivering'`)
		return err
	})
}
