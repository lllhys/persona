-- A claimed write is never retried: a lost phone reply cannot prove the write did not happen.
CREATE TABLE IF NOT EXISTS phone_write_queue (
    sequence bigserial PRIMARY KEY,
    id text NOT NULL UNIQUE,
    tool text NOT NULL,
    args jsonb NOT NULL CHECK (jsonb_typeof(args) = 'object'),
    state text NOT NULL DEFAULT 'queued' CHECK (state IN ('queued', 'delivering', 'succeeded', 'declined', 'uncertain', 'cancelled', 'failed')),
    result jsonb,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS phone_write_queue_pending ON phone_write_queue (sequence) WHERE state IN ('queued', 'delivering');
CREATE INDEX IF NOT EXISTS phone_write_queue_completed ON phone_write_queue (updated_at) WHERE state NOT IN ('queued', 'delivering');
