# Archived client redesign and Android design proposal

> Historical draft from 2026-10-30, not an implementation status report. Android and hub work has since shipped.
> Follow `docs/android/android.md`, `docs/android/android-design.md`, and the current module guides.
> Retained for interaction requirements, deferred platform scope, and original design trade-offs; no development images are kept.

> Status: **Draft v3, awaiting review** (2026-10-30). Only research and design so far; no code changed.
> Section 12's questions were decided as recommended on 2026-10-30. Whether hub has a domain usable for DDNS, and which physical devices are available, still need your input. Implementation follows Section 10's phases.
>
> **Scope for this round** (decided 2026-10-30):
> - **Android and hub only; iOS is read-only**. iOS is a reference: read its implementation and tests to build Android. Specifications describing both clients apply only to Android this round; iOS follows later.
> - **Phones have no Google services**.
> - **Phones connect directly to hub over the public internet, IPv6 preferred**; users configure their own VPNs.
> - Every hub change must leave **existing iOS apps working without code changes**.
>
> **Changes from v2 to v3**:
> - Rewritten for this scope: notifications use only persistent connections; FCM, Firebase, and Google login removed (D8). Public direct connectivity added (D9, [hub-changes.md](hub-changes.md) Section 1 rewritten). All iOS changes moved to “Later.”
> - Chinese-ROM compatibility added ([android.md](android.md) 5.18): data-backed minSdk 31, vendor background/notification restrictions, readiness checklist, self-tests, distribution, and in-app updates.
> - Added [flows.md](flows.md) (15 main-feature flowcharts), UI previews `prototypes/android/` (removed in #9030; see git history), and 3 research notes.

This document set answers four questions:

1. Which strengths of the author's other native iOS app (a Lark-gateway client + driving Claude Code in herdr over SSH from a phone) should we adopt?
2. What is the redesigned structure and interaction model? Android implements it this round; iOS follows later.
3. How do we build an Android app matching the redesign?
4. Which choices in the first Android version already merged to master (PR #84, Agents) are better than this proposal, and which should it replace ([android.md](android.md) Section 1)?

| Document | Contents | Recommendation |
| --- | --- | --- |
| This document | Findings, key decisions, target architecture, roadmap, risks, open questions | Read first (about 20 minutes) |
| [ux.md](ux.md) | Shared interaction/UI specification: information architecture, page changes, components, state machines, copy glossary, design tokens | Read for what the redesign looks like |
| [android.md](android.md) | Android technical proposal: item-by-item comparison with PR #84, stack, modules, key subsystems, platform mapping, first-release scope, testing/distribution | Read for Android implementation |
| [ios-refactor.md](ios-refactor.md) | Phased steps for iOS to follow later (**not executed this round**) | Read later |
| [hub-changes.md](hub-changes.md) | Server changes (required / recommended / optional) | Read for the extent of hub changes |
| [parity.md](parity.md) | Feature-parity matrix: 213 existing iOS features + redesign additions, Android approach and priority for each | Acceptance checklist |
| [flows.md](flows.md) | Main-feature flows (Mermaid): pairing, route selection, persistent connection, notification approvals, sending, chat synchronization, Inbox | Read for how flows work |
| `prototypes/android/` | Android UI previews (interactive HTML, light/dark); removed in #9030, see git history | Superseded |
| [research/](research/) | 11 original research notes (code locations and sources) | Consult to verify evidence |

---

## 0. One-page summary

- **One product specification, two native clients; Android only this round.** Android uses native Kotlin + Jetpack Compose; iOS remains unchanged. No shared code; three shared assets:
  - **Behavior specifications and fixtures**: the same JSON inputs and expected outputs. Read iOS implementations/tests this round (read-only), turn them into Android-side fixtures; continuity rules use existing Go simulations. iOS adopts these fixtures later.
  - **Design tokens**: one JSON source, values from current iOS `Theme` / `StatusPalette`; generate only Kotlin this round.
  - **hub protocol**: samples exported by Go tests.

  Android core modules are pure Kotlin without Android dependencies; future KMP sharing would require only build-configuration changes.
- **Android follows this modular layering.** Borrow the reference iOS app's contracts / implementation / pages separation, but use only a dozen or so modules.
  - Layers: `Contracts` (wire types and narrow interfaces) → `Core` (pure logic) → `HubClient` (network) → `Data` (persistence/synchronization) → `DesignSystem` → `Feature` modules → `App` (composition root).
  - Pages depend only on interfaces, enforced at compile time.
  - iOS later follows the same split, including breaking `AppModel`'s 22 responsibilities into roughly ten services ([ios-refactor.md](ios-refactor.md); not this round).
- **Borrow the reference iOS app's three-state outcomes, but put heavy work on hub.**
  - States: success / definitely not sent / outcome unknown.
  - Every side-effecting request carries an operation ID; hub persists receipts in Postgres.
  - Automatically resend unsent requests; retry sent-but-unacknowledged requests with the same ID, deduplicated by hub. Errors midway through typing on a computer enter “Needs verification,” checked automatically against chat history.
  - This removes the need for the reference iOS app's phone-side durable queue and manual-verification system.
- **Keep Persona's interaction strengths and add the reference iOS app's reading tools and complete native interactions.**
  - Keep information architecture, inverted lists, collapsed work rows, terminal, pushes.
  - Reading tools: in-session search, task progress, full-text reader, failure counts even when collapsed.
  - Native interactions: denial reasons, mutual exclusion of plan feedback and approval, countdowns, MCP forms, sub-Agent attribution, multiple requests in Inbox.
  - One status location: a single row above chat input says what is happening; “Needs verification” becomes a formal state.
- **Reorganize Android from merged PR #84, not a rewrite.**
  - About 2.7 thousand lines, one module, physically validated, all 10 local unit tests pass.
  - Better choices than the original draft, adopted directly: foreground-service WebSocket notifications instead of push services (most domestic phones lack Google services); zxing QR scanning without Google; cleartext HTTP allowed globally (network security config does not support subnets); older target to avoid Android 17 local-network permission; direct question choices in notifications, withdrawn after handling.
  - Replace under this proposal: toolchain/SDK levels, modules/architecture, storage (tokens currently plaintext), chat presentation (no work-row collapse or `fold_work`), Markdown, state colors (differ from iOS), notification Allow without unlock.
  - Item comparison and three lists (keep, move, replace): [android.md](android.md) Section 1.
- **First Android release = 76 iOS P0 features plus Share to Persona, public direct connectivity, Chinese-ROM compatibility, and in-app updates.**
  - Includes pairing, connection state/diagnostics, caches, offline sending, Agents list, chat page, four interaction panels, Persona main chat, basic Inbox, notifications with actions, one native-style theme.
  - Terminal, Live Updates (persistent lock-screen notifications), and phone tools in batch two; no watch or voice assistant.
- **Design Android for phones without Google services.**
  - Notifications only through a persistent connection: a foreground service stays on hub WebSocket and posts local notifications. Already in PR #84; no FCM.
  - No libraries requiring Google Play services. zxing for QR, system `LocationManager` for location, no Google-account Gmail login on Android.
  - Support mainstream Android versions and Chinese ROMs ([android.md](android.md) 5.18):
    - minSdk 31: about 91% coverage by StatCounter China pageviews, including APK-capable HarmonyOS 3 / 4. HarmonyOS 5+ cannot install APKs directly; no first-release commitment.
    - Vendors restrict background work and notifications by default (Xiaomi autostart off, HarmonyOS 4 local notifications silent/rate-limited, Honor Smart data saver cuts background mobile data…), with no APIs to query these switches. Guide each setting by ROM, then self-test with a locked-screen notification + hub-recorded disconnect intervals.
    - No Play Store: host APKs on hub, check updates in-app, show system installation confirmation.
  - Recommend two small hub changes: hub detects completion and sends it by WS instead of phone-side calculation, matching iPhone push policy; register phone tools per device so simultaneous iPhone/Android connections stop overwriting one another.
  - Also recommend durable idempotent receipts for messages/operations, idempotent Lark sending, client capability reports, unified errors, application-level ping.
- **Phones connect directly to hub over the public internet, IPv6 preferred (D9).**
  - hub opens a new TLS port, signs its certificate with its own key, includes the public-key fingerprint in the QR code, and clients pin it.
  - hub discovers usable addresses (public IPv6, DDNS name, LAN, verified reachable public IPv4) and distributes them. Phones probe concurrently with staggered starts; certificate and `hub_id` must both match.
  - QR format stays unchanged; cleartext port remains for LAN and existing iOS keeps working.
  - Home networks are the barrier: modem IPv6 inbound firewalls, changing residential prefixes, port blocking in some regions. App and hub can only diagnose and guide (`personad hub doctor`, app connection diagnostics). Strongly recommend a DDNS name for hub.
- **Execution order:**
  1. Set the specification: read iOS implementation/tests, write fixtures and token JSON; make hub changes such as public connectivity, preserving existing iOS at every step.
  2. Upgrade PR #84's Android toolchain, extract pure-Kotlin core and connect fixtures, then replace chat, interactions, notifications. Validate the three largest risks first: long-chat scrolling, persistent-connection power/survival, cellular public-IPv6 hub connectivity.
  3. Then split work into module lanes for parallel agents.
- **Section 12's questions were decided as recommended (2026-10-30).** Two inputs remain, neither blocks P0:
  - Does hub have a domain usable for DDNS? Without it, a residential prefix change while away can leave the phone unable to find hub.
  - Which physical devices are available (brand, OS version)? This determines where A1 first measures battery, survival, and carrier idle timeouts.

---

## 1. Background and goals

### 1.1 Current state

| | Persona iOS | Persona Android (PR #84, merged) | Reference iOS app |
| --- | --- | --- | --- |
| Size | About 42 thousand Swift lines (37.6 thousand production), one XcodeGen project, `Shared/` compiled as source into app, watch, and widget targets | About 2.7 thousand Kotlin lines, one `:app` module, Agents only | About 170 thousand Swift lines (90 thousand source + 66 thousand tests) + about 13 thousand Node host-component lines; one local SPM package, 39 targets |
| Backend | Own hub (Go + Postgres); phone connects only to hub, which starts nodes on computers through SSH | Same hub, server unchanged | No relay: phone connects directly to Lark gateway (HTTP + WS) and hosts (SSH exec + PTY) |
| Tests | 24 unit-test files, about 2.7 thousand lines; 16 UI tests + Python mock hub; about 80 DEBUG launch arguments | 10 JUnit tests passed locally; manual validation on emulator + one no-Google-services physical device | 173 test files, about 66 thousand lines, no UI tests; acceptance with synthetic services, isolated simulators, contract scripts |
| Strengths | Live push/pull synchronization, pushes and lock-screen Live Activities, multi-machine state model, chat presentation, terminal, headless assistant | Notifications without push services (foreground-service persistent connection), direct notification answers | Reliability engineering (three-state outcomes, idempotent receipts), compile-time layering, chat-list correctness, complete native interactions, review/acceptance methods |

### 1.2 Goals

1. **Android app**:
   - Core paths (follow progress, decide from the phone, handle Inbox, chat with Persona) match iOS quality, with polish worthy of a top Play Store app.
   - Work on mainstream Chinese phones without Google services.
   - Connect directly to hub over the public internet.
2. **hub changes**: support public direct connectivity and Android while existing iOS apps work without code changes.
3. **Later**: iOS adopts the same structure/interactions ([ios-refactor.md](ios-refactor.md)); both clients evolve together, with one specification per feature, independent implementations, and the same fixtures/parity table for acceptance.

### 1.3 Non-goals

- No server-architecture change: retain hub relay, not the reference iOS app's direct phone-to-SSH approach (phones do not handle private keys, host fingerprints, jump hosts).
- Keep “Do not build another Lark”: adopt none of the reference iOS app's Lark home, contacts, search, approvals, workplace, etc. (`docs/extensions/feishu.md`).
- No shared UI code (no Compose Multiplatform).
- First Android release has no Wear OS app or voice-assistant entry (alternate forms in [android.md](android.md) Section 6).

---

## 2. Research findings (summary)

See [research/](research/) for detailed evidence; every point below has a code location in the notes.

### 2.1 What to learn from the reference iOS app

| # | Borrow | Reference implementation | Persona application |
| --- | --- | --- | --- |
| 1 | **Compile-time-enforced layering** | Models/narrow protocols in `*Contracts`, implementations in `*Client`; `*Feature` depends only on Contracts, never Client. Only App sees implementations and pages and injects dependencies | Matching modules on both clients (Section 4); split `AppModel` and the 755-line `APIClient` |
| 2 | **Three-state outcomes + write-ahead log + read-only verification** | Any transport failure on a write means unknown outcome, without automatic replay; hosts persist idempotency with exclusively created receipt files + argument digests | Durable hub database receipts let clients retry automatically and safely (D3) |
| 3 | **Scopes + generations** | Credentials, caches, drafts, logs partition by connection → account → session, carrying generations; transport rejects requests predating reconnect, and version checked after await | Local data scoped by `hub_id + pairing generation`; uniform version guards in page models (D6) |
| 4 | **Bidirectional capability negotiation + tolerant decoding** | Host hello reports protocol/version and capabilities; client requests declare understood card types; a failed optional field is dropped alone | Clients report platform/version/capabilities too (D5), allowing hub degradation across coexisting iOS/Android versions |
| 5 | **Concurrency patterns** | Models own/cancel tasks; refreshes arriving while busy merge into one catch-up; pages share subscriptions; bounded event streams reread everything on overflow; two-step intent + confirmation token rejects expired dialogs | Shared page-model specification ([ux.md](ux.md) Section 6, [android.md](android.md) Section 4) |
| 6 | **Complete native interactions** | Denial reasons disable Allow; plan feedback disables direct approval; countdowns, MCP server attribution, MCP forms/links, sub-Agent attribution, per-request unsubmitted input, permission cards at history end, multiple-request summaries | Upgraded interaction panels ([ux.md](ux.md) 3.4). hub already supports denial reasons; the app has never sent them |
| 7 | **Reading tools** | In-session search including tool input/output, highlighted matches, one-step return to previous position; task-progress summary; fullscreen paginated long-text reader with search; collapsed tool steps still show N failures | Chat additions ([ux.md](ux.md) 3.3) |
| 8 | **Chat-list correctness** | Read anchor = (message ID, row offset); read status only while foreground, located, unobscured; sends carry client ID, confirmations match ID not text; after disconnect, fill both sides of anchor, deleting local rows only when server marks page complete | Shared by Lark chats (currently non-idempotent sends, possible refresh gaps, no reconnect refresh) and Agent chats ([ux.md](ux.md) 3.5) |
| 9 | **Consistent uncertainty presentation** | Fixed wording, icon, color for Result pending / Needs verification, with a read-only verification entry | New pending-state component family and copy glossary ([ux.md](ux.md) Section 5) |
| 10 | **Detailed initial connection** | Real connection stages instead of fake percentages; failures classified as DNS / refused / unreachable / timeout / authentication, each with actionable guidance | Pairing flow ([ux.md](ux.md) 3.7) |
| 11 | **Review and acceptance methods** | Interactive HTML prototype → review document → user approval → native implementation → acceptance record; graded passes (implemented / protocol / simulator / real account / physical device); fixed cross-language test vectors; source-generated API → UI mapping | Quality and acceptance in Section 9 |

### 2.2 What not to borrow

- **The entire direct phone-to-SSH stack**: private-key store, host fingerprints, jump hosts, host-component installer. Persona phones connect only to hub.
- **A second phone-side durable queue and manual-verification system**: host SQLite queues, background services, a dozen states per message. The reference iOS app needs these because it lacks a persistent relay; Persona delegates them to hub.
- **Polling-based observation**: session pages every second, host lists every 8 seconds. Persona already has WebSocket + rev incremental synchronization.
- **Handwritten log classes per operation**: the reference iOS app has 31 totaling 4,088 lines. Persona uses one generic operation log.
- **39 overly fine-grained targets**, and field-by-field defensive validation of untrusted upstreams. Persona trusts only its own hub; validation concentrates at hub boundaries and contract tests.
- **60-row VStack window rendering**: Persona's inverted list + independent lazy rows is better for performance and input following output.
- **Visual tokens**: the reference iOS app's design system is only about 300 lines, without font/spacing/radius tokens, weaker than Persona's.

### 2.3 Main current Persona iOS issues

| Issue | Evidence (details in [research/persona-ios-arch.md](research/persona-ios-arch.md)) |
| --- | --- |
| `AppModel` is a god object | 998 lines + 3 extension files, 22 responsibilities; cyclic `.shared` calls with `AgentSync` / `LarkSync` / `InboxStore` |
| Core logic in Views, without unit tests | `chatLines` folding is about 285 lines in a private `ChatView` method; Agents `rank` in `ChatsView`; Inbox grouping in `InboxView`; Lark read reporting in `LarkChatView` `@State` |
| Duplicate rules already disagree | 3 state orderings (code, watch, `docs/protocol/agent-states.md` conflict); 5 plan-approval entries use 3 modes (notifications/Live Activities acceptEdits, Siri/watch bypassPermissions); 3 deep-link routers; only 1 send path uses outbox, at least 5 call hub directly |
| No single protocol source | Go, Swift, `docs/protocol/protocol.md` are handwritten and drifted (app ignores hub's `assistant_schedule`; hub does not emit documented `session` / `ping` events) |
| Reliability gaps | node deduplicates `client_msg_id` only in memory for 10 minutes (`server/internal/node/sendsafe.go`); app treats **timeouts** as no network and resends (`SendSafety.swift`), potentially typing twice after hub/node redeployment; outbox/drafts in Caches, swallowed `try?` errors, no hub partitioning |
| Lark chat-page issues | Non-idempotent sends: hub generates random clientId each time (`server/internal/lark/lark.go`); refresh appends the newest page and loses messages if more than a page arrived between refreshes; open chats wait 60-second polling after reconnect; Inbox-opened chats lack a new-message divider and do not show cache first |
| Large files cause parallel-development conflicts | `ChatView.swift` changed in 38% of iOS commits in one week (93 of 243) |

### 2.4 Persona strengths the redesign must preserve

- hub push/pull synchronization + item continuity (`history` / `from_seq` / `to_seq`, self-healing gaps); see `docs/archive/integrations/msg-render-plan.md`.
- Pushes (approve permissions and plans directly in notifications), lock-screen Live Activities.
- Multiple machines + unified states: Needs you / Unread / Working / Waiting / Muted / Seen.
- Chat presentation: each message is its own segment, work rows expand in place into independent lazy rows, inverted lists align precisely to the bottom.
- Terminal: hub-relayed live streaming and replay, phone-size takeover, wheel forwarding.
- Headless assistant, knowledge base, AI Inbox triage.
- Screen cards: any TUI dialog becomes a native card, checking `rev` before sending keys.

### 2.5 Evaluation of PR #84 (first Android release)

Conclusion: **reorganize from it, not a rewrite**. Keep physically validated parts; replace structure, UI, and storage under this proposal.

| Better than the original draft; adopt directly | Replace under this proposal |
| --- | --- |
| Persistent foreground-service WebSocket, local notifications: most domestic phones lack Google services and cannot receive FCM; vendor push requires developer accounts | Toolchain/SDK levels (AGP 8.7 / Kotlin 2.0 / target 34 / minSdk 26) |
| zxing scanning without Google Play services | One module, global `Hub`, no ViewModel or DI |
| Cleartext HTTP allowed globally with `base-config`: network security config does not support subnets and hub address is known only at pairing | Plaintext pairing token in SharedPreferences; no cache/outbox |
| Direct question/screen-dialog choices in notifications, withdrawn after handling | One row per tool call, no work-row collapse or `fold_work` |
| Report Seen only in foreground | Custom Markdown without tables/code highlighting |
| `android.application_id` in `config.json`, `scripts/android.sh` | State colors (Working green, Unread blue, unlike iOS); notification Allow without unlock; phone calculates completion |

See [android.md](android.md) Section 1 for item comparison and reorganization.

> Research used master 77999ab, before PR #84 and about 20 other commits merged. iOS then added:
> - Work-process folding on long-session first pages (`fold_work`).
> - Demo mode (in-app fake hub).
> - iPhone Duo two columns.
> - Agents quick filters, compact list, collapsed spaces.
> - Persona tab header and sidebar.
> - Phone confirmation before the assistant writes to Lark.
>
> These were added to [parity.md](parity.md) Section 26; research-note line numbers refer to 77999ab.

---

## 3. Key decisions

### D1. Code sharing: two native clients + shared specification (recommended)

| Approach | Decision | Rationale |
| --- | --- | --- |
| **A. Native clients, shared specifications/fixtures** | **Adopt** | Best Android experience, direct platform APIs/performance tools; 42 thousand SwiftUI lines on iOS unaffected; simplest build chain, parallel agents do not block one another. Only about 5 thousand lines of pure logic require matching behavior, and the project already has one-specification + Go-simulation + fixtures precedent (`server/internal/transcript/continuity_corpus_test.go` reimplements app continuity rules) |
| B. KMP-shared core | Defer, but write Android core along its boundaries | Real benefits, but requires replacing iOS's tested, battle-hardened Swift data layer with Kotlin. Swift export still Alpha as of 2026-10; SKIE must catch up on every Kotlin upgrade; exported Kotlin types are not Sendable under Swift 6 strict concurrency; watch and Live Activity extensions must also link K/N frameworks despite extension memory limits |
| C. Share UI too with Compose Multiplatform | Reject | No official Liquid Glass; native iOS text input still experimental, sacrificing iOS feel to share UI |
| D. Move turn grouping / answer selection to hub | Reject | Seems to thin all three clients, but the actual answer is the longest segment across wakes (`Turn.pickAnswerAcrossWakes`); new content **retroactively changes** earlier item ownership, conflicting with append-only items / seq-incremental protocol |

Implementation rules:

- Create `spec/` at repository root:
  - `spec/fixtures/`: fixtures. Read iOS `chatLines`, `ChatStoreTests`, `RebuildTests`, and terminal recordings this round (read-only, no iOS changes), and write Android-side JSON cases. Export continuity rules from Go simulation (`continuity_corpus_test.go`).
  - `spec/tokens/`: design tokens.
  - `spec/README.md`: fixture location for every behavior rule.
- Only JUnit reads these JSON files this round; XCTest adopts the same files when iOS follows. Any rule change on either side starts with fixtures.
- Android `:core:contracts`, `:core:domain`, `:core:terminal` are pure Kotlin/JVM, depending only on kotlinx.serialization, coroutines, datetime.
- Reevaluate KMP once Swift export reaches Beta and Android has run one stable release.

### D2. Layers and modules (matching on both clients)

```
App (composition root, routing, HubScope container, DEBUG hooks)
 ├─ Feature: Chat · Agents · Assistant · Inbox · Terminal · Readers · Machines · Settings
 │     Depends only on ↓ Contracts / Core / DesignSystem (and a few reusable Features)
 ├─ Platform: push · Live Activities / Live Updates · phone tools · shortcuts · watch
 ├─ Data: secure storage · operation log (outbox / drafts) · cache · sync engine
 ├─ HubClient: domain-specific HTTP clients · WebSocket · connection monitor · error classification
 ├─ DesignSystem: generated tokens + components
 ├─ Core: pure logic (continuity, turn folding, sorting/grouping, approval policy, tag parsing, screen dialogs, formatting)
 ├─ TerminalCore: VT-subset grid · wrapping · key encoding · frame ordering / takeover state machine
 └─ Contracts: wire types · events · capabilities · error codes · narrow interfaces (protocol / interface)
```

- Fixed dependency direction: `App → Feature → Contracts ← HubClient / Data`. Features directly using HubClient implementations fail compilation.
- **Only App sees both implementations and pages**, constructing and injecting at the composition root: `AppContainer` on iOS, Hilt or a handwritten root on Android.
- `HubScope(hub_id, pairing generation)` owns its clients, storage, child models, and long-running tasks. Re-pairing discards the whole scope.
- About 18 Android Gradle modules; later iOS uses about 16 same-name targets in a local SPM package, mapped in Section 4. Far fewer than the reference iOS app's 39, but enough for each Feature to have its own namespace. Modules prevent parallel agents colliding on names or editing the same large file.

### D3. Reliability: three-state outcomes, heavy work on hub

| Write category | Examples | Guarantee |
| --- | --- | --- |
| **Messages** | Agent / assistant messages, Lark replies, Inbox replies, email sending, AI twin draft sending | Client generates `client_msg_id` and **writes local outbox before clearing input**. hub persists in Postgres `client_ops` (ID, device, kind, target, argument digest, state, result). Same ID/arguments returns original result; same ID/different arguments rejected. Offline queue sends automatically in order on reconnect |
| **One-shot operations** | Answer interaction, interrupt, rename, close Agent, change mode, stop loop, `/clear` | Carry `client_op_id`, likewise persisted/deduplicated by hub. **Do not queue offline** (computer state may have changed; existing rule). On unknown outcome, reread authoritative state (interaction list, session state) or query `GET /api/v1/ops/{id}`; card shows “Result pending,” not an error |
| **Settings** | Pinning, muting, notification preferences, schedules | Whole-table PUT, last write wins, optimistic updates with rollback on failure; no operation log needed |

How clients classify outcomes (one shared table, [ux.md](ux.md) 5.2):

- **Request never sent** (offline, DNS failure, connection refused) → automatically resend when network recovers.
- **Sent without a response** (timeout, interrupted connection) → safe automatic retry with the same ID when hub supports `client_ops`. Old hubs show “Result pending” and check chat history for the message. This fixes the current timeout-as-offline blind resend.
- **Definite hub rejection** (409 `input_draft` / `dialog_open` / `op_id_reused`, etc.) → show reason and available actions (such as “Clear and send”).
- **Failure midway through typing on a computer** (502 `delivered: unknown`) → “Needs verification”: automatically search chat history for the message; a match means delivered. After the time limit without a match, ask the user to “Resend / Don't send.”

Optional (P2): all Agent-targeted writes carry a **target-instance fingerprint** (machine + pane + process-start identity + session ID), checked by node. A mismatch returns definite `stale_session`, then the app asks “The session reopened elsewhere. Send to the new process?” This mirrors reference-app process binding but is less restrictive; that app freezes everything when the process changes.

### D4. Synchronization: keep the protocol, specify the rules

- Agent chats retain `history` / `from_seq` / `to_seq` / `covered` continuity rules (`docs/protocol/protocol.md`, “Item continuity”). Put rules in `spec/fixtures/continuity/*.json`, shared by iOS `ChatStore.apply`, Android reducer, and Go simulation.
- Reference-count subscriptions; limit hole-filling to 8 pages before switching to the latest page; refetch pending interactions after reconnect; background synchronization fetches only changed-rev sessions (`AgentSync`). Keep these rules unchanged and document them in the specification.
- Add gap detection to Lark chats:
  - hub passes upstream `syncState`, message `clientId`, and `localState` through.
  - App refresh reads forward from the last message (`after`); if it cannot catch up, replace the entire screen with the latest page and explain.
  - Delete local rows only when upstream marks that page complete.
  - Notify open chat pages to verify once after reconnect.
- Internal app event dispatch uses a bounded buffer; overflow emits “Full verification required,” never silently drops events (the reference app's approach).

### D5. Capability negotiation and compatibility

- **hub → client**: retain `features`, delivered in `/health` and WS `hello`.
- **Client → hub**: add two places. Send `{"type":"client_hello","platform":"android|ios|watch","version","build","features":[…]}` after WebSocket connection; HTTP includes `X-Persona-Client: <platform>/<version>`. hub uses them to:
  - Route pushes and phone tools by platform.
  - Downgrade unfamiliar new interaction types to screen cards.
  - Log for diagnostics.
- **Unified error body**: `{"error": "Human-readable message", "code": "Machine-readable code", "retryable": bool, "delivered": "none|unknown|yes"}`. Keep existing `{error}` and flat 409 fields for old apps. Clients use only `code`, no wording matching; currently `APIError.isTimeoutText` infers timeouts from text.
- **Tolerant decoding**: same rules on both clients.
  - Unknown enum values become unknown.
  - Failed optional enhancement fields are dropped individually.
  - Unknown WS events are ignored.
  - Dates before 2000 become empty; Go zero time is `0001-01-01`.

### D6. Local data and scopes

| Category | iOS | Android | Rule |
| --- | --- | --- | --- |
| Secrets (device tokens) | Keychain (`WhenUnlockedThisDeviceOnly`) | Keystore-generated key + Tink encryption (no deprecated `security-crypto`) | Clear on unpairing |
| Durable data (outbox, operation logs, drafts, read positions, navigation) | Application Support | `filesDir` (Room or atomic files) | Versioned envelope: newer reads older, older refuses to overwrite newer; **unreadable data preserves the original file, stops writes, and warns—never silently replace with empty data** |
| Caches (list snapshots, last two chat turns, images, web pages) | Caches | `cacheDir` | Disposable anytime; clear from storage settings |

- All paths under `<hub_id>/<pairing generation>/`. hub generates an instance UUID on first run, sent through `/health` and `hello`. Changing hub or re-pairing creates a new scope, clearing the old one entirely; currently re-pairing does not clear caches.
- (When iOS follows later) **Do not rename** cache directories, UserDefaults keys, or DEBUG launch arguments during iOS refactoring. If needed, use a new name and read the old format for one release, or the first launch after upgrade appears empty.

### D7. Design system: one token source, generated for both clients

- Single source: `spec/tokens/*.json`, close to the W3C design-token community format. Includes:
  - Semantic colors by style × light/dark.
  - State colors (dot / text / tint).
  - Radii, spacing, font roles, motion.
  - **Contrast rules**: machine-checkable readability thresholds from `docs/apple/ios.md`.
- `scripts/assets/gen_tokens.py` generates the following; `--check` regenerates/compares and checks contrast for every style × light/dark, integrated into pre-commit:
  - iOS: `Tokens.swift` (only when iOS follows), pure-data types `nonisolated`.
  - Android: `Tokens.kt`.
  - Prototype CSS.
  - Documentation palette tables.
- **Enforce adoption**: grep allowlists forbid colors, radius literals, and fixed font sizes outside token files; use current counts as a decrease-only baseline. Currently iOS views use `Theme.Font` and `Theme.Space` only once each, with 70 radius literals remaining.
- **Android styles**: each platform defaults to its native materials.
  - iOS defaults to Liquid Glass; Android to Native (Material You dynamic colors).
  - Claude / Clay / Island / Lark are four shared token combinations, added to Android in batch two.
  - Do not imitate Liquid Glass on Android: it requires AGSL shaders, drops frames on low-end devices, and is not Android's design language.
- Shared accessibility component rules:
  - At accessibility sizes, horizontal layouts become vertical, decorative icons disappear, line limits relax.
  - Reduce Motion stops all animations.
  - Count badges may abbreviate visually, but screen readers read the full number.
  - Minimum tap targets: iOS 44pt, Android 48dp.

### D8. Android notifications: persistent connection only, no Google services

Under the chosen premise, phones lack Google Play services and cannot receive FCM; vendor pushes require developer accounts and identity verification. Android therefore has one delivery path: **persistent connection** (already PR #84, [android.md](android.md) 5.8).

- **Mechanism**: a foreground service (`specialUse`) stays connected to hub WebSocket and posts notifications locally on events. Phone-tool calls arrive directly, without wake-up.
- **hub changes**: works unchanged. Recommend hub-detected completion sent by WS ([hub-changes.md](hub-changes.md) 3.9); negotiate background heartbeat intervals per connection for battery savings ([hub-changes.md](hub-changes.md) 1.6).
- **Costs**:
  - One persistent notification.
  - Battery use **not measured yet**; measure in A1.
  - Chinese ROMs require manual autostart/battery exemptions, guided step by step in readiness checklist.
- **Notification rules**:
  - App assembles notifications. Allow / Approve and question/screen-dialog choice buttons require unlock (`setAuthenticationRequired`); Deny / Send back do not.
  - `RemoteInput` accepts denial reasons or plan feedback directly.
  - Single-choice questions and screen-dialog options become buttons.
  - Withdraw notification after interaction is handled.
  - No notification while viewing that session in foreground.
- **Notification preferences**: per-device preferences (`/devices/{id}/prefs`) apply as usual.
- **Lock-screen summary**: persistent-connection notification also summarizes Needs you / Working / Unread. Request Live Update promotion while active on Android 16+.

### D9. Public direct phone-to-hub connectivity, IPv6 preferred

Under the chosen premise, phones away from home (cellular, others' Wi-Fi) must connect directly to the home hub without relying on VPNs. Users configure their own VPNs; the app does not. See [research/public-direct-ipv6.md](research/public-direct-ipv6.md) (China IPv6 status, address discovery, TLS, hardening, keepalive, with sources) and [research/hub-network-current.md](research/hub-network-current.md) (current hub networking code, with line numbers).

- **Why feasible**:
  - IPv6 is widespread on domestic mobile networks (70.85% of mobile traffic at end of 2025). Carriers block inbound phone IPv6, not outbound connections.
  - Most residential broadband gets global IPv6; hub needs only one stable address.
  - Many homes lack public IPv4 (carrier NAT); offer it only after confirming inbound reachability.
- **The barrier is at home**; app and hub can only diagnose and guide:
  - Modem/router IPv6 inbound firewall enabled by default, often with only on/off settings.
  - Prefix changes, more than twice daily in some regions.
  - Ports such as 80 / 443 blocked in some regions.
- **Approach** ([hub-changes.md](hub-changes.md) Section 1, [android.md](android.md) 5.1):
  1. Open a new TLS hub port, keeping cleartext unchanged for LAN and existing iOS. Sign certificates with hub's own key, put the public-key fingerprint in the QR, and pin it on phones. A public-CA certificate can replace it when a domain is available, still pinning the public key.
  2. hub discovers usable public IPv6, DDNS names, LAN addresses, verified public IPv4. Send after authentication and push changes.
  3. Phones probe with staggered concurrency in order public IPv6 → domain → LAN → public IPv4. **Certificate and `hub_id` must both match**, since an old prefix may belong to someone else now. Remember winners per network.
  4. Harden: reject public-source cleartext; rate-limit authentication failures by IPv6 /64; QR uses short-lived temporary tokens, not the permanently valid master token.
  5. Explain connection failures through `personad hub doctor` and app diagnostics.
- **Recommend a DDNS name**: residential prefix changes immediately drop old connections. Android has no FCM side channel; away from home it cannot receive the new address and must recover through a domain. No domain means degraded operation.
- **hub initiates background heartbeats**: Android deep sleep stops client pings, so only hub can initiate them, with per-connection interval negotiation. Background starts at 4 minutes and adapts by network; foreground uses 20 seconds plus application ping ([hub-changes.md](hub-changes.md) 1.6).
- **Existing iOS compatibility**:
  - QR format unchanged, new information in extra parameters.
  - Cleartext port retained.
  - Connections without heartbeat negotiation retain 20 seconds.
  - New `hello` fields do not collide with iOS-decoded fields (checked).
  - Only behavior change: reject public-source cleartext. Documentation has always required LAN / VPN, so normal use is unaffected; `allow_public_plaintext` can restore old behavior.
- **Android 17 local-network permission is unavoidable**: on home Wi-Fi, LAN addresses and hub public IPv6 in the same /64 both count as local, requiring `ACCESS_LOCAL_NETWORK`. At target 37, request it in pairing/connection flows and distinguish blocked permission from unreachable hub ([android.md](android.md) 5.1).
- **iOS later**: parse new QR parameters, add certificate pinning and multi-address probes; background stays on APNs, letting hub send new addresses after changes. Download Lark video before playing because AVPlayer cannot use pinning ([ios-refactor.md](ios-refactor.md) R5).

---

## 4. Target architecture

### 4.1 Module mapping (Android ↔ future iOS)

| Responsibility | iOS (local SPM package `ios/PersonaKit`) | Android (Gradle) | Contents |
| --- | --- | --- | --- |
| Contracts | `PersonaContracts` | `:core:contracts` (pure Kotlin) | Wire types (Codable / `@Serializable`, explicit field names), WS events, capabilities, error codes, narrow interfaces (`SessionsReading`, `MessageSending`, `InteractionResolving`…) |
| Pure logic | `PersonaCore` | `:core:domain` (pure Kotlin) | Continuity reducer; items → Rows → Blocks → Turn → Lines folding; frozen snapshots; outbox state machine; sorting/grouping/companions/Inbox selectors; approval-policy table; message-tag parsing; screen-dialog parsing; formatting; Fleet / shortcut copy |
| Terminal core | `TerminalCore` | `:core:terminal` (pure Kotlin) | VT-subset grid, wide characters, wrapping, scroll detection, key names → bytes, modifier state machine, frame ordering/takeover state machine |
| Networking | `HubClient` | `:core:hub` | Domain-specific HTTP clients (sessions, interactions, machines, files, Inbox, Lark, mail, assistant, knowledge, devices), WebSocket, connection monitor, error classification |
| Data | `PersonaData` | `:core:data` | Secure storage, generic operation log (outbox / op / drafts), caches, media caches, sync engines (session subscriptions, AgentSync, LarkSync, InboxSync), `HubScope` |
| Design system | `DesignSystem` | `:core:designsystem` | Generated tokens, components (Section 5) |
| Chat | `ChatFeature` | `:feature:chat` | List engine, message/work rows, interaction panels, composer, search, task progress; shared by Agents and assistant |
| Agents | `AgentsFeature` | `:feature:agents` | List, spaces, machines, new sessions, request Inbox |
| Assistant | `AssistantFeature` | `:feature:assistant` | Home, main chat/history, knowledge, profile/schedules/memory |
| Inbox | `InboxFeature` | `:feature:inbox` | Unified list, Lark chats/cards, email |
| Terminal | `TerminalFeature` | `:feature:terminal` | Terminal view, key bar, gestures, settings |
| Readers | `ReadersFeature` | `:feature:readers` | Link overlay + WebView pool, Lark docs, file viewer, image viewer/editor, full-text reader |
| Machines | `MachinesFeature` | `:feature:machines` | Details, monitoring dashboard, charts |
| Settings | `SettingsFeature` | `:feature:settings` | Pairing/onboarding, devices, notifications, phone data, storage, styles |
| Platform | `PlatformKit` | `:platform:notify` (foreground service, notifications, Live Updates), `:platform:phonetools`, `:platform:shortcuts` | Push registration/actions, Live Activities / Live Updates, phone tools, shortcuts/share target, haptics |
| Composition root | App target (`AppContainer`, `Router`, `DebugHooks`) | `:app` (DI, NavHost, debug entries in `src/debug`) | Only place knowing every implementation |
| Watch/extensions | Watch and Widgets targets link only `PersonaContracts` + `PersonaCore` + required HubClient/tokens | — | Stop compiling the entire `Shared/` source into them |

### 4.2 What replaces `AppModel`

| New service | Responsibilities taken over (current `AppModel` category numbers) |
| --- | --- |
| `SessionDirectory` | Machines, sessions, indexes, pin/mute, selectors (4, 16, 17) |
| `ChatRepository` | `ChatStore` registry/LRU, subscription references, gap-fill scheduling, history pagination (8, 9, 10) |
| `InteractionCenter` | Pending interactions, resolve, **approval-policy table** (one table defining modes across all 5 entry points) (13) |
| `MessageSender` | **Single send use case**: every entry (chat, assistant first message, Hand to Persona, Lark, Inbox, Siri, watch) uses it for unified idempotency, network waiting, rejection handling (14) |
| `TerminalStreams` | Terminal-stream routing, hub capabilities (11) |
| `AssistantService` | Assistant settings, main chat, prewarming, launcher switching (7) |
| `PhoneToolHost` | Phone-tool registration/execution, confirmation dialogs (12) |
| `ConnectionMonitor` | WS lifecycle, connection state, catch-up (2, 3) |
| `Router` | Tabs, open session, deep links; consolidate `AppModel.open`, `InboxView.open`, `OpenSessionIntent` (5) |
| `Feedback` | Toasts, haptics, error-presentation policy (6, 22) |
| `PairingService` | Pairing, credentials, device-token exchange, `HubScope` switching (1) |

Page-model specification:

- iOS: `@MainActor @Observable` models with actor services.
- Android: `ViewModel` + `StateFlow`, shared mutable state guarded by `Mutex` or a single-thread dispatcher.

Both follow:

- Record version before await; check afterward and discard stale results.
- Models own and cancel their tasks.
- Refreshes arriving while busy merge into one catch-up read.
- Multiple pages share one subscription.
- Bounded event streams reread everything on overflow.

---

## 5. Interaction and UI redesign (summary)

Full specification in [ux.md](ux.md). Principles: **keep information architecture, improve reading and decisions, explain uncertainty, one presentation per concept**.

| Area | Main changes | Priority |
| --- | --- | --- |
| Global | Keep four tabs. Add pending-state components (Sending / Send when connected / Result pending / Needs verification / Not applied), unified copy glossary | P0 |
| Agents list | **Draft previews** and **verification indicators** on rows. Upgrade Needs your attention to a **cross-session request Inbox** (remaining time per item, opens full interaction page directly). Add History scope for non-running sessions by project: open read-only, restore only after Continue this session | P1 |
| Chat | **One status row** above input: definite state left, live-spinner text right. Needs verification and Syncing are formal states; header contains identity only | P0 |
| Chat | Work rows **show failure counts when collapsed**; sticky Collapse bar when expanded; Previous / Next change in Edit diffs; consistent Raw input / Raw output / Copy on tool cards | P1 |
| Chat | **In-session search** (body, thinking, tool input/output, system events), highlights with Return to previous position; **task progress** strip/fullscreen page; **full-text reader** (pagination, search, full copy) | P1 |
| Interactions | Regular permission cards at **history end**, complex requests open a full page; multiple requests in Inbox; **countdowns**, **denial reasons** disabling Allow, **plan feedback** mutually exclusive with approval, risk explanation for Approve and execute automatically; MCP server attribution; **MCP forms/links**; sub-Agent attribution; save unsubmitted input per request | P0 (denial reasons, exclusion, countdowns) / P1 (rest) |
| Drafts | Persist attachments and drafts with versions; compare revision before a receipt clears a draft; list preview “Draft · …” | P1 |
| Lark chats | Same inverted-list engine as chat; read anchors; Inbox-entry divider “This is where this item started”; guarded read reporting; Jump to latest counts only loaded messages actually below, showing N+ when more remain unloaded; outbox with clientId, ID-only confirmation; durable drafts; gap detection | P0 (idempotent sends, gaps) / P1 (rest) |
| Pairing/connection | After QR, **real staged progress** (parse → probe addresses → verify certificate/hub → exchange device token → read machines); classified failures (no address / refused / timeout / certificate mismatch / local-network permission / old hub). Explain what pairing leaves on phone/hub. Post-pair readiness checklist (computer availability, versions, notification permissions, battery/autostart), skippable items with consequences. New **connection diagnostics** (route, per-address probe results, certificate fingerprint, heartbeat interval) | P0 |
| Design system | Token generation, component catalog (both clients, direct DEBUG entry), accessibility rules | P0 |

---

## 6. Android proposal (summary)

Full proposal in [android.md](android.md).

- **Starting point**: reorganize PR #84 with keep / move / replace lists (android.md 1.3), keeping the app usable at every step.
- **Stack**:
  - Toolchain: Kotlin 2.4 + AGP 9.5 + Gradle 9.8; compileSdk 37; targetSdk initially 36, then 37 after local-network permission in pairing.
  - minSdk 31 (Android 12): lock-screen Allow / Approve can require unlock; about 91% China StatCounter pageview coverage, including APK-capable HarmonyOS 3 / 4 (API 31 compatibility layer).
  - UI/architecture: Jetpack Compose (BOM 2026.09) + Material 3; `ViewModel` + `StateFlow`; Hilt; Navigation 3.
  - Network/storage: OkHttp 5 (HTTP + WebSocket, custom TrustManager public-key pinning for hub client) + kotlinx.serialization; Room 2.8 + DataStore; Tink-encrypted tokens.
  - Media: Coil 3; no player library. Lark video/voice messages are cards opening in Lark. mikepenz Markdown renderer; zxing QR scanning from PR #84.
  - Notifications: foreground-service persistent connection, no FCM; tests: Roborazzi.
  - No libraries depending on Google Play services (`com.google.android.gms:*`, Firebase).
  - Exact versions and rationale in android.md Section 2.
- **Challenges and solutions**:
  - **Chat list**: `LazyColumn(reverseLayout = true)` is naturally inverted, avoiding most iOS workarounds for inversion, safe areas, and lazy height estimation. Still implement one row per step, stable keys, frozen snapshots, entry positioning; use `fold_work` for long sessions.
  - **Terminal**: port iOS's custom VT grid, wrapping, key encoding to pure Kotlin with real recorded-frame fixtures. Canvas rendering, custom `InputConnection` for input methods. This is Android's largest rewrite, in batch two; until then keep PR #84's once-per-second screen-reading terminal window.
  - **Markdown**: mikepenz supports tables and replacing nodes with tool cards, quote cards, path tags; adopt iOS's lightweight code-highlighting rules. Compose `SelectionContainer` selects across paragraphs in place, unlike iOS's plain-text-sheet selection limitation.
  - **Link overlay**: custom same-window overlay with full / 60% docking; chat stays usable at 60%. Keep 2–3 WebViews pooled (Android WebView uses more memory).
- **Different platform forms**:
  - APNs → persistent connection (D8).
  - Dynamic Island / lock-screen Live Activities → persistent notification; Android 17 MetricStyle for three metrics, Live Update promotion while active on Android 16+.
  - Watch → not in first release; notifications/actions bridge automatically to Wear OS.
  - Siri → Quick Settings tiles, launcher shortcuts, Share to Persona.
  - HealthKit → Health Connect: requires Google Play services; China-market health apps do not write data there, so first release does not register health tools on those phones.
  - Reminders → no Android system API; remove these 3 tools or replace with Google Tasks on hub.
  - Clipboard → foreground-only reads.
  - Google-account Gmail login → not on Android: login API requires Google Play services. Mail accounts live on hub and are shared; add once on iPhone, or use app-password IMAP.
- **Android-specific opportunities**:
  - Share to Persona: send text, links, images, files from any app to main chat, low cost and aligned with gathering information. Recommend first release.
  - Two-column list/chat on tablets and foldables (matching iPhone Duo).
  - SMS, call history, notification listener: too sensitive; do not implement (Section 12 question 10).
- **Two platform restrictions**:
  - Android 17 (target 37) requires runtime `ACCESS_LOCAL_NETWORK`. On home Wi-Fi, LAN addresses and hub public IPv6 in the same /64 count as local; switching to public IPv6 does not avoid it. Without permission, TCP **times out**, looking like unreachable hub. Stay at target 36 until pairing/connection permission flows are ready; physically test VPN behavior.
  - Android 9+ defaults to blocking cleartext HTTP. Network security config is static XML without subnet support, so allow globally (PR #84), then enforce in-app: `http://` only for LAN, VPN subnets, loopback; **all public addresses use TLS with public-key pinning**.

---

## 7. iOS refactoring roadmap (not this round)

iOS stays read-only this round. R0–R5 in [ios-refactor.md](ios-refactor.md) remain a later reference:
- Extract pure logic.
- Create local packages.
- Split `APIClient` and synchronizers.
- Split `AppModel`.
- Split `ChatView`.
- Implement new interactions and cleanup.

iOS will directly adopt the fixtures/tokens written on the Android side this round.

---

## 8. hub changes (summary)

Full list in [hub-changes.md](hub-changes.md). On LAN, Android basics (Agents, interactions, persistent-connection notifications) **work without hub changes**, as PR #84 did. The changes below support public direct connectivity and consistent, reliable behavior. **Requirement: existing iOS apps work without code changes** (retain LAN cleartext, old QR format, single addresses).

| Level | Changes |
| --- | --- |
| **Required** (public connectivity, D9) | New TLS port (hub-signed certificate, public-key fingerprint, renewal with same key); address discovery/distribution (`/endpoints`, new `hello` fields, `endpoints` event); short-lived temporary QR token (format unchanged); reject public cleartext, /64 authentication-failure limiting; `hub_id`; per-connection heartbeat negotiation; `personad hub doctor`; optional recommended DDNS; new `docs/operations/network.md` |
| **Required** (first Android release) | Per-device phone-tool registration/routing: current `register_phone` overwrites the whole table and calls the latest device, making iPhone/Android overwrite each other; hub-hosted APK and in-app update checks; per-device connected/disconnected intervals (background self-tests and overnight measurements) |
| **Recommended** (with first Android release) | hub-detected completion/other notifications by WS to persistent devices (`notify`), replacing phone calculation; application ping/pong for weak-network detection; `client_hello`, `X-Persona-Client`; unified errors |
| **Recommended** (reliability, P3 with both clients' outboxes) | `client_ops` + `GET /api/v1/ops/{id}`, durable message/one-shot deduplication; Lark sends pass clientId, messages carry `clientId` / `localState`, pages carry `syncState` |
| **Optional** (other) | Replace hardcoded iPhone text with phone; withdraw iOS notifications after interaction handling; complete omitted APIs/events in `docs/protocol/protocol.md`; move client prompt templates to hub |

---

## 9. Quality and acceptance

- **Test layers** (corresponding clients):

  | Layer | iOS | Android | Shared assets |
  | --- | --- | --- | --- |
  | Pure logic | Swift Testing in SPM, no app launch | JUnit on JVM | `spec/fixtures` |
  | Protocol | Decode Go-exported samples | Same | Go-generated `spec/fixtures/wire` |
  | Page models | Fakes gated by continuations for deterministic late results/cancellation/scope switching | coroutines-test + `CompletableDeferred` gates | Scenario list |
  | Component snapshots | Catalog × light/dark × text size × narrow screen | Roborazzi JVM rendering | Synthetic privacy-safe data |
  | UI flows | XCUITest + `scripts/mock/mock_hub.py` | Compose UI tests; instrumented tests use `adb reverse` to the same mock | Mock/fault switches; Go validates mock response formats against real hub; add Executed but receipt lost switch |
  | Real flow | Local test hub (fake HOME, independent database) | Same | `docs/operations/testing.md` process |

- **Graded acceptance**: [parity.md](parity.md) has an iOS and Android column per feature (only Android updated this round; iOS records current state), with Implemented → Unit tests passed → Mock screenshots passed (light/dark × text sizes) → Test hub passed → Physical device passed. Reuse the reference iOS app's page checklist plus three checks: cross-client consistency, single-line buttons, links inside the app.
- **Screenshots**: direct debug entry, then capture on both clients. iOS launch arguments; Android `adb shell am start` extras, code in debug source only. Release packages must contain no debug-switch strings: iOS `strings`, Android APK scan.
- **Performance**: every documented number must be measured with at least 10 samples, reporting min / p50 / p90 / max / mean. Initial baselines:
  - Cold-start opening of large sessions.
  - Dropped frames scrolling long chats.
  - hub-to-phone notification latency.
  - End-to-end phone-tool wake/execution time.
- **Privacy**: `scripts/privacy_check.py` covers `android/` and `spec/`; fixtures generated by allowlist-based redaction scripts, manually reviewed before commit.
- **HTML prototypes before new interactions**: `prototypes/<topic>/`, generated token CSS, iPhone 393 and Android about 360dp frames, covering light/dark, narrow screens, Reduce Motion, errors. Native clients only after user approval. First prototypes: status row, full interaction/request Inbox, in-session search, pairing progress.

---

## 10. Milestones and parallel work

| Phase | Contents | Completion criteria |
| --- | --- | --- |
| **P0 Specification** (first) | Read iOS to write `spec/fixtures` and token JSON. hub: public connectivity (TLS, multiple addresses, short-lived QR tokens, rate limits, heartbeat negotiation, `hub doctor`), `client_hello`, errors, `hub_id`, app ping. **Android A0**: upgrade PR #84 toolchain (target initially 36), extract pure-Kotlin core and move its continuity/models/row merging into it with fixtures; first HTML prototypes | All Android fixtures pass; PR #84 physical flows (pair, send, notification answers) still work; **unchanged iOS app works with new hub** (run iOS UI tests and physical pairing) |
| **A1 Minimum usable Android slice** | Hilt, Navigation 3, encrypted storage, Room; public connectivity (multi-address probes, IPv6, certificate pinning); chat shared list engine + `chatLines` + `fold_work` + status row + new interactions; unlock for notification Allow / Approve, denial reasons; iOS-matching state colors; direct debug entries | Measured opening/scrolling a 650-command session (at least 10 samples); measured hourly power and locked-screen survival on mainstream Chinese ROMs; measured cellular idle timeouts for three carriers to set heartbeats; cellular public-IPv6 hub connection; physical notification → approve → Agent resumes |
| **A2 Complete first Android release** | Four tabs (Persona, full Agents, Inbox, Settings), connection state, staged pairing progress, offline outbox, Share to Persona, Chinese-ROM readiness checklist; hub `notify` and per-device phone-tool routing | First-release P0 features at least Test hub passed in parity.md; usable daily by self/testers |
| **A3 New Android features / batch two** | Section 5 interaction improvements; hub reliability (`client_ops`, idempotent Lark); terminal, Live Updates, phone tools, knowledge, files/Git, link overlays, Lark docs/cards, machine monitoring, other styles, demo mode | Item-by-item parity.md |
| (Later) iOS follows | [ios-refactor.md](ios-refactor.md) | Not this round |

Parallel-work rules (from CLAUDE.md lessons):

- Module lanes, each with an independent worktree and simulator; one separate AVD per Android lane.
- Handoffs only through `spec/` and `Contracts`. Merge `spec/` changes first, then others rebase.
- Android A0 touches all PR #84 files; other lanes leave `android/` alone during it.
- **Nobody changes `ios/`**; state this explicitly in subagent prompts.
- Before push, check `git diff origin/master --stat` contains only your files.

---

## 11. Risks

| Risk | Impact | Mitigation |
| --- | --- | --- |
| Persistent-connection power/survival (strict Chinese-ROM background policies; PR #84 has not tested long locked-screen connections physically) | Delayed notifications or high power use | A1 physical hourly power/locked survival; hub-sent network-negotiated heartbeats; readiness checklist for autostart/battery limits |
| Android 17 local-network permission | Target 37 without handling stays Connecting on home Wi-Fi (LAN or same-/64 public IPv6) | Stay target 36 until permission flows; show Local network permission required with entry after denial; physically test VPN |
| Modem/router IPv6 inbound firewall cannot disable selectively | No public connectivity, or entire home network exposed | Bridge mode and own router allowing only hub address/port; hub-machine firewall; `hub doctor` |
| Residential prefix changes several times daily without a domain | Phone cannot recover hub while away | Recommend DDNS; recover via pushed new addresses → domain → LAN; diagnostic guidance |
| Region-specific carrier port blocking | Unreachable ports for some users | Configurable ports, avoid 80 / 443 / 8080 / 8443; clear `hub doctor` diagnostics |
| 4G/5G NAT/firewall idle timeout below 4 minutes (domestic data only from about 2015 2G/3G) | Frequent background disconnects, reconnect power cost | Adaptive shorter heartbeats per network; A1 three-carrier measurements |
| TUN proxy on hub machine | Inbound IPv6 responses routed into proxy, silently failing public connections | `hub doctor`; exclusion rules in `docs/operations/network.md` |
| Lost hub identity key | Every device must re-pair by QR | Key in backup instructions; `hello` may announce next key fingerprint |
| HarmonyOS 5+ cannot install APKs | Some Huawei users cannot use it | HarmonyOS 4.x/earlier works (API 31); physically test Droitong persistent notifications, no commitment |
| Vendor background/notification restrictions default on, no query API | Notifications stop shortly after locking when setup is wrong | Version/ROM-specific checklist; wait for next exemption on `startForeground` failure, disabled battery optimization itself is exempt; locked test notification + hub downtime self-tests |
| HarmonyOS 4 local notifications default to Information/marketing, silent, 2–5 daily | Needs you alerts neither sound nor wake screen | Disable Silent notifications, enable banners/lock screen; counts in persistent notification; test whether it consumes quota |
| Process alive but cannot receive (Honor Smart data saver, Huawei sleep-time connectivity, vendor freezing) | No notifications, restored on app open, mistaken for hub failure | Name those checklist settings; downtime diagnostics; 8-hour locked tests, at least 10 nights |
| Android/iOS behavior mismatch | Same chat folds differently | Fixtures from read-only iOS implementation/tests; compare same-session simulator screenshots; iOS later adopts fixtures |
| hub changes break existing iOS | iPhone cannot connect/pair | Backward compatibility: cleartext for LAN/VPN, `hub pair --legacy` old QR, optional new fields; unchanged-iOS UI tests and physical pairing after every hub change |
| Long-chat Compose performance | Hundreds of tool steps stutter | A1 early validation/dropped-frame measurements; one row per step, stable keys, `contentType`, ID-cached derived data |
| Android terminal input methods/gestures | Pinyin, modifiers, selection worse than iOS | Batch two; port VT core/recorded frames first; separate input prototype |
| Full Live Updates only Android 16+, extra vendor restrictions | Lock-screen summary weaker than Dynamic Island | Ordinary persistent-notification fallback; Needs you uses independent high-priority alerts |
| No Google-account Gmail login on Android (Play-services dependency) | Android-only users cannot add Gmail Google login | Shared hub accounts: add on iPhone once or app-password IMAP |
| Unstable inference gateway (8 agents simultaneously hit 503 during research) | Parallel-agent interruptions | Resumable lane tasks; save notes/output incrementally |

---

## 12. Decisions on open questions

Decided 2026-10-30: follow all recommendations; DDNS domain and physical test-device details will be supplied separately.

| # | Question | Decision |
| --- | --- | --- |
| 1 | Does hub have a domain for DDNS (Cloudflare, Alibaba Cloud DNS, DNSPod)? | Recommended: configure it if available, hub updates records automatically; wildcard certificate names keep specific names out of certificate-transparency scans. Otherwise start with pushed new addresses + LAN fallback and explain in diagnostics. **Awaiting domain availability** |
| 2 | Physical test devices | Minimum recommended matrix: existing Xiaomi (HyperOS 3 / Android 16), Huawei HarmonyOS 4.x, OPPO ColorOS 16 or 17; vendor cloud devices first for vivo/Honor; three carrier SIMs. **Awaiting available models** |
| 3 | Simultaneous iPhone/Android use? | Design for yes: per-device phone-tool routing, completion alerts to both |
| 4 | Distribution | Self-signed APK on hub, in-app updates with system confirmation; signing certificate configured outside repository ([android.md](android.md) Section 9) |
| 5 | How to supersede PR #84? | Reorganize its code via three lists (android.md 1.3), no rewrite; one lane owns `android/` during A0; user coordinates with PR #84 author |
| 6 | Default Android style | Native (Material You dynamic colors), state colors matching iOS; Claude / Clay / Island / Lark in batch two |
| 7 | First Android scope | 76 iOS P0 features + Share to Persona + public direct connectivity, Chinese-ROM support, in-app updates; terminal, Live Updates, phone tools in batch two |
| 8 | hub changes | Public connectivity (hub-changes.md Section 1) in P0; WS completion, per-device phone tools, connection records with first Android release; `client_ops` alongside Android outbox |
| 9 | Public-connectivity details | Separate TLS port (same-port TLS detection deferred); phones paired with PR #84 without fingerprints fetch one over authenticated LAN and pin it; hub identity-key changes require QR re-pairing, next-key `hello` announcements only for planned rotation. With public connectivity enabled, master tokens accepted only from loopback/LAN; unauthenticated `/health` returns only `ok`, `hub_id`, TLS port/fingerprint publicly, unchanged on LAN |
| 10 | Android-specific capabilities | No SMS, call history, other-app notification reading; any later implementation needs separate toggles/confirmation |
| 11 | Watch | No Wear OS app in first release; notifications bridge automatically |
