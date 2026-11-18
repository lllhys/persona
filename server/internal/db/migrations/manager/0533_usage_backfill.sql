-- How far each machine's token usage backfill got (docs/server/machines.md「Backfill」, #9484): once, its node reads the
-- transcripts from before the history began and sends the hours; an hour is stored only when it has no usage yet.
CREATE TABLE IF NOT EXISTS usage_backfill (
    machine_id text        PRIMARY KEY,
    through    timestamptz,                -- the start of the last hour sent so far: a scan cut off goes on after it
    done_at    timestamptz,                -- the scan finished: the node is not asked again
    hours      integer     NOT NULL DEFAULT 0, -- hours stored (an hour that had usage is left as it was)
    updated_at timestamptz NOT NULL DEFAULT now()
);
