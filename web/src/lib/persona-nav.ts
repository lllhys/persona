// The sidebar's entries around Persona (#1239): which row a route lights up, the pages under Persona, and how a long list
// keeps the open row. Pure, so the order and the selection are tested without a DOM.
import type { Route } from "./router";

/** The sidebar row a route selects. Persona's main conversation is one row, under `/`, `/persona` and `/persona/<main>` alike. */
export function sidebarActiveKey(route: Route, main?: string): string {
  switch (route.name) {
    case "session":
      return `s:${route.mid}/${route.sid}`;
    case "persona":
      return !route.sid || route.sid === main ? "persona" : `j:${route.sid}`;
    case "persona-new":
      return "j:new";
    case "health":
      return "health";
    case "history":
      return route.origin ? `auto:${route.origin}` : "history";
    case "machine":
      return "machines";
    case "agents":
      return route.id ? `a:${route.id}` : "agents";
    case "extension":
      return extensionNavKey(route.extension, route.ui);
    default:
      return route.name;
  }
}

/** An extension page's sidebar row (docs/extensions/extensions.md "UI slots" nav_page). */
export const extensionNavKey = (extension: string, ui: string) => `p:${extension}/${ui}`;

/** A shortened list's first `n`, and the open item after them when it is further down (#2283). */
export function firstAndCurrent<T>(list: readonly T[], n: number, isCurrent: (x: T) => boolean): T[] {
  const first = list.slice(0, n);
  const current = list.slice(n).find(isCurrent);
  return current ? [...first, current] : first;
}

/** The page entries under Persona, in the sidebar's order (the app's "Assistant" first, then the rest of its drawer). "Messages" shows
 * only in a browser that turned on "Show messages on home" (lark-chats.ts), on a hub with feature `lark`. The extensions' pages (nav_page) go right before "Settings". */
export const NAV_PAGES = ["home", "inbox", "health", "messages", "workspace-board", "todos", "routines", "kb", "machines", "terminal", "settings"] as const;
