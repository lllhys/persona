# Shared server test latency: fixed-revision verification (#5730)

> Historical measurements: the November 12 comparisons use their explicitly recorded sources, not the
> branch after synchronizing main on November 13. The first synchronization incorporates main
> `5474a23c63c7abd61a3d1126f675796987fc7bbe`; a later synchronization incorporates
> `a74f49300d54054d28c4ca1549a41a9ffa31c6fb`, including #6286's automatic plain-run partitions and resident
> test isolation. Each timing and failure measurement describes only its recorded source.
> See the dated synchronization and recent-PR comparison sections below.

Audience: contributors and maintainers running bounded server verification. This record concerns shared test cost,
independently of the Web skill picker (#5473) and ZIP importer (#5581). It does not establish either feature's merge readiness.

## Revisions and environment

- Main baseline: `f1468ef3d2ae9566430d2357e7d5bb9d3a973b81`, fetched before creating the independent optimization branch.
- Initial implementation: `522192ca9a5e576fd5203decc5acfe13b321d89d`.
- Implementation used by the completed main full-suite measurements, including the Codex polling correction: `be189594939b3294f07c5e634c8c74cc89b1524c`.
- Go 1.27.1, Linux/amd64, 32 allowed CPUs. Historical reports used Go 1.27.3; these measurements do not reproduce that toolchain.
- Independent baseline/head source copies; no other large Go verification jobs launched by this task during the final matched queue.
  The host is shared, not exclusively reserved. Each run records load, exact command, revision, exit code and wall time.
- Runtime/profile/proxy variables removed with `server_test_env.clean_environment`. Go settings/cache paths resolved before
  replacing HOME. HOME, USERPROFILE, temporary directories and XDG_RUNTIME_DIR point at a fresh owned fixture for each run.
  No test database URL is set. Existing database/platform/opt-in skips remain visible; database coverage is unavailable locally.
- Cold means an empty Go build cache, with the module cache already populated. OS caches were not flushed.
  Both variants were compiled into their warm cache before matched execution; preparation is excluded from test timings.
- A local supervisor applies the unchanged 600-second outer deadline, terminates the owned process group, and kills it after
  five seconds if needed. It also checks for detached processes carrying that run's unique temporary-directory marker.
  Cleanup and remaining owned processes are recorded separately. Raw logs remain private; only derived measurements are published.

An exploratory eight-package sample lost its outer supervisor when its queue coordinator was stopped. It is excluded from
all timing conclusions. The final queue keeps its supervisor alive throughout every sample.

## What changed

`testNode` previously paused 300 ms on every scripted-shell start even though that fixture's shell was already ready.
The existing test seam now declares the shell settled. Shell-readiness tests still call `watchShell`, exercising the real
observation path, startup questions, changing screens and slow shells.

Scripted Claude and Codex hosts also use a 20 ms readiness poll instead of paying the normal 500 ms between synchronously
produced frames. Timing-sensitive tests keep the production cadence through `withStartWaits`; normal nodes remain
at 500 ms. Virtual-time tests exercise real captured screens, a working frame resetting readiness, cancellation
before a read and the start deadline. They do not wrap real sockets.

`herdrNode` previously joined unfinished first-message checks only after their recovery window expired. The fixture now owns
a cancellation context, stops its starts, cancels and joins its checks, then joins engine lists before restoring shared state
or closing its socket. Delivery/recovery assertions still wait for `firstWait` themselves. Normal nodes retain the detached
background context, production deadlines and retry counts. No test, assertion, security fingerprint or opt-in boundary was removed.

The Codex app-server test fixture also polled for each approval response. It now registers a waiter before sending
and wakes it from the existing JSON-RPC reader, matched by request ID. Its five-second budget and all security
classifications remain unchanged. Cancellation, deadlines, EOF, write failures, unrelated replies and JSON-RPC errors
are exercised through real pipes and held node/hub approvals. The fixture reader is joined at cleanup.

The same validation exposed a race in `TestCodexStartAndTurns`: it reassigned the node directory while an earlier
turn-completed notification was still listing sessions. Fixed main reproduces the identical write/read race.
Configuring the test directory before starting its transport preserves the image/turn assertions and removes that race.

This reuses the partition runner already merged in #5615/#5675; those changes are not part of this delivery. At the
November 12 sources, direct `go test` did not select partitions automatically. The later synchronization retains
that behavior's upstream replacement from #6286. See `docs/operations/testing.md` ("Bounded server package verification").

## Before/after measurements

The first completed comparison below uses the initial `522192ca9a5e` implementation; it is retained after the Codex correction.

| Measurement | Main baseline | Implementation | Outcome |
| --- | --- | --- | --- |
| Cold eight-package test compilation | 46.153 s | 47.022 s | Both exit 0; no compilation speedup |
| Five-package direct command, warm cache | 600.108 s | 600.118 s | Both outer timeout, exit 124 |
| Eight-package direct command, warm cache | 600.107 s | 600.108 s | Both outer timeout, exit 124 |

The direct calls remain over the external budget. They must not be reported as passing. Their supervisor cleanup
took 0.089–0.090 s, with zero remaining owned processes; compilation and supervisor cleanup do not explain the
600-second execution limit. The head's completed package summaries include assistant at 210.906/210.832 s and
hub at 133.337/132.411 s in the five/eight-package calls respectively; node has no package terminal before termination.
Normal Go output does not provide a complete test-terminal inventory for a terminated package.

The separate warm compilation preparations took 3.364 s (base) and 28.981 s (head). These are preparation costs,
excluded from the execution comparison. A source change invalidates its own build artifacts even when dependencies
are already cached; the cold-cache comparison above measures that separately.

The initial complete eight-package partitioned comparison took **439.601 s → 322.021 s** (26.7% lower wall time),
both exit 1, with the same single Chrome failure. The 7,282 common roots consumed 1,125.060 s → 971.600 s summed
elapsed time, saving 153.460 s. Node workers ended in 348.859/435.917 s before and 313.027/318.405 s after.
These overlapping worker times must not be added to the package-set wall time. Adding regressions reshuffles the
existing round-robin partitions, so common-root elapsed is also reported to avoid crediting scheduling luck.

The measured `be189594939b` source's cold eight-package compilation took 45.541 s (baseline 46.153 s); warm preparations took
3.292 s before and 25.334 s after. There is no meaningful cold-compilation improvement to attribute to fixture
changes. These preparations are excluded from test execution comparisons.

Largest observed reductions in the initial comparison (top-level elapsed includes fixture cleanup):

| Test | Baseline | Initial head | Saved |
| --- | --- | --- | --- |
| `TestStartSubmitsImageAndLongPaste` | 23.970 s | 4.330 s | 19.640 s |
| `TestEngineSwitchHoldsTheEarlierSessionWhileItResumesIt` | 10.680 s | 3.350 s | 7.330 s |
| `TestEngineSwitchFallsBackToAFreshSession` | 6.150 s | 1.370 s | 4.780 s |
| `TestEngineSwitchLooksAgainBeforeTheQuit` | 4.780 s | 0.280 s | 4.500 s |
| `TestEngineSwitchLeavesAnEarlierSessionBeingResumed` | 4.780 s | 0.280 s | 4.500 s |
| `TestEngineSwitchBackResumesTheEarlierSession` | 4.840 s | 0.350 s | 4.490 s |

At the final source `be189594939b`, the original five-package command still times out at **600.084 s**, and the
eight-package command at **600.083 s** (both exit 124). Supervisor cleanup is 0.068/0.067 s respectively, with zero
remaining owned processes. These outcomes remain failures to meet the original direct-command budget.

The final complete partitioned comparison is **439.601 s → 356.385 s**, saving **83.216 s (18.9%)** in observed
wall time. Both exit 1. Common-root elapsed falls **1,125.060 s → 954.020 s**, saving **171.040 s**. This reduces
common test cost more than the initial version, but its additional three regression roots reshuffle the existing
round-robin assignments: final node workers take 261.841/352.818 s, compared with the initial head's
313.027/318.405 s. The later observed wall time, rather than the faster initial trial, is the full-suite measurement at `be189594939b`.

Warm time before the first JSON event is 2.456/2.410 s before/after. Final node workers spend 0.027–0.028 s
between process start and the first test, and 0.009–0.010 s after the last test terminal. Baseline workers spend
0.028–0.029 s and 0.010–0.012 s respectively. Supervisor cleanup is 0.026 s for both, with zero remaining owned
processes. Fixture teardown occurs inside each test's elapsed time; these phases separate it from process cleanup.

The original commands, without diagnostic or internal-timeout flags, are:

```sh
go -C server test -count=1 \
  ./internal/assistant ./internal/assistantskills ./internal/assistantwire ./internal/hub ./internal/node
go -C server test -count=1 \
  ./internal/apierr ./internal/assistant ./internal/assistantskills ./internal/assistantwire \
  ./internal/hub ./internal/i18n ./internal/node ./internal/prompts
```

Complete event accounting uses the existing runner separately, from `server/`, under the same 600-second outer limit:

```sh
python3 ../scripts/test/go_test_partitioned.py go test -json -count=1 \
  ./internal/apierr ./internal/assistant ./internal/assistantskills ./internal/assistantwire \
  ./internal/hub ./internal/i18n ./internal/node ./internal/prompts
```

That diagnostic adds `-json` and the runner's discovered node `-run` patterns. It is not an original direct command, and a
completed partitioned run is not a direct-command pass. Parent/subtest durations overlap; summed elapsed time uses top-level tests only.

## Additional response-event optimization

This follow-up is test-only. Its source commit is `be8e80bbdb6d3452621be9227fe8a11e26167a07`. The completed main full-suite measurements
above remain attributed to `be189594939b`; they are not relabelled as measurements of the later source. Completed
five/eight-package commands were not repeated. The following comparison selects the entire existing security matrix:

```sh
go -C server test -json -count=1 -run '^TestAssistantBypassShellNames$' ./internal/node
```

| Warm-cache measurement | `be189594939b` | Reply-event fixture | Result |
| --- | --- | --- | --- |
| Command wall time | 26.684 s | 3.442 s | Both exit 0; 23.242 s (87.1%) saved |
| Root test elapsed | 24.930 s | 1.670 s | 23.260 s (93.3%) saved |
| Terminal identities, including nested cases | 3,517 pass | 3,517 pass | Zero missing, added or changed states |

The matrix still evaluates all 1,172 commands through both approval adapters. The observed saving matches the
removed 20 ms polling tick per Codex response; no command, expected decision or phone-interaction assertion changes.
Before the fix, a real held approval response advanced the virtual clock by 20 ms and failed
`TestFakeCodexRequestWakesOnItsReply`. Afterward it wakes without advancing time. Preparation/compilation is excluded
from the matched warm-cache comparison; first green compilation is recorded separately in the receipt artifact.

Four new regression roots passed 20 repetitions (0.125 s execution, 1.788 s wall). All 44 roots in the five existing
fixture-consumer files plus those four regressions passed three repetitions: 48 roots and 4,577 identities, exactly
three pass terminals each (13,731 events; 17.723 s wall). Filtering the earlier full `be189` JSON to those 44 existing
roots also reconciles all 4,568 common identities to identical states in each repetition, with no missing or added
identity inside an existing root.

The first consumer race run failed (65.867 s wall, 35.567 s execution) at the concurrent directory reassignment
in `TestCodexStartAndTurns`. The identical isolated `-race -count=20` command reproduces that race on fixed main
(3.290 s wall, exit 1). Initializing the directory before transport startup then passes the same 20 repetitions
(32.358 s wall including recompilation, 2.059 s execution) and the complete 48-root consumer selection with
`-race -count=3` (38.508 s wall, 36.292 s execution, exit 0). The broader `codexWaitTick` race described below remains
an independent retained failure. Neither race is attributed to the external plain-test FAIL reports.

## Terminal-event reconciliation

The baseline partitioned run completed all 7,282 inventoried top-level tests exactly once: 6,889 passed,
392 retained existing skips, and one failed (`TestPreviewDrivesARealBrowser`: Chrome closed before answering).
Including subtests, it recorded 16,948 terminal identities (16,352 pass, 595 skip, one fail), with zero duplicate
run/terminal events and zero unfinished tests. No inventoried root was missing or unexpected.

The initial head has all 16,948 common terminal identities with identical pass/fail/skip states, plus nine
terminal identities belonging to its six added regression roots. There are zero removed identities, changed
states, duplicate runs/terminals, unfinished tests, or missing/unexpected inventoried roots.

The final inventory contains 7,291 roots: all 7,282 baseline roots plus nine regressions, with no deletion.
The final JSON has 6,898 passed roots, 392 existing skipped roots and the same single browser failure. Including
subtests it has 16,368 pass, 595 skip and one fail (16,964 identities): all 16,948 common identities preserve their
terminal state, and 16 added identities belong to the nine regressions. There are zero missing roots, unexpected
roots, duplicate run/terminal events, unfinished identities, removed identities or changed common states.

The accompanying [root terminal table](2026-11-12-server-test-latency-5730.tsv) lists every inventoried root in
the baseline, measured `be189` main source and both patched feature snapshots. `absent` means that snapshot lacks
the root, `not_selected` means its package is outside that command, and `no_terminal` means an inventoried root did
not complete. An elapsed value of `n/a` means that main snapshot has no measured terminal for the root.
The main optimized column explicitly names `be189`; it does not represent a new full-main run at
the later reply-event source. Dynamic subtest names and raw output are withheld. Full private nested manifests
(`.terminals.json` schema) have SHA-256 `76f4517c1d6e95b88b5026ae5f51415e58a4910a569793cf89c12f9a855d6a06`
for main and `7d78d5591b72bbb315d9ea54ef1b98890efc48ab578b32c95eadb789bb5333b7` for `be189`.

[Derived receipts](2026-11-12-server-test-latency-5730-results.json) retain exact argv, source revisions, exit status,
wall time, load samples and owned-process cleanup counts. Checkout/output paths use explicit placeholders. They
include the 48-root normal/race selection, failed race reproductions and the first unsuccessful feature-runner
invocations, so those failures cannot be mistaken for completed feature suites.

## Feature-head integration

Both reported heads were exported independently, without modifying their branches. The startup/teardown patch applies cleanly.
The Claude readiness patch needs context-only adaptation in `waitReady`: those snapshots predate main's additional pane-disappearance
handling beside the polling select. The same polling seam is applied without porting or changing that existing pane handling:

- #5473: `cb2ba1521e01ec956dd367d3f84e87a01a5ee677`.
- #5581: `cc575818ac55207696518a2674c3fc5b0505d7ab`.

The subsequent shared Codex polling patch and the test-only reply-event patch `be8e80bbdb6d` apply cleanly to both
patched exports. Older snapshots lack main's later `fakeHub.onContext` seam; that unrelated version difference is
preserved. The helper implementation and new regression file match the optimization source.

Both feature snapshots predate the existing repository partition entrypoint. Initial relative-path invocations
returned exit 2 before tests (0.037/0.036 s); those receipts provide no complete-suite credit. The corrected invocation
runs the already merged fixed-main runner by absolute path from each feature's `server/` directory. It discovers and
executes that feature snapshot's own tests, without copying or redesigning the scheduler:

```sh
# Set RUNNER_CHECKOUT to the fixed-main runner checkout.
# Run from the isolated feature export's server directory.
python3 "$RUNNER_CHECKOUT/scripts/test/go_test_partitioned.py" go test -json -count=1 \
  ./internal/apierr ./internal/assistant ./internal/assistantskills ./internal/assistantwire \
  ./internal/hub ./internal/i18n ./internal/node ./internal/prompts
# For #5581, select assistant, assistantskills, assistantwire, hub and node instead.
```

The runner checkout is the frozen `be189594939b` main-based source; its existing scheduling code is unchanged from
the fixed baseline. Each invocation retains the 600-second outer deadline. These are the first actual partitioned
executions for the exported feature sources; their earlier completed direct calls are not repeated.

| Export and selected set | Earlier direct call with `be189` patches | Complete partitioned call with `be8e` patches | Root terminals |
| --- | --- | --- | --- |
| #5473, eight packages | 600.122 s, exit 124 (outer timeout) | 300.666 s, exit 1 (failure, no outer timeout) | 6,866 pass / 392 existing skip / 1 fail = 7,259 |
| #5581, five packages | 600.121 s, exit 124 (outer timeout) | 312.675 s, exit 1 (failure, no outer timeout) | 6,797 pass / 392 existing skip / 1 fail = 7,190 |

Both corrected runs have zero missing/unexpected inventoried roots, duplicate run/terminal events, unfinished
identities or remaining owned processes. Their private audits include 16,897 and 16,845 nested identities respectively.
The sole failure is `TestPreviewDrivesARealBrowser` at `preview_live_test.go:149`, with the same Chrome-closed assertion
seen on fixed main (1.620 s in #5473, 0.070 s in #5581). No different failure is hidden behind the completed runner.
These exports have no complete unpatched same-feature timing sample; comparison to main is not a feature speedup claim.
Their earlier direct invocations use the preceding implementation and are not measurements of `be8e`.

Relative to each feature's earlier patched inventory, the reply-event change adds exactly four roots and deletes none:
7,255 → 7,259 for #5473 and 7,186 → 7,190 for #5581. The combined public root table contains 7,334 distinct roots across
all four source inventories, SHA-256 `d0e0ad8110e211473cebf820fa840dc734c6ae9f720de055237294c759c3c764`.
This union includes version-specific tests; an older snapshot's `absent` entries are not deleted tests in this PR.

Their source and test inventories differ from the fixed current-main baseline. Results are kept separate, with the feature
SHA and optimization patch identified; they do not resolve #5473's merge conflict or establish either feature as fully green.

## Regression and repository checks

- Before the fix, `TestHerdrFixtureCancelsUnfinishedPromptChecks` failed after 3.41 s because teardown waited for a recovery
  failure instead of cancelling the check. `TestFixtureShellIsAlreadySettled` failed because its virtual clock advanced 300 ms.
- The initial six fixture/lifetime/readiness regressions passed 20 repetitions (0.902 s test execution). The detached-lifetime regression
  confirms that a normal check can receive its transcript receipt after its submitting request is cancelled.
- Before the Codex correction, three additional regressions failed at the expected 500 ms fixture poll, including
  matching-rollout success, launcher failure and the fixture deadline. All nine regressions then passed 20 repetitions
  (0.956 s test execution). Cancellation still ends the pending poll without reading the host, and a normal node
  still waits 500 ms. The success case reads real rollout files and rejects a different first message.
- Initial risk-focused `-race -count=3` passed: the six regressions, dropped-message recovery, retained/never-delivered prompts,
  image/long-paste submission, unrelated drafts/dialogs, startup questions, real shell settling, unknown engine state and a disappeared Orca pane (68.491 s test execution).
- `go -C server build ./...`, `go -C server vet ./...`, the Windows/amd64 build, and the full `herdrlive` node test compile passed.
  Live agents were not started. The repo-wide lazy-regexp, architecture, timezone and plugin guards passed.
- Existing partition discovery (6 tests), partition runner (9 tests), and isolated server launcher/environment (6 tests) passed.
  The sleep ratchet passed without baseline changes. No shell or workflow file changed; additional shell/workflow lint is not applicable.
- The expanded Codex `-race -count=3` run exited 1 at `TestCodexStartWithoutAMessage`: restoring
  `codexWaitTick` in `withCodexWaits` races with the detached `lookForCodexThreads` loop. Neither waiting-path file
  changes in this PR. The identical isolated command below also failed with the same race on fixed main
  (46.078 s wall, including race compilation) and final head (5.095 s wall). This is retained as an existing failure;
  a passing focused race run does not establish that the broader race suite is green.

  ```sh
  go -C server test -race -count=20 -run '^TestCodexStartWithoutAMessage$' ./internal/node
  ```

- Final focused `-race -count=3` passed (68.558 s test execution, 70.808 s wall). Its exact selection covers the
  nine new regressions, real Codex start/permission/launcher/trust paths, concurrent starts, stalled-start failure,
  cancellation and abandoned-pane cleanup/retry. The preceding broad failure and both baseline/head race
  reproductions remain above; no test or failure was removed from the normal suite.
- Final build, vet, Windows/amd64 build, complete `herdrlive` test compile and the four repository guard packages
  passed together (55.179 s wall) at `be189594939b`.
- Final document-path, Go-path, section-reference and wire-document checks are recorded in the derived receipts.
  Privacy checks cover tracked/staged/range contents and commit/reply text before publication. Only two private
  values are available from local configuration; a clean scan is bounded by that available configuration. Public
  artifacts contain derived measurements and static root names, with private checkout paths replaced by placeholders.

The local `scripts/ci/server.sh node` failed identically on baseline and implementation at the Postgres 18 fixture (exit 1,
Docker socket permission denied); tests did not start through that lane. No permission or global configuration was changed.
A manual Server CI dispatch on the authorized fork returned HTTP 404: its default branch has no registered workflows.
No hosted CI result is claimed, and that default branch was not changed. A maintainer must provide an available CI ref/environment.

Remote automated verifier comments on [PR #5845](https://github.com/nwverse/persona/pull/5845) report node-package
failures at 540.403 s for `be1895949`, 539.305 s for the documentation-only `98e5d33bd`, and 504.553 s for
the reply-event implementation `be8e80bbd` (2026-11-12T15:37:35Z). All three report package FAIL, not timeout.
The complete comment bodies supply only a short output tail, without named failing assertions, the full command or
environment. Ordinary refusal/resume/close messages in that tail are not identified failed assertions. The package
elapsed values do not establish that the complete external verifier met its deadline or passed, and do not attribute
its failure to the local Chrome result.

The first two SHAs were checked through GitHub's check-runs, check-suites, combined commit-status and Actions-run APIs:
all contain zero associated entries; the combined status is `pending` with an empty statuses array. PR reviews and
inline review comments are empty. Recent repository Actions runs precede this PR and use other SHAs. The repository
contains no matching automated-verifier implementation or full-log link. Issue #5730 contains only its original claim.
Consequently no complete watcher log or named remote failure is available through these public sources. The new `be8e80bbd` comment likewise contains only the short normal-log tail and package FAIL, with no named
failed assertion. Its lower package elapsed time is not a controlled before/after sample or proof that the complete
external verifier met its deadline. The detailed external failure cause remains unknown; no additional logs are requested or awaited.
Local failures and measured hotspots are investigated independently. Local direct calls at `be189594939b` still time out as recorded above.

- [Verifier report, 2026-11-12T14:42:57Z](https://github.com/nwverse/persona/pull/5845#issuecomment-6826718109).
- [Verifier report, 2026-11-12T14:52:24Z](https://github.com/nwverse/persona/pull/5845#issuecomment-6826959409).
- [Verifier report at the reply-event source, 2026-11-12T15:37:35Z](https://github.com/nwverse/persona/pull/5845#issuecomment-6828106659).

No UI or native code changed: screenshots, native builds and online deployment verification are not applicable. Nothing was merged or deployed.

## Main synchronization on November 13

This update merges main `5474a23c63c7abd61a3d1126f675796987fc7bbe` into the existing PR branch. Merge commit
`d875f617235a466d7a3a20fae3327443e450dc73` retains both that main commit and the previous PR head
`fc534efd464c85a11f0c2fa077af577f18fd91ee` as parents. No feature branch was imported separately.

The only manual conflict was the top of `docs/lessons/testing.md`: both fixture lessons and the upstream lesson
about settling background list counters were retained. `node.go` merged automatically, retaining the existing
fixture seams alongside main's changes. The four test files carrying #5939, #6039, #6133, #6141 and #6187 match
this fixed main byte for byte, and all five commits remain ancestors. The diff against synchronized main contains
only this PR's fixture changes, regressions and documentation.

The following checks use Go 1.27.1 on Linux/amd64, 32 allowed CPUs, warm shared Go caches, fresh owned HOME/temp/runtime
directories, no test database URL and a shared host. The baseline is an isolated export of the fixed main tree.
The queue is serial across major Go jobs. Every supervised command retains a 600-second outer deadline; completion,
nonzero failure and timeout remain separate outcomes. These measurements do not update or replace the November 12 samples.

| Complete eight-package partitioned run | Wall time | Exit / outcome | Root terminals |
| --- | --- | --- | --- |
| Fixed main `5474a23c63c7` | 434.166 s | 1 / failure, no outer timeout | 7,047 pass / 405 existing skip / 1 fail = 7,453 |
| Merge source `d875f617235a` | 299.227 s | 1 / failure, no outer timeout | 7,060 pass / 405 existing skip / 1 fail = 7,466 |

The only failure on both sides is `TestPreviewDrivesARealBrowser`, `preview_live_test.go:149`, with
`Google Chrome did not answer: the browser closed` (0.070 s on main, 1.640 s after merging). This comparison does
not identify the external verifier's failure or establish that the external gate passes.

All 17,418 common terminal identities have identical pass/fail/skip states. The merge adds 25 identities belonging
to its 13 regression roots, with zero removed identities or changed common states. Both runs have zero missing or
unexpected inventoried roots, duplicate runs/terminals or unfinished identities. Each supervisor records 0.026 s
cleanup and zero remaining owned processes. These are complete failure outcomes, not passing suites.

The complete command is run from each tree's `server/` directory:

```sh
python3 ../scripts/test/go_test_partitioned.py go test -json -count=1 \
  ./internal/apierr ./internal/assistant ./internal/assistantskills ./internal/assistantwire \
  ./internal/hub ./internal/i18n ./internal/node ./internal/prompts
```

The eight-package selection includes all roots in the reported five-package set. It is not a new direct `go test`
five/eight-package sample. The existing partition runner and its parameter/failure behavior are unchanged. The
[November 13 synchronization receipts](2026-11-13-server-test-sync-5730.json) contain exact argv, measured source
SHAs, exits, load and cleanup data, inventory/terminal reconciliation, retained upstream fix hashes and check results.
Raw output and dynamic subtest names remain private.

- All 13 fixture/lifetime/readiness regression roots passed 20 repetitions: 25 identities, exactly 500 pass terminals.
- The related 68-root selection passed `-race -count=3` (105.007 s test execution, 142.503 s wall).
- The five targeted upstream roots ran five times. Workflow launch-line, idle-list counting and both widget-wake roots
  passed. Codex continuation/paginated-history retained its existing skip because this Linux environment has no
  `sqlite3` executable; that assertion was not exercised locally.
- The complete server build lane passed (107.255 s wall): formatting, vet, Linux build, Windows build/vet,
  release-tag programs on Linux/Darwin, tagged tunnel/build-tag tests, extension schema/SDK bundle and Go SDK tests.
- Complete `herdrlive` node test compilation passed (12.264 s wall); no live agent was started.
- Repository guards passed: privacy, frozen migrations (101 unchanged), sleep ratchet (322 files, no increase),
  web font ratchet, path-case and external-reference checks. Four document guards and existing partition discovery,
  runner and isolated-environment Python suites (6/9/6 tests) passed. Final publication privacy checks cover tracked,
  staged and range contents; only two configured private values are available.
- No shell/workflow/UI implementation differs from synchronized main, so additional shell/workflow lint or UI/native
  screenshots are not applicable to this merge resolution. Nothing was merged upstream or deployed, and no new
  GitHub comment/review was sent. External verification remains separate from the local results above.

## Recently merged PR comparison on November 13

Merge status alone is not evidence that the same external command passed. The following public records were
read when investigating why recently merged PRs appeared unaffected:

| PR | Available verification evidence |
| --- | --- |
| [#6264](https://github.com/nwverse/persona/pull/6264) | The external plain `go test -count=1 ./internal/node` timed out at 600 s. The author's local two-partition runs completed in 321/375 s; the follow-up explains the merge used that local verification. |
| [#6261](https://github.com/nwverse/persona/pull/6261) | The external assistant/node/proto command timed out at 600 s. The author reports a 673.3 s local node run with a larger Go timeout; the reviewer reports focused validation. This is not a 600 s full-command pass. |
| [#6233](https://github.com/nwverse/persona/pull/6233) | The external seven-package command timed out at 600 s. The author's partitioned node verification retains two failures also seen on their main. |
| [#6290](https://github.com/nwverse/persona/pull/6290) | Its reported eight affected packages do not include `internal/node`, so they do not exercise the real-browser test. |
| [#6286](https://github.com/nwverse/persona/pull/6286) | The verifier finished node in 160.028 s, then 149.897 s, both FAIL. The later report names `TestResidentReloadReapsIdleProcessesBeforeTransportEnds`. The final version fixes that test's home isolation. |

The [#6264 timeout](https://github.com/nwverse/persona/pull/6264#issuecomment-6843933661),
[#6261 timeout](https://github.com/nwverse/persona/pull/6261#issuecomment-6843855473),
[#6233 timeout](https://github.com/nwverse/persona/pull/6233#issuecomment-6843303871), and
[#6286 named failure](https://github.com/nwverse/persona/pull/6286#issuecomment-6844165003) are different outcomes
from #5845's [493.632 s package FAIL at 96c1deb30](https://github.com/nwverse/persona/pull/5845#issuecomment-6842853882).
The older #5845 reports do not name a failed assertion. Their cause remains unknown; the named failure on another
PR is not proof of the same cause here. No additional external logs are requested or awaited.

`liveTab` already skips in short mode or when no browser is found; when one is found, startup must succeed.
Those merged PRs do not supply per-test terminal evidence distinguishing a browser pass from an existing skip.
Locally, an independent launch probe with the same CDP pipe arguments, an owned profile and the sandbox enabled
aborts with `No usable sandbox!`, before `Browser.getVersion` answers. The Playwright browser's user-namespace
UID mapping fails, and its installed helper is not a setuid sandbox. This explains the local failure, not the
external #5845 FAIL. Browser/CDP/launcher/live-test source is identical between the fixed main and this PR;
no sandbox bypass, host permission or global policy change was made.

The resident-test comparison uses only disposable homes. A parent holds a real `flock` on the fixture's
`.persona/node-attach.sock.run` while running the existing reload test three times in a child test binary.
On both main `5474a23c63c7` and PR code `d875f617235a` (also the code at docs-only `96c1deb30`), the test passes
3/3 without the held lock and fails 3/3 with `a resident node is already running (...run is locked)` when it is held.
An overlay containing only #6286's `shortHome(t)` addition passes 3/3 in either case. The parent's lock remains
held throughout; there is no live-node access, socket deletion or leftover owned process. This demonstrates an
existing test-isolation defect and validates the upstream correction without attributing the old external failure.

The next synchronization pins main `a74f49300d54054d28c4ca1549a41a9ffa31c6fb` and merges it into the original
PR branch as `c428e8bfcdbb13b55a7f169ae48a8201769fae14`, without conflicts. It retains #6286's automatic plain-run
partitions, failure summary and resident isolation, #6264's machine-Claude isolation, and #6261's Codex validation
alongside this PR's fixtures. These are upstream fixes, not new work claimed by #5845. No new scheduler or
production browser change is introduced by this PR. Older timing samples remain tied to their recorded sources.


## Verification after retaining the upstream plain-run partitions

Fixed baseline: `a74f49300d54054d28c4ca1549a41a9ffa31c6fb`. Optimized code:
`c428e8bfcdbb13b55a7f169ae48a8201769fae14`. Documentation commits after that code source do not change its
test implementation. The baseline is an isolated archive of that exact main tree. Both sources use Go 1.27.1,
Linux/amd64, 32 allowed CPUs, the same warm shared Go caches and a fresh owned HOME/temp/runtime per command.
The host is shared; the receipts preserve load samples rather than claiming exclusive resources.
Major Go jobs execute sequentially. Each command keeps the 600 s outer deadline, including its Go invocation.

| Complete command | Fixed main | Optimized code |
| --- | --- | --- |
| Direct eight-package `go test -count=1` | 218.692 s / exit 1 / no outer timeout | 218.549 s / exit 1 / no outer timeout |
| Direct five-package `go test -count=1` | 218.736 s / exit 1 / no outer timeout | 218.869 s / exit 1 / no outer timeout |
| Eight-package existing explicit runner, `-json -count=1` | 409.955 s / exit 1 / no outer timeout | 327.483 s / exit 1 / no outer timeout |

Every completed direct run names only `TestPreviewDrivesARealBrowser` as failing. The original resident reload
test is present and no longer fails; it retains the upstream home isolation. These are full failure outcomes,
not green suites. Direct runs now use #6286's six automatic node partitions. Their complete node inventories
are 2,558 roots on main and 2,571 after this PR's 13 added regressions. The diagnostic `-json` runs explicitly
use the pre-existing two-partition Python entrypoint, since `-json` disables automatic splitting.

In the direct eight-package comparison, node execution is 182.216 s on main and 122.165 s
after the fixture changes (33.0% lower). Overall direct wall time is essentially
unchanged because assistant, at about 216 s in both runs, is now the longer package. The automatic scheduler
belongs to upstream #6286; this PR's contribution remains the measured fixture cost reduction. These observations
do not reinterpret the historical 600 s direct timeouts from earlier sources.

The JSON audit is separate from raw direct output, which contains package results and named failures but not
a complete per-test event stream:

| Diagnostic terminal audit | Fixed main | Optimized code |
| --- | --- | --- |
| Root outcomes | 7,069 pass / 405 existing skip / 1 fail | 7,081 pass / 405 existing skip / 2 fail |
| Inventoried roots | 7,475 | 7,488 |
| Sum of root elapsed times | 1141.930 s | 944.280 s |

Of 17,445 common terminal identities, 17,444 retain identical states. There are
25 added identities in this PR's 13 regression roots,
0 removed identities and 1 common identity with a changed state.
Both inventories have zero missing/unexpected roots, duplicate runs/terminals or unfinished identities.

The one changed state is `TestAgentsSwitchFilterBusyAndMovedMeanwhile` in assistant: main passes, while this
head's diagnostic run fails at `agents_switch_test.go:154` with `moved meanwhile, switched anyway` and two
`session.modes, session.switch, session.modes` call sequences. Assistant passes in all four direct commands.
The assistant and related package source is identical to fixed main. This root was introduced by upstream
#6233 and is absent at `96c1deb30`, so it cannot explain that older external verifier failure. Its cause remains
unresolved; this single difference is not classified as a regression, known main failure, environment error or
flake. Further targeted main/head reproduction is required before attributing this difference to any change;
the full head result is not green.

Private nested manifests have published SHA-256 digests; dynamic subtest names and raw logs remain private.
No new skip or timeout increase is introduced relative to this fixed main.

Compilation is recorded separately: main 28.803 s, head
24.512 s, before matched execution. Supervisor cleanup is recorded separately in every
receipt, with zero remaining owned processes. The full exact argv, package results, partition summaries, source
SHAs, guard outcomes and diagnostic reconciliation are in the
[fixed-main partition receipts](2026-11-13-server-test-main-partitions-5730.json).

- All 13 fixture regression roots passed 20 repetitions: 25 identities and exactly 500 pass terminals.
- The affected selection, extended with the upstream partition/log/failure-summary tests and resident reload root,
  passed `-race -count=3` (143.614 s wall); the exact pattern is in the receipts.
- The full server build lane passed (88.695 s): formatting/vet, Linux/Windows build checks,
  release-tag programs, tagged tests, schema/SDK checks and Go SDK tests.
- Complete `herdrlive` node test compilation passed (12.114 s); no live agent started.
- Repository guards, four document checks and existing Python partition/environment suites passed. No sleep
  baseline, shell/workflow implementation, UI or native code differs from this fixed main because of this PR.
- No database URL was provided; existing database/platform/opt-in skips remain visible. Local Chrome still lacks
  a usable sandbox, as independently diagnosed above. That environment failure is not fixed or skipped here,
  and it does not identify the older external verifier failures on #5845. No hosted CI green result, upstream
  merge or deployment is claimed.
