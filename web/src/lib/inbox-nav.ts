// Back from an Inbox item (#3329): the list it was opened from is some history entries back, as a Settings item's
// category page is (views/settings/nav.ts). An item opened from the list, or from an item opened that way (a row beside
// it on a wide screen, a related item, its line), carries how many; ‹ and Done go back that far instead of pushing
// another /inbox. An item reached any other way (a link, a notification, ⌘K) has no list under it.
import { takeOverlayEntry } from "./back-close";
import { navigate, routeAt, type Route } from "./router";

const STEPS = "personaInboxSteps";

/** How many entries back the list is from the entry `state` belongs to; 0: no list under it. */
export function stepsToInbox(state: unknown): number {
  const n = state && typeof state === "object" ? (state as Record<string, unknown>)[STEPS] : undefined;
  return typeof n === "number" && n > 0 ? n : 0;
}

/** The history state of an item opened from the page showing now: one step from the list, one more than the item it was
 *  opened from, or none when that item has no list under it either. */
export function openedItemState(): Record<string, unknown> | null {
  const here = routeAt(location.href);
  if (here?.name === "inbox" && !here.id) return { [STEPS]: 1 };
  const n = stepsToInbox(history.state);
  return n ? { [STEPS]: n + 1 } : null;
}

/** Opens `r` from an Inbox page; an item counts its way back to the list (`openedItemState`). */
export function openFromInbox(r: Route): void {
  navigate(r, false, r.name === "inbox" && r.id ? openedItemState() : null);
}

/** ‹ and Done on an item: back to the list entry it was opened from, else the list in place of the item. */
export function backToInboxList(): void {
  const n = stepsToInbox(history.state);
  if (!n) return navigate({ name: "inbox" }, true);
  // An overlay open over the item (a menu) has an entry of its own on top, carrying the item's state: off with it too,
  // its marker taken first so its close does not go back once more.
  if (!takeOverlayEntry()) return history.go(-n);
  history.replaceState(null, "", location.href);
  history.go(-n - 1);
}
