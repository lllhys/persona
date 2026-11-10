List the user's Persona to-dos (the To-dos page in the app, not Feishu tasks): open ones by default, earliest due first (those without a due date last), each with its id, title, state, priority, project, due date, reminder and the agents on it, and the first line of its notes. Times are in the user's time zone. Filter by state, by due date (due_before / due_after: a day or a moment), overdue ones only, with or without agents, by knowledge-base project, by kind (concerns only), or a word in the title or notes. Use it before changing a to-do, to find its id; ask with todo_get for one in full.

```json
{
  "type": "object",
  "properties": {
    "status": {
      "type": "string",
      "enum": ["open", "done", "all"],
      "description": "which to-dos: open (default), done, or all"
    },
    "due_before": {
      "type": "string",
      "description": "only those due by then: a day (2026-11-15, through its end) or a moment (2026-11-15 18:00, RFC 3339); leaves out those without a due date"
    },
    "due_after": {
      "type": "string",
      "description": "only those due from then on: a day or a moment; leaves out those without a due date"
    },
    "overdue": {
      "type": "boolean",
      "description": "only open ones whose due date has passed"
    },
    "with_agents": {
      "type": "boolean",
      "description": "true: only those agents work on (tasks); false: only the user's own"
    },
    "query": {
      "type": "string",
      "description": "words to find in the title or notes"
    },
    "project": {
      "type": "string",
      "description": "only the to-dos in this knowledge-base project (its slug or name)"
    },
    "kind": {
      "type": "string",
      "enum": ["todo", "concern"],
      "description": "only concerns (things the user raised in the main conversation that wait for you to report their outcome), or only the other to-dos; both when left out"
    },
    "limit": {
      "type": "integer",
      "description": "at most this many (default 50, at most 200)"
    }
  }
}
```
