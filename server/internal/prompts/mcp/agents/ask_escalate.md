Hand the cards a task's agents wait on to the user, when a 督办 message gave them to you first and the user must decide: a tool permission or a plan (no conversation approves those), or a question that is irreversible, seen by other people, changes the goal or goes against a decision already made on the task. Every card of the task the user has not been told of goes to them at once, as one reminder, with your reason and recommendation on it; their answers go straight to the agents and are kept as the task's decisions. Only the task's supervisor and the main conversation can. A leader's escalation is told to the main conversation too, so don't ask the user the same thing again yourself.

```json
{
  "type": "object",
  "required": ["todo_id", "reason"],
  "properties": {
    "todo_id": {
      "type": "string",
      "description": "the task's todo id (todo_…), from the 督办 message"
    },
    "reason": {
      "type": "string",
      "description": "in a sentence, in the user's language: what they are to decide and why it is theirs"
    },
    "recommend": {
      "type": "string",
      "description": "the option you recommend, in a line (an option's label when there is one)"
    }
  }
}
```
