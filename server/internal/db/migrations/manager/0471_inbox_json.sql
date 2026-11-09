-- The inbox's rows, stored for the reads that matter (#2779, docs/assistant/inbox.md "Storage", docs/lessons/server.md).
-- One rewrite of the table: about 2 s for 28,600 items.
--
-- - data is json instead of jsonb. Every reader unmarshals the bytes in Go (assistant/inbox_store.go and
--   inbox_history.go), and the manager's start reads every open item and those done in the last 8 days: jsonb turned
--   each one back into text first. json keeps the text the manager wrote.
-- - lz4 instead of pglz for the documents large enough to be compressed: faster to decompress. It goes in the same
--   ALTER as the type change, which recomputes every value: SET COMPRESSION on its own, or a rewrite that copies the
--   values as they are, leaves every stored value pglz. A server built without lz4 (feature_not_supported) or a
--   Postgres before 14 (syntax_error) takes the same rewrite without it. EXECUTE, because a statement written in the
--   block fails when the block is compiled, before the handler is active.
-- - source is the item's source, kept by Postgres: DeleteInboxSource finds a source's rows through its index instead of
--   reading every document. Computing it also keeps jsonb's rejection of \u0000 in any string of an item: a write with
--   one fails as it did.
-- - inbox_items_open is (done_at, id) instead of (done_at, updated_at DESC): an upsert of an open item then changes no
--   indexed column and can be a HOT update; the done items' pages read (done_at, id) in order.
-- - jsonb_typeof(json) answers the done page of a build from before this one (docs/operations/hub-restart.md: going
--   back to the previous version), which filters with jsonb_typeof(data->'connector'). The built-in jsonb_typeof(jsonb)
--   is unchanged. Drop it once no build from before this migration can run.
DO $$
BEGIN
    EXECUTE 'ALTER TABLE inbox_items
        ALTER COLUMN data TYPE json USING data::json,
        ALTER COLUMN data SET COMPRESSION lz4,
        ADD COLUMN source text GENERATED ALWAYS AS (data->>''source'') STORED';
EXCEPTION WHEN feature_not_supported OR syntax_error THEN
    EXECUTE 'ALTER TABLE inbox_items
        ALTER COLUMN data TYPE json USING data::json,
        ADD COLUMN source text GENERATED ALWAYS AS (data->>''source'') STORED';
END $$;
DROP INDEX inbox_items_open;
CREATE INDEX inbox_items_open ON inbox_items (done_at, id);
CREATE INDEX inbox_items_source ON inbox_items (source);
CREATE FUNCTION jsonb_typeof(json) RETURNS text LANGUAGE sql IMMUTABLE STRICT PARALLEL SAFE AS 'SELECT json_typeof($1)';
