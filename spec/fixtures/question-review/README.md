# Question review fixtures

`review.json` holds the review step a card of several questions shows before its answers go (#2970): the click that opens it, its Submit and Previous, and the cards that skip it. The format, rules and replay are documented with the answer fixtures in [`../answer/README.md`](../answer/README.md): its review-step paragraph and its rule table for this file.

These cases sit outside `spec/fixtures/answer` because the Android replay reads every file in that folder and does not model the review. Only `server/internal/proto/resolution_fixtures_test.go` reads this folder: the Go decoding checks and the source-backed Swift replay (`PERSONA_VERIFY_IOS_FIXTURES=1`).
