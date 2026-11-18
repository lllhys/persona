import { conversationKeyDeleted } from "./conversation-deleted";
// What was sent from the composer, per conversation, for ↑/↓ recall and the Ctrl+R search (issue #2434; Moshi keeps the
// last 100 sent messages of each session and searches the ones of all of them). Stored on this browser only, never
// sent anywhere. Everything here is pure or takes the storage as an argument, so it is tested without a DOM.

export const HISTORY_MAX = 100;
export const HISTORY_PREFIX = "persona.sent.";

export interface SentEntry {
  /** The message as it was typed. */
  text: string;
  /** When it was sent (ms): orders entries of different conversations in the search. */
  at: number;
}

export type HistoryStore = Pick<Storage, "getItem" | "setItem" | "length" | "key">;

/** localStorage, or nothing where the browser has none (a private mode that throws, a test without a DOM). */
export function browserStore(): HistoryStore | undefined {
  try {
    return typeof localStorage === "undefined" ? undefined : localStorage;
  } catch {
    return undefined;
  }
}

const storageKey = (draftKey: string) => HISTORY_PREFIX + draftKey;

function parse(raw: string | null): SentEntry[] {
  if (!raw) return [];
  try {
    const v: unknown = JSON.parse(raw);
    if (!Array.isArray(v)) return [];
    return v
      .filter((e): e is SentEntry => Boolean(e) && typeof (e as SentEntry).text === "string" && typeof (e as SentEntry).at === "number" && (e as SentEntry).text.trim() !== "")
      .slice(-HISTORY_MAX);
  } catch {
    return [];
  }
}

/** The conversation's sent messages, oldest first. */
export function readHistory(draftKey: string, store: HistoryStore | undefined = browserStore()): SentEntry[] {
  if (conversationKeyDeleted(draftKey)) return [];
  try {
    return store ? parse(store.getItem(storageKey(draftKey))) : [];
  } catch {
    return [];
  }
}

/** The list after sending `text`: blank messages are not kept, an earlier copy of the same text moves to the end
 *  (recalling it once is enough), and only the last HISTORY_MAX stay. */
export function withSent(list: SentEntry[], text: string, at: number): SentEntry[] {
  if (!text.trim()) return list;
  return [...list.filter((e) => e.text !== text), { text, at }].slice(-HISTORY_MAX);
}

/** Remembers a message that went out. A full or blocked storage loses the entry, never the send. */
export function recordSent(draftKey: string, text: string, store: HistoryStore | undefined = browserStore(), at: number = Date.now()): SentEntry[] {
  if (conversationKeyDeleted(draftKey)) return [];
  const next = withSent(readHistory(draftKey, store), text, at);
  try {
    store?.setItem(storageKey(draftKey), JSON.stringify(next));
  } catch {
    /* storage full or disabled */
  }
  return next;
}

// ── ↑ / ↓ recall ─────────────────────────────────────────────────────────────

/** Whether ↑ / ↓ walk the history: the box is empty or still shows the message it recalled. A multi-line message
 *  keeps the arrows for its own lines until the caret is on the first (↑) or the last (↓) one. */
export function canRecall(o: { draft: string; shown: string | null; dir: "up" | "down"; firstLine: boolean; lastLine: boolean }): boolean {
  if (o.draft === "") return true;
  if (o.shown === null || o.draft !== o.shown) return false;
  return o.dir === "up" ? o.firstLine : o.lastLine;
}

/** One step: ↑ goes to an older message (the newest from an empty box, stopping at the oldest), ↓ to a newer one and
 *  past the newest back to an empty box. `index` -1 is the empty box. */
export function recallStep(length: number, index: number, dir: "up" | "down"): number {
  if (length === 0) return -1;
  if (dir === "up") return index < 0 ? length - 1 : Math.max(0, index - 1);
  if (index < 0) return -1;
  return index + 1 >= length ? -1 : index + 1;
}

/** Where the walk stands while the box shows a message being edited, untouched (#4503). If that message was sent from
 *  here, it stands on that entry (usually the newest). Otherwise it stands just past the newest. Either way the next ↑
 *  goes to an older message than the one being edited, and ↓ goes back to an empty box. */
export function recallIndexOf(list: SentEntry[], text: string): number {
  for (let i = list.length - 1; i >= 0; i--) if (list[i].text === text) return i;
  return list.length;
}

// ── keys ─────────────────────────────────────────────────────────────────────

export interface KeyLike {
  key: string;
  ctrlKey: boolean;
  metaKey: boolean;
  altKey: boolean;
  shiftKey: boolean;
  isComposing?: boolean;
  keyCode?: number;
}

/** An input method is composing (the Enter or arrow that picks a candidate is not ours). */
export const composing = (e: Pick<KeyLike, "isComposing" | "keyCode">): boolean => Boolean(e.isComposing) || e.keyCode === 229;

export const plainCtrl = (e: KeyLike, k: string) => e.ctrlKey && !e.metaKey && !e.altKey && !e.shiftKey && e.key.toLowerCase() === k;

/** Ctrl+R in the box opens the search (⌃ on a Mac too: ⌘R stays the browser's reload). */
export function opensSearch(e: KeyLike): boolean {
  return !composing(e) && plainCtrl(e, "r");
}

// Sent-text recall is also a local copy of the deleted conversation, including selected skill references.
export function forgetSentHistory(draftKey: string) {
  try { localStorage.removeItem(storageKey(draftKey)); } catch { /* Storage may be unavailable. */ }
}
