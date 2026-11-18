-- To-dos imported from another app (#4009, docs/assistant/todos.md "Imported from Reminders"): one row per item of the
-- source (source 'reminders': the iPhone's Reminders, external_id its calendarItemExternalIdentifier). fingerprint is a
-- hash of the title, notes and due date last taken from it, so an import changes the to-do only when they changed there.
-- todo_id is the to-do it became; NULL once that to-do was deleted in Persona, which keeps the item from coming back.
CREATE TABLE IF NOT EXISTS todo_imports (
    source      text        NOT NULL CONSTRAINT todo_imports_source_known CHECK (source IN ('reminders')),
    external_id text        NOT NULL CONSTRAINT todo_imports_external_id_set CHECK (external_id <> ''),
    todo_id     text        REFERENCES todos (id) ON DELETE SET NULL,
    fingerprint text        NOT NULL,
    created_at  timestamptz NOT NULL,
    updated_at  timestamptz NOT NULL,
    PRIMARY KEY (source, external_id)
);
CREATE UNIQUE INDEX IF NOT EXISTS todo_imports_todo ON todo_imports (todo_id) WHERE todo_id IS NOT NULL;
