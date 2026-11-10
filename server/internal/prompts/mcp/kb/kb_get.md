Read one entity: its metadata (links to people / chats / projects, tags), compressed context (summary.md), a project's timeline, a chat's messages of the last 7 days, a recording's meeting minutes with where each action item stands (a suggestion, or its to-do: open, done, cancelled, deleted) and its full timestamped transcript (recordings: the user's phone / watch recordings of the last days, text only), a meeting's or event's recordings. id from kb_list / kb_search (a project also by name).

```json
{
  "type": "object",
  "required": ["kind", "id"],
  "properties": {
    "kind": {
      "type": "string",
      "enum": ["projects", "chats", "people", "docs", "meetings", "events", "recordings"]
    },
    "id": {
      "type": "string"
    }
  }
}
```
