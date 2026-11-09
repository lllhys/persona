import { focusSource } from "./compatibility";
// Focus sync between herdr's own client (the full surface) and the page (docs/integrations/terminal.md "Focus sync"). herdr's focus is
// one state shared by all its clients, and it does not say who moved it, so the node attributes each change and the hub
// tells every view whether the change is its own (server/internal/hub/herdrclient.go). Pure logic here, tested without
// a DOM: what to do with a change (follow it, ignore its echo, only note it) and the guard that keeps the two
// directions from feeding each other:
//
//   herdr → page: a change this view made (a click in herdr's own sidebar) moves the page to that pane's address;
//   page → herdr: the page showing another address focuses its pane;
//
// and a follow must not send a focus back, nor a focus we sent come back as a follow.
import type { Route } from "./router";
import { canShowTerminal, terminalTarget } from "./session-view";
import type { Session } from "./types";

export type FocusSource = "client" | "persona" | "other";

/** herdr's focus moved (the hub's `herdr_focus`). */
export interface HerdrFocus {
  /** The view of this connection the message is for. */
  id: string;
  machine_id: string;
  workspace_id: string;
  tab_id: string;
  pane_id: string;
  /** The agent session in that pane, when there is one. */
  session_id?: string;
  /** "client": a full view of the node was just typed or clicked into; "persona": a focus the page asked for; "other": anything else. */
  source: FocusSource;
  /** The change was made in this view. */
  mine: boolean;
}

const str = (v: unknown) => (typeof v === "string" ? v : "");

export function parseFocus(msg: Record<string, unknown>): HerdrFocus | null {
  const pane_id = str(msg.pane_id);
  const id = str(msg.id);
  if (!pane_id || !id) return null;
  const source: FocusSource = focusSource(msg.source);
  const session_id = str(msg.session_id);
  return {
    id,
    machine_id: str(msg.machine_id),
    workspace_id: str(msg.workspace_id),
    tab_id: str(msg.tab_id),
    pane_id,
    ...(session_id && { session_id }),
    source,
    mine: msg.mine === true,
  };
}

/** What the terminal opens by for the pane of a change: its agent session's id, else the pane's "pane:" address. */
export function focusAddress(e: Pick<HerdrFocus, "pane_id" | "session_id">): string {
  return e.session_id || `pane:${e.pane_id}`;
}

export type FocusAction =
  /** The page is already on it. */
  | "same"
  /** The focus we asked for, coming back. */
  | "echo"
  /** Made in this view: the page goes there. */
  | "follow"
  /** Made elsewhere (the computer's window, another browser): the page stays, and says so. */
  | "elsewhere";

/**
 * What to do with a focus change. `shown` is the address the surface is on; `front` that this surface is the live one in
 * front (a surface that is hidden, released or still opening never moves the page); `sent` that the page asked for this
 * address a moment ago (FocusGuard.wasSent).
 */
export function decideFocus(e: HerdrFocus, now: { shown: string; front: boolean; sent: boolean }): FocusAction {
  const address = focusAddress(e);
  if (address === now.shown) return "same";
  if (e.source === "persona" && e.mine) return "echo";
  if (now.sent) return "echo"; // the node lost track of its own mark (a restart): the address is the one we sent
  if (e.source === "client" && e.mine && now.front) return "follow";
  return "elsewhere";
}

/** How long a focus the page sent is remembered as the page's own (the node's mark lives 3 s). */
export const SENT_MS = 3000;
/** How long a follow is remembered, for the page it opened not to send a focus back. */
export const FOLLOW_MS = 2000;

/** Keeps the two directions apart (see the top of this file). One per page. */
export class FocusGuard {
  private sent = new Map<string, number>();
  private followed: { address: string; at: number } | null = null;

  /** The page asked herdr to focus this address. */
  noteSent(address: string, now = Date.now()) {
    this.sent.set(address, now);
    for (const [a, at] of this.sent) if (now - at > SENT_MS) this.sent.delete(a);
  }

  wasSent(address: string, now = Date.now()): boolean {
    const at = this.sent.get(address);
    return at !== undefined && now - at <= SENT_MS;
  }

  /** The page is about to move to this address because herdr's focus moved there. */
  noteFollow(address: string, now = Date.now()) {
    this.followed = { address, at: now };
  }

  /** The page arrived at `address`: true once if it got there by a follow, which must not be sent back to herdr. */
  consumeFollow(address: string, now = Date.now()): boolean {
    const f = this.followed;
    if (!f || f.address !== address) return false;
    this.followed = null;
    return now - f.at <= FOLLOW_MS;
  }

  clear() {
    this.sent.clear();
    this.followed = null;
  }
}

export const focusGuard = new FocusGuard();

/** Where the page goes to follow a change: an agent's pane to its page in the terminal view, any other pane to the "Terminal" page. */
export function followRoute(mid: string, e: Pick<HerdrFocus, "pane_id" | "session_id">, sessions: readonly Session[]): Route {
  if (e.session_id) {
    const s = sessions.find((x) => x.session_id === e.session_id);
    if (s && canShowTerminal(s)) return terminalTarget(mid, e.session_id, sessions);
  }
  return { name: "terminal", mid, id: `pane:${e.pane_id}` };
}
