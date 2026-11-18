// Where the app opens (#9071, docs/web/web.md "Start page"): Persona's main chat, unless the user chose another page in
// Settings › General › Start page. It also carries the sidebar's arrangement to and from the hub (below). The hub keeps the choice (GET / PUT /api/v1/navigation, its default manager's): a
// hub has one owner, so every device of theirs follows. This browser keeps a copy, so an opening decides before the
// first paint without waiting for the hub, and keeps the choice itself where the hub has no such route (404). Only an
// opening at the root address goes there (a new tab, the installed app, the hub's address typed): a reload, Back /
// Forward, a link to a page and the sidebar's Persona row are left alone. A page that is not there (its extension
// removed or turned off, Messages or the workflow board switched off) opens the main chat instead; the choice stays,
// and Settings says so.
import { useEffect, useRef, useSyncExternalStore } from "react";
import { api, ApiError } from "./api";
import { stripBase } from "./base";
import { hasSlots } from "./extension-slots";
import { NAV_PAGES } from "./persona-nav";
import { adoptSidebarPrefs, chosen, getSidebarPrefs, onSidebarPrefsSaved } from "./sidebar-pages";
import { isResumed } from "./reconnect";
import { navigate, type Route } from "./router";
import { onHubEvent, useStore } from "./store";

const KEY = "persona.startPage";

const listeners = new Set<() => void>();
const changed = () => listeners.forEach((l) => l());
let current = "";
try {
  current = localStorage.getItem(KEY) ?? "";
} catch {
  // storage off: the main chat
}

/** The choice as this browser has it: "" for the main chat, else a page's key as the sidebar names it (lib/persona-nav.ts,
 *  `p:<extension>/<page>` for an extension's). */
export const startPage = () => current;

/** Keeps `key` here: the hub's answer or push, or the choice itself where the hub can't keep it. */
export function keepStartPage(key: string) {
  current = key;
  try {
    if (key) localStorage.setItem(KEY, key);
    else localStorage.removeItem(KEY);
  } catch {
    // storage off: this page only
  }
  changed();
}

// Whether the hub keeps the choice: null until its answer, false for a hub (or a manager) without the route.
let synced: boolean | null = null;
export const startPageSynced = () => synced;

/** Says the hub keeps the choice (true), or answered `e` (false for a 404 / 405: it has no such route). */
export function noteStartPageSynced(e?: unknown) {
  const was = synced;
  if (!e) synced = true;
  else if (e instanceof ApiError && (e.status === 404 || e.status === 405)) synced = false;
  if (synced !== was) changed();
}

/** The route a page's key opens; null for the main chat and for a key this version does not know. */
export function startRoute(key: string): Route | null {
  if (key.startsWith("p:")) {
    const at = key.indexOf("/");
    return at > 2 && at < key.length - 1 ? { name: "extension", extension: key.slice(2, at), ui: key.slice(at + 1) } : null;
  }
  return key !== "settings" && (NAV_PAGES as readonly string[]).includes(key) ? ({ name: key } as Route) : null;
}

// The start page this load opened on and its address, until it is known whether the page is there (useStartPageCheck).
let opened: { key: string; path: string } | null = null;

/** At load, before the first paint (App): an opening at the root address goes to the start page instead. */
export function openStartPage() {
  const to = startRoute(current);
  if (!to || stripBase(location.pathname) !== "/" || location.search || location.hash) return;
  const entry = performance.getEntriesByType?.("navigation")[0] as PerformanceNavigationTiming | undefined;
  if (entry && entry.type !== "navigate") return; // a reload or Back / Forward stays on the page it was on
  navigate(to, true);
  opened = { key: current, path: location.pathname };
}

/** Whether the start page `key` is gone, `listed` saying whether the sidebar lists it now: undefined while that can't be
 *  told yet (the hub has not answered which extension pages there are, or whether the workflow board is on). */
export function startPageGone(key: string, listed: boolean, known: { extensions: boolean; board: boolean }): boolean | undefined {
  if (listed) return false;
  return (key.startsWith("p:") ? known.extensions : key !== "workspace-board" || known.board) ? true : undefined;
}

// The hub has answered which extension pages there are: their list changed after the hello, which is when
// lib/extension-slots.ts asks for it.
let answered = false;

const subscribe = (l: () => void) => {
  listeners.add(l);
  return () => void listeners.delete(l);
};
export const useStartPage = () => useSyncExternalStore(subscribe, startPage);
export const useStartPageSynced = () => useSyncExternalStore(subscribe, startPageSynced);
export const useExtensionPagesKnown = () => useSyncExternalStore(subscribe, () => answered);

/** In the sidebar's pages (useNavPages, always drawn): once it can be told whether the page this load opened on is there,
 *  one that is not opens the main chat in its place, unless the user has gone elsewhere since. */
export function useStartPageCheck(listed: (key: string) => boolean, extensionPages: readonly unknown[], boardKnown: boolean) {
  const online = useStore((s) => s.conn === "online");
  const slots = useStore((s) => hasSlots(s.features));
  // this list as it was at the first render after the hello: each caller's own, as each memoizes its list
  const first = useRef<readonly unknown[] | null>(null);
  useEffect(() => {
    if (!online) return;
    if (!first.current) first.current = extensionPages;
    else if (extensionPages !== first.current && !answered) {
      answered = true;
      changed();
    }
    if (!opened) return;
    const gone = startPageGone(opened.key, listed(opened.key), { extensions: answered || !slots, board: boardKnown });
    if (gone === undefined) return;
    if (gone && location.pathname === opened.path) navigate({ name: "persona" }, true);
    opened = null;
  });
}

// The sidebar's arrangement (lib/sidebar-pages.ts) goes the same way: each change made here is sent; while one is on its
// way the hub's pushes of it are not taken (an older one would undo a newer); one the hub did not take goes again at the
// next hello instead of being replaced by the hub's.
let sending = 0;
let unsent = false;
onSidebarPrefsSaved((pages) => {
  if (synced === false) return; // this browser only
  sending++;
  api<{ pages?: unknown }>("/navigation", { method: "PUT", json: { pages } })
    .then(
      (r) => {
        noteStartPageSynced();
        unsent = false;
        if (sending === 1 && r.pages) adoptSidebarPrefs(r.pages);
      },
      (e) => {
        noteStartPageSynced(e);
        unsent = synced !== false;
      },
    )
    .finally(() => sending--);
});

// The hub's choices, read at every hello that did not resume (a resumed one gets the pushes it missed) and pushed on
// each change. A hub that never stored one is handed the one this browser kept while there was no record.
onHubEvent("hello", (msg) => {
  if (isResumed(msg)) return;
  api<{ start?: string; pages?: unknown }>("/navigation").then(
    (r) => {
      noteStartPageSynced();
      const handover: Record<string, unknown> = {};
      if (typeof r.start === "string") keepStartPage(r.start);
      else if (current) handover.start = current;
      if (r.pages && !unsent) adoptSidebarPrefs(r.pages);
      else if (chosen(getSidebarPrefs())) handover.pages = getSidebarPrefs();
      if (Object.keys(handover).length) void api("/navigation", { method: "PUT", json: handover }).then(() => (unsent = false), () => {});
    },
    noteStartPageSynced, // the copies here stay
  );
});
onHubEvent("navigation", (msg) => {
  if (typeof msg.start === "string") keepStartPage(msg.start);
  if (msg.pages && !sending) adoptSidebarPrefs(msg.pages);
});
