Search the knowledge base: summaries and metadata of chats, people, projects, docs, meetings, events, recordings (with their minutes and full transcripts), project timelines, the notes directories the user added (hits with an absolute path) and, with messages=true, the full message archive (newest days first). All words must match. Hits come in the order the files are read, not by relevance, so a broad search can fill the limit with chats: give kind to search one kind only, the entries whose name matches first, then as kb_list ranks them. A past meeting (its decisions, what is still open, who does what) is kind=recordings: found by its title, the calendar event's title or what was said; then kb_get it.

```json
{
  "type": "object",
  "required": ["query"],
  "properties": {
    "query": {
      "type": "string"
    },
    "kind": {
      "type": "string",
      "enum": ["projects", "chats", "people", "docs", "meetings", "events", "recordings"],
      "description": "only this kind's entries (no notes directories; the message archive only with chats)"
    },
    "messages": {
      "type": "boolean",
      "description": "also search the message archive (default true)"
    },
    "limit": {
      "type": "integer",
      "description": "default 40"
    }
  }
}
```
