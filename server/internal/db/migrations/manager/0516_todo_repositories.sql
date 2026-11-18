-- Business associations are independent of workspace ownership and task progress.
CREATE TABLE IF NOT EXISTS todo_repository_sets (
    todo_id text PRIMARY KEY REFERENCES todos(id) ON DELETE CASCADE,
    revision bigint NOT NULL CHECK (revision >= 0 AND revision <= 9007199254740991),
    repositories jsonb NOT NULL DEFAULT '[]'::jsonb CHECK (jsonb_typeof(repositories) = 'array')
);
