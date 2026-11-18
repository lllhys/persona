// The finished agents this page marked seen that the hub has not recorded yet (the app's SeenOutbox, #1588;
// docs/protocol/agent-states.md "Seen"). A `seen` sent over a socket that is still connecting, or that looks open but is
// gone (a laptop waking up, a tab coming back after its socket went quiet), is lost without a word: the hub kept the
// agent Unread and the page showed it green again with the next list. A mark stays until a list from the hub shows it
// recorded (a later `seen_at`) or the agent moved on; until then the agent is shown seen and the mark goes again
// (over HTTP when the socket's is not taken in time, over the socket again after a reconnect).
//
// Only the hub's own times are compared with each other (its `seen_at`, the machine's `updated_at`): the page's clock
// only says when to try again and when to give up.
import { stateOf } from "./format";
import { runtimeIdentity } from "./runtime-list";
import { timeMs } from "./times";
import type { Session } from "./types";

/** How long a mark sent over the socket gets before it goes again over HTTP, which answers. */
export const SEEN_CONFIRM_MS = 5_000;
/** Given up after this: the hub was out of reach all along. */
export const SEEN_KEEP_MS = 10 * 60_000;
/** The machine touched the finished agent's transcript without a new turn (the hub's own slack is 2 s). */
export const SEEN_SAME_TURN_MS = 2_000;

export interface SeenMark {
  /** The hub's `seen_at` and the agent's `updated_at` when it was marked (ms). */
  seenBefore: number;
  updatedBefore: number;
  /** The `seen_at` this page wrote into its own copy ("Mark all as seen"): not the hub's record. */
  localSeenAt?: string;
  /** On the page's clock. */
  markedAt: number;
  sentAt: number;
  route: "socket" | "http";
  /** The hub answered the HTTP request: nothing more to send, only the list to wait for. */
  answered: boolean;
}

/** By the agent's runtimeIdentity (lib/runtime-list.ts). */
export type SeenMarks = Readonly<Record<string, SeenMark>>;

const expired = (m: SeenMark, now: number) => now - m.markedAt > SEEN_KEEP_MS;

/** Finished and not seen yet: Unread, or Just finished (seen as far as its state goes, but nobody looked, #9751). */
export function unseen(s: Session): boolean {
  const st = stateOf(s);
  return st === "unread" || (st === "idle" && Boolean(s.work?.just_finished));
}

/** `s` as seen: no longer Unread, nor Just finished. */
export function seenCopy(s: Session): Session {
  return { ...s, state: "idle", ...(s.work?.just_finished ? { work: { ...s.work, just_finished: false } } : {}) };
}

/** A mark for `s` as the page has it now; only an unseen one (Unread or Just finished) waits for the hub. */
export function newMark(s: Session, now: number, localSeenAt?: string): SeenMark | undefined {
  if (!unseen(s)) return undefined;
  return { seenBefore: timeMs(s.seen_at), updatedBefore: timeMs(s.updated_at), localSeenAt, markedAt: now, sentAt: now, route: "socket", answered: false };
}

/** The hub's row says the mark is over: recorded, a new turn ended since, the agent works or needs you again, or the
 *  mark is too old. */
export function markOver(s: Session, m: SeenMark, now: number): boolean {
  const recorded = timeMs(s.seen_at) > m.seenBefore && s.seen_at !== m.localSeenAt;
  const newTurn = timeMs(s.updated_at) > m.updatedBefore + SEEN_SAME_TURN_MS;
  const st = stateOf(s);
  return recorded || newTurn || st === "working" || st === "needs_you" || expired(m, now);
}

/** The row as the page shows it: an unseen one whose mark still waits for the hub is seen. */
export function heldSeen(s: Session, m: SeenMark | undefined, now: number): Session {
  return m && unseen(s) && !markOver(s, m, now) ? seenCopy(s) : s;
}

/** `rows` as the page shows them; the same array when no mark holds any of them. */
export function heldRows(rows: Session[], marks: SeenMarks, now: number): Session[] {
  let out = rows;
  rows.forEach((s, i) => {
    const shown = heldSeen(s, marks[runtimeIdentity(s)], now);
    if (shown === s) return;
    if (out === rows) out = [...rows];
    out[i] = shown;
  });
  return out;
}

/** The marks still kept: the expired ones go (their agents were never listed again). */
export function liveMarks(marks: SeenMarks, now: number): SeenMarks {
  const gone = Object.keys(marks).filter((key) => expired(marks[key], now));
  if (!gone.length) return marks;
  const out = { ...marks };
  for (const key of gone) delete out[key];
  return out;
}

const waiting = (m: SeenMark, now: number) => m.route === "socket" && !m.answered && !expired(m, now);

/** Marks sent over the socket and not taken within SEEN_CONFIRM_MS: for HTTP now. */
export function dueMarks(marks: SeenMarks, now: number): string[] {
  return Object.keys(marks).filter((key) => waiting(marks[key], now) && now - marks[key].sentAt >= SEEN_CONFIRM_MS);
}

/** When the next mark sent over the socket is due (page clock, ms), if any. */
export function nextDue(marks: SeenMarks, now: number): number | undefined {
  const at = Object.values(marks).filter((m) => waiting(m, now)).map((m) => m.sentAt + SEEN_CONFIRM_MS);
  return at.length ? Math.min(...at) : undefined;
}

/** Not answered yet: sent again once the socket is back (an HTTP request that failed waits for that). */
export function unansweredMarks(marks: SeenMarks, now: number): string[] {
  return Object.keys(marks).filter((key) => !marks[key].answered && !expired(marks[key], now));
}
