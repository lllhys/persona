Create a Persona to-do when the user asks or agrees to record work or a reminder. Immediate, without confirmation; agents execute via task_plan. An open title/due duplicate returns the existing to-do, but refuses supplied bindings: edit those with todo_repositories_get/set. Use a short action title in the user's language and details in notes. In a leader chat, records a piece of that direction; dispatch with task_plan todo_id, or finish unassigned work with todo_complete. Optional repository_bindings save atomically; get IDs/catalog_revision from repositories_list. Metadata does not create files or advance task progress.

```json
{
  "type": "object",
  "required": [
    "title"
  ],
  "properties": {
    "title": {
      "type": "string",
      "description": "what is to be done, in a line, in the user's language"
    },
    "notes": {
      "type": "string",
      "description": "Source, links, amounts and other details."
    },
    "due": {
      "type": "string",
      "description": "Due date YYYY-MM-DD when only the day is known; otherwise YYYY-MM-DD HH:MM in the user's time zone or RFC3339. Relative 2h/3d also accepted."
    },
    "remind": {
      "type": "string",
      "description": "Reminder datetime (user's time zone or RFC3339) or relative 30m/2h/1d; never past. Omit unless requested or the deadline is close and important."
    },
    "priority": {
      "type": "string",
      "enum": [
        "high",
        "normal",
        "low"
      ],
      "description": "normal when left out"
    },
    "kind": {
      "type": "string",
      "enum": [
        "todo",
        "concern"
      ],
      "description": "todo (default); concern (main chat only) tracks a user-raised matter until concern_report reports its outcome; other completion tools cannot close it."
    },
    "project": {
      "type": "string",
      "description": "Existing knowledge-base project slug/name (kb_list projects), preferably a long-term work area. Set only when clear; do not invent near-duplicates."
    },
    "part_of": {
      "type": "string",
      "description": "Request to-do id this is a piece of; hand out via task_plan todo_id."
    },
    "after": {
      "type": "array",
      "items": {
        "type": "string"
      },
      "description": "With part_of: sibling piece ids it waits for."
    },
    "repository_bindings": {
      "type": "object",
      "required": [
        "catalog_revision",
        "repositories"
      ],
      "properties": {
        "catalog_revision": {
          "type": "integer",
          "minimum": 0
        },
        "repositories": {
          "type": "array",
          "maxItems": 20,
          "items": {
            "type": "object",
            "required": [
              "repo_id"
            ],
            "properties": {
              "repo_id": {
                "type": "string"
              },
              "directory_alias": {
                "type": "string"
              },
              "requested_ref": {
                "type": "object",
                "properties": {
                  "kind": {
                    "type": "string",
                    "enum": [
                      "default",
                      "branch",
                      "tag",
                      "commit",
                      "ref"
                    ]
                  },
                  "value": {
                    "type": "string"
                  }
                },
                "additionalProperties": false
              },
              "links": {
                "type": "array",
                "items": {
                  "type": "object",
                  "properties": {
                    "url": {
                      "type": "string"
                    },
                    "kind": {
                      "type": "string",
                      "enum": [
                        "issue",
                        "pull_request"
                      ]
                    },
                    "number": {
                      "type": "string"
                    },
                    "title": {
                      "type": "string",
                      "description": "Full source title as plain text; max 1,024 UTF-8 bytes after trimming.",
                      "maxLength": 1024
                    }
                  },
                  "additionalProperties": false
                }
              }
            },
            "additionalProperties": false
          }
        }
      },
      "additionalProperties": false
    }
  }
}
```
