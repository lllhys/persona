# Plan: align the Android app with iOS (#4194)

Status: plan, written 2026-11-11 against `main` at 309b3fe76; the two area documents against c57caeddc. The owner settled nine of the questions, the spec settles a tenth, and one is open (below). Each phase and each area has its own issue and its own document, with the requirements first and the technical design second. When a phase is done, its rules move into `docs/design/DESIGN_SYSTEM.md` and `docs/android/android-design.md` and its document is deleted; when every phase is done, this folder goes.

## Goal

Someone who uses Persona on an iPhone and on an Android phone gets the same app: the same screens, the same actions in the same places, the same words, colors, spacing and motion, and the same features. iOS is the reference implementation, and the measure is `docs/design/DESIGN_SYSTEM.md`, not a pixel copy of the iPhone. Each platform keeps rendering natively, and there is no shared UI code. Where the platform forces a difference, the difference is written down with its reason.

## Ground rules

- Only the Glass style is aligned. Brief and Board keep working but get no new alignment work; the iOS-only styles (Clay, Island, Lark, Beautiful) are not ported.
- Principle 2 in `docs/design/PRINCIPLES.md`: a change that brings an Android screen in line with the spec is a fix. Anything else that changes a default look is a new style or theme color.
- `DESIGN_SYSTEM.md` section 1.4: a change to navigation, to what a primary screen contains, or to how pending requests look or work is a large change, and the owner looks at its screenshots before merge.
- Every UI change shows Android next to iOS, light and dark. Phase 4 builds the tool; until then the screenshots are taken by hand.
- One PR per item, linked to its phase issue. A rule that lands also lands in `DESIGN_SYSTEM.md` and gets its entry in `docs/design/DECISIONS.md`, in the same PR.

## Owner decisions (2026-11-11)

| # | Question | Decision | What it means for Android |
| --- | --- | --- | --- |
| 1 | The needs-you color (DS-01) | Blue, as iOS shows it today | Nothing visible changes. `spec/tokens`, Android's generated value and the docs say blue; the web's amber is a separate fix. |
| 2 | The contrast policy | Follow iOS | Android uses the iOS color values and the iOS floors: links 3:1, white on accent 3.5:1, bubble text 4.5:1. The text contrast rule in `DESIGN_SYSTEM.md` gets the matching exception. |
| 3 | Swiping between home destinations, dragging out the sidebar | Not on Android | The avatar at the top left opens the sidebar and the switcher changes pages, as today. The swipe that closes an Agents row stays, since nothing competes with it. |
| 4 | Where pending requests sit in a chat | Follow iOS (2026-11-12) | The request cards stack above the composer, which stays usable; each folds to a one-line preview, and past half the page they collapse into "N pending". `DESIGN_SYSTEM.md` section 4.8 is corrected with the change ([phase 2](phase-2-interactions.md), T6; [chats](chats.md), C10). |
| 5 | The Inbox swipe direction | Follow iOS | The owner keeps Android's own direction only where it is a platform convention, and Android has none for swipe actions. |
| 6 | An in-app text size on Android | Open | Proposal: Small, Standard and Large, as on iOS. |
| 7 | Feedback | One layer, modeled on the iOS toast | Snackbars, per-page hosts and in-app system toasts all go through one component. |
| 8 | Styles | Glass only | See the ground rules. |
| 9 | Which assistant's computers the Agents page shows | Settled by `docs/assistant/managers.md` ("Clients"): the current assistant's | Nothing: Android does this already. iOS shows every assistant's, and gets an iOS issue. |
| 10 | Android's assistant home page | Drop it, as on iOS | New Chat is the only blank page, and Back from a conversation goes to the main conversation ([chats](chats.md), C11). |
| 11 | The Agent chat header's terminal and permission-mode buttons | Drop both, as on iOS | The terminal opens from ⋯ and the work line, the mode from the composer's model menu ([chats](chats.md), C3 and C7). |

Question 6 is tracked in #4208. Decisions 10 and 11 were taken there on 2026-11-11, decision 4 on 2026-11-12.

## Phases

| Phase | Issue | Document | What it delivers |
| --- | --- | --- | --- |
| 0 | #4208 | [Decisions and baseline](phase-0-decisions-and-baseline.md) | The decisions recorded, a living parity list, an Android audit against the spec |
| 1 | #4209 | [Tokens and guards](phase-1-tokens-and-guards.md) | One token source and checks that catch drift; iOS spacing, motion, type, colors and contrast; an icon map; lint guards; hit targets; haptics; one feedback layer |
| 2 | #4210 | [Interactions](phase-2-interactions.md) | iOS interactions where the platform allows; the rest written down |
| 3 | #4211 | [Feature gaps](phase-3-feature-gaps.md) | The features Android lacks, most-used first |
| 4 | #4212 | [Staying aligned](phase-4-staying-aligned.md) | Paired screenshots, iOS reading the shared fixtures, a self-check line, the intentional-differences section |

## Areas

The phases compare the platforms by kind of work. Two primary screens differ in so many places that each also gets a document of its own, comparing the screen as a whole; they run alongside phases 2 and 3.

| Area | Issue | Document | What it delivers |
| --- | --- | --- | --- |
| The Agents page | #4338 | [Agents page](agents-page.md) | Needs You by session, the groupings in the iOS shapes, kept folds, the iOS rows and headers, search |
| The Agent chat and the assistant chat | #4339 | [Chats](chats.md) | Tool names, subagents, the header, the work summary, the composer's model button, the assistant conversation's requests, records and history |

The Agents page area closed on 2026-11-12 and the chats area on 2026-11-13; each document lists what still differs ("Left at closing").

Phase 0 and the token pipeline in phase 1 come first. The paired screenshots from phase 4 should exist before the large changes in phases 2 and 3 and in the areas, because reviewing those depends on them. Phases 2 and 3 and the two areas can run side by side.

## Intentional differences so far

| Difference | Why |
| --- | --- |
| No swiping between home destinations or dragging out the sidebar | Owner decision (3); the screen edges are Android's back gesture |
| Bundled OFL fonts instead of SF Pro | SF Pro can't be shipped on Android |
| Phosphor icons instead of SF Symbols, matched by meaning | SF Symbols can't be shipped on Android |
| Liquid Glass drawn with the Haze library, with plainer fallbacks | No system Liquid Glass on Android; see `docs/android/android.md` ("Interface style performance") |
| 48dp touch targets instead of 44pt | Each platform's own minimum |
| The ongoing notification instead of Live Activities | No Dynamic Island on Android |
| Launcher shortcuts and Quick Settings tiles instead of Siri, Control Center, Watch and CarPlay | Platform features |
| A foreground service instead of push | The app doesn't depend on Google services |
| The right-edge scrubber, the recent-photos grid, the blurred lift-up menu, in-app speech recognition | See [phase 2](phase-2-interactions.md) |
| No Personal Agents entry or page on the Agents page, for now | Deferred on 2026-11-12: few people use personal agents ([Agents page](agents-page.md), A11) |

## Out of scope

- A pixel copy of iOS, and shared UI code (Compose Multiplatform).
- Porting the iOS-only styles.
- The web, apart from the needs-you color, and the frozen Mac app.
