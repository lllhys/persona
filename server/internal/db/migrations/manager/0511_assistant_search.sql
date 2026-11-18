-- The assistant's chat search (#6282, docs/design/proposals/assistant-search.md "The index"): what the user wrote and
-- what the assistant replied in the assistant's conversations, one row per message, for GET /api/v1/assistant/search.
-- body is the text the chat shows of it (spec/fixtures/chat-search "index": no hidden tags, no quoted contexts, a card
-- as its fallback); folded is body after NFKC and lower case, the only column a search matches (a substring scan: no
-- trigram index, see the proposal). Thoughts, tools, system events and messages Persona injected are never stored.
CREATE TABLE IF NOT EXISTS assistant_search_items (
    machine_id text NOT NULL,
    session_id text NOT NULL,
    seq        integer NOT NULL,
    item_id    text NOT NULL,
    role       text NOT NULL,
    ts         timestamptz,
    body       text NOT NULL,
    folded     text NOT NULL,
    PRIMARY KEY (machine_id, session_id, seq, item_id)
);

-- How far each conversation is indexed: the transcript's history token (a new one means the file was replaced or
-- rewritten and its seqs start over) and the last seq read. A conversation without a row is not indexed yet.
CREATE TABLE IF NOT EXISTS assistant_search_cursors (
    machine_id text NOT NULL,
    session_id text NOT NULL,
    history    text NOT NULL DEFAULT '',
    after_seq  integer NOT NULL DEFAULT 0,
    indexed_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (machine_id, session_id)
);
