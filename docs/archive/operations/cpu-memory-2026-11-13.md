# CPU and memory optimization, 2026-11-13

The delivered changes optimize four Go paths: stream queue retention, machine Views capacity, extension RPC encoding and empty mute checks. They preserve functionality, wire data, update timing and the existing UI. No client source or tests are changed in this PR. It was developed from `6df2ae628` in an isolated worktree and integrated with `main` at `a6c73fd7e`. Initial audits and independent round reviews used Astra; implementation used separate developers. All measurements below use disposable local fixtures, not a production installation.

## Round 1

The two Web rows below are excluded experiments, not delivered improvements. They initially passed behavior checks but did not satisfy the current startup-size gate after upstream integration. Their code, tests and measurements are preserved locally; all Web changes were removed from the final PR.

| Path | Change | Measured result |
| --- | --- | --- |
| Stream writer | Clear a consumed queue slot after saving the chunk locally. Delivery, acknowledgments and backpressure stay unchanged. | After 128 open streams each deliver a 512 KiB backlog, median retained heap after collection falls from 71,707,776 to 4,593,736 bytes: 64.005 MiB less. |
| Machine views | Reserve the exact capacity from the same machine list being iterated. | Views: 32,544 to 9,792 B/op, 27 to 22 allocations. The existing extension event benchmark: 35,607 to 12,815 B/op, 55 to 50 allocations. |
| Go extension RPC | Append freshly encoded payloads to their frame without encoding them again. Keep standard encoding for errors, escaped metadata and possible depth-limit cases. | Go 1.24: one fewer allocation per measured call/notification/reply; Go 1.27: two fewer. See the timing limits below. |
| Web session lists (excluded) | Count additions/removals against the frozen map without constructing another map. | A synthetic public incremental load of an 8,000-row list falls from 9.133 to 7.281 ms. Map constructions per load fall from five to four. |
| Web message merging (excluded) | Fill the ID map directly; bypass fold processing only when no message has fold metadata. | An 8,000-item history plus 50 updates falls from 3.147 to 2.798 ms without folds, and 4.208 to 3.654 ms with folds. |

The stream measurement verifies delivered bytes and acknowledgments before measuring an idle connection. It is retained heap for that backlog scenario, not a claim about every server's RSS. The machine-view fixture has 20 computers with 200 sessions each; its event takes the real lookup path but targets an unopened stream. Allocation changes are more reliable than its wall-clock timings under concurrent host load.

SDK tests compare exact frames, newline, errors, escaping, invalid UTF-8, custom marshalers and nesting limits against the previous encoder on Go 1.24.4 and 1.27.1. Seven-run Go 1.24 timing medians improve by 30.8–62.9% across six synthetic paths. Go 1.27's large-view notification has a sequential sample with no improvement (+0.4%); seven alternating before/after rounds measure 769,787 to 618,111 ns/op (-19.7%). Both samples are retained. These timings do not establish a process-wide CPU reduction.

The Web benchmarks transpile the actual before/after source and use in-memory API responses. The public-load comparison verifies the complete requests, published snapshots and final state. Smaller message histories do not establish a timing improvement: one 500-item sample changes from 0.185 to 0.197 ms. No universal browser-latency or memory-byte claim follows from those microbenchmarks.

## Round 2 experiment, excluded from this PR

The independent first-round review approved the source changes and recommended one bounded experiment. Final duplicate checking found the same live-loop reuse work in the still-open [PR 4993](https://github.com/nwverse/persona/pull/4993), tracked by [issue 3197](https://github.com/nwverse/persona/issues/3197). To respect that existing work, this PR excludes the experiment's implementation and tests. The local worktree, patch and measurements are preserved. The numbers in this section are experimental evidence, not benefits delivered by this PR. A three-minute baseline of the shipping server, with 10 computers, 50 sessions per computer and 20 connected clients, allocated a median 425.9 MiB/min in its manager. The allocation profile attributed 67.88% to copying session snapshots for the live fleet calculation.

Copies cannot simply be removed: device updates mutate their authoritative session records, while fleet decoration changes its own copies. The excluded experiment kept the existing lock-protected copy and full decoration, archive and folding behavior. Only the live loop reuses its own destination arrays, after both consumers finish. Retained logical Session-array capacity is limited to 1 MiB in total; allocator rounding and small per-machine metadata are additional. Payload references are cleared, and removed or substantially shrunken fleets release their buffers. Independent snapshot callers retain their previous ownership contract and allocation budget.

The independent second-round reviewer recommended retaining this change. Two experiment binaries built from the server sources ran the same workload for five minutes each after a one-minute warmup. Both ended with 20 connected clients and 30,600 received session lists. A market refresh delayed one window by 37 seconds, so the stronger comparison uses their common four-minute interval. Existing cumulative samples were selected at matching endpoints without interpolation; manager endpoints differ by only 47 milliseconds. RSS has 120 samples per process in that interval.

| Manager metric, common four minutes | Before round 2 | After round 2 | Change |
| --- | ---: | ---: | ---: |
| Allocated MiB/min | 426.768 | 179.821 | -57.86% |
| Process CPU ms/min | 2,456.838 | 2,038.414 | -17.03% |
| Runtime GC CPU ms/min | 867.827 | 363.305 | -58.14% |
| Live heap, sample mean MiB | 15.980 | 17.486 | +1.506 MiB |
| RSS, median MiB | 100.074 | 104.164 | +4.090 MiB |

The unchanged hub provides a useful control: CPU is 1,531.382 versus 1,531.746 ms/min, and median RSS is 74.547 MiB in both variants. Combined hub/manager CPU falls about 10.5%. These are measurements of this synthetic workload on a shared host, not universal production savings or statistically independent trials. The raw tool labels memory as MB but divides by 2^20; this report uses MiB.

The resident-memory increase is an explicit tradeoff, not a memory reduction. A separate real-Devices heap probe measures roughly 0.8 MiB additional live heap for the reusable 500-row arrays. Shrinking to 20 rows first discards the large buffers, then retains 30,560 logical bytes; an empty fleet retains zero. The reviewer accepted this bounded cost for the repeatable allocation and CPU savings. Original snapshot allocation limits were preserved: an intermediate implementation exceeded the existing count by one, was corrected, and the unchanged gate passed again.

Evidence: `fleet-round2-final-{before,after}/report.json`, `fleet-round2-final-sources.json`, `fleet-round2-final-rss.jsonl` and the independently calculated `astra-round2-overlap.json`, all under the artifact directory below. Earlier exploratory measurements predate the allocation correction and are not final-source measurements.

## Round 3

The second-round allocation profile identified a smaller independent target: constructing a mute lookup key accounted for 40 MiB, or 4.52% of its sampled manager allocations. When neither sessions nor spaces are muted, that key cannot affect the result. The final change returns false after the same settings read and lock, preserving the entire nonempty-rule path. It retains no additional state and does not depend on round 2.

Seven alternating runs of the actual method, with 64-byte machine and session IDs, reproduce 224 B and two allocations per call becoming zero for empty preferences; Allowed-only preferences change from 144 B and one allocation to zero. Their timing medians are 318.00 to 32.23 ns/op and 167.20 to 35.85 ns/op. Short identifiers remain allocation-free. Nonempty session/space rules keep the same allocation counts. Initial sequential timing controls showed slowdowns; alternating reruns did not reproduce a statistically significant difference (p=0.318–0.805), but high host noise prevents a claim of strict timing equivalence.

The regression matrix checks the original evaluator and explicit expected results across eleven settings cases and five identifier forms, including Unicode, slashes and empty values. Concurrent settings replacement, existing session reads, mute, snapshot, attention and notification tests pass, including race detection. These method-level measurements are not an additional process CPU or RSS claim. An independent 500-row real-Devices consumer benchmark with short IDs found no established end-to-end improvement; short-ID lookups already allocated zero. The fresh independent Astra review approved this change and recommended stopping: no further safe, high-value independent candidate was established by the evidence.

## Verification before upstream integration

- Full Web tests: 11,092 passed, one explicitly opt-in live-hub test skipped. Lint and production builds passed. The unchanged baseline had 11,086 passing tests and the same skip.
- Full Go SDK tests and extension race tests passed on both Go toolchains; short fuzz runs completed on both.
- Existing stream byte delivery, close-tail and backpressure tests passed, including race detection. The new retention regression fails on the original code at both partial and complete queue drain and passes with the fix.
- All three server test groups ran against an isolated Postgres fixture. An architecture test initially needed an uncached dependency; the complete package passed after downloading it. An extension identity test failed once in the full run; isolated baseline/head runs each passed five times, and full baseline/head extension-package reruns both passed. Initial failure logs are retained; the intermittent failure's root cause is not established. After round 2, the complete Assistant package passed in 400.981 seconds and Devices passed in 88.931 seconds. Independent race and three repetitions of the unchanged snapshot allocation gate passed.
- Linux/Windows Go vet and builds passed. The first-round reviewer inspected six before/after light/dark browser screenshot pairs and found no meaningful content or layout regression. Animated indicators and some color intensity vary between captures; this is not a blanket pixel-identity claim.
- The final production-browser fixture passed 8,000 rows, 159 continuation pages, filters, exact lookup, incremental updates and older-server fallback. Its old select/input operations required adaptation to current radio/menu controls. The legacy exact-title visibility assertion was scoped to its known main-session link, preserving its original 30-second timeout and all functional assertions.
- Independent legacy probes found exactly the same ordered 8,000 links and rendered text in baseline and changed builds, with matching SHA-256 `519898a1a8c44e810c381de1e16bef2d165ff067ca216130ac76f98aefa64d7f`. Their screenshots differ only in animated indicators. Both versions can still exceed the legacy fixture's cold-start deadline: that old-server page builds 308,901 DOM elements. The final passing run is not a fix or a universal latency guarantee for this existing limitation.

Raw logs, benchmark programs, source hashes, profiles, screenshots and independent review reports are retained locally under `/tmp/persona-perf-20261113/`. Their filenames identify the workload and round; early failed or superseded runs are preserved rather than represented as passes. No deployment or production acceptance is claimed. Apple and Android code is unchanged; native-device performance was not measured by this Linux run.

## Verification after upstream integration

The final outgoing scope contains only the three Go changes from round 1 and the independent round 3 mute guard. Round 2 was removed because it duplicates open work. All Web source and tests were subsequently restored to the integrated main revision; the Web experiments below are not part of the delivered diff.

- Independent current-source race checks passed for Devices, Assistant and Streams; the unchanged snapshot allocation limit passed three times without race instrumentation. During the excluded Web experiments, targeted checks passed 136 tests, including session-list, merge-oracle and all existing turns tests. SDK full tests and extension race checks passed.
- Full Web run: 11,188 passed, 13 failed, one opt-in live test skipped. Unmodified `a6c73fd7e`, using the same dependencies, had 11,179 passed, 16 failed and the same skip. Eleven of the thirteen head failures also fail on that baseline. Sidebar menu failures reproduce consistently. The remaining display/command cases passed paired isolated checks; their full-run failures are not attributed to this change, and their underlying cause remains unverified. Neither full run is represented as passing.
- The existing Web font guard fails identically on baseline and head: `TerminalOptions.tsx` has 19 hard-coded sizes against a baseline of 17. This file is unchanged. Privacy, migration/sleep guards, path-case, external-reference and 32 documentation checks pass.
- A full Assistant run started before the upstream update and later read changed golden files, failing `TestGoldenBackgroundSupervisor`. That mixed-revision run is retained but superseded by the stable post-integration run; no golden was edited to pass.

The stable current-source full packages passed: Devices 88.746 s, Streams 2.055 s, Assistant 434.085 s and Extmgr 86.798 s. Linux vet/build and Windows build passed. Web lint passed. The first head build exceeded the existing Japanese first-screen budget by 56 bytes (474,738 versus 474,682). Pristine main also failed at 474,717 bytes, 35 above the same limit. Two intermediate follow-ups still exceeded the limit at 474,744 and 474,728 bytes. A third candidate removed the added no-fold shortcut, kept direct Map insertion and one final sort, and simplified overlay counting. It still measured 474,733 bytes: 16 above pristine main and 51 above the limit. All three candidates passed 136 differential tests and independent source review, but none met the no-growth build gate. All Web changes were therefore excluded. No budget or test assertion has changed.

## Final scope and stopping decision

The four delivered Go paths retain their tested source throughout the Web experiments. Independent Astra reviews approved their equivalence, byte/depth behavior, concurrency and resource measurements. The full current-source Go package, SDK, race and build receipts above apply to that unchanged final code. There is no delivered Web optimization, no live-loop reuse, and no claim of their timings or process CPU results as benefits of this PR.

Stop further optimization in this task. The significant live-loop target already has an open PR. Further client work needs an implementation that meets the unchanged startup budget; adding state, altering polling or changing rendering to force a larger gain is not justified by the present evidence. The preserved experiments remain available for future work. The final independent review verifies the reduced outgoing diff before publication.
