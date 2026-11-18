-- Urgent calls (docs/assistant/urgent-call.md, #8734): one row per matter the assistant rang the owner's phone about, its
-- rings (attempt 1, and 2 after a missed one), what came of them and the call's transcript. next_at is when the manager
-- acts on it next (a ring nobody reported on, the retry, a reminder after two missed calls) and step says which; NULL:
-- nothing left to do. version guards every change, so a restart or a second manager never acts on a row twice.
CREATE TABLE IF NOT EXISTS urgent_calls (
    id             text PRIMARY KEY,
    machine_id     text NOT NULL,
    conversation   text NOT NULL,
    matter         text NOT NULL,
    title          text NOT NULL,
    body           text NOT NULL DEFAULT '',
    why            text NOT NULL,
    rule_id        text NOT NULL DEFAULT '',
    rule_text      text NOT NULL DEFAULT '',
    happening      text NOT NULL DEFAULT '',
    owner_quote    text NOT NULL DEFAULT '',
    interaction_id text NOT NULL DEFAULT '',
    state          text NOT NULL,
    attempt        integer NOT NULL DEFAULT 1,
    created_at     timestamptz NOT NULL DEFAULT now(),
    rang_at        timestamptz,
    next_at        timestamptz,
    step           text NOT NULL DEFAULT '',
    reminders      integer NOT NULL DEFAULT 0,
    answered_at    timestamptz,
    ended_at       timestamptz,
    seconds        integer NOT NULL DEFAULT 0,
    outcome        text NOT NULL DEFAULT '',
    transcript     text NOT NULL DEFAULT '',
    version        integer NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS urgent_calls_next_at ON urgent_calls (next_at) WHERE next_at IS NOT NULL;
CREATE INDEX IF NOT EXISTS urgent_calls_created_at ON urgent_calls (created_at);
