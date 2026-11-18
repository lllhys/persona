package db

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/jackc/pgx/v5"
)

const ConversationDeletionsKey = "assistant.deleted_conversations"

var ErrConversationDeleted = errors.New("conversation deleted")

func conversationDeletionKey(machine, session string) string {
	raw, _ := json.Marshal([2]string{machine, session})
	return string(raw)
}

// PutConversationDeletion merges one entry against the current database value,
// never an instance's cached whole map. The row lock also orders search commits
// from overlapping manager processes before deletion and index cleanup.
func (d *DB) PutConversationDeletion(ctx context.Context, machine, session string, row json.RawMessage) error {
	return pgx.BeginFunc(ctx, d.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `INSERT INTO settings (key,value,updated_at) VALUES ($1,'{}',now()) ON CONFLICT (key) DO NOTHING`, ConversationDeletionsKey); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE settings SET value=value || jsonb_build_object($2::text,$3::jsonb), updated_at=now() WHERE key=$1`, ConversationDeletionsKey, conversationDeletionKey(machine, session), row); err != nil {
			return err
		}
		return dropSearchConversationTx(ctx, tx, machine, session)
	})
}

// ConversationDeletion is an authoritative read, including when this process
// previously cached an empty ledger before another process deleted the chat.
func (d *DB) ConversationDeletion(ctx context.Context, machine, session string) (json.RawMessage, bool, error) {
	var raw []byte
	err := d.pool.QueryRow(ctx, `SELECT value -> $2 FROM settings WHERE key=$1`, ConversationDeletionsKey, conversationDeletionKey(machine, session)).Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, false, nil
	}
	return raw, len(raw) > 0 && string(raw) != "null", err
}

func (d *DB) ConversationDeletedID(ctx context.Context, session string) (bool, error) {
	var deleted bool
	err := d.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM settings s CROSS JOIN LATERAL jsonb_each(s.value) entry WHERE s.key=$1 AND entry.value->>'session_id'=$2)`, ConversationDeletionsKey, session).Scan(&deleted)
	return deleted, err
}

// This shared row lock lasts through a conversation-derived write. Deletion's
// UPDATE waits for earlier commits; later writes observe the marker.
func conversationAvailableTx(ctx context.Context, tx pgx.Tx, machine, session string) error {
	if _, err := tx.Exec(ctx, `INSERT INTO settings (key,value,updated_at) VALUES ($1,'{}',now()) ON CONFLICT (key) DO NOTHING`, ConversationDeletionsKey); err != nil {
		return err
	}
	var deleted bool
	if err := tx.QueryRow(ctx, `SELECT value ? $2 FROM settings WHERE key=$1 FOR SHARE`, ConversationDeletionsKey, conversationDeletionKey(machine, session)).Scan(&deleted); err != nil {
		return err
	}
	if deleted {
		return ErrConversationDeleted
	}
	return nil
}
