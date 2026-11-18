Set up a recurring routine: at a time every day (or selected weekdays), Persona starts a new conversation with `prompt`, you do the work, and the user gets a push with your answer (with `push: "asked"`, only what you send with notify_user). For times a time of day and weekdays can't say, pass `cron` instead of `time` / `weekdays`: a standard 5-field `minute hour day-of-month month day-of-week` in the user's time zone (`*/30 9-18 * * 1-5`, `0 10 1 * *`, `0 9 * * 1,3,5`), or @hourly / @daily / @weekly / @monthly / @yearly. Day of week 0 and 7 are Sunday; restricted day fields match either. Runs are at least 5 minutes apart; seconds, @every and TZ= are refused. A bad expression is refused; the reply lists the next runs, so check them. Pass `id` (from list_followups) to change an existing routine: only fields given change; `enabled: false` pauses it. A routine can take `until` (RFC 3339 or 30m / 2h / 1d, not a bare clock time): after it Persona turns it off and records a skipped run. Omit `until` to keep an existing end time; null clears it. The routine runs on Main chat's launcher, model and effort unless overridden with `launcher`, `model` / `effort`; an empty `launcher` returns to Main chat. `inbox` (off / worthy / every) is the user's Inbox setting: pass it only when requested. With `fresh_each_run: true`, a recurring task starts a new chat on every run and carries its state note.

```json
{
  "type": "object",
  "properties": {
    "fresh_each_run": { "type": "boolean" },
    "id": {
      "type": "string",
      "description": "to change an existing routine"
    },
    "title": {
      "type": "string",
      "description": "short name shown in the app, e.g. 早报"
    },
    "prompt": {
      "type": "string",
      "description": "what to do each time, self-contained"
    },
    "time": {
      "type": "string",
      "description": "HH:MM, 24-hour"
    },
    "weekdays": {
      "type": "array",
      "items": {
        "type": "integer",
        "minimum": 1,
        "maximum": 7
      },
      "description": "1 = Monday … 7 = Sunday; omit or empty = every day"
    },
    "cron": {
      "type": "string",
      "description": "instead of time / weekdays: minute hour day-of-month month day-of-week in the user's time zone, e.g. */30 9-18 * * 1-5 or 0 10 1 * *; or @hourly / @daily / @weekly / @monthly / @yearly"
    },
    "enabled": {
      "type": "boolean"
    },
    "report": {
      "type": "boolean",
      "description": "a daily report (such as a leader conversation's): each run opens with Persona's north-star lines (today's interruptions and availability, the change against yesterday until the same time, the source), which the report starts with. Omit to keep an existing routine's"
    },
    "push": {
      "type": "string",
      "enum": ["every", "asked"],
      "description": "every (default for a new task): each run's answer is pushed to the user; asked: only what a run sends with notify_user, for a task that watches something (it then runs in its own conversation). Omit to keep an existing task's"
    },
    "inbox": {
      "type": "string",
      "enum": ["off", "worthy", "every"],
      "description": "what of a run goes into the user's Inbox, independent of push: off (default for a new task), worthy (a run puts a row there with inbox_post only when it finds something worth a look; for a task that watches something), every (every run ends with a row made by Persona; for a briefing the user follows daily). Set it only when the user asked for it. Omit to keep an existing task's"
    },
    "run_if": {
      "type": ["object", "null"],
      "description": "Optional owner-approved shell check on the routine computer before scheduled runs. Exit 0 runs; exit 1 skips. Other errors follow on_error. Omit to keep it or use null to remove it.",
      "properties": {
        "command": {"type": "string", "maxLength": 4096},
        "timeout_s": {"type": "integer", "minimum": 1, "maximum": 60, "default": 30},
        "on_error": {"type": "string", "enum": ["run", "skip"], "default": "run"}
      },
      "required": ["command"],
      "additionalProperties": false
    },
    "until": {
      "type": ["string", "null"],
      "description": "optional end time (RFC 3339 or 30m / 2h / 1d). After it, Persona turns the task off and records a skipped run instead of running. Omit to keep an existing task's end time; null clears it. Ignored for a one-off."
    },
    "launcher": {
      "type": "string",
      "description": "this routine's own launcher, instead of Main chat's (a name on the computer where its runs start). Empty clears the override and follows Main chat again. Omit to keep an existing routine's"
    },
    "model": {
      "type": "string",
      "description": "model id for the launcher above; without a launcher it pins the launcher the routine runs on now. Empty uses the launcher's default. Omit to keep an existing routine's"
    },
    "effort": {
      "type": "string",
      "enum": ["low", "medium", "high", "xhigh", "max"],
      "description": "thinking effort for Claude launchers; without a launcher it pins the launcher the routine runs on now. Omit to keep an existing routine's; empty clears it"
    }
  }
}
```
