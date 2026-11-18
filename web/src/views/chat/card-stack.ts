// The cards above the composer as one slot: the first waiting card in full with "2 / 6" on it, the next one taking
// its place once it is answered (its answer buttons where the last one's were), and what was just answered as one
// line over it that goes a few seconds after the last answer. Before, every card waiting and every answered receipt
// had a card of its own on a wide screen: a run of eleven settings approvals stacked half the page and moved the
// buttons with every answer. This file is the part with no DOM: the run's count, the answered line's words and when
// it goes. PendingCards (Interactions.tsx) and AnsweredSummary draw them.
import { t } from "../../i18n";
import type { AnsweredCard } from "../../lib/store";

/** How long the answered line stays after the last answer, unless the pointer or the keyboard is on it, or it is open.
 *  The store keeps each answer longer (ANSWERED_MS): the agent's state lags behind it (answered-lookup.ts). */
export const ANSWERED_SHOWN_MS = 3_000;

/**
 * The cards seen waiting since the stack was last empty, in the order they came: one more each time a card comes,
 * none once nothing waits. A card that goes (answered here or anywhere else) stays in the run, so the count says how
 * far through it the user is.
 */
export function nextRun(seen: readonly string[], waiting: readonly string[]): readonly string[] {
  if (!waiting.length) return seen.length ? [] : seen;
  const added = waiting.filter((id) => !seen.includes(id));
  return added.length ? [...seen, ...added] : seen;
}

/** Where the first waiting card stands in the run: "position / total" (the run holds every waiting card). */
export function runPosition(seen: readonly string[], waiting: number): { position: number; total: number } {
  const total = Math.max(seen.length, waiting);
  return { position: total - waiting + 1, total };
}

/** How the answers in the line went. Only a tool approval and a plan say allow / deny; the rest are just answered. */
export interface AnsweredTally {
  count: number;
  allowed: number;
  denied: number;
  handedBack: number;
}

export function answeredTally(cards: readonly AnsweredCard[]): AnsweredTally {
  const out: AnsweredTally = { count: cards.length, allowed: 0, denied: 0, handedBack: 0 };
  for (const c of cards) {
    const r = c.resolution;
    if (r.dismiss === true) out.handedBack++;
    else if (c.ix.kind === "permission" ? r.allow === true : c.ix.kind === "plan" && r.approve === true) out.allowed++;
    else if (c.ix.kind === "permission" ? r.allow === false : c.ix.kind === "plan" && r.approve === false) out.denied++;
  }
  return out;
}

/** The line's words after its count: "All allowed" when each was an approval that was allowed, else how many were
 *  allowed, denied and handed back; empty when none of them is either (questions, forms). */
export function tallyDetail(tally: AnsweredTally): string {
  if (tally.allowed === tally.count) return t("chat-interactions.answered_all_allowed");
  return [
    tally.allowed ? t("chat-interactions.answered_allowed", { count: tally.allowed }) : "",
    tally.denied ? t("chat-interactions.answered_denied", { count: tally.denied }) : "",
    tally.handedBack ? t("chat-interactions.answered_handed_back", { count: tally.handedBack }) : "",
  ].filter(Boolean).join(" · ");
}

/** The latest answer's time (the store's `at`; 0 for none). */
export function latestAnswer(cards: readonly AnsweredCard[]): number {
  return cards.reduce((m, c) => Math.max(m, c.at ?? 0), 0);
}

/** When the line goes: ANSWERED_SHOWN_MS after the latest answer, or after the user last left it (`released`). */
export function answeredHideAt(latest: number, released: number): number {
  return Math.max(latest, released) + ANSWERED_SHOWN_MS;
}

/** Whether the line shows: while held (open, hovered, focused), else until its time is up. */
export function answeredShown({ now, latest, released, held }: { now: number; latest: number; released: number; held: boolean }): boolean {
  return held || now < answeredHideAt(latest, released);
}

/** The answers the open line lists: those it listed when opened that the store has since let go of, then the store's
 *  (newest last). Open, nothing goes from under the user's eyes. */
export function keptAnswers(kept: readonly AnsweredCard[], live: readonly AnsweredCard[]): AnsweredCard[] {
  const ids = new Set(live.map((c) => c.ix.id));
  return [...kept.filter((c) => !ids.has(c.ix.id)), ...live].sort((a, b) => (a.at ?? 0) - (b.at ?? 0));
}
