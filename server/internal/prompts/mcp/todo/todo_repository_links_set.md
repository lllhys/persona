Replace all links for repo_id at the revision from todo_repositories_get; URLs must match its saved identity. Use {url} or {kind,number}. [] clears; omitted/empty titles preserve retained titles. On conflict reread and reconcile. No files, task progress or completion changes.

```json
{
  "type": "object",
  "required": [
    "id",
    "repo_id",
    "expected_revision",
    "links"
  ],
  "properties": {
    "id": {
      "type": "string"
    },
    "repo_id": {
      "type": "string"
    },
    "expected_revision": {
      "type": "integer",
      "minimum": 0
    },
    "links": {
      "type": "array",
      "maxItems": 50,
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
```
