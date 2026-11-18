Replace associations atomically using revisions from todo_repositories_get and repositories_list. [] removes metadata only. Retained source snapshots stay fixed; remove and reselect to adopt a changed catalog identity. Editable after execution. Omitted links preserve; [] clears. Use {url} or {kind,number}; omitted/empty titles preserve retained titles. On conflict reread and reconcile. No files, task progress or completion changes.

```json
{
  "type": "object",
  "required": [
    "id",
    "expected_revision",
    "expected_catalog_revision",
    "repositories"
  ],
  "properties": {
    "id": {
      "type": "string"
    },
    "expected_revision": {
      "type": "integer",
      "minimum": 0
    },
    "expected_catalog_revision": {
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
```
