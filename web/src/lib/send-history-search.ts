// Only the Ctrl+R panel needs cross-conversation search; keep it out of the first-screen recall path.
import { browserStore, composing, HISTORY_PREFIX, plainCtrl, readHistory, type HistoryStore, type KeyLike, type SentEntry } from "./send-history";

export const SEARCH_MAX = 100;

/** Every conversation's sent messages, newest first: the candidates of the search. */
export function readAllHistory(store: HistoryStore | undefined = browserStore()): (SentEntry & { draftKey: string })[] {
  const all: (SentEntry & { draftKey: string })[] = [];
  try {
    if (store) for (let i = 0; i < store.length; i++) {
      const k = store.key(i);
      if (k?.startsWith(HISTORY_PREFIX)) {
        const draftKey = k.slice(HISTORY_PREFIX.length);
        all.push(...readHistory(draftKey, store).map((entry) => ({ ...entry, draftKey })));
      }
    }
  } catch {
    /* unreadable storage: no candidates */
  }
  return all.sort((a, b) => b.at - a.at);
}

/** Every query word must match, case-insensitively; newest first, one row per text, at most `max`. */
export function searchSent(candidates: SentEntry[], query: string, max: number = SEARCH_MAX): string[] {
  const words = query.toLowerCase().split(" ").filter(Boolean);
  const seen = new Set<string>();
  const out: string[] = [];
  for (const e of candidates) {
    if (seen.has(e.text)) continue;
    const text = e.text.toLowerCase();
    if (!words.every((w) => text.includes(w))) continue;
    seen.add(e.text);
    out.push(e.text);
    if (out.length >= max) break;
  }
  return out;
}

export type SearchAction = "up" | "down" | "pick" | "cancel";

/** ↑ ↓ and Ctrl+R/P/S/N move; Enter/Tab picks; Esc/Ctrl+G cancels; typing stays the search box's. */
export function searchAction(e: KeyLike): SearchAction | null {
  if (composing(e)) return null;
  const bare = !e.ctrlKey && !e.metaKey && !e.altKey && !e.shiftKey;
  if (bare && e.key === "ArrowUp") return "up";
  if (bare && e.key === "ArrowDown") return "down";
  if (bare && (e.key === "Enter" || e.key === "Tab")) return "pick";
  if (bare && e.key === "Escape") return "cancel";
  if (plainCtrl(e, "r") || plainCtrl(e, "p")) return "up";
  if (plainCtrl(e, "s") || plainCtrl(e, "n")) return "down";
  if (plainCtrl(e, "g")) return "cancel";
  return null;
}

/** The highlighted row after a move: ↑ goes toward the newest and wraps to the last. */
export function moveActive(active: number, count: number, action: "up" | "down"): number {
  if (count <= 0) return 0;
  return action === "up" ? (active - 1 + count) % count : (active + 1) % count;
}
