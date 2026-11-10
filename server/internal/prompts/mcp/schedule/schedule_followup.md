Wake yourself up later: at the given time Persona sends `prompt` into this conversation (or a new one) as if the user wrote it, you do the work, and the user gets a push with your answer (with `push: "asked"`, only what you send with notify_user). Use for "remind me…", "check again in an hour", meeting prep 10 minutes before a meeting, watching something until it changes. `at` is RFC 3339 (2026-10-27T09:50:00+08:00) or relative ("30m", "2h", "1d"). For something to do again and again (a patrol every 30 minutes) pass `every` ("30m", "2h", at least 5m) once: Persona repeats it by itself and the user sees, edits and stops it in the app; never schedule the next run from inside a run. An `every` task can take `until`, an instant after which the task stops (RFC 3339 or 30m / 2h / 1d, the same forms as `at`, not a bare clock time and not a condition to watch for): after that instant Persona turns it off and records a skipped run instead of running. A one-off ignores `until`. Omit it to keep an existing task's end time; null clears it. The task runs on Main chat's launcher, model and thinking effort unless you give it its own with `launcher`, `model` and `effort`; the permission mode stays the user's choice in the app. A follow-up that continues this conversation (`new_conversation` false) runs on that conversation's launcher, so giving it its own needs `new_conversation: true`; the call is refused otherwise.

```json
{
  "type": "object",
  "required": ["prompt"],
  "properties": {
    "at": {
      "type": "string",
      "description": "when (the first run, with every); default with every: one interval from now"
    },
    "every": {
      "type": "string",
      "description": "repeat this often, e.g. 30m"
    },
    "prompt": {
      "type": "string",
      "description": "what to do then, self-contained"
    },
    "title": {
      "type": "string",
      "description": "short label shown in the app"
    },
    "new_conversation": {
      "type": "boolean",
      "description": "start a fresh conversation instead of continuing this one"
    },
    "push": {
      "type": "string",
      "enum": ["every", "asked"],
      "description": "every (default for a new task): each run's answer is pushed to the user; asked: only what a run sends with notify_user, for a task that watches something or a standing loop (a patrol, a daily round). Continuing this conversation, a run's answer is then not pushed or sent to channels; from Main chat it runs in its own conversation instead. Omit to keep an existing task's"
    },
    "until": {
      "type": ["string", "null"],
      "description": "optional end time (RFC 3339 or 30m / 2h / 1d). After it, Persona turns the task off and records a skipped run instead of running. Omit to keep an existing task's end time; null clears it. Ignored for a one-off."
    },
    "launcher": {
      "type": "string",
      "description": "this task's own launcher, instead of Main chat's (a name on the computer where its runs start). Needs new_conversation: true: a follow-up that continues this conversation runs on that conversation's launcher. Empty clears the override and follows Main chat again. Omit to keep an existing task's"
    },
    "model": {
      "type": "string",
      "description": "model id for the launcher above; without a launcher it pins the launcher the task runs on now. Empty uses the launcher's default. Omit to keep an existing task's"
    },
    "effort": {
      "type": "string",
      "enum": ["low", "medium", "high", "xhigh", "max"],
      "description": "thinking effort for Claude launchers; without a launcher it pins the launcher the task runs on now. Omit to keep an existing task's; empty clears it"
    }
  }
}
```
