# Recovery review follow-ups

This records reviews of `87d0ca342764ffbe2d7b95703e397c735213d58f` and the later
`e93b21c520f66194f14b2b1728d870703fc98d6c` follow-up. Original findings were read
and traced; the precise reachable reproductions and limits are recorded below.
This is author verification, not reviewer approval or thread resolution. Scope remains Web development and its Hub/Node backend
update chain, with three verified snapshots and program-only Apply backup. No native
client adaptation, deployment or production-data restoration is included.

## Upstream synchronization and full-delta review

The published branch at `dbd9809511e8a59e2e42f06a46ad83cd77166c64` was unchanged remotely.
Upstream `main` was pinned at `576970b459ee569997fbebab487cd4220dac67f8` and merged normally
as `74c86ecb00005bd49a1f122683b3861694bc31d3`. Git reported no textual conflicts. The
combined update paths and the complete feature delta were then reviewed, including
the upstream stopped-release-source warning and the Hub administration route contract.
No history was rewritten. This remains author review, not independent approval.

At the final pre-delivery read, upstream had advanced to
`39ef1e79639759925e52eae51f152c26666a44a7`; the original fork branch still matched
`dbd9809511e8a59e2e42f06a46ad83cd77166c64`. A second ordinary merge retained the
new upstream work. Its sole textual conflict was two independent additions at the
start of `docs/lessons/server.md`; both recovery and loopback-CLI lessons were kept.
The automatic `hub/config.go` merge preserves development settings and upstream
CLI endpoint/dial behavior. No feature budget/guard changes were introduced: the
upstream table-rendering change owns its existing 480,735 B startup limit. Fresh
build/browser and affected regression evidence after this merge is bound separately
in the PR; the first synchronized run and gallery remain historical evidence.

| Finding | Evidence and correction |
| --- | --- |
| Late install-script release refresh changes a newer operation | A deterministic publisher callback switches to development through the real settings route before returning the old release response. The response replaced the selected development candidate. A second case reserves an install before the response and loses its progress/detail. Refresh now loads persisted source settings, checks the channel again under the write lock and changes availability only while no install/confirmation is active. `TestInstallScriptRefreshPreservesNewerOperation` failed in both cases before repair; `TestInstallScriptRefreshLoadsSelectedChannel` covers stored development selection and unreadable settings before the first refresh. |
| Standalone watchdog loses recovery after a failed restart | Real program files and the write-ahead adoption journal show that the journal disappeared before the restart callback, including when restart returned an error. Cleanup now follows successful restart. Failure retains the exact journal and reusable preimages, keeping staged-package cleanup fenced. `TestDevelopmentWatchRetainsJournalUntilRestart` covers failure, successful cleanup and retry; the existing release watchdog cases remain unchanged. |
| Named built-in manager database omitted from backup | In a private PostgreSQL 16.15 cluster, an assistant config containing both `database_name` and a saved URL backed up only the Hub database. The live manager's `provisionOwn` selects the name first. Inventory now mirrors that selection, deriving the named database from the effective Hub connection. The new real-database test restores the second dump into a fresh database and verifies distinct synthetic manager history, while source rows remain unchanged. |
| New settings text exceeds the upstream font ratchet | The guard found 25 literal font sizes against the unchanged baseline of 22. The three added text sites now use `text-label`, `text-submeta` and `text-meta`. The first two retain their exact sizes; the backup explanation uses the shared 13 px text token. No baseline, assertion or budget was raised. |
| Web startup budget exhausted after synchronization | The normal production build measured Japanese first-screen bytes at 480,845 B against the unchanged 480,666 B limit. Hub settings presentation and its Docker/channel/warning strings shared the startup module. Those helpers now load with the settings page; startup keeps notice/state/result text, including the development states. The repaired build measures Japanese at 480,098 B, leaving 568 B; 80 focused Web tests pass. Existing localization/startup guards and Hub settings/notice regressions verify the split. No performance budget or guard was changed. |

The old 29-image gallery remains reachable in published commit history. Under the newly
merged collaboration rule, its images and capture manifest are removed from the final
tree; the PR retains their fixed-commit links. The typography correction and changed
upstream renderer require fresh browser evidence, bound separately in the PR rather
than relabeling the historical captures. New review captures follow the add-then-remove
`pr-assets` workflow. The PR records the final source commit, commands, capture checksums
and remote head after these follow-ups.

| Finding | Reproduction | Repair and regression |
| --- | --- | --- |
| [Omitted platform package](https://github.com/nwverse/persona/pull/6923#discussion_r4580152346) | The full saved-version installer/confirmation test lost the unstaged second platform's normal package path when fleet automation was off. | Confirmation now passes the development candidate identity. Backup apply swaps staged platforms only; omitted regular packages stay in place with their original version identity, preserving existing labels. Fresh-build and release set-aside behavior stays separate. `TestDevelopmentAppliesOlderProgramBackupThroughSharedInstaller` verifies confirmation, and `TestDevelopmentBackupKeepsUnstagedPackageIdentity` covers explicit labels and invalid-marker failure with retained staging. |
| [URI via PGDATABASE](https://github.com/nwverse/persona/pull/6923#discussion_r4580152350) | Actual pg_dump 16.15 / libpq 160015 treated the full URI as a database name. Both URI and keyword backups failed using the immutable original helper in a private test cluster. The finding is applicable on this tested toolchain. | Use libpq `PQconninfoParse`, preserve its URI/query/keyword parsing and map explicit options to their documented environment variables; reject unsafe/unavailable bindings before writer pause. Full DSNs and passwords stay out of argv. The opt-in real PostgreSQL test verifies escaped credentials, query options/application name, actual pg_dump argv, successful custom dump/restore into a fresh synthetic database and unchanged source rows. |
| [Recovery metadata](https://github.com/nwverse/persona/pull/6923#discussion_r4580512578) | Scalar/list metadata could raise TypeError; a personad entry mapped to a valid Node archive could pass verification and select the wrong program. | One shared schema check for listing/verify/prepare validates required fields, primitive/container types, timestamps, original absolute paths, inventory, program/platform allowlists, exact expected artifact names and lowercase hashes. Payload verification checks actual files and decompressed hashes; preparation independently checks staged native bytes after the compressed checksum. Malformed entries raise ValueError, remain retained and are excluded from listings/rotation. |
| [Archive members/links](https://github.com/nwverse/persona/pull/6923#discussion_r4580512581) | Backslash/drive/UNC forms, hard links, duplicate paths and symlink-parent graphs could pass the previous archive checks. | Shared canonical POSIX member/link validation rejects those forms, absolute/traversal/noncanonical names and dangling/chained links. Only direct relative links to archived files/directories are allowed. The operator-only `extract-history` command uses this same validator and an empty destination, plus Python's data filter. Apply backup never calls extraction. Malicious archives are rejected before destination creation; legal synthetic history and its relative link restore successfully. |
| [Failed boot recovery](https://github.com/nwverse/persona/pull/6923#discussion_r4581680631) | A later review arrived during final verification. With no pending marker, both a missing preimage and failed exec made boot return true; the failed exec also removed the adoption journal. | Boot now returns recovery errors to both standalone and edge-slot entry points before config/database startup. Rollback propagates restoration/exec failures and retains development journal/staging until the restored executable runs. `TestDevelopmentCrashRecoveryFailureBlocksAttemptedBuild` covers both failures, retained material through pruning and successful retry after repair. Earlier cutpoints and release boot tests remain covered. |

## Admission and Edge follow-up

The review of `e93b21c520f66194f14b2b1728d870703fc98d6c` added three P1 findings:

| Finding | Precise reproduction and disposition |
| --- | --- |
| [Concurrent admission](https://github.com/nwverse/persona/pull/6923#discussion_r4581908321) | Channels hold the first `now=true` request in service discovery while a later ordinary request reserves its own attempt. Releasing discovery made the first request set the second attempt's `now=true`. The ordinary request does not itself set that flag; the bug is transferred consent across accepted operations. Reject a request overtaken during discovery with `errUpdateBusy`, without changing `now`. `TestUpdateDelayedProbeCannotForceAnotherAttempt` proves this ordering; `TestUpdateNowConfirmsAcceptedWaitingAttempt` keeps explicit confirmation of an already accepted idle-waiting attempt working. |
| [Edge recovery](https://github.com/nwverse/persona/pull/6923#discussion_r4581908325) | Real missing Hub/companion preimages and a conflicting Web directory all returned a restart path after RestoreAll failed. Keep both journals and staging and return an explicit callback error. Distinguish failure from no matching update, so activation cannot fall through to the old binary path. Both slot/control activation and confirmation-timeout consumers stop the Edge and surface the error. `TestRestoreUpdateFailureRetainsRecovery` checks actual file failures, partial restoration, pruning and repaired retry; `TestUpdateRestoreFailureStopsEdge` runs real synthetic child slots through all three consumer paths. Existing release restoration and boot error propagation remain covered. |
| [Invalid source](https://github.com/nwverse/persona/pull/6923#discussion_r4581908329) | A nonempty invalid source different from the stored value was already rejected; arbitrary overwrite of the release choice was not reproduced as stated. The actual gap is an invalid value equal to stored state, including the default empty source: it returned 200 and changed other preferences in the same request, idle or active. Validate every provided source as exactly release/web-dev before comparison or mutation. `TestUpdateSourceRejectsEveryInvalidValueWithoutSideEffects` covers both states, default/valid/invalid stored values and rejects without changing memory, persistence or selected candidate. |

The two backup endpoints administer this Hub's local installation and recovery
transaction, under the same owner as its existing update/history/settings endpoints.
The previously disclosed route-inventory failure is addressed by registering those
two exact Hub administration routes with their ownership rationale. The architecture
contract explicitly requires this declaration for Hub-owned administration. No guard
logic, wildcard, count/budget or manager-business exemption changes.

The full App renderer remains Web source tree `bc453ef2d738f54b53af919951c4f3b95eceb656`.
The public gallery and the successful 29-capture browser run on `e93b21c52` are reused;
the new final head is bound in the PR and has the same renderer. Captures are not
relabeled as freshly generated. Normal Web/Linux builds and focused Go/race/Edge,
Python, docs and privacy checks are recorded against the final candidate in the PR.
The real-child Edge fixture needs a short Unix socket path: in long temporary-directory
environments, use an owned short symlink into that temporary directory, set the test
child's TMPDIR to it and remove only that alias after completion. All test data remains
in the owned temporary root; no existing services are used.

## Real PostgreSQL evidence

The acceptance test uses existing PostgreSQL binaries to initialize and stop its own
small cluster. It binds only an owned private Unix socket, disables TCP and pins safe
PGHOST/PGPORT defaults even during the original-helper reproduction. Synthetic source
and restoration databases are created in that cluster. No existing database is used.
Passwords are generated per run, used through private files/environment, and checked
against helper output, receipts and the actual pg_dump command line. The table-lock
fixture verifies the URI/keyword application_name through pg_stat_activity, then releases
only its own lock connection and restores the dump into a fresh synthetic database.

Primary references: PostgreSQL's [connection parser](https://www.postgresql.org/docs/16/libpq-connect.html#LIBPQ-PQCONNINFOPARSE)
and [environment parameters](https://www.postgresql.org/docs/16/libpq-envars.html).
The test establishes behavior on the installed 16.15/libpq160015 toolchain, not every
PostgreSQL version or native macOS execution. Passwords in a child environment remain
visible to the installation account/privileged processes; this is not a new isolation
boundary. libpq must be discoverable beside the PostgreSQL tools. Unsupported parameter
bindings fail explicitly rather than falling back to putting secrets in argv.

```sh
PERSONA_TEST_POSTGRES_BIN=/path/to/postgresql/bin python3 -B -m unittest -v scripts/test/test_web_development_postgres.py
# Real PostgreSQL 16.15 / libpq 160015: 4 tests OK (URI/keyword dump-restore, named manager inventory and invalid-parameter rejection before writer pause).
python3 -B -m unittest -v scripts/test/test_web_development_update.py scripts/test/test_web_development_recovery.py
# Existing cancellation/coordination/rotation/program-only fixtures plus malformed schema/archive tests.
cd server
go test ./internal/hub ./internal/update -run 'Test(Development|AFailedSwap|SwapProgram|HubOnlyUpdate|NodeUpdatePolicy)' -count=1 -timeout=3m
go test -race ./internal/hub ./internal/update -run 'Test(Development|AFailedSwap|SwapProgram|HubOnlyUpdate|NodeUpdatePolicy)' -count=1 -timeout=3m
```

The malformed-manifest test covers scalar roots, invalid container/field types, wrong
hash types, wrong artifact mapping, path escape and metadata list/verify/prepare
contracts. Archive cases cover absolute/traversal/backslash/drive/UNC paths, hard links,
symlink escapes/parents/chains, duplicates and canonicalization. A staged-file fault
between compressed checksum and native verification is rejected. Hard-linked history
fails snapshot creation without pruning the previous verified snapshot.

## Earlier follow-up evidence and boundaries

The PR follow-up binds the precise final head, fresh command results and remote state.
Normal Web development compilation and tagged Linux backend compilation remain the
build gate; targeted fault/real-database tests address these findings. The original
crash, cancellation and partial-replacement regressions are rerun and preserved.
For that earlier follow-up, Web source tree `bc453ef2d738f54b53af919951c4f3b95eceb656`
was unchanged, so the historical 29-image gallery and its checksums described that UI.
The [evidence record](../pr-6923-web-update/README.md) points to the PR retaining it.
The successful browser matrix on `e93b21c52` is reused for the unchanged renderer,
with the new final head/source binding recorded in the PR.

The earlier route-inventory failure at `e93b21c52` is preserved above as historical
evidence and is repaired by the scoped ownership declarations in this follow-up.
The final PR reports the new guard result separately from other focused checks.

Retained snapshots are local recovery material, not authenticated artifacts against
an account able to replace both data and hashes. Old/corrupt journals and snapshots,
conflicting overrides, failed writer resume and SIGKILL still require operator
inspection. Program rollback does not undo database migrations; the existing idle
check is not an admission lock. Native macOS host runtime, real writer hooks, production
restoration, power-loss/filesystem fault acceptance and maintainer visual approval
remain outside this isolated evidence. No hosted CI success is inferred from empty checks.
