Record discovered/opened issue/PR links without manual user entry. Accept 1–50 exact URLs across links:[{url,title}] and legacy urls; group repositories automatically, including unlisted ones. Use actual source titles, never guesses; nonempty titles refresh, omitted/empty preserve. Deduplicate links; this explicit call can restore removed links. Atomic; preserves aliases, refs and source snapshots. Use todo_repositories_get and revision-checked set tools to replace/remove. Associations do not prove completion.

```json
{
  "type": "object",
  "required": [
    "id"
  ],
  "properties": {
    "id": {
      "type": "string"
    },
    "urls": {
      "type": "array",
      "minItems": 1,
      "maxItems": 50,
      "items": {
        "type": "string"
      }
    },
    "links": {
      "type": "array",
      "maxItems": 50,
      "items": {
        "type": "object",
        "required": [
          "url"
        ],
        "properties": {
          "url": {
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
  "additionalProperties": false,
  "anyOf": [
    {
      "required": [
        "urls"
      ]
    },
    {
      "required": [
        "links"
      ]
    }
  ]
}
```
