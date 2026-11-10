# Proposal: a continuous improvement loop, Opus verifying cheaper execution models

Status: proposal (#3630), waiting for the owner's approval. The loop does not run until then. Facts about the relay gateway
below come from its model list, its own setup page and a few small test calls on 2026-11-10; nothing in routing or default
models was changed.

## 1. Layers

- **Opus is the brain**: splits the work, decides direction, writes the logic, verifies every result and owns quality.
- **Execution models do scoped work** by task type: frontend and motion go to Kimi K3.1 Pro; batch, mechanical edits go to a
  flash-class model. Every execution result is verified by Opus before it merges; nothing an execution model wrote merges
  on its own say-so.
- **"Always Day One"** is `auto_model/alwaysday1` (and `auto_model/alwaysday1_max`) in the relay's catalog: a relay-side
  automatic model that picks among candidates. A test call was answered by a Seed model
  (`doubao-seed-evolving-latest-version`). It is not used in the table below until we know which models it picks and when.

## 2. The relay's "hybrid mode"

What it is: a client setup in which Claude Code keeps the caller's own Anthropic credential, and the relay switches **per
request by model name**: bare Claude names pass through to Anthropic on the caller's credential; catalog names
(`provider/model`, for example `ark/kimi-k3.1-pro`) go to the relay's managed routes. Codex has the same mode (bare names on
the ChatGPT login, catalog names managed).

- **Not a planner/executor router.** The relay does not split a task into planning and execution or pick a weaker model
  for parts of it; it only routes each request by the name the client sends. The planning split stays in Claude Code:
  the main session plus sub-agents whose definitions name their model.
- **Could it carry our dispatch?** In principle yes, and it is the preferred way: one base URL, Opus by bare name, each
  sub-agent by catalog name, nothing of our own to run. Today it cannot: passthrough needs a caller-owned Anthropic
  credential, our Opus comes through another gateway, and the owner's own subscription is out of bounds. Using it needs
  the owner's decision on which credential passes through.
- **Until then**: the execution agent runs as its own Claude Code session on a per-run settings file kept outside the
  repository (`.claude/agents/frontend-motion.md`, "Running this agent"). Defaults and routing stay as they are. Note
  that `CLAUDE_CODE_SUBAGENT_MODEL` overrides every agent's `model`, so the Agent tool alone does not reach K3.1 here.

## 3. Routing table

| Task type | Model (relay name) | Example | Opus verifies |
| --- | --- | --- | --- |
| Planning, logic, server, protocol, counts, review | Opus (main session) | change how "Not done" is counted | it is the author; a second Opus pass reviews the diff |
| Frontend and motion on web, iOS, Android | Kimi K3.1 Pro (`ark/kimi-k3.1-pro`), agent `frontend-motion` | web sidebar folds in the iOS timing (#3629) | diff against the checklist in section 5, screenshots and key frames, `npm test` / build |
| Batch, mechanical edits | flash class: DeepSeek V4.1 Flash (`opensource/deepseek_v4_1_flash`) | rename a token across 40 files, fill a missing translation key | full diff read, tests, privacy scan |
| Unknown or mixed | Opus | anything that does not fit a row | n/a |

Availability on 2026-11-10: K3.1 Pro answered through the relay and in `claude -p`; DeepSeek V4.1 Flash timed out twice
(90 s, no bytes) and GLM 5.3 Flash returned 503 `upstream_unavailable`. The batch row is on hold until one of them answers;
until then batch work stays with Opus.

## 4. Cadence

- **One item per working day.** Opus picks it from open to-dos and experience issues (bugs and UI problems first,
  smallest well-defined one), claims the issue and writes the brief.
- **Flow**: execute (execution model) → verify (Opus, section 5) → PR → merge → deploy → live check. Deploys need the hub
  machine and are done by Persona's product lead; the loop hands over the merged PR and the check command, and does not
  deploy itself.
- **The owner's attention is the first principle.** The loop creates no new pending items for the owner: no questions,
  cards or reviews asked of them. Only finished results are reported, grouped by direction; process (attempts, bounces,
  retries) stays in the issue and PR. Anything that needs a product decision is set aside, not asked.

## 5. Keeping execution models to the repository's rules

- **Injected before work**: `docs/design/PRINCIPLES.md`, `docs/design/DESIGN_SYSTEM.md`, `docs/design/HIG_AUDIT.md`,
  `CLAUDE.md`, `docs/operations/collaboration.md`, `.github/pull_request_template.md`, plus the platform doc and the iOS code
  the brief names. The agent definition requires reading them; the brief quotes the sections that matter.
- **Opus checklist**, item by item, written into the PR as "Opus verification": PRINCIPLES.md 1 to 6; the DESIGN_SYSTEM.md
  "UI change self-check"; no new kind of finding from HIG_AUDIT.md; repository conventions (folder, naming, component and
  test style of the neighbouring code, English, privacy scan, tokens instead of literals, docs updated with new tokens);
  reduced motion; counts and states unchanged; screenshots light and dark.
- **Bounce**: any item that fails goes back to the execution model with the failing item quoted. After two bounces Opus
  fixes it itself or drops the item and says why in the issue; the PR says which parts the execution model wrote and
  which Opus changed.
- **Against drift, automate what can be checked**: the existing ratchets stay the gate (`TokenRatchetTests`,
  `TypeScaleTests`, `IconVocabularyTests`, `ContinuousMotionTests`, `theme-contrast.test.ts`,
  `android_token_baseline.py`). To add: a web ratchet for literal durations, sizes and colors in `web/src/views`; a check
  that a UI PR's body has the principles self-check filled; a check that the paths the agent definition requires still
  exist. A violation found after merge becomes a case in PRINCIPLES.md and, where possible, a new check.

## 6. Acceptance and rollback

- **A round is done** when the PR is merged with its evidence (tests, screenshots, Opus verification), the product lead has
  deployed it, and the live check passed; the issue is closed with the deployed version.
- **Quality not met** before merge: the bounce rule above. After merge: a revert PR on the same issue, the issue reopened,
  and the lesson added to `docs/lessons/` or PRINCIPLES.md "Cases". An undo is never redone silently by the next round.
- **The loop pauses itself** after two reverted rounds in a row, and reports that once as a result.
