// Pinned Persona conversations (the iOS sidebar's "Pinned", `assistant.pinned`; Android's drawer): kept per browser, as the
// app keeps them per device. The main conversation is always first and is never in the list. The newest pin comes first.
// The hub's leader conversations are pinned on every browser, ahead of these (pinOrder).
import { useSyncExternalStore } from "react";
import { isAutoConversation } from "./conversation-origins";
import { firstAndCurrent } from "./persona-nav";
import { toggled } from "./pins";
import type { Session } from "./types";
import { byTime } from "./times";

const KEY = "assistant.pinned";
const listeners = new Set<() => void>();
let cached: { raw: string | null; ids: string[] } = { raw: null, ids: [] };

/** The stored ids (an unreadable or broken value is none). */
export function parsePinned(raw: string | null): string[] {
  if (!raw) return [];
  try {
    const v: unknown = JSON.parse(raw);
    return Array.isArray(v) ? v.filter((x): x is string => typeof x === "string" && x !== "") : [];
  } catch {
    return [];
  }
}

export function getPinnedConversations(): string[] {
  let raw: string | null = null;
  try {
    raw = localStorage.getItem(KEY);
  } catch {
    // storage blocked: no pins
  }
  if (raw !== cached.raw) cached = { raw, ids: parsePinned(raw) };
  return cached.ids;
}

function save(ids: string[]) {
  try {
    if (ids.length) localStorage.setItem(KEY, JSON.stringify(ids));
    else localStorage.removeItem(KEY);
  } catch {
    // storage blocked: the pin lasts until the page reloads at most
  }
  listeners.forEach((l) => l());
}

/** Pins `sid` (first), or takes its pin back. */
export function togglePinnedConversation(sid: string) {
  save(toggled(getPinnedConversations(), sid));
}

export function unpinAllConversations() {
  save([]);
}

if (typeof window !== "undefined") {
  // another tab pinned one
  window.addEventListener("storage", (e) => {
    if (e.key === KEY || e.key === null) listeners.forEach((l) => l());
  });
}

const subscribe = (l: () => void) => {
  listeners.add(l);
  return () => void listeners.delete(l);
};

export function usePinnedConversations(): string[] {
  return useSyncExternalStore(subscribe, getPinnedConversations, () => cached.ids);
}

/** The order of the sidebar's pinned Persona chats: the leader conversations first (the hub's `assistant.leaders`, the
 *  same on every browser and device, in their order), then this browser's own pins. A leader pinned here too is listed once. */
export function pinOrder(leaders: readonly string[] | undefined, pins: readonly string[]): string[] {
  const lead = leaders ?? [];
  return [...lead, ...pins.filter((id) => !lead.includes(id))];
}

/** The sidebar's "Persona chats" apart from the main one: the pinned ones in pin order (all of them), then the newest
 *  `limit` others, plus the open one when it is further down (#2283). An id pinned for a conversation that is gone is skipped.
 *  "Recent" has the user's own only: the ones the hub opened by itself are under "Automatic" (#3264), unless pinned. */
export function sidebarConversations(conversations: readonly Session[], main: string | undefined, pins: readonly string[], limit = 5, current?: string): { pinned: Session[]; recent: Session[] } {
  const others = conversations.filter((c) => c.session_id !== main);
  const order = new Map(pins.map((id, i) => [id, i]));
  const pinned = others.filter((c) => order.has(c.session_id)).sort((a, b) => order.get(a.session_id)! - order.get(b.session_id)!);
  const rest = others.filter((c) => !order.has(c.session_id) && !isAutoConversation(c)).sort((a, b) => byTime(b.updated_at, a.updated_at));
  return { pinned, recent: firstAndCurrent(rest, limit, (c) => c.session_id === current) };
}
