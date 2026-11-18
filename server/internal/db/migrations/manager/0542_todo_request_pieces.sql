-- A request in pieces (docs/assistant/todos.md "Requests in pieces"): part_of is the to-do whose request this one is a
-- piece of ('' for none), after_ids the pieces of that request it waits for. The manager checks both when they are
-- written (the to-dos exist, one level, no cycle); neither is a foreign key, so a deleted to-do leaves an id the
-- readers skip.
ALTER TABLE todos ADD COLUMN IF NOT EXISTS part_of text NOT NULL DEFAULT '';
ALTER TABLE todos ADD COLUMN IF NOT EXISTS after_ids text[] NOT NULL DEFAULT '{}';
