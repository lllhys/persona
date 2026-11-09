Add a to-do to the user's Persona to-dos, when the user asks you to note something to do or to remind them of it (or agrees to your suggestion): it shows on the To-dos page of the app, sorted by its due date, and with remind set the phone is pushed about it at that time. It is the user's own to-do, with no agent on it: work for an agent is task_plan. Done at once, nothing to confirm. An open to-do with the same title and due date is not made twice: you get that one back. Write the title as the thing to do (short, the user's language); put links, amounts and details in notes. Read due dates and times in the user's time zone; when only the day is known, give the day. When it clearly belongs to one of the user's areas of work, give project: the To-dos page groups to-dos by it.

```json
{
  "type": "object",
  "required": ["title"],
  "properties": {
    "title": {
      "type": "string",
      "description": "what is to be done, in a line"
    },
    "notes": {
      "type": "string",
      "description": "details: where it came from, links, amounts, what to bring"
    },
    "due": {
      "type": "string",
      "description": "when it is due: a day (2026-11-15) when only the day is known, else a moment (2026-11-15 18:00 in the user's time zone, or RFC 3339 with its offset); 2h / 3d from now also work"
    },
    "remind": {
      "type": "string",
      "description": "when to push the phone about it: a moment (2026-11-14 09:00 in the user's time zone, or RFC 3339), or 30m / 2h / 1d from now; not in the past. Leave it out unless the user asked for a reminder or the deadline is close and matters"
    },
    "priority": {
      "type": "string",
      "enum": ["high", "normal", "low"],
      "description": "normal when left out"
    },
    "project": {
      "type": "string",
      "description": "the knowledge-base project it belongs to: the slug or name of an existing project, usually a long-term one for an area of the user's work (kb_list projects). Set it when the area is clear; never make up a near-duplicate of an existing project"
    }
  }
}
```
