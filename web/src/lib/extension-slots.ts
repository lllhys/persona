// The extensions' places outside their settings (docs/extensions/extensions.md "UI slots"): entries on the sidebar (nav_page), cards on
// "Overview" (overview_card), sections on a machine's page (machine_section), inbox items' pages (inbox_item: the list is
// cached here, so opening an item asks nothing). One list for the whole web, asked for once
// (GET /api/v1/extensions/-/ui without a placement) and again on reconnect, when an extension changes here, and when the tab
// comes back after a while; a nav_page's badge changes in place on the extension_ui event (plugin_ui from an older
// hub). Only on a hub with UI slots (extension_ui_slots, or plugin_ui_slots): an older one is never asked (it would answer an empty list anyway).
// The list this browser was last given is kept (docs/web/web.md "Extension pages on reload"): drawn from the first paint,
// before the hello, and replaced by the hub's answer.
import { defaultKnown, defaultManager, fromDefaultManager, messageManager, onDefaultManager, otherManager } from "./manager-choice";
import { feedCacheScope, scopedKey } from "./feed-cache";
import { useEffect, useMemo, useSyncExternalStore } from "react";
import { applyBadge, inboxItemPlace, placesOf, uiPlaces, type BadgeEvent, type SlotName, type UIPlace } from "./extension-ui";
import { isResumed } from "./reconnect";
import { getState, onHubEvent, useStore } from "./store";
import { extIdOf, hubHasExt, newRoutes } from "./extension-api";

/** Whether the hub has UI slots: extension_ui_slots (#1844), or plugin_ui_slots before it. */
export const hasSlots = (features: string[]) => hubHasExt(features, "ui_slots");

// Kept under the feed cache's hub and sign-in scope with the default manager it was read under. `kept`: that manager
// while the list shown is the kept one (null once the hub answered); `placesScope`: whose the list shown is.
const KEPT = "ext-places";
let kept: string | null = null;
let placesScope = feedCacheScope();
let places: UIPlace[] = keptPlaces();
let asked = 0; // when the list was last asked for
let generation = 0;
let started = false;
let helloVersion = ""; // the hub version the last hello named
const listeners = new Set<() => void>();
// Another manager's places (several managers, docs/assistant/managers.md "Clients"): its inbox items' pages and its computers'
// sections are its own extensions'. Asked for with its name when a page first needs them, again with the default's.
const others = new Map<string, UIPlace[]>();
const othersAsked = new Map<string, number>(); // the generation each was last asked in
const NONE: UIPlace[] = [];

function keptPlaces(): UIPlace[] {
  try {
    const v = JSON.parse(localStorage.getItem(scopedKey(placesScope, KEPT) ?? "") ?? "null");
    const list = v.places.filter((u: UIPlace) => [u.extension, u.id, u.title, u.placement].every((x) => typeof x === "string"));
    kept = String(v.manager);
    return list;
  } catch {
    return []; // nothing (readable) kept, signed out, or no storage: the hub's answer, as before
  }
}

/** The list shown, kept for the next load unless `scope` (whose it is) has signed out or changed since. */
function set(next: UIPlace[], scope = placesScope) {
  if (next === places) return;
  places = next;
  placesScope = scope;
  kept = null;
  try {
    const key = scopedKey(scope, KEPT);
    if (key) localStorage.setItem(key, JSON.stringify({ manager: defaultKnown() ? defaultManager() : "", places: next }));
  } catch {
    // storage full or off: the next load waits for the hub, as before
  }
  listeners.forEach((l) => l());
}

function setOther(manager: string, next: UIPlace[]) {
  if (others.get(manager) === next) return;
  others.set(manager, next);
  listeners.forEach((l) => l());
}

/** Asks the hub for another manager's places, unless asked already since the last refresh. */
function askOther(manager: string) {
  if (othersAsked.get(manager) === generation || !hasSlots(getState().features)) return;
  const g = generation;
  othersAsked.set(manager, g);
  uiPlaces("", undefined, undefined, manager).then(
    (list) => othersAsked.get(manager) === g && setOther(manager, list),
    () => {}, // nothing of its own: its items keep their own detail
  );
}

/** Asks the hub again (an extension turned on or off, installed, upgraded…), the other managers' asked for too. */
export function refreshExtensionPlaces() {
  const g = ++generation;
  const { features } = getState();
  if (!hasSlots(features)) {
    // before the hello what is kept stays; a hub without slots has none
    if (features.length) set(places.length ? [] : places, feedCacheScope());
    return;
  }
  asked = Date.now();
  const scope = feedCacheScope();
  uiPlaces("").then(
    (list) => g === generation && set(list, scope),
    () => {}, // keep what is shown; the next reconnect asks again
  );
  for (const m of others.keys()) askOther(m);
}

function start() {
  if (started || typeof window === "undefined") return;
  started = true;
  onHubEvent("hello", (msg) => {
    // resumed: missed plugin_ui pushes are replayed; but a hub that came back as another version may run other extensions
    const version = String(msg.server_version ?? "");
    const same = version === helloVersion;
    helloVersion = version;
    if (!isResumed(msg) || !same) refreshExtensionPlaces();
  });
  // A hub with the new names sends each badge twice, as extension_ui and (for older apps) plugin_ui: the old one is
  // only read from a hub that sends nothing else.
  const badge = (legacy: boolean) => (msg: Record<string, unknown>) => {
    if (legacy && newRoutes()) return;
    if (msg.invalidate) return; // not a badge (UI version 3)
    const ev = { ...(msg as unknown as BadgeEvent), extension: extIdOf(msg) };
    // the places come from the default manager (GET …/-/ui names none): another manager's badge of the same id is not
    // theirs but that manager's places'. Before the roster says which is the default none is taken: they are all read
    // again once it does.
    if (!defaultKnown()) return;
    if (fromDefaultManager(msg)) set(applyBadge(places, ev));
    else {
      const m = messageManager(msg);
      const theirs = others.get(m);
      if (theirs) setOther(m, applyBadge(theirs, ev));
    }
  };
  onHubEvent("extension_ui", badge(false));
  onHubEvent("plugin_ui", badge(true));
  // the default became known or moved: its places and badges are read again (those left out before), and the others';
  // a list kept under another default is not its
  onDefaultManager(() => {
    if (kept && kept !== defaultManager()) set([]);
    refreshExtensionPlaces();
  });
  document.addEventListener("visibilitychange", () => {
    if (document.visibilityState === "visible" && Date.now() - asked > 60_000) refreshExtensionPlaces();
  });
  helloVersion = getState().version;
  if (getState().conn === "online") refreshExtensionPlaces();
}

const subscribe = (l: () => void) => {
  start();
  listeners.add(l);
  return () => {
    listeners.delete(l);
  };
};

/** The places of one slot, in the hub's order; none on a hub without plugin_ui_slots. `manager`: whose extensions (an
 *  inbox item's, a computer's); none, or the default: the default's. */
export function useExtensionSlot(placement: SlotName, manager?: string): UIPlace[] {
  // null before the hello: the kept list, if any
  const on = useStore((s) => (s.features.length ? hasSlots(s.features) : null));
  const other = otherManager(manager);
  const snapshot = () => (other ? (others.get(other) ?? NONE) : places);
  const all = useSyncExternalStore(subscribe, snapshot, snapshot);
  useEffect(() => {
    if (on && other) askOther(other);
  }, [on, other]);
  return useMemo(() => (on !== false ? placesOf(all, placement) : []), [on, all, placement]);
}

/** One place by extension and id (a page opened from the sidebar or a card), if the hub listed it. */
export function useExtensionPlace(extension: string, id: string): UIPlace | undefined {
  const all = useSyncExternalStore(subscribe, () => places);
  return useMemo(() => all.find((u) => u.extension === extension && u.id === id), [all, extension, id]);
}

/**
 * The extension page that draws the inbox items of `source` (an inbox_item of the extension whose connector it is), if the hub
 * lists one; none on a hub without the slot (it lists none), and the item keeps its own detail. `manager`: the item's
 * (its own extensions draw it); none: the default's.
 */
export function useInboxItemPlace(source?: string, manager?: string): UIPlace | undefined {
  const items = useExtensionSlot("inbox_item", manager);
  return useMemo(() => inboxItemPlace(items, source), [items, source]);
}
