-- When a supervised task entered pending_review (docs/assistant/unified-entry.md "Needs you and In progress"): the
-- apps list a review younger than an hour with the tasks in progress, as its supervisor's to do, and an older one
-- under Needs you. NULL in every other state. A task already waiting for review starts from its ledger's last write,
-- the time its review has been counted from so far (side_handover.go).
ALTER TABLE todo_supervision ADD COLUMN IF NOT EXISTS pending_since timestamptz;
UPDATE todo_supervision SET pending_since = updated_at WHERE state = 'pending_review' AND pending_since IS NULL;
