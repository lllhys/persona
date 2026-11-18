# Review answers

What the node's `git.review` really answers (`GET /api/v1/machines/{mid}/git/review`, [review.md](../../../docs/web/review.md), #151), for the clients to decode. Written by `TestGitReviewWireFixture` in `server/internal/node/gitreview_fixture_test.go` from real repositories, with the temporary folder, commit and blob ids replaced by placeholders; rewrite it with `go test ./internal/node -run '^TestGitReviewWireFixture$' -update` and read the diff.

The answer leaves out what is empty (`omitempty`, `server/internal/proto/review.go`), and a client reads it as empty:

| Case | What it pins |
| --- | --- |
| `branch_hunks_without_header` | A hunk with no line above it to name, every hunk of a new file and of one Git does not track yet have no `header`; a hunk further down has one. |
| `no_commit_yet` | Before the first commit the base is `{"empty": true}`, with no `hash`, and there is no `head`. |

Read by `apple/PersonaTests/ReviewLogicTests.swift` (`readsHunksWithoutAHeader`, `readsARepositoryWithNoCommitYet`). iOS once required both fields and could not read a real review while the mock hub, which always sent them, passed (#8733). The web's types (`web/src/lib/session-review.ts`, `web/src/lib/code-view.ts`) have both optional.
