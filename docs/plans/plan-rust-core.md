# Plan: one Rust core for the iOS, Android and HarmonyOS clients (#6793)

Move the client logic that has no UI (sync and continuity, list state, counts, protocol decoding, and later storage)
out of Swift, Kotlin and ArkTS into one Rust library, and keep each client's UI native: SwiftUI, Jetpack Compose and
ArkUI. The maintainer made the open decisions on 2026-11-13 and added the iOS-reference decision later that day
("Decisions" at the end). The first code is a vertical slice whose measurements decide whether the migration goes on;
its workspace and Android host replay have landed. The three client bindings now have host evidence of different
depths, but only HarmonyOS's Agents-list sort uses the core in production; the other domains and platforms remain at
their binding, adapter or DEBUG-shadow stages.

**Who it serves**: everyone who uses the phone apps, indirectly. Nothing the user sees changes. They get the same
counts and the same chat on every phone, fixes that reach every client at once, and less per-chunk work while a
reply streams. Contributors carry the cost: a Rust toolchain in every mobile build.

## Why now, and what it replaces

Decision D1 of the client redesign (`docs/archive/redesign/README.md`) chose native clients that share **specifications
and fixtures**, not code. It deferred a shared core (Kotlin Multiplatform at the time) for these reasons:

1. It would replace iOS's tested Swift data layer.
2. Swift export from Kotlin was alpha, and the types it exported were not `Sendable` under Swift 6.
3. The watch and Live Activity extensions would have to link the shared framework within tight memory limits.

Since then, the picture has changed:

- **A third native client.** The HarmonyOS client (`harmony/`, #5147) is ArkTS. Kotlin Multiplatform cannot target it;
  Rust can, and the client already ships a Rust library: the SSH core in `harmony/native/ssh`.
- **Drift is measurable.** `spec/fixtures/` holds 23 domains, and most are replayed by only some of iOS, Android,
  HarmonyOS, the web app and Go. For example, `continuity` is replayed by Android,
  HarmonyOS and Go but not iOS, and `needs-you` by iOS and Android but not HarmonyOS. Each fix still lands up to three
  times.
- **Duplicated performance work.** Streaming chunk assembly is optimised per client: #4044 on Android, and `StreamChunks`,
  `StreamRepair` and `StreamReveal` in iOS's `PersonaKit`.

How the reasons for D1 change with Rust:

| D1 reason | With Rust |
| --- | --- |
| Replacing tested Swift code | Still true. The migration goes one domain at a time, each behind its fixtures, with a shadow run before the old code is deleted ("Migration rules"). |
| Swift interop maturity | Swift imports the core's C entry points directly, and the generated codecs are plain value types; the slice must prove they compile under Swift 6.2 strict concurrency with Xcode 26.0 ("Apple"). |
| Extensions' memory | Still true. The watch, widgets, Live Activity and notification extensions do not link the core in this plan ("Apple"). |

If this plan is adopted, D1 is superseded for the domains that move. Domains that have not moved keep D1's rule:
fixtures first, then each implementation.

## Scope

**In scope**: the iOS app (and iPadOS), the Android app, the HarmonyOS app.

**Out of scope**:

- **The web app** keeps its own implementations and its fixture replays. A WebAssembly build of the same core is
  possible later and needs its own issue.
- **The Go hub** keeps its implementations (`attention`, `triage`, `fold-changes`, the continuity corpus) and keeps
  replaying the fixtures.
- **The retired native Mac app** is removed with its target; it does not acquire a new core binding. The Electron desktop keeps its existing web implementation and is not a new Rust binding target (#6770).
- **Persona Link and the watch.**

### The iOS reference

**Behavior, state machines and data flow follow iOS.** The shared core does not establish a new client behavior, and
another client's existing implementation is not the reference. `continuity` was initially ported from Android, so its
Rust implementation and the shared fixtures must be checked against the current iOS `ChatStore` before the iOS binding
can be accepted. Where they differ, add a fixture from the observed iOS behavior and change the core to match; do not
change iOS to accommodate the port. If the audit finds an actual iOS bug, file that separately rather than silently
changing the contract here.

**Android and HarmonyOS match iOS's interaction and visual result.** The existing native-style and theme choices are
not deleted: within the corresponding iOS style, they follow its hierarchy, states and motion while staying native to
each OS. Whole-screen alignment and paired screenshots use the existing #4194 work. Phase 1 adds bindings and replay
only; it does not change any existing screen.

**Performance targets come from measured iOS.** Record the current iOS first-frame, streaming, scrolling and memory
numbers on the same named device, build and workload used for the binding comparison. The fixed dispatch/drain ceilings
below remain additional limits, not substitutes for the iOS baseline. Android and HarmonyOS are compared with that
baseline using equivalent workloads; simulator and host timings are labelled and never presented as physical-device
results. Until an iOS measurement exists, the corresponding comparative cell stays "not measured", never "passed".

## Architecture

### Layout

```text
native/app-core/                 Cargo workspace, its own Cargo.lock and rust-toolchain.toml
  crates/persona-core/           pure logic: domains, stores, view states, patches. No IO, no clock, no FFI, no unsafe
  crates/persona-core-runtime/   one worker thread: the action queue, timers and file storage. No tokio
  crates/persona-core-ffi/       the C entry points (Swift) and JNI entry points (Android)
  crates/persona-core-ohos/      the same entry points as a NAPI module (napi-ohos)
  crates/persona-core-typegen/   generates the Swift, Kotlin and ArkTS codecs from the core's types
  tests/                         replays spec/fixtures/<domain> through persona-core
  scripts/                       build-apple.sh, build-android.sh, build-ohos.sh
```

Each client gets one adapter module, the only code that calls the entry points:

- **Apple**: a `PersonaCore` Swift package beside `PersonaKit` and `PersonaSSH` under apple/Packages. It holds an
  XCFramework binary target with a module map for the C entry points, the generated Swift codecs, and thin adapters.
- **Android**: a `:core:rustcore` module with the generated Kotlin codecs and the `external fun` declarations. The `.so`
  libraries go to the app; a host build of the library serves the JVM unit tests.
- **HarmonyOS**: a folder under the entry module's `ets/core` with the generated codecs, the `.d.ts` of the NAPI
  module and adapters, packaged the way the SSH core is.

### The boundary: a few entry points and bytes

The whole boundary is a handful of entry points: create a core, `dispatch` an action, `drain` a view's patches, take
the result of a host effect, and register the `changed` wake-up. Actions, effects and patches cross as bytes in one
compact binary encoding. Its codecs for Swift, Kotlin and ArkTS are generated from the Rust types by
persona-core-typegen. A serde-reflection type registry is the starting point, the same approach the Crux framework uses
for this shape of core. If serde-generate's output does not pass ArkTS's checks or Swift 6's, the generator is our own,
built from that registry.

Why bytes and not a generated typed API such as UniFFI:

- **The encoding is not avoidable.** UniFFI serializes compound records into a buffer on every call too.
- **The Android cost is avoidable.** UniFFI's Kotlin bindings call through JNA, whose calls and callbacks cost much
  more than a direct JNI call, and JNA adds its own native library per ABI.
- **A small, stable surface.** The glue around five entry points is small enough to write and review by hand. New
  domains add types to the codecs, not functions to the boundary.

How each client calls the entry points:

- **Swift** calls the C functions directly.
- **Kotlin** calls JNI functions that Rust implements with the `jni` crate, with no C layer.
- **ArkTS** calls a NAPI module that napi-ohos builds from the same crate. It is one `.so`, without the second C++ library
  the SSH core uses.

Generated codecs are committed, with a check that regenerating them changes nothing. This follows the repository's
precedent for generated code: `apple/Packages/PersonaWire/Sources/PersonaWire/ExtensionWire.swift`. Compiled libraries are never committed (the
SSH core's rule). One pinned toolchain serves the whole workspace, starting from the SSH core's 1.98.0.

### A deterministic core: actions in, effects and patches out

The pure crate never performs IO and never reads a clock. Everything it needs comes in as an `Action`. Everything it
wants done goes out as an `Effect`:

- **Effects Rust can run itself** (timers, file storage) are run by persona-core-runtime on its worker thread, with no
  trip across the boundary.
- **Effects only the OS can run** (transport, secure storage) go to the client's `PlatformHost`.

The sketch below shows the Rust side of the boundary.

```rust
pub struct Core { /* state per hub scope */ }

impl Core {
    pub fn new(config: CoreConfig, host: Arc<dyn PlatformHost>) -> Arc<Self>;
    /// Queues one action for the worker thread; safe and cheap on the main thread.
    pub fn dispatch(&self, action: Action);
    /// Patches for one view since `version`, already built by the worker, pulled by the UI on its next frame.
    pub fn drain(&self, view: ViewId, since: u64) -> PatchBatch;
}

pub trait PlatformHost: Send + Sync {
    /// A cheap wake-up with no payload: something changed at `version`.
    fn changed(&self, version: u64);
    /// Effects only the OS can run: network requests, secure storage. Results come back as actions.
    fn run(&self, effect: Effect);
}
```

Why it is shaped this way:

- **Fixtures replay with no fakes.** A fixture's events are actions, and its expected requests are effects. The
  continuity fixtures issue `FetchAfter` immediately and use an explicit `continue_fill` step for a duplicate
  gap signal while a fetch is in flight, so no test waits on a clock.
- **Effects map onto `PlatformHost`.** Each host effect kind is one capability. Adding a capability is an API change
  that is visible in review.
- **Pull, not push, for view state.** `changed` only wakes the client. The UI pulls patches on its next frame through
  CADisplayLink, Choreographer or ArkUI's frame callback, so bursts of changes coalesce into at most one patch batch per
  frame. That gives the 16–32 ms streaming coalescing without timers in the core, and keeps the burst off the main
  thread (`docs/apple/performance.md`, P7).
- **Keyed list diffs.** List patches are insert, update, remove and move by a stable id. They map directly onto
  SwiftUI's `ForEach` identity (P6), Compose's `LazyColumn` keys and ArkUI's `LazyForEach` data-source notifications.
  A full screen's state never crosses the boundary on every change.
- **Threading.** The core's state belongs to its worker thread.
  - `dispatch` only copies an action into the queue, so the main thread can call it directly, without hopping to a
    background queue first.
  - The worker applies actions, runs timers and file IO, and builds patches.
  - `drain` hands over the patches the worker already built.
  - Transport results come back from the host's network threads through `dispatch`.

What never crosses the boundary:

- **User-facing text.** The core returns codes and enums. Words, dates and numbers are formatted from each client's
  localization resources (`docs/protocol/i18n.md`).
- **Pixels.** Images, video and audio are decoded natively; only URLs, paths and metadata cross.
- **Haptics.** The UI decides them from the state it shows.

### PlatformHost capabilities by phase

| Capability | Phase | Notes |
| --- | --- | --- |
| Timers | later, if a domain needs one | The continuity slice has no settling timer: iOS fills a gap immediately. Any later timer has an independently justified behavior and a fixture |
| Transport (HTTP and WebSocket) | 2 | Stays native (decision 2). The host sends requests the core asks for, and passes response bodies and WebSocket frames to `dispatch` as raw bytes. JSON is decoded once, in Rust, off the main thread |
| Storage | 4 | Run by persona-core-runtime, not the host (decision 4). The host passes the per-hub scope directories (D6) when it creates the core |
| Secure storage | 3 | Host: Keychain, Keystore + Tink, HarmonyOS Asset Store (what the SSH core uses). The core sees opaque references, never key bytes, unless a domain needs them |
| Push registration, Live Activities / live views | never in the core | The core produces the state; the native code delivers it. The Live Activity extension cannot link the core |

### Errors, panics and symbols

- **Panics at the boundary.** Panics never cross FFI. Every entry point runs inside `catch_unwind` and returns an
  error code, as the SSH core's entry points do. Release builds use `opt-level = 3`, fat LTO and one codegen unit.
  They keep `panic = "unwind"`: its cost is negligible, and it lets a panic become a report instead of an abort.
- **Panic reports.** A `std::panic::set_hook` hook writes a short report, with a backtrace and no user data, into a
  bounded buffer. The client's diagnostics read that buffer.
- **Symbols.** Release builds keep debug information:
  - Apple: the XCFramework's debug symbols go into the app's dSYM.
  - Android: the unstripped `.so` goes to the Play Console's native debug symbols.
  - HarmonyOS: the unstripped `.so` is archived beside the HAP.
  `docs/operations/release.md` gains these steps once the core ships.

## Per-platform notes

### Apple

- **Swift 6 isolation.** App targets default to MainActor isolation. The generated codecs therefore live in their own
  package, `PersonaCore`, which is not MainActor by default. Its data types are `Sendable` values, and only its
  adapters call the C entry points.
- **Compiler.** The slice is built with Xcode 26.0 (Swift 6.2), the minimum compiler, under strict concurrency.
- **Where it is linked.** `apple/Shared` is compiled by the iOS, Mac, Vision, watch and widget targets, so `Shared/`
  never imports `PersonaCore`. The core is called from the iOS app's own code and from packages the watch does not
  link, as `PersonaKit` already is.
- **Rust targets.** The iOS device, iOS simulator and macOS targets are standard Rust targets; macOS is needed for
  `swift test`. The watchOS (`arm64_32`, `aarch64`) and visionOS targets were tier 3 in Rust's platform support list
  when this was written, so building them needs a nightly toolchain with `build-std`. Until that changes:
  - The watch keeps its Swift code.
  - Vision Pro keeps its Swift implementation of any domain that moves, behind the same adapter protocol.
  - This is decision 5.
- **Performance.** `docs/apple/performance.md` applies unchanged:
  - No main-thread call into the core does work proportional to a chat or list.
  - Core creation is lazy and off the launch path (P10).
  - The slice attaches before and after measurements from an optimized build (P12).
- **Build entry points.** XcodeGen needs the XCFramework before `xcodebuild` runs, so `scripts/app/sim.sh` and
  `scripts/ci/apple.sh` build it first. The build is cached by the hash of Cargo.lock and the core's sources, so an
  unchanged core costs nothing.

### Android

- **Libraries.** `cargo-ndk` builds `.so` files for the ABIs the app ships: at least `arm64-v8a`, and `x86_64` for
  emulators. They are linked for 16 KB pages, which Google Play requires for apps targeting Android 15 and later.
- **Kotlin bindings.** Kotlin declares the entry points as `external fun`, and Rust implements them as JNI functions.
  There is no JNA and no extra native library. D1 made `:core:domain` pure JVM, so its unit tests load a host build of
  the same library with `System.load`.
- **Size.** Binary size is measured per ABI ("Go / no-go").

### HarmonyOS

- **Precedent.** The SSH core already shows the build: Rust 1.98.0 for `aarch64-unknown-linux-ohos`, the API 26 native
  SDK and offline locked builds. The core reuses its toolchain setup and its license inventory scripts.
- **Binding.** napi-ohos (ohos-rs) builds the NAPI module and its `.d.ts` from the core's crate (decision 1). The
  `changed` wake-up reaches ArkTS through a thread-safe function.
- **Tests.** `harmony/tests` run in Node on the host and cannot load the device library. The fixtures are replayed in
  `cargo test`, and the ArkTS adapters are tested in Node against a fake core. One on-device suite replays the slice's
  fixtures through the real library; the slice must show it runs, following `docs/harmony/device-validation.md`.
- **Two Rust libraries.** The SSH core and the app core each carry a copy of std. Merging them is possible later; it is
  not part of this plan.

## Verification

- **`cargo test`** replays `spec/fixtures/<domain>` for every domain the core implements. From then on, that domain's
  reference implementation is the core. `spec/README.md` currently says "behavior follows the current iOS
  implementation"; its domain table gains a column naming the reference, and moved domains name the core.
- **Each client** replays the moved domain's fixtures through its real binding during the slice, to prove the
  binding. iOS also replays the full suite while its native `ChatStore` is still the behavioral reference, so a fixture
  gap cannot be hidden by agreeing with the Rust port. After that, a smoke subset per client is enough, since the logic
  is one implementation.
- **The web app and Go** keep replaying the fixtures of their own implementations, so all implementations of a rule
  are still checked against one source.
- **A new core lane in `scripts/ci/`** (core.sh) runs `cargo fmt --check`, `clippy -D warnings`, `cargo test --locked`, the
  codec-drift check and the license inventory check. `scripts/ci/apple.sh`, `scripts/ci/android.sh` and HarmonyOS's
  build install the pinned toolchain. `docs/operations/testing.md` ("Hosted CI") gains the lane.
- **Licenses.** Each app ships the notices for the core's Cargo closure, generated from Cargo.lock the way
  `harmony/native/ssh/collect-licenses.py` does.

## Phases

| Phase | Work | Exit |
| --- | --- | --- |
| 0 | This plan; the decisions below | Decisions recorded here |
| 1 | Workspace, toolchain, runtime thread, codec generator, the three bindings, CI lane, and `continuity` (decision 3) end to end on all three clients | Every client replays the continuity fixtures through its binding; the go / no-go table is filled in |
| 2 | The chat's item list driven by `continuity` patches, raw transport bytes decoded in Rust, then `sync`; iOS's native adapter and shadow run come first | Patches drive the real chat list on each client within the performance budgets, with the same iOS behavior and measured reference on each |
| 3 | The domains with the most drift, one issue each: `needs-you` / `attention` / `entries` once #5974 settles the definition, `lines` and `answer` (turn folding, the answer across wakes), `sort` / `recents`, `wire` (tolerant decoding, D5), streaming assembly (#4044) | Each domain: shadow run, then old code deleted |
| 4 | Storage in the core: the per-hub caches and the snapshot that launch draws from (D6), files first (decision 4). Design: `docs/plans/plan-rust-core-phase4-storage.md` (#9085) | Cold launch draws from the snapshot before any sync |

Every phase after 0 opens its own issue under #6793. Markdown parsing is not on the list: iOS renders markdown text with
MarkdownUI, and moving a syntax tree across FFI for every streamed chunk is a cost to measure before proposing it.

### Progress

Phase 1 (#6871) lands in several PRs.

- **First PR.**
  - `native/app-core` with `persona-core`'s `continuity`, the runtime, the C and JNI entry points and the Kotlin codecs.
  - `cargo test` replays all 38 continuity cases (94 steps), directly and through the runtime.
  - The iOS-alignment follow-up corrected the initial port's repeated-item, immediate-fill, opened-fold and promotion
    behavior, including the previously missing adjacency cases. The native-Store XCTest replay below is the separate
    check of iOS behavior; passing the shared Rust fixtures alone was not a substitute for it.
  - Android's `:core:rustcore` replays them through the real JNI binding on a host build.
  - The new `core` CI lane runs these checks.
  - Size, for orientation only: the stripped x86_64 release library is 678 KB (322 KB gzipped). The go / no-go numbers
    come from the device ABIs.
- **Apple binding and iOS-reference replay.**
  - `apple/Packages/PersonaCore` holds the generated Swift codecs, the C adapter and the XCFramework builder. Only the
    iOS app and `PersonaTests` link it; no binary is committed. The iOS production `ChatStore` is unchanged apart from
    restoring the existing `supervision_user` Codable key, and no screen changes.
  - The shared continuity corpus now has 38 cases (94 steps), including immediate fill, a complete repeated-item
    `ListOp.Update`, the `from_seq == covered + 1` boundary, opened-fold retention and supervision promotion.
    Real-C XCTest and a native-Store XCTest replay the same JSON and effects/patches; macOS `swift test` exercises the
    same C binding under Swift 6 strict concurrency. Local verification used Xcode 27.2, so Xcode 26.0 is not verified.
- **Android ABI packaging and real packaged-JNI replay.**
  - `:core:rustcore-android` packages stripped arm64-v8a and x86_64 libraries with separate symbols, exact Cargo
    notices, R8 keeps and uncompressed 16 KiB-aligned APK entries. The JVM/JNI and actual x86_64 emulator replays each
    pass all 38 cases / 94 steps. arm64 hardware and a 16 KiB-kernel device remain unmeasured.
  - Per-ABI stripped/package increments are 651,600 B (arm64-v8a) and 713,952 B (x86_64), with stripped gzip sizes
    323,071 B and 343,371 B. These are not store download deltas. Emulator performance exceeded the p99 budgets:
    dispatch 13,100.597 µs and drain+decode+apply 17,271.624 µs over 1,000 samples, so performance is not accepted.
  - **Android boundary profiling before the later optimization pass (#7274).** The original optimized span and 100 warm-up +
    1,000-sample protocol now retain raw nanosecond samples and explicit 50 µs / 1 ms assertions. On the same owned,
    software-accelerated x86_64 API 35 / 4 KiB-kernel emulator, the final repeat run measured dispatch p99 883.312 µs
    and drain+decode+apply p99 3,414.419 µs (p50 126.169 / 761.306 µs): both still fail. Earlier raw runs reached
    16,400.210 / 8,534.626 µs, illustrating the environment's large variation rather than an improvement to accept.
    Separate opt-in attribution and low-overhead controls are not substitute benchmark results. The final attribution
    (nonce `9198f0c2c237463caedafa69c014f4c6`) uses a named tail of 11 samples: the p99 sample at sorted index 989
    and the 10 samples slower than it. This is not the 10 samples above p99; the report never calls that set "top 1%".
    It found no actual `try_lock` contention and no overlapping GC events in those 11 samples. Caller scheduler
    switches occurred in 11 / 11 dispatch and 9 / 11 frame samples; worker TID was measured independently. Real patch
    lock acquisition without `/proc` reads totaled 30,274 ns across those 11 frame samples, versus 53,047,659 ns
    including those reads; work while holding that lock was separately 3,321,555 ns, Kotlin decode 75,033,476 ns and
    apply 10,582,298 ns. Thus these measurements do not isolate a production lock or codec defect.
    A low-overhead control with the original two-loop order still failed both budgets (dispatch 2,112.881 µs,
    frame 3,989.433 µs); its lock/JNI-free CPU loop had a 1,012.698 µs maximum despite 0.662 µs p50 (an earlier control
    reached 10,099.827 µs). This supports instrumentation and software-emulator scheduling as important contributors,
    not a claim that the original failure is waived or that physical arm64 meets a budget. These runs did not
    establish a production codec/allocation/synchronization correction; continuity behavior remains unchanged. The
    subsequent lock-free dispatch / JNI hand-off pass is recorded separately below. Physical arm64 / 16 KiB
    measurements remain required.
  - **Android string decode and failing-report capture, after #7274.** Kotlin's generated `BinaryDeserializer` now
    decodes a bounds-checked string range directly, retaining strict UTF-8 validation and cursor behavior while avoiding
    its temporary `ByteArray`. Typegen owns the change and checks an independent Rust/bincode golden plus malformed,
    truncated, trailing and excessive-length inputs. No wire type, runtime queue/lock, registry lease or original
    benchmark span/budget changed. The runner preserves the real nonzero JUnit output before enforcing its failure;
    the physical arm64 / 16 KiB cells remain **blocked: no physical device** by the maintainer's 2026-11-14 decision,
    not passes. Raw emulator A/B evidence and its limits belong in the linked PR, not a claim of device acceptance.
  - **Lock-free dispatch and zero-copy JNI hand-off (#9048).** The previous dispatch path held `queue: Mutex<Queue>`
    on the caller only to do `pending += 1` and check `stopping`, and `Runtime::dispatch(&[u8])` always copied the
    action bytes a second time even when the caller already owned them. `Shared.pending` and `Shared.stopping` are
    now an `AtomicU64` and an `AtomicBool`; the worker decrements with `fetch_sub` and only takes a mutex when the
    queue becomes empty and a `flush` waiter needs waking. The stop flag's Acquire/Release pairs with the channel
    send so an action accepted before shutdown still precedes Stop. A new `Runtime::dispatch_owned(Vec<u8>)` moves
    the encoded bytes straight into the worker's channel; the JNI `NativeBridge.dispatch` uses it so the
    `convert_byte_array` `Vec<u8>` is no longer cloned into a second buffer. The C ABI `persona_core_dispatch` keeps
    its borrowed-slice signature and routes through `dispatch(&[u8])` (one copy, same as before). The diagnostics
    `pending_lock*` counters stay at zero and only the patch-lock span is measured. Same host, same protocol (100
    warm-ups + 1,000 samples, raw nanoseconds, `PERSONA_MEASUREMENT_DIAGNOSTICS=0`), `:core:rustcore:boundaryCostTest`
    on `47d9e8468d` base versus the optimized branch:
    - dispatch p50 across 4 base runs 1.63–2.85 µs (mean 2.27) and 6 optimized runs 1.84–2.97 µs (mean 2.35);
      p99 base 3.29–5.04 µs (mean 4.37) and optimized 3.48–7.43 µs (mean 4.78); max base ≤ 14.13 µs, optimized
      ≤ 20.22 µs. **The two distributions overlap**: this change is a structural simplification, not a measurable
      host speedup. Both sides already sit far below the 50 µs budget on this host.
    - drain+decode+apply p50 base 3.76–4.20 µs, optimized 3.47–4.99 µs; p99 base 16.25–32.08 µs, optimized
      16.16–38.22 µs; max base 80.82–472.32 µs, optimized 53.25–302.50 µs. Same overlap; the 1 ms budget is met
      on host by both.
    All 38 continuity cases / 94 steps, 17 wire cases, the close-safe lifetime suite and the host-JNI replay pass;
    `scripts/ci/core.sh` is green. **This is host-only evidence**: it does not move the emulator cells, does not
    reach a physical device, and the physical arm64 / 16 KiB cells stay **blocked: no physical device**. Packaged
    x86_64 emulator A/B data was not collected on this run (no KVM on the build machine; the owned software-emulator boot
    stalled and was stopped). The go / no-go row's status is unchanged.
- **HarmonyOS binding, with device acceptance pending.**
  - `persona-core-ohos` provides one ohos-rs NAPI library with a close-safe runtime registry and thread-safe callbacks.
    The generated ArkTS codec has actual Node 24 round-trip tests; the adapter's fake-NAPI tests are not device proof.
    Its host registry replays all 38 cases / 94 steps, and the runtime emits one Update rather than remove/reinsert
    for a changed full item. The `coreReplay` ohosTest is ready to replay that same corpus through the real NAPI handle.
  - The API 26 native SDK, HAP link, installed HAP and physical-device replay were not available in this environment;
    no device or performance claim is made. Its exact 38-package / 71-notice runtime closure is checked on the host.
- **HarmonyOS host checks (#7414).**
  - The core lane failed on every clean checkout: its HarmonyOS notices step compared against a build output only
    the device build writes. It now writes that output itself (38 locked registry packages, 71 notice files).
  - The lane adds the `aarch64-unknown-linux-ohos` Rust target and lints the binding with its `ohos` feature, so the
    NAPI code is compiled by CI for the first time. Nothing is linked and no SDK is used.
  - `build-ohos.sh` now exports `OHOS_NDK_HOME`, without which the binding's build script panicked before compiling.
    Checked with a stub SDK layout only: the release closure compiles and the build stops at the link step.
  - The callback mismatch initially found here was fixed by #7713: callee-unhandled callbacks, byte-buffer effects,
    the two-argument targeted wake and generated-declaration drift checks now match. Its Node checks remain host
    evidence; no device execution proved that contract.
- **ArkTS codec against the real core on the host (#7450, #7485).**
  - A host replay drives the real runtime through `persona-core-ohos`'s registry with the generated ArkTS codec
    encoding actions and decoding effects and patches: all 38 cases / 94 steps, plus 35 independently built values
    whose ArkTS and Rust bytes must match in both directions. The core lane builds the helper and runs it with the
    two existing ArkTS core tests; a missing helper fails rather than skips.
  - It immediately caught a real defect the self-round-trip test could not see: `u32` wire fields were written and
    read as 8 bytes, so the codec could not decode a real patch. Typegen now emits every integer at its own width;
    Kotlin and Swift are unchanged.
  - This is host evidence only: no NAPI library, thread-safe callbacks, HAP or device. `coreReplay` stays blocked.
- **Still to come.**
  - iOS: physical-device performance, launch, memory and download-size evidence remains open; Xcode 26.0 is not verified.
  - HarmonyOS: the API 26 `.so`, HAP build and on-device replay.
  - Android: physical arm64, 16 KiB-kernel evidence and an iOS-referenced performance comparison.
  - Continuity: both Apple bindings and Android's host/emulator replay pass the same corpus. HarmonyOS's native replay
    must still run before calling the slice three-platform consistent.
  - The go / no-go measurements, with the iOS baseline named for every comparative measurement.

Phase 2 lands in several PRs.

- **Continuity payloads decoded in the core (#7385): pure decoding only.**
  - `persona_core::transport` decodes the latest / older page, the catch-up page, an opened fold's page and the
    WebSocket `items` frame from raw bytes into `continuity`'s inputs, keeping each item's JSON byte for byte.
    Acceptance follows iOS's `ItemsResponse`, `NewerResponse`, `foldedWork`'s `Range` and `ServerEvent.decode`.
  - `cargo test` replays the 17 wire cases those decoders own, all accepted payloads, and rebuilds the continuity
    corpus's 78 payload steps from bytes, with the same step and state as the hand-built batches. The corpus's three
    `http_latest` steps without `has_more` are not wire pages; the decoder refuses them, as iOS's `ItemsResponse`
    would. The refusal rules rest on the crate's unit tests, which try every key a decoder reads with every kind of
    wrong value.
  - At the time of that PR no boundary action carried bytes; the follow-up below adds them. Clients continued to decode
    natively in production; current raw-transport adoption remains unchanged.
  - Not full parity: envelope keys only other events use and nested structs the core does not read are not
    checked, and whole numbers must be integer literals, the only spelling the hub writes
    (`native/app-core/README.md`, "Transport decoding").
  - Not executed on iOS: the rules were read from the Swift sources and swift-foundation's `JSONDecoder`, and the
    wire cases' expectations are derived from the iOS sources too (checked by the hub's golden test and the Android
    replay). No shared wire case was added for the refusal rules, which wait for an iOS run to confirm them.
- **Byte-carrying actions, and the wire corpus through the JNI and ArkTS boundaries (#7385 scope 3).**
  - `Action::WirePayload` carries one response body or WebSocket frame as received, with the variant naming the
    request it answers. The worker decodes it with `persona_core::transport`; `dispatch` only queues the bytes. A
    frame of another type is ignored, and a frame iOS would drop is dropped without an error.
  - Replays of the same 17 wire cases, with the JSON exactly as the files hold it: through the runtime (32 items
    byte for byte; every case except the opened-fold range page is also compared with the equivalent hand-built
    `ChatEvent`, and separate tests drive a range page into an opened fold and an older page into history), through
    the real JNI binding and through ArkTS plus the transport decoder on the host. The generated Kotlin, Swift and
    ArkTS codecs gain the new variant; no client uses it yet.
  - Host measurement (best of 8, debug build): dispatching a 200 KB page costs about 21 µs against 1.8 µs for a
    small action, i.e. a copy, not a decode. The optimized Android benchmark and its budgets are unchanged.
  - Also fixed here: `ItemError`'s message quoted serde's error text, which repeats the offending value, so a
    malformed cached item could put message text into diagnostics. It now carries a context and a line/column
    position only, with a test.
  - At this stage no production client sends bytes. The C ABI's later Envelope-subset replay does not replay the full
    wire corpus through SwiftPM; Swift continuity replay is not evidence of raw-transport parity. On-device cells stay
    blocked: no physical device.

- **The core boundary's missing contracts (#7554).**
  - `ChatPatch.verified` carries whether the chat's items are known complete, the rule Android used to derive per
    client (`gap == None && (backlog || already verified)`), so no client re-implements the reducer's gap logic.
  - `Action::Ack` echoes a host's sequence number in the patch, so an applied no-op is distinguishable from a
    pending action. `Action::HubChanged` settles as if a gap were found, iOS's `settle(.fill)`.
  - Folds are keyed by id and independent: different folds open and complete in either order, as on iOS, and
    `Request::FetchRange`, `ChatEvent::Range` / `RangeFailed` and `Payload::Range` carry an optional `fold_id`.
    Callers that send no id keep the older single-opening behaviour.
  - The three codecs are regenerated; the existing 38-case / 94-step corpus still passes, with new tests for the
    verified rule, hub-changed input, acknowledgement and two folds completing in either order. The Android host
    JNI and ArkTS host replays cover the new fields.
  - The Android adapter (#7448) consumes these contracts behind its native production default and DEBUG shadow. The
    other clients have not adopted the runtime continuity contracts in production.

- **The `sync` domain's survey and fixtures-first design (#9230), documentation only.**
  - `docs/plans/plan-rust-core-sync.md` surveys iOS's chat-sync layer with file/line citations (socket lifecycle,
    reconnect/backoff, the event-resume cursor, per-chat subscription, gap-to-request, stalled catch-up, background
    prefetch, the catch-up barrier and foreground/background), defines the `spec/fixtures/sync-driver` fixture
    inventory with explicit clock/network injection, and carves out the first pure `SyncDriver` state-machine slice
    (lifecycle/network results in → request/reconnect/backoff/catch-up effects out, no real socket or persistence).
  - Nothing is implemented; no production network, client or core behavior changed, and no device claim is made.

- **Production iOS continuity adapter and DEBUG shadow (#8970).**
  - The iOS app's `ChatStore` stays authoritative behind `ChatContinuityAdapting`; DEBUG mirrors the same inputs into
    the real C core off the main actor, applies patches and compares decoded items and continuity state. Transport
    effects stay inside the shadow. Reports contain domain, rule and input shape, never content or identifiers.
  - The adapter covers cached snapshots, prefetch/fill pages, native reload retention, older pages and independent
    folds. The production shadow exposed and fixed empty-span list retention and pending patch state/ops mixing;
    the shared corpus now replays 39 cases / 95 steps on native iOS, real C and the runtime.
  - Release retains the original native calls and contains neither the shadow nor its switch.
  - Native/C replays, production comparisons and optimized simulator A/B measurements are reported with the PR; they
    are not a production Rust selection, a physical-device result or the end of the shadow-run cycle.

Phase 3 lands in several PRs.

- **Nested-item Envelope fallback removal (#9187).**
  - After #9082, an independent reviewer found that a valid `items` array could let a different bad scalar bypass the
    shared Envelope gate and become `Frame::Other`. Production now refuses the shared error before Other routing;
    there is no `NestedItems` shortcut and no additional full `ServerEvent` implementation.
  - Mixed bad-scalar / malformed-history inputs assert the source-required refusal, with the exact Foundation error
    priority marked `source-undetermined`. Isolated `rows` has `wrong_type: rows` from `Envelope.rows: Int?`;
    `args:1e999` is `malformed_json` from `JSONValue` / swift-foundation's non-finite-number rejection, never a
    payload-bearing error string. Expectations come from Swift sources, not from Rust output.
  - Existing 17 accepted wire cases, 38 / 94 continuity steps, #8955's 64 source-derived cases, real host JNI,
    ArkTS and independent C ABI replay all pass. This is host evidence only, not Swift/iOS or production-client proof.

- **Production shared-envelope scalar checks (#8995).**
  - `persona_core::transport::decode_frame` now validates the current iOS shared Envelope's scalar, list-shape and
    raw-JSON keys before routing: a wrong-typed known key drops the frame even when another event owns it. Unknown
    event types remain `Frame::Other`, and accepted items keep their original JSON and exact `bad_item` locations.
  - The unchanged #8955 source-derived fixture drives the production replay. Full Item, Machine, Session, interaction
    and Fleet nested models, number-spelling / encoding migration and non-items event routing are outside this slice.
  - The 17 accepted wire cases and the continuity corpus's 38 cases / 94 steps pass unchanged. Real host Kotlin/JNI,
    ArkTS and an independently compiled C11 client replay shared-scalar refusals and Other routing through the real
    production decoder / runtime. This is host evidence only, not Swift, device or production-client migration proof.
  - Android's current #7448 adapter maps its existing inputs to `Action.Chat`; it does not dispatch raw `WirePayload`
    frames in production, so this decoder change does not alter its default inputs or the debug shadow's comparison.

- **Source-derived WebSocket envelope tolerance (#8955).**
  - `spec/fixtures/wire-envelope` holds 64 hand-derived current-iOS cases covering unknown types, missing/null and
    wrong-typed known keys, extra and duplicate keys, raw JSON fields, `mesh` / `fleet` leniency and the routing
    requirements of six event types. Every case cites its Swift source; none was generated from Rust or run on iOS.
  - `persona_core::wire_envelope` is a separate pure diagnostic projection, not the continuity `decode_frame` path:
    it returns accepted / ignored / dropped (or out-of-scope for unprojected event payloads) without routing an event.
    The existing continuity behavior and 17 wire cases remain unchanged.
  - Rust and real host Kotlin/JNI replay compare every complete expected object against the same fixture, preserving
    duplicate-key input bytes and checking malformed input and wrong expectations. This is host evidence only; no
    Swift, production adapter, simulator or physical-device claim is made. Full nested models, numeric literal
    compatibility and subscription pre-routing stay out of this diagnostic slice. The later C ABI covers only the
    production Envelope gate's subset, not this complete projection; no SwiftPM or real-NAPI replay exists for it.

- **Agents list sort, recents and state decoding (#7579).**
  - `persona_core::sort` projects the Agents list from a JSON input: rank, grouping (state / machine / space),
    quick filters (all, needsYou, unread, working, backgroundWaiting), search with caller-supplied `now` for
    `since:` windows, pin/mute/quiet separation, heldInList (#2213) and fold/expand rules.
  - `persona_core::state` decodes client and hub session states: client-state precedence, hub `done` with a
    2-second seen tolerance, waiting detection from loop info / background tasks / subagents / workflows.
  - `persona_core::recents` implements the conversation and setup LRU with dedup, skip-current and limits.
  - 46 sort cases (33 projection + 2 session_state + 11 hub_state) + 21 recents cases (11 conversations +
    10 setups) = 67 exact fixture cases. #9033 adds three iOS-derived held-row projections: quiet held rows retain
    Unread rank in State, stay out of idle folds, and keep their space out of quiet spaces. Expectations cite
    `AgentListOrder.swift` and `AppModel.swift` and are not generated from Kotlin or Rust. The exact manifest assertion
    catches added, dropped or renamed cases; a mutation test catches a reversed date comparison.
  - Multi-hub grouping uses `group|machine_id` (`MachineRef.path`); BackgroundWaiting quick filter shows only
    rank-4; `since:` uses the caller's `now`, never a system clock.
  - Host boundaries: C FFI `persona_core_project_sort`, JNI `Java_..._projectSort`, NAPI `core_project_sort`
    (all pure JSON functions, no runtime handle). ArkTS host replay covers all 33 agents_list projection cases
    through the real Rust core. The core lane's ArkTS host tests include `sort.test.mjs`.
  - Stripped x86_64 host library: 1,423,872 B (up from 741,032 B; the sort/recents/state modules add ~683 KB).
  - HarmonyOS production uses `coreProjectSort` through `CoreAgentListAdapter` (#8957, #8966, final integration PR
    #9066), with iOS
    state/machine/space groups, pinned/quiet/folded rows, filters and ranks. The temporary native sorter and debug
    shadow are deleted after the unchanged fixture corpus matches 30/30. `spec/README.md` names the core as this
    domain's reference. A native failure retains the last usable projection and exposes failure; there is no second
    release sorter. Device-side NAPI linking, HAP and physical-device execution remain blocked: no physical device.
  - iOS and Android remain native by default. Android has an opt-in DEBUG-only sort shadow (#8954 / PR #9067); iOS's
    adapter and DEBUG shadow remain separate, unmerged work in #9058. `recents` and the standalone state projections
    have cargo evidence, and state also has ArkTS host replay; all 21 recents fixture cases now have ArkTS host replay
    (#9584) with complete expected objects and generated declaration drift checks, but no real NAPI invocation. The
    independently compiled Linux C11 client also replays all 21 recents complete objects through
    `persona_core_project_recents` (#9654), including wrong expectations, malformed operations and invalid UTF-8.
    Real Kotlin/JNI `projectState` (#9691) replays all 13 state complete objects through the same pure fixture adapter
    as the ArkTS host, with malformed/wrong-shaped inputs and wrong expectations rejected. Neither domain has a
    production migration or separate SwiftPM replay.

- **Turn grouping and wake-aware answer selection (#8218).**
  - `persona_core::turns` projects the static corpus's rows into turns, following current iOS `ChatBlock.build`.
    It handles raw-item frozen cuts, entry-based user pictures, tool pairing, compaction, supervision provenance and
    the wake of an idle agent, which opens a turn of its own since #9140 (until then: recap reopening and pending
    wakes inside the turn before it); it does not project rendered `ChatLine`s.
  - All 57 static turn cases compare complete `turns` arrays after applying the checked-in current-iOS overlay
    (`spec/fixtures/lines/ios/differences.json`, #8429). The independent maintainer's macOS replay passed the 59
    executable content cases and its properties; this Rust PR does not run Swift or claim that replay itself.
  - The Kotlin `projectTurns` declaration reaches the same pure projection through the real host JNI (#8756). Its
    replay independently applies the repository overlay and compares all 57 complete expected `turns` arrays: 54
    cases contain 62 complete turn objects after the overlay; `empty_session` and the two empty-turn layout cases
    compare empty arrays only. It checks malformed input plus a wrong expected value. This is host-JNI evidence, not
    a production Android adapter.
  - The four operation-sequence cases also have real Kotlin/JNI coverage (#8815). The host uses `RustCore`'s
    dispatch/drain patches to retain raw item JSON, then `projectTurns` for all 10 checkpoints: 13 complete turn
    objects and exact item IDs match the corpus plus the current-iOS overlay. A separate replay sends the existing
    burst replacement through the standalone `replace` branch against the same expected checkpoint. Malformed input
    and a deliberately wrong checkpoint are rejected. This is runtime-operation and turn consistency, not the
    incremental ChatStore rebuild algorithm or its timing evidence.
  - ArkTS host replay (#9235) adds `coreProjectTurns` and a pure `turns` JSON entry to the existing
    `persona-core-ohos` host helper, with generated declaration drift coverage. `turns.test.mjs` compares all 57
    unchanged static cases (62 complete post-overlay turn objects and three empty arrays), and uses the generated
    ArkTS codec plus existing dispatch/flush/drain commands for all 10 operation checkpoints (13 complete turn
    objects) and the standalone replacement. The real runtime requests the original fold range before the helper
    supplies it. Exact manifests, malformed/wrong-shaped input and deliberately wrong expectations are checked;
    fixture expectations and turn semantics remain unchanged.
  - This is ArkTS host helper evidence only. It does not execute the NAPI export or load a Linux NAPI addon, and
    does not replace #8808's separate real loader work. No HAP/HDC, physical-device, production adapter, rendered
    ChatLine/layout, incremental ChatStore rebuild or performance result is established.
  - `persona_core::task_index` covers the fixture-facing `TaskIndex.build` fields (#8842): started calls, notice/call
    association, descriptions, UTC start times and per-call status sequences. Real Kotlin/JNI replay compares all five
    existing task-index expected objects, plus a background receipt whose notices/statuses are empty but whose original
    description is retained. Offset timestamps are normalized to whole-second UTC. Malformed input and a wrong expected
    object are rejected. This is not rendered `ChatLine`, complete TaskIndex consumer state, UI badges, actual
    background execution or timing evidence.
  - `persona_core::viewed_images` covers the viewed-image leaf (#8860, #8943). Real Kotlin/JNI compares the
    complete post-overlay `vi-2#0` line for `tool_images_belong_to_call`, including `d:` identity from the original
    image payload and its owning image turn, plus duplicate/running/error/Read-extension and wrong-expected negatives.
    The owner comes from the existing turn projection and tool-step id, not from confusing `tool_use_id` with the use
    item id. Folded-work replay follows `ViewedImagesTests.aFoldGivesTheSameStripAsItsWork` with complete
    source-derived expected lines: path/size/`i:` identity, order and deduplication across ordinary and folded steps.
    The first segment stops at the next queued user; the last agent result independently owns running/error status.
    Optional fold payloads do not reject ordinary or queued text+image items (#9024); real fold counts/through fields
    retain their existing semantics. Inline data with width/height and no explicit size uses its
    extended-grapheme-count-derived size. `FoldImage.ref`
    synthesis is outside the pure leaf. This is not full `ChatLine`, multiple viewed lines, UI loading, actual tool
    execution or performance evidence.
  - The C ABI `persona_core_project_viewed_images` follows the same JSON/buffer ownership contract (#9017). The
    independently compiled C11 client compares the complete post-overlay ordinary viewed object and a complete
    source-derived folded object, including path/size identities and array order. It rejects malformed input, no-image
    no-line and deliberately wrong expected objects, and the header/export check covers the new function. This is
    Linux C ABI evidence, not Swift, Apple ABI, NAPI or device acceptance.
  - The two source-order layout cases, rendered lines, incremental rebuilds and production adapters are outside this
    slice. Static turns now have C ABI and macOS SwiftPM replay (#8912, #8952), but no real-NAPI replay; viewed images
    have C ABI replay (#9017) and macOS SwiftPM replay of 28 complete objects plus eight no-line boundaries (#9237 /
    #9246). ArkTS host replay (#9577) compares the ordinary complete post-overlay object and the source-derived folded
    object, with malformed, no-image and wrong-expectation controls. `coreProjectViewedImages` is generated and drift
    checked, but no real NAPI invocation is claimed. Both turn comparisons deliberately ignore
    rendered `lines` and `task_index` even where an overlay replaces them. No physical-device or performance claim is made.
    `spec/README.md` continues to name the shared corpus, not Rust, as the reference until migration.

- **Question answers and recommendation labels (#8460).**
  - `persona_core::answer` constructs the `ios_current` synchronous question answers and display-only recommendation
    labels. The current corpus has 15 question + 9 recommendation complete fixture comparisons (#9233 adds the
    newline / NBSP / TAB trimming case) and Apple/CoreFoundation-derived whitespace boundaries.
  - `AnswerBindingFixtureTest` drives `projectAnswer` and `displayAnswer` through real host-JNI string boundaries
    (#8838). The current corpus's 15 question + 9 recommendation cases compare complete expected objects, including
    conditional next-page, multi-select picks and display fields. Invalid pages/options, malformed input and a deliberately wrong
    expectation are rejected, and the U+200B boundary is exercised. This is synchronous host-JNI evidence, not UI
    timing, hub delivery or production Android integration. The current SwiftUI multi-question review confirmation
    remains separately #8483. The later C ABI, macOS SwiftPM and ArkTS host replays are recorded below; they do not
    execute the real NAPI boundary, transport or a physical device.
  - The draft a click leaves (#9238): a single choice empties its question's Other entry and keeps the key, as iOS's
    `QuestionForm.pick` does, and Advance and Submit change neither map. The core follows iOS; Android's builder removes the
    key, so the answer shadow reports `other-state` until its adapter drops emptied entries
    (`native/app-core/README.md`, "Android answer adapter and shadow (#9143)").

- **Streaming block boundaries (#8607).**
  - `persona_core::stream_chunks` projects only `StreamChunks.split` and its direct fence recognition. Its 21
    source-cited cases compare complete expected objects; direct Apple-test assertions are distinguished from
    source-derived boundaries. No Apple runtime was run for this slice.
  - `StreamBindingFixtureTest` drives the same pure projection through the real Kotlin/JNI `projectStreamChunks`
    boundary (#8827). All 21 complete `{done, tail}` expected objects come from the unchanged checked-in fixture;
    malformed input and a deliberately wrong expectation are rejected. Reconstruction is a secondary invariant, not
    the expected oracle. This is host-JNI evidence, not production Android integration or executed Swift.
  - `persona_core::stream_repair` adds the open-fence repair and code/text end projection (#9051), with real
    Kotlin/JNI replay of 21 complete source-derived repaired strings and six explicit code/text end cases from
    `StreamRepairTests`. It preserves
    opener marker/indent, holds unfinished opener/closer lines and reports a closing line as code until its newline.
    The final unfinished block-marker hold-back (#9084) separately covers all 13 Apple `markersOnly` inputs/controls,
    with malformed/wrong-expected and digit-boundary checks. Malformed input and deliberately wrong expected objects
    are rejected. Tables (#9164) add 22 repair and 15 table/text end assertions through the same real Kotlin/JNI
    boundary: half-written rows, early delimiter insertion, list indentation and lazy-line exclusion. Inline repair
    (#9286) adds 51 explicit Apple assertions plus source-derived boundary controls through that same real JNI
    boundary. It also replays all 342 Unicode-scalar prefixes of Apple's mixed reply through real stream-chunks JNI
    into stream-repair JNI, retaining every completed prefix; a finished reply remains unchanged. The inline scanner
    follows scalar-based flanking, code-span, link/image, escape, URL and inline-formula rules; display-math
    recognition prevents inline repair there. Math end (#9395) independently replays all six explicit Apple math
    assertions and 57 source-derived display/inline/math-boundary controls through real Kotlin/JNI, including
    `MathMarkdown.display`'s extended-grapheme predicate, open/closed blocks, code spans, money, CJK prose and
    trailing indentation. Plain-open-fence projection (#9515) adds all five explicit Apple assertions and 31
    source-derived scanner, indentation, whitespace, Unicode, drawn-predicate and closed/open-fence controls through a
    separate real Kotlin/JNI entry point. Its caller supplies the complete drawn-info-string list as input, replacing
    Apple's `drawn` closure; no renderer is called. Existing repaired/end behavior and the C ABI/ArkTS repair corpus are
    unchanged. The C ABI projection `persona_core_project_stream_repair` and the `stream_repair` ArkTS host
    projection (#9241) replay the existing fence/table/marker inputs, including inherited inline expectations, plus
    the JNI digit, leading-blank-line and delimiter boundaries as 22 fence + 19 marker/boundary + 39
    table/boundary complete `{repaired, end}` objects. Both surfaces reject malformed input and wrong `end`, wrong
    `repaired` and extra-field expectations; they do not replay the complete inline, math or mixed-reply corpora. The
    NAPI export `coreProjectStreamRepair` is committed in the generated ArkTS declaration and covered by the drift
    check. The plain-fence projection (#9563) also reaches the independent Linux C ABI client and real ArkTS host helper:
    both replay #9515's five explicit Apple assertions and 31 source-derived controls through complete `{text}` objects
    and wrong-expectation checks with the caller's full drawn-info-string list. `coreProjectPlainOpenFence` and its declaration are committed and drift
    checked, but neither that NAPI function nor a SwiftPM projection has been executed.
    `MathMarkdown.rewrite`, visual gaps, timing, rendering, the plain-fence SwiftPM projection,
    production adapters and the #4044 performance goal remain open. These repair slices have cargo, real host-JNI, C ABI
    and ArkTS host evidence; real NAPI / SwiftPM projection is not yet covered.

- **Answer, stream chunks and task index through the ArkTS host (#8937).**
  - `persona-core-ohos` exports `core_project_answer`, `core_display_answer`, `core_project_stream_chunks` and
    `core_project_task_index` beside its sort projection. The four pure functions have the existing JSON string
    contract; `core_display_answer` accepts a bare label rather than a JSON-encoded string. Their generated ArkTS
    declaration and exact export drift checks are committed. These declarations compile with the `ohos` feature, but
    no Linux addon is loaded and no real NAPI argument conversion is claimed.
  - `harmony/tests/core-host/projections.test.mjs` calls the real Rust functions through the existing host helper and
    compares the current 15 + 9 answer/display, 21 stream-chunk and five complete task-index expectations from the
    checked-in fixtures (#9233 added the ninth recommendation), plus the background receipt. It pins exact case names
    and rejects malformed input and deliberately wrong
    expected objects for each projection. The core lane includes this replay; a missing helper fails rather than skips.
  - This is ArkTS host evidence only, not a HAP or physical-device result. No production adapter, turns, StreamRepair,
    UI timing, hub delivery or background execution is exercised. #8808 retains the separate turn NAPI loader work.

- **The four pure projections reach the C ABI (#8912).**
  - `persona-core-ffi` gains `persona_core_project_answer`, `persona_core_display_answer`,
    `persona_core_project_stream_chunks`, `persona_core_project_turns` and `persona_core_project_task_index`, sharing
    `persona_core_project_sort`'s contract: one UTF-8 JSON document in, one JSON document out, no core handle, the
    caller frees the result with `persona_core_bytes_free`, and any decode/projection/encode failure returns an empty
    buffer. `display_answer` takes a bare JSON string (the option label), as its JNI counterpart does. The checked-in
    `include/persona_core.h` declares all five; the existing dynamic-export drift check covers them (13 exports).
  - The #8560 independently compiled C11 client now replays the unchanged fixtures through the real release library on
    Linux: answer 15 question + 9 recommendation cases (complete conditional expected objects, invalid page/option,
    the U+200B trim boundaries, and #9233's newline / NBSP / TAB trimming case), stream_chunks all 21 complete `{done, tail}` objects, turns all 57 static cases with
    the checked-in `spec/fixtures/lines/ios/differences.json` overlay applied by the client (54 cases hold 62 complete
    turn objects; three compare empty arrays only), and task_index all five complete expected objects plus the
    background receipt boundary. Both sides are parsed and compared canonically (sorted object keys, exact array order
    and scalars), so no comparison depends on Rust's serialization key order. Exact-name manifests fail on an added,
    dropped or renamed case; malformed input returns an empty buffer per domain and a deliberately wrong expected
    object is rejected per domain. The core lane's `Independent C ABI client` step runs all of this; a missing fixture,
    client source or release library fails rather than skips.
  - This is Linux host C ABI evidence only: no NAPI, production adapter, physical-device or performance claim. It was
    followed by the Swift wrappers and macOS SwiftPM replay below; this step itself did not execute the Swift importer
    or Apple ABI. The Swift adapters and their real-C replay are covered by #8952; the
    turn operation checkpoints, rendered lines and StreamRepair stay out of this slice.

- **The six pure projections reach the Swift C binding (#8952).**
  - `PersonaCoreProjections` calls `persona_core_project_sort`, `persona_core_project_answer`,
    `persona_core_display_answer`, `persona_core_project_stream_chunks`, `persona_core_project_turns` and
    `persona_core_project_task_index`. Each wrapper takes and returns UTF-8 JSON, copies the returned bytes and frees
    every result with `persona_core_bytes_free`; an empty C result is an explicit error. There is no core handle,
    production caller or worker. These synchronous calls stay off the main actor when their input is sizable.
  - macOS `swift test` replays the unchanged fixtures through the real C XCFramework: all 33 `agents_list` sort cases,
    answer 15 question + 9 recommendation cases (conditional expected objects, invalid page/option and U+200B; the
    ninth recommendation is #9233), all 21
    complete stream-chunk `{done, tail}` objects, all 57 static turn cases with the checked-in current-iOS overlay
    applied by the test caller (54 cases hold 62 complete turn objects; three compare empty arrays), and all five
    complete task-index objects plus the background receipt. Comparisons retain object keys, array order and exact
    scalar types; exact-name manifests, malformed inputs and deliberately wrong expectations are checked per domain.
  - Fixture reads and the overlay live in `PersonaCoreTestSupport`, not the shipped library. This is **macOS-host
    SwiftPM evidence, not iOS simulator or physical-device evidence**. It does not establish production adapters,
    rendered lines, incremental rebuilds, StreamRepair, transport, timing or the separate `session_state` / `hub_state`
    projections (which have no C entry point in this six-function slice). Local verification used Xcode 27.1, so
    **26.0 not verified**.

- **Android heldInList production alignment (#9033).**
  - Android's production `AgentSession` carries the visible-chat held flag through `AgentListOrder` / `AgentList`.
    A quiet held row retains Unread rank and stays visible, and its space is excluded from quiet-space grouping.
    The shell's existing visible Agent chat is latched only if the row was Unread on opening, and released on leaving.
  - `spec/fixtures/sort/held.json` supplies three State, Space and quiet-fold cases derived from current iOS
    `AgentListOrder.stays`, `rank`, `isQuiet` and `AppModel.visibleSession` / `markSeen`. Rust, real Kotlin/JNI,
    the independent C client, ArkTS host and Swift/C host replay compare the complete expected objects. A separate
    core fix matches `SessionKey.path` for held identity: the hub group must not be lost from a held row's id.
    No iOS production behavior changes.
  - Native JVM Compose captures cover Glass, Brief and Board in light and dark, both held and released. They are
    synthetic host-render evidence, not an emulator, device or performance measurement; Apple compilation is host
    SwiftPM evidence, not simulator or physical-device evidence.

### Migration rules (each domain)

1. **Fixtures first.** The domain's fixtures cover the rules the current iOS implementation follows today. Compare
   Android and HarmonyOS with that reference; gaps found while porting become new cases checked against iOS.
   Expectations are never generated from the code under test, including the initial Android-derived continuity port.
2. **Core next.** The core passes them in `cargo test`.
3. **Adapter.** iOS puts its current implementation and the core behind one protocol first, with the native path kept
   as the shadow-run reference. Android and HarmonyOS follow the same adapter contract.
4. **Shadow run.** For one TestFlight and internal-testing cycle, DEBUG and test builds run both implementations and
   report mismatches to diagnostics. A report holds the domain, the rule and the shape of the input, never content.
   Release builds use the core only after the shadow run is clean.
5. **Delete.** The old implementations are deleted, and `spec/README.md` names the core as the domain's reference.

## Go / no-go after phase 1

The slice measures these against the limits of decision 6. If any limit is exceeded and cannot be fixed within the
slice, the plan stops: D1 stays, and the result is recorded here. Numbers say whether they come from a device or a
simulator, and from which build (`docs/apple/performance.md`, "Measuring").

| Measure | Limit | iOS | Android | HarmonyOS |
| --- | --- | --- | --- | --- |
| Download size added, per platform (per ABI on Android) | ≤ 1 MB for phase 1; every later phase reports its own increment | **Measured, macOS host / unsigned generic-iOS build:** local payload proxy +763,346 B and local ZIP +355,744 B, both below 1,000,000 B; **download-size limit: not measured / open**. Physical-device evidence: **blocked: no physical device**. [1][2] | **Measured, host-built packages:** arm64-v8a package proxy +651,600 B and x86_64 +713,952 B, both below 1,000,000 B (gzip 323,071 / 343,371 B); **download-size limit: not measured / open**. [3] | **Not measured:** no linked API 26 library or HAP increment; **download-size limit: open**. Physical-device evidence: **blocked: no physical device**. [4] |
| Launch to first frame | no change beyond run-to-run noise; the core is not created before the first frame (`docs/apple/performance.md`, "Budgets") | **Not measured as a comparison:** the retained native simulator run is an incomplete historical observation, not a with/without-core baseline. Physical-device evidence: **blocked: no physical device**. [1][2] | **Not measured.** The packaged app does not construct the core on launch; that is not a timing comparison. [3] | **Not measured**; physical-device evidence: **blocked: no physical device**. [4] |
| `dispatch` called on the main thread | p99 ≤ 50 µs, encoding included | **Measured, simulator:** 6.750 µs p99, **within the fixed budget** (optimized binding harness, 100 warm-ups + 1,000 samples; physical: **blocked: no physical device**). [1][2] | **Exceeds, unresolved:** original software-emulator p99 13,100.597 µs; final retained repeat 883.312 µs, both over 50 µs. Physical arm64 / 4 KiB runs varied 22.578–135.626 µs: **not the required 16 KiB acceptance or a stable fixed-budget pass**; 16 KiB device: **blocked: no physical device**. [3][5][6] | **Not measured:** host registry tests are not timing evidence; physical NAPI: **blocked: no physical device**. [4] |
| `drain` + decode + apply, once per frame, on the slice's data | p99 ≤ 1 ms; nothing on the main thread over 16 ms while scrolling or streaming (8 ms is the goal on 120 Hz screens) | **Measured, simulator:** one-item 6.958 µs and 100-item 183.667 µs p99, **within 1 ms**; these are synthetic binding/string-list workloads. The 16 ms / 8 ms production scrolling-streaming limits are **not measured**. Physical-device and equivalent production-frame evidence: **blocked: no physical device / not measured**. [1][2] | **Exceeds, unresolved:** original software-emulator p99 17,271.624 µs; final retained repeat 3,414.419 µs, both over 1 ms. Physical arm64 / 4 KiB runs varied 50.963–312.906 µs: **not a 16 KiB or equivalent-workload pass**; 16 KiB device: **blocked: no physical device**. The 16 ms / 8 ms production scrolling-streaming limits are **not measured**. [3][5][6] | **Not measured:** host replay is not timing evidence; physical NAPI: **blocked: no physical device**. [4] |
| A hub frame to the screen | no later than today on each client | **Not measured:** no equivalent native/core production-screen comparison; physical: **blocked: no physical device**. [1][2] | **Not measured:** no production screen or hub-frame comparison; iOS baseline: **not measured**. [3][5] | **Not measured:** no NAPI/HAP production screen comparison; physical: **blocked: no physical device**. [4] |
| Memory | resident memory the core adds is reported; opening and closing the same chat 10 times ends within 5 MB of the start | **Measured, simulator:** ten binding create/open/drain/close cycles changed `phys_footprint` by +16,384 B, **within 5 MB**; native chat/list round trips changed resident size by +4,128,768 B, also within 5 MB, but that run is a legacy non-acceptance observation. Total resident memory added by the core in production: **not measured**. Physical: **blocked: no physical device**. [1][2] | **Not measured:** no ten-open/close or resident-memory report; physical: **blocked: no physical device**. [3] | **Not measured:** host tests do not report device memory; physical: **blocked: no physical device**. [4] |
| Incremental app build with the core unchanged | no Rust rebuild | **Measured, macOS host:** second unchanged-input XCFramework build reported `cached XCFramework is current`, **within limit**; full incremental app-build time not measured. [7] | **Measured, Linux host:** repeated unchanged-core Gradle build was `UP-TO-DATE`, **within limit**. [3] | **Not measured:** no actual API 26 HAP incremental build. [4] |
| Clean build time added | reported per platform; no fixed limit | **Not measured:** no matched clean with/without-core app build. | **Not measured:** no matched clean with/without-core app build. | **Not measured:** no actual API 26 clean HAP build. A hosted Ubuntu core-lane compile took 11.48 s for the release FFI step, not an app-build increment ([run 43564606776](https://github.com/nwverse/persona/actions/runs/43564606776)). |
| Contributor setup | `scripts/app/sim.sh`, `scripts/app/android.sh` and HarmonyOS's build still need one command after installing rustup | **Not measured as a complete fresh-contributor run:** `scripts/app/sim.sh` invokes the XCFramework builder before the app build, and the builder installs the pinned toolchain/targets through rustup; the binding PR verified the framework-first build path, not a clean one-command `sim.sh` run. **Setup limit: unverified**; Xcode 26.0 also remains not verified. [7] | **Measured, Linux host:** the Gradle app build invokes `buildAndroidCore`, which installs missing pinned Rust targets. **`scripts/app/android.sh` / one-command contributor setup: not measured / unverified**; SDK, JDK 21 and NDK are prerequisites, and the reviewed Gradle script used Linux NDK host tools. [3][6] | **Not measured:** no actual DevEco/API 26 one-command build was run; **setup limit: unverified**. The API 26 SDK and physical device remain **blocked: no physical device**. [4] |

**Reading the table.** "Measured" records an existing observation, not physical-device or production acceptance;
host, emulator and simulator values stay in their own categories. Android's package increments are APK/native-library
bytes, not store downloads. The physical-arm64 measurements in [6] came from a contributor's Pixel 11 Pro XL,
Android 17 / API 37, 4 KiB kernel, main `564904f816`, Rust 1.98.0 / NDK 29.0.14206865; the six R8 release-test runs
were not independently rerun by the reviewer and do not establish the required 16 KiB-kernel result. The maintainer's
2026-11-14 decision leaves the specified physical-device cells **blocked: no physical device**, never passed by
substituting a simulator or host (#6871, [decision comment](https://github.com/nwverse/persona/issues/6871#issuecomment-6869406664)).
An **exceeds, unresolved** cell follows the rule above: if the limit cannot be fixed within the slice, the plan stops
and D1 stays. Filling in this table does not itself close #6871 or approve production migration.

**Sources and exact workloads.**

1. **[iOS reference-measurement PR #7639](https://github.com/nwverse/persona/pull/7639)** — **unmerged and closed**;
   its final PR head retains the raw reports, while `main` does not contain its harness or artifacts. It was closed
   after a retained build log was found to contain registered app identities; the cited final head removes that
   attachment, while earlier PR refs remain historical ([disposition](https://github.com/nwverse/persona/pull/7639#issuecomment-6876490516)).
   Its reported measurements are therefore existing evidence, not merged or completed physical-device acceptance.
   All iOS simulator values below use arm64 iPhone 17 Pro / iOS 27.0 simulators, Xcode 27.1 / Swift 6.4 /
   Rust 1.98.0, optimized Debug (`-O`, wholemodule, `PERF_SIGNPOST`), not a store Release or Xcode 26.0.
2. **Retained iOS evidence readme, PR #7639 head** (`README.md` in docs/archive/apple/reference-measurements-6871/ at the head commit
   [`5eefe3cf0d1b`](https://github.com/nwverse/persona/commit/5eefe3cf0d1b31ad05c5339bf3295b21e5b824f8), which `main` does not contain; the reports below are in its `raw/`) —
   exact run identities, raw files and limitations. Binding/lifecycle source base `84a5a314c553` with the recorded
   uncommitted run-identity changes, simulator `58B70B19-21DA-4051-B017-0D828579958A`, 100 warm-ups + 1,000 samples.
   Dispatch raw report (`raw/core-boundary.json`)
   includes generated Swift bincode encoding + handle lock + C queue, not worker/flush time.
   100-item drain report (`raw/core-large-page.json`)
   covers drain/bincode decode/string-list apply, not a presented frame. Lifecycle report (`raw/core-lifecycle.json`)
   has one warmup and ten cycles; `phys_footprint` 62,851,952 → 62,868,336 B. The native-memory report's
   `resident_size` is 628,326,400 → 632,455,168 B. Launch and native UI records predate final run/trace metadata and
   are explicitly not final acceptance. The package comparison used unsigned generic-iOS Release + `PERF_SIGNPOST`
   on a recorded dirty measurement tree: `.app` 127,081,693 → 127,845,039 B; local ZIP 60,500,929 → 60,856,673 B.
   No App Store processing, thinning, physical artifact or equivalent production-core UI run was performed.
3. **[Android packaging and benchmark PR #7128](https://github.com/nwverse/persona/pull/7128)** — merged as
   `eeac9a5794`; Rust 1.98.0, NDK 29.0.14206865, JDK 21 on x86_64 Linux. Package report: arm64-v8a raw symbols
   6,998,464 B, stripped/package 651,600 B, gzip 323,071 B; x86_64 7,007,880 / 713,952 / 343,371 B. The benchmark
   used the non-debuggable R8 release androidTest APK on an owned software x86_64 Android 15 / API 35 emulator,
   build `AE3A.240806.043`, 4 KiB kernel, 100 warm-ups + 1,000 samples. Original p50/p99/max: dispatch
   155.520 / 13,100.597 / 61,333.795 µs; frame 745.018 / 17,271.624 / 56,622.362 µs. The same PR reports the
   unchanged-core `UP-TO-DATE` build and Gradle app build, not clean-build, store-download or one-command
   `scripts/app/android.sh` evidence. Its Gradle build selected the Linux NDK host tools. The separate macOS
   physical-device report in [6] used a local NDK host-directory symlink, so it does not establish a general
   fresh-contributor setup.
4. **[HarmonyOS NAPI binding PR #7174](https://github.com/nwverse/persona/pull/7174)** — merged as `8827b3f885`;
   host-only macro/codec/runtime checks passed, but the API 26 `.so` link, HAP build/install, real NAPI replay,
   size and timing were not run. **[Callback follow-up #7713](https://github.com/nwverse/persona/pull/7713)** and
   its [final Node 24 check](https://github.com/nwverse/persona/pull/7713#issuecomment-6878001978) remain host
   evidence only, not a device build or performance result.
5. **[Android retained benchmark PR #7224](https://github.com/nwverse/persona/pull/7224)** — merged as `1c0229e078`;
   same software emulator, API 35 / build `AE3A.240806.043`, 4 KiB kernel, no `/dev/kvm`, optimized R8 release test,
   100 warm-ups + 1,000 samples. Final original-span dispatch p50/p99/max 126.169 / 883.312 / 2,768.431 µs; frame
   761.306 / 3,414.419 / 6,189.888 µs. Earlier raw p99 16,400.210 / 8,534.626 µs is retained, not selectively
   replaced. Attribution and a low-overhead control identify instrumentation/scheduling as contributors but do not
   waive either failure or demonstrate a physical-device result.
6. **[Physical-arm64 report in #6871](https://github.com/nwverse/persona/issues/6871#issuecomment-6866766583)** —
   contributor-reported, not independently rerun. Unmodified runs 1–3: dispatch p99 135.626 / 32.786 / 93.047 µs,
   frame p99 152.385 / 50.963 / 124.297 µs. Runs 4–6 added only a local p90 reporting field: dispatch p99
   22.578 / 103.411 / 108.880 µs, frame p99 52.708 / 312.906 / 292.187 µs. Large run-to-run variation, a 4 KiB
   kernel and no iOS physical baseline mean these are not accepted as full-budget, 16 KiB or comparative passes.
7. **[Apple binding PR #7160](https://github.com/nwverse/persona/pull/7160)** — merged as `5f1920294a`; Xcode 27.2 /
   Swift 6.4.2, Rust 1.98.0. It reports the cached unchanged-input XCFramework build, real-C and native-store replay,
   and `scripts/app/sim.sh`'s framework-first path. Xcode 26.0 is not verified; no launch, memory, actual app-size
   or timing acceptance is claimed there. **[Targeted-wake follow-up #7401](https://github.com/nwverse/persona/pull/7401)**
   likewise leaves physical performance and production migration unaccepted.

## Decisions

Made by the maintainer on 2026-11-13, with one rule: **performance first; only where the performance difference is
negligible, the option that is better in the long term.**

1. **Bindings: a few entry points that carry bytes; no UniFFI, no JNA.**
   - **Why.** UniFFI serializes records on every call anyway, and its Kotlin bindings go through JNA, which is slower
     than JNI on both calls and callbacks and adds a native library per ABI.
   - **How each client calls it.** Swift calls the C entry points directly, Kotlin through JNI functions written in
     Rust, and ArkTS through napi-ohos from the same crate, as one `.so`.
   - **Typed values.** They come from the generated codecs ("The boundary: a few entry points and bytes").
2. **Transport stays native.**
   - **Why.** The OS network stacks bring HTTP/2 and HTTP/3, connection reuse, radio and power scheduling, background
     transfers, system proxies and the pinning each client already has. tokio and rustls would add size and threads
     for none of that.
   - **How bytes reach the core.** The host hands response bodies and WebSocket frames to the core as raw bytes. JSON
     is decoded once, in Rust, off the main thread, never also natively.
3. **First domain: `continuity`.**
   - **Why.** It sits on the hot path: the chat's item list, streaming and reconnects. Phase 1's measurements therefore
     describe the path that matters.
   - **Why not `needs-you`.** It would not exercise that path, and #5974 is still redefining it.
4. **Storage moves into the core, files first.**
   - **Why.** Persisting state the core holds through the host would copy it across the boundary on every write. Then
     launch would read it back natively and copy it in again.
   - **How.** persona-core-runtime writes the per-hub caches and the launch snapshot itself, in the same binary
     encoding, on its worker thread.
   - **SQLite.** It joins only with a domain that needs indexed queries over large data, and that phase measures its
     size.
   - **Secrets.** They stay in each OS's secure storage, through the host.
5. **The watch and Vision Pro keep Swift.**
   - **The watch** is the most memory- and size-constrained device, so it does not take a Rust library.
   - **Vision Pro** would gain nothing measurable either way. The long-term choice wins: release builds never depend
     on a nightly toolchain, which tier-3 targets need.
   - **Revisit** both when their Rust targets reach tier 2.
6. **Go / no-go limits: tightened** to the table above. Download size added ≤ 1 MB, a main-thread `dispatch` p99
   ≤ 50 µs, a frame's `drain` p99 ≤ 1 ms, no later frames on screen than today, and memory checked. Comparative
   targets come from measured iOS under the same workload; missing iOS evidence leaves the comparison open.
7. **iOS is the unified clients' reference.** Added by the maintainer on 2026-11-13, and applies to every earlier
   decision and phase here.
   - **Behavior.** Shared behavior, state machines and data flow match iOS. Audit the initial Android-derived
     `continuity` port, add fixtures for differences and fix the port before iOS binding acceptance; iOS is not changed
     to match an existing port or fixture.
   - **UI and interaction.** Android and HarmonyOS follow iOS's interaction and visual result in their native UI
     toolkits and existing styles. Phase 1 changes no screen; cross-client UI differences are separate scoped PRs with
     paired screenshots, not incidental binding changes.
   - **Performance.** First frame, streaming, scrolling and memory targets use measured iOS numbers with the device,
     build and workload recorded. Fixed FFI budgets still apply. Do not infer a device result from a host or simulator
     measurement.
