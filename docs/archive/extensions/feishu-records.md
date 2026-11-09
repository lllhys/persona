# Lark gateway: records

Moved unchanged from the [Lark guide](../../extensions/feishu.md) ("Topic pagination limit (H06)"), which now keeps only the conclusion.
This is the read-only review of the gateway's source that found topic replies page only backward.
Written 2026-11-06, before the current issue tracker. `git blame` dates it to the move; blame the parent of the move commit to see the original lines.

## Topic pagination: gateway source review (2026-10-31)

Read-only gateway source review 2026-10-31, version `2da814b1da1fd2ddd0cb9df05c419f7c87421739`. `GET /agent/v1/messages/:messageId/thread` supports latest page / `beforePosition` older, **not newer forward page**:

- `src/server/routes/api.ts` reads only `beforePosition` / limit, passes to `hydrateThread` / threadPage. `afterPosition` ignored; absent `beforePosition` latest.
- `src/server/lark/domains/im-domain.ts` `pullThread` only `beforePosition`, retrieves from end / before position; `src/server/services/messenger-service.ts` also one direction.
- `src/server/repositories/messenger-repository.ts` `threadPage` queries `threadPosition < beforeThreadPosition`, descending then reverse; items / `hasOlder` / `nextOlderCursor`, no `hasNewer` / nextNewerCursor.
