-- Remember imported references so an old report cannot undo a user removal.
CREATE TABLE todo_repository_link_receipts (
    todo_id TEXT PRIMARY KEY REFERENCES todos(id) ON DELETE CASCADE,
    seen_urls TEXT[] NOT NULL DEFAULT '{}',
    legacy_imported BOOLEAN NOT NULL DEFAULT FALSE
);
