package db

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// The assistant's chat search index (#6282, migrations/manager/0511_assistant_search.sql,
// docs/design/proposals/assistant-search.md "The index"): one row per message the user wrote or the assistant replied
// in the assistant's conversations, and per conversation how far it is read.

// SearchItemRow is one message of a conversation as the index keeps it: Body is the text the chat shows of it, Folded
// the same after NFKC and lower case (what a search matches).
type SearchItemRow struct {
	Seq    int
	ItemID string
	Role   string
	TS     time.Time
	Body   string
	Folded string
}

// SearchCursorRow is how far a conversation is indexed: its transcript's history token and the last seq read.
type SearchCursorRow struct {
	MachineID string
	SessionID string
	History   string
	AfterSeq  int
	IndexedAt time.Time
}

// SearchHitRow is a message that holds every word searched for; Hits is how many of its conversation's do.
type SearchHitRow struct {
	MachineID string
	SessionID string
	Seq       int
	ItemID    string
	Role      string
	TS        time.Time
	Body      string
	Hits      int
}

// SearchCursors returns every conversation's cursor.
func (d *DB) SearchCursors(ctx context.Context) ([]SearchCursorRow, error) {
	rows, err := d.pool.Query(ctx, `SELECT machine_id, session_id, history, after_seq, indexed_at FROM assistant_search_cursors`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SearchCursorRow
	for rows.Next() {
		var r SearchCursorRow
		if err := rows.Scan(&r.MachineID, &r.SessionID, &r.History, &r.AfterSeq, &r.IndexedAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// PutSearchItems stores a page of a conversation's messages and moves its cursor to afterSeq, in one transaction.
// reset drops the conversation's rows first: its transcript was rewritten, so the page starts it over.
func (d *DB) PutSearchItems(ctx context.Context, machine, session, history string, afterSeq int, items []SearchItemRow, reset bool) error {
	return pgx.BeginFunc(ctx, d.pool, func(tx pgx.Tx) error {
		if err := conversationAvailableTx(ctx, tx, machine, session); err != nil {
			return err
		}
		b := &pgx.Batch{}
		if reset {
			b.Queue(`DELETE FROM assistant_search_items WHERE machine_id = $1 AND session_id = $2`, machine, session)
		}
		for _, r := range items {
			var ts *time.Time
			if !r.TS.IsZero() {
				ts = &r.TS
			}
			b.Queue(`INSERT INTO assistant_search_items (machine_id, session_id, seq, item_id, role, ts, body, folded)
				VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
				ON CONFLICT (machine_id, session_id, seq, item_id)
				DO UPDATE SET role = EXCLUDED.role, ts = EXCLUDED.ts, body = EXCLUDED.body, folded = EXCLUDED.folded`,
				machine, session, r.Seq, r.ItemID, r.Role, ts, r.Body, r.Folded)
		}
		b.Queue(`INSERT INTO assistant_search_cursors (machine_id, session_id, history, after_seq, indexed_at)
			VALUES ($1, $2, $3, $4, now())
			ON CONFLICT (machine_id, session_id)
			DO UPDATE SET history = EXCLUDED.history, after_seq = EXCLUDED.after_seq, indexed_at = EXCLUDED.indexed_at`,
			machine, session, history, afterSeq)
		return tx.SendBatch(ctx, b).Close()
	})
}

// DropSearchConversation removes a conversation from the index: its rows and its cursor.
func (d *DB) DropSearchConversation(ctx context.Context, machine, session string) error {
	return pgx.BeginFunc(ctx, d.pool, func(tx pgx.Tx) error {
		return dropSearchConversationTx(ctx, tx, machine, session)
	})
}

func dropSearchConversationTx(ctx context.Context, tx pgx.Tx, machine, session string) error {
	b := &pgx.Batch{}
	b.Queue(`DELETE FROM assistant_search_items WHERE machine_id = $1 AND session_id = $2`, machine, session)
	b.Queue(`DELETE FROM assistant_search_cursors WHERE machine_id = $1 AND session_id = $2`, machine, session)
	return tx.SendBatch(ctx, b).Close()
}

// PurgeSearch empties the index: every row and every cursor.
func (d *DB) PurgeSearch(ctx context.Context) error {
	return pgx.BeginFunc(ctx, d.pool, func(tx pgx.Tx) error {
		b := &pgx.Batch{}
		b.Queue(`DELETE FROM assistant_search_items`)
		b.Queue(`DELETE FROM assistant_search_cursors`)
		return tx.SendBatch(ctx, b).Close()
	})
}

// searchResetKey is the manager setting that counts the index's resets (ResetSearch).
const searchResetKey = "assistant_search.reset"

// ResetSearch empties the index (`assistant search-reindex`, #6340) and counts one more reset, in one transaction. A
// manager that runs meanwhile still holds the cursors it read before: seeing the count move (SearchReset), it lets
// go of them and reads every conversation again from the start (assistant/search_index.go).
func (d *DB) ResetSearch(ctx context.Context) error {
	return pgx.BeginFunc(ctx, d.pool, func(tx pgx.Tx) error {
		b := &pgx.Batch{}
		b.Queue(`DELETE FROM assistant_search_items`)
		b.Queue(`DELETE FROM assistant_search_cursors`)
		b.Queue(`INSERT INTO settings (key, value, updated_at) VALUES ($1, '1', now())
			ON CONFLICT (key) DO UPDATE SET value = to_jsonb(COALESCE((settings.value #>> '{}')::bigint, 0) + 1), updated_at = now()`,
			searchResetKey)
		return tx.SendBatch(ctx, b).Close()
	})
}

// SearchReset is how many times the index was reset (ResetSearch): 0 before the first.
func (d *DB) SearchReset(ctx context.Context) (int64, error) {
	var n int64
	_, err := d.Setting(ctx, searchResetKey, &n)
	return n, err
}

// SearchAssistantItems returns the messages of the given conversations (machines[i]/sessions[i]) whose folded text
// holds every one of words (already folded), at most perConversation of each conversation (its newest), newest
// first. A substring scan of the text (no trigram index: docs/design/proposals/assistant-search.md "The index").
func (d *DB) SearchAssistantItems(ctx context.Context, machines, sessions, words []string, perConversation int) ([]SearchHitRow, error) {
	if len(machines) == 0 || len(machines) != len(sessions) || len(words) == 0 {
		return nil, nil
	}
	args := []any{machines, sessions, perConversation}
	match := ""
	for _, w := range words {
		args = append(args, w)
		match += fmt.Sprintf(" AND strpos(folded, $%d) > 0", len(args))
	}
	rows, err := d.pool.Query(ctx, `SELECT machine_id, session_id, seq, item_id, role, ts, body, hits FROM (
			SELECT machine_id, session_id, seq, item_id, role, ts, body,
				row_number() OVER (PARTITION BY machine_id, session_id ORDER BY seq DESC, item_id DESC) AS n,
				count(*) OVER (PARTITION BY machine_id, session_id) AS hits
			FROM assistant_search_items
			WHERE (machine_id, session_id) IN (SELECT * FROM unnest($1::text[], $2::text[]))`+match+`
		) m WHERE n <= $3 ORDER BY ts DESC NULLS LAST, seq DESC, item_id DESC`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SearchHitRow
	for rows.Next() {
		var r SearchHitRow
		var ts *time.Time
		if err := rows.Scan(&r.MachineID, &r.SessionID, &r.Seq, &r.ItemID, &r.Role, &ts, &r.Body, &r.Hits); err != nil {
			return nil, err
		}
		if ts != nil {
			r.TS = *ts
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
