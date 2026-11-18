-- When the owner last looked at a to-do that was in Just finished (#5707, docs/assistant/todo-status-sop.md "Seen"): Got it
-- on its row, or opening it. A to-do seen at or after the moment that put it in Just finished goes to Done; anything that
-- happens later brings it back. NULL when never seen; Undo clears it. Marking it is not activity: updated_at stays.
ALTER TABLE todos ADD COLUMN IF NOT EXISTS seen_at timestamptz;
