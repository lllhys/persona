List retained phone write receipts, optionally selecting one id. queued means waiting for a connected phone; delivering means awaiting confirmation; succeeded, declined, failed, uncertain and cancelled are terminal. pending_count counts all queued and delivering writes, even when filtering by id. Queued does not mean written. An uncertain write is never retried automatically: check the phone's actual data and pending approval before requesting another write.

```json
{"type":"object","properties":{"id":{"type":"string","description":"Optional queue receipt id."}},"additionalProperties":false}
```
