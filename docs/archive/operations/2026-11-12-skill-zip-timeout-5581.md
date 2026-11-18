# Skill ZIP import: five-package timeout follow-up

Point-in-time verification for #5478 / PR #5581 on 2026-11-12. This is not a production,
hosted-CI or complete database-backed pass. The original five-package command still times out.
No test, assertion, timeout or production configuration was changed to obtain a pass.

## Source identity

- The maintainer's timed-out revision, `7ad56ac0930b693e1fb02a658f18e61bc417c6d5`, is a
  real previous PR head, not main or a synthetic merge revision. Its parent is `6762a1b16`;
  its main merge-base is `84b7a832029bd43e43a35ce8ca04d2427db6248d`.
- The reproduced PR code is `5763ad3a8eac2a439c4b2a1373e14df86e02f8af`. The reported
  revision is its ancestor: the intervening commits merge main and improve old-node error
  classification. The browser harness and archive/concurrency tests are unchanged between them.
- The exact clean comparison baseline is `1f96978b7f6ad2c2e3f3b5c9786f9c98fc8a352e`,
  the PR code's main merge-base. Both sources were frozen during their respective runs.
  Further moving-main changes were not integrated to chase test outcomes.
- The verified PR `server` tree is `6c9da18689764b35c6aab4e0295daa6da621cdf4`.
  The follow-up documentation commit does not change it.

The historical verifier provided a 600-second timeout, but no package events, dump,
cache or environment details. Its precise last test cannot be reconstructed. The reproductions
below establish the current behavior; they do not dismiss the historical result as an old-head error.

## Conditions and exact commands

Linux/amd64, Go 1.27.3, 32 logical CPUs. Runs were sequential, with the same existing Go
module/build caches and ambient environment. No `-race`, `-short`, test selection or extra
test parallelism was added to the five-package commands. `GOFLAGS`, `GOMAXPROCS`,
`SKILL_IMPORT_QA` and the test database variables were unset. Database-dependent cases
therefore retain their normal explicit skips, not database coverage. No existing Go suite
was active before launch; no production service was stopped. Sampled load was well below
the CPU count. Raw logs and paths remain local; only synthetic or path-free summaries are public.

Set `PR_SERVER` and `BASE_SERVER` to the server directories of independent, clean checkouts
of the immutable revisions above. These names are placeholders, not installation targets.

Original verification, run once on each source:

```sh
timeout --signal=TERM --kill-after=10s 600s \
  go -C "$PR_SERVER" test -count=1 \
  ./internal/assistant ./internal/assistantskills ./internal/assistantwire \
  ./internal/hub ./internal/node
```

The baseline command differs only in replacing `PR_SERVER` with `BASE_SERVER`.
`/usr/bin/time -p` measured the outer invocation.

| Original command | PR | Clean main |
| --- | --- | --- |
| Outer exit / wall time | 124 / 600.01 s | 124 / 600.00 s |
| assistant | PASS, 211.195 s | PASS, 211.323 s |
| assistantskills | PASS, 0.155 s | PASS, 0.027 s |
| assistantwire | PASS, 0.004 s | PASS, 0.004 s |
| hub | PASS, 135.203 s | PASS, 134.591 s |
| node | Not completed at outer deadline | Not completed at outer deadline |

Diagnostic parameters were a separate run, not a replacement pass:

```sh
timeout --signal=TERM --kill-after=10s 600s \
  go -C "$PR_SERVER" test -count=1 -json -v -timeout=570s \
  ./internal/assistant ./internal/assistantskills ./internal/assistantwire \
  ./internal/hub ./internal/node
```

The local observer applied the same 600-second outer process-group deadline and retained JSON
events. The 570-second Go deadline deliberately obtains its dump before the outer termination;
it does not increase the verification budget. The baseline uses identical diagnostic flags.

## Current diagnostic evidence

The PR's diagnostic starts test execution in about 2.4 seconds with warm caches; assistant,
assistantskills, assistantwire and hub pass in 211.477, 0.149, 0.004 and 132.723 seconds.
Node exits 1 at its 570-second diagnostic deadline (572.539 seconds total), after 1,412
top-level passes and 28 normal skips. No completed node test fails before the deadline.

Node continues through assistant launch, Codex start, engine-switch, herdr and launcher-switch
tests. Its completed top-level durations sum to 564.8 seconds; the current test has run for
about four seconds. This accounts for almost the entire test interval rather than a
post-test cleanup or compilation stall. The longest completed top-level node test is
`TestAssistantBypassShellNames` at 24.91 seconds.

At the cutoff, `TestLauncherSwitchThatNoLongerApplies` is in the existing
`waitGrace → ensureSubmitted → afterPrompt → send` path. `waitGrace` waits on the existing
two-second `submitGrace` or context cancellation. This test and path are unchanged relative
to the comparison baseline; the dump does not show an import mutex or archive barrier in
the current test's stack. A test interrupted by the package-wide deadline is not, by itself,
evidence that the test deadlocked.

| Diagnostic command | PR | Clean main |
| --- | --- | --- |
| Whole-command exit / wall time | 1 / 572.539 s | 1 / 572.543 s |
| Test startup with warm caches | about 2.4 s | about 2.5 s |
| assistant | PASS, 211.477 s | PASS, 212.854 s |
| assistantskills | PASS, 0.149 s | PASS, 0.026 s |
| assistantwire | PASS, 0.004 s | PASS, 0.004 s |
| hub | PASS, 132.723 s | PASS, 134.192 s |
| node | FAIL at 570.039 s | FAIL at 570.040 s |
| Completed top-level node tests | 1,412 passed / 28 skipped | 1,415 passed / 28 skipped |
| Current test at deadline | `TestLauncherSwitchThatNoLongerApplies`, about 4 s | `TestLauncherSwitchFailureRollsBack`, about 2 s |

Clean main's completed top-level node durations sum to 566.93 seconds. All 1,412 completed
PR node tests also complete on main; their summed PR-minus-main duration is 1.87 seconds,
with a maximum absolute single-test difference of 0.66 seconds. The longest node test is
the same on main (24.93 seconds). Its cutoff stack is in existing launcher probing
(`Cmd.Output → launcherProbe → launcherModelCheck → start → carryOut`), not import or
browser-fixture cleanup. Both runs are interrupted while still progressing through the
serial corpus, rather than in the same stuck individual test.

## Compilation versus execution

To measure cold compilation separately without clearing or changing a shared cache, each
source received its own new, empty `GOCACHE` and output directory. The existing module cache
was retained. The compile-only command for each source was:

```sh
COLD_ROOT=$(mktemp -d)
mkdir "$COLD_ROOT/cache" "$COLD_ROOT/bin"
/usr/bin/time -p timeout --signal=TERM --kill-after=10s 180s \
  env GOCACHE="$COLD_ROOT/cache" \
  go -C "$PR_SERVER" test -c -o "$COLD_ROOT/bin/" \
  ./internal/assistant ./internal/assistantskills ./internal/assistantwire \
  ./internal/hub ./internal/node
```

The baseline substitutes `BASE_SERVER`, with a separate empty cache and output directory.
Both commands exit 0: PR wall/user/system times are 46.23/438.05/68.93 seconds, and clean
main times are 45.80/438.41/69.08 seconds. All five binaries are built; no tests run in
this measurement. This is cold **build-cache** compilation, not a cold dependency download.
The original runs reached test binaries in about 28 seconds, while the subsequent warm-cache
diagnostics started them in about 2.5 seconds. The diagnostic node test process itself
consumes 570 seconds on both sources, independently of compilation.

## Browser harness and import lifecycle

The full diagnostic explicitly reports:

```text
--- SKIP: TestSkillImportBrowserHarness (0.00s)
```

The environment check is before temporary storage, service setup, listener creation or a
goroutine. Neither the assistant's `TestMain`, `testhome.Main` nor `dbtest.Main` enables it.
Only an explicitly supplied `SKILL_IMPORT_QA=1` enters the interactive fixture. The historical
verifier's environment is unknown; there is no evidence that it supplied this variable.
The earlier 1106.287-second manual browser fixture is not this default suite path.
On explicit use, the fixture closes its HTTP server when its stop signal is received or
its existing deadline fails the test. It is not a live hub authentication or paid-model test.

Both `TestImportAtomicPublicationAcrossImporters` and
`TestImportConflictsReferencesAndConcurrentPublication` pass in the full skill package.
All 28 top-level skill-package tests pass in 0.149 seconds; its added cases do not account
for the five-package deadline. No safety or cancellation coverage was removed.

## Additional complete node partitions

The additional node run uses the repository's unchanged `go_partitions.list_patterns`,
not a hand-selected import subset: 2,466 listed top-level test/example/fuzz entries are
split into two disjoint lists of 1,233 each. Subtests follow their parent. Each process
retains `-p 1 -count=1 -timeout=10m` and a 600-second outer deadline. Diagnostic `-json`
events are retained locally. This command structure reproduces the additional coverage
from the PR checkout, using the same existing environment and caches:

```sh
python3 - <<'PY'
from pathlib import Path
import subprocess
import sys

root = Path.cwd()
sys.path.insert(0, str(root / "scripts/test"))
import go_partitions

patterns = go_partitions.list_patterns(root / "server")
runs = [
    subprocess.Popen([
        "timeout", "--signal=TERM", "--kill-after=10s", "600s",
        "go", "-C", str(root / "server"), "test", "-json", "-p", "1",
        "-count=1", "-timeout=10m", "-run", pattern, go_partitions.PACKAGE,
    ])
    for pattern in patterns
]
codes = [run.wait() for run in runs]
print("Complete node partitions:", codes)
sys.exit(next((code for code in codes if code), 0))
PY
```

The actual diagnostic runner additionally writes one event log per process and records
monotonic startup/completion times, applying its outer deadline to the whole process group.
No database is supplied, so the normal fixture-dependent skips remain a coverage boundary.

| Additional partition | Package time / exit | Top-level passed / skipped / failed |
| --- | --- | --- |
| 0 | 356.793 s / 1 | 1,213 / 19 / 1 |
| 1 | 439.576 s / 1 | 1,216 / 16 / 1 |

Combined wall time is 441.567 seconds. The terminal event sets contain exactly 2,466
distinct entries, with zero overlap; neither partition times out. This run is **not green**:

- `TestPreviewDrivesARealBrowser` fails at `preview_live_test.go:149`, during browser startup:
  `Google Chrome did not answer: the browser closed`. The available browser identifies as
  Chrome for Testing 149.0.7827.55. The crash's lower-level cause is not established.
- `TestResidentReloadReapsIdleProcessesBeforeTransportEnds` fails at
  `resident_reload_unix_test.go:88`, before acquiring the resident transport's existing run
  lock. This existing test does not establish its own temporary home in this invocation.
  The live resident lock was neither released nor retried, and no live node was stopped.

`TestLauncherSwitchThatNoLongerApplies` and `TestLauncherSwitchFailureRollsBack`, interrupted
by the earlier serial diagnostic deadlines, now pass in 4.01 and 6.84 seconds.
`TestSkillImportNodeRoutingAndDiscovery` passes in 0.00 seconds within the complete run.

### Bounded comparison of additional failures

After the complete run ended, the two failures were diagnosed sequentially on both frozen
sources, without another full suite in parallel. Each child uses the repository's unchanged
`server_test_env.clean_environment`, the same pinned Go cache locations and a new temporary
home. No live database, runtime profile or proxy routing is inherited. The node test binary
finds the same browser through the existing tool PATH.

The diagnostic test command is distinct from both original five-package commands:

```sh
timeout --signal=TERM --kill-after=10s 120s \
  go -C "$SERVER" test -json -count=1 -timeout=90s \
  -run "^${TEST_NAME}$" ./internal/node
```

`TEST_NAME` is one of the two exact names above; no test source or assertion is changed.
For the locked-resident condition, a local driver creates only the temporary home's
`.persona/node-attach.sock.run` and holds `fcntl.flock(LOCK_EX | LOCK_NB)` across the child.
The unlocked condition uses a separate empty temporary home, not the real resident socket.

| Diagnostic case | PR | Clean main |
| --- | --- | --- |
| Real-browser test, isolated home | FAIL, same browser-closed error, 0.07 s | FAIL, same error, 0.08 s |
| Resident test, synthetic held lock | FAIL, existing lock guard, 0.00 s | FAIL, same guard, 0.00 s |
| Resident test, separate empty home | PASS, 0.20 s | PASS, 0.20 s |

All six whole commands complete in about 1.8–2.0 seconds including startup. Browser and
resident test/implementation files have no PR diff relative to the comparison baseline.
These are additional baseline/environment boundaries, not import regressions or passes for
the original check. They do not justify removing tests, altering browser sandbox settings,
stopping a resident node or claiming complete database-backed success.

## Interpretation and remaining verification

Both original commands time out without database inputs. This is separate from the
previously reported race-instrumented failures and Docker fixture permission failure.
The repository already documents the complete serial node suite outgrowing ten minutes
and uses unchanged, disjoint full-coverage partitions in `scripts/test/go_partitions.py`
for CI and release verification. That partitioner is identical at both relevant main bases
and this PR. See [Testing](../../operations/testing.md), Hosted CI.

No PR-introduced ordinary-test hang has been demonstrated, so this follow-up does not
alter unrelated waits, reduce assertions or invent a production fix. It retains the
original timeout as a failing verification result. Complete partitioned coverage, when
reported, is additional evidence and never relabels that command as passing.

On an authorized Docker-enabled runner, the unchanged repository entry point is:

```sh
bash scripts/ci/server.sh
```

It provisions disposable Postgres 18 and runs all existing server shards, including the
complete node partitions. Do not point tests at a live database, expand Docker permissions,
increase the watcher timeout or run only import-specific tests to claim this check passed.
Adapting an external verifier to the repository's already-approved complete partitioning
is a maintainer decision, not a change made by this PR.

The official node lane was also retried separately on each immutable source:
`bash scripts/ci/server.sh node` exits 1 on both, at `Postgres 18 fixture (0.0s, exit 1)`
with Docker socket permission denied, before tests start. An additional 30-second local
outer cap was not reached. No container, database, permission, credential or production
configuration was changed.

Documentation verification for this follow-up:

| Command | Result |
| --- | --- |
| `python3 scripts/privacy_check.py` | Exit 0, clean |
| `python3 scripts/test/test_doc_paths.py` | Exit 0, 12 tests |
| `python3 scripts/test/test_doc_go_paths.py` | Exit 0, 3 tests |
| `python3 scripts/test/test_doc_sections.py` | Exit 0, 4 tests |
| `python3 scripts/test/test_doc_wire.py` | Exit 0, 13 tests |
| `git diff --check` and `git diff --cached --check` | Exit 0 |

The follow-up changes only this record and its archive index. The earlier Web, typecheck,
build, localization and inspected light/dark/narrow/focus evidence remains in the same PR;
those source trees were not changed or rerun for this timeout diagnosis.

The existing race failures remain separately documented in the PR. GitHub reports no hosted
checks, not a hosted-CI pass. No merge, deployment, paid runtime or production validation
was performed.
