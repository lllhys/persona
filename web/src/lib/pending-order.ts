// Ordering of the sidebar's Pending / Needs You section (#8167): the chat-like Agents list stays newest first, but the
// pending rows rank by what they need, and within one urgency class the one waiting longest leads. The section's
// membership and every count are decided elsewhere (views/sidebar-pending.ts, lib/format.ts); this file only orders.
import { timeMs } from "./times";
import type { Interaction, Session } from "./types";

/** The fields an ordering row has to offer (views/sidebar-pending.ts's SidebarPendingRow satisfies it as is). */
export interface PendingOrderItem {
  key: string;
  cards: readonly Pick<Interaction, "created_at">[];
  session?: Pick<Session, "state" | "state_since" | "updated_at" | "failure_reason" | "turn_error" | "attention_reason" | "awaits_reply">;
}

/**
 * The urgency classes, most urgent first:
 * 0 blocked on an answer — a pending card, or a host block that waits for the person at the terminal;
 * 1 unseen error / needs-check — a stopped turn the host flags (an error or an attention reason) with no card to answer;
 * 2 resurfaced reminder — a seen question left unanswered (`awaits_reply`, whose reminder the manager resends, #5096);
 * 3 unread completed turn — a finished turn nobody has seen.
 * The section only admits needs-you rows today, so classes 2 and 3 normally have no members: the ranking still holds
 * when a later definition lets them in, and degrades to the classes that are present.
 */
export type PendingUrgency = 0 | 1 | 2 | 3;

export const PENDING_URGENCY = {
  blockedAnswer: 0,
  needsCheck: 1,
  resurfacedReminder: 2,
  unreadTurn: 3,
} as const satisfies Record<string, PendingUrgency>;

/** A stopped turn the host says needs a look rather than an answer: it ended on an error, or carries an attention reason. */
function flaggedForCheck(item: PendingOrderItem): boolean {
  const s = item.session;
  return Boolean(s && (s.failure_reason || s.turn_error || s.attention_reason?.trim()));
}

/** Which urgency class a pending row belongs to. */
export function pendingUrgency(item: PendingOrderItem): PendingUrgency {
  if (item.cards.length > 0) return PENDING_URGENCY.blockedAnswer;
  const s = item.session;
  if (!s) return PENDING_URGENCY.blockedAnswer;
  if (flaggedForCheck(item)) return PENDING_URGENCY.needsCheck;
  if (s.awaits_reply) return PENDING_URGENCY.resurfacedReminder;
  // stateOf only reports "unread" from the hub's state field; the status fallback never produces it (lib/format.ts).
  if (s.state === "unread") return PENDING_URGENCY.unreadTurn;
  // A needs-you session without a card is a host block waiting on the person (docs/protocol/agent-states.md): same
  // class as a card, ordered by when its state began.
  return PENDING_URGENCY.blockedAnswer;
}

/** A row whose wait start cannot be dated follows every dated row in its class, instead of jumping to the front. */
export const UNKNOWN_PENDING_SINCE = Number.MAX_SAFE_INTEGER;

/**
 * When the wait began, in milliseconds: the oldest of the row's card timestamps and the session's state/update times,
 * so the longest wait leads. Every RFC3339 value is parsed as a time (lib/times.ts); nothing parseable means unknown.
 */
export function pendingSince(item: PendingOrderItem): number {
  const stamps = [
    ...item.cards.map((card) => timeMs(card.created_at)),
    timeMs(item.session?.state_since),
    timeMs(item.session?.updated_at),
  ].filter((ms) => ms > 0);
  return stamps.length ? Math.min(...stamps) : UNKNOWN_PENDING_SINCE;
}

/** Pending rows compared as the section draws them: urgency class, then the longest wait, then the stable key. */
export function comparePending(a: PendingOrderItem, b: PendingOrderItem): number {
  return pendingUrgency(a) - pendingUrgency(b)
    || pendingSince(a) - pendingSince(b)
    || a.key.localeCompare(b.key);
}

/** A new array of the rows in their pending order; the input array and its rows are left untouched. */
export function orderPending<T extends PendingOrderItem>(items: readonly T[]): T[] {
  return [...items].sort(comparePending);
}
