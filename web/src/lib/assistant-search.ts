import { conversationDeleted, conversationDeletionRevision, conversationDeletionScope } from "./conversation-deleted";
import { useStore } from "./store";
// Finding an assistant chat by what was said in it (#6283, docs/protocol/protocol.md "Assistant chat search (#6282)"):
// the manager's GET /api/v1/assistant/search, asked while ⌘K or "All chats" is searched, 200 ms after the last key,
// each new query cancelling the one before. Local matches (titles, last messages) show at once and the server's merge
// in (super rule 5); without the hub's assistant_search feature, or on a 404 from the current assistant's manager (an
// older build), there is nothing to merge and the local filter is the whole answer, as before.
import { useEffect, useMemo, useState, useSyncExternalStore } from "react";
import { api, ApiError, credentialOwner, enc, isAbort, subscribeCredentialOwner } from "./api";
import { shownManager, useShownManager } from "./manager-choice";

export const ASSISTANT_SEARCH_FEATURE = "assistant_search";

export interface AssistantSearchHit {
  item_id: string;
  seq: number;
  role: "user" | "assistant";
  ts?: string;
  snippet: string;
  /** [start, end) of each match in the snippet, UTF-16 (as the DOM counts). */
  ranges: [number, number][];
}

export interface AssistantSearchResult {
  session_id: string;
  machine_id: string;
  title: string;
  main?: boolean;
  archived?: boolean;
  origin?: string;
  updated_at?: string;
  title_match?: boolean;
  hits: AssistantSearchHit[];
  more_hits?: number;
}

export interface AssistantSearchAnswer {
  results: AssistantSearchResult[];
  /** Some chats are not read into the index yet, or it could not be searched this time. */
  partial: boolean;
  indexed?: { done: number; total: number };
}

/** The wait after the last key before asking (search in chat's too). */
export const SEARCH_SETTLE_MS = 200;

/** The managers that answered 404 (a build from before the search), by the manager the request named. */
const unsupported = new Set<string>();
const searchScope = () => JSON.stringify([credentialOwner(), conversationDeletionScope(), shownManager()]);
const liveAnswer = (answer: AssistantSearchAnswer): AssistantSearchAnswer => ({ ...answer, results: answer.results.filter((r) => !conversationDeleted(r.machine_id, r.session_id)) });

/** Asks the current assistant's manager; null when it cannot (no such route). */
export async function searchAssistantChats(query: string, signal?: AbortSignal): Promise<AssistantSearchAnswer | null> {
  const key = searchScope();
  if (unsupported.has(key)) return null;
  try {
    const a = await api<AssistantSearchAnswer>(`/assistant/search?q=${enc(query)}&limit=30`, { signal });
    if (signal?.aborted || key !== searchScope()) return null;
    return liveAnswer({ results: a?.results ?? [], partial: Boolean(a?.partial), indexed: a?.indexed });
  } catch (e) {
    if (e instanceof ApiError && e.status === 404) {
      if (!signal?.aborted && key === searchScope()) unsupported.add(key);
      return null;
    }
    throw e;
  }
}

/** The server's answer for `query` (trimmed) while `on`: undefined until it comes, and while nothing is typed. */
export function useAssistantSearch(query: string, on: boolean): { answer?: AssistantSearchAnswer; searching: boolean } {
  const q = query.trim();
  const owner = useSyncExternalStore(subscribeCredentialOwner, credentialOwner, credentialOwner);
  const manager = useShownManager();
  const epoch = useStore(conversationDeletionScope);
  const revision = useStore(conversationDeletionRevision);
  const scope = JSON.stringify([owner, epoch, manager]);
  const [state, setState] = useState<{ q: string; scope: string; answer?: AssistantSearchAnswer }>({ q: "", scope });
  useEffect(() => {
    setState({ q: "", scope });
    if (!on || !q) return;
    const ctrl = new AbortController();
    const id = window.setTimeout(() => {
      searchAssistantChats(q, ctrl.signal).then(
        (answer) => { if (!ctrl.signal.aborted && scope === searchScope()) setState({ q, scope, answer: answer ?? undefined }); },
        (e) => { if (!isAbort(e) && !ctrl.signal.aborted && scope === searchScope()) setState({ q, scope }); },
      );
    }, SEARCH_SETTLE_MS);
    return () => { clearTimeout(id); ctrl.abort(); };
  }, [q, on, scope]);
  useEffect(() => { setState((s) => s.answer ? { ...s, answer: liveAnswer(s.answer) } : s); }, [revision]);
  const current = on && q && state.q === q && state.scope === scope;
  const answer = useMemo(() => current && state.answer ? liveAnswer(state.answer) : undefined, [current, state.answer, revision]);
  return { answer, searching: Boolean(on && q && (!current || state.q !== q)) };
}

/** Characters a found chat's line keeps before its first match (#7045). The manager's snippet starts up to 40 before
 *  it, more than a phone-width row shows, so each client cuts for its own width, as the iPhone does (#6284). */
export const SNIPPET_LEAD = { phone: 8, wide: 20 } as const;

/** `snippet` from `lead` characters (code points, never half an emoji) before its first match, "…" in front when cut,
 *  with the ranges (UTF-16) moved to match. A match near the start is left as it is. */
export function leadSnippet(snippet: string, ranges: readonly [number, number][], lead: number): { snippet: string; ranges: [number, number][] } {
  const first = Math.min(snippet.length, ...ranges.map((r) => r[0]));
  const before = first > 0 ? Array.from(snippet.slice(0, first)) : [];
  if (!ranges.length || before.length <= lead + 1) return { snippet, ranges: ranges.map(([a, b]) => [a, b]) };
  const kept = before.slice(before.length - lead).join("");
  const shift = first - kept.length - 1; // "…" is one UTF-16 unit
  return { snippet: "…" + kept + snippet.slice(first), ranges: ranges.map(([a, b]) => [a - shift, b - shift]) };
}

/** The snippet cut at its matches: [text, matched] pieces (the ranges sorted, not overlapping, in range). `fit`: the
 *  row's width, to start the line just before the first match (`SNIPPET_LEAD`); left out, the whole snippet. */
export function markParts(snippet: string, ranges: readonly [number, number][], fit?: keyof typeof SNIPPET_LEAD): [string, boolean][] {
  if (fit) ({ snippet, ranges } = leadSnippet(snippet, ranges, SNIPPET_LEAD[fit]));
  const out: [string, boolean][] = [];
  let at = 0;
  for (const [a0, b0] of [...ranges].sort((x, y) => x[0] - y[0])) {
    const a = Math.max(at, Math.min(a0, snippet.length));
    const b = Math.max(a, Math.min(b0, snippet.length));
    if (a > at) out.push([snippet.slice(at, a), false]);
    if (b > a) out.push([snippet.slice(a, b), true]);
    at = b;
  }
  if (at < snippet.length) out.push([snippet.slice(at), false]);
  return out;
}

/** The chats a search shows: the server's results in its order (each with its newest hit), then the local matches it
 *  did not return (a chat not read into its index yet, or one it could not search), as they came. */
export function mergeSearch<T extends { session_id: string }>(local: readonly T[], answer: AssistantSearchAnswer | undefined, known: readonly T[], make: (r: AssistantSearchResult) => T): { chat: T; hit?: AssistantSearchHit }[] {
  if (!answer) return local.map((chat) => ({ chat }));
  const byId = new Map(known.map((c) => [c.session_id, c]));
  const out: { chat: T; hit?: AssistantSearchHit }[] = [];
  const seen = new Set<string>();
  for (const r of answer.results) {
    seen.add(r.session_id);
    out.push({ chat: byId.get(r.session_id) ?? make(r), hit: r.hits[0] });
  }
  for (const chat of local) if (!seen.has(chat.session_id)) out.push({ chat });
  return out;
}
