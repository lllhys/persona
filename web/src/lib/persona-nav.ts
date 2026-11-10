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

/**
 * Where the page goes when another assistant becomes the current one (the sidebar's switcher): a page about one thing
 * of the assistant left behind (a conversation, an agent, a computer, an inbox item, a knowledge entry, a to-do) goes
 * to that thing's list, which now shows the new assistant's; a conversation or an agent goes to the new assistant's
 * main conversation. null: the page stays (a list, Settings), and shows the new assistant's by itself.
 */
export function routeAfterSwitch(route: Route): Route | null {
  switch (route.name) {
    case "persona":
      return route.sid ? { name: "persona" } : null;
    case "session":
    case "tab":
    case "space":
    case "request":
      return { name: "persona" };
    case "machine":
      return { name: "machines" };
    case "inbox":
      return route.id ? { name: "inbox" } : null;
    case "kb":
      return route.id ? { name: "kb", ...(route.kind && { kind: route.kind }) } : null;
    case "todos": {
      if (!route.id) return null;
      const { id: _id, ...list } = route;
      return list;
    }
    case "agents":
      return route.id ? { name: "agents" } : null;
    default:
      return null;
  }
}

/**
 * Whether the page at route opens again when another assistant becomes current, or a kept pick that left the roster
 * goes back to the default: a page that stays (routeAfterSwitch null) and reads the assistant's things itself, once,
 * through the routes that name the pick (lib/manager-choice.ts managerHeaderFor): All chats, the assistant's profile,
 * Scheduled tasks and the knowledge base. Opened again, it reads the new one's and nothing it shows or sends is the
 * last one's. The other pages that stay follow the pick by themselves: the assistant page and the lists filtered from
 * the store (the Overview, Agents, computers, the Inbox, To-dos and their projects), and Settings' editors, which pick
 * their own target; the rest are no assistant's (the health space, Messages, the terminals, an extension's page).
 */
export function reopensOnSwitch(route: Route): boolean {
  return route.name === "history" || route.name === "profile" || route.name === "routines" || route.name === "kb";
}
