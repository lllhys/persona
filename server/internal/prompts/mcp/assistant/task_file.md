Copy a file a task's Agent made from its computer to this conversation's computer, and answer where it is now. Use it when the result is a file — a report, a PDF, an image, an archive — and you need to read it or give it to the user: never ask the Agent to paste the file's content or resend it in pieces, since a long reply is clipped and a binary file cannot be pasted at all. Name the task with `todo_id`, or the Agent with `machine` and `session_id`; `path` is the file's absolute path on that computer (as the Agent named it, or as agent_read shows it). Only a computer of a task this conversation opened or supervises is read. The copy stays on this computer: give its path to the user in backticks. A file that cannot come says why (the computer offline, over 50 MB, a directory, a path the computer refuses).

```json
{
  "type": "object",
  "required": ["path"],
  "properties": {
    "path": {
      "type": "string",
      "description": "the file's absolute path on the Agent's computer, or ~/…"
    },
    "todo_id": {
      "type": "string",
      "description": "the task it belongs to (from the live snapshot); leave out to name the Agent with machine and session_id"
    },
    "machine": {
      "type": "string",
      "description": "the Agent's computer, with session_id, when todo_id is not known"
    },
    "session_id": {
      "type": "string",
      "description": "the Agent's session id, with machine"
    }
  }
}
```
