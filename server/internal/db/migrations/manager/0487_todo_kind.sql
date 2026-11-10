-- What kind of todo it is (#3561, docs/assistant/todos.md "Concerns"): '' for a to-do or a task, 'concern' for something
-- the owner raised in the main conversation and wants an answer to. A concern closes only once the main conversation has
-- told the owner the outcome (a decision of source 'reported'); nothing closes it on its own. Set when it is made, never
-- changed.
ALTER TABLE todos ADD COLUMN IF NOT EXISTS kind text NOT NULL DEFAULT '';
