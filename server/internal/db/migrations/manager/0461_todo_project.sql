-- The knowledge-base project a todo belongs to (#2581, docs/assistant/todos.md "Project"): the slug of a project under
-- ~/.persona/data/projects, '' for none. The To-dos page groups open todos by it, and a finished task's outcome goes to its
-- timeline. Not a foreign key: projects live in the knowledge base's files, not in this database.
ALTER TABLE todos ADD COLUMN IF NOT EXISTS project text NOT NULL DEFAULT '';
