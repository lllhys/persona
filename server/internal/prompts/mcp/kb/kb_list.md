List knowledge-base entities of one kind ranked by importance (P0 first), relevance to the user, recency: projects (short = a feature / launch, long = a team mission), chats, people, docs, meetings, events, recordings (transcripts of the user's phone / watch recordings, kept a few days; newest first, saying which have meeting minutes). Meetings, events and recordings show when they started, in the user's time zone. Optional tag filter.

```json
{
  "type": "object",
  "required": ["kind"],
  "properties": {
    "kind": {
      "type": "string",
      "enum": ["projects", "chats", "people", "docs", "meetings", "events", "recordings"]
    },
    "tag": {
      "type": "string"
    },
    "limit": {
      "type": "integer",
      "description": "default 30"
    }
  }
}
```
